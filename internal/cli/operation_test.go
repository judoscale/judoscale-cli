package cli

import (
	"net/http"
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

	res := run(t, srv, nil, "", "list-app-processes", "42")
	if res.code != 0 || !strings.Contains(res.stdout, `"processes"`) {
		t.Errorf("%+v", res)
	}
	if res := run(t, srv, nil, "", "list-app-processes"); res.code != 2 || !strings.Contains(res.stderr, "usage: judo list-app-processes <app_id>") {
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

	res := run(t, srv, nil, "", "get-process-metric-series", "7", "--from", "A", "queue_time", "--to=B")
	if res.code != 0 {
		t.Errorf("%+v", res)
	}
}

func TestOperationRequiredFlag(t *testing.T) {
	res := run(t, nil, nil, "", "get-process-metric-series", "7", "queue_time", "--from", "A")
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

	res := run(t, srv, nil, "", "list-process-scale-events", "1", "--all")
	if res.code != 0 || strings.Join(strings.Fields(res.stdout), "") != `{"scale_events":[{"id":1},{"id":2}]}` {
		t.Errorf("%+v", res)
	}
	// --all exists only on paginated operations.
	if res := run(t, srv, nil, "", "list-apps", "--all"); res.code != 2 {
		t.Errorf("list-apps --all: %+v", res)
	}
}

func TestOnlyReadOnlyOperationsAreCommands(t *testing.T) {
	res := run(t, nil, nil, "", "delete-app", "1")
	if res.code != 2 || !strings.Contains(res.stderr, "unknown command") {
		t.Errorf("%+v", res)
	}
	help := run(t, nil, nil, "")
	for _, want := range []string{"list-apps", "get-process-metric-series", "api "} {
		if !strings.Contains(help.stdout, want) {
			t.Errorf("help missing %q", want)
		}
	}
	if strings.Contains(help.stdout, "delete-app") || strings.Contains(help.stdout, "update-app") {
		t.Error("help lists write operations")
	}
}

func TestOperationHelp(t *testing.T) {
	res := run(t, nil, nil, "", "list-process-scale-events", "--help")
	if res.code != 0 {
		t.Fatalf("%+v", res)
	}
	for _, want := range []string{"Usage: judo list-process-scale-events <process_id> [flags]", "--cursor", "--from", "--all"} {
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
