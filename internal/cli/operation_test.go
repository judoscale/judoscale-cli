package cli

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOperationPathParam(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apps/42/processes" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Write([]byte(`{"processes":[]}`))
	})

	res := run(t, srv, nil, "", "processes", "list", "42")
	if res.code != 0 || !strings.Contains(res.stdout, `"processes"`) {
		t.Errorf("%+v", res)
	}
	if res := run(t, srv, nil, "", "processes", "list"); res.code != 2 || !strings.Contains(res.stderr, "usage: judo processes list <app_id>") {
		t.Errorf("missing arg: %+v", res)
	}
}

func TestOperationFlagsAfterPositionals(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/v1/processes/7/metrics/queue_time" || q.Get("from") != "A" || q.Get("to") != "B" {
			t.Errorf("url = %s", r.URL)
		}
		w.Write([]byte(`{}`))
	})

	res := run(t, srv, nil, "", "metrics", "get-series", "7", "--from", "A", "queue_time", "--to=B")
	if res.code != 0 {
		t.Errorf("%+v", res)
	}
}

func TestOperationRequiredFlag(t *testing.T) {
	res := run(t, nil, nil, "", "metrics", "get-series", "7", "queue_time", "--from", "A")
	if res.code != 2 || !strings.Contains(res.stderr, "missing required flag --to") {
		t.Errorf("%+v", res)
	}
}

func TestOperationAllPages(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "" {
			w.Write([]byte(`{"scale_events":[{"id":1}],"next_cursor":"n"}`))
			return
		}
		w.Write([]byte(`{"scale_events":[{"id":2}]}`))
	})

	res := run(t, srv, nil, "", "scale-events", "list", "1", "--all")
	if res.code != 0 || strings.Join(strings.Fields(res.stdout), "") != `{"scale_events":[{"id":1},{"id":2}]}` {
		t.Errorf("%+v", res)
	}
	// --all exists only on paginated operations.
	if res := run(t, srv, nil, "", "apps", "list", "--all"); res.code != 2 {
		t.Errorf("apps list --all: %+v", res)
	}
}

func TestOperationBodyFromFlags(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		want := `{"autoscale_metrics":["queue_time"],"autoscaling_enabled":true,"max_scale":10,"schedule_time_zone":"UTC","sniping_enabled":false}`
		if r.Method != "PATCH" || r.URL.Path != "/v1/processes/7" || string(body) != want {
			t.Errorf("%s %s %s", r.Method, r.URL.Path, body)
		}
		w.Write([]byte(`{}`))
	})

	res := run(t, srv, nil, "", "processes", "update", "7", "--max_scale", "10", "--autoscaling_enabled",
		"--sniping_enabled=false", "--schedule_time_zone", "UTC", "--autoscale_metrics", `["queue_time"]`)
	if res.code != 0 {
		t.Errorf("%+v", res)
	}
}

func TestOperationBodyFromData(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"label":"ci"}` {
			t.Errorf("body = %s", body)
		}
		w.Write([]byte(`{}`))
	})

	if res := run(t, srv, nil, "", "api-tokens", "create", "-d", `{"label":"ci"}`); res.code != 0 {
		t.Errorf("--data: %+v", res)
	}
	if res := run(t, srv, nil, `{"label":"ci"}`, "api-tokens", "create", "--data", "-"); res.code != 0 {
		t.Errorf("--data -: %+v", res)
	}
}

func TestOperationBodyErrors(t *testing.T) {
	cases := [][]string{
		{"processes", "update", "7", "--max_scale", "ten"},
		{"processes", "update", "7", "--autoscale_metrics", "queue_time"},
		{"processes", "update", "7", "--max_scale", "1", "--data", "{}"},
	}
	for _, args := range cases {
		if res := run(t, nil, nil, "", args...); res.code != 2 {
			t.Errorf("%v: %+v", args, res)
		}
	}
}

func TestOperationWithoutBody(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || r.URL.Path != "/v1/apps/3/maintenance" || len(body) != 0 {
			t.Errorf("%s %s %q", r.Method, r.URL.Path, body)
		}
		w.Write([]byte(`{}`))
	})

	if res := run(t, srv, nil, "", "maintenance", "enable", "3"); res.code != 0 {
		t.Errorf("%+v", res)
	}
}

func TestDeleteNeedsYes(t *testing.T) {
	var calls int
	srv := server(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "DELETE" {
			t.Errorf("method = %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	res := run(t, srv, nil, "", "apps", "delete", "1")
	if res.code != 2 || !strings.Contains(res.stderr, "pass --yes") || calls != 0 {
		t.Errorf("without --yes: %+v (calls %d)", res, calls)
	}
	if res := run(t, srv, nil, "", "apps", "delete", "1", "--yes"); res.code != 0 || calls != 1 {
		t.Errorf("with --yes: %+v (calls %d)", res, calls)
	}
	if res := run(t, srv, nil, "", "maintenance", "disable", "1"); res.code != 0 || calls != 2 {
		t.Errorf("maintenance disable: %+v (calls %d)", res, calls)
	}
	if res := run(t, nil, nil, "", "apps", "delete", "--help"); !strings.Contains(res.stdout, "--yes") {
		t.Errorf("help: %s", res.stdout)
	}
}

func TestWriteOperationHelp(t *testing.T) {
	res := run(t, nil, nil, "", "processes", "update", "--help")
	for _, want := range []string{"--max_scale int", "--autoscaling_enabled  ", "--queue_names json", "--schedule_time_zone string", "-d, --data string", "--name=false"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("help missing %q:\n%s", want, res.stdout)
		}
	}
	group := run(t, nil, nil, "", "apps")
	if !strings.Contains(group.stdout, "  update ") || !strings.Contains(group.stdout, "  delete ") {
		t.Errorf("apps help missing write commands:\n%s", group.stdout)
	}
}

func TestOperationHelp(t *testing.T) {
	res := run(t, nil, nil, "", "scale-events", "list", "--help")
	if res.code != 0 {
		t.Fatalf("%+v", res)
	}
	for _, want := range []string{"Usage: judo scale-events list <process_id> [flags]", "--cursor", "--from", "--all"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("help missing %q:\n%s", want, res.stdout)
		}
	}
}

func TestOperationHelpListsNestedFields(t *testing.T) {
	res := run(t, nil, nil, "", "schedule", "update", "--help")
	for _, want := range []string{"--entries json", "wday int", "time string", "min_scale int"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("help missing %q:\n%s", want, res.stdout)
		}
	}
}

func TestKebab(t *testing.T) {
	if got := kebab("getProcessMetricSeriesPerInstance"); got != "get-process-metric-series-per-instance" {
		t.Errorf("kebab = %q", got)
	}
}

func TestRootHelpListsResources(t *testing.T) {
	help := run(t, nil, nil, "").stdout
	last := -1
	for _, want := range []string{"General:", "Resources:", "  team ", "  apps ", "  processes ", "  metrics ", "  schedule "} {
		i := strings.Index(help, want)
		if i < 0 || i < last {
			t.Fatalf("%q out of order or missing in:\n%s", want, help)
		}
		last = i
	}
	if strings.Contains(help, "list-apps") || strings.Contains(help, "get-series") {
		t.Errorf("root help lists commands:\n%s", help)
	}
}

func TestGroupHelp(t *testing.T) {
	for _, args := range [][]string{{"schedule"}, {"schedule", "--help"}, {"schedule", "help"}} {
		res := run(t, nil, nil, "", args...)
		if res.code != 0 || !strings.Contains(res.stdout, "Usage: judo schedule <command>") || !strings.Contains(res.stdout, "  update ") {
			t.Errorf("%v: %+v", args, res)
		}
	}
	if res := run(t, nil, nil, "", "schedule", "nope"); res.code != 2 || !strings.Contains(res.stderr, `unknown command "schedule nope"`) {
		t.Errorf("unknown: %+v", res)
	}
}

func TestResourceOrder(t *testing.T) {
	const cached = `{"tags":[{"name":"B"},{"name":"A"}],"paths":{
		"/1":{"get":{"operationId":"getA","summary":"a","tags":["A"]}},
		"/2":{"get":{"operationId":"getB","summary":"b","tags":["B"]}},
		"/3":{"get":{"operationId":"getZ","summary":"z","tags":["Z"]}},
		"/4":{"get":{"operationId":"getY","summary":"y","tags":["Y"]}},
		"/5":{"get":{"operationId":"getNone","summary":"n"}}}}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "judoscale"), 0o755)
	os.WriteFile(filepath.Join(dir, "judoscale", "openapi.json"), []byte(cached), 0o644)

	help := runIn(t, dir, nil, nil, "").stdout
	last := -1
	// Declared tags in spec order, undeclared alphabetically, untagged last.
	for _, want := range []string{"\n  b ", "\n  a ", "\n  y ", "\n  z ", "\n  other "} {
		i := strings.Index(help, want)
		if i < 0 || i < last {
			t.Fatalf("%q out of order or missing in:\n%s", want, help)
		}
		last = i
	}
}
