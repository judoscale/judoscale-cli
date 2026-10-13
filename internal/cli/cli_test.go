package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type result struct {
	code           int
	stdout, stderr string
}

// run executes the CLI against srv (may be nil) with a fresh config dir.
func run(t *testing.T, srv *httptest.Server, env map[string]string, stdin string, args ...string) result {
	t.Helper()
	return runIn(t, t.TempDir(), srv, env, stdin, args...)
}

func runIn(t *testing.T, configDir string, srv *httptest.Server, env map[string]string, stdin string, args ...string) result {
	t.Helper()
	var out, errOut bytes.Buffer
	e := map[string]string{"JUDOSCALE_API_TOKEN": "jdo_test"}
	if srv != nil {
		e["JUDOSCALE_API_URL"] = srv.URL
	}
	for k, v := range env {
		e[k] = v
	}
	app := &App{
		Version:   "dev",
		Stdin:     strings.NewReader(stdin),
		Stdout:    &out,
		Stderr:    &errOut,
		Getenv:    func(k string) string { return e[k] },
		ConfigDir: configDir,
	}
	return result{app.Run(context.Background(), args), out.String(), errOut.String()}
}

func server(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAPICommand(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/apps" || r.Header.Get("Authorization") != "Bearer jdo_test" {
			t.Errorf("%s %s %q", r.Method, r.URL, r.Header.Get("Authorization"))
		}
		if got := r.Header.Get("User-Agent"); got != "judoscale-cli/dev" {
			t.Errorf("User-Agent = %q", got)
		}
		w.Write([]byte(`{"apps":[]}`))
	})

	res := run(t, srv, nil, "", "api", "get", "/v1/apps")
	if res.code != 0 || res.stdout != "{\n  \"apps\": []\n}\n" {
		t.Errorf("%+v", res)
	}
}

func TestAPICommandBodyFromStdin(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		if r.Method != "PATCH" || buf.String() != `{"name":"x"}` {
			t.Errorf("%s %q", r.Method, buf)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	res := run(t, srv, nil, `{"name":"x"}`, "api", "PATCH", "/v1/team", "-d", "-")
	if res.code != 0 || res.stdout != "" {
		t.Errorf("%+v", res)
	}
}

func TestAPICommandAll(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "" {
			w.Write([]byte(`{"items":[1],"next_cursor":"n"}`))
			return
		}
		w.Write([]byte(`{"items":[2]}`))
	})

	res := run(t, srv, nil, "", "api", "GET", "/v1/x", "--all")
	if res.code != 0 || strings.Join(strings.Fields(res.stdout), "") != `{"items":[1,2]}` {
		t.Errorf("%+v", res)
	}
	if res := run(t, srv, nil, "", "api", "POST", "/v1/x", "--all"); res.code != 2 {
		t.Errorf("--all with POST: %+v", res)
	}
}

func TestAPIErrorEnvelopeExitsNonZero(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"error":{"type":"not_found","message":"No such app"}}`))
	})

	res := run(t, srv, nil, "", "api", "GET", "/v1/apps/1")
	if res.code != 1 || !strings.Contains(res.stderr, "not_found: No such app") || res.stdout != "" {
		t.Errorf("%+v", res)
	}
}

func TestMissingToken(t *testing.T) {
	res := run(t, nil, map[string]string{"JUDOSCALE_API_TOKEN": ""}, "", "api", "GET", "/v1/apps")
	if res.code != 1 || !strings.Contains(res.stderr, "JUDOSCALE_API_TOKEN is not set") {
		t.Errorf("%+v", res)
	}
}

func TestUnknownCommand(t *testing.T) {
	res := run(t, nil, nil, "", "nope")
	if res.code != 2 || !strings.Contains(res.stderr, "unknown command") {
		t.Errorf("%+v", res)
	}
}

func TestVersion(t *testing.T) {
	if res := run(t, nil, nil, "", "version"); res.stdout != "judo dev\n" {
		t.Errorf("%+v", res)
	}
}
