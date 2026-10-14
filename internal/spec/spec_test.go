package spec

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const miniSpec = `{
  "openapi": "3.2.0",
  "x-min-cli-version": "%s",
  "tags": [{"name": "Things"}, {"name": "Parts"}],
  "paths": {
    "/v1/things/{id}/parts/{part}": {
      "parameters": [{"name": "id", "in": "path", "required": true}],
      "get": {
        "operationId": "getThingPart", "summary": "Get a part", "tags": ["Parts", "Things"],
        "parameters": [
          {"name": "part", "in": "path", "required": true},
          {"name": "to", "in": "query", "required": true, "description": "end", "schema": {"type": "string"}},
          {"$ref": "#/components/parameters/From"}
        ]
      },
      "delete": {"operationId": "deleteThingPart"}
    },
    "/v1/noid": {"get": {"summary": "no operationId"}}
  },
  "components": {"parameters": {"From": {"name": "from", "in": "query", "description": "start"}}}
}`

func mini(min string) []byte { return []byte(strings.Replace(miniSpec, "%s", min, 1)) }

func TestParse(t *testing.T) {
	s, err := Parse(mini(""), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Operations) != 2 {
		t.Fatalf("ops = %+v", s.Operations)
	}
	if len(s.Tags) != 2 || s.Tags[0] != "Things" || s.Tags[1] != "Parts" {
		t.Errorf("tags = %v", s.Tags)
	}
	op := s.Operations[1] // sorted: deleteThingPart, getThingPart
	if op.Tag != "Parts" || s.Operations[0].Tag != "" {
		t.Errorf("tags: get=%q delete=%q", op.Tag, s.Operations[0].Tag)
	}
	if op.ID != "getThingPart" || op.Method != "GET" || op.Path != "/v1/things/{id}/parts/{part}" {
		t.Errorf("op = %+v", op)
	}
	if len(op.PathParams) != 2 || op.PathParams[0].Name != "id" || op.PathParams[1].Name != "part" {
		t.Errorf("path params = %+v", op.PathParams)
	}
	if len(op.QueryParams) != 2 || op.QueryParams[0] != (Param{Name: "from", Description: "start"}) || op.QueryParams[1] != (Param{Name: "to", Description: "end", Required: true, Type: "string"}) {
		t.Errorf("query params = %+v", op.QueryParams)
	}
}

func TestParseRequestBody(t *testing.T) {
	data := `{"paths":{"/a":{"patch":{"operationId":"updateA","requestBody":{"content":{"application/json":{"schema":{
	  "type":"object",
	  "properties":{"name":{"type":"string","description":"the name"},"count":{"type":"integer"},"tags":{"type":"array"}},
	  "required":["name"]
	}}}}}}}}`
	s, err := Parse([]byte(data), "dev")
	if err != nil {
		t.Fatal(err)
	}
	want := []Param{
		{Name: "count", Type: "integer"},
		{Name: "name", Description: "the name", Required: true, Type: "string"},
		{Name: "tags", Type: "array"},
	}
	if got := s.Operations[0].BodyParams; !slices.Equal(got, want) {
		t.Errorf("body params = %+v", got)
	}
}

func TestParseDuplicateOperationID(t *testing.T) {
	data := `{"paths":{"/a":{"get":{"operationId":"x"}},"/b":{"get":{"operationId":"x"}}}}`
	if _, err := Parse([]byte(data), "dev"); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("err = %v", err)
	}
}

func TestMinCLIVersion(t *testing.T) {
	cases := []struct {
		min, cli string
		ok       bool
	}{
		{"", "1.0.0", true},
		{"1.2.0", "dev", true},
		{"1.2.0", "1.2.0", true},
		{"1.2.0", "v1.10.0", true},
		{"1.2.0", "1.1.9", false},
		{"2.0", "1.9.9", false},
		{"1.2.0", "1.2.1-rc1", true},
	}
	for _, c := range cases {
		_, err := Parse(mini(c.min), c.cli)
		if (err == nil) != c.ok {
			t.Errorf("min=%q cli=%q: err = %v", c.min, c.cli, err)
		}
	}
	if _, err := Parse(mini("1.2.0"), "1.0.0"); err == nil || !strings.Contains(err.Error(), "upgrade the CLI") {
		t.Errorf("err = %v", err)
	}
}

func TestSnapshotParses(t *testing.T) {
	s, err := Parse(Snapshot, "dev")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, op := range s.Operations {
		if op.ID == "listProcessScaleEvents" {
			found = true
			if len(op.PathParams) != 1 || len(op.QueryParams) != 3 {
				t.Errorf("op = %+v", op)
			}
		}
	}
	if !found {
		t.Error("listProcessScaleEvents missing from snapshot")
	}
}

func TestLoadPrefersCache(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, cacheFile), mini(""), 0o644)

	s, warn, err := Load(dir, "dev")
	if err != nil || warn != "" {
		t.Fatalf("err=%v warn=%q", err, warn)
	}
	if len(s.Operations) != 2 {
		t.Errorf("expected cached spec, got %d ops", len(s.Operations))
	}
}

func TestLoadFallsBackToSnapshot(t *testing.T) {
	snap, _ := Parse(Snapshot, "dev")

	s, warn, err := Load(t.TempDir(), "dev")
	if err != nil || warn != "" || len(s.Operations) != len(snap.Operations) {
		t.Fatalf("missing cache: err=%v warn=%q", err, warn)
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, cacheFile), mini("9.0.0"), 0o644)
	s, warn, err = Load(dir, "1.0.0")
	if err != nil || !strings.Contains(warn, "ignoring cached spec") || len(s.Operations) != len(snap.Operations) {
		t.Fatalf("too-new cache: err=%v warn=%q", err, warn)
	}

	os.WriteFile(filepath.Join(dir, cacheFile), []byte("not json"), 0o644)
	if _, warn, _ = Load(dir, "1.0.0"); !strings.Contains(warn, "ignoring cached spec") {
		t.Errorf("corrupt cache warn = %q", warn)
	}
}

func TestUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openapi.json" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("update must not send the API token")
		}
		w.Write(mini(""))
	}))
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "judoscale")

	s, path, err := Update(context.Background(), srv.Client(), srv.URL, dir, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Operations) != 2 || path != filepath.Join(dir, cacheFile) {
		t.Errorf("ops=%d path=%s", len(s.Operations), path)
	}
	cached, _, _ := Load(dir, "dev")
	if len(cached.Operations) != 2 {
		t.Errorf("cache not used after update")
	}
}

func TestUpdateRefusesBadSpecAndKeepsCache(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, cacheFile), mini(""), 0o644)

	for name, body := range map[string][]byte{
		"too new":       mini("9.0.0"),
		"invalid json":  []byte("nope"),
		"no operations": []byte(`{"paths":{}}`),
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
		if _, _, err := Update(context.Background(), srv.Client(), srv.URL, dir, "1.0.0"); err == nil {
			t.Errorf("%s: expected error", name)
		}
		srv.Close()
	}
	if got, _ := os.ReadFile(filepath.Join(dir, cacheFile)); string(got) != string(mini("")) {
		t.Error("cache was overwritten")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestUpdateHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, _, err := Update(context.Background(), srv.Client(), srv.URL, t.TempDir(), "dev"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v", err)
	}
}
