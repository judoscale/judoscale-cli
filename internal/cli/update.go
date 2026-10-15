package cli

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/judoscale/judoscale-cli/internal/spec"
)

func (a *App) runUpdate(ctx context.Context, args []string) error {
	if len(args) > 0 {
		if args[0] == "-h" || args[0] == "--help" {
			fmt.Fprintln(a.Stdout, "Download the latest API spec and cache it.\nCommands are built from the cached spec, which overrides the one built into the binary.\n\nUsage: judo update")
			return nil
		}
		return usagef("usage: judo update")
	}
	if a.appDir() == "" {
		return fmt.Errorf("cannot determine the user config directory")
	}

	httpClient := &http.Client{Timeout: 60 * time.Second}
	s, path, err := spec.Update(ctx, httpClient, a.baseURL(), a.appDir(), a.Version)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "Updated API spec (%d operations) at %s\n", len(s.Operations), path)
	return nil
}
