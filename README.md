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

Login prints only the token and a newline. Match, catalogue, and draft commands print
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
through launch and drafting. Registration, ownership transfer, and admin commands
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

## Drafting

Both commands require an explicit token and match ID and accept the usual
server, certificate, and config flags. They use `GET /match/:id/draft` and
`POST /match/:id/draft` and print an indented `api.DraftResponse` containing
only your draft progress, offered cards, match revision, and status.

```sh
# Fetch your current step, options, and revision (also how to resume).
./grimcli draft --token '<user-token>' --id '<match-id>'

# Use an offered design ID and the revision returned by the server.
request_id=$(uuidgen)
./grimcli draft-pick --token '<user-token>' --id '<match-id>' \
  --type choose_chantry --cardid chantry_fire --revision 0 \
  --requestid "$request_id"

# If the response was lost, repeat the exact command with the same request ID.
./grimcli draft-pick --token '<user-token>' --id '<match-id>' \
  --type choose_chantry --cardid chantry_fire --revision 0 \
  --requestid "$request_id"
```

All pick flags are required, including `--revision 0` for a new match.
Revisions must be nonnegative integers, IDs must be nonblank, and request IDs
must be UUIDs in hyphenated `8-4-4-4-12` form (hexadecimal, either case).
The step determines the type: `chantry` → `choose_chantry`, `victory_path` →
`choose_victory_path`, and `minion` → `choose_minion`. Picks are irreversible.
The CLI submits once and reports backend errors on stderr.

Keep the request ID **and all pick arguments** unchanged when retrying a
request. The backend returns its recorded response if it already accepted
that request; this may show an earlier revision. On `stale_revision`, fetch
`draft` again before selecting a card using the current revision and a new
request ID. Other players' picks can advance the match revision.

A completed player's step is `complete` with an empty cards array. Match
status `setup` means waiting for other players; `playing` means the backend
has automatically started the game. Other players' private options and
choices stay on the server.

## Interactive sessions

Build the CLI with `go build .`, install `jq` (and `uuidgen` for drafting), and run the Bash wrapper:

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

Home offers **Create a lobby**, **Join a lobby**, **My lobbies**, **My matches**,
**Account**, and **Quit**. Create prompts for a name and opens the new lobby.
Join shows numbered public lobbies with names, occupancy, and IDs; choosing one
joins it and opens its screen. My lobbies and My matches show numbered memberships
with IDs and statuses. Each list offers `r` or Enter with no input to **Refresh**,
`m` to **Enter ID manually**, and `0` to go **Back**. Membership lists also offer
`s` to reopen the last successful selection. Empty lists explain how to continue.

Lobby screens show the name, ID, status, owner ID, member IDs, individual readiness,
and ready count. Open lobbies offer **Mark me ready**, **Mark me unready**, **Refresh**,
and **More actions**. With two to four members and everyone ready, **Launch match
(owner only)** becomes available. The API does not identify your roster entry, so
both readiness actions remain visible and the server authorizes owner actions.
More actions offers **Leave**, **Remove member (owner only)** using a numbered
roster, **Delete lobby (owner only)**, and **Full JSON details**. Successful leave
or deletion returns Home. Closed lobbies with a match ID offer **Open match**;
other non-open lobbies offer inspection and navigation.

Launch opens the match screen. During setup, choose **Begin/resume draft** to
start making picks. Match screens show the name, ID, status, player count, public
draft-completion count, and your draft progress. Completed drafts display “Draft
complete; waiting for other players.” Playing matches show “Game started” and
available turn details; finished matches show the outcome. Every match screen
offers **Refresh** and **More actions**, containing **Full JSON details** and
**Catalogue JSON**. On any lobby or match screen, press Enter with no input
to fetch the latest server data and redraw, or choose **Refresh** explicitly.
Updates are manual; refresh after another player acts. Whitespace-only input
keeps its existing behavior. Login, drafting, text-entry, and nested-menu
prompts are unchanged.

Lobby and match selections are independent and kept in memory. Successful creation,
joining, or detail selection remembers the relevant ID; launch remembers the match.
Self-leave or deletion clears the lobby selection. Successful login clears both
selections. **Account → Log in again** supports changing users; cancelling or failing
login preserves the current session. Failed requests keep selections and offer
**Retry** or **Back**. After a successful mutation, a failed follow-up fetch retries
only the fetch. Launch retries retain the same lobby-derived request ID.

Drafting requires `uuidgen` on your PATH (commonly provided by `uuid-runtime` on
Debian/Ubuntu). Each entry fetches current draft progress. Numbered cards show
names, design IDs, printed properties, and placeholder warnings. Enter an offered
number to submit an irreversible pick. Each new pick gets one UUID and the
returned step's revision. After a failed draft request, `r` **Retry** resends the
exact pending request; `f` **Refresh** fetches authoritative progress before
offering another choice, useful after a stale revision. `0` **Back** (or `b`) and
Escape leave drafting. Pending requests exist only in memory during the flow and
are discarded when you leave it. Completion or cancellation returns to the match
screen and fetches its current state. Drafting resumes only when you choose it again.

Numbers are local to each screen. Use `0` for **Back** and `q` for **Quit** at menus
and card choices. Escape cancels the current prompt or leaves the current screen;
at Home it redisplays Home. Cancelling names, IDs, roster choices, or login
credentials preserves the session and sends no mutation. Escape during initial
setup exits. Backspace edits input; Ctrl-U clears the current input.

The wrapper keeps its token and selected lobby and match IDs in memory only, displays no
token, and writes no credentials or session files. Entered passwords are
discarded after each login attempt. Quit with `q`, EOF (Ctrl-D), or
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

Run `bash -n grimcli-session.sh`, `go test -race ./...`, and `go vet ./...` to check
changes. Wrapper tests require Bash and `jq` and use a temporary stub CLI;
draft tests supply a stub `uuidgen`. They need no running backend.
