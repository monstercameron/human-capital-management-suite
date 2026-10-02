package main

import (
	"reflect"
	"testing"
)

func TestAgentUXR5Setup_AccessCheckSelection(t *testing.T) {
	options := []personaAdminSelectionOption{{Value: "ir-001-walt-brennan", Label: "Walt Brennan — Employee"}, {Value: "ir-008-curtis-bell", Label: "Curtis Bell — Agent reviewer"}}
	for _, test := range []struct {
		name, typed, selected, wantValue, wantLabel string
		wantOK                                      bool
	}{
		{name: "enter accepts exact highlighted label", typed: "Walt Brennan — Employee", wantValue: "ir-001-walt-brennan", wantLabel: "Walt Brennan — Employee", wantOK: true},
		{name: "enter accepts highlighted prefix", typed: "Walt", wantValue: "ir-001-walt-brennan", wantLabel: "Walt Brennan — Employee", wantOK: true},
		{name: "retains selected option", typed: "Curtis Bell — Agent reviewer", selected: "ir-008-curtis-bell", wantValue: "ir-008-curtis-bell", wantLabel: "Curtis Bell — Agent reviewer", wantOK: true},
		{name: "refuses unknown free text", typed: "Unknown", wantLabel: "Unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, label, ok := personaAdminResolveSelection(test.typed, test.selected, options)
			if value != test.wantValue || label != test.wantLabel || ok != test.wantOK {
				t.Fatalf("resolve=(%q,%q,%v), want (%q,%q,%v)", value, label, ok, test.wantValue, test.wantLabel, test.wantOK)
			}
		})
	}
	if got := personaAdminMissingPreviewFields("policy-helper", "", ""); !reflect.DeepEqual(got, []string{"subject", "conversation"}) {
		t.Fatalf("missing fields=%v", got)
	}
}
