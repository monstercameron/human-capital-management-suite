package application

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// AGENTUX-076 (AGENTUX-028): verify once where it matters and carry the verified
// authority across the bookkeeping boundaries between. These tests run the real
// foreground authority and the real runstate rechecker with owners that count (and
// optionally delay) each read, and put each mutable authority fact through the
// boundaries a run passes.

type agentux076Owners struct {
	delay                 time.Duration
	persona, policy       int
	epochReads, grantGets int
	builder               int
	epoch                 uint64
	admission             agentinvoke.Admission
	allow                 bool
	clock                 time.Time
}

func (o *agentux076Owners) pause() {
	if o.delay > 0 {
		time.Sleep(o.delay)
	}
}

type agentux076Persona struct{ o *agentux076Owners }

func (p agentux076Persona) Resolve(context.Context, agentinvoke.AdmissionRequest) (agentinvoke.Admission, error) {
	p.o.persona++
	p.o.pause()
	return p.o.admission, nil
}

// agentux076Policy asks the persona authority the way the real T0 policy does.
type agentux076Policy struct{ o *agentux076Owners }

func (p agentux076Policy) IsBoundT0Run(ctx context.Context, request agentinvoke.RunRequest) (bool, error) {
	p.o.policy++
	if _, err := agentUX076Resolve(ctx, agentux076Persona{p.o}, agentinvoke.AdmissionRequest{TenantID: request.TenantID, ConversationID: request.ConversationID, InvokerID: request.InvokerID, PersonaID: request.PersonaID}); err != nil {
		return false, err
	}
	p.o.pause()
	return p.o.allow && request.Grant.TargetAgentID == "agent-a" && !request.Grant.ExpiresAt.IsZero(), nil
}

type agentux076Store struct {
	agentdelegation.GrantStore
	o *agentux076Owners
}

func (s agentux076Store) Get(id string) (agentdelegation.Grant, error) {
	s.o.grantGets++
	s.o.pause()
	return s.GrantStore.Get(id)
}

func (s agentux076Store) CurrentRevocationEpoch(tenant values.TenantId, user string) uint64 {
	s.o.epochReads++
	s.o.pause()
	return s.o.epoch
}

type agentux076Grants struct {
	store agentdelegation.GrantStore
	o     *agentux076Owners
}

func (g agentux076Grants) ForTenant(context.Context, values.TenantId) (agentdelegation.GrantStore, error) {
	return agentux076Store{GrantStore: g.store, o: g.o}, nil
}

type agentux076Admissions struct{ record agentrun.Record }

func (r agentux076Admissions) GetByID(context.Context, string) (agentrun.Record, error) {
	return r.record, nil
}

// agentux076Boundaries is a run's authority as the executor drives it.
type agentux076Boundaries struct {
	t         *testing.T
	owners    *agentux076Owners
	authority *PersonaForegroundRunAuthority
	rechecker *PersonaRunAdmissionRechecker
	request   agentrun.Request
	ctx       context.Context
}

func agentux076NewBoundaries(t *testing.T, delay time.Duration) *agentux076Boundaries {
	t.Helper()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	request := foregroundAuthorityRequest(now)
	owners := &agentux076Owners{delay: delay, epoch: 1, allow: true, clock: now,
		admission: agentinvoke.Admission{Persona: agentinvoke.Persona{ID: "persona-a", Version: "7", InstallationID: "install-a", Current: true}, Installation: agentinvoke.Installation{ID: "install-a", Current: true}, Discoverable: map[string][]string{"skill.read": {"scope:read"}}, HumanMember: true, AudienceMember: true, PersonaInstalled: true}}
	authority := &PersonaForegroundRunAuthority{
		builder: &PersonaRunRequestBuilder{source: foregroundRequestSourceFunc(func(context.Context, agentinvoke.RunRequest) (PersonaRunRequestFacts, error) {
			owners.builder++
			owners.pause()
			return foregroundFactsFromRequest(request), nil
		})},
		persona: agentux076Persona{owners},
		grants:  agentux076Grants{store: foregroundPositiveStore{grant: foregroundPositiveGrant(now), epoch: 1}, o: owners},
		policy:  agentux076Policy{owners}, now: func() time.Time { return owners.clock },
	}
	// What admission recorded: the snapshot the first verification gave.
	base := trust.WithPrincipal(context.Background(), foregroundPrincipal(t, now.Add(-time.Minute), now.Add(time.Hour), "persona-mention", "user-a", "tenant-a"))
	snapshot, err := authority.VerifyAdmission(base, request)
	if err != nil {
		t.Fatal(err)
	}
	id, err := agentrun.AdmissionRequestID(request.Source)
	if err != nil {
		t.Fatal(err)
	}
	record := agentrun.Record{ID: id, Request: request, Decision: agentrun.DecisionAccepted, Authority: snapshot, AdmittedAt: now}
	rechecker, err := NewPersonaRunAdmissionRechecker("tenant-a", agentux076Admissions{record}, authority)
	if err != nil {
		t.Fatal(err)
	}
	ctx, _ := withAgentUXRunTiming(base)
	*owners = agentux076Owners{delay: delay, epoch: owners.epoch, allow: true, clock: now, admission: owners.admission}
	return &agentux076Boundaries{t: t, owners: owners, authority: authority, rechecker: rechecker, request: request, ctx: agentUX076WithRun(ctx)}
}

func (b *agentux076Boundaries) boundary(carry bool) error {
	ctx := b.ctx
	if carry {
		ctx = agentUX076CarryBoundary(ctx)
	}
	return b.rechecker.Recheck(ctx, "tenant-a", b.recordID())
}

func (b *agentux076Boundaries) recordID() string {
	id, _ := agentrun.AdmissionRequestID(b.request.Source)
	return id
}

// beforeValidation is what the output authority does just before the answer is
// written: the run drops what it carried and verifies in full.
func (b *agentux076Boundaries) beforeValidation() error {
	agentUX076InvalidateRun(b.ctx)
	_, err := b.authority.VerifyAdmission(b.ctx, b.request)
	return err
}

// A run claims, then passes the two model-call checkpoints and the document
// search's begin-effect, then validates its answer. It verifies in full at the
// claim and before the answer is written, and probes at the three between.
func TestTodo_AGENTUX_076_Overhead(t *testing.T) {
	carried := agentux076NewBoundaries(t, 0)
	for step, carry := range []bool{false, true, true, true} {
		if err := carried.boundary(carry); err != nil {
			t.Fatalf("boundary %d: %v", step, err)
		}
	}
	if err := carried.beforeValidation(); err != nil {
		t.Fatal(err)
	}
	o := carried.owners
	if o.builder != 2 || o.persona != 2 || o.policy != 2 {
		t.Fatalf("a run verified in full %d times (persona authority read %d, T0 policy %d), want 2, 2 and 2", o.builder, o.persona, o.policy)
	}
	if probes := o.epochReads - o.builder; probes != 3 {
		t.Fatalf("the three carried boundaries probed the revocation epoch %d times", probes)
	}

	// Every boundary verifying in full is what a run did before: four times.
	every := agentux076NewBoundaries(t, 0)
	for step := 0; step < 4; step++ {
		if err := every.boundary(false); err != nil {
			t.Fatalf("boundary %d: %v", step, err)
		}
	}
	if every.owners.builder != 4 || every.owners.persona != 4 {
		t.Fatalf("four plain boundaries verified in full %d times", every.owners.builder)
	}

	// Inside one boundary the verification is shared, carried or not: the helpers
	// that ask again do not read the owners again.
	before := carried.owners.builder
	for i := 0; i < 5; i++ {
		if _, err := carried.authority.VerifyAdmission(carried.ctx, carried.request); err != nil {
			t.Fatal(err)
		}
	}
	if carried.owners.builder != before {
		t.Fatalf("helpers inside one boundary read the owners again: %d -> %d", before, carried.owners.builder)
	}
}

// A grant revoked between admission and an effect is refused at the next
// boundary, carried or not; so are a grant or a session that has run out. The
// installation, the person's membership and the stop controls are read again
// before the answer is written, and refused there.
func TestTodo_AGENTUX_076_Security(t *testing.T) {
	t.Run("grant revoked after the claim", func(t *testing.T) {
		b := agentux076NewBoundaries(t, 0)
		if err := b.boundary(false); err != nil {
			t.Fatal(err)
		}
		b.owners.epoch = 2
		if err := b.boundary(true); err == nil {
			t.Fatal("the model-call checkpoint after the grant was revoked was allowed")
		}
		// And the same revocation is refused by a boundary that does not carry.
		if err := b.boundary(false); err == nil {
			t.Fatal("a full boundary after the grant was revoked was allowed")
		}
	})
	t.Run("grant revoked before the effect", func(t *testing.T) {
		b := agentux076NewBoundaries(t, 0)
		for _, carry := range []bool{false, true} {
			if err := b.boundary(carry); err != nil {
				t.Fatal(err)
			}
		}
		b.owners.epoch = 2
		err := b.boundary(true) // begin-effect of the document search
		if err == nil || !errors.Is(err, errPersonaRunTenantRuntime) {
			t.Fatalf("the document search began after the grant was revoked: %v", err)
		}
	})
	t.Run("grant revoked before the answer is written", func(t *testing.T) {
		b := agentux076NewBoundaries(t, 0)
		for _, carry := range []bool{false, true, true, true} {
			if err := b.boundary(carry); err != nil {
				t.Fatal(err)
			}
		}
		b.owners.epoch = 2
		if err := b.beforeValidation(); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
			t.Fatalf("the answer was written after the grant was revoked: %v", err)
		}
	})
	t.Run("session and grant run out", func(t *testing.T) {
		b := agentux076NewBoundaries(t, 0)
		if err := b.boundary(false); err != nil {
			t.Fatal(err)
		}
		b.owners.clock = b.owners.clock.Add(2 * time.Hour)
		if err := b.boundary(true); err == nil {
			t.Fatal("a carried boundary was allowed after the session and the grant ran out")
		}
	})
	// Facts the probe does not read are refused where the run does read them in
	// full: before its answer is written.
	for name, change := range map[string]func(*agentux076Owners){
		"installation suspended":       func(o *agentux076Owners) { o.admission.Installation.Suspended = true },
		"agent suspended":              func(o *agentux076Owners) { o.admission.Persona.Suspended = true },
		"person left the conversation": func(o *agentux076Owners) { o.admission.HumanMember = false },
		"audience changed":             func(o *agentux076Owners) { o.admission.AudienceMember = false },
		"stop control engaged":         func(o *agentux076Owners) { o.allow = false },
	} {
		t.Run(name, func(t *testing.T) {
			b := agentux076NewBoundaries(t, 0)
			if err := b.boundary(false); err != nil {
				t.Fatal(err)
			}
			change(b.owners)
			for step := 0; step < 3; step++ {
				if err := b.boundary(true); err != nil {
					t.Fatalf("a carried boundary %d read the owners and refused: %v", step, err)
				}
			}
			if err := b.beforeValidation(); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
				t.Fatalf("after %s the answer was allowed to be written: %v", name, err)
			}
			// The next boundary that is not carried refuses it too.
			if err := b.boundary(false); err == nil {
				t.Fatalf("after %s a full boundary was allowed", name)
			}
		})
	}
	t.Run("a boundary the executor does not mark never carries", func(t *testing.T) {
		b := agentux076NewBoundaries(t, 0)
		if err := b.boundary(false); err != nil {
			t.Fatal(err)
		}
		b.owners.admission.Installation.Suspended = true
		if err := b.boundary(false); err == nil {
			t.Fatal("an unmarked boundary leaned on the earlier verification")
		}
	})
}

// With the owners' reads costing what a database round trip costs, the carried
// run's verification takes well under the plain run's, and a run's overhead
// stays inside three seconds around a 300 ms model.
func TestTodo_AGENTUX_076_Performance(t *testing.T) {
	const roundTrip = 40 * time.Millisecond
	measure := func(carry bool) (time.Duration, int) {
		b := agentux076NewBoundaries(t, roundTrip)
		started := time.Now()
		for step := 0; step < 4; step++ {
			if err := b.boundary(carry && step > 0); err != nil {
				t.Fatal(err)
			}
		}
		// A run that verified at every boundary had nothing more to do before its
		// answer; a carrying run verifies once more there.
		if carry {
			if err := b.beforeValidation(); err != nil {
				t.Fatal(err)
			}
		}
		return time.Since(started), b.owners.persona + b.owners.policy + b.owners.grantGets + b.owners.epochReads + b.owners.builder
	}
	plain, plainReads := measure(false)
	carried, carriedReads := measure(true)
	t.Logf("authority across a run, owners at %s a read: every boundary in full %s (%d reads), carried %s (%d reads)", roundTrip, plain.Round(time.Millisecond), plainReads, carried.Round(time.Millisecond), carriedReads)
	if carriedReads >= plainReads || carried >= plain*3/4 {
		t.Fatalf("carrying did not shorten the run's authority: %s/%d reads against %s/%d", carried, carriedReads, plain, plainReads)
	}

	// The run itself, with a model that takes 300 ms a turn.
	const turn = 300 * time.Millisecond
	timedCtx, timing, run, elapsed, err, model, outputs := agentUXSpeedRunWithModelDelay(t, turn)
	if err != nil || run.State != runstate.StateCompleted || model.calls != 2 || outputs != 1 {
		t.Fatalf("run = %s, %d model calls, %d outputs, %v", run.State, model.calls, outputs, err)
	}
	timing.mu.Lock()
	modelTime := timing.stages["model_execute"].Duration
	stages := map[string]time.Duration{}
	for name, metric := range timing.stages {
		stages[name] = metric.Duration
	}
	timing.mu.Unlock()
	if modelTime < 2*turn {
		t.Fatalf("the model's two turns were measured as %s", modelTime)
	}
	if overhead := elapsed - modelTime; overhead > 3*time.Second {
		t.Fatalf("overhead %s around %s of model time, the limit is three seconds", overhead, modelTime)
	}
	names := make([]string, 0, len(stages))
	for name := range stages {
		names = append(names, name)
	}
	sort.Strings(names)
	var line []string
	for _, name := range names {
		line = append(line, name+"="+stages[name].Round(time.Millisecond).String())
	}
	t.Logf("run %s total, model %s, overhead %s; stages: %s", elapsed.Round(time.Millisecond), modelTime.Round(time.Millisecond), (elapsed - modelTime).Round(time.Millisecond), strings.Join(line, " "))
	agentUXSpeedEmit(timedCtx, false)
}

// agentux076Lister is the agent's document reach for the list_documents tool.
type agentux076Lister struct {
	reach PersonaReadableDocuments
	err   error
	calls int
}

func (l *agentux076Lister) ListPersonaReadableDocuments(_ context.Context, _, _, _ string, placed, workspace, _ bool) (PersonaReadableDocuments, error) {
	l.calls++
	if l.err != nil {
		return PersonaReadableDocuments{}, l.err
	}
	out := PersonaReadableDocuments{}
	if placed {
		out.Placed = l.reach.Placed
	}
	if workspace {
		out.Workspace = l.reach.Workspace
	}
	return out, nil
}

// The list_documents tool is offered when the agent reads documents, returns the
// list the facts block carries, is journaled and read again before it leaves, and
// is never mistaken for a source the answer can cite. The tool helpers do not run
// the T0 policy a second time inside the boundary that just verified it.
func TestTodo_AGENTUX_076_ListDocuments(t *testing.T) {
	ctx, tools, _, _, _, journal, record, run := runtimeToolFixture(t)
	lister := &agentux076Lister{reach: PersonaReadableDocuments{Placed: []PersonaReadableDocument{{DocumentID: "doc-pto", Title: "Paid time off policy"}}, Workspace: []PersonaReadableDocument{{DocumentID: "doc-conduct", Title: "Code of conduct"}}}}

	// Without a lister the tool is not offered: the schemas are what they were.
	schemas, err := tools.ToolSchemas(ctx, record, run)
	if err != nil || len(schemas) != 1 || schemas[0].Name != personaDocumentSearchTool {
		t.Fatalf("schemas without a lister = %+v %v", schemas, err)
	}
	tools.cfg.Lister = lister
	schemas, err = tools.ToolSchemas(ctx, record, run)
	if err != nil || len(schemas) != 2 || schemas[1].Name != personaListDocumentsTool || validatePersonaRunToolProjection(schemas) != nil {
		t.Fatalf("schemas with a lister = %+v %v", schemas, err)
	}
	if !personaRunToolProposalProjected(schemas, agentmodel.ToolProposal{ID: "c", Name: personaListDocumentsTool, Arguments: json.RawMessage(`{}`)}) {
		t.Fatal("a list_documents proposal is not accepted as projected")
	}

	proposal := agentmodel.ToolProposal{ID: "call-list", Name: personaListDocumentsTool, Arguments: json.RawMessage(`{}`)}
	output, ref, digest, err := tools.Execute(ctx, record, run, proposal)
	if err != nil || ref == "" || digest != personaRunT0ToolOutputDigest(output) || len(journal.rows) != 1 || journal.rows[0].ToolName != personaListDocumentsTool {
		t.Fatalf("list_documents output=%s ref=%s rows=%d err=%v", output, ref, len(journal.rows), err)
	}
	var listing personaFactsListing
	if json.Unmarshal(output, &listing) != nil || len(listing.PlacedHere) != 1 || listing.PlacedHere[0].Title != "Paid time off policy" || listing.PlacedHereTotal != 1 || len(listing.Workspace) != 0 {
		t.Fatalf("the list = %s", output)
	}
	// The same list the facts block carries, byte for byte.
	same := personaAgentFacts{Reads: true, Documents: PersonaReadableDocuments{Placed: lister.reach.Placed, PlacedTotal: 1}}
	if want, _ := same.personaListDocumentsResult(); !strings.Contains(string(output), `"placed_here":[{"id":"doc-pto","title":"Paid time off policy"}]`) || len(want) == 0 {
		t.Fatalf("the list is not the facts block's: %s", output)
	}

	// Journaled and read again before it leaves: a document the asker can no
	// longer read refuses it; one placed since is not a reason to refuse.
	if err := tools.RecheckPersonaRuntimeToolResult(ctx, journal.rows[0]); err != nil {
		t.Fatalf("a current list was refused: %v", err)
	}
	lister.reach.Placed = append(lister.reach.Placed, PersonaReadableDocument{DocumentID: "doc-new", Title: "New policy"})
	if err := tools.RecheckPersonaRuntimeToolResult(ctx, journal.rows[0]); err != nil {
		t.Fatalf("a document placed since refused the list: %v", err)
	}
	lister.reach.Placed = nil
	if err := tools.RecheckPersonaRuntimeToolResult(ctx, journal.rows[0]); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("a list naming a document the asker can no longer read was allowed: %v", err)
	}
	forged := journal.rows[0]
	forged.Output = []byte(`{"reads_documents":true,"placed_here":[{"id":"doc-secret","title":"Secret"}]}`)
	if err := tools.RecheckPersonaRuntimeToolResult(ctx, forged); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("a list that does not match its digest was allowed: %v", err)
	}
	lister.err = errors.New("document store unavailable")
	if err := tools.RecheckPersonaRuntimeToolResult(ctx, journal.rows[0]); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("a list that could not be read again was allowed: %v", err)
	}
	// A store that cannot list says so; it is not an empty list.
	if out, _, _, err := tools.Execute(ctx, record, run, agentmodel.ToolProposal{ID: "call-down", Name: personaListDocumentsTool, Arguments: json.RawMessage(`{}`)}); err != nil || !strings.Contains(string(out), `"unavailable":true`) {
		t.Fatalf("an unreadable store was reported as %s %v", out, err)
	}
	lister.err = nil

	// Other arguments, an unknown identity or another run are refused.
	for name, bad := range map[string]agentmodel.ToolProposal{
		"not an object": {ID: "x", Name: personaListDocumentsTool, Arguments: json.RawMessage(`[]`)},
		"no call id":    {ID: "", Name: personaListDocumentsTool, Arguments: json.RawMessage(`{}`)},
	} {
		if _, _, _, err := tools.Execute(ctx, record, run, bad); !errors.Is(err, errPersonaRuntimeTools) {
			t.Errorf("%s: %v", name, err)
		}
	}

	// A list is not something a sentence can cite: grounding skips it.
	gateway, err := agentsecurity.NewToolGateway([]agentsecurity.ToolDescriptor{PersonaChatReplyToolDescriptor()})
	if err != nil {
		t.Fatal(err)
	}
	if grounding, err := tools.ReadPersonaRunToolGrounding(ctx, record, run, gateway); err != nil || len(grounding) != 0 {
		t.Fatalf("a list became grounding: %d %v", len(grounding), err)
	}

	// The T0 policy is run once per verification: when the authority check that
	// just answered for this invocation ran that very policy, the tool helpers do
	// not run it again.
	authority, ok := tools.cfg.Policy.authority.(*dynamicT0Authority)
	if !ok {
		t.Fatal("the fixture's persona authority is not the counting one")
	}
	timed, _ := withAgentUXRunTiming(ctx)
	timed = agentUX076WithRun(timed)
	authority.calls = 0
	if _, err := tools.ToolSchemas(timed, record, run); err != nil || authority.calls != 1 {
		t.Fatalf("an unverified run asked the persona authority %d times through its tool schemas: %v", authority.calls, err)
	}
	agentUXSpeedCacheAuthority(timed, record.Request, record.Authority)
	agentUX076NoteVerified(timed, tools.cfg.Policy, 1, time.Now().Add(time.Hour))
	authority.calls = 0
	if _, err := tools.ToolSchemas(timed, record, run); err != nil || authority.calls != 0 {
		t.Fatalf("a verified run asked the persona authority %d times through its tool schemas: %v", authority.calls, err)
	}
	if _, _, _, err := tools.Execute(timed, record, run, proposal); err != nil || authority.calls != 0 {
		t.Fatalf("a verified run asked the persona authority %d times to run a tool: %v", authority.calls, err)
	}
	// A different policy, or an authority that was not verified, is not covered.
	agentUX076NoteVerified(timed, struct{ other bool }{}, 1, time.Now().Add(time.Hour))
	authority.calls = 0
	if _, err := tools.ToolSchemas(timed, record, run); err != nil || authority.calls != 1 {
		t.Fatalf("a different policy was taken for the one that verified: %d reads", authority.calls)
	}
}
