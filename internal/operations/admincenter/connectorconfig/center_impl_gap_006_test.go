package connectorconfig

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/fakeincumbent"
)

const gap006Tenant = "5e3f1c2b-0000-4000-8000-000000000001"

func gap006Connection(t *testing.T) *connectivity.ConnectorConnection {
	t.Helper()
	def := fakeincumbent.DefaultDefinition()
	reg := connectivity.NewRegistry()
	pub, err := reg.Publish(def, connectivity.PublicationMeta{PublishedBy: "user:platform@hcmnext", PublishedAt: time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("publish definition: %v", err)
	}
	ref, err := connectivity.ParseCredentialRef("secretref://harborcare/workday/client")
	if err != nil {
		t.Fatalf("parse credential: %v", err)
	}
	conn, err := connectivity.NewConnection(pub, connectivity.ConnectionSpec{
		ConnectionID:     "conn-harborcare-workday",
		TenantID:         gap006Tenant,
		OrgID:            "org-harborcare-us",
		SystemID:         "sys-workday-prod",
		Environment:      connectivity.EnvironmentProduction,
		Residency:        "us-east",
		ConnectorID:      def.ConnectorID,
		ConnectorVersion: def.Version,
		AuthMode:         connectivity.AuthOAuth2ClientCredentials,
		CredentialRef:    ref,
		Scopes:           []string{"worker.read"},
		EndpointPolicy: connectivity.EndpointPolicy{
			AllowedHosts:  []string{"api.workday.example"},
			RequireTLS:    true,
			EgressProfile: "cell-egress/us-east",
		},
		Capabilities: connectivity.ReadCapabilities(connectivity.ObjectKinds()...),
		Bounds:       def.Bounds,
		CreatedAt:    time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("new connection: %v", err)
	}
	return conn
}

var gap006Scope = Scope{TenantID: gap006Tenant, Environment: string(connectivity.EnvironmentProduction)}

// TestTodo_IMPL_GAP_006 is the primary regression: a subject-denied status
// view must carry zero evidence references, whatever the field rulings say.
//
// RED: BuildStatus seeded EvidenceRefs before the subject-denied early return,
// so a withheld view still disclosed nonpublic evidence IDs.
// GREEN: denial returns withheld with no evidence; allowed views keep only a
// cloned copy of evidence the field ruling authorises.
func TestTodo_IMPL_GAP_006(t *testing.T) {
	t.Parallel()
	conn := gap006Connection(t)
	evidence := []EvidenceRef{{ID: "evidence:private-sync", Kind: "diagnostic"}}

	t.Run("subject denied withholds evidence even when field evidence is granted", func(t *testing.T) {
		t.Parallel()
		d := &Decision{SubjectDisclosable: false, Fields: map[Field]bool{FieldEvidence: true}}
		v, err := BuildStatus(conn, gap006Scope, d, evidence)
		if err != nil {
			t.Fatalf("BuildStatus: %v", err)
		}
		if !v.Withheld {
			t.Fatal("Withheld=false, want true")
		}
		if len(v.EvidenceRefs) != 0 {
			t.Fatalf("EvidenceRefs=%v, want none for denied subject", v.EvidenceRefs)
		}
	})

	t.Run("subject allowed without field evidence returns no evidence", func(t *testing.T) {
		t.Parallel()
		d := &Decision{SubjectDisclosable: true, Fields: map[Field]bool{FieldStatus: true}}
		v, err := BuildStatus(conn, gap006Scope, d, evidence)
		if err != nil {
			t.Fatalf("BuildStatus: %v", err)
		}
		if v.Withheld {
			t.Fatal("Withheld=true, want false")
		}
		if len(v.EvidenceRefs) != 0 {
			t.Fatalf("EvidenceRefs=%v, want none without field evidence", v.EvidenceRefs)
		}
	})

	t.Run("subject allowed with field evidence returns an independent copy", func(t *testing.T) {
		t.Parallel()
		src := []EvidenceRef{{ID: "evidence:private-sync", Kind: "diagnostic"}}
		d := &Decision{SubjectDisclosable: true, Fields: map[Field]bool{FieldEvidence: true}}
		v, err := BuildStatus(conn, gap006Scope, d, src)
		if err != nil {
			t.Fatalf("BuildStatus: %v", err)
		}
		if len(v.EvidenceRefs) != 1 || v.EvidenceRefs[0] != src[0] {
			t.Fatalf("EvidenceRefs=%v, want the authorised evidence", v.EvidenceRefs)
		}
		src[0].ID = "mutated"
		if v.EvidenceRefs[0].ID != "evidence:private-sync" {
			t.Fatalf("returned view aliases source evidence: %v", v.EvidenceRefs)
		}
	})

	t.Run("scope mismatch still fails closed", func(t *testing.T) {
		t.Parallel()
		d := &Decision{SubjectDisclosable: true, Fields: map[Field]bool{FieldEvidence: true}}
		if _, err := BuildStatus(conn, Scope{TenantID: "other-tenant"}, d, evidence); err == nil {
			t.Fatal("tenant mismatch returned no error")
		}
		if _, err := BuildStatus(conn, Scope{Environment: "SANDBOX"}, d, evidence); err == nil {
			t.Fatal("environment mismatch returned no error")
		}
	})
}
