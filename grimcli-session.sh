#!/usr/bin/env bash
# Secrets must not enter shell tracing, even when launched with bash -x.
set +x
set -u

usage() {
    cat <<'EOF'
Usage: grimcli-session.sh [--config PATH]

Interactively log in and inspect matches, catalogues, and lobby memberships.
Requires Bash, jq, and a compiled grimcli beside this script.
Defaults come from grimcli.json in the current working directory.
Tokens and selected match IDs are kept in memory until exit.
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
terminal_state=''
pending_key=''
restore_terminal() {
    if [[ -n "$terminal_state" ]]; then
        stty "$terminal_state"
        terminal_state=''
    fi
}
cleanup() { restore_terminal; unset token match_id input; }
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

login || exit 0
while true; do
    printf '\n1) List matches\n2) List lobbies\n3) View match\n4) View catalogue\n5) Change selected match ID\n6) Log in again\n0) Quit\n'
    [[ -n "$match_id" ]] && printf 'Selected match: %s\n' "$match_id"
    prompt 'Choose an action: ' || continue
    case "$input" in
        1) command_name=matches ;;
        2) command_name=lobbies ;;
        3) command_name=match ;;
        4) command_name=catalogue ;;
        5) select_match; continue ;;
        6) login; continue ;;
        0|q|Q) exit 0 ;;
        *) printf 'Choose one of the listed actions.\n' >&2; continue ;;
    esac
    if [[ "$command_name" == match || "$command_name" == catalogue ]]; then
        if [[ -z "$match_id" ]]; then
            select_match || continue
        fi
    fi
    args=("$command_name" "${common_args[@]}" --token "$token")
    if [[ "$command_name" == match || "$command_name" == catalogue ]]; then
        args+=(--id "$match_id")
    fi
    if ! "$cli" "${args[@]}"; then
        printf 'Command failed. You can retry, change the match ID, or log in again.\n' >&2
    fi
    unset args
done
