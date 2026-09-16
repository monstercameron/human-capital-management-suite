package application

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// recordingLogger captures structured events so a test can assert on what the
// composition announced rather than on what it printed.
type recordingLogger struct {
	mu     sync.Mutex
	events []string
}

func (l *recordingLogger) Info(msg string, _ ...any)  { l.record(msg) }
func (l *recordingLogger) Error(msg string, _ ...any) { l.record(msg) }

func (l *recordingLogger) record(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, msg)
}

func (l *recordingLogger) saw(msg string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, event := range l.events {
		if event == msg {
			return true
		}
	}
	return false
}

var _ bootstrap.Logger = (*recordingLogger)(nil)

// stubStore is an app.Store that records nothing and answers nothing. It
// exists so a composition test can replace persistence without a database.
type stubStore struct {
	mu            sync.Mutex
	bootstrapped  []string
	bootstrapFail error
}

func (s *stubStore) Bootstrap(_ context.Context, tenant string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bootstrapped = append(s.bootstrapped, tenant)
	return s.bootstrapFail
}

func (s *stubStore) AppendIntent(context.Context, app.IntentRecord) (app.AppendResult, error) {
	return app.AppendResult{}, errors.New("stub store: no appends")
}

func (s *stubStore) LoadIntent(context.Context, string, string) (app.IntentRecord, error) {
	return app.IntentRecord{}, app.ErrIntentNotFound
}

func (s *stubStore) ListIntents(context.Context, string, int32, string) (app.IntentPage, error) {
	return app.IntentPage{}, nil
}

func (s *stubStore) Timeline(context.Context, string, string) ([]app.TimelineEntry, error) {
	return nil, nil
}

func (s *stubStore) tenants() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bootstrapped...)
}

var _ app.Store = (*stubStore)(nil)

// stubVerifier refuses every credential. Composition is what these tests
// assert on, not admission.
type stubVerifier struct{}

func (stubVerifier) Verify(context.Context, trust.Credential) (*trust.Principal, error) {
	return nil, errors.New("stub verifier: no principals")
}

var _ trust.Verifier = stubVerifier{}

// secondStubVerifier is a second admission adapter with the same behaviour
// and a different type, used to prove the composed graph records which
// adapter was composed rather than only that one was.
type secondStubVerifier struct{}

func (secondStubVerifier) Verify(context.Context, trust.Credential) (*trust.Principal, error) {
	return nil, errors.New("second stub verifier: no principals")
}

var _ trust.Verifier = secondStubVerifier{}

// TestOptionsApplyFoldsSeamsWithoutMutatingTheReceiver proves Options is a
// value: applying options produces a new configuration rather than editing
// shared state, which is what keeps two compositions in one process
// independent.
func TestOptionsApplyFoldsSeamsWithoutMutatingTheReceiver(t *testing.T) {
	base := Options{}
	store := &stubStore{}
	verifier := stubVerifier{}
	now := func() time.Time { return time.Unix(0, 0).UTC() }

	applied := base.Apply(
		nil, // a nil option is ignored rather than panicking
		WithStore(store),
		WithVerifier(verifier),
		WithTelemetryProvider(nil),
		WithClock(now),
		WithEvidence(app.NewMemoryEvidenceSink()),
	)
	if base.NewStore != nil || base.NewVerifier != nil || base.Now != nil {
		t.Fatal("Apply mutated the receiver")
	}
	if applied.NewStore == nil || applied.NewVerifier == nil || applied.NewTelemetry == nil {
		t.Fatal("Apply did not install the supplied factories")
	}
	got, err := applied.NewStore(nil, ServeConfig{})
	if err != nil || got != app.Store(store) {
		t.Errorf("NewStore = (%v, %v), want the supplied store", got, err)
	}
	gotVerifier, err := applied.NewVerifier(ServeConfig{})
	if err != nil || gotVerifier != trust.Verifier(verifier) {
		t.Errorf("NewVerifier = (%v, %v), want the supplied verifier", gotVerifier, err)
	}
	provider, err := applied.NewTelemetry(context.Background(), "id", ServeConfig{OTelExporter: OTelExporterStdout})
	if err != nil || provider != nil {
		t.Errorf("NewTelemetry = (%v, %v), want an explicit nil provider: telemetry off is a decision", provider, err)
	}
	if applied.Now == nil || !applied.Now().Equal(time.Unix(0, 0).UTC()) {
		t.Error("WithClock did not install the supplied clock")
	}
	if applied.Evidence == nil {
		t.Error("WithEvidence did not install the supplied sink")
	}
}

// TestOptionsListenDefaultsToNetListen proves the listener seam has a real
// production default rather than being required of every caller.
func TestOptionsListenDefaultsToNetListen(t *testing.T) {
	listener, err := Options{}.listen()("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("default listen: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	sentinel := errors.New("supplied listener factory")
	supplied := Options{}.Apply(WithListener(func(string, string) (net.Listener, error) { return nil, sentinel }))
	if _, err := supplied.listen()("tcp", "127.0.0.1:0"); !errors.Is(err, sentinel) {
		t.Errorf("supplied listen error = %v, want %v", err, sentinel)
	}
}

// TestOptionsSeamsCoverEveryConstructedAdapter walks the remaining seams. The
// point is coverage of the swap surface, not of the adapters: an adapter this
// root constructs but does not expose as a seam is one a test would have to
// reach around, which is where production-only branches come from.
func TestOptionsSeamsCoverEveryConstructedAdapter(t *testing.T) {
	migrated := false
	sentinel := errors.New("composed")
	options := Options{}.Apply(
		WithStoreFactory(func(*pgxadapter.Pool, ServeConfig) (app.Store, error) { return &stubStore{}, nil }),
		WithMigrator(func(context.Context, string, bootstrap.Logger) error {
			migrated = true
			return nil
		}),
		WithIntentClock(nil),
		WithIDs(nil),
		WithDomainInputs(nil, nil, nil),
		WithExecutionComposer(func(*app.CellConfig, *pgxadapter.Pool, app.EvidenceStore, ServeConfig) error {
			return sentinel
		}),
	)
	if options.Migrate == nil || options.NewStore == nil || options.ComposeExecution == nil {
		t.Fatal("a seam was not installed")
	}
	if err := options.Migrate(context.Background(), "postgres://x", nil); err != nil || !migrated {
		t.Errorf("migrator = %v, ran %t; want it to run", err, migrated)
	}
	if err := options.ComposeExecution(nil, nil, nil, ServeConfig{}); !errors.Is(err, sentinel) {
		t.Errorf("execution composer error = %v, want %v", err, sentinel)
	}
}
