# Judoscale CLI

Official command line client for [Judoscale](https://judoscale.com). Built for humans, CI and AI agents: JSON output by default, a single static binary.

## Install

_Coming soon: Homebrew tap and a `curl` install script._

From source (Go required):

```sh
go build -o judo ./cmd/judo
```

## Usage

Create an API token in your Judoscale team settings and log in with it:

```sh
judo auth login
```

The token is checked against the API and saved to `~/.config/judoscale/credentials`, readable only by you. In CI and scripts, set `JUDOSCALE_API_TOKEN` instead (or pipe the token in with `judo auth login --with-token`). The environment variable takes precedence over the saved token.

`judo auth status` shows where the token comes from and which team it belongs to, and `judo auth logout` removes the saved one.

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

Add `--dry-run` to preview any command, including `judo api`. It prints the method, URL, headers (with the token masked) and the JSON body.

When the API rate-limits a request, the CLI waits as long as the API asks and retries, up to three times.

### MCP server

`judo mcp` runs a [Model Context Protocol](https://modelcontextprotocol.io) server on stdin and stdout, so AI agents can call the API. Each operation is a tool named after its operation ID in snake case (`list_apps`, `update_process`), with parameters typed from the spec. It uses the same token as the other commands.

```sh
claude mcp add judoscale -- judo mcp
```

The server is read-only by default. Pass `--writes` to also expose the tools that create, update and delete, and keep in mind that the API token's own permissions still apply: a read-only token can never write.

A single response over 100 KB is refused with an error rather than passed to the model, which keeps one call from filling its context. Narrow the request, for example with a shorter `from`/`to` window on the metrics tools.

### AI agents

Agents can use `judo` as is: output is JSON, errors go to stderr with a non-zero exit code, and `judo <resource> <command> --help` lists every flag. Give an agent its own token, and make it read-only unless it needs to change things (a `member` token also can't use admin-only actions):

```sh
judo api-tokens create --label agent --role member --read_only
```

Creating a token needs an admin token, and the plain token is only shown once.

Agents that support MCP can use the MCP server above instead.

To point an agent at the CLI, add something like this to its instructions (`AGENTS.md`, `CLAUDE.md`):

```md
## Judoscale

Use the `judo` CLI for the Judoscale API. Output is JSON.

- `judo help` lists resources, and `judo <resource> <command> --help` shows a command's flags.
- Preview a change with `--dry-run` first. Deletes need `--yes`.
- Use `--all` on list commands to follow pagination.
```

### Environment

| Variable                    | Description                                         |
| --------------------------- | --------------------------------------------------- |
| `JUDOSCALE_API_TOKEN`       | API token, overrides the one from `judo auth login` |
| `JUDOSCALE_NO_UPDATE_CHECK` | Set to turn off the daily update check              |

### API spec

The binary embeds a snapshot of the spec. `judo update` downloads the latest spec from the Judoscale API to `~/.config/judoscale/openapi.json` (or `$XDG_CONFIG_HOME/judoscale/`, `%AppData%\judoscale\` on Windows), and that copy overrides the snapshot. The CLI never updates it on its own. If the latest spec needs a newer CLI, `judo update` asks you to upgrade instead.

### Updates

On a terminal, the CLI checks once a day whether a newer release exists and prints a one-line notice. It never updates itself. It stays quiet in CI (`CI` set), for `judo api` and `judo mcp`, and when piped, and `JUDOSCALE_NO_UPDATE_CHECK` turns it off.

### Exit codes

`0` success, `1` API or runtime error (details on stderr), `2` usage error.

## Development

```sh
script/test   # go vet + go test
script/build
```

The embedded snapshot lives in `internal/spec/openapi.json`. Refresh it with `script/update-spec [base-url]` (production by default; requires `jq`), which downloads the spec through `judo update`, drops examples and points `servers` at production. Run `script/build` afterwards to embed it. A daily workflow also runs `script/update-spec` and opens a PR when the spec changes.
