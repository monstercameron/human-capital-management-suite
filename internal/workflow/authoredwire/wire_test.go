package authoredwire

import (
	"strings"
	"testing"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
)

// TestIssue3641_UnmarshalDecodesAuthoredDocument pins that a protojson document
// decodes into the wire message field by field.
func TestIssue3641_UnmarshalDecodesAuthoredDocument(t *testing.T) {
	data := []byte(`{"workflowId":"wf-1","version":3,"name":"Onboard"}`)
	pb, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: unexpected error: %v", err)
	}
	if got := pb.GetWorkflowId(); got != "wf-1" {
		t.Fatalf("WorkflowId = %q, want %q", got, "wf-1")
	}
	if got := pb.GetVersion(); got != 3 {
		t.Fatalf("Version = %d, want %d", got, 3)
	}
	if got := pb.GetName(); got != "Onboard" {
		t.Fatalf("Name = %q, want %q", got, "Onboard")
	}
}

// TestIssue3641_UnmarshalRejectsUnknownField pins strictness: an unknown key
// must fail, never be silently dropped.
func TestIssue3641_UnmarshalRejectsUnknownField(t *testing.T) {
	_, err := Unmarshal([]byte(`{"workflowId":"wf-1","inlineTransform":"x"}`))
	if err == nil {
		t.Fatal("Unmarshal accepted an unknown field; want an error")
	}
}

// TestIssue3641_UnmarshalRejectsMistypedField pins that a value of the wrong
// JSON type is an error.
func TestIssue3641_UnmarshalRejectsMistypedField(t *testing.T) {
	_, err := Unmarshal([]byte(`{"workflowId":42}`))
	if err == nil {
		t.Fatal("Unmarshal accepted a mistyped field; want an error")
	}
}

// TestIssue3641_UnmarshalRejectsMalformedJSON pins that syntactically broken
// input is an error and yields no message.
func TestIssue3641_UnmarshalRejectsMalformedJSON(t *testing.T) {
	pb, err := Unmarshal([]byte(`{"workflowId":`))
	if err == nil {
		t.Fatal("Unmarshal accepted malformed JSON; want an error")
	}
	if pb != nil {
		t.Fatalf("Unmarshal returned a message alongside an error: %v", pb)
	}
}

// TestIssue3641_MarshalRoundTrips pins that Marshal output is accepted by
// Unmarshal and preserves every field set on the message.
func TestIssue3641_MarshalRoundTrips(t *testing.T) {
	in := &workflowv1.AuthoredWorkflowDefinition{
		WorkflowId: "wf-9",
		Version:    2,
		Name:       "Offboard",
	}
	data, err := Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: unexpected error: %v", err)
	}
	out, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal of Marshal output: unexpected error: %v", err)
	}
	if out.GetWorkflowId() != "wf-9" || out.GetVersion() != 2 || out.GetName() != "Offboard" {
		t.Fatalf("round trip lost fields: got %v", out)
	}
}

// TestIssue3641_MarshalIsMultilineIndented pins the rendering options: the
// output is multiline and indented with two spaces, as the stored artifact is.
func TestIssue3641_MarshalIsMultilineIndented(t *testing.T) {
	data, err := Marshal(&workflowv1.AuthoredWorkflowDefinition{WorkflowId: "wf-1", Name: "n"})
	if err != nil {
		t.Fatalf("Marshal: unexpected error: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "\n  \"workflowId\": \"wf-1\"") {
		t.Fatalf("Marshal output is not two-space multiline protojson:\n%s", text)
	}
}

// TestIssue3641_MarshalNilMessageIsEmptyObject pins that a nil message renders
// as an empty JSON object rather than failing or panicking.
func TestIssue3641_MarshalNilMessageIsEmptyObject(t *testing.T) {
	data, err := Marshal(nil)
	if err != nil {
		t.Fatalf("Marshal(nil): unexpected error: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "{}" {
		t.Fatalf("Marshal(nil) = %q, want %q", got, "{}")
	}
}
