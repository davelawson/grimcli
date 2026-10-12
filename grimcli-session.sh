#!/usr/bin/env bash
# Secrets must not enter shell tracing, even when launched with bash -x.
set +x
set -u

usage() {
    cat <<'EOF'
Usage: grimcli-session.sh [--config PATH]

Interactively log in, manage lobbies, launch matches, and inspect catalogues.
Requires Bash, jq, and a compiled grimcli beside this script.
Defaults come from grimcli.json in the current working directory.
Tokens and selected lobby and match IDs are kept in memory until exit.
Escape cancels input and returns to the menu (or exits during initial setup).
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
cleanup() { restore_terminal; unset token match_id lobby_id input; }
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
                match_id=''
                lobby_id=''
                printf 'Logged in.\n'
                return
            fi
            printf 'Login returned an empty token.\n' >&2
        fi
        unset password login_args candidate
        prompt 'Login failed. Enter r to retry or q to quit: ' || return 1
        retry=$input
        case "$retry" in r|R) ;; *) exit 0 ;; esac
    done
}

select_match() {
    while true; do
        if [[ -n "$match_id" ]]; then
            prompt "Match ID (blank keeps $match_id): " || return 1
            [[ -n "$input" ]] && match_id=$input
            return 0
        fi
        prompt 'Match ID: ' || return 1
        match_id=$input
        [[ -n "$match_id" ]] && return 0
    done
}

# Prompt into input without changing the current selection. Callers commit only
# after a successful command, or when explicitly selecting a lobby.
require_value() {
    input=''
    while [[ -z "$input" ]]; do
        prompt "$1" || return 1
    done
}

select_lobby() {
    if [[ -n "$lobby_id" ]]; then
        prompt "Lobby ID (blank keeps $lobby_id): " || return 1
        [[ -n "$input" ]] && lobby_id=$input
    else
        require_value 'Lobby ID: ' || return 1
        lobby_id=$input
    fi
    return 0
}

login || exit 0
while true; do
    printf '\n1) List matches\n2) List lobbies\n3) View match\n4) View catalogue\n5) Change selected match ID\n6) Log in again\n7) Browse public lobbies\n8) View lobby\n9) Create lobby\n10) Join lobby\n11) Ready\n12) Unready\n13) Launch lobby\n14) Leave lobby\n15) Remove user from lobby\n16) Delete lobby\n17) Change selected lobby ID\n0) Quit\n'
    [[ -n "$match_id" ]] && printf 'Selected match: %s\n' "$match_id"
    [[ -n "$lobby_id" ]] && printf 'Selected lobby: %s\n' "$lobby_id"
    prompt 'Choose an action: ' || continue
    ready_value=''
    case "$input" in
        1) command_name=matches ;;
        2) command_name=lobbies ;;
        3) command_name=match ;;
        4) command_name=catalogue ;;
        5) select_match; continue ;;
        6) login; continue ;;
        7) command_name=public-lobbies ;;
        8) command_name=lobby ;;
        9) command_name=lobby-create ;;
        10) command_name=lobby-join ;;
        11) command_name=lobby-ready; ready_value=true ;;
        12) command_name=lobby-ready; ready_value=false ;;
        13) command_name=lobby-launch ;;
        14) command_name=lobby-leave ;;
        15) command_name=lobby-remove-user ;;
        16) command_name=lobby-delete ;;
        17) select_lobby; continue ;;
        0|q|Q) exit 0 ;;
        *) printf 'Choose one of the listed actions.\n' >&2; continue ;;
    esac
    args=("$command_name" "${common_args[@]}" --token "$token")
    request_id=''
    case "$command_name" in
        match|catalogue)
            request_id=$match_id
            if [[ -z "$request_id" ]]; then
                require_value 'Match ID: ' || continue
                request_id=$input
            fi
            args+=(--id "$request_id")
            ;;
        lobby-create)
            require_value 'Lobby name: ' || continue
            args+=(--name "$input")
            ;;
        lobby|lobby-*)
            request_id=$lobby_id
            if [[ -z "$request_id" ]]; then
                require_value 'Lobby ID: ' || continue
                request_id=$input
            fi
            args+=(--id "$request_id")
            ;;
    esac
    if [[ "$command_name" == lobby-remove-user ]]; then
        require_value 'Target user ID: ' || continue
        args+=(--userid "$input")
    fi
    [[ -n "$ready_value" ]] && args+=(--ready="$ready_value")
    if response=$("$cli" "${args[@]}"); then
        [[ -n "$response" ]] && printf '%s\n' "$response"
        case "$command_name" in
            lobby-create|lobby-join|lobby-launch)
                case "$command_name" in
                    lobby-create) id_filter='.id' ;;
                    lobby-join) id_filter='.lobby.id' ;;
                    lobby-launch) id_filter='.match.id' ;;
                esac
                if selected_id=$(jq -er "$id_filter | select(type == \"string\" and length > 0)" <<<"$response"); then
                    if [[ "$command_name" == lobby-launch ]]; then
                        match_id=$selected_id
                        lobby_id=$request_id
                    else
                        lobby_id=$selected_id
                    fi
                else
                    printf 'Response is missing the selected ID.\n' >&2
                fi
                ;;
            lobby-leave|lobby-delete) lobby_id='' ;;
            lobby|lobby-ready|lobby-remove-user) lobby_id=$request_id ;;
            match|catalogue) match_id=$request_id ;;
        esac
    else
        printf 'Command failed. You can retry, change the selected ID, or log in again.\n' >&2
    fi
    unset args response selected_id
done
