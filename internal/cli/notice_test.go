package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// notifyRun runs `judo version`-style commands as an interactive terminal on
// version 1.0.0, with GitHub replaced by srv.
func notifyRun(t *testing.T, dir string, srv *httptest.Server, env map[string]string, args ...string) string {
	t.Helper()
	var errOut bytes.Buffer
	app := &App{
		Version:     "1.0.0",
		Stdin:       strings.NewReader(""),
		Stdout:      &bytes.Buffer{},
		Stderr:      &errOut,
		Getenv:      func(k string) string { return env[k] },
		ConfigDir:   dir,
		Interactive: true,
		ReleasesURL: srv.URL,
	}
	if code := app.Run(context.Background(), args); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	return errOut.String()
}

func releaseServer(t *testing.T, tag string, hits *int) *httptest.Server {
	return server(t, func(w http.ResponseWriter, r *http.Request) {
		*hits++
		w.Write([]byte(`{"tag_name":"` + tag + `"}`))
	})
}

func TestUpdateNoticeOncePerDay(t *testing.T) {
	var hits int
	srv := releaseServer(t, "v1.2.0", &hits)
	dir := t.TempDir()

	out := notifyRun(t, dir, srv, nil, "version")
	if !strings.Contains(out, "1.2.0 (you have 1.0.0)") {
		t.Errorf("first run: %q", out)
	}
	if out := notifyRun(t, dir, srv, nil, "version"); out != "" || hits != 1 {
		t.Errorf("second run: %q (hits %d)", out, hits)
	}

	// Due again after a day.
	state := filepath.Join(dir, "judoscale", updateCheckFile)
	old := time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339)
	os.WriteFile(state, []byte(`{"checked_at":"`+old+`"}`), 0o600)
	if out := notifyRun(t, dir, srv, nil, "version"); !strings.Contains(out, "1.2.0") || hits != 2 {
		t.Errorf("after a day: %q (hits %d)", out, hits)
	}
}

func TestUpdateNoticeQuietWhenCurrent(t *testing.T) {
	var hits int
	srv := releaseServer(t, "v1.0.0", &hits)
	if out := notifyRun(t, t.TempDir(), srv, nil, "version"); out != "" || hits != 1 {
		t.Errorf("%q (hits %d)", out, hits)
	}
}

func TestUpdateNoticeSkipped(t *testing.T) {
	var hits int
	srv := releaseServer(t, "v9.0.0", &hits)

	for name, env := range map[string]map[string]string{
		"CI":      {"CI": "true"},
		"opt-out": {"JUDOSCALE_NO_UPDATE_CHECK": "1"},
	} {
		if out := notifyRun(t, t.TempDir(), srv, env, "version"); out != "" {
			t.Errorf("%s: %q", name, out)
		}
	}
	for _, args := range [][]string{{"api", "--help"}, {"mcp", "--help"}} {
		if out := notifyRun(t, t.TempDir(), srv, nil, args...); out != "" {
			t.Errorf("%v: %q", args, out)
		}
	}
	if hits != 0 {
		t.Errorf("hits = %d", hits)
	}
}

func TestUpdateNoticeNotInteractive(t *testing.T) {
	var hits int
	srv := releaseServer(t, "v9.0.0", &hits)
	// The test helper from cli_test.go is not interactive and runs as "dev".
	if res := run(t, srv, nil, "", "version"); res.stderr != "" || hits != 0 {
		t.Errorf("%+v (hits %d)", res, hits)
	}
}

func TestUpdateNoticeCheckFailureIsQuiet(t *testing.T) {
	dir := t.TempDir()
	srv := server(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) })
	if out := notifyRun(t, dir, srv, nil, "version"); out != "" {
		t.Errorf("%q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "judoscale", updateCheckFile)); err != nil {
		t.Errorf("failed check not recorded: %v", err)
	}
}
