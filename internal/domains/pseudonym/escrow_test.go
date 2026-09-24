package pseudonym_test

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/pseudonym"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

type escrowTestProvider struct {
	mu      sync.Mutex
	decrypt []custody.Handle
}

func (p *escrowTestProvider) Encrypt(ctx custody.Context, object custody.Handle, plaintext []byte) (custody.Ciphertext, custody.Receipt, error) {
	if err := ctx.Validate(); err != nil {
		return custody.Ciphertext{}, custody.Receipt{}, err
	}
	block, err := aes.NewCipher(testKey(object))
	if err != nil {
		return custody.Ciphertext{}, custody.Receipt{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return custody.Ciphertext{}, custody.Receipt{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	for i := range nonce {
		nonce[i] = byte(i + 1)
	}
	return custody.Ciphertext{Handle: object, Algorithm: "test-aes-gcm", Data: append(nonce, gcm.Seal(nil, nonce, plaintext, nil)...)}, custody.Receipt{ID: "encrypt", Handle: object, Operation: custody.Encrypt, At: testNow, ContextDigest: custody.ContextDigest(ctx.RequestContext)}, nil
}

func (p *escrowTestProvider) Decrypt(ctx custody.Context, object custody.Handle, sealed custody.Ciphertext) ([]byte, custody.Receipt, error) {
	if err := ctx.Validate(); err != nil {
		return nil, custody.Receipt{}, err
	}
	p.mu.Lock()
	p.decrypt = append(p.decrypt, object)
	p.mu.Unlock()
	if sealed.Handle != object || len(sealed.Data) < 12 {
		return nil, custody.Receipt{}, errors.New("test provider: key binding failed")
	}
	block, err := aes.NewCipher(testKey(object))
	if err != nil {
		return nil, custody.Receipt{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, custody.Receipt{}, err
	}
	nonce, data := sealed.Data[:gcm.NonceSize()], sealed.Data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return nil, custody.Receipt{}, err
	}
	return plaintext, custody.Receipt{ID: "decrypt", Handle: object, Operation: custody.Decrypt, At: testNow, ContextDigest: custody.ContextDigest(ctx.RequestContext)}, nil
}

func (p *escrowTestProvider) Sign(custody.Context, custody.Handle, []byte) (custody.Signature, custody.Receipt, error) {
	return custody.Signature{}, custody.Receipt{}, errors.New("test provider: unsupported")
}
func (p *escrowTestProvider) Verify(custody.Context, custody.Handle, []byte, custody.Signature) (bool, custody.Receipt, error) {
	return false, custody.Receipt{}, errors.New("test provider: unsupported")
}
func (p *escrowTestProvider) IssueLease(custody.Context, custody.Handle, custody.Operation, time.Duration) (custody.Lease, error) {
	return custody.Lease{}, errors.New("test provider: unsupported")
}
func (p *escrowTestProvider) RenewLease(custody.Context, custody.Lease, time.Duration) (custody.Lease, error) {
	return custody.Lease{}, errors.New("test provider: unsupported")
}
func (p *escrowTestProvider) Rotate(custody.Context, custody.Handle) (custody.Handle, custody.Receipt, error) {
	return custody.Handle{}, custody.Receipt{}, errors.New("test provider: unsupported")
}
func (p *escrowTestProvider) Revoke(custody.Context, custody.Handle, string) (custody.Receipt, error) {
	return custody.Receipt{}, errors.New("test provider: unsupported")
}

type recordingDeriver struct {
	inner   custody.KeyDeriver
	allowed custody.Handle
	mu      sync.Mutex
	handles []custody.Handle
}

func (d *recordingDeriver) Derive(ctx custody.Context, handle custody.Handle, label []byte) (custody.DerivedValue, custody.Receipt, error) {
	d.mu.Lock()
	d.handles = append(d.handles, handle)
	d.mu.Unlock()
	if handle != d.allowed {
		return custody.DerivedValue{}, custody.Receipt{}, errors.New("test deriver: handle is not the derivation key")
	}
	return d.inner.Derive(ctx, handle, label)
}

type testRevelationAuthority struct {
	revoked      map[string]bool
	roleOverride string
}

func (a *testRevelationAuthority) ResolveRevelationApprover(_ custody.Context, d approval.ApprovalDecision) (pseudonym.VerifiedApprover, error) {
	if a.revoked[d.DecisionID] {
		return pseudonym.VerifiedApprover{}, errors.New("revoked")
	}
	principals := map[string]string{"decision:requester": "case-worker", "decision:custodian": "custodian-1", "decision:requester-alias": "case-worker", "decision:custodian-alias": "case-worker"}
	principal, ok := principals[d.DecisionID]
	if !ok {
		return pseudonym.VerifiedApprover{}, errors.New("unknown decision")
	}
	role := "requester"
	if d.DecisionID == "decision:custodian" {
		role = "privacy-custodian"
	}
	if a.roleOverride != "" && d.DecisionID == "decision:requester" {
		role = a.roleOverride
	}
	return pseudonym.VerifiedApprover{DecisionID: d.DecisionID, DecisionDigest: d.Digest(), PrincipalID: principal, Tenant: "tenant-1", Role: role}, nil
}

func testApproval(id, principal string, request pseudonym.RevelationRequest) approval.ApprovalDecision {
	request.RequestedBy = "case-worker"
	request.RequesterRole = "requester"
	request.Approver = "custodian-1"
	request.ApproverRole = "privacy-custodian"
	ref, err := pseudonym.RevelationApprovalReference(revelationPolicy(), request)
	if err != nil {
		panic(err)
	}
	return approval.ApprovalDecision{DecisionID: id, Binding: approval.DecisionBinding{ProposalDigest: ref}, Outcome: approval.OutcomeApproved, Approver: approval.ApproverReference{PrincipalID: principal}, AuthorityDecisionRef: "authz:" + id, DecidedAt: values.NewInstant(testNow)}
}

var testNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func testKey(handle custody.Handle) []byte {
	sum := sha256.Sum256([]byte(handle.ID + "\x00" + handle.Version + "\x00" + handle.Tenant + "\x00" + handle.Region))
	return sum[:]
}

func escrowHandles() (custody.Handle, custody.Handle, custody.Handle) {
	return custody.Handle{ID: "derive-key", Kind: custody.Key, Version: "v1", Tenant: "tenant-1", Region: "us-east-1"}, custody.Handle{ID: "escrow-key", Kind: custody.Key, Version: "v1", Tenant: "tenant-1", Region: "us-east-1"}, custody.Handle{ID: "tenant-kek", Kind: custody.Key, Version: "v1", Tenant: "tenant-1", Region: "us-east-1"}
}

func escrowService(t *testing.T) (*pseudonym.EscrowedService, *escrowTestProvider, *recordingDeriver, custody.Handle, custody.Handle, *testRevelationAuthority) {
	t.Helper()
	derivationKey, escrowKey, tenantKEK := escrowHandles()
	base := custody.NewInMemoryFake(func() time.Time { return testNow })
	if err := base.Register(derivationKey); err != nil {
		t.Fatal(err)
	}
	deriver := &recordingDeriver{inner: base, allowed: derivationKey}
	provider := &escrowTestProvider{}
	authority := &testRevelationAuthority{revoked: map[string]bool{}}
	service, err := pseudonym.NewEscrowedService(pseudonym.EscrowConfig{Deriver: deriver, Provider: provider, DerivationKey: derivationKey, EscrowKey: escrowKey, TenantKEKs: []custody.Handle{tenantKEK}, Clock: func() time.Time { return testNow }, ApprovalAuthority: authority})
	if err != nil {
		t.Fatal(err)
	}
	return service, provider, deriver, derivationKey, escrowKey, authority
}

func escrowContext() custody.Context {
	return custody.Context{RequestContext: custody.RequestContext{Workload: "case-worker", Tenant: "tenant-1", Region: "us-east-1", Purpose: "case-intake", Destination: "case"}}
}

// TestTodo_ANON_003 proves separate derivation and escrow custody, dual
// control, bounded purpose/evidence release, and digest-only release events.
func TestTodo_ANON_003(t *testing.T) {
	service, provider, deriver, derivationKey, escrowKey, _ := escrowService(t)
	ctx := escrowContext()
	p, err := service.Generate(ctx, pseudonym.GenerateRequest{Subject: "subject-123", Scope: "program-a", Purpose: "case-intake"})
	if err != nil {
		t.Fatal(err)
	}
	record, ok := service.Escrow().Record(p.ID)
	if !ok || record.Ciphertext.Handle != escrowKey {
		t.Fatalf("record = %+v, ok=%v; want escrow handle", record, ok)
	}
	if record.Ciphertext.Handle == derivationKey || record.Ciphertext.Handle.ID == "tenant-kek" {
		t.Fatalf("mapping is not separately custodied: %+v", record.Ciphertext.Handle)
	}
	if _, _, err := provider.Decrypt(ctx, derivationKey, record.Ciphertext); err == nil {
		t.Fatal("derivation key opened escrow ciphertext")
	}
	for _, handle := range deriver.handles {
		if handle != derivationKey {
			t.Fatalf("deriver saw non-derivation handle: %+v", handle)
		}
	}
	if _, _, err := deriver.Derive(ctx, escrowKey, []byte("escrow-must-not-derive")); err == nil {
		t.Fatal("escrow key derived a pseudonym value")
	}
	if _, _, err := service.Release(ctx, pseudonym.EscrowReleaseRequest{Pseudonym: p, RequestedBy: "case-worker", EscrowCustodian: "custodian-1", Purpose: "case-intake", TTL: time.Minute}); !errors.Is(err, pseudonym.ErrRevelationEvidenceRequired) {
		t.Fatalf("direct Release must refuse without revelation evidence, got %v", err)
	}
	evidence1 := mintRevelationEvidence(t, service, p, "legal:case-1")
	if _, _, err := service.ReleaseWithEvidence(ctx, governedRelease(p, evidence1), evidence1); err != nil {
		t.Fatal(err)
	}
	evidence2 := mintRevelationEvidence(t, service, p, "legal:case-2")
	subject, event, err := service.ReleaseWithEvidence(ctx, governedRelease(p, evidence2), evidence2)
	if err != nil || subject != "subject-123" || event.Digest == "" || event.Outcome != "released" {
		t.Fatalf("release = %q, %+v, %v", subject, event, err)
	}
	if len(service.Escrow().Events()) != 3 {
		t.Fatalf("events = %+v, want both release attempts", service.Escrow().Events())
	}
	if got := pseudonym.ExplainEscrow(); strings.Contains(got, "subject-123") || strings.Contains(got, "subject") && strings.Contains(got, "mapping") {
		t.Fatalf("ExplainEscrow carries mapping language: %q", got)
	}
}

// TestTodo_ANON_003_Race exercises concurrent release calls against one
// ciphertext record and verifies every successful release is evidenced.
func TestTodo_ANON_003_Race(t *testing.T) {
	service, _, _, _, _, _ := escrowService(t)
	ctx := escrowContext()
	p, err := service.Generate(ctx, pseudonym.GenerateRequest{Subject: "subject-123", Scope: "program-a", Purpose: "case-intake"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			evidence := mintRevelationEvidence(t, service, p, fmt.Sprintf("legal:case-%d", i))
			_, _, releaseErr := service.ReleaseWithEvidence(ctx, governedRelease(p, evidence), evidence)
			errs <- releaseErr
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := len(service.Escrow().Events()); got != 16 {
		t.Fatalf("event count = %d, want 16", got)
	}
}

// TestTodo_ANON_003_Security proves self-approval, missing evidence, excessive
// TTL, same-key construction, and tenant-key reuse are refused.
func TestTodo_REV_043_01(t *testing.T) {
	service, _, _, _, _, _ := escrowService(t)
	ctx := escrowContext()
	p, err := service.Generate(ctx, pseudonym.GenerateRequest{Subject: "subject-123", Scope: "program-a", Purpose: "case-intake"})
	if err != nil {
		t.Fatal(err)
	}
	request := revelationRequest(p)
	decision, err := service.AuthorizeRevelation(ctx, revelationPolicy(), request, testApproval("decision:requester", "case-worker", request), testApproval("decision:custodian", "custodian-1", request))
	if err != nil {
		t.Fatalf("approved distinct principals: %v", err)
	}
	release := governedRelease(p, decision.Evidence)
	if _, _, err := service.ReleaseWithEvidence(ctx, release, decision.Evidence); err != nil {
		t.Fatalf("valid release: %v", err)
	}
}

// TestTodo_REV_043_01_Security proves aliases and stale approvals cannot
// satisfy dual control at authorization or at the final decrypt boundary.
func TestTodo_REV_043_01_Security(t *testing.T) {
	service, _, _, _, _, authority := escrowService(t)
	ctx := escrowContext()
	p, err := service.Generate(ctx, pseudonym.GenerateRequest{Subject: "subject-123", Scope: "program-a", Purpose: "case-intake"})
	if err != nil {
		t.Fatal(err)
	}
	request := revelationRequest(p)
	boundRequester := testApproval("decision:requester", "case-worker", request)
	boundCustodian := testApproval("decision:custodian", "custodian-1", request)
	authority.roleOverride = "unknown-role"
	if _, err := service.AuthorizeRevelation(ctx, revelationPolicy(), request, boundRequester, boundCustodian); !errors.Is(err, pseudonym.ErrRevelationDenied) {
		t.Fatalf("unknown requester role accepted: %v", err)
	}
	authority.roleOverride = ""
	mutations := []struct {
		name  string
		apply func(*pseudonym.RevelationRequest)
	}{
		{"legal basis", func(r *pseudonym.RevelationRequest) { r.LegalBasisRef = "legal:other-case" }},
		{"fields", func(r *pseudonym.RevelationRequest) { r.Fields = []string{"other_field"} }},
		{"recipient", func(r *pseudonym.RevelationRequest) { r.Recipients = []string{"other-investigator"} }},
		{"generation", func(r *pseudonym.RevelationRequest) { r.Pseudonym.Generation++ }},
		{"expiry", func(r *pseudonym.RevelationRequest) { r.ExpiresAt = r.ExpiresAt.Add(time.Minute) }},
	}
	for _, mutation := range mutations {
		mutated := request
		mutated.Fields = append([]string(nil), request.Fields...)
		mutated.Recipients = append([]string(nil), request.Recipients...)
		mutation.apply(&mutated)
		if _, err := service.AuthorizeRevelation(ctx, revelationPolicy(), mutated, boundRequester, boundCustodian); !errors.Is(err, pseudonym.ErrRevelationDenied) {
			t.Fatalf("approval replayed after %s mutation: %v", mutation.name, err)
		}
	}
	_, err = service.AuthorizeRevelation(ctx, revelationPolicy(), request, testApproval("decision:requester-alias", "case-worker", request), testApproval("decision:custodian-alias", "case-worker", request))
	if !errors.Is(err, pseudonym.ErrRevelationDenied) {
		t.Fatalf("same principal under two approval IDs: %v", err)
	}
	forged := testApproval("decision:requester", "case-worker", request)
	forged.Approver.PrincipalID = "custodian-1"
	if _, err = service.AuthorizeRevelation(ctx, revelationPolicy(), request, forged, testApproval("decision:custodian", "custodian-1", request)); !errors.Is(err, pseudonym.ErrRevelationDenied) {
		t.Fatalf("mutated approval identity accepted: %v", err)
	}
	approved, err := service.AuthorizeRevelation(ctx, revelationPolicy(), request, testApproval("decision:requester", "case-worker", request), testApproval("decision:custodian", "custodian-1", request))
	if err != nil {
		t.Fatal(err)
	}
	authority.revoked["decision:custodian"] = true
	if _, _, err := service.ReleaseWithEvidence(ctx, governedRelease(p, approved.Evidence), approved.Evidence); !errors.Is(err, pseudonym.ErrRevelationDenied) {
		t.Fatalf("revoked custodian authority at reveal: %v", err)
	}
	legacy := mintLegacyRevelationEvidence(t, p)
	if _, _, err := service.ReleaseWithEvidence(ctx, pseudonym.EscrowReleaseRequest{Pseudonym: p, RequestedBy: "case-worker", EscrowCustodian: "custodian-1", Purpose: "case-intake", TTL: time.Minute}, legacy); !errors.Is(err, pseudonym.ErrRevelationEvidence) {
		t.Fatalf("free-text receipt accepted: %v", err)
	}
}

func mintLegacyRevelationEvidence(t testing.TB, p pseudonym.Pseudonym) pseudonym.RevelationEvidence {
	t.Helper()
	d, err := pseudonym.EvaluateRevelation(revelationPolicy(), revelationRequest(p))
	if err != nil {
		t.Fatal(err)
	}
	return d.Evidence
}

func TestTodo_ANON_003_Security(t *testing.T) {
	derivationKey, _, tenantKEK := escrowHandles()
	base := custody.NewInMemoryFake(func() time.Time { return testNow })
	provider := &escrowTestProvider{}
	for _, config := range []pseudonym.EscrowConfig{
		{Deriver: base, Provider: provider, DerivationKey: derivationKey, EscrowKey: derivationKey},
		{Deriver: base, Provider: provider, DerivationKey: derivationKey, EscrowKey: tenantKEK, TenantKEKs: []custody.Handle{tenantKEK}},
	} {
		if _, err := pseudonym.NewIdentityEscrow(config); !errors.Is(err, pseudonym.ErrEscrowKeySeparation) {
			t.Fatalf("config = %+v, err = %v", config, err)
		}
	}
	service, _, _, _, _, _ := escrowService(t)
	ctx := escrowContext()
	p, err := service.Generate(ctx, pseudonym.GenerateRequest{Subject: "subject-123", Scope: "program-a", Purpose: "case-intake"})
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []pseudonym.EscrowReleaseRequest{
		{Pseudonym: p, RequestedBy: "alice", EscrowCustodian: "alice", Purpose: "case-intake", EvidenceRef: "ev", TTL: time.Hour},
		{Pseudonym: p, RequestedBy: "alice", EscrowCustodian: "bob", Purpose: "case-intake", TTL: time.Hour},
		{Pseudonym: p, RequestedBy: "alice", EscrowCustodian: "bob", Purpose: "case-intake", EvidenceRef: "ev", TTL: pseudonym.MaxEscrowReleaseTTL + time.Nanosecond},
	} {
		if _, _, err := service.Release(ctx, request); !errors.Is(err, pseudonym.ErrEscrowReleaseDenied) {
			t.Fatalf("request = %+v, err = %v", request, err)
		}
	}
}

// governedRelease is the two-party release request the ANON-004 revelation
// receipt binds to (requester and custodian match revelationRequest).
func governedRelease(p pseudonym.Pseudonym, evidence pseudonym.RevelationEvidence) pseudonym.EscrowReleaseRequest {
	request := pseudonym.RevelationRequest{Pseudonym: pseudonym.Pseudonym{ID: evidence.PseudonymRef, Tenant: evidence.Tenant, Scope: evidence.Scope, Generation: evidence.Generation, Purpose: evidence.EscrowPurpose}, RequestedBy: evidence.RequestedBy, Approver: evidence.Approver, ApproverRole: evidence.ApproverRole, RequesterRole: evidence.RequesterRole, Purpose: evidence.Purpose, LegalBasisRef: evidence.LegalBasisRef, Scope: evidence.Scope, TTL: evidence.ExpiresAt.Sub(evidence.AuthorizedAt), RequestedAt: evidence.AuthorizedAt, ExpiresAt: evidence.ExpiresAt, Recipients: evidence.Recipients, Fields: evidence.Fields, NotificationPolicy: evidence.NotificationPolicy, CaseRef: evidence.CaseRef}
	return pseudonym.EscrowReleaseRequest{Pseudonym: p, RequestedBy: evidence.RequestedBy, EscrowCustodian: evidence.Approver, Purpose: p.Purpose, TTL: time.Minute, RequesterDecision: testApproval("decision:requester", evidence.RequestedBy, request), CustodianDecision: testApproval("decision:custodian", evidence.Approver, request)}
}

// mintRevelationEvidence authorizes one revelation for p under the test
// policy; distinct legal-basis refs yield distinct single-use receipts.
func mintRevelationEvidence(t testing.TB, service *pseudonym.EscrowedService, p pseudonym.Pseudonym, legalBasis string) pseudonym.RevelationEvidence {
	t.Helper()
	request := revelationRequest(p)
	request.LegalBasisRef = legalBasis
	decision, err := service.AuthorizeRevelation(escrowContext(), revelationPolicy(), request, testApproval("decision:requester", "case-worker", request), testApproval("decision:custodian", "custodian-1", request))
	if err != nil || !decision.Allowed {
		t.Fatalf("revelation decision = %+v, err = %v", decision, err)
	}
	return decision.Evidence
}
