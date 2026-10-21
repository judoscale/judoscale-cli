package cli

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateWritesCacheWhichOverridesSnapshot(t *testing.T) {
	const newSpec = `{"openapi":"3.2.0","paths":{"/v1/widgets":{"get":{"operationId":"listWidgets","summary":"List widgets","tags":["Widgets"]}}}}`
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openapi.json":
			w.Write([]byte(newSpec))
		case "/v1/widgets":
			w.Write([]byte(`{"widgets":[]}`))
		default:
			http.NotFound(w, r)
		}
	})
	dir := t.TempDir()

	if res := runIn(t, dir, srv, nil, "", "widgets", "list"); res.code != 2 {
		t.Fatalf("before update: %+v", res)
	}
	res := runIn(t, dir, srv, nil, "", "update")
	if res.code != 0 || !strings.Contains(res.stdout, "1 operations") {
		t.Fatalf("update: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "judoscale", "openapi.json")); err != nil {
		t.Fatal(err)
	}
	if res := runIn(t, dir, srv, nil, "", "widgets", "list"); res.code != 0 {
		t.Errorf("after update: %+v", res)
	}
	if res := runIn(t, dir, srv, nil, "", "apps", "list"); res.code != 2 {
		t.Errorf("cache should replace snapshot: %+v", res)
	}
}

func TestUpdateSaysWhenSpecIsCurrent(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Write([]byte(`{"paths":{"/a":{"get":{"operationId":"a"}}}}`))
	})
	dir := t.TempDir()

	if res := runIn(t, dir, srv, nil, "", "update"); !strings.HasPrefix(res.stdout, "Updated API spec") {
		t.Errorf("first: %+v", res)
	}
	if res := runIn(t, dir, srv, nil, "", "update"); res.code != 0 || !strings.HasPrefix(res.stdout, "API spec is already up to date (1 operations)") {
		t.Errorf("second: %+v", res)
	}
}

func TestUpdateRefusesSpecNeedingNewerCLI(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"x-min-cli-version":"99.0.0","paths":{"/a":{"get":{"operationId":"a"}}}}`))
	})
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	app := &App{Version: "1.0.0", Stdout: &out, Stderr: &errOut, ConfigDir: dir,
		Getenv: func(k string) string {
			if k == "JUDOSCALE_API_URL" {
				return srv.URL
			}
			return ""
		}}

	if code := app.Run(context.Background(), []string{"update"}); code != 1 || !strings.Contains(errOut.String(), "upgrade the CLI") {
		t.Errorf("code=%d stderr=%s", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "judoscale", "openapi.json")); err == nil {
		t.Error("spec was cached")
	}
}
