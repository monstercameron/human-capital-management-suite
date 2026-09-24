package bootstrap

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

type bootstrapProvider struct {
	deny bool
	now  time.Time
}

func (p *bootstrapProvider) IssueLease(ctx custody.Context, handle custody.Handle, op custody.Operation, ttl time.Duration) (custody.Lease, error) {
	if p.deny {
		return custody.Lease{}, errors.New("provider denied")
	}
	if err := ctx.Validate(); err != nil {
		return custody.Lease{}, err
	}
	return custody.Lease{ID: "lease-1", Handle: handle, Operation: op, ExpiresAt: p.now.Add(ttl), ContextDigest: custody.ContextDigest(ctx.RequestContext)}, nil
}
func (*bootstrapProvider) Encrypt(custody.Context, custody.Handle, []byte) (custody.Ciphertext, custody.Receipt, error) {
	return custody.Ciphertext{}, custody.Receipt{}, errors.New("unused")
}
func (*bootstrapProvider) Decrypt(custody.Context, custody.Handle, custody.Ciphertext) ([]byte, custody.Receipt, error) {
	return nil, custody.Receipt{}, errors.New("unused")
}
func (*bootstrapProvider) Sign(custody.Context, custody.Handle, []byte) (custody.Signature, custody.Receipt, error) {
	return custody.Signature{}, custody.Receipt{}, errors.New("unused")
}
func (*bootstrapProvider) Verify(custody.Context, custody.Handle, []byte, custody.Signature) (bool, custody.Receipt, error) {
	return false, custody.Receipt{}, errors.New("unused")
}
func (*bootstrapProvider) RenewLease(custody.Context, custody.Lease, time.Duration) (custody.Lease, error) {
	return custody.Lease{}, errors.New("unused")
}
func (*bootstrapProvider) Rotate(custody.Context, custody.Handle) (custody.Handle, custody.Receipt, error) {
	return custody.Handle{}, custody.Receipt{}, errors.New("unused")
}
func (*bootstrapProvider) Revoke(custody.Context, custody.Handle, string) (custody.Receipt, error) {
	return custody.Receipt{}, errors.New("unused")
}

func bootstrapFixture(t *testing.T, now time.Time, deny bool) (*Bootstrapper, tls.ConnectionState, custody.Handle) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootTemplate := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "bootstrap-root"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, public, private)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(root)
	issuer, err := workload.NewMTLSIssuer(workload.MTLSIssuerConfig{Issuer: "bootstrap-root", Certificate: root, Signer: private, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := issuer.Issue(workload.IdentitySpec{Tenant: "tenant-a", Cell: "cell-a", Service: "worker", Instance: "worker-1", Lifetime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := workload.NewMTLSVerifier(workload.MTLSVerifierConfig{Roots: pool, ExpectedTenant: "tenant-a", ExpectedCell: "cell-a", ExpectedService: "worker", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	provider := &bootstrapProvider{deny: deny, now: now}
	b, err := New(verifier, provider, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	handle := custody.Handle{ID: "connector-secret", Kind: custody.Secret, Version: "v1", Tenant: "tenant-a", Region: "us-east"}
	return b, tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{issued.Certificate}}, handle
}

func TestTodo_TRUST_027(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	b, peer, handle := bootstrapFixture(t, now, false)
	result, err := b.Acquire(context.Background(), peer, Request{Handle: handle, Tenant: "tenant-a", Region: "us-east", Purpose: "connector.read", Destination: "provider-a", Operation: custody.LeaseOperation, TTL: time.Minute})
	if err != nil || !result.Ready || result.Lease.ID == "" || result.Lease.ExpiresAt.After(now.Add(time.Minute)) {
		t.Fatalf("bootstrap failed or widened lease: result=%+v err=%v", result, err)
	}
	if result.Evidence.Workload != "worker/worker-1" || result.Evidence.Outcome != "granted" {
		t.Fatalf("incomplete bootstrap evidence: %+v", result.Evidence)
	}
}

func TestTodo_TRUST_027_Security(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	b, peer, handle := bootstrapFixture(t, now, false)
	peer.PeerCertificates = nil
	result, err := b.Acquire(context.Background(), peer, Request{Handle: handle, Tenant: "tenant-a", Region: "us-east", Purpose: "connector.read", Destination: "provider-a", Operation: custody.LeaseOperation, TTL: time.Minute})
	if err == nil || result.Ready || result.Lease.ID != "" {
		t.Fatalf("unattested peer became ready: result=%+v err=%v", result, err)
	}
	if !errors.Is(err, ErrBootstrapDenied) {
		t.Fatalf("wrong denial class: %v", err)
	}
	if _, err := b.Acquire(context.Background(), tls.ConnectionState{}, Request{Handle: handle, Tenant: "tenant-a", Region: "us-east", Purpose: "connector.read", Destination: "provider-a", Operation: custody.LeaseOperation, TTL: MaxBootstrapLeaseTTL + time.Second}); !errors.Is(err, ErrInvalidBootstrap) {
		t.Fatalf("unbounded TTL was not refused: %v", err)
	}
}

func TestTodo_TRUST_027_Race(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	b, peer, handle := bootstrapFixture(t, now, true)
	const workers = 16
	start := make(chan struct{})
	results := make([]Result, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = b.Acquire(context.Background(), peer, Request{Handle: handle, Tenant: "tenant-a", Region: "us-east", Purpose: "connector.read", Destination: "provider-a", Operation: custody.LeaseOperation, TTL: time.Minute})
		}(i)
	}
	close(start)
	wg.Wait()
	for i, result := range results {
		if errs[i] == nil || result.Ready || result.Lease.ID != "" || !errors.Is(errs[i], ErrBootstrapDenied) {
			t.Fatalf("concurrent custody denial %d became ready: result=%+v err=%v", i, result, errs[i])
		}
	}
}

func TestTodo_TRUST_027_Mutation(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	b, peer, handle := bootstrapFixture(t, now, false)
	result, err := b.Acquire(context.Background(), peer, Request{Handle: handle, Tenant: "tenant-b", Region: "us-east", Purpose: "connector.read", Destination: "provider-a", Operation: custody.LeaseOperation, TTL: time.Minute})
	if err == nil || result.Ready || !errors.Is(err, ErrBootstrapDenied) {
		t.Fatalf("tenant mutation was accepted: result=%+v err=%v", result, err)
	}
}

func FuzzTodo_TRUST_027(f *testing.F) {
	f.Add("tenant-a", "us-east", "provider-a", int64(time.Minute))
	f.Fuzz(func(t *testing.T, tenant, region, destination string, ttl int64) {
		err := validateRequest(Request{Handle: custody.Handle{ID: "id", Kind: custody.Secret, Version: "v1", Tenant: tenant, Region: region}, Tenant: tenant, Region: region, Purpose: "read", Destination: destination, Operation: custody.LeaseOperation, TTL: time.Duration(ttl)})
		if err == nil && (tenant == "" || region == "" || destination == "") {
			t.Fatal("empty bootstrap coordinate passed validation")
		}
	})
}
