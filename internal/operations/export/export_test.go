package export

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var exportNow = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func exportRequest(profile Profile) Request {
	return Request{TenantID: "tenant-1", SubjectID: "operator-1", Scope: "promotion:1", Purpose: "promotion-evidence", SchemaDigest: "sha256:schema", Classification: "CONFIDENTIAL_HR", DLPPolicyRef: "dlp/export-v1", AllowedFields: []string{"name", "note"}, ExpiresAt: exportNow.Add(time.Hour), Profile: profile, Locale: "en-US", Records: []Record{{ID: "b", Fields: []Field{{Name: "name", Value: "Bob"}, {Name: "note", Value: "safe"}}}, {ID: "a", Fields: []Field{{Name: "name", Value: "Alice"}, {Name: "note", Value: "=SUM(A1:A2)"}}}}}
}

func TestSpreadsheetExportNeutralizesFormulaCellsWhileMachineExportPreservesExactValues(t *testing.T) {
	human, err := Export(exportRequest(HumanSpreadsheet), exportNow)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(human.Content, []byte("'=SUM(A1:A2)")) || len(human.Warnings) != 1 {
		t.Fatalf("human content=%q warnings=%v", human.Content, human.Warnings)
	}
	machine, err := Export(exportRequest(MachineData), exportNow)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(machine.Content, []byte(`"value":"=SUM(A1:A2)"`)) || bytes.Contains(machine.Content, []byte("'=SUM")) {
		t.Fatalf("machine content=%q", machine.Content)
	}
	if err := human.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := machine.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_EXPORT_001_Property(t *testing.T) {
	for _, profile := range []Profile{HumanSpreadsheet, MachineData} {
		artifact, err := Export(exportRequest(profile), exportNow)
		if err != nil {
			t.Fatal(err)
		}
		if artifact.Manifest.TenantID == "" || artifact.Manifest.Scope == "" || artifact.Manifest.Purpose == "" || artifact.Manifest.SchemaDigest == "" || artifact.Manifest.Classification == "" || artifact.Manifest.DLPPolicyRef == "" || artifact.Manifest.TransformationProfile == "" || artifact.Manifest.ManifestDigest == "" {
			t.Fatalf("incomplete manifest=%+v", artifact.Manifest)
		}
	}
}

func TestTodo_EXPORT_001_Golden(t *testing.T) {
	first, err := Export(exportRequest(HumanSpreadsheet), exportNow)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Export(exportRequest(HumanSpreadsheet), exportNow)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Content, second.Content) || first.Manifest.ManifestDigest != second.Manifest.ManifestDigest {
		t.Fatalf("non-deterministic export first=%q second=%q", first.Content, second.Content)
	}
}

func TestTodo_EXPORT_001_Integration(t *testing.T) {
	r := exportRequest(HumanSpreadsheet)
	r.Locale = "de-DE"
	artifact, err := Export(r, exportNow)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Manifest.Delimiter != ';' || !strings.Contains(string(artifact.Content), ";") {
		t.Fatalf("locale artifact=%+v content=%q", artifact.Manifest, artifact.Content)
	}
	if !strings.Contains(string(artifact.Content), "\"Bob\"") && !strings.Contains(string(artifact.Content), ";Bob") {
		t.Fatalf("record missing from content=%q", artifact.Content)
	}
}

func TestTodo_EXPORT_001_Security(t *testing.T) {
	for _, value := range []string{"=1+1", "+1", "-1", "@cmd", "\t=1", "\r=1", "\n=1", "＝1", "＋1", "－1", "＠cmd"} {
		r := exportRequest(HumanSpreadsheet)
		r.Records[0].Fields[1].Value = value
		artifact, err := Export(r, exportNow)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(artifact.Content), "'"+value) {
			t.Fatalf("value %q was not neutralized: %q", value, artifact.Content)
		}
	}
	for _, value := range []string{"=1+1", "+1", "-1", "@cmd", "\t=1", "\r=1", "\n=1", "\uFF1D1", "\uFF0B1", "\uFF0D1", "\uFF20cmd"} {
		r := exportRequest(MachineData)
		r.Records[1].Fields[1].Value = value
		artifact, err := Export(r, exportNow)
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			Records []struct {
				Fields []Field `json:"fields"`
			} `json:"records"`
		}
		if err := json.Unmarshal(artifact.Content, &document); err != nil {
			t.Fatal(err)
		}
		if got := document.Records[0].Fields[1].Value; got != value {
			t.Fatalf("machine value %q changed to %q", value, got)
		}
	}
	r := exportRequest(MachineData)
	r.Records[0].Fields = append(r.Records[0].Fields, Field{Name: "secret", Value: "x"})
	if _, err := Export(r, exportNow); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("unauthorized field=%v", err)
	}
}

func TestTodo_EXPORT_001_Conformance(t *testing.T) {
	for _, profile := range []Profile{HumanSpreadsheet, MachineData} {
		r := exportRequest(profile)
		r.AllowedFields = []string{"note", "name"}
		artifact, err := Export(r, exportNow)
		if err != nil {
			t.Fatal(err)
		}
		if err := artifact.Verify(); err != nil {
			t.Fatal(err)
		}
		artifact.Content[0] ^= 1
		if !errors.Is(artifact.Verify(), ErrTampered) {
			t.Fatal("content mutation accepted")
		}
	}
}

func FuzzTodo_EXPORT_001(f *testing.F) {
	f.Add("=formula")
	f.Add("ordinary")
	f.Fuzz(func(t *testing.T, value string) {
		r := exportRequest(HumanSpreadsheet)
		r.Records[0].Fields[1].Value = value
		artifact, err := Export(r, exportNow)
		if err != nil {
			return
		}
		if err := artifact.Verify(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestMachineDataJSONIsTypedAndRoundTripsExactValues(t *testing.T) {
	artifact, err := Export(exportRequest(MachineData), exportNow)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Records []struct {
			Fields []Field `json:"fields"`
		} `json:"records"`
	}
	if err := json.Unmarshal(artifact.Content, &document); err != nil {
		t.Fatal(err)
	}
	if document.Records[0].Fields[1].Value != "=SUM(A1:A2)" {
		t.Fatalf("round trip value=%q", document.Records[0].Fields[1].Value)
	}
}
