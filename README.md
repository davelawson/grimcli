# grimcli

A small, stateless Go CLI for the Grimoire HTTP API. Each protected command
requires an explicit token, allowing commands for multiple users from the same
installation. Tokens and sessions are never stored. Optional JSON configuration
provides argument defaults, including login credentials.

## Setup

Requires Go 1.22.2 or later. For local development, clone this repository beside
`grim`; `go.mod` currently replaces `github.com/davelawson/grim` with `../grim`.
The backend checkout must include the importable module-path change.

```sh
go build .
go test ./...
go vet ./...
```

Start the backend separately using its README instructions. This CLI imports
only `github.com/davelawson/grim/api` and does not access the backend database.

Once the backend's `v0.1.0` release is published, switch to it:

```sh
go mod edit -dropreplace=github.com/davelawson/grim
go get github.com/davelawson/grim@v0.1.0
go mod tidy
```

## Commands

```sh
./grimcli login --server https://localhost:8080 \
  --email alice@example.com --password '...'

./grimcli match --server https://localhost:8080 \
  --token '<alice-token>' --id '<match-id>'

./grimcli match --server https://localhost:8080 \
  --token '<bob-token>' --id '<match-id>'

./grimcli catalogue --server https://localhost:8080 \
  --token '<alice-token>' --id '<match-id>'
```

Login prints only the token and a newline. Match and catalogue commands print
indented JSON response envelopes. Tokens are sent directly in the Authorization
header, following the current backend contract. The server flag takes an HTTP
or HTTPS origin without a path, query, or embedded credentials.

For a local HTTPS server with a self-signed certificate, add
`--cacert /path/to/grim.crt` to each command. Certificates still need to be valid
for the server hostname. System trust roots remain available.

Requests have a 15-second timeout, with no automatic retries or redirects.
Diagnostics go to stderr. Exit codes are 0 for success/help, 1 for request or
certificate failures, and 2 for invalid arguments. Use `--help` or
`<command> --help` for usage.

The CLI supports login, inspection, membership listing, and lobby management
through launch. Registration, ownership transfer, drafting, and admin commands
are outside its current scope.

## Config-file defaults

Each command automatically reads `grimcli.json` from the current working
directory when present. Use `--config /path/to/defaults.json` after the command
to select another file. For example:

```json
{
  "server": "https://localhost:8080",
  "cacert": "$GRIM_SSL/grim.crt",
  "email": "bob@aol.com",
  "password": "password123"
}
```

Explicit flags override individual config values, including empty values:
`--cacert=""` disables the configured certificate file. The `server` and
`cacert` defaults apply to all commands; `email` and `password` apply only to
login. `--token` must still be supplied explicitly. Commands that address a match or lobby require `--id`; listing commands do
not accept it. Lobby names, target IDs, and readiness are supplied explicitly.

Only the configured `cacert` value expands environment references (`$VAR` or
`${VAR}`). A referenced variable must be set when that default is used.
Explicit command-line paths use normal shell expansion. Relative certificate
paths are relative to the current working directory.

```sh
./grimcli login
./grimcli login --email alice@example.com --password '...'
./grimcli match --token '<bob-token>' --id '<match-id>'
./grimcli login --config ./another-config.json
```

The automatic config file may be absent. An explicitly selected missing file,
an unreadable file, or invalid JSON produces exit code 2 without an API call.
Help does not load config or display configured credentials. The local
`grimcli.json` is ignored by Git and created with owner-only read/write access
(mode `0600`).

## List your memberships

```sh
./grimcli matches --token '<user-token>'
./grimcli lobbies --token '<user-token>'
```

These commands use the same server, certificate, and config flags as other
commands. They call authenticated `GET /matches` and `GET /lobbies` for the
supplied token's user and print indented JSON envelopes. Entries contain only
`id` and `status`; fetch individual items for details. All nondeleted states
are included, ordered by ID, without pagination. Empty lists are `[]`.
Neither command accepts `--id` or another user's ID.

## Lobby flow

Every command below requires `--token '<user-token>'` and uses the existing
server, certificate, and config defaults.

| Command | Required arguments | Result |
| --- | --- | --- |
| `public-lobbies` | None | Available public lobby summaries |
| `lobby` | `--id '<lobby-id>'` | Member-only lobby details |
| `lobby-create` | `--name 'Friday game'` | Public lobby ID; creator becomes owner/member |
| `lobby-join` | `--id '<lobby-id>'` | Join as the token's user; returns lobby |
| `lobby-ready` | `--id '<lobby-id>' --ready=true` or `--ready=false` | Set own readiness; returns lobby |
| `lobby-launch` | `--id '<lobby-id>'` | Owner launches; returns match with status 201 |
| `lobby-leave` | `--id '<lobby-id>'` | Self-departure, no response body |
| `lobby-remove-user` | `--id '<lobby-id>' --userid '<user-id>'` | Explicit-target removal, no response body |
| `lobby-delete` | `--id '<lobby-id>'` | Delete owned open lobby, no response body |

Responses print as indented JSON envelopes. Leave, removal, and deletion print
nothing on success. `--ready` requires an explicit `true` or `false` value.
Launch always sends the lobby ID as `requestid`; retry the same command after a
lost response without generating or storing another UUID.

Lobbies hold at most four members, and each user may belong to one open lobby.
Roster changes reset everyone's readiness. Launch requires the owner and two
to four ready members. Owners must transfer ownership through the backend API
or delete their open lobby before leaving. Closed lobbies reject changes.

## Interactive sessions

Build the CLI with `go build .`, install `jq`, and run the Bash wrapper:

```sh
./grimcli-session.sh
# Or select another defaults file:
./grimcli-session.sh --config /path/to/defaults.json
```

The compiled `grimcli` must be beside the script. Config defaults still come
from `grimcli.json` in your current working directory. The wrapper prompts for
an email and a hidden password; leave either field blank to use that field's
configured default. Fields without defaults require input. If no server is
configured, the wrapper prompts for its origin. Certificate defaults and their
environment expansion use the CLI's existing config handling.

Menu actions 1–6 retain their original meanings. Actions 7–17 browse public
lobbies, view/create/join a lobby, ready/unready, launch, leave, remove a user,
delete, and change the selected lobby ID. Names, missing lobby IDs, and removal
targets are prompted as needed. Use action 17 to choose a different lobby.

Lobby and match selections are independent. Successful creation or joining
selects the returned lobby; launch selects the returned match. Self-leave or
deletion clears the lobby selection. Successful login clears both selections.
JSON responses print to the terminal while `jq` extracts returned IDs. Failed
calls preserve selections and return to the menu, allowing launch retries with
the same lobby ID. Failed logins offer retry or quit.

Press Escape while entering a menu choice, lobby name, lobby/match ID, removal target, or login credentials to
cancel the action and return to the menu. Cancellation preserves your current
login and selections and sends no request. Escape during initial setup
exits the wrapper. Backspace edits input; Ctrl-U clears the current input.

The wrapper keeps its token and selected lobby and match IDs in memory only, displays no
token, and writes no credentials or session files. Entered passwords are
discarded after each login attempt. Quit with menu option `0`, EOF (Ctrl-D), or
Ctrl-C. Each process has independent state, so simulate multiple players by
running the wrapper in two terminals and entering different login credentials:

```sh
# Terminal 1: log in as Alice.
./grimcli-session.sh

# Terminal 2: log in as Bob.
./grimcli-session.sh
```

The wrapper uses the CLI's existing credential flags; their values may be
visible in process arguments while a CLI call runs. Lobby actions use the same per-command token and request conventions as
inspection commands.

Run `bash -n grimcli-session.sh`, `go test ./...`, and `go vet ./...` to check
changes. Wrapper tests require Bash and `jq` and use a temporary stub CLI;
they need no running backend.
