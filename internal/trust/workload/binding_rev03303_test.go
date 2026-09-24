package workload

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"
)

func TestTodo_REV_033_03(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	ca, caKey, roots := testMTLSAuthority(t, now)
	mtlsIssuer, err := NewMTLSIssuer(MTLSIssuerConfig{Issuer: "root", Certificate: ca, Signer: caKey, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	// Build a matching signed workload identity and an independently issued
	// mTLS identity for the same unique process instance.
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewIssuer(IssuerConfig{Name: "workload-authority", KeyID: "key-1", Private: private, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(VerifierConfig{Keys: NewStaticKeySource().WithKey("workload-authority", "key-1", pub), Cell: "cell-a", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Issue(IssueSpec{Subject: "worker/worker-1", Role: RoleWorker, Cell: "cell-a", Lifetime: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	workloadID, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := mtlsIssuer.Issue(IdentitySpec{Tenant: "tenant-a", Cell: "cell-a", Service: "worker", Instance: "worker-1", Lifetime: 3 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	peerVerifier, err := NewMTLSVerifier(MTLSVerifierConfig{Roots: roots, ExpectedTenant: "tenant-a", ExpectedCell: "cell-a", ExpectedService: "worker", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := peerVerifier.Verify(issued.Certificate)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := BindMTLSIdentity(peer, workloadID, now)
	if err != nil {
		t.Fatalf("BindMTLSIdentity: %v", err)
	}
	decision, err := Authorize(Request{Caller: bound, Resource: ResourceLedger, Action: ActionWrite, EvaluatedAt: now})
	if err != nil || decision.Effect != EffectAllow || decision.RuleID != "svc.worker.ledger.write" {
		t.Fatalf("bound call authorization = %+v, %v", decision, err)
	}
	if !bound.ExpiresAt().Equal(issued.Certificate.NotAfter.UTC()) {
		t.Fatalf("bound identity expiry=%s, want earliest credential expiry %s", bound.ExpiresAt(), issued.Certificate.NotAfter.UTC())
	}
}

func TestTodo_REV_033_03_Security(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	ca, caKey, roots := testMTLSAuthority(t, now)
	mtlsIssuer, err := NewMTLSIssuer(MTLSIssuerConfig{Issuer: "root", Certificate: ca, Signer: caKey, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewIssuer(IssuerConfig{Name: "workload-authority", KeyID: "key-1", Private: private, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(VerifierConfig{Keys: NewStaticKeySource().WithKey("workload-authority", "key-1", pub), Cell: "cell-a", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	peerVerifier, err := NewMTLSVerifier(MTLSVerifierConfig{Roots: roots, ExpectedTenant: "tenant-a", ExpectedCell: "cell-a", ExpectedService: "worker", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	peerCert, err := mtlsIssuer.Issue(IdentitySpec{Tenant: "tenant-a", Cell: "cell-a", Service: "worker", Instance: "worker-1", Lifetime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := peerVerifier.Verify(peerCert.Certificate)
	if err != nil {
		t.Fatal(err)
	}

	issue := func(subject string, role ProcessRole, cell string) Identity {
		t.Helper()
		token, err := signer.Issue(IssueSpec{Subject: subject, Role: role, Cell: cell, Lifetime: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		id, err := verifier.Verify(context.Background(), token)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	tests := []struct {
		name string
		peer MTLSIdentity
		id   Identity
		at   time.Time
	}{
		{name: "no verified peer", id: issue("worker/worker-1", RoleWorker, "cell-a"), at: now},
		{name: "no issued identity", peer: peer, at: now},
		{name: "certificate cannot substitute for issued identity", peer: peer, id: peer.AsWorkloadIdentity(), at: now},
		{name: "subject mismatch", peer: peer, id: issue("worker/worker-2", RoleWorker, "cell-a"), at: now},
		{name: "role mismatch", peer: peer, id: issue("worker/worker-1", RoleProjector, "cell-a"), at: now},
		{name: "expired mTLS", peer: peer, id: issue("worker/worker-1", RoleWorker, "cell-a"), at: peer.ExpiresAt()},
		{name: "expired signed identity", peer: peer, id: issue("worker/worker-1", RoleWorker, "cell-a"), at: now.Add(time.Minute)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := BindMTLSIdentity(tc.peer, tc.id, tc.at); !errors.Is(err, ErrMTLSWorkloadMismatch) {
				t.Fatalf("BindMTLSIdentity = %v, want ErrMTLSWorkloadMismatch", err)
			}
		})
	}
}
