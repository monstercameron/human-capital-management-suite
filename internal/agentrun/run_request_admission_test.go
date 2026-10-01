package agentrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTodo_AGENT_015(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.FixedZone("EDT", -4*60*60))
	request := validAdmissionRequest(now)
	store := NewMemoryAdmissionStore()
	authority := &admissionAuthority{snapshot: snapshotFor(request), err: nil}
	service, err := NewAdmissionService(AdmissionConfig{Authority: authority, Store: store, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}

	first, created, err := service.Admit(context.Background(), request)
	if err != nil || !created {
		t.Fatalf("first Admit = (%+v, %t, %v), want created accepted record", first, created, err)
	}
	if first.Decision != DecisionAccepted || first.Authority.Agent != request.Agent || first.Authority.Principal != request.Principal {
		t.Fatalf("accepted record did not pin request authority: %+v", first)
	}
	second, created, err := service.Admit(context.Background(), request)
	if err != nil || created || second.ID != first.ID || second.RequestDigest != first.RequestDigest {
		t.Fatalf("replay = (%+v, %t, %v), want original record", second, created, err)
	}
	if got := authority.calls.Load(); got != 2 {
		t.Fatalf("authority checks = %d, want both deliveries checked against current authority", got)
	}
	stored, err := store.Get(request.Source)
	if err != nil || stored.Decision != DecisionAccepted || stored.Authority.GrantRef != "grant:current" {
		t.Fatalf("stored record = %+v, %v", stored, err)
	}
	zoneReplay := request
	zoneReplay.Deadline = request.Deadline.In(time.FixedZone("UTC+2", 2*60*60))
	if got, created, err := service.Admit(context.Background(), zoneReplay); err != nil || created || got.ID != first.ID {
		t.Fatalf("equivalent deadline with different timezone = (%+v,%t,%v), want same request", got, created, err)
	}
}

func TestTodo_AGENT_015_Race(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	request := validAdmissionRequest(now)
	store := NewMemoryAdmissionStore()
	authority := &admissionAuthority{snapshot: snapshotFor(request)}
	service, err := NewAdmissionService(AdmissionConfig{Authority: authority, Store: store, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	const contenders = 40
	start := make(chan struct{})
	var wg sync.WaitGroup
	var created atomic.Int32
	errs := make(chan error, contenders)
	ids := make(chan string, contenders)
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			record, inserted, err := service.Admit(context.Background(), request)
			if err != nil {
				errs <- err
				return
			}
			if inserted {
				created.Add(1)
			}
			ids <- record.ID
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		t.Errorf("concurrent admission: %v", err)
	}
	if got := created.Load(); got != 1 {
		t.Fatalf("created records = %d, want one", got)
	}
	wantID := requestID(request.Source)
	for id := range ids {
		if id != wantID {
			t.Errorf("record ID = %q, want %q", id, wantID)
		}
	}
}

func TestTodo_AGENT_015_Security(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	valid := validAdmissionRequest(now)
	tests := []struct {
		name   string
		change func(*Request)
	}{
		{name: "unknown source kind", change: func(r *Request) { r.Source.Kind = "MODEL_CALLBACK" }},
		{name: "missing stable source key", change: func(r *Request) { r.Source.Key = " " }},
		{name: "latest version is not pinned", change: func(r *Request) { r.Agent.Version = "" }},
		{name: "missing legal entity", change: func(r *Request) { r.LegalEntity = "" }},
		{name: "missing purpose", change: func(r *Request) { r.Purpose = "" }},
		{name: "persona mention without persona pin", change: func(r *Request) { r.Persona = nil }},
		{name: "missing audience snapshot", change: func(r *Request) { r.Audience.SnapshotID = "" }},
		{name: "missing context digest", change: func(r *Request) { r.Context.Digest = "" }},
		{name: "expired deadline", change: func(r *Request) { r.Deadline = now }},
		{name: "empty budget", change: func(r *Request) { r.Budget.MaxCostMicros = 0 }},
		{name: "on behalf of without delegated credential", change: func(r *Request) { r.Principal.DelegatedCredentialRef = "" }},
		{name: "sponsored cannot borrow invoker", change: func(r *Request) { r.Principal.Mode = ModeSponsored; r.Principal.SponsorID = "scheduler" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := valid
			tc.change(&request)
			store := NewMemoryAdmissionStore()
			authority := &admissionAuthority{snapshot: snapshotFor(valid)}
			service, err := NewAdmissionService(AdmissionConfig{Authority: authority, Store: store, Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			if request.Source.Key == " " || request.Source.Kind == "MODEL_CALLBACK" {
				if _, _, err := service.Admit(context.Background(), request); !errors.Is(err, ErrInvalidRequest) {
					t.Fatalf("invalid source key should fail before persistence, got %v", err)
				}
				return
			}
			record, created, err := service.Admit(context.Background(), request)
			if err != nil || !created || record.Decision != DecisionRefused || record.Authority != (AuthoritySnapshot{}) {
				t.Fatalf("malformed request was not durably refused: record=%+v created=%t err=%v", record, created, err)
			}
			if authority.calls.Load() != 0 {
				t.Fatal("invalid request reached the authority resolver")
			}
		})
	}

	request := valid
	store := NewMemoryAdmissionStore()
	authority := &admissionAuthority{snapshot: snapshotFor(request)}
	service, err := NewAdmissionService(AdmissionConfig{Authority: authority, Store: store, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Admit(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	changed := request
	changed.Purpose = "different purpose"
	if _, _, err := service.Admit(context.Background(), changed); !errors.Is(err, ErrSourceConflict) {
		t.Fatalf("changed payload replay = %v, want conflict", err)
	}

	for _, mutate := range []func(*Request){
		func(r *Request) { r.Source.TenantID = "tenant-b" },
		func(r *Request) { r.Source.Kind = SourceWorkflow },
	} {
		other := request
		mutate(&other)
		otherAuthority := &admissionAuthority{snapshot: snapshotFor(other)}
		otherService, err := NewAdmissionService(AdmissionConfig{Authority: otherAuthority, Store: store, Now: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		if _, created, err := otherService.Admit(context.Background(), other); err != nil || !created {
			t.Fatalf("distinct tenant/source kind collided: created=%t err=%v", created, err)
		}
	}
}

func TestTodo_AGENT_015_Fault(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	request := validAdmissionRequest(now)
	store := NewMemoryAdmissionStore()
	authority := &admissionAuthority{err: &AdmissionRefusal{Code: "INSTALLATION_REVOKED"}}
	service, err := NewAdmissionService(AdmissionConfig{Authority: authority, Store: store, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	record, created, err := service.Admit(context.Background(), request)
	if err != nil || !created || record.Decision != DecisionRefused || record.RefusalCode != "INSTALLATION_REVOKED" {
		t.Fatalf("authority refusal = (%+v, %t, %v)", record, created, err)
	}
	if record.Authority != (AuthoritySnapshot{}) {
		t.Fatalf("refusal pinned authority snapshot: %+v", record.Authority)
	}

	transient := errors.New("authority database unavailable")
	other := request
	other.Source.Key = "post:other"
	failed, err := NewAdmissionService(AdmissionConfig{Authority: &admissionAuthority{err: transient}, Store: store, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := failed.Admit(context.Background(), other); !errors.Is(err, transient) {
		t.Fatalf("transient authority error = %v", err)
	}
	if _, err := store.Get(other.Source); err == nil {
		t.Fatal("transient infrastructure failure was persisted as a business refusal")
	}

	badSnapshot := snapshotFor(request)
	badSnapshot.BudgetCeiling.MaxCostMicros = request.Budget.MaxCostMicros - 1
	invalid, err := NewAdmissionService(AdmissionConfig{Authority: &admissionAuthority{snapshot: badSnapshot}, Store: store, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	other.Source.Key = "post:bad-snapshot"
	record, created, err = invalid.Admit(context.Background(), other)
	if err != nil || !created || record.Decision != DecisionRefused || record.RefusalCode != "AUTHORITY_SNAPSHOT_INVALID" {
		t.Fatalf("invalid authority snapshot was accepted: (%+v, %t, %v)", record, created, err)
	}
}

func TestTodo_AGENT_015_SourceConverter(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	request := validAdmissionRequest(now)
	authority := &admissionAuthority{snapshot: snapshotFor(request)}
	store := NewMemoryAdmissionStore()
	service, err := NewAdmissionService(AdmissionConfig{Authority: authority, Store: store, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.AdmitFromSource(context.Background(), request); !errors.Is(err, ErrSourceConverter) {
		t.Fatalf("missing source converter = %v", err)
	}
	request.Source = SourceIdentity{}
	service.converter = sourceConverter{source: validAdmissionRequest(now).Source}
	if record, created, err := service.AdmitFromSource(context.Background(), request); err != nil || !created || record.Decision != DecisionAccepted {
		t.Fatalf("converted admission = (%+v,%t,%v)", record, created, err)
	}
	bad := service
	bad.converter = sourceConverter{err: errors.New("source projection unavailable")}
	if _, _, err := bad.AdmitFromSource(context.Background(), request); err == nil {
		t.Fatal("source conversion failure was accepted")
	}

	request = validAdmissionRequest(now)
	canonical := CanonicalSourceConverter{}
	request.Source.Key = "caller-supplied-key-is-ignored"
	firstSource, err := canonical.ConvertSource(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	secondSource, err := canonical.ConvertSource(context.Background(), request)
	if err != nil || firstSource.Key != secondSource.Key || firstSource.Key == request.Source.Key || firstSource.Ref != request.Source.Ref {
		t.Fatalf("canonical source conversion = (%+v,%+v,%v)", firstSource, secondSource, err)
	}
	changedTarget := request
	changedTarget.InstallationID = "other-installation"
	otherSource, err := canonical.ConvertSource(context.Background(), changedTarget)
	if err != nil || otherSource.Key == firstSource.Key {
		t.Fatalf("different target installation reused source key: %+v, %v", otherSource, err)
	}
	changedPersona := request
	changedPersona.Persona = &PersonaRef{ID: "persona-other", Version: request.Persona.Version, Digest: request.Persona.Digest}
	personaSource, err := canonical.ConvertSource(context.Background(), changedPersona)
	if err != nil || personaSource.Key == firstSource.Key {
		t.Fatalf("different persona reused source key: %+v, %v", personaSource, err)
	}
	changedBudget := request
	changedBudget.Budget.MaxCostMicros++
	stableSource, err := canonical.ConvertSource(context.Background(), changedBudget)
	if err != nil || stableSource.Key != firstSource.Key {
		t.Fatalf("mutable budget changed occurrence key: %+v, %v", stableSource, err)
	}
	request.Source.Ref = " "
	if _, err := canonical.ConvertSource(context.Background(), request); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing occurrence reference = %v", err)
	}

	identity := validAdmissionRequest(now).Source
	first, err := SourceKeyDigest(identity)
	if err != nil {
		t.Fatal(err)
	}
	identity.Key += "-different"
	second, err := SourceKeyDigest(identity)
	if err != nil || first == second || len(first) != 64 {
		t.Fatalf("source-key hashes failed to scope the raw key: %q, %q, %v", first, second, err)
	}
}

func TestTodo_AGENT_015_AdmissionConstruction(t *testing.T) {
	if _, err := NewAdmissionService(AdmissionConfig{Store: NewMemoryAdmissionStore()}); !errors.Is(err, ErrAuthorityMissing) {
		t.Fatalf("missing authority = %v", err)
	}
	if _, err := NewAdmissionService(AdmissionConfig{Authority: &admissionAuthority{}, Store: nil}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing durable store = %v", err)
	}
	var service *AdmissionService
	if _, _, err := service.Admit(context.Background(), validAdmissionRequest(time.Now().UTC())); !errors.Is(err, ErrAuthorityMissing) {
		t.Fatalf("nil service = %v", err)
	}
}

type admissionAuthority struct {
	snapshot AuthoritySnapshot
	err      error
	calls    atomic.Int32
}

type sourceConverter struct {
	source SourceIdentity
	err    error
}

func (c sourceConverter) ConvertSource(context.Context, Request) (SourceIdentity, error) {
	return c.source, c.err
}

func (a *admissionAuthority) VerifyAdmission(_ context.Context, _ Request) (AuthoritySnapshot, error) {
	a.calls.Add(1)
	return a.snapshot, a.err
}

func validAdmissionRequest(now time.Time) Request {
	return Request{
		Source:         SourceIdentity{TenantID: "tenant-a", Kind: SourcePersonaMention, Key: "post-42/persona-3", Ref: "post-42"},
		Persona:        &PersonaRef{ID: "persona-3", Version: "4", Digest: digestForTest("persona-v4")},
		LegalEntity:    "entity-a",
		Agent:          VersionRef{AgentID: "agent-3", Version: "2", Digest: digestForTest("agent-v2")},
		InstallationID: "installation-4",
		Principal:      PrincipalChain{Mode: ModeOnBehalfOf, AgentPrincipalID: "principal-agent-3", InvokerID: "user-7", DelegatedCredentialRef: "credential-ref-7"},
		Purpose:        "persona-mention",
		Audience:       AudienceScope{ID: "conversation-8", SnapshotID: "audience-rev-4", Digest: digestForTest("audience")},
		Context:        ContextScope{ID: "thread-9", SnapshotID: "context-12", Digest: digestForTest("context")},
		Deadline:       now.Add(2 * time.Minute),
		Budget:         Budget{MaxCostMicros: 1000, MaxInputTokens: 4000, MaxOutputTokens: 1000},
		CauseID:        "chat-post-42",
	}
}

func snapshotFor(request Request) AuthoritySnapshot {
	return AuthoritySnapshot{Agent: request.Agent, InstallationID: request.InstallationID, Principal: request.Principal, Audience: request.Audience, Context: request.Context,
		BudgetCeiling: Budget{MaxCostMicros: 2000, MaxInputTokens: 8000, MaxOutputTokens: 2000}, GrantRef: "grant:current", PolicyDigest: digestForTest("policy")}
}

func digestForTest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}
