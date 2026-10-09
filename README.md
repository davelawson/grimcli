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

The initial version supports login, inspection, and membership listing only. It has no game logic,
public SDK, registration, lobby management, launch, drafting, or admin commands.

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
login. `--token` must still be supplied explicitly. Single-match commands also require
`--id`; listing commands do not accept it.

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
