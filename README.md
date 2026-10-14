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
judo help                      # list resources
judo apps                      # list the commands for apps
judo apps list
judo processes list 123
judo metrics get-series 456 queue_time --from 2026-01-01T00:00:00Z --to 2026-01-02T00:00:00Z
judo scale-events list 456 --all   # follow next_cursor, merge all pages
judo processes update 456 --max_scale 10 --autoscaling_enabled
judo processes update 456 --queue_names '["default","mailers"]'
judo api-tokens create --data '{"label":"ci","read_only":true}'
judo api GET /v1/team          # raw request
judo update                    # refresh the API spec
```

Commands are built from the Judoscale API's OpenAPI spec. They are grouped by resource: run `judo help` to list the resources, `judo <resource>` to list its commands, and `judo <resource> <command> --help` for details.

Request body fields are flags. Boolean flags are set with `--name` or `--name=false`, and array or object fields take a JSON value. Pass `-d`/`--data` to send a whole JSON body instead (`--data -` reads it from stdin).

Destructive commands, like `judo apps delete 123`, refuse to run without `--yes`. `judo api` never asks.

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
script/test   # go vet + go test
script/build
```

The embedded snapshot lives in `internal/spec/openapi.json`. Refresh it with `script/update-spec [base-url]` (production by default; requires `jq`), which downloads the spec through `judo update`, drops examples and points `servers` at production. Run `script/build` afterwards to embed it.
