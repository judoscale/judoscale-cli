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

	"github.com/judoscale/judoscale-cli/internal/spec"
)

// noticeAPI stands in for GitHub's releases and the API's /openapi.json.
type noticeAPI struct {
	*httptest.Server
	tag, specETag         string
	releaseHits, specHits int
}

func newNoticeAPI(t *testing.T, tag, specETag string) *noticeAPI {
	n := &noticeAPI{tag: tag, specETag: specETag}
	n.Server = server(t, func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "judoscale-cli/") {
			t.Errorf("User-Agent = %q", ua)
		}
		if r.URL.Path == "/openapi.json" {
			n.specHits++
			if r.Method != http.MethodHead || r.Header.Get("If-None-Match") == n.specETag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", n.specETag)
			return
		}
		n.releaseHits++
		w.Write([]byte(`{"tag_name":"` + n.tag + `"}`))
	})
	return n
}

// notifyRun runs a command as an interactive terminal on version 1.0.0, with
// GitHub and the API replaced by api.
func notifyRun(t *testing.T, dir string, api *noticeAPI, env map[string]string, args ...string) string {
	t.Helper()
	e := map[string]string{"JUDO_API_URL": api.URL}
	for k, v := range env {
		e[k] = v
	}
	var errOut bytes.Buffer
	app := &App{
		Version:     "1.0.0",
		Stdin:       strings.NewReader(""),
		Stdout:      &bytes.Buffer{},
		Stderr:      &errOut,
		Getenv:      func(k string) string { return e[k] },
		ConfigDir:   dir,
		Interactive: true,
		ReleasesURL: api.URL + "/releases/latest",
	}
	if code := app.Run(context.Background(), args); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	return errOut.String()
}

// currentETag is the ETag of the embedded snapshot, which is what the API
// answers "not modified" to when there's no cached spec.
func currentETag(t *testing.T) string {
	etag := spec.ETag(t.TempDir())
	if etag == "" {
		t.Skip("snapshot has no ETag")
	}
	return etag
}

func TestUpdateNoticeOncePerDay(t *testing.T) {
	api := newNoticeAPI(t, "v1.2.0", currentETag(t))
	dir := t.TempDir()

	out := notifyRun(t, dir, api, nil, "version")
	if !strings.Contains(out, "1.2.0 (you have 1.0.0)") || strings.Contains(out, "API spec") {
		t.Errorf("first run: %q", out)
	}
	if out := notifyRun(t, dir, api, nil, "version"); out != "" || api.releaseHits != 1 || api.specHits != 1 {
		t.Errorf("second run: %q (hits %d, %d)", out, api.releaseHits, api.specHits)
	}

	// Due again after a day.
	state := filepath.Join(dir, "judoscale", updateCheckFile)
	old := time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339)
	os.WriteFile(state, []byte(`{"checked_at":"`+old+`"}`), 0o600)
	if out := notifyRun(t, dir, api, nil, "version"); !strings.Contains(out, "1.2.0") || api.releaseHits != 2 {
		t.Errorf("after a day: %q (hits %d)", out, api.releaseHits)
	}
}

func TestUpdateNoticeQuietWhenCurrent(t *testing.T) {
	api := newNoticeAPI(t, "v1.0.0", currentETag(t))
	if out := notifyRun(t, t.TempDir(), api, nil, "version"); out != "" || api.releaseHits != 1 || api.specHits != 1 {
		t.Errorf("%q (hits %d, %d)", out, api.releaseHits, api.specHits)
	}
}

func TestSpecNotice(t *testing.T) {
	api := newNoticeAPI(t, "v1.0.0", `W/"newer"`)

	out := notifyRun(t, t.TempDir(), api, nil, "version")
	if !strings.Contains(out, "A newer API spec is available. Run `judo update`") || strings.Contains(out, "judo is available") {
		t.Errorf("snapshot outdated: %q", out)
	}

	// With a cached spec, its own ETag is compared.
	dir := t.TempDir()
	cache := filepath.Join(dir, "judoscale")
	os.MkdirAll(cache, 0o700)
	os.WriteFile(filepath.Join(cache, "openapi.json"), []byte("{}"), 0o600)
	os.WriteFile(filepath.Join(cache, "openapi.etag"), []byte(`W/"newer"`), 0o600)
	if out := notifyRun(t, dir, api, nil, "version"); out != "" {
		t.Errorf("cache current: %q", out)
	}

	// A cache without an ETag can't be compared.
	os.Remove(filepath.Join(cache, "openapi.etag"))
	os.Remove(filepath.Join(cache, updateCheckFile))
	hits := api.specHits
	if out := notifyRun(t, dir, api, nil, "version"); out != "" || api.specHits != hits {
		t.Errorf("cache without etag: %q (spec hits %d)", out, api.specHits-hits)
	}
}

func TestSpecNoticeOnDevBuild(t *testing.T) {
	api := newNoticeAPI(t, "v9.0.0", `W/"newer"`)
	var errOut bytes.Buffer
	app := &App{
		Version:     "dev",
		Stdin:       strings.NewReader(""),
		Stdout:      &bytes.Buffer{},
		Stderr:      &errOut,
		Getenv:      func(k string) string { return map[string]string{"JUDO_API_URL": api.URL}[k] },
		ConfigDir:   t.TempDir(),
		Interactive: true,
		ReleasesURL: api.URL + "/releases/latest",
	}
	app.Run(context.Background(), []string{"version"})
	if out := errOut.String(); !strings.Contains(out, "API spec") || strings.Contains(out, "judo is available") || api.releaseHits != 0 {
		t.Errorf("%q (release hits %d)", out, api.releaseHits)
	}
}

func TestUpdateNoticeSkipped(t *testing.T) {
	api := newNoticeAPI(t, "v9.0.0", `W/"newer"`)

	for name, env := range map[string]map[string]string{
		"CI":      {"CI": "true"},
		"opt-out": {"JUDO_NO_UPDATE_CHECK": "1"},
	} {
		if out := notifyRun(t, t.TempDir(), api, env, "version"); out != "" {
			t.Errorf("%s: %q", name, out)
		}
	}
	for _, args := range [][]string{{"api", "--help"}} {
		if out := notifyRun(t, t.TempDir(), api, nil, args...); out != "" {
			t.Errorf("%v: %q", args, out)
		}
	}
	if api.releaseHits+api.specHits != 0 {
		t.Errorf("hits = %d, %d", api.releaseHits, api.specHits)
	}
}

func TestUpdateNoticeNotInteractive(t *testing.T) {
	api := newNoticeAPI(t, "v9.0.0", `W/"newer"`)
	// The test helper from cli_test.go is not interactive and runs as "dev".
	if res := run(t, api.Server, nil, "", "version"); res.stderr != "" || api.releaseHits+api.specHits != 0 {
		t.Errorf("%+v (hits %d, %d)", res, api.releaseHits, api.specHits)
	}
}

func TestUpdateNoticeCheckFailureIsQuiet(t *testing.T) {
	dir := t.TempDir()
	srv := server(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) })
	api := &noticeAPI{Server: srv}
	if out := notifyRun(t, dir, api, nil, "version"); out != "" {
		t.Errorf("%q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "judoscale", updateCheckFile)); err != nil {
		t.Errorf("failed check not recorded: %v", err)
	}
}
