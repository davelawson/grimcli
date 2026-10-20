#!/usr/bin/env bash
# Secrets must not enter shell tracing, even when launched with bash -x.
set +x
set -u

usage() {
    cat <<'EOF'
Usage: grimcli-session.sh [--config PATH]

Interactively log in, manage lobbies, launch matches, draft, play complete games, and inspect catalogues.
Requires Bash, jq, and a compiled grimcli beside this script.
Drafting and gameplay require uuidgen; gameplay requires Bash 4.4 or newer.
Defaults come from grimcli.json in the current working directory.
Tokens, selections, catalogues and pending gameplay inputs stay in memory until exit.
Home: Create a lobby, Join a lobby, My lobbies, My matches, Account, Quit.
Lists use numbered choices, r or Enter to Refresh, and m to Enter ID manually.
On lobby and match screens, Enter also refreshes.
Use 0 for Back, q for Quit, and Escape to cancel input or leave a screen.
Escape at Home redisplays Home; during initial setup it exits.
EOF
}

fail() {
    printf '%s\n' "$1" >&2
    exit 2
}

config_path=grimcli.json
explicit_config=false
while (($#)); do
    case "$1" in
        --help|-h) usage; exit 0 ;;
        --config)
            (($# >= 2)) || fail '--config requires a path'
            config_path=$2
            explicit_config=true
            shift 2
            ;;
        *) fail "Unknown argument: $1" ;;
    esac
done

command -v jq >/dev/null 2>&1 || fail 'jq is required to read config defaults'
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd) || exit 2
cli=$script_dir/grimcli
[[ -x "$cli" && ! -d "$cli" ]] || fail 'Build grimcli beside this script with: go build .'

((BASH_VERSINFO[0] > 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] >= 4))) || fail 'Bash 4.4 or newer is required'
source "$script_dir/gameplay-session.sh" || fail 'Cannot load gameplay-session.sh'

token=''
match_id=''
lobby_id=''
terminal_state=''
pending_key=''
restore_terminal() {
    if [[ -n "$terminal_state" ]]; then
        stty "$terminal_state"
        terminal_state=''
    fi
}
cleanup() { restore_terminal; game_clear; unset token match_id lobby_id input; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Keep certificate expansion and request validation in the CLI. Inspect only
# the config defaults needed to decide which interactive fields are required.
common_args=()
server=''
has_email=false
has_password=false
if [[ -e "$config_path" || -L "$config_path" ]] || $explicit_config; then
    [[ -f "$config_path" && -r "$config_path" ]] || fail 'Cannot read config file'
    # Match the CLI's case-insensitive known fields, null defaults, and ignored
    # unknown fields. Slurping also rejects multiple top-level JSON values.
    metadata=$(jq -ers '
        if length != 1 or (.[0] | type) != "object" then error("invalid config") else .[0] end
        | with_entries(.key |= ascii_downcase)
        | if all(to_entries[];
            if (.key == "server" or .key == "cacert" or .key == "email" or .key == "password")
            then (.value == null or (.value | type) == "string") else true end)
          then . else error("invalid defaults") end
        | [.server // "", ((.email // "") != ""), ((.password // "") != "")]
    ' -- "$config_path" 2>/dev/null) || fail 'Config must contain one JSON object with string defaults'
    server=$(jq -r '.[0]' <<<"$metadata")
    has_email=$(jq -r '.[1]' <<<"$metadata")
    has_password=$(jq -r '.[2]' <<<"$metadata")
    unset metadata
    common_args+=(--config "$config_path")
fi

# Read keys immediately so Escape works before Enter. Keep terminal echo off
# for the whole prompt, including the gaps between password keystrokes.
# Return 1 for cancellation; EOF exits the session as before.
prompt() {
    local key next_key status=0 hidden=${2:-}
    input=''
    if [[ -t 0 ]]; then
        terminal_state=$(stty -g) || exit 2
        # Handle Ctrl-C as a key while reading; this avoids a signal racing
        # Bash's own terminal setup for read -n. External signals still trap.
        stty -echo -icanon -isig || exit 2
    fi
    printf '%s' "$1" >&2
    while true; do
        if [[ -n "$pending_key" ]]; then
            key=$pending_key
            pending_key=''
        elif ! IFS= read -r -n 1 key; then
            status=1
            break
        fi
        case "$key" in
            $'\e')
                # Ignore arrow/function-key sequences instead of interpreting
                # their Escape prefix as cancellation. Preserve any unrelated
                # immediately following key for the next prompt.
                if [[ -t 0 ]] && IFS= read -r -n 1 -t 0.05 next_key; then
                    if [[ "$next_key" == '[' || "$next_key" == O ]]; then
                        while IFS= read -r -n 1 -t 0.05 next_key; do
                            [[ "$next_key" == [@-~] ]] && break
                        done
                        continue
                    fi
                    pending_key=$next_key
                fi
                status=2
                break
                ;;
            ''|$'\r') break ;;
            $'\003') exit 130 ;;
            $'\004') status=1; break ;;
            $'\177'|$'\b')
                if [[ -n "$input" ]]; then
                    input=${input%?}
                    [[ -t 0 && "$hidden" != hidden ]] && printf '\b \b' >&2
                fi
                ;;
            $'\025')
                while [[ -n "$input" ]]; do
                    input=${input%?}
                    [[ -t 0 && "$hidden" != hidden ]] && printf '\b \b' >&2
                done
                ;;
            *)
                input+=$key
                [[ -t 0 && "$hidden" != hidden ]] && printf '%s' "$key" >&2
                ;;
        esac
    done
    restore_terminal
    [[ -t 0 || "$hidden" == hidden ]] && printf '\n' >&2
    case "$status" in
        1) input=''; exit 0 ;;
        2) input=''; printf 'Cancelled.\n' >&2; return 1 ;;
    esac
    return 0
}

while [[ -z "$server" ]]; do
    prompt 'Server origin (e.g. https://localhost:8080): ' || exit 0
    server=$input
done
common_args+=(--server "$server")

login() {
    local email password candidate retry
    local -a login_args
    while true; do
        email=''
        password=''
        while [[ -z "$email" ]]; do
            if $has_email; then
                prompt 'Email (blank uses config default): ' || return 1
            else
                prompt 'Email: ' || return 1
            fi
            email=$input
            $has_email && break
        done
        while [[ -z "$password" ]]; do
            if $has_password; then
                prompt 'Password (blank uses config default): ' hidden || return 1
            else
                prompt 'Password: ' hidden || return 1
            fi
            password=$input
            input=''
            $has_password && break
        done
        login_args=(login "${common_args[@]}")
        [[ -n "$email" ]] && login_args+=(--email "$email")
        [[ -n "$password" ]] && login_args+=(--password "$password")
        if candidate=$("$cli" "${login_args[@]}"); then
            unset password login_args
            if [[ -n "$candidate" ]]; then
                token=$candidate
                game_clear
                response=''
                match_id=''
                lobby_id=''
                printf 'Logged in.\n'
                return
            fi
            printf 'Login returned an empty token.\n' >&2
        fi
        unset password login_args candidate
        prompt 'Login failed. Retry (r), Back (0), or Quit (q): ' || return 1
        retry=$input
        case "$retry" in r|R) ;; q|Q) exit 0 ;; *) return 1 ;; esac
    done
}

# Prompt without changing selections; commit only after successful requests.
require_value() {
    input=''
    while [[ -z "$input" ]]; do
        prompt "$1" || return 1
    done
}

# All pending pick arguments live only in this invocation. Retry reuses the
# whole argv array, including the UUID; refresh replaces it with a GET.
draft_flow() {
    command -v uuidgen >/dev/null 2>&1 || { printf 'uuidgen is required for drafting.\n' >&2; return 1; }
    local draft_id=$match_id response step status revision card_id pick_uuid selection
    local -a draft_args
    if [[ -z "$draft_id" ]]; then
        require_value 'Match ID: ' || return 1
        draft_id=$input
    fi
    draft_args=(draft "${common_args[@]}" --token "$token" --id "$draft_id")
    while true; do
        if ! response=$("$cli" "${draft_args[@]}"); then
            while true; do
                menu_prompt 'Draft request failed. Retry (r), Refresh (f), or Back (0): ' || return 1
                case "$input" in
                    r|R|retry|Retry) break ;;
                    f|F|refresh|Refresh)
                        draft_args=(draft "${common_args[@]}" --token "$token" --id "$draft_id")
                        break
                        ;;
                    0|b|B|back|Back) return ;;
                    *) printf 'Choose Retry (r), Refresh (f), or Back (0).\n' >&2 ;;
                esac
            done
            continue
        fi
        match_id=$draft_id
        step=$(jq -r '.draft.step' <<<"$response")
        status=$(jq -r '.status' <<<"$response")
        revision=$(jq -r '.revision' <<<"$response")
        if [[ "$status" == playing ]]; then
            printf 'Game started.\n'
            return
        fi
        if [[ "$step" == complete ]]; then
            printf 'Draft complete; waiting for other players.\n'
            return
        fi
        local choice_type
        case "$step" in
            chantry) choice_type=choose_chantry ;;
            victory_path) choice_type=choose_victory_path ;;
            minion) choice_type=choose_minion ;;
            *) printf 'Unknown draft step. Refresh by opening drafting again.\n' >&2; return 1 ;;
        esac
        printf '\nDraft step: %s (revision %s)\n' "$step" "$revision"
        jq -r '.cards | to_entries[] |
            "\(.key + 1)) \(.value.name) [design ID: \(.value.id)]",
            (if .value.placeholder then "   WARNING: Placeholder card; printed properties are provisional." else empty end),
            (.value | to_entries[] | select(.key != "id" and .key != "name" and .key != "placeholder") |
                select(.value != null and .value != "" and .value != [] and .value != {}) |
                "   \(.key): \(.value | tojson)")' <<<"$response"
        printf 'Picks are irreversible. 0 or Escape returns to the match without submitting this choice; q quits.\n'
        while true; do
            menu_prompt 'Choose an offered card number (0 Back, q Quit): ' || return 1
            [[ "$input" == 0 ]] && return
            selection=$input
            # Compare decimal labels as strings: reject signs, fractions, leading
            # zeroes, and huge numbers without Bash integer overflow.
            card_id=$(jq -r --arg number "$selection" '.cards | to_entries[] |
                select((.key + 1 | tostring) == $number) | .value.id' <<<"$response")
            [[ -n "$card_id" ]] && break
            printf 'Choose one of the offered card numbers.\n' >&2
        done
        if ! pick_uuid=$(uuidgen) || [[ -z "$pick_uuid" ]]; then
            printf 'Could not generate a draft request UUID.\n' >&2
            return 1
        fi
        draft_args=(draft-pick "${common_args[@]}" --token "$token" --id "$draft_id"
            --type "$choice_type" --cardid "$card_id" --revision "$revision" --requestid "$pick_uuid")
    done
}

# Commands share one response slot. A retry always reuses the exact argv; callers
# perform follow-up GETs separately so a successful mutation is never repeated.
request() {
    local label=$1
    shift
    local -a request_args=("$@" "${common_args[@]}" --token "$token")
    while true; do
        if response=$("$cli" "${request_args[@]}"); then
            return 0
        fi
        while true; do
            menu_prompt "$label failed. Retry (r) or Back (0): " || return 1
            case "$input" in
                r|R) break ;;
                0|b|B) return 1 ;;
                *) printf 'Choose Retry (r) or Back (0).\n' >&2 ;;
            esac
        done
    done
}

menu_prompt() {
    prompt "$1" || return 1
    case "$input" in q|Q) exit 0 ;; esac
}

lobby_details() {
    jq -e '.lobby | .id != null and .status != null and
        (.members | type) == "array" and (.readiness | type) == "object"' <<<"$1" >/dev/null 2>&1
}

match_details() {
    jq -e '.match | .id != null and .status != null and
        (.participants | type) == "array" and (.wizards | type) == "object"' <<<"$1" >/dev/null 2>&1
}

# Compare labels as strings to avoid shell arithmetic overflow and reject
# signs, fractions, and leading zeroes, just as the draft chooser does.
numbered_id() {
    jq -r --arg number "$2" "$1 | to_entries[] |
        select((.key + 1 | tostring) == \$number) | .value.id" <<<"$3"
}

render_lobby() {
    jq -r '.lobby as $l | $l.members as $members |
        ([$members[] | select($l.readiness[.] == true)] | length) as $ready |
        "\nLobby: \($l.name)", "ID: \($l.id)", "Status: \($l.status)",
        "Owner ID: \($l.owner)", "Members (\($members | length)/4):",
        ($members[] | "  \(.) — \(if $l.readiness[.] == true then "ready" else "not ready" end)"),
        "Ready: \($ready)/\($members | length)",
        (if $l.status == "open" then
            if ($members | length) < 2 then "Needs more players (2–4 required)."
            elif ($members | length) > 4 then "Needs 2–4 players to launch."
            elif $ready != ($members | length) then "Waiting for every player to mark ready."
            else "Everyone is ready. The owner can launch the match." end
         elif $l.status == "closed" and (($l.matchid // "") != "") then "This lobby has a match. Open it to continue."
         else "This lobby is not open; inspection only." end)' <<<"$1"
}

render_match() {
    if jq -e '.match.status == "playing" or .match.status == "finished"' <<<"$1" >/dev/null; then
        game_render "$1" "$(jq -r '.match.id' <<<"$1")"
        return
    fi
    jq -r '.match as $m |
        "\nMatch: \($m.name)", "ID: \($m.id)", "Status: \($m.status)",
        "Players: \($m.participants | length)",
        "Drafts complete: \([$m.wizards[] | select(.draftcomplete == true)] | length)/\($m.participants | length)",
        "Your draft: \($m.you.draft.step // "unavailable")",
        (if $m.status == "setup" then
            if $m.you.draft.step == "complete" then "Draft complete; waiting for other players."
            elif $m.you.draft.step != null then "Begin/resume draft to make your next pick."
            else "Your draft progress is unavailable." end
         elif $m.status == "playing" then
            "Game started.",
            (if $m.turn.activewizard != null then "Active wizard: \($m.turn.activewizard)" else empty end),
            (if $m.turn.number != null then "Turn: \($m.turn.number)" else empty end),
            (if $m.turn.phase != null then "Phase: \($m.turn.phase)" else empty end),
            (if $m.turn.step != null then "Step: \($m.turn.step)" else empty end)
         elif $m.status == "finished" then "Outcome: \($m.outcome // "unavailable")"
         else empty end),
        (if $m.you.draft != null then
            ($m.you.draft | to_entries[] | select(.key != "step") | "  \(.key): \(.value)")
         else empty end)' <<<"$1"
}

match_more() {
    local details=$1 id=$2
    while true; do
        printf '\nMatch — More actions\n1) Full JSON details\n2) Catalogue JSON\n0) Back\nq) Quit\n'
        menu_prompt 'Choose an action: ' || return
        case "$input" in
            1) printf '%s\n' "$details" ;;
            2) if request 'Catalogue request' catalogue --id "$id"; then printf '%s\n' "$response"; fi ;;
            0) return ;;
            *) printf 'Choose one of the listed actions.\n' >&2 ;;
        esac
    done
}

match_screen() {
    local id=$1 details=${2:-} can_draft gameplay_lane
    if ! match_details "$details"; then
        game_fetch "$id" || return
        details=$game_details
    fi
    match_id=$id
    game_details=$details
    if jq -e '.match.you.userid != null and .match.status != "setup"' <<<"$details" >/dev/null; then
        game_catalogue "$id" || true
    fi
    while true; do
        render_match "$details"
        can_draft=$(jq -r '.match | .status == "setup" and
            (.you.draft.step == "chantry" or .you.draft.step == "victory_path" or .you.draft.step == "minion")' <<<"$details")
        [[ "$can_draft" == true ]] && printf '1) Begin/resume draft\n'
        gameplay_lane=$(game_lane "$details")
        if [[ -n ${game_pending[$id]:-} ]]; then
            printf '1) Resume pending input / fetch\n'
        else
            case "$gameplay_lane" in
                actions) printf '1) Gameplay actions\n' ;;
                priority) printf '1) Respond to Priority\n' ;;
                choice) printf '1) Resolve required choice\n' ;;
            esac
        fi
        printf '2) Refresh (Enter)\n3) More actions\n0) Back\nq) Quit\n'
        menu_prompt 'Choose an action: ' || return
        case "$input" in
            1)
                if [[ -n ${game_pending[$id]:-} || "$gameplay_lane" == actions || "$gameplay_lane" == priority || "$gameplay_lane" == choice ]]; then
                    game_details=''
                    game_action "$id" || true
                    [[ -n "$game_details" ]] && details=$game_details
                    continue
                fi
                if [[ "$can_draft" != true ]]; then
                    printf 'Choose one of the listed actions.\n' >&2
                    continue
                fi
                draft_flow
                # Completion, cancellation, and failure all return here. Fetch
                # authoritative progress but wait for another explicit draft action.
                game_fetch "$id" || return
                details=$game_details
                ;;
            ''|2)
                game_fetch "$id" || return
                details=$game_details
                ;;
            3) match_more "$details" "$id" ;;
            0) return ;;
            *) printf 'Choose one of the listed actions.\n' >&2 ;;
        esac
    done
}

# Removal chooses a roster entry rather than accepting a free-form user ID.
choose_member() {
    local details=$1 selected
    while true; do
        printf '\nRemove member (owner only)\n'
        jq -r '.lobby.members | to_entries[] | "\(.key + 1)) \(.value)"' <<<"$details"
        printf '0) Back\nq) Quit\n'
        menu_prompt 'Choose a member number: ' || return 1
        [[ "$input" == 0 ]] && return 1
        selected=$(jq -r --arg number "$input" '.lobby.members | to_entries[] |
            select((.key + 1 | tostring) == $number) | .value' <<<"$details")
        if [[ -n "$selected" ]]; then
            input=$selected
            return 0
        fi
        printf 'Choose one of the listed member numbers.\n' >&2
    done
}

lobby_more() {
    # Return new details through response, or 2 when leaving/deleting sent us Home.
    local id=$1 details=$2
    while true; do
        printf '\nLobby — More actions\n1) Leave\n2) Remove member (owner only)\n3) Delete lobby (owner only)\n4) Full JSON details\n0) Back\nq) Quit\n'
        menu_prompt 'Choose an action: ' || { response=$details; return 0; }
        case "$input" in
            1|3)
                local command_name=lobby-leave
                [[ "$input" == 3 ]] && command_name=lobby-delete
                if request 'Lobby mutation' "$command_name" --id "$id"; then
                    lobby_id=''
                    printf 'Lobby action succeeded: %s.\n' "${command_name#lobby-}"
                    return 2
                fi
                ;;
            2)
                choose_member "$details" || continue
                if request 'Remove member' lobby-remove-user --id "$id" --userid "$input"; then
                    printf 'Member removed.\n'
                    request 'Lobby fetch' lobby --id "$id" || return 1
                    details=$response
                fi
                ;;
            4) printf '%s\n' "$details" ;;
            0) response=$details; return 0 ;;
            *) printf 'Choose one of the listed actions.\n' >&2 ;;
        esac
    done
}

lobby_screen() {
    local id=$1 details=${2:-} status can_launch linked_match more_status
    if ! lobby_details "$details"; then
        request 'Lobby fetch' lobby --id "$id" || return
        details=$response
    fi
    lobby_id=$id
    while true; do
        render_lobby "$details"
        status=$(jq -r '.lobby.status' <<<"$details")
        linked_match=$(jq -r '.lobby.matchid // empty' <<<"$details")
        can_launch=false
        if [[ "$status" == open ]]; then
            can_launch=$(jq -r '.lobby | . as $l | (.members | length) as $n |
                $n >= 2 and $n <= 4 and all(.members[]; $l.readiness[.] == true)' <<<"$details")
            printf '1) Mark me ready\n2) Mark me unready\n3) Refresh (Enter)\n4) More actions\n'
            [[ "$can_launch" == true ]] && printf '5) Launch match (owner only)\n'
        else
            [[ "$status" == closed && -n "$linked_match" ]] && printf '1) Open match\n'
            printf '2) Refresh (Enter)\n3) Full JSON details\n'
        fi
        printf '0) Back\nq) Quit\n'
        menu_prompt 'Choose an action: ' || return
        if [[ "$status" != open ]]; then
            case "$input" in
                1)
                    if [[ "$status" == closed && -n "$linked_match" ]]; then
                        match_screen "$linked_match"
                        return
                    fi
                    printf 'Choose one of the listed actions.\n' >&2
                    ;;
                ''|2) request 'Lobby fetch' lobby --id "$id" || return; details=$response ;;
                3) printf '%s\n' "$details" ;;
                0) return ;;
                *) printf 'Choose one of the listed actions.\n' >&2 ;;
            esac
            continue
        fi
        case "$input" in
            1|2)
                local ready=true
                [[ "$input" == 2 ]] && ready=false
                if request 'Readiness update' lobby-ready --id "$id" --ready="$ready"; then
                    printf 'Readiness updated.\n'
                    if ! lobby_details "$response"; then
                        request 'Lobby fetch' lobby --id "$id" || return
                    fi
                    details=$response
                fi
                ;;
            ''|3) request 'Lobby fetch' lobby --id "$id" || return; details=$response ;;
            4)
                more_status=0
                lobby_more "$id" "$details" || more_status=$?
                [[ "$more_status" == 2 ]] && return 2
                [[ "$more_status" != 0 ]] && return
                details=$response
                ;;
            5)
                if [[ "$can_launch" != true ]]; then
                    printf 'Choose one of the listed actions.\n' >&2
                    continue
                fi
                # The CLI derives the launch request ID from the lobby ID.
                if request 'Launch' lobby-launch --id "$id"; then
                    printf 'Match launched.\n'
                    local launched_id
                    launched_id=$(jq -r '.match.id // empty' <<<"$response")
                    if [[ -n "$launched_id" ]]; then
                        match_id=$launched_id
                        match_screen "$launched_id" "$response"
                        return
                    fi
                    printf 'Launch response is missing the match ID; refresh the lobby.\n' >&2
                    request 'Lobby fetch' lobby --id "$id" || return
                    details=$response
                fi
                ;;
            0) return ;;
            *) printf 'Choose one of the listed actions.\n' >&2 ;;
        esac
    done
}

open_selection() {
    local kind=$1 id=$2
    if [[ "$kind" == public-lobbies ]]; then
        if request 'Join' lobby-join --id "$id"; then
            local joined_id
            joined_id=$(jq -r '.lobby.id // empty' <<<"$response")
            lobby_id=${joined_id:-$id}
            printf 'Joined lobby.\n'
            lobby_screen "$lobby_id" "$response"
        fi
    elif [[ "$kind" == lobbies ]]; then
        lobby_screen "$id"
    else
        match_screen "$id"
    fi
}

selection_screen() {
    local kind=$1 listing list_filter title selected remembered screen_status
    case "$kind" in
        public-lobbies) title='Join a lobby'; list_filter='.lobbies' ;;
        lobbies) title='My lobbies'; list_filter='.lobbies' ;;
        matches) title='My matches'; list_filter='.matches' ;;
    esac
    request 'List fetch' "$kind" || return
    listing=$response
    while true; do
        printf '\n%s\n' "$title"
        if [[ "$kind" == public-lobbies ]]; then
            jq -r '.lobbies | to_entries[] |
                "\(.key + 1)) \(.value.name) — \(.value.membercount)/4 players [ID: \(.value.id)]"' <<<"$listing"
        else
            jq -r "$list_filter | to_entries[] |
                \"\(.key + 1)) ID: \(.value.id) — \(.value.status)\"" <<<"$listing"
        fi
        if [[ $(jq "$list_filter | length" <<<"$listing") == 0 ]]; then
            case "$kind" in
                public-lobbies) printf 'No public lobbies. Refresh, enter an ID, or go Back to create a lobby.\n' ;;
                lobbies) printf 'No lobby memberships. Go Back to create or join a lobby, or enter an ID.\n' ;;
                matches) printf 'No matches. Go Back to create or join a lobby and launch, or enter an ID.\n' ;;
            esac
        fi
        remembered=''
        [[ "$kind" == lobbies ]] && remembered=$lobby_id
        [[ "$kind" == matches ]] && remembered=$match_id
        [[ -n "$remembered" ]] && printf 's) Reopen remembered selection [ID: %s]\n' "$remembered"
        printf 'r) Refresh (Enter)\nm) Enter ID manually\n0) Back\nq) Quit\n'
        menu_prompt 'Choose a number or action: ' || return
        case "$input" in
            0) return ;;
            ''|r|R) request 'List fetch' "$kind" || return; listing=$response; continue ;;
            m|M) require_value 'Enter ID: ' || continue; selected=$input ;;
            s|S)
                if [[ -z "$remembered" ]]; then printf 'No remembered selection.\n' >&2; continue; fi
                selected=$remembered
                ;;
            *)
                selected=$(numbered_id "$list_filter" "$input" "$listing")
                if [[ -z "$selected" ]]; then printf 'Choose one of the listed numbers or actions.\n' >&2; continue; fi
                ;;
        esac
        screen_status=0
        open_selection "$kind" "$selected" || screen_status=$?
        [[ "$screen_status" == 2 ]] && return
    done
}

create_lobby() {
    require_value 'Lobby name: ' || return
    request 'Create lobby' lobby-create --name "$input" || return
    local created_id
    created_id=$(jq -r '.id // empty' <<<"$response")
    if [[ -z "$created_id" ]]; then
        printf 'Create response is missing the lobby ID.\n' >&2
        return
    fi
    lobby_id=$created_id
    printf 'Lobby created.\n'
    lobby_screen "$created_id"
}

account_screen() {
    while true; do
        printf '\nAccount\n1) Log in again\n0) Back\nq) Quit\n'
        menu_prompt 'Choose an action: ' || return
        case "$input" in
            1) login ;;
            0) return ;;
            *) printf 'Choose one of the listed actions.\n' >&2 ;;
        esac
    done
}

login || exit 0
while true; do
    printf '\nHome\n1) Create a lobby\n2) Join a lobby\n3) My lobbies\n4) My matches\n5) Account\nq) Quit\n'
    menu_prompt 'Choose an action: ' || continue
    case "$input" in
        1) create_lobby ;;
        2) selection_screen public-lobbies ;;
        3) selection_screen lobbies ;;
        4) selection_screen matches ;;
        5) account_screen ;;
        *) printf 'Choose one of the listed actions.\n' >&2 ;;
    esac
done
