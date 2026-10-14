package cli

import (
	"slices"
	"testing"

	"github.com/judoscale/judoscale-cli/internal/spec"
)

func treeNames(t *testing.T, doc string) []string {
	t.Helper()
	s, err := spec.Parse([]byte(doc), "dev")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, g := range commandTree(s) {
		for _, c := range g.commands {
			names = append(names, c.fullName())
		}
	}
	return names
}

func TestCommandTreeSnapshot(t *testing.T) {
	s, err := spec.Parse(spec.Snapshot, "dev")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, g := range commandTree(s) {
		for _, c := range g.commands {
			names = append(names, c.op.ID+" "+c.fullName())
		}
	}
	for _, want := range []string{
		"getTeam team get",
		"getApp apps get",
		"listAppProcesses processes list",
		"enableAppMaintenance maintenance enable",
		"getProcessSchedule schedule get",
		"updateProcessAutoscaleMetrics autoscale-metrics update",
		"getProcessMetricSeries metrics get-series",
		"listProcessScaleEvents scale-events list",
		"createApiToken api-tokens create",
	} {
		if !slices.Contains(names, want) {
			t.Errorf("missing %q in %v", want, names)
		}
	}
	if len(names) != len(s.Operations) {
		t.Errorf("%d commands for %d operations", len(names), len(s.Operations))
	}
}

func TestCommandTreeGroupClash(t *testing.T) {
	names := treeNames(t, `{"paths":{
		"/apps/{app_id}/metrics":{"get":{"operationId":"listAppMetrics","tags":["App Metrics"]}},
		"/processes/{process_id}/metrics":{"get":{"operationId":"listProcessMetrics","tags":["Process Metrics"]}}}}`)
	want := []string{"app-metrics list", "process-metrics list"}
	if !slices.Equal(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestCommandTreeCommandClash(t *testing.T) {
	names := treeNames(t, `{"paths":{
		"/apps/{app_id}/scale_events":{"get":{"operationId":"listAppScaleEvents","tags":["Process Scale Events"]}},
		"/processes/{process_id}/scale_events":{"get":{"operationId":"listProcessScaleEvents","tags":["Process Scale Events"]}}}}`)
	want := []string{"scale-events list-app", "scale-events list-process"}
	if !slices.Equal(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestCommandTreeUntagged(t *testing.T) {
	names := treeNames(t, `{"paths":{"/widgets":{"get":{"operationId":"listWidgets"}}}}`)
	if !slices.Equal(names, []string{"other list-widgets"}) {
		t.Errorf("got %v", names)
	}
}
