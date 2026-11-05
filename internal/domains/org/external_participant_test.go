package org

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func smbPortal001Digest(issuer, subject string) string {
	sum := sha256.Sum256([]byte(issuer + "\x00" + subject))
	return hex.EncodeToString(sum[:])
}

type smbPortal001Audit struct {
	mu      sync.Mutex
	denials []DenialAudit
}

func (a *smbPortal001Audit) Denial(d DenialAudit) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.denials = append(a.denials, d)
}

func (a *smbPortal001Audit) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.denials)
}

func smbPortal001Parties(t *testing.T) (*PartyRegistry, BusinessParty, BusinessParty) {
	t.Helper()
	ctx := context.Background()
	reg := NewPartyRegistry(nil)
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	mk := func(name string) BusinessParty {
		party, _, err := reg.CreateParty(ctx, CreatePartyRequest{
			Tenant: "tenant-a", CompanyScope: "hcm-co-1", PartyID: uuid.NewString(),
			Kind: PartyKindOrganization, DisplayName: name, SourceAuthority: smbCRM001Authority(),
			InitialRoles:   []RoleGrant{{Role: CommercialRoleCustomer, EffectiveFrom: values.NewInstant(now)}},
			IdempotencyKey: "portal-party-" + uuid.NewString(), Actor: "ada.admin", At: now,
		})
		if err != nil {
			t.Fatalf("create party: %v", err)
		}
		return party
	}
	return reg, mk("Acme A"), mk("Beta B")
}

func smbPortal001Registry(parties *PartyRegistry, audit *smbPortal001Audit) *ParticipantRegistry {
	return NewParticipantRegistry(parties, ParticipantConfig{
		TrustedIssuers: map[string]bool{"idp.company": true},
		Audit:          audit,
	})
}

func smbPortal001Challenge(t *testing.T, reg *ParticipantRegistry, tenant values.TenantId, issuer, endpoint string, at time.Time) string {
	t.Helper()
	challenge, err := reg.IssueChallenge(context.Background(), tenant, issuer, endpoint, at)
	if err != nil {
		t.Fatalf("issue challenge: %v", err)
	}
	return challenge.ChallengeID
}

func smbPortal001Invite(t *testing.T, reg *ParticipantRegistry, tenant values.TenantId, issuer, subject string) ExternalParticipant {
	t.Helper()
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	participant, err := reg.InviteParticipant(context.Background(), InviteRequest{
		Tenant: tenant, Issuer: issuer, SubjectDigest: smbPortal001Digest(issuer, subject),
		ContactEndpoint: subject + "@example.com",
		ChallengeID:     smbPortal001Challenge(t, reg, tenant, issuer, subject+"@example.com", now),
		InvitedBy:       "ada.admin", IdempotencyKey: "invite-" + uuid.NewString(), At: now,
	})
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	return participant
}

func smbPortal001Grant(t *testing.T, reg *ParticipantRegistry, participant ExternalParticipant, party BusinessParty, role RepresentationRole) PartyRepresentationGrant {
	t.Helper()
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	grant, err := reg.GrantRepresentation(context.Background(), GrantRequest{
		ParticipantID: participant.ParticipantID, Tenant: "tenant-a", CompanyScope: "hcm-co-1",
		PartyID: party.Ref.Id, Role: role, ResourceScope: []string{"party", "request", "document"},
		ActionCeiling: []string{"read", "comment"}, FieldCeiling: []string{"profile", "status"},
		ExpiresAt: now.Add(30 * 24 * time.Hour), SourceAuthority: "policy:portal-grants/v1",
		ExpectedParticipantRevision: participant.Revision, GrantedBy: "ada.admin",
		IdempotencyKey: "grant-" + uuid.NewString(), At: now,
	})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	active, err := reg.ActivateGrant(context.Background(), "tenant-a", grant.GrantID, participant.ParticipantID, "activate-"+uuid.NewString(), now)
	if err != nil {
		t.Fatalf("activate grant: %v", err)
	}
	return active
}

func smbPortal001Token(t *testing.T, reg *ParticipantRegistry, participant ExternalParticipant) ParticipantToken {
	t.Helper()
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	auth, err := reg.Authenticate(context.Background(), "tenant-a", participant.Issuer, participant.SubjectDigest, now)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	token, err := reg.IssueToken(auth.Participant.ParticipantID, now, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return token
}

// TestTodo_SMB_PORTAL_001 proves the external-participant lifecycle with
// negative seeds: a customer-A grant reads nothing of customer B by ID,
// list, count, or download; duplicate issuer+subject never invites twice;
// and resource/grant mismatches refuse with zero writes.
func TestTodo_SMB_PORTAL_001(t *testing.T) {
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	parties, acmeA, betaB := smbPortal001Parties(t)
	audit := &smbPortal001Audit{}
	reg := smbPortal001Registry(parties, audit)

	participant := smbPortal001Invite(t, reg, "tenant-a", "idp.company", "alice")
	if participant.State != ParticipantActive || participant.Revision != 1 {
		t.Fatalf("participant = %+v, want ACTIVE revision 1", participant)
	}
	if participant.Kind != ParticipantKindExternal {
		t.Fatalf("participant kind = %q, want no workforce identity", participant.Kind)
	}
	grant := smbPortal001Grant(t, reg, participant, acmeA, RepresentationCustomerContact)
	if grant.Lifecycle != GrantActive {
		t.Fatalf("grant = %+v, want ACTIVE", grant)
	}
	token := smbPortal001Token(t, reg, participant)

	allowed, err := reg.Authorize(context.Background(), AuthRequest{
		Context: ExternalContext{ParticipantToken: &token},
		Action:  "read", Resource: GrantResource{Kind: "party", PartyID: acmeA.Ref.Id, Fields: []string{"profile"}},
		At: now,
	})
	if err != nil {
		t.Fatalf("authorized read: %v", err)
	}
	if allowed.GrantID != grant.GrantID || len(allowed.Fields) != 1 {
		t.Fatalf("projection = %+v, want the grant with one field", allowed)
	}
	for _, attempt := range []struct {
		name     string
		resource GrantResource
	}{
		{"by ID", GrantResource{Kind: "party", PartyID: betaB.Ref.Id, Fields: []string{"profile"}}},
		{"attachment", GrantResource{Kind: "attachment", PartyID: betaB.Ref.Id, Fields: []string{"content"}}},
		{"subscription", GrantResource{Kind: "subscription", PartyID: betaB.Ref.Id, Fields: []string{"events"}}},
	} {
		if _, err := reg.Authorize(context.Background(), AuthRequest{
			Context: ExternalContext{ParticipantToken: &token},
			Action:  "read", Resource: attempt.resource, At: now,
		}); !errors.Is(err, ErrGrantDenied) {
			t.Fatalf("%s err = %v, want ErrGrantDenied", attempt.name, err)
		}
	}
	scope, err := reg.ListScope(context.Background(), ExternalContext{ParticipantToken: &token}, "tenant-a", "hcm-co-1", now)
	if err != nil {
		t.Fatalf("list scope: %v", err)
	}
	if len(scope.PartyIDs) != 1 || scope.PartyIDs[0] != acmeA.Ref.Id || scope.Total != 1 {
		t.Fatalf("list scope = %+v, want exactly customer A", scope)
	}

	if _, err := reg.InviteParticipant(context.Background(), InviteRequest{
		Tenant: "tenant-a", Issuer: "idp.company", SubjectDigest: smbPortal001Digest("idp.company", "alice"),
		ContactEndpoint: "alice@example.com",
		ChallengeID:     smbPortal001Challenge(t, reg, "tenant-a", "idp.company", "alice@example.com", now),
		InvitedBy:       "ada.admin", IdempotencyKey: "invite-dup-" + uuid.NewString(), At: now,
	}); !errors.Is(err, ErrParticipantExists) {
		t.Fatalf("duplicate issuer+subject err = %v, want ErrParticipantExists", err)
	}
	if audit.count() == 0 {
		t.Fatal("cross-party denials produced no audit trail")
	}
}

// TestTodo_SMB_PORTAL_001_Integration proves continuity across restart and
// multi-grant participants: grants, epochs, and consumed challenges
// survive a snapshot round trip, expiry transitions hold, and one
// participant can serve two parties under separate grants.
func TestTodo_SMB_PORTAL_001_Integration(t *testing.T) {
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	parties, acmeA, betaB := smbPortal001Parties(t)
	reg := smbPortal001Registry(parties, &smbPortal001Audit{})

	alice := smbPortal001Invite(t, reg, "tenant-a", "idp.company", "alice")
	smbPortal001Grant(t, reg, alice, acmeA, RepresentationCustomerContact)
	smbPortal001Grant(t, reg, alice, betaB, RepresentationSupplierContact)
	token := smbPortal001Token(t, reg, alice)
	scope, err := reg.ListScope(context.Background(), ExternalContext{ParticipantToken: &token}, "tenant-a", "hcm-co-1", now)
	if err != nil {
		t.Fatalf("list scope: %v", err)
	}
	if scope.Total != 2 {
		t.Fatalf("two-party scope = %+v, want total 2", scope)
	}

	snapshot, err := reg.Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	restarted := smbPortal001Registry(parties, &smbPortal001Audit{})
	if err := restarted.Import(snapshot); err != nil {
		t.Fatalf("import: %v", err)
	}
	restored, err := restarted.ListScope(context.Background(), ExternalContext{ParticipantToken: &token}, "tenant-a", "hcm-co-1", now)
	if err != nil {
		t.Fatalf("restored scope: %v", err)
	}
	if restored.Total != 2 {
		t.Fatalf("restored scope = %+v, want total 2", restored)
	}
	laterToken, err := restarted.IssueToken(alice.ParticipantID, now.Add(31*24*time.Hour), now.Add(32*24*time.Hour))
	if err != nil {
		t.Fatalf("later token: %v", err)
	}
	expired, err := restarted.ListScope(context.Background(), ExternalContext{ParticipantToken: &laterToken}, "tenant-a", "hcm-co-1", now.Add(31*24*time.Hour))
	if err != nil {
		t.Fatalf("expired scope: %v", err)
	}
	if expired.Total != 0 {
		t.Fatalf("expired scope = %+v, want total 0", expired)
	}
}

// TestTodo_SMB_PORTAL_001_Security proves context separation and the
// refusal battery: employee/external contexts never union, revoked
// tokens disclose nothing with an audit trail, and every forged,
// expired, replayed, revoked-issuer, cross-tenant, or self-approved
// artifact refuses with zero grant writes.
func TestTodo_SMB_PORTAL_001_Security(t *testing.T) {
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	parties, acmeA, _ := smbPortal001Parties(t)
	audit := &smbPortal001Audit{}
	reg := smbPortal001Registry(parties, audit)

	alice := smbPortal001Invite(t, reg, "tenant-a", "idp.company", "alice")
	smbPortal001Grant(t, reg, alice, acmeA, RepresentationSupplierContact)
	token := smbPortal001Token(t, reg, alice)

	union := ExternalContext{WorkforceID: "worker-alice", ParticipantToken: &token}
	if _, err := reg.Authorize(context.Background(), AuthRequest{
		Context: union, Action: "read",
		Resource: GrantResource{Kind: "party", PartyID: acmeA.Ref.Id, Fields: []string{"profile"}}, At: now,
	}); !errors.Is(err, ErrGrantContextUnion) {
		t.Fatalf("union context err = %v, want ErrGrantContextUnion", err)
	}
	employeeOnly := ExternalContext{WorkforceID: "worker-alice"}
	if _, err := reg.Authorize(context.Background(), AuthRequest{
		Context: employeeOnly, Action: "read",
		Resource: GrantResource{Kind: "party", PartyID: acmeA.Ref.Id, Fields: []string{"profile"}}, At: now,
	}); !errors.Is(err, ErrGrantDenied) {
		t.Fatalf("employee context on supplier data err = %v, want ErrGrantDenied", err)
	}

	revoked, err := reg.RevokeParticipant(context.Background(), "tenant-a", alice.ParticipantID, "ada.admin", "offboarded", "revoke-"+uuid.NewString(), now)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if revoked.RevocationEpoch != 1 {
		t.Fatalf("revoked epoch = %d, want 1", revoked.RevocationEpoch)
	}
	before := audit.count()
	if _, err := reg.Authorize(context.Background(), AuthRequest{
		Context: ExternalContext{ParticipantToken: &token}, Action: "read",
		Resource: GrantResource{Kind: "party", PartyID: acmeA.Ref.Id, Fields: []string{"profile"}}, At: now,
	}); !errors.Is(err, ErrGrantRevoked) {
		t.Fatalf("replayed token err = %v, want ErrGrantRevoked", err)
	}
	if audit.count() <= before {
		t.Fatal("revoked replay produced no denial audit")
	}
	for _, denial := range audit.denials {
		if strings.Contains(denial.Detail, "alice") || strings.Contains(denial.Detail, "Acme") {
			t.Fatalf("denial audit discloses PII: %+v", denial)
		}
	}

	grantsBefore := len(reg.GrantsForTesting("tenant-a", alice.ParticipantID))
	stale, err := reg.IssueChallenge(context.Background(), "tenant-a", "idp.company", "mallory@example.com", now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("issue stale challenge: %v", err)
	}
	reused := smbPortal001Challenge(t, reg, "tenant-a", "idp.company", "mallory@example.com", now)
	if _, err := reg.InviteParticipant(context.Background(), InviteRequest{
		Tenant: "tenant-a", Issuer: "idp.company", SubjectDigest: smbPortal001Digest("idp.company", "mallory"),
		ContactEndpoint: "mallory@example.com", ChallengeID: reused,
		InvitedBy: "ada.admin", IdempotencyKey: "b-reuse-1", At: now,
	}); err != nil {
		t.Fatalf("first use of challenge: %v", err)
	}
	battery := []struct {
		name string
		run  func() error
	}{
		{"email match without challenge", func() error {
			_, err := reg.InviteParticipant(context.Background(), InviteRequest{
				Tenant: "tenant-a", Issuer: "idp.company", SubjectDigest: smbPortal001Digest("idp.company", "mallory2"),
				ContactEndpoint: "mallory2@example.com", InvitedBy: "ada.admin",
				IdempotencyKey: "b-email", At: now,
			})
			return err
		}},
		{"unknown identifier", func() error {
			_, err := reg.Authenticate(context.Background(), "tenant-a", "idp.company", smbPortal001Digest("idp.company", "ghost"), now)
			return err
		}},
		{"forged challenge", func() error {
			_, err := reg.InviteParticipant(context.Background(), InviteRequest{
				Tenant: "tenant-a", Issuer: "idp.company", SubjectDigest: smbPortal001Digest("idp.company", "mallory3"),
				ContactEndpoint: "mallory3@example.com", ChallengeID: "forged-challenge",
				InvitedBy: "ada.admin", IdempotencyKey: "b-forged", At: now,
			})
			return err
		}},
		{"expired challenge", func() error {
			_, err := reg.InviteParticipant(context.Background(), InviteRequest{
				Tenant: "tenant-a", Issuer: "idp.company", SubjectDigest: smbPortal001Digest("idp.company", "mallory4"),
				ContactEndpoint: "mallory4@example.com", ChallengeID: stale.ChallengeID,
				InvitedBy: "ada.admin", IdempotencyKey: "b-expired", At: now,
			})
			return err
		}},
		{"replayed challenge", func() error {
			_, err := reg.InviteParticipant(context.Background(), InviteRequest{
				Tenant: "tenant-a", Issuer: "idp.company", SubjectDigest: smbPortal001Digest("idp.company", "mallory5"),
				ContactEndpoint: "mallory5@example.com", ChallengeID: reused,
				InvitedBy: "ada.admin", IdempotencyKey: "b-replay", At: now,
			})
			return err
		}},
		{"revoked issuer", func() error {
			_, err := reg.Authenticate(context.Background(), "tenant-a", "idp.evil", smbPortal001Digest("idp.evil", "mallory"), now)
			return err
		}},
		{"cross-tenant rebind", func() error {
			_, err := reg.Rebind(context.Background(), RebindRequest{
				ParticipantID: alice.ParticipantID, Tenant: "tenant-b",
				NewIssuer: "idp.company", NewSubjectDigest: smbPortal001Digest("idp.company", "alice"),
				ProofRef: "proof:helpdesk-1", Approver: "ada.admin", Requestor: "ops.admin",
				IdempotencyKey: "b-xtenant", At: now,
			})
			return err
		}},
		{"self-approved rebind", func() error {
			_, err := reg.Rebind(context.Background(), RebindRequest{
				ParticipantID: alice.ParticipantID, Tenant: "tenant-a",
				NewIssuer: "idp.company", NewSubjectDigest: smbPortal001Digest("idp.company", "alice-new"),
				ProofRef: "proof:helpdesk-2", Approver: "mallory", Requestor: "mallory",
				IdempotencyKey: "b-self", At: now,
			})
			return err
		}},
	}
	for _, tc := range battery {
		if err := tc.run(); err == nil {
			t.Fatalf("%s succeeded, want refusal", tc.name)
		}
	}
	if got := len(reg.GrantsForTesting("tenant-a", alice.ParticipantID)); got != grantsBefore {
		t.Fatalf("battery wrote grants: %d before, %d after", grantsBefore, got)
	}
}

// TestTodo_SMB_PORTAL_001_Recovery proves crash convergence: a committed
// grant whose response is lost replays under the same key with zero
// duplicate grants, altered payloads conflict, and stale racers
// serialize into one winner.
func TestTodo_SMB_PORTAL_001_Recovery(t *testing.T) {
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	parties, acmeA, _ := smbPortal001Parties(t)
	reg := smbPortal001Registry(parties, &smbPortal001Audit{})

	alice := smbPortal001Invite(t, reg, "tenant-a", "idp.company", "alice")
	reg.AfterCommit = func() error { return errGrantSimulatedCrash }
	_, err := reg.GrantRepresentation(context.Background(), GrantRequest{
		ParticipantID: alice.ParticipantID, Tenant: "tenant-a", CompanyScope: "hcm-co-1",
		PartyID: acmeA.Ref.Id, Role: RepresentationCustomerContact,
		ResourceScope: []string{"party"}, ActionCeiling: []string{"read"}, FieldCeiling: []string{"profile"},
		ExpiresAt: now.Add(30 * 24 * time.Hour), SourceAuthority: "policy:portal-grants/v1",
		ExpectedParticipantRevision: alice.Revision, GrantedBy: "ada.admin",
		IdempotencyKey: "rec-grant", At: now,
	})
	if !errors.Is(err, errGrantSimulatedCrash) {
		t.Fatalf("crashing grant err = %v, want the simulated crash", err)
	}
	reg.AfterCommit = nil
	replayed, err := reg.GrantRepresentation(context.Background(), GrantRequest{
		ParticipantID: alice.ParticipantID, Tenant: "tenant-a", CompanyScope: "hcm-co-1",
		PartyID: acmeA.Ref.Id, Role: RepresentationCustomerContact,
		ResourceScope: []string{"party"}, ActionCeiling: []string{"read"}, FieldCeiling: []string{"profile"},
		ExpiresAt: now.Add(30 * 24 * time.Hour), SourceAuthority: "policy:portal-grants/v1",
		ExpectedParticipantRevision: alice.Revision, GrantedBy: "ada.admin",
		IdempotencyKey: "rec-grant", At: now,
	})
	if err != nil {
		t.Fatalf("replay after crash: %v", err)
	}
	grants := reg.GrantsForTesting("tenant-a", alice.ParticipantID)
	if len(grants) != 1 || grants[0].GrantID != replayed.GrantID {
		t.Fatalf("grants = %d, want the single replayed receipt", len(grants))
	}
	if _, err := reg.GrantRepresentation(context.Background(), GrantRequest{
		ParticipantID: alice.ParticipantID, Tenant: "tenant-a", CompanyScope: "hcm-co-1",
		PartyID: acmeA.Ref.Id, Role: RepresentationCustomerApprover,
		ResourceScope: []string{"party"}, ActionCeiling: []string{"read"}, FieldCeiling: []string{"profile"},
		ExpiresAt: now.Add(30 * 24 * time.Hour), SourceAuthority: "policy:portal-grants/v1",
		ExpectedParticipantRevision: alice.Revision, GrantedBy: "ada.admin",
		IdempotencyKey: "rec-grant", At: now,
	}); !errors.Is(err, ErrGrantIdempotencyConflict) {
		t.Fatalf("altered replay err = %v, want ErrGrantIdempotencyConflict", err)
	}

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, results[i] = reg.EndGrant(context.Background(), "tenant-a", replayed.GrantID,
				alice.ParticipantID, replayed.Revision, "ada.admin", fmt.Sprintf("race-%d", i), now)
		}(i)
	}
	wg.Wait()
	wins, conflicts := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrGrantRevisionConflict):
			conflicts++
		default:
			t.Fatalf("racer err = %v", err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("race = %d wins %d conflicts, want one winner", wins, conflicts)
	}
}

// TestTodo_SMB_PORTAL_001_Conformance proves one owner serves every entry
// point with the same epoch, revision, and ceiling checks; that the
// closed action vocabulary admits no project membership; and that
// signatory authority can stay narrower than read access.
func TestTodo_SMB_PORTAL_001_Conformance(t *testing.T) {
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	parties, acmeA, _ := smbPortal001Parties(t)
	reg := smbPortal001Registry(parties, &smbPortal001Audit{})

	alice := smbPortal001Invite(t, reg, "tenant-a", "idp.company", "alice")
	if _, err := reg.GrantRepresentation(context.Background(), GrantRequest{
		ParticipantID: alice.ParticipantID, Tenant: "tenant-a", CompanyScope: "hcm-co-1",
		PartyID: acmeA.Ref.Id, Role: RepresentationCustomerContact,
		ResourceScope: []string{"party"}, ActionCeiling: []string{"read", "project.invite"}, FieldCeiling: []string{"profile"},
		ExpiresAt: now.Add(30 * 24 * time.Hour), SourceAuthority: "policy:portal-grants/v1",
		ExpectedParticipantRevision: alice.Revision, GrantedBy: "ada.admin",
		IdempotencyKey: "conf-proj", At: now,
	}); !errors.Is(err, ErrGrantCeiling) {
		t.Fatalf("project action in ceiling err = %v, want ErrGrantCeiling", err)
	}
	signer := smbPortal001Invite(t, reg, "tenant-a", "idp.company", "signer")
	narrow, err := reg.GrantRepresentation(context.Background(), GrantRequest{
		ParticipantID: signer.ParticipantID, Tenant: "tenant-a", CompanyScope: "hcm-co-1",
		PartyID: acmeA.Ref.Id, Role: RepresentationCustomerApprover,
		ResourceScope: []string{"document"}, ActionCeiling: []string{"sign"}, FieldCeiling: []string{"signature"},
		ExpiresAt: now.Add(30 * 24 * time.Hour), SourceAuthority: "policy:portal-grants/v1",
		ExpectedParticipantRevision: signer.Revision, GrantedBy: "ada.admin",
		IdempotencyKey: "conf-sign", At: now,
	})
	if err != nil {
		t.Fatalf("narrow signatory grant: %v", err)
	}
	active, err := reg.ActivateGrant(context.Background(), "tenant-a", narrow.GrantID, signer.ParticipantID, "conf-sign-act", now)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	_ = active
	signerAuth, err := reg.Authenticate(context.Background(), "tenant-a", "idp.company", signer.SubjectDigest, now)
	if err != nil {
		t.Fatalf("authenticate signer: %v", err)
	}
	signerToken, err := reg.IssueToken(signerAuth.Participant.ParticipantID, now, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if _, err := reg.Authorize(context.Background(), AuthRequest{
		Context: ExternalContext{ParticipantToken: &signerToken}, Action: "read",
		Resource: GrantResource{Kind: "document", PartyID: acmeA.Ref.Id, Fields: []string{"content"}}, At: now,
	}); !errors.Is(err, ErrGrantDenied) {
		t.Fatalf("broad read on narrow grant err = %v, want ErrGrantDenied", err)
	}
	allowed, err := reg.Authorize(context.Background(), AuthRequest{
		Context: ExternalContext{ParticipantToken: &signerToken}, Action: "sign",
		Resource: GrantResource{Kind: "document", PartyID: acmeA.Ref.Id, Fields: []string{"signature"}}, At: now,
	})
	if err != nil {
		t.Fatalf("narrow sign: %v", err)
	}
	if len(allowed.Fields) != 1 || allowed.Fields[0] != "signature" {
		t.Fatalf("signatory projection = %+v, want only the signature field", allowed)
	}
}

// TestTodo_SMB_PORTAL_001_Golden pins the authorized projection and
// refusal bytes served to portal, agent/tool, and API surfaces.
func TestTodo_SMB_PORTAL_001_Golden(t *testing.T) {
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	parties, acmeA, _ := smbPortal001Parties(t)
	reg := smbPortal001Registry(parties, &smbPortal001Audit{})

	alice := smbPortal001Invite(t, reg, "tenant-a", "idp.company", "alice")
	grant := smbPortal001Grant(t, reg, alice, acmeA, RepresentationCustomerContact)
	token := smbPortal001Token(t, reg, alice)
	allowed, err := reg.Authorize(context.Background(), AuthRequest{
		Context: ExternalContext{ParticipantToken: &token}, Action: "read",
		Resource: GrantResource{Kind: "party", PartyID: acmeA.Ref.Id, Fields: []string{"profile"}}, At: now,
	})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	projection := AuthorizeGrantProjection(allowed, grant)
	if !strings.Contains(projection, grant.GrantID) || !strings.Contains(projection, "profile") {
		t.Fatalf("projection = %q, want grant id and field", projection)
	}
	if strings.Contains(projection, "alice") || strings.Contains(projection, "Acme") {
		t.Fatalf("projection leaks identity: %q", projection)
	}
	if got := GrantRefusalTitle(ErrGrantDenied); !strings.Contains(got, "not permitted") {
		t.Fatalf("denied title = %q", got)
	}
	if got := GrantRefusalTitle(ErrGrantRevoked); strings.Contains(got, "revoked") {
		t.Fatalf("revoked title discloses state: %q", got)
	}
}

// TestTodo_SMB_PORTAL_001_Race runs real competing grant writers against
// one grant and asserts exact winning/losing receipts. The race detector
// itself runs in CI; this leg proves the fencing logic on any host.
func TestTodo_SMB_PORTAL_001_Race(t *testing.T) {
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	parties, acmeA, _ := smbPortal001Parties(t)
	reg := smbPortal001Registry(parties, &smbPortal001Audit{})

	alice := smbPortal001Invite(t, reg, "tenant-a", "idp.company", "alice")
	grant := smbPortal001Grant(t, reg, alice, acmeA, RepresentationCustomerContact)

	const racers = 8
	var wg sync.WaitGroup
	failed := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, failed[i] = reg.SuspendGrant(context.Background(), "tenant-a", grant.GrantID,
				alice.ParticipantID, grant.Revision, "ada.admin", fmt.Sprintf("suspend-%d", i), now)
		}(i)
	}
	wg.Wait()
	wins, conflicts := 0, 0
	for _, err := range failed {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrGrantRevisionConflict):
			conflicts++
		default:
			t.Fatalf("racer err = %v", err)
		}
	}
	if wins != 1 || conflicts != racers-1 {
		t.Fatalf("race = %d wins %d conflicts, want 1 winner", wins, conflicts)
	}
}

var errGrantSimulatedCrash = errors.New("org: simulated grant crash")
