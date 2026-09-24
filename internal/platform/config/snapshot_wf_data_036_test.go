package config

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func wf036Consumer(kind ConsumerKind, id string) Consumer {
	return Consumer{Kind: kind, ID: id}
}

func wf036Definition(key string, typ workflow.ValueType, consumer Consumer) ParameterDefinition {
	return ParameterDefinition{
		Key:              key,
		Type:             typ,
		Classification:   "CONFIDENTIAL",
		Owner:            "finance-operations",
		Required:         true,
		HighImpact:       true,
		AllowedConsumers: []Consumer{consumer},
	}
}

func TestTodo_WF_DATA_036(t *testing.T) {
	workflowConsumer := wf036Consumer(ConsumerWorkflow, "hcmnext.promotion/v1")
	definition := wf036Definition("finance.approval_threshold", workflow.ValueType{Kind: workflow.KindDecimal}, workflowConsumer)
	definition.Default = &ParameterDefault{Type: workflow.ValueType{Kind: workflow.KindDecimal}, Value: "0.10"}
	if err := definition.Validate(); err != nil {
		t.Fatalf("valid parameter definition rejected: %v", err)
	}

	snapshot, err := NewSnapshotWithDefinitions("tenant", "v1", nil, []ParameterDefinition{definition})
	if err != nil {
		t.Fatalf("NewSnapshotWithDefinitions: %v", err)
	}
	got, ok := snapshot.ParameterDefinition(definition.Key)
	if !ok || got.Type.Kind != workflow.KindDecimal || got.Owner != definition.Owner || len(got.AllowedConsumers) != 1 {
		t.Fatalf("snapshot definition = %+v, present=%t", got, ok)
	}
	if err := got.CheckRead(workflowConsumer, workflow.ValueType{Kind: workflow.KindDecimal}); err != nil {
		t.Fatalf("authorized typed consumer rejected: %v", err)
	}
	if err := got.CheckRead(wf036Consumer(ConsumerWorkflow, "hcmnext.other/v1"), workflow.ValueType{Kind: workflow.KindDecimal}); !errors.Is(err, ErrParameterConsumerDenied) {
		t.Fatalf("unauthorized consumer error = %v, want ErrParameterConsumerDenied", err)
	}
	if err := got.CheckRead(workflowConsumer, workflow.ValueType{Kind: workflow.KindMoney}); !errors.Is(err, ErrParameterTypeMismatch) {
		t.Fatalf("different requested type error = %v, want ErrParameterTypeMismatch", err)
	}

	if err := wf036Definition("finance.bad_float", workflow.ValueType{Kind: workflow.Kind("FLOAT")}, workflowConsumer).Validate(); !errors.Is(err, ErrInvalidParameterDefinition) {
		t.Fatalf("FLOAT parameter type error = %v, want ErrInvalidParameterDefinition", err)
	}
	if err := wf036Definition("finance.bad_map", workflow.ValueType{Kind: workflow.Kind("MAP")}, workflowConsumer).Validate(); !errors.Is(err, ErrInvalidParameterDefinition) {
		t.Fatalf("untyped MAP parameter type error = %v, want ErrInvalidParameterDefinition", err)
	}
	secretWithoutRules := wf036Definition("finance.bad_secret", workflow.ValueType{Kind: workflow.KindString, Brand: "SecretReference"}, workflowConsumer)
	if err := secretWithoutRules.Validate(); !errors.Is(err, ErrInvalidParameterDefinition) {
		t.Fatalf("SecretReference without strict rules error = %v, want ErrInvalidParameterDefinition", err)
	}

	legacy, err := NewEntry("legacy.float", KindFloat, "1.5", true, SemanticGeneric, Refs{})
	if err != nil {
		t.Fatalf("legacy generic config entry behavior changed: %v", err)
	}
	if _, err := NewSnapshot("tenant", "v1", []Entry{legacy}); err != nil {
		t.Fatalf("legacy config snapshot rejected: %v", err)
	}
}

func TestTodo_WF_DATA_036_Golden(t *testing.T) {
	workflowConsumer := wf036Consumer(ConsumerWorkflow, "hcmnext.promotion/v1")
	packageConsumer := wf036Consumer(ConsumerPackage, "hcmnext.finance")
	definitions := []ParameterDefinition{
		wf036Definition("finance.threshold", workflow.ValueType{Kind: workflow.KindDecimal}, workflowConsumer),
		wf036Definition("finance.currency", workflow.ValueType{Kind: workflow.KindString, Brand: "CurrencyCode"}, packageConsumer),
	}
	snapshot, err := NewSnapshotWithDefinitions("tenant", "v1", nil, definitions)
	if err != nil {
		t.Fatal(err)
	}
	got := snapshot.ParameterDefinitions()
	if len(got) != 2 {
		t.Fatalf("definition count = %d, want 2", len(got))
	}
	golden := []string{
		"finance.currency|STRING#CurrencyCode|CONFIDENTIAL|finance-operations|true|true|PACKAGE:hcmnext.finance",
		"finance.threshold|DECIMAL|CONFIDENTIAL|finance-operations|true|true|WORKFLOW:hcmnext.promotion/v1",
	}
	for i, definition := range got {
		consumer := definition.AllowedConsumers[0]
		line := definition.Key + "|" + definition.Type.String() + "|" + definition.Classification + "|" + definition.Owner + "|" + boolText(definition.Required) + "|" + boolText(definition.HighImpact) + "|" + string(consumer.Kind) + ":" + consumer.ID
		if line != golden[i] {
			t.Fatalf("definition[%d] = %q, want %q", i, line, golden[i])
		}
	}
	got[0].AllowedConsumers[0].ID = "mutated"
	again, ok := snapshot.ParameterDefinition("finance.currency")
	if !ok || again.AllowedConsumers[0].ID != "hcmnext.finance" {
		t.Fatalf("snapshot definition changed through returned slice: %+v, present=%t", again, ok)
	}
}

func TestTodo_WF_DATA_036_Property(t *testing.T) {
	cases := []struct {
		name string
		typ  workflow.ValueType
	}{
		{name: "string", typ: workflow.ValueType{Kind: workflow.KindString}},
		{name: "branded string", typ: workflow.ValueType{Kind: workflow.KindString, Brand: "CurrencyCode"}},
		{name: "integer", typ: workflow.ValueType{Kind: workflow.KindInteger}},
		{name: "decimal", typ: workflow.ValueType{Kind: workflow.KindDecimal}},
		{name: "money", typ: workflow.ValueType{Kind: workflow.KindMoney}},
		{name: "bool", typ: workflow.ValueType{Kind: workflow.KindBool}},
		{name: "instant", typ: workflow.ValueType{Kind: workflow.KindInstant}},
		{name: "local date", typ: workflow.ValueType{Kind: workflow.KindLocalDate}},
		{name: "enum", typ: workflow.ValueType{Kind: workflow.KindEnum, EnumRef: "finance.currency/v1"}},
		{name: "message", typ: workflow.ValueType{Kind: workflow.KindMessage, MessageRef: "finance.approval/v1"}},
		{name: "list", typ: workflow.ValueType{Kind: workflow.KindList, Element: &workflow.ValueType{Kind: workflow.KindDecimal}}},
	}
	consumer := wf036Consumer(ConsumerCapability, "hcmnext.finance.read")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			definition := wf036Definition("finance.value", tc.typ, consumer)
			if err := definition.Validate(); err != nil {
				t.Fatalf("definition rejected: %v", err)
			}
			if err := definition.CheckRead(consumer, tc.typ); err != nil {
				t.Fatalf("same type rejected: %v", err)
			}
			wrong := workflow.ValueType{Kind: workflow.KindString}
			if tc.typ.Kind == workflow.KindString {
				wrong = workflow.ValueType{Kind: workflow.KindString, Brand: "DifferentBrand"}
			}
			if err := definition.CheckRead(consumer, wrong); !errors.Is(err, ErrParameterTypeMismatch) {
				t.Fatalf("different type %s accepted for %s: %v", wrong, tc.typ, err)
			}
			if !tc.typ.Nullable {
				widened := tc.typ
				widened.Nullable = true
				if err := definition.CheckRead(consumer, widened); !errors.Is(err, ErrParameterTypeMismatch) {
					t.Fatalf("nullable widening %s accepted for %s: %v", widened, tc.typ, err)
				}
			}
		})
	}
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
