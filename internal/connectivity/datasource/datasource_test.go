package datasource_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/datasource"
)

// fakeAdapter is the PRIMARY-contract stand-in: it records the Source it
// was opened with and serves canned documents without any network.
type fakeAdapter struct {
	src     datasource.Source
	docs    []datasource.Document
	queries []datasource.Query
	writes  []datasource.Write
	closed  bool
}

func (f *fakeAdapter) Source() datasource.SourceID { return f.src.SourceID() }
func (f *fakeAdapter) AdapterID() string           { return f.src.AdapterID }
func (f *fakeAdapter) Query(_ context.Context, q datasource.Query) ([]datasource.Document, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	f.queries = append(f.queries, q)
	return f.docs, nil
}
func (f *fakeAdapter) Write(_ context.Context, w datasource.Write) error {
	if err := w.Validate(); err != nil {
		return err
	}
	if !f.src.Writable(w.Collection) {
		if f.src.ReadOnly {
			return fmt.Errorf("%w: %s/%s", datasource.ErrReadOnly, f.src.Tenant, f.src.ID)
		}
		return fmt.Errorf("%w: %q", datasource.ErrCollectionNotWritable, w.Collection)
	}
	f.writes = append(f.writes, w)
	return nil
}
func (f *fakeAdapter) Close(_ context.Context) error { f.closed = true; return nil }

type fakeSecrets struct {
	cred datasource.Credential
	err  error
}

func (f fakeSecrets) ResolveSecret(_ context.Context, _ string, _ datasource.SecretRef) (datasource.Credential, error) {
	return f.cred, f.err
}

type fakeEgress struct {
	denied map[string]bool
	calls  [][2]any
}

func (f *fakeEgress) AllowHost(_ context.Context, host string, port int) error {
	f.calls = append(f.calls, [2]any{host, port})
	if f.denied[host] {
		return fmt.Errorf("%w: %s", datasource.ErrEgressDenied, host)
	}
	return nil
}

func testSource() datasource.Source {
	return datasource.Source{
		Tenant:      "tenant-a",
		ID:          "ledger",
		AdapterID:   "fake",
		Host:        "mongo.internal",
		Port:        27017,
		Database:    "ledgerdb",
		Secret:      datasource.SecretRef{Store: "platform", Key: "ledger-ro"},
		TLSRequired: true,
		ReadOnly:    true,
	}
}

func testRegistry(t *testing.T) (*datasource.Registry, *fakeAdapter) {
	t.Helper()
	r := datasource.NewRegistry()
	fake := &fakeAdapter{docs: []datasource.Document{[]byte(`{"_id":1}`)}}
	if err := r.RegisterAdapter("fake", func(_ context.Context, src datasource.Source, _ datasource.OpenDeps) (datasource.Connector, error) {
		cp := *fake
		cp.src = src
		return &cp, nil
	}); err != nil {
		t.Fatalf("registering fake adapter: %v", err)
	}
	return r, fake
}

// TestTodo_WF_DATA_015 is the PRIMARY contract: registration,
// validation, tenant-bound open, and read-only query flow.
func TestTodo_WF_DATA_015(t *testing.T) {
	ctx := context.Background()
	r, _ := testRegistry(t)
	if err := r.AddSource(testSource()); err != nil {
		t.Fatalf("adding valid source: %v", err)
	}

	deps := datasource.OpenDeps{Secrets: fakeSecrets{}, Egress: &fakeEgress{}}
	conn, err := r.Open(ctx, "tenant-a", "ledger", deps)
	if err != nil {
		t.Fatalf("opening registered source: %v", err)
	}
	if got := conn.Source(); got.Tenant != "tenant-a" || got.ID != "ledger" {
		t.Fatalf("connector source = %+v, want tenant-a/ledger", got)
	}
	if conn.AdapterID() != "fake" {
		t.Fatalf("adapter id = %q, want fake", conn.AdapterID())
	}

	docs, err := conn.Query(ctx, datasource.Query{Collection: "entries"})
	if err != nil {
		t.Fatalf("querying: %v", err)
	}
	if len(docs) != 1 || string(docs[0]) != `{"_id":1}` {
		t.Fatalf("documents = %q, want one canned document", docs)
	}
	if err := conn.Write(ctx, datasource.Write{Collection: "entries", Documents: []datasource.Document{[]byte(`{}`)}}); !errors.Is(err, datasource.ErrReadOnly) {
		t.Fatalf("write to read-only source = %v, want ErrReadOnly", err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("closing: %v", err)
	}
}

// TestTodo_WF_DATA_015_Validation pins every deny-by-default shape rule:
// each malformed source fails AddSource with a wrapped sentinel.
func TestTodo_WF_DATA_015_Validation(t *testing.T) {
	r, _ := testRegistry(t)
	mutate := map[string]func(*datasource.Source){
		"empty tenant":       func(s *datasource.Source) { s.Tenant = "" },
		"empty id":           func(s *datasource.Source) { s.ID = "" },
		"empty adapter":      func(s *datasource.Source) { s.AdapterID = "" },
		"empty host":         func(s *datasource.Source) { s.Host = "" },
		"host with scheme":   func(s *datasource.Source) { s.Host = "mongodb://mongo.internal" },
		"host with port":     func(s *datasource.Source) { s.Host = "mongo.internal:27017" },
		"host with userinfo": func(s *datasource.Source) { s.Host = "user@mongo.internal" },
		"host with space":    func(s *datasource.Source) { s.Host = "mongo internal" },
		"negative port":      func(s *datasource.Source) { s.Port = -1 },
		"huge port":          func(s *datasource.Source) { s.Port = 70000 },
		"empty database":     func(s *datasource.Source) { s.Database = "" },
		"empty secret store": func(s *datasource.Source) { s.Secret.Store = "" },
		"empty secret key":   func(s *datasource.Source) { s.Secret.Key = "" },
		"TLS not required":   func(s *datasource.Source) { s.TLSRequired = false },
		"writable no colls":  func(s *datasource.Source) { s.ReadOnly = false },
		"blank coll name":    func(s *datasource.Source) { s.ReadOnly = false; s.WritableCollections = []string{" "} },
	}
	for name, fn := range mutate {
		src := testSource()
		fn(&src)
		if err := r.AddSource(src); err == nil {
			t.Errorf("%s: AddSource accepted a malformed source", name)
		} else if !errors.Is(err, datasource.ErrInvalidSource) && !errors.Is(err, datasource.ErrInsecureTransport) {
			t.Errorf("%s: error %v wraps neither ErrInvalidSource nor ErrInsecureTransport", name, err)
		}
	}
	if err := r.AddSource(datasource.Source{}); err == nil {
		t.Errorf("zero source: AddSource accepted it")
	}

	bad := testSource()
	bad.AdapterID = "nope"
	if err := r.AddSource(bad); !errors.Is(err, datasource.ErrUnknownAdapter) {
		t.Errorf("unknown adapter: error %v, want ErrUnknownAdapter", err)
	}
	if err := r.RegisterAdapter("fake", nil); err == nil {
		t.Errorf("nil factory: RegisterAdapter accepted it")
	}
	nilConn := func(context.Context, datasource.Source, datasource.OpenDeps) (datasource.Connector, error) {
		return nil, nil
	}
	if err := r.RegisterAdapter("", nilConn); err == nil {
		t.Errorf("empty adapter id: RegisterAdapter accepted it")
	}
	if err := r.RegisterAdapter("fake", nilConn); !errors.Is(err, datasource.ErrDuplicateAdapter) {
		t.Errorf("duplicate adapter: error %v, want ErrDuplicateAdapter", err)
	}
	if err := r.AddSource(testSource()); err != nil {
		t.Fatalf("adding valid source: %v", err)
	}
	if err := r.AddSource(testSource()); !errors.Is(err, datasource.ErrDuplicateSource) {
		t.Errorf("duplicate source: error %v, want ErrDuplicateSource", err)
	}
	if err := r.RegisterAdapter("nilconn", nilConn); err != nil {
		t.Fatalf("registering nilconn adapter: %v", err)
	}
	nilSrc := testSource()
	nilSrc.ID = "nilconn"
	nilSrc.AdapterID = "nilconn"
	if err := r.AddSource(nilSrc); err != nil {
		t.Fatalf("adding nilconn source: %v", err)
	}
	if _, err := r.Open(context.Background(), "tenant-a", "nilconn", datasource.OpenDeps{Secrets: fakeSecrets{}, Egress: &fakeEgress{}}); err == nil {
		t.Errorf("nil connector: Open accepted it")
	}
}

// TestTodo_WF_DATA_015_QueryWriteValidation pins query and write shape
// rules at the contract level, before any adapter sees them.
func TestTodo_WF_DATA_015_QueryWriteValidation(t *testing.T) {
	if err := (datasource.Query{}).Validate(); !errors.Is(err, datasource.ErrInvalidQuery) {
		t.Errorf("empty query: %v, want ErrInvalidQuery", err)
	}
	if err := (datasource.Query{Collection: "c", Limit: -1}).Validate(); !errors.Is(err, datasource.ErrLimitExceeded) {
		t.Errorf("negative limit: %v, want ErrLimitExceeded", err)
	}
	if err := (datasource.Query{Collection: "c", Limit: datasource.MaxLimit + 1}).Validate(); !errors.Is(err, datasource.ErrLimitExceeded) {
		t.Errorf("over-cap limit: %v, want ErrLimitExceeded", err)
	}
	if got := (datasource.Query{Collection: "c"}).EffectiveLimit(); got != datasource.DefaultLimit {
		t.Errorf("zero limit resolves to %d, want DefaultLimit %d", got, datasource.DefaultLimit)
	}
	if got := (datasource.Query{Collection: "c", Limit: 7}).EffectiveLimit(); got != 7 {
		t.Errorf("explicit limit resolves to %d, want 7", got)
	}
	if err := (datasource.Write{}).Validate(); !errors.Is(err, datasource.ErrInvalidWrite) {
		t.Errorf("empty write: %v, want ErrInvalidWrite", err)
	}
	if err := (datasource.Write{Collection: "c"}).Validate(); !errors.Is(err, datasource.ErrInvalidWrite) {
		t.Errorf("documentless write: %v, want ErrInvalidWrite", err)
	}
	big := make([]datasource.Document, datasource.MaxWriteBatch+1)
	if err := (datasource.Write{Collection: "c", Documents: big}).Validate(); !errors.Is(err, datasource.ErrBatchTooLarge) {
		t.Errorf("over-cap batch: %v, want ErrBatchTooLarge", err)
	}
}

// TestTodo_WF_DATA_015_Security proves the isolation rules: no
// cross-tenant existence oracle, no nil-seam open, and secret/egress
// failures that never carry credential material.
func TestTodo_WF_DATA_015_Security(t *testing.T) {
	ctx := context.Background()
	r, _ := testRegistry(t)
	if err := r.AddSource(testSource()); err != nil {
		t.Fatalf("adding valid source: %v", err)
	}
	deps := datasource.OpenDeps{Secrets: fakeSecrets{}, Egress: &fakeEgress{}}

	// Same source id under a second tenant is a distinct registration.
	other := testSource()
	other.Tenant = "tenant-b"
	other.Secret = datasource.SecretRef{Store: "platform", Key: "ledger-ro-b"}
	if err := r.AddSource(other); err != nil {
		t.Fatalf("adding same id under tenant-b: %v", err)
	}
	// Cross-tenant open reports exactly what an absent source reports.
	crossConn, crossErr := r.Open(ctx, "tenant-b", "ledger", deps)
	if crossErr != nil {
		t.Fatalf("tenant-b opening its own source: %v", crossErr)
	}
	if got := crossConn.Source(); got.Tenant != "tenant-b" {
		t.Fatalf("tenant-b connector bound to %+v", got)
	}
	// A source registered under tenant-a only: tenant-b sees exactly what
	// an absent source reports (no cross-tenant existence oracle).
	exclusive := testSource()
	exclusive.ID = "exclusive"
	if err := r.AddSource(exclusive); err != nil {
		t.Fatalf("adding exclusive source: %v", err)
	}
	if _, err := r.Open(ctx, "tenant-b", "exclusive", deps); !errors.Is(err, datasource.ErrUnknownSource) {
		t.Fatalf("cross-tenant open = %v, must wrap ErrUnknownSource", err)
	}
	if _, absentErr := r.Open(ctx, "tenant-a", "missing", deps); !errors.Is(absentErr, datasource.ErrUnknownSource) {
		t.Fatalf("absent open = %v, must wrap ErrUnknownSource", absentErr)
	}

	if _, err := r.Open(ctx, "tenant-a", "ledger", datasource.OpenDeps{Egress: &fakeEgress{}}); err == nil {
		t.Errorf("nil secrets port: Open accepted it")
	}
	if _, err := r.Open(ctx, "tenant-a", "ledger", datasource.OpenDeps{Secrets: fakeSecrets{}}); err == nil {
		t.Errorf("nil egress policy: Open accepted it")
	}

	// A factory that surfaces seam failures; the material must not leak.
	const password = "s3cret-material-must-not-leak"
	r2 := datasource.NewRegistry()
	if err := r2.RegisterAdapter("leaky-check", func(ctx context.Context, src datasource.Source, d datasource.OpenDeps) (datasource.Connector, error) {
		cred, err := d.Secrets.ResolveSecret(ctx, src.Tenant, src.Secret)
		if err != nil {
			return nil, fmt.Errorf("%w: %s/%s", datasource.ErrSecretResolution, src.Tenant, src.ID)
		}
		if err := d.Egress.AllowHost(ctx, src.Host, src.Port); err != nil {
			return nil, err
		}
		if cred.Password == "" {
			return nil, fmt.Errorf("%w: empty material", datasource.ErrSecretResolution)
		}
		return nil, fmt.Errorf("%w: %s/%s refuses test dial", datasource.ErrEgressDenied, src.Tenant, src.ID)
	}); err != nil {
		t.Fatalf("registering check adapter: %v", err)
	}
	leakSrc := testSource()
	leakSrc.AdapterID = "leaky-check"
	if err := r2.AddSource(leakSrc); err != nil {
		t.Fatalf("adding check source: %v", err)
	}
	_, err := r2.Open(ctx, "tenant-a", "ledger", datasource.OpenDeps{
		Secrets: fakeSecrets{cred: datasource.Credential{Username: "u", Password: password}},
		Egress:  &fakeEgress{},
	})
	if err == nil {
		t.Fatalf("check adapter: Open succeeded, want a refusal")
	}
	if strings.Contains(err.Error(), password) {
		t.Errorf("refusal %q carries credential material", err)
	}
	_, err = r2.Open(ctx, "tenant-a", "ledger", datasource.OpenDeps{
		Secrets: fakeSecrets{err: errors.New("vault sealed")},
		Egress:  &fakeEgress{},
	})
	if !errors.Is(err, datasource.ErrSecretResolution) {
		t.Errorf("sealed vault: %v, want ErrSecretResolution", err)
	}
}
