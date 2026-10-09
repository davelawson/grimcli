# grimcli

A small, stateless Go CLI for the Grimoire HTTP API. Each protected command
requires an explicit token, allowing commands for multiple users from the same
installation. No credentials or sessions are stored, and environment variables
are not used for configuration.

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

The initial version supports login and inspection only. It has no game logic,
public SDK, registration, lobby management, launch, drafting, or admin commands.
