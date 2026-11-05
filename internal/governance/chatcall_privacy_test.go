package governance

import (
	"strings"
	"testing"
)

// TestTodo_CHATCALL_005 pins the call security, privacy and compliance
// record: always-on transport encryption with a per-kind end-to-end answer,
// strictest-wins consent, metadata rules, administrator visibility, abuse
// handling, governance inheritance from CHATVOICE-005, and the threat model
// the SECURITY test holds to one mitigation or accepted risk per threat.
func TestTodo_CHATCALL_005(t *testing.T) {
	record := CallPrivacyRecord()
	if strings.TrimSpace(record.TransportEncryption) == "" {
		t.Fatal("the record does not state the transport encryption rule")
	}
	if !strings.Contains(record.TransportEncryption, "always") {
		t.Fatal("transport encryption is not stated as always on")
	}
	if len(record.EndToEndByKind) != 4 {
		t.Fatalf("end-to-end answers = %d, want one per call kind", len(record.EndToEndByKind))
	}
	for _, row := range record.EndToEndByKind {
		if strings.TrimSpace(row.CallKind) == "" || strings.TrimSpace(row.Recommendation) == "" ||
			strings.TrimSpace(row.Protects) == "" || strings.TrimSpace(row.Prevents) == "" {
			t.Fatalf("end-to-end row leaves kind, recommendation, protection or cost open: %+v", row)
		}
	}
	if strings.TrimSpace(record.Consent.Rule) == "" || len(record.Consent.StrictJurisdictions) == 0 ||
		strings.TrimSpace(record.Consent.WorksCouncil) == "" {
		t.Fatalf("the consent section leaves the rule, the strict jurisdictions or the works-council answer open: %+v", record.Consent)
	}
	if len(record.MetadataFields) == 0 {
		t.Fatal("the record enumerates no call metadata")
	}
	for _, field := range record.MetadataFields {
		lower := strings.ToLower(field)
		for _, forbidden := range []string{"audio", "video", "transcript", "recording", "media"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("metadata field %q carries content, not metadata", field)
			}
		}
	}
	for _, section := range []struct {
		name, value string
	}{
		{"retention", record.MetadataRetention},
		{"hold", record.MetadataHold},
		{"export", record.MetadataExport},
		{"administrator visibility", record.AdminVisibility},
		{"tenant visibility", record.TenantVisibility},
		{"abuse handling", record.AbuseHandling},
		{"governance inheritance", record.GovernanceInheritance},
	} {
		if strings.TrimSpace(section.value) == "" {
			t.Fatalf("the record leaves %s unanswered", section.name)
		}
	}
	rendered := RenderCallPrivacy()
	for _, phrase := range []string{
		"DTLS-SRTP", "SFrame",
		"announce", "continuously", "decline",
		"works council", "California", "Germany",
		"CHATVOICE-005",
		"eavesdropping at a relay", "silently recording", "compromised forwarding server", "call injection", "address disclosure",
	} {
		if !strings.Contains(rendered, phrase) {
			t.Fatalf("record does not pin %q", phrase)
		}
	}
}

// TestTodo_CHATCALL_005_Security holds the threat model to exactly one
// answer per threat — a mitigation or an accepted risk, never both, never
// neither — and asserts the record's hard rules: end-to-end encryption
// forbids server media features, and metadata never carries content.
func TestTodo_CHATCALL_005_Security(t *testing.T) {
	record := CallPrivacyRecord()
	if len(record.Threats) != 5 {
		t.Fatalf("threats = %d, want 5", len(record.Threats))
	}
	wantThreats := []string{
		"eavesdropping at a relay",
		"a participant silently recording with their own tools",
		"a compromised forwarding server",
		"call injection",
		"address disclosure",
	}
	for i, want := range wantThreats {
		threat := record.Threats[i]
		if threat.Threat != want {
			t.Fatalf("threat %d = %q, want %q", i, threat.Threat, want)
		}
		mitigated := strings.TrimSpace(threat.Mitigation) != ""
		accepted := strings.TrimSpace(threat.AcceptedRisk) != ""
		if mitigated == accepted {
			t.Fatalf("threat %q must carry exactly one of mitigation or accepted risk: %+v", want, threat)
		}
	}
	for _, row := range record.EndToEndByKind {
		if !row.EndToEnd {
			continue
		}
		if row.ServerCaptions || row.ServerRecording || row.ServerAgents {
			t.Fatalf("kind %q claims end-to-end encryption while keeping server media features: %+v", row.CallKind, row)
		}
	}
	if !strings.Contains(record.GovernanceInheritance, "fenced") || !strings.Contains(record.GovernanceInheritance, "never the audio") {
		t.Fatal("governance inheritance does not carry the CHATVOICE-005 transcript-only fence")
	}
}

func TestTodo_CHATCALL_005_Golden(t *testing.T) {
	const digest = "a292bb18754a7846169b5cf605320aa8245787c014bf3aed0b700d03ffd772fe"
	if got := strings.Count(RenderCallPrivacy(), "\n"); got < 20 {
		t.Fatalf("record has %d lines", got)
	}
	if CallPrivacyDigest() != digest {
		t.Fatalf("the record changed; digest is now %s", CallPrivacyDigest())
	}
}
