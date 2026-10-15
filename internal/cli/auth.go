package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/judoscale/judoscale-cli/internal/client"
)

const authHelp = `Log in with a Judoscale API token, so you don't need JUDOSCALE_API_TOKEN.

Usage: judo auth <command>

Commands:
  login   Check an API token and save it (--with-token reads it from stdin)
  logout  Remove the saved token
  status  Show where the token comes from and the team it belongs to

JUDOSCALE_API_TOKEN takes precedence over the saved token.
`

func (a *App) runAuth(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(a.Stdout, authHelp)
		return nil
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "login":
		return a.authLogin(ctx, rest)
	case "logout":
		if len(rest) > 0 {
			return usagef("usage: judo auth logout")
		}
		return a.authLogout()
	case "status":
		if len(rest) > 0 {
			return usagef("usage: judo auth status")
		}
		return a.authStatus(ctx)
	}
	return usagef("unknown command %q; run `judo auth` to list its commands", "auth "+cmd)
}

// fetchTeam calls GET /v1/team with the token, which also validates it. It
// returns the raw response and the team's name.
// TestTeamNameIsInTheSpec guards the name field against spec changes.
func (a *App) fetchTeam(ctx context.Context, token string) (body []byte, name string, err error) {
	c := client.New(a.baseURL(), token)
	c.UserAgent = "judoscale-cli/" + a.Version
	body, err = c.Do(ctx, "GET", "/v1/team", nil, nil)
	if err != nil {
		return nil, "", err
	}
	var team struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &team); err != nil {
		return nil, "", fmt.Errorf("unexpected response from /v1/team: %w", err)
	}
	return body, team.Name, nil
}

func (a *App) authLogin(ctx context.Context, args []string) error {
	fromStdin := false
	for _, arg := range args {
		if arg != "--with-token" {
			return usagef("usage: judo auth login [--with-token]")
		}
		fromStdin = true
	}

	var token string
	if fromStdin {
		data, err := io.ReadAll(a.Stdin)
		if err != nil {
			return err
		}
		token = strings.TrimSpace(string(data))
	} else {
		fmt.Fprint(a.Stderr, "Paste your API token: ")
		line, err := bufio.NewReader(a.Stdin).ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		token = strings.TrimSpace(line)
	}
	if token == "" {
		return errors.New("no token given")
	}

	_, name, err := a.fetchTeam(ctx, token)
	if err != nil {
		return err
	}
	path, err := a.saveToken(token)
	if err != nil {
		return err
	}

	fmt.Fprintf(a.Stdout, "Logged in to %s\nSaved to %s\n", name, path)
	if a.Getenv("JUDOSCALE_API_TOKEN") != "" {
		fmt.Fprintln(a.Stdout, "Note: JUDOSCALE_API_TOKEN is set and takes precedence over the saved token.")
	}
	return nil
}

func (a *App) authLogout() error {
	removed, err := a.removeToken()
	if err != nil {
		return err
	}
	if removed {
		fmt.Fprintln(a.Stdout, "Logged out; removed the saved token.")
	} else {
		fmt.Fprintln(a.Stdout, "No saved token.")
	}
	if a.Getenv("JUDOSCALE_API_TOKEN") != "" {
		fmt.Fprintln(a.Stdout, "Note: JUDOSCALE_API_TOKEN is still set.")
	}
	return nil
}

func (a *App) authStatus(ctx context.Context) error {
	token, source, err := a.token()
	if err != nil {
		return err
	}
	if token == "" {
		return errors.New("not logged in; run `judo auth login` or set JUDOSCALE_API_TOKEN")
	}
	fmt.Fprintf(a.Stdout, "Token: %s (from %s)\n", mask(token), source)
	fmt.Fprintf(a.Stdout, "API:   %s\n", a.baseURL())

	body, _, err := a.fetchTeam(ctx, token)
	if err != nil {
		return err
	}
	fmt.Fprintln(a.Stdout)
	return a.printJSON(body)
}

// mask shows only the end of a token.
func mask(token string) string {
	if len(token) <= 8 {
		return "****"
	}
	return "****" + token[len(token)-4:]
}
