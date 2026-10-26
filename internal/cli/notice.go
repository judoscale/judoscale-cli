package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/judoscale/judoscale-cli/internal/spec"
)

const (
	latestReleaseURL = "https://api.github.com/repos/judoscale/judoscale-cli/releases/latest"
	updateCheckFile  = "update-check.json"
	updateCheckEvery = 24 * time.Hour
	updateCheckWait  = 2 * time.Second
)

type updateCheckState struct {
	CheckedAt time.Time `json:"checked_at"`
}

// notifyUpdates prints a hint when a newer CLI release or API spec exists. It
// checks at most once a day, only on a terminal, and never fails the command.
func (a *App) notifyUpdates(ctx context.Context) {
	if !a.Interactive || a.appDir() == "" ||
		a.Getenv("CI") != "" || a.Getenv("JUDO_NO_UPDATE_CHECK") != "" {
		return
	}
	if !a.updateCheckDue() {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, updateCheckWait)
	defer cancel()
	var cliNotice, specNotice string
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); cliNotice = a.cliUpdateNotice(ctx) }()
	go func() { defer wg.Done(); specNotice = a.specUpdateNotice(ctx) }()
	wg.Wait()

	var notices []string
	for _, n := range []string{cliNotice, specNotice} {
		if n != "" {
			notices = append(notices, n)
		}
	}
	if len(notices) > 0 {
		fmt.Fprintf(a.Stderr, "\n%s\n", strings.Join(notices, "\n"))
	}
}

func (a *App) cliUpdateNotice(ctx context.Context) string {
	if a.Version == "dev" {
		return ""
	}
	latest, err := a.latestRelease(ctx)
	if err != nil || !spec.IsNewer(latest, a.Version) {
		return ""
	}
	return fmt.Sprintf("A new version of judo is available: %s (you have %s)", latest, a.Version)
}

// specUpdateNotice asks the API whether the spec in use is still current,
// with a conditional request that doesn't download the spec.
func (a *App) specUpdateNotice(ctx context.Context) string {
	etag := spec.ETag(a.appDir())
	if etag == "" {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, strings.TrimRight(a.baseURL(), "/")+"/openapi.json", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("If-None-Match", etag)
	res, err := a.httpClient().Do(req)
	if err != nil {
		return ""
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ""
	}
	return "A newer API spec is available. Run `judo update` to get it."
}

// updateCheckDue reports whether a check is due, and records it as done so a
// failing check isn't retried on every run.
func (a *App) updateCheckDue() bool {
	path := filepath.Join(a.appDir(), updateCheckFile)
	var state updateCheckState
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &state)
	}
	if time.Since(state.CheckedAt) < updateCheckEvery {
		return false
	}
	data, _ := json.Marshal(updateCheckState{CheckedAt: time.Now().UTC()})
	if os.MkdirAll(a.appDir(), 0o700) == nil {
		os.WriteFile(path, data, 0o600)
	}
	return true
}

func (a *App) latestRelease(ctx context.Context) (string, error) {
	url := a.ReleasesURL
	if url == "" {
		url = latestReleaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := a.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("release check: %s", res.Status)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(res.Body).Decode(&release); err != nil {
		return "", err
	}
	return strings.TrimPrefix(release.TagName, "v"), nil
}
