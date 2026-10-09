package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const apiHelp = `Make a raw API request and print the JSON response.

Usage: judoscale api <METHOD> <path> [flags]

Flags:
  -d, --data string  JSON request body; use - to read it from stdin
      --all          Follow next_cursor and merge all pages (GET only)

Example:
  judoscale api GET /v1/apps
`

func (a *App) runAPI(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("api", flag.ContinueOnError)
	var data string
	var all bool
	fs.StringVar(&data, "data", "", "")
	fs.StringVar(&data, "d", "", "")
	fs.BoolVar(&all, "all", false, "")

	pos, err := parseInterleaved(fs, args)
	if err != nil {
		if err == flagHelp {
			fmt.Fprint(a.Stdout, apiHelp)
		}
		return err
	}
	if len(pos) != 2 {
		return usagef("usage: judoscale api <METHOD> <path> [flags]")
	}
	method, path := strings.ToUpper(pos[0]), pos[1]
	if all && method != http.MethodGet {
		return usagef("--all only works with GET")
	}

	var body []byte
	if data == "-" {
		if body, err = io.ReadAll(a.Stdin); err != nil {
			return err
		}
	} else if data != "" {
		body = []byte(data)
	}

	c, err := a.newClient()
	if err != nil {
		return err
	}
	var resp []byte
	if all {
		resp, err = c.GetAll(ctx, path, nil)
	} else {
		resp, err = c.Do(ctx, method, path, nil, body)
	}
	if err != nil {
		return err
	}
	return a.printJSON(resp)
}
