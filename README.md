# Judoscale CLI

Official command line client for [Judoscale](https://judoscale.com). Built for humans, CI and AI agents: JSON output by default, a single static binary.

## Install

_Coming soon: Homebrew tap and a `curl` install script._

From source (Go required):

```sh
go build -o judo ./cmd/judo
```

## Usage

Create an API token in your Judoscale team settings and export it:

```sh
export JUDOSCALE_API_TOKEN=jdo_...
```

```sh
judo help                      # list commands
judo list-apps
judo list-app-processes 123
judo get-process-metric-series 456 queue_time --from 2026-01-01T00:00:00Z --to 2026-01-02T00:00:00Z
judo list-process-scale-events 456 --all   # follow next_cursor, merge all pages
judo api GET /v1/team          # raw request
judo update                    # refresh the API spec
```

Commands are built from the Judoscale API's OpenAPI spec. Run `judo help` to list them and `judo <command> --help` for details.

Only read-only (`GET`) operations are exposed as commands so far; `judo api` can send any method.

### Environment

| Variable              | Description                                        |
| --------------------- | -------------------------------------------------- |
| `JUDOSCALE_API_TOKEN` | API token (required for API commands)              |
| `JUDOSCALE_API_URL`   | API base URL (default `https://api.judoscale.com`) |

### API spec

The binary embeds a snapshot of the spec. `judo update` downloads `<base url>/openapi.json` to `~/.config/judoscale/openapi.json` (or `$XDG_CONFIG_HOME/judoscale/`, `%AppData%\judoscale\` on Windows), and that copy overrides the snapshot. The CLI never updates it on its own. If the latest spec needs a newer CLI, `judo update` asks you to upgrade instead.

### Exit codes

`0` success, `1` API or runtime error (details on stderr), `2` usage error.

## Development

```sh
go vet ./...
go test ./...
go build -o judo ./cmd/judo
```

The embedded snapshot lives in `internal/spec/openapi.json`. Refresh it with `script/update-spec [base-url]` (production by default; requires `jq`), which downloads the spec through `judo update`, drops examples and points `servers` at production.
