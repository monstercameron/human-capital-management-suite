package eastwest_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/eastwest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/bundle"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

func TestTodo_REV_033_03_Integration(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	ca, caKey := rev03303CA(t, now)
	pin, err := bundle.NewPinnedCert(ca.Raw)
	if err != nil {
		t.Fatal(err)
	}
	trustBundle, err := bundle.New(1, "east-west-workload", []bundle.PinnedCert{pin}, nil, now.Add(-time.Minute), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	trustVerifier := bundle.NewVerifier(trustBundle)

	mtlsIssuer, err := workload.NewMTLSIssuer(workload.MTLSIssuerConfig{
		Issuer: "workload-root", Certificate: ca, Signer: caKey, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	peerIssued, err := mtlsIssuer.Issue(workload.IdentitySpec{
		Tenant: "tenant-a", Cell: "cell-a", Service: "worker", Instance: "worker-17", Lifetime: 3 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := trustVerifier.Verify(peerIssued.Certificate, nil, now, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}); err != nil {
		t.Fatalf("pinned trust bundle rejected peer: %v", err)
	}
	mtlsVerifier, err := workload.NewMTLSVerifier(workload.MTLSVerifierConfig{
		Roots: trustBundle.RootPool(), ExpectedTenant: "tenant-a", ExpectedCell: "cell-a", ExpectedService: "worker",
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := mtlsVerifier.Verify(peerIssued.Certificate)
	if err != nil {
		t.Fatal(err)
	}

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := workload.NewIssuer(workload.IssuerConfig{Name: "workload-authority", KeyID: "key-1", Private: private, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := workload.NewVerifier(workload.VerifierConfig{
		Keys: workload.NewStaticKeySource().WithKey("workload-authority", "key-1", public), Cell: "cell-a", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := issuer.Issue(workload.IssueSpec{Subject: "worker/worker-17", Role: workload.RoleWorker, Cell: "cell-a", Lifetime: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := workload.BindMTLSIdentity(peer, issued, now)
	if err != nil {
		t.Fatalf("bind signed workload credential to pinned mTLS peer: %v", err)
	}
	if !bound.ValidAt(now) || bound.Subject() != peer.Subject() || bound.Role() != workload.RoleWorker {
		t.Fatalf("bound workload identity does not preserve the verified peer: subject=%q role=%q", bound.Subject(), bound.Role())
	}
	authorization, err := workload.Authorize(workload.Request{Caller: bound, Resource: workload.ResourceLedger, Action: workload.ActionWrite, EvaluatedAt: now})
	if err != nil || authorization.Effect != workload.EffectAllow {
		t.Fatalf("bound credential denied by workload service policy: decision=%+v err=%v", authorization, err)
	}

	policy, err := eastwest.CompileManifest(&eastwest.Manifest{
		Version: 1, Module: "github.com/monstercameron/human-capital-management-suite", EffectiveAt: now.Format(time.RFC3339),
		Dependencies: []eastwest.Dependency{{
			Consumer: "worker", Dependency: "postgres-store", Owner: "data-plane", Criticality: "critical",
			TenantScope: "tenant-local", CellScope: "cell-local", WorkloadID: "worker", NetworkPath: "cell-private",
			Protocol: "grpc", SchemaRange: "v1", Timeout: "1s", Staleness: "0s", Fallback: "deny", SLO: "99.9%",
			Version: "1", EvidenceRef: "evidence/rev03303", VerifiedAt: now.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := policy.AllowsPeer(peer, "postgres-store", "READ", "cell-a", now); err != nil {
		t.Fatalf("verified peer denied by east-west policy: %v", err)
	}

	certificateOnly := peer.AsWorkloadIdentity()
	if _, err := workload.BindMTLSIdentity(peer, certificateOnly, now); !errors.Is(err, workload.ErrMTLSWorkloadMismatch) {
		t.Fatalf("certificate without issued workload credential accepted: %v", err)
	}
	if err := policy.AllowsPeer(workload.MTLSIdentity{}, "postgres-store", "READ", "cell-a", now); !errors.Is(err, eastwest.ErrPeerNotVerified) {
		t.Fatalf("unverified peer reached east-west policy: %v", err)
	}
}

func rev03303CA(t *testing.T, now time.Time) (*x509.Certificate, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "REV-033-03 test root"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(2 * time.Hour),
		BasicConstraintsValid: true, IsCA: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, private
}
