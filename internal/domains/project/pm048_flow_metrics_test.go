package project

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestTodo_PM_048(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	definition := FlowMetricDefinition{
		Version: "flow-v1", ProjectID: "project-1", Timezone: "America/New_York",
		WindowStart: start, WindowEnd: asOf,
		ExcludedTaskIDs: map[TaskID]string{"excluded": "records hold"},
	}
	events := []TaskFlowEvent{
		{Sequence: 3, TaskID: "open", ProjectID: "project-1", Kind: FlowStatusChanged, StatusCategory: FlowActive, OccurredAt: start.Add(5 * 24 * time.Hour)},
		{Sequence: 2, TaskID: "done", ProjectID: "project-1", Kind: FlowStatusChanged, StatusCategory: FlowActive, OccurredAt: start.Add(24 * time.Hour)},
		{Sequence: 4, TaskID: "done", ProjectID: "project-1", Kind: FlowStatusChanged, StatusCategory: FlowDone, OccurredAt: start.Add(3 * 24 * time.Hour)},
		{Sequence: 1, TaskID: "open", ProjectID: "project-1", Kind: FlowTaskCreated, StatusCategory: FlowNotStarted, OccurredAt: start.Add(4 * 24 * time.Hour)},
		{Sequence: 1, TaskID: "done", ProjectID: "project-1", Kind: FlowTaskCreated, StatusCategory: FlowNotStarted, OccurredAt: start},
		{Sequence: 1, TaskID: "excluded", ProjectID: "project-1", Kind: FlowTaskCreated, StatusCategory: FlowNotStarted, OccurredAt: start},
	}
	metrics, err := ComputeFlowMetrics(definition, events, asOf)
	if err != nil {
		t.Fatalf("ComputeFlowMetrics: %v", err)
	}
	if metrics.Timezone != "America/New_York" || metrics.Throughput != 1 || metrics.WIP != 1 || len(metrics.Tasks) != 2 {
		t.Fatalf("metrics lost definition or event semantics: %+v", metrics)
	}
	if metrics.Tasks[0].TaskID != "done" || metrics.Tasks[0].CycleTime != 48*time.Hour || metrics.Tasks[1].TaskID != "open" || !metrics.Tasks[1].WIP {
		t.Fatalf("unexpected event-derived metrics: %+v", metrics.Tasks)
	}
	invalid := definition
	invalid.Timezone = "not/a-timezone"
	if _, err := ComputeFlowMetrics(invalid, events, asOf); !errors.Is(err, ErrInvalidFlowDefinition) {
		t.Fatalf("invalid timezone error = %v", err)
	}
}

func TestTodo_PM_048_Golden(t *testing.T) {
	definition := FlowMetricDefinition{Version: "v1", ProjectID: "p", Timezone: "UTC", WindowStart: time.Unix(0, 0).UTC(), WindowEnd: time.Unix(100, 0).UTC()}
	metrics, err := ComputeFlowMetrics(definition, []TaskFlowEvent{
		{Sequence: 1, TaskID: "a", ProjectID: "p", Kind: FlowTaskCreated, StatusCategory: FlowNotStarted, OccurredAt: time.Unix(0, 0).UTC()},
		{Sequence: 2, TaskID: "a", ProjectID: "p", Kind: FlowStatusChanged, StatusCategory: FlowActive, OccurredAt: time.Unix(10, 0).UTC()},
		{Sequence: 3, TaskID: "a", ProjectID: "p", Kind: FlowStatusChanged, StatusCategory: FlowDone, OccurredAt: time.Unix(20, 0).UTC()},
	}, time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprintf("version=%s timezone=%s task=%s age=%s cycle=%s wip=%t throughput=%d wip_total=%d", metrics.DefinitionVersion, metrics.Timezone, metrics.Tasks[0].TaskID, metrics.Tasks[0].Age, metrics.Tasks[0].CycleTime, metrics.Tasks[0].WIP, metrics.Throughput, metrics.WIP)
	want := "version=v1 timezone=UTC task=a age=1m40s cycle=10s wip=false throughput=1 wip_total=0"
	if got != want {
		t.Fatalf("golden mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestTodo_PM_048_Property(t *testing.T) {
	definition := FlowMetricDefinition{Version: "v1", ProjectID: "p", Timezone: "UTC", WindowStart: time.Unix(0, 0).UTC(), WindowEnd: time.Unix(100, 0).UTC()}
	events := []TaskFlowEvent{
		{Sequence: 1, TaskID: "a", ProjectID: "p", Kind: FlowTaskCreated, StatusCategory: FlowNotStarted, OccurredAt: time.Unix(0, 0).UTC()},
		{Sequence: 2, TaskID: "a", ProjectID: "p", Kind: FlowStatusChanged, StatusCategory: FlowActive, OccurredAt: time.Unix(10, 0).UTC()},
		{Sequence: 3, TaskID: "a", ProjectID: "p", Kind: FlowStatusChanged, StatusCategory: FlowDone, OccurredAt: time.Unix(20, 0).UTC()},
	}
	want, err := ComputeFlowMetrics(definition, events, definition.WindowEnd)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(events); i++ {
		permuted := append([]TaskFlowEvent(nil), events[i:]...)
		permuted = append(permuted, events[:i]...)
		got, err := ComputeFlowMetrics(definition, permuted, definition.WindowEnd)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("permutation %d changed immutable-event metrics: got=%+v err=%v want=%+v", i, got, err, want)
		}
	}
}
