package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type issue3805Catalog struct {
	mu      sync.Mutex
	entries map[string][]AgentUXAmbientWorkflow
	err     error
	calls   int
}

func (c *issue3805Catalog) OfferableWorkflows(_ context.Context, _, person string) ([]AgentUXAmbientWorkflow, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	return c.entries[person], nil
}

func (c *issue3805Catalog) set(person string, entries []AgentUXAmbientWorkflow) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[person] = entries
}

func issue3805Service(t *testing.T) (*AgentUXAmbientService, *agentUXAmbientTestModel, *agentUXAmbientTestEffects, *issue3805Catalog, *time.Time) {
	t.Helper()
	s, m, e := agentUXAmbientFixture(t)
	catalog := &issue3805Catalog{entries: map[string][]AgentUXAmbientWorkflow{"author": issue3805Entries(), "luis": issue3805Entries(), "peer": issue3805Entries()}}
	s.Workflows = catalog
	now := s.Now()
	clock := &now
	s.Now = func() time.Time { return *clock }
	if err := s.SetGrant(t.Context(), "tenant-a", "general", "author", AgentUXAmbientGrant{Agent: AgentUXAmbientWorkflowAgent, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	return s, m, e, catalog, clock
}

func issue3805Offers(t *testing.T, s *AgentUXAmbientService, person string) []AgentUXAmbientOffer {
	t.Helper()
	offers, err := s.ListOffers(t.Context(), "tenant-a", "general", person)
	if err != nil {
		t.Fatal(err)
	}
	var result []AgentUXAmbientOffer
	for _, offer := range offers {
		if offer.Kind == AgentUXAmbientWorkflowKind {
			result = append(result, offer)
		}
	}
	return result
}

const issue3805BankMessage = "I need to update my bank details before payroll Friday"

func TestIssue3805_WorkflowOffer_PrivateOfferAndAcceptStartsNothing_Integration(t *testing.T) {
	s, m, e, _, _ := issue3805Service(t)
	ctx := t.Context()
	agentUXAmbientPost(t, s, "bank", "author", issue3805BankMessage)
	for range 2 {
		if err := s.ProcessWorkflowOffer(ctx, "tenant-a", "general", "bank"); err != nil {
			t.Fatal(err)
		}
	}
	offers := issue3805Offers(t, s, "author")
	if len(offers) != 1 {
		t.Fatalf("replay made %d offers", len(offers))
	}
	offer := offers[0]
	if offer.Scope != "PRIVATE" || offer.Person != "author" || offer.State != "OFFERED" || offer.WorkflowID != "bank-details" || offer.Title != "Change of bank details" || offer.Revision != 1 {
		t.Fatalf("offer %+v", offer)
	}
	if strings.Contains(offer.Title+offer.Reason, "payroll") || strings.Contains(strings.ToLower(offer.Title), "i need") {
		t.Fatalf("offer repeats the message: %+v", offer)
	}
	if m.calls != 0 {
		t.Fatalf("a classifier offer called the model %d times", m.calls)
	}
	// Nobody else in the room sees it, or can answer it.
	for _, person := range []string{"luis", "peer"} {
		if got := issue3805Offers(t, s, person); len(got) != 0 {
			t.Fatalf("private offer leaked to %s: %+v", person, got)
		}
		if _, err := s.Control(ctx, "tenant-a", "general", person, AgentUXAmbientCommand{ID: offer.ID, Action: "OPEN", ExpectedRevision: 1}); !errors.Is(err, ErrAgentUXAmbientDenied) {
			t.Fatalf("%s answered the author's offer: %v", person, err)
		}
	}
	if _, err := s.ListOffers(ctx, "tenant-b", "general", "author"); !errors.Is(err, ErrAgentUXAmbientDenied) {
		t.Fatalf("another tenant read the room: %v", err)
	}
	// A stale revision cannot act.
	if _, err := s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: offer.ID, Action: "OPEN", ExpectedRevision: 9}); !errors.Is(err, ErrAgentUXAmbientInvalid) {
		t.Fatalf("stale revision: %v", err)
	}
	accepted, err := s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: offer.ID, Action: "OPEN", ExpectedRevision: 1})
	if err != nil || accepted.State != "ACCEPTED" || accepted.Revision != 2 {
		t.Fatalf("accept %+v %v", accepted, err)
	}
	again, err := s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: offer.ID, Action: "OPEN", ExpectedRevision: 2})
	if err != nil || again.State != "ACCEPTED" || again.Revision != 2 {
		t.Fatalf("accepting twice changed the offer %+v %v", again, err)
	}
	// Accepting is a pointer to the workflow's own page, never an effect.
	if e.set != 0 || e.cancel != 0 {
		t.Fatalf("accepting scheduled work: %+v", e)
	}
	if tasks, err := s.ListTasks(ctx, "tenant-a", "author"); err != nil || len(tasks) != 0 {
		t.Fatalf("accepting added a task %+v %v", tasks, err)
	}
	if href := AgentUXAmbientWorkflowStartHref(accepted.WorkflowID); !strings.HasSuffix(href, "/start/bank-details") || !strings.HasPrefix(href, "/") {
		t.Fatalf("start href %q", href)
	}
}

func TestIssue3805_WorkflowOffer_ConsentIsRequired_Security_Integration(t *testing.T) {
	s, _, _, catalog, _ := issue3805Service(t)
	ctx := t.Context()
	// A member cannot switch the room's consent on, and nothing is read without it.
	if err := s.SetGrant(ctx, "tenant-a", "general", "peer", AgentUXAmbientGrant{Agent: AgentUXAmbientWorkflowAgent, Enabled: true}); !errors.Is(err, ErrAgentUXAmbientDenied) {
		t.Fatalf("member granted consent: %v", err)
	}
	if err := s.SetGrant(ctx, "tenant-a", "other", "author", AgentUXAmbientGrant{Agent: AgentUXAmbientWorkflowAgent, Enabled: true, AutomaticPublic: true}); !errors.Is(err, ErrAgentUXAmbientInvalid) {
		t.Fatalf("automatic public workflow offers: %v", err)
	}
	agentUXAmbientPost(t, s, "no-grant", "author", issue3805BankMessage)
	if err := s.ProcessWorkflowOffer(ctx, "tenant-a", "other", "no-grant"); err != nil {
		t.Fatal(err)
	}
	// A conversation with no grant is not read; neither is a direct one.
	if err := s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,created_at) VALUES('dm','tenant-a','direct','author','tenant-a',1,$1,$2)`, issue3805BankMessage, s.Now().Add(time.Second))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessWorkflowOffer(ctx, "tenant-a", "direct", "dm"); err != nil {
		t.Fatal(err)
	}
	if catalog.calls != 0 {
		t.Fatalf("catalog read for an unconsented conversation: %d", catalog.calls)
	}
	var n int
	if err := s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM agentux_ambient_offer WHERE kind='WORKFLOW_START'`).Scan(&n)
	}); err != nil || n != 0 {
		t.Fatalf("offers without consent: %d %v", n, err)
	}
	// "Don't act on my messages" keeps the author's messages out.
	if err := s.SetOptOut(ctx, "tenant-a", "general", "author", true); err != nil {
		t.Fatal(err)
	}
	agentUXAmbientPost(t, s, "opted-out", "author", issue3805BankMessage)
	if err := s.ProcessWorkflowOffer(ctx, "tenant-a", "general", "opted-out"); err != nil {
		t.Fatal(err)
	}
	if got := issue3805Offers(t, s, "author"); len(got) != 0 {
		t.Fatalf("offered to a person who opted out: %+v", got)
	}
	// A message from before consent is not read either.
	if err := s.SetOptOut(ctx, "tenant-a", "general", "author", false); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,created_at) VALUES('old','tenant-a','general','author','tenant-a',99,$1,$2)`, issue3805BankMessage, s.Now().Add(-48*time.Hour))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessWorkflowOffer(ctx, "tenant-a", "general", "old"); err != nil {
		t.Fatal(err)
	}
	if got := issue3805Offers(t, s, "author"); len(got) != 0 {
		t.Fatalf("offered from a message posted before consent: %+v", got)
	}
}

func TestIssue3805_WorkflowOffer_NotNowAndNeverAreRemembered_Integration(t *testing.T) {
	s, _, _, _, clock := issue3805Service(t)
	ctx := t.Context()
	post := func(id, author, body string) {
		agentUXAmbientPost(t, s, id, author, body)
		if err := s.ProcessWorkflowOffer(ctx, "tenant-a", "general", id); err != nil {
			t.Fatal(err)
		}
	}
	post("bank-1", "author", issue3805BankMessage)
	offer := issue3805Offers(t, s, "author")[0]
	dismissed, err := s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: offer.ID, Action: "DISMISS", ExpectedRevision: offer.Revision})
	if err != nil || dismissed.State != "DISMISSED" {
		t.Fatalf("not now %+v %v", dismissed, err)
	}
	if got := issue3805Offers(t, s, "author"); len(got) != 0 {
		t.Fatalf("dismissed offer still listed: %+v", got)
	}
	// Not now: the same workflow is not offered again to the same person for a
	// day, while another person is still offered it.
	post("bank-2", "author", "I want to change my bank account too")
	if got := issue3805Offers(t, s, "author"); len(got) != 0 {
		t.Fatalf("not now was not remembered: %+v", got)
	}
	post("bank-luis", "luis", issue3805BankMessage)
	if got := issue3805Offers(t, s, "luis"); len(got) != 1 {
		t.Fatalf("another person lost their offer: %+v", got)
	}
	// A different workflow for the same person is not muted.
	post("leave-1", "author", "I'd like to take annual leave in March")
	if got := issue3805Offers(t, s, "author"); len(got) != 1 || got[0].WorkflowID != "leave" {
		t.Fatalf("another workflow muted: %+v", got)
	}
	*clock = clock.Add(25 * time.Hour)
	post("bank-3", "author", "I have to update my bank details again")
	var bank AgentUXAmbientOffer
	for _, o := range issue3805Offers(t, s, "author") {
		if o.WorkflowID == "bank-details" {
			bank = o
		}
	}
	if bank.ID == "" {
		t.Fatal("not now never expired")
	}
	// Never: stays muted across time, for every later message.
	if _, err = s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: bank.ID, Action: "NEVER", ExpectedRevision: bank.Revision}); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(90 * 24 * time.Hour)
	post("bank-4", "author", "I need to update my bank details")
	for _, o := range issue3805Offers(t, s, "author") {
		if o.WorkflowID == "bank-details" {
			t.Fatalf("never was not remembered: %+v", o)
		}
	}
	// "Never" is stored as no end date, not as a long pause.
	var until *time.Time
	if err = s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT muted_until FROM agentux_ambient_workflow_mute WHERE tenant_id='tenant-a' AND person_id='author' AND workflow_id='bank-details'`).Scan(&until)
	}); err != nil || until != nil {
		t.Fatalf("never became a pause: %v %v", until, err)
	}
}

func TestIssue3805_WorkflowOffer_SourceChangesAreHonoured_Integration(t *testing.T) {
	s, _, _, _, _ := issue3805Service(t)
	ctx := t.Context()
	agentUXAmbientPost(t, s, "edit", "author", issue3805BankMessage)
	if err := s.ProcessWorkflowOffer(ctx, "tenant-a", "general", "edit"); err != nil {
		t.Fatal(err)
	}
	offer := issue3805Offers(t, s, "author")[0]
	edit := func(body string, deleted bool) {
		if err := s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE chat_post SET body=$1,revision=revision+1,tombstoned=$2 WHERE tenant_id='tenant-a' AND id='edit'`, body, deleted)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	// An edit the outbox has not processed yet: the card cannot be accepted and
	// shows no title.
	edit("I need to update my bank details before Friday", false)
	stale := issue3805Offers(t, s, "author")
	if len(stale) != 1 || stale[0].State != "SOURCE_UNAVAILABLE" || stale[0].Title != "" {
		t.Fatalf("stale card %+v", stale)
	}
	if _, err := s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: offer.ID, Action: "OPEN", ExpectedRevision: offer.Revision}); !errors.Is(err, ErrAgentUXAmbientDenied) {
		t.Fatalf("accepted an offer for a changed message: %v", err)
	}
	// Once read again, the offer is fresh for the new revision and can be accepted.
	if err := s.ProcessWorkflowOffer(ctx, "tenant-a", "general", "edit"); err != nil {
		t.Fatal(err)
	}
	fresh := issue3805Offers(t, s, "author")
	if len(fresh) != 1 || fresh[0].State != "OFFERED" || fresh[0].SourceRevision != 2 || fresh[0].Revision != offer.Revision+1 {
		t.Fatalf("re-read offer %+v", fresh)
	}
	// An edit that no longer asks for it withdraws the offer.
	edit("Payroll uses the bank details on file", false)
	if err := s.ProcessWorkflowOffer(ctx, "tenant-a", "general", "edit"); err != nil {
		t.Fatal(err)
	}
	withdrawn := issue3805Offers(t, s, "author")
	if len(withdrawn) != 1 || withdrawn[0].State != "CANCELLED" || withdrawn[0].Title != "" {
		t.Fatalf("withdrawn offer %+v", withdrawn)
	}
	if _, err := s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: offer.ID, Action: "OPEN", ExpectedRevision: withdrawn[0].Revision}); !errors.Is(err, ErrAgentUXAmbientDenied) && !errors.Is(err, ErrAgentUXAmbientInvalid) {
		t.Fatalf("accepted a withdrawn offer: %v", err)
	}
	// Dismissing a withdrawn card is allowed and is not a "Not now" for the workflow.
	if _, err := s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: offer.ID, Action: "DISMISS", ExpectedRevision: withdrawn[0].Revision}); err != nil {
		t.Fatal(err)
	}
	agentUXAmbientPost(t, s, "bank-after", "author", "I need to change my bank account")
	if err := s.ProcessWorkflowOffer(ctx, "tenant-a", "general", "bank-after"); err != nil {
		t.Fatal(err)
	}
	if got := issue3805Offers(t, s, "author"); len(got) != 1 || got[0].State != "OFFERED" {
		t.Fatalf("dismissing a withdrawn offer muted the workflow: %+v", got)
	}
	// A deleted message cancels what it offered.
	edit(issue3805BankMessage, true)
	if err := s.ProcessWorkflowOffer(ctx, "tenant-a", "general", "edit"); err != nil {
		t.Fatal(err)
	}
}

func TestIssue3805_WorkflowOffer_AcceptRechecksAccessAndCatalog_Security_Integration(t *testing.T) {
	s, _, e, catalog, _ := issue3805Service(t)
	ctx := t.Context()
	offerOne := func(id string) AgentUXAmbientOffer {
		agentUXAmbientPost(t, s, id, "author", issue3805BankMessage)
		if err := s.ProcessWorkflowOffer(ctx, "tenant-a", "general", id); err != nil {
			t.Fatal(err)
		}
		for _, o := range issue3805Offers(t, s, "author") {
			if o.Source == id {
				return o
			}
		}
		t.Fatalf("no offer for %s", id)
		return AgentUXAmbientOffer{}
	}
	// The workflow stops being offerable (untagged, withdrawn or no longer
	// startable by this person) before the person presses Start: nothing starts
	// and the card says why.
	unavailable := offerOne("gone")
	catalog.set("author", []AgentUXAmbientWorkflow{{ID: "leave", Name: "Leave request", Cues: []string{"time off"}}})
	got, err := s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: unavailable.ID, Action: "OPEN", ExpectedRevision: unavailable.Revision})
	if err != nil || got.State != "UNAVAILABLE" || got.Reason != "workflow_unavailable" {
		t.Fatalf("withdrawn workflow %+v %v", got, err)
	}
	// The workflow's name is refreshed from the catalog at acceptance.
	catalog.set("author", []AgentUXAmbientWorkflow{{ID: "bank-details", Name: "Update bank details", Cues: []string{"bank details"}}})
	renamed := offerOne("renamed")
	accepted, err := s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: renamed.ID, Action: "OPEN", ExpectedRevision: renamed.Revision})
	if err != nil || accepted.State != "ACCEPTED" || accepted.Title != "Update bank details" {
		t.Fatalf("renamed workflow %+v %v", accepted, err)
	}
	// A person who left the room cannot answer or see what they were offered.
	revoked := offerOne("revoked")
	if err = s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_membership SET state='removed',left_at=$1 WHERE tenant_id='tenant-a' AND conversation_id='general' AND member_id='author'`, s.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: revoked.ID, Action: "OPEN", ExpectedRevision: revoked.Revision}); !errors.Is(err, ErrAgentUXAmbientDenied) {
		t.Fatalf("a former member accepted: %v", err)
	}
	if _, err = s.ListOffers(ctx, "tenant-a", "general", "author"); !errors.Is(err, ErrAgentUXAmbientDenied) {
		t.Fatalf("a former member read offers: %v", err)
	}
	if e.set != 0 || e.cancel != 0 {
		t.Fatalf("an offer had an effect: %+v", e)
	}
}

func TestIssue3805_WorkflowOffer_BudgetAndFrequency_Integration(t *testing.T) {
	s, _, _, _, _ := issue3805Service(t)
	ctx := t.Context()
	bodies := []string{"I need to update my bank details", "I want to change my bank account", "I have to book time off", "I'd like to take annual leave", "I must update my bank details now"}
	for i, body := range bodies {
		id := "p" + string(rune('a'+i))
		agentUXAmbientPost(t, s, id, "author", body)
		if err := s.ProcessWorkflowOffer(ctx, "tenant-a", "general", id); err != nil {
			t.Fatal(err)
		}
	}
	// Three a day per person per conversation, however many messages match, and
	// the same workflow is not offered twice while the first offer is open.
	offers := issue3805Offers(t, s, "author")
	if len(offers) > agentUXAmbientWorkflowPersonDailyLimit || len(offers) != 2 {
		t.Fatalf("offers per person: %d %+v", len(offers), offers)
	}
	seen := map[string]bool{}
	for _, o := range offers {
		if seen[o.WorkflowID] {
			t.Fatalf("the same workflow offered twice: %+v", offers)
		}
		seen[o.WorkflowID] = true
	}
	// Counted against the same read journal as every ambient offer, so a
	// grant's daily limit applies too.
	if err := s.SetGrant(ctx, "tenant-a", "general", "author", AgentUXAmbientGrant{Agent: AgentUXAmbientWorkflowAgent, Enabled: true, ConversationLimit: 1, DailyLimit: 1}); err != nil {
		t.Fatal(err)
	}
	agentUXAmbientPost(t, s, "luis-1", "luis", "I need to update my bank details")
	if err := s.ProcessWorkflowOffer(ctx, "tenant-a", "general", "luis-1"); err != nil {
		t.Fatal(err)
	}
	if got := issue3805Offers(t, s, "luis"); len(got) != 0 {
		t.Fatalf("offer past the grant's limit: %+v", got)
	}
	grants, _, err := s.Grants(ctx, "tenant-a", "general", "author")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, g := range grants {
		if g.Agent == AgentUXAmbientWorkflowAgent {
			found = true
			if !g.Enabled || !g.Paused {
				t.Fatalf("workflow grant %+v", g)
			}
		}
	}
	if !found {
		t.Fatalf("the workflow offers are not in the room's list of readers: %+v", grants)
	}
}

func TestIssue3805_WorkflowOffer_CatalogFailureOffersNothing_Integration(t *testing.T) {
	s, _, _, catalog, _ := issue3805Service(t)
	ctx := t.Context()
	catalog.err = errors.New("catalog offline")
	agentUXAmbientPost(t, s, "offline", "author", issue3805BankMessage)
	if err := s.ProcessWorkflowOffer(ctx, "tenant-a", "general", "offline"); err != nil {
		t.Fatalf("a catalog outage must not stall the queue: %v", err)
	}
	if got := issue3805Offers(t, s, "author"); len(got) != 0 {
		t.Fatalf("offered without a catalog: %+v", got)
	}
	// Without a catalog the service never offers and the grant cannot be made.
	bare := *s
	bare.Workflows = nil
	if err := bare.ProcessWorkflowOffer(ctx, "tenant-a", "general", "offline"); !errors.Is(err, ErrAgentUXAmbientInvalid) {
		t.Fatalf("no catalog: %v", err)
	}
	if err := bare.SetGrant(ctx, "tenant-a", "other", "author", AgentUXAmbientGrant{Agent: AgentUXAmbientWorkflowAgent, Enabled: true}); !errors.Is(err, ErrAgentUXAmbientInvalid) {
		t.Fatalf("grant without a catalog: %v", err)
	}
}

// A card shown as withdrawn because its message was edited (the offer row still
// reads OFFERED until the outbox re-reads it) is closed without pausing the
// workflow, while a real "never" is stored with no end date.
func TestIssue3805_WorkflowOffer_ClosingAStaleCardDoesNotMute_Integration(t *testing.T) {
	s, _, _, _, _ := issue3805Service(t)
	ctx := t.Context()
	agentUXAmbientPost(t, s, "stale", "author", issue3805BankMessage)
	if err := s.ProcessWorkflowOffer(ctx, "tenant-a", "general", "stale"); err != nil {
		t.Fatal(err)
	}
	offer := issue3805Offers(t, s, "author")[0]
	if err := s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post SET revision=revision+1 WHERE tenant_id='tenant-a' AND id='stale'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: offer.ID, Action: "DISMISS", ExpectedRevision: offer.Revision}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM agentux_ambient_workflow_mute`).Scan(&n)
	}); err != nil || n != 0 {
		t.Fatalf("closing a withdrawn card muted the workflow: %d %v", n, err)
	}
	// A real "never" is stored with no end date.
	agentUXAmbientPost(t, s, "fresh", "author", "I want to change my bank account")
	if err := s.ProcessWorkflowOffer(ctx, "tenant-a", "general", "fresh"); err != nil {
		t.Fatal(err)
	}
	fresh := issue3805Offers(t, s, "author")[0]
	if _, err := s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: fresh.ID, Action: "NEVER", ExpectedRevision: fresh.Revision}); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM agentux_ambient_workflow_mute WHERE person_id='author' AND workflow_id='bank-details' AND muted_until IS NULL`).Scan(&n)
	}); err != nil || n != 1 {
		t.Fatalf("never was not stored: %d %v", n, err)
	}
}
