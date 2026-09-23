package evidence

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/monstercameron/human-capital-management-suite/internal/cryptoagility"
	kernelevidence "github.com/monstercameron/human-capital-management-suite/internal/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/endpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const (
	testTenant  = "acme-corp"
	testSubject = "subject-a"
	testPurpose = "evidence_export"
)

// admittedContext builds a fully trusted request context, the same
// machinery internal/transport/operations's own tests use
// (transport.Admit over a fixture Verifier), extended with the
// purposes a caller is authorized for.
func admittedContext(t *testing.T, subject string, purposes []string, message proto.Message, method string) context.Context {
	t.Helper()
	return admittedContextTenant(t, testTenant, subject, purposes, message, method)
}

// admittedContextTenant is admittedContext parameterized by tenant, used to
// build a caller in a different tenant than the fixture under test.
func admittedContextTenant(t *testing.T, tenant, subject string, purposes []string, message proto.Message, method string) context.Context {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-" + subject, IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(100000, 0),
		CredentialDigest: "credential-" + subject, Purposes: purposes,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, _, admitErr := transport.Admit(context.Background(), transport.Config{
		Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return p, nil }),
		Now:      func() time.Time { return time.Unix(10, 0).UTC() },
	}, transport.AdmissionRequest{
		Metadata: transport.MapMetadata{transport.AuthorizationMetadataKey: {"Bearer fixture"}},
		Method:   method, Kind: transport.KindGRPC, Message: message,
	})
	if admitErr != nil {
		t.Fatalf("transport.Admit: %v", admitErr)
	}
	return ctx
}

// harness bundles one fully-wired evidence server plus every fake dependency
// a test needs to inspect directly.
type harness struct {
	server    *server
	lineage   *MemoryLineageSource
	receipts  *MemoryReceiptStore
	artifacts *MemoryArtifactSink
	ops       *OperationJournal
	idem      *endpoint.Coordinator
	purposes  StaticPurposePolicy
	dispatch  *gatedDispatcher
	signerKey cryptoagility.Key
	verifyKey map[string]cryptoagility.Key
	policy    cryptoagility.AlgorithmPolicy
	packKey   PackageKey
	now       time.Time
}

// gatedDispatcher captures dispatched jobs instead of running them until the
// test releases them, so a test can assert the request path returned before
// the job's work happened.
type gatedDispatcher struct {
	jobs chan struct {
		ctx context.Context
		job exportJob
		run func(context.Context, exportJob)
	}
	sync bool
}

func newGatedDispatcher() *gatedDispatcher {
	return &gatedDispatcher{jobs: make(chan struct {
		ctx context.Context
		job exportJob
		run func(context.Context, exportJob)
	}, 8)}
}

func (d *gatedDispatcher) Dispatch(ctx context.Context, job exportJob, run func(context.Context, exportJob)) {
	if d.sync {
		run(ctx, job)
		return
	}
	d.jobs <- struct {
		ctx context.Context
		job exportJob
		run func(context.Context, exportJob)
	}{ctx, job, run}
}

// Release runs exactly one queued job synchronously and waits for it to
// finish, simulating the async dispatcher having gotten around to it. The
// job runs on the context Dispatch received, so context propagation is
// observable in tests.
func (d *gatedDispatcher) Release(t *testing.T) {
	t.Helper()
	select {
	case entry := <-d.jobs:
		entry.run(entry.ctx, entry.job)
	case <-time.After(2 * time.Second):
		t.Fatal("gatedDispatcher: no job was dispatched")
	}
}

// harnessOption adjusts the dependency set before the server is built, so a
// test can substitute a port implementation (for example a LineageSource
// that drifts between reads) without duplicating the whole harness.
type harnessOption func(*Dependencies)

// withLineageSource replaces the lineage port.
func withLineageSource(src LineageSource) harnessOption {
	return func(d *Dependencies) { d.Lineage = src }
}

func newHarness(t *testing.T, opts ...harnessOption) *harness {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signerKey := cryptoagility.Key{ID: "evidence-export-key-1", Algorithm: cryptoagility.AlgorithmEd25519, Private: priv, Public: pub}
	policy := cryptoagility.AlgorithmPolicy{Active: cryptoagility.AlgorithmEd25519, Accepted: map[string]bool{cryptoagility.AlgorithmEd25519: true}}
	verifyKeys := map[string]cryptoagility.Key{signerKey.ID: {ID: signerKey.ID, Algorithm: signerKey.Algorithm, Public: pub}}

	now := time.Unix(1_000_000, 0).UTC()
	h := &harness{
		lineage:   NewMemoryLineageSource(),
		receipts:  NewMemoryReceiptStore(),
		artifacts: NewMemoryArtifactSink(),
		ops:       NewOperationJournal(func() time.Time { return now }),
		idem:      endpoint.NewCoordinator(),
		purposes: StaticPurposePolicy{
			testPurpose: append([]string(nil), kernelevidence.Dimensions...),
			"redacted_view": {
				"intent", "request", "proposal", "approvals", "transaction-heads", "reconciliation",
			},
		},
		dispatch:  newGatedDispatcher(),
		signerKey: signerKey,
		verifyKey: verifyKeys,
		policy:    policy,
		packKey:   PackageKey{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32},
		now:       now,
	}
	deps := Dependencies{
		Receipts: h.receipts, Lineage: h.lineage, Purposes: h.purposes,
		Operations: h.ops, Artifacts: h.artifacts, Idempotency: h.idem,
		PackageKey: h.packKey, Signer: h.signerKey, SigningPolicy: h.policy,
		Dispatcher: h.dispatch, Clock: func() time.Time { return h.now },
	}
	for _, opt := range opts {
		opt(&deps)
	}
	srv, err := newServer(deps)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	h.server = srv
	return h
}

// fullLineage returns a complete, all-fifteen-dimension snapshot for one
// intent, every dimension PRESENT with a digest. noteFor lets a test inject
// a specific Note value on a specific dimension (e.g. a formula-injection
// payload); pass "" for none.
func fullLineage(tenant, intentID string, noteFor, note string) LineageSnapshot {
	dims := make([]DimensionFact, 0, len(kernelevidence.Dimensions))
	for _, name := range kernelevidence.Dimensions {
		n := ""
		if name == noteFor {
			n = note
		}
		dims = append(dims, DimensionFact{Name: name, Status: kernelevidence.StatusPresent, Digest: "sha256:" + name + "-digest", Note: n})
	}
	return LineageSnapshot{
		Tenant: tenant, IntentRef: intentID, LineageDigest: "sha256:lineage-" + intentID,
		AuthorityLineage: []string{"authority:hr-admin/v1"}, Dimensions: dims,
		ClosedAt: time.Unix(999, 0).UTC(),
	}
}

// withDimension returns a copy of snap with one dimension's status/digest/
// note replaced by name.
func withDimension(snap LineageSnapshot, name string, status kernelevidence.Status, digest, note string) LineageSnapshot {
	out := snap
	out.Dimensions = append([]DimensionFact(nil), snap.Dimensions...)
	for i, d := range out.Dimensions {
		if d.Name == name {
			out.Dimensions[i] = DimensionFact{Name: name, Status: status, Digest: digest, Note: note}
		}
	}
	return out
}

// withoutDimension drops one dimension entirely, simulating a lineage
// record that never named it at all.
func withoutDimension(snap LineageSnapshot, name string) LineageSnapshot {
	out := snap
	out.Dimensions = nil
	for _, d := range snap.Dimensions {
		if d.Name != name {
			out.Dimensions = append(out.Dimensions, d)
		}
	}
	return out
}
