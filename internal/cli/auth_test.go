package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/judoscale/judoscale-cli/internal/spec"
)

const teamBody = `{"name":"Acme","api_token":{"label":"ci","role":"member","read_only":true}}`

// teamServer answers GET /v1/team for the token "good" and 401s otherwise.
func teamServer(t *testing.T) string {
	t.Helper()
	s := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/team" || r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"type":"unauthorized","message":"Invalid token"}}`))
			return
		}
		w.Write([]byte(teamBody))
	})
	return s.URL
}

func TestAuthLoginSavesToken(t *testing.T) {
	url := teamServer(t)
	dir := t.TempDir()
	env := map[string]string{"JUDO_API_TOKEN": "", "JUDO_API_URL": url}

	res := runIn(t, dir, nil, env, "good\n", "auth", "login")
	if res.code != 0 || !strings.Contains(res.stdout, "Logged in to Acme") {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(res.stderr, "Paste your API token") {
		t.Errorf("no prompt: %q", res.stderr)
	}
	path := filepath.Join(dir, "judoscale", "credentials")
	if data, _ := os.ReadFile(path); strings.TrimSpace(string(data)) != "good" {
		t.Errorf("saved = %q", data)
	}
}

func TestAuthLoginWithToken(t *testing.T) {
	url := teamServer(t)
	env := map[string]string{"JUDO_API_TOKEN": "", "JUDO_API_URL": url}

	res := runIn(t, t.TempDir(), nil, env, "good", "auth", "login", "--with-token")
	if res.code != 0 || strings.Contains(res.stderr, "Paste") {
		t.Errorf("%+v", res)
	}
}

func TestAuthLoginRejectsBadToken(t *testing.T) {
	url := teamServer(t)
	dir := t.TempDir()
	env := map[string]string{"JUDO_API_TOKEN": "", "JUDO_API_URL": url}

	res := runIn(t, dir, nil, env, "bad\n", "auth", "login")
	if res.code != 1 || !strings.Contains(res.stderr, "unauthorized") {
		t.Errorf("%+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "judoscale", "credentials")); !os.IsNotExist(err) {
		t.Errorf("a rejected token was saved: %v", err)
	}
	if res := runIn(t, dir, nil, env, "\n", "auth", "login"); res.code != 1 {
		t.Errorf("empty token: %+v", res)
	}
}

func TestAuthStatusAndLogout(t *testing.T) {
	url := teamServer(t)
	dir := t.TempDir()
	env := map[string]string{"JUDO_API_TOKEN": "", "JUDO_API_URL": url}

	if res := runIn(t, dir, nil, env, "", "auth", "status"); res.code != 1 || !strings.Contains(res.stderr, "not logged in") {
		t.Errorf("logged out: %+v", res)
	}

	runIn(t, dir, nil, env, "good", "auth", "login", "--with-token")
	res := runIn(t, dir, nil, env, "", "auth", "status")
	if res.code != 0 || !strings.Contains(res.stdout, `"name": "Acme"`) || !strings.Contains(res.stdout, "****") || strings.Contains(res.stdout, "good") {
		t.Errorf("status: %+v", res)
	}

	if res := runIn(t, dir, nil, env, "", "auth", "logout"); res.code != 0 || !strings.Contains(res.stdout, "removed") {
		t.Errorf("logout: %+v", res)
	}
	if res := runIn(t, dir, nil, env, "", "auth", "status"); res.code != 1 {
		t.Errorf("after logout: %+v", res)
	}
}

func TestAuthStatusFromEnv(t *testing.T) {
	url := teamServer(t)
	env := map[string]string{"JUDO_API_TOKEN": "good", "JUDO_API_URL": url}

	res := runIn(t, t.TempDir(), nil, env, "", "auth", "status")
	if res.code != 0 || !strings.Contains(res.stdout, "from JUDO_API_TOKEN") {
		t.Errorf("%+v", res)
	}
}

// login reads the team's name, so fail if a spec refresh drops it.
func TestTeamNameIsInTheSpec(t *testing.T) {
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(spec.Snapshot, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Components.Schemas["Team"].Properties["name"]; !ok {
		t.Error("schema Team has no property \"name\"")
	}
}
