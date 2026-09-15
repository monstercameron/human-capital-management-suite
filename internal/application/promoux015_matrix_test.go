package application

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/latencygate"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
)

// promoux015CommittedOnce is the harness's exactly-once oracle: between two
// effect snapshots a promotion commit must add exactly one promotion outcome
// ledger event and exactly one promotion outcome outbox record. It is a named
// function so TestTodo_PROMOUX_015_Mutation can prove it is not vacuous.
func promoux015CommittedOnce(before, after promoux015Effects) error {
	if after.outcomeEvents-before.outcomeEvents != 1 || after.outcomeOutbox-before.outcomeOutbox != 1 {
		return fmt.Errorf("promotion outcome effects moved by %d ledger events and %d outbox records, want exactly 1 each (%+v -> %+v)",
			after.outcomeEvents-before.outcomeEvents, after.outcomeOutbox-before.outcomeOutbox, before, after)
	}
	return nil
}

// promoux015ReviewedClosed is the review oracle: the proposer's own listed
// journey reached its terminal stage and names the proposer as its initiator.
//
// WF-RUN-034: served revalidation now runs for real and, with no durable
// GOVERN-002 historical decision to confirm against, closes the promotion
// PROMOTION_BLOCKED, which the journey projects as stage BLOCKED. The
// projection's journeyStageClosed does not yet treat that workflow terminal
// as closed (it reads BLOCKED as a proposal to correct), so this oracle no
// longer requires responsibility CLOSED; that projection gap is reported
// with WF-RUN-034 rather than asserted away here.
func promoux015ReviewedClosed(j *journeyv1.Journey) error {
	if j.GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED {
		return fmt.Errorf("stage = %s, want BLOCKED", j.GetStage())
	}
	if !promoux015HasRelationship(j.GetViewer(), journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_INITIATOR) {
		return fmt.Errorf("viewer relationships %v do not name the initiator", j.GetViewer().GetRelationships())
	}
	return nil
}

// promoux015Unchanged is the zero-effect oracle every refused, stale,
// duplicated, replayed or reconnected variant must satisfy.
func promoux015Unchanged(before, after promoux015Effects) error {
	if after.outcomeEvents != before.outcomeEvents || after.outcomeOutbox != before.outcomeOutbox || after.commitReceipts != before.commitReceipts {
		return fmt.Errorf("a promotion outcome effect was written: %+v -> %+v", before, after)
	}
	return nil
}

// promoux015Deny asserts err is a gRPC refusal with code.
func promoux015Deny(t *testing.T, err error, want codes.Code, what string) {
	t.Helper()
	promoux015Code(t, err, want, what)
}

// restart stops the composed server and composes a fresh one over the same
// PostgreSQL database, re-dialling the gRPC client: nothing a test holds in
// memory survives, only what the database recorded.
func (h *promoux015Harness) restart() {
	h.t.Helper()
	stopCtx, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	if err := h.composed.Stop(stopCtx); err != nil {
		h.t.Fatalf("stop the composed server: %v", err)
	}
	composed, err := ComposeServe(context.Background(), ServeInput{Config: h.cfg, Pool: h.pool, Identity: "promoux015-restarted-" + uuid.NewString()})
	if err != nil {
		h.t.Fatalf("recompose over the same database: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.t.Cleanup(func() {
		cancel()
		c, s := context.WithTimeout(context.Background(), 20*time.Second)
		defer s()
		_ = composed.Stop(c)
	})
	if err := composed.Start(ctx); err != nil {
		h.t.Fatalf("start the recomposed server: %v", err)
	}
	h.composed = composed
	h.redial()
}

// redial replaces the gRPC client with a new connection, as a browser or CLI
// reconnecting after a dropped transport would.
func (h *promoux015Harness) redial() {
	h.t.Helper()
	conn, err := grpc.NewClient(h.composed.GRPCAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		h.t.Fatalf("redial: %v", err)
	}
	h.t.Cleanup(func() { _ = conn.Close() })
	h.client = journeyv1.NewJourneyServiceClient(conn)
}

// TestTodo_PROMOUX_015_Integration runs the fixture's failure variants on the
// production path and proves each writes no promotion outcome: a denial, a
// stale decision against an approval already decided, a duplicate start for a
// worker already in flight, and a reconnect that must neither lose nor repeat
// the journey.
func TestTodo_PROMOUX_015_Integration(t *testing.T) {
	h := promoux015Compose(t)

	t.Run("a denial records its one terminal fact and nothing more", func(t *testing.T) {
		before := h.effects()
		id := h.proposeAndExecute()
		rejected, err := h.client.DecideJourney(h.rpc("finance-partner"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: false, Reason: "not within budget"})
		if err != nil {
			t.Fatalf("DecideJourney(reject) as the finance partner: %v", err)
		}
		if got := rejected.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED {
			t.Fatalf("rejected stage = %s, want REJECTED", got)
		}
		// A rejection is a terminal outcome and the workflow records it once
		// (hcmnext.workflow.PromotionOutcome at REJECTED); no approval that
		// follows, no effective-date wait and no second record may follow it.
		denied := h.effects()
		if err := promoux015CommittedOnce(before, denied); err != nil {
			t.Fatalf("the rejection's terminal record: %v", err)
		}
		_, err = h.client.DecideJourney(h.rpc("admin"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "approve after rejection"})
		if err == nil {
			t.Fatal("a manager decision after the rejection was accepted")
		}
		if fired, err := h.scheduler(h.afterEffectiveDate()).Tick(context.Background()); err != nil || fired.Fired != 0 {
			t.Fatalf("Tick after a rejection = %+v, %v; want nothing fired", fired, err)
		}
		if err := promoux015Unchanged(denied, h.effects()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("a duplicate start, a stale decision and a reconnect write nothing", func(t *testing.T) {
		before := h.effects()
		req, _ := h.discoverPromotion("hiring-manager")
		req.EffectiveDate = time.Now().UTC().AddDate(0, 2, 0).Format(time.DateOnly)
		proposed, err := h.client.ProposeJourney(h.rpc("hiring-manager"), req)
		if err != nil {
			t.Fatalf("ProposeJourney: %v", err)
		}
		id := proposed.GetJourney().GetIntentId()

		_, err = h.client.ProposeJourney(h.rpc("hiring-manager"), req)
		promoux015Deny(t, err, codes.AlreadyExists, "a duplicate start for a worker already in flight")

		if _, err := h.client.ExecuteJourney(h.rpc("admin"), &journeyv1.ExecuteJourneyRequest{IntentId: id}); err != nil {
			t.Fatalf("ExecuteJourney: %v", err)
		}
		if _, err := h.client.DecideJourney(h.rpc("finance-partner"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "finance approves"}); err != nil {
			t.Fatalf("DecideJourney(finance): %v", err)
		}
		_, err = h.client.DecideJourney(h.rpc("finance-partner"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "finance approves again"})
		promoux015Deny(t, err, codes.PermissionDenied, "a stale second decision by the principal who already decided")

		beforeReconnect, err := h.client.InspectJourney(h.rpc("admin"), &journeyv1.InspectJourneyRequest{IntentId: id})
		if err != nil {
			t.Fatalf("InspectJourney before reconnect: %v", err)
		}
		h.redial()
		afterReconnect, err := h.client.InspectJourney(h.rpc("admin"), &journeyv1.InspectJourneyRequest{IntentId: id})
		if err != nil {
			t.Fatalf("InspectJourney after reconnect: %v", err)
		}
		if beforeReconnect.GetDetail().GetDetailDigest() != afterReconnect.GetDetail().GetDetailDigest() ||
			afterReconnect.GetDetail().GetJourney().GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL {
			t.Fatalf("reconnect changed the journey: %s/%s -> %s/%s",
				beforeReconnect.GetDetail().GetJourney().GetStage(), beforeReconnect.GetDetail().GetDetailDigest(),
				afterReconnect.GetDetail().GetJourney().GetStage(), afterReconnect.GetDetail().GetDetailDigest())
		}
		if err := promoux015Unchanged(before, h.effects()); err != nil {
			t.Fatal(err)
		}
	})
}

// TestTodo_PROMOUX_015_Recovery restarts the whole composed server between the
// two approvals, over the same database, and proves the promotion continues
// from what PostgreSQL recorded -- the second approval, the effective-date
// wait and exactly one commit -- with no approval repeated or lost.
func TestTodo_PROMOUX_015_Recovery(t *testing.T) {
	h := promoux015Compose(t)
	before := h.effects()
	req, _ := h.discoverPromotion("hiring-manager")
	proposed, err := h.client.ProposeJourney(h.rpc("hiring-manager"), req)
	if err != nil {
		t.Fatalf("ProposeJourney: %v", err)
	}
	id := proposed.GetJourney().GetIntentId()
	if _, err := h.client.ExecuteJourney(h.rpc("admin"), &journeyv1.ExecuteJourneyRequest{IntentId: id}); err != nil {
		t.Fatalf("ExecuteJourney: %v", err)
	}
	if _, err := h.client.DecideJourney(h.rpc("finance-partner"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "finance approves"}); err != nil {
		t.Fatalf("DecideJourney(finance): %v", err)
	}

	h.restart()

	recovered, err := h.client.InspectJourney(h.rpc("admin"), &journeyv1.InspectJourneyRequest{IntentId: id})
	if err != nil {
		t.Fatalf("InspectJourney after restart: %v", err)
	}
	if got := recovered.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL {
		t.Fatalf("recovered stage = %s, want MANAGER_APPROVAL", got)
	}
	_, err = h.client.DecideJourney(h.rpc("finance-partner"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "repeat after restart"})
	promoux015Deny(t, err, codes.PermissionDenied, "the finance partner deciding again after a restart")
	approved := h.effects()
	if _, err := h.client.DecideJourney(h.rpc("admin"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "manager approves"}); err != nil {
		t.Fatalf("DecideJourney(manager) after restart: %v", err)
	}
	if fired, err := h.scheduler(h.afterEffectiveDate()).Tick(context.Background()); err != nil || fired.Fired != 1 {
		t.Fatalf("Tick after restart = %+v, %v; want exactly one timer fired", fired, err)
	}
	if err := promoux015CommittedOnce(approved, h.effects()); err != nil {
		t.Fatal(err)
	}
	h.restart()
	if fired, err := h.scheduler(h.afterEffectiveDate()).Tick(context.Background()); err != nil || fired.Fired != 0 {
		t.Fatalf("Tick after a second restart = %+v, %v; want nothing fired", fired, err)
	}
	if err := promoux015CommittedOnce(before, h.effects()); err != nil {
		t.Fatalf("across both restarts: %v", err)
	}
}

// TestTodo_PROMOUX_015_Security proves each persona sees and does only what
// its own authority admits on the live path: conflicted approvals are refused
// before any write, the employee cannot reach the workforce or the journey,
// the listing's reporting-line visibility expands only a manager's own
// reports, and no ordinary reviewer receives raw work item identifiers.
func TestTodo_PROMOUX_015_Security(t *testing.T) {
	h := promoux015Compose(t)
	id := h.proposeAndExecute()
	before := h.effects()

	for _, refused := range []string{"hiring-manager", "admin", "individual-contributor"} {
		_, err := h.client.DecideJourney(h.rpc(refused), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "conflicted"})
		promoux015Deny(t, err, codes.PermissionDenied, "a conflicted finance decision by "+refused)
	}
	if err := promoux015Unchanged(before, h.effects()); err != nil {
		t.Fatal(err)
	}

	if j := h.journeyFor("individual-contributor", id); j != nil {
		t.Fatalf("the employee persona can see the promotion journey %s", j.GetIntentId())
	}

	proposerView, err := h.client.ListWorkers(h.rpc("hiring-manager"), &journeyv1.ListWorkersRequest{})
	if err != nil {
		t.Fatalf("ListWorkers as the proposer: %v", err)
	}
	for _, w := range proposerView.GetWorkers() {
		if w.GetWorkerRef() == promoux015Finance {
			t.Fatalf("the proposer's reporting-line expansion disclosed %s, who does not report to it", promoux015Finance)
		}
	}

	var workItemIDs []string
	rows, err := h.pool.Query(context.Background(), `SELECT work_item_id::text FROM work_item`)
	if err != nil {
		t.Fatalf("read work item ids: %v", err)
	}
	for rows.Next() {
		var wid string
		if err := rows.Scan(&wid); err != nil {
			t.Fatalf("scan work item id: %v", err)
		}
		workItemIDs = append(workItemIDs, wid)
	}
	rows.Close()
	proposerJourneys, err := h.client.ListJourneys(h.rpc("hiring-manager"), &journeyv1.ListJourneysRequest{})
	if err != nil {
		t.Fatalf("ListJourneys as the proposer: %v", err)
	}
	for _, wid := range workItemIDs {
		if strings.Contains(proposerJourneys.String(), wid) {
			t.Fatalf("the proposer's journey list leaks raw work item id %s", wid)
		}
	}
}

// TestTodo_PROMOUX_015_Mutation proves the harness's own oracles are not
// vacuous by planting the defects they exist to catch into real rows and real
// projections and observing each oracle refuse them.
func TestTodo_PROMOUX_015_Mutation(t *testing.T) {
	h := promoux015Compose(t)
	before := h.effects()
	id := h.runSeparatedPromotion()
	approved := h.effects()
	if fired, err := h.scheduler(h.afterEffectiveDate()).Tick(context.Background()); err != nil || fired.Fired != 1 {
		t.Fatalf("Tick = %+v, %v; want exactly one timer fired", fired, err)
	}
	committed := h.effects()
	if err := promoux015CommittedOnce(approved, committed); err != nil {
		t.Fatalf("the unmutated journey fails its own oracle: %v", err)
	}

	t.Run("a zero-effect variant that wrote an outcome is caught", func(t *testing.T) {
		if promoux015Unchanged(before, committed) == nil {
			t.Fatal("promoux015Unchanged accepted a committed promotion as zero-effect")
		}
	})

	t.Run("a duplicated outcome effect is caught", func(t *testing.T) {
		tag, err := h.pool.Exec(context.Background(), `
			INSERT INTO outbox (tenant_id, outbox_id, effect_identity, ordering_key, schema_ref, payload, status)
			SELECT tenant_id, gen_random_uuid(), effect_identity || ':planted-duplicate', ordering_key, schema_ref, payload, status
			FROM outbox WHERE schema_ref ILIKE '%PromotionOutcome%' LIMIT 1`)
		if err != nil {
			t.Fatalf("plant a duplicate outcome outbox record: %v", err)
		}
		if tag != 1 {
			t.Fatalf("planted %d duplicate outbox records, want 1", tag)
		}
		if promoux015CommittedOnce(approved, h.effects()) == nil {
			t.Fatal("promoux015CommittedOnce accepted two promotion outcome outbox records as exactly once")
		}
	})

	t.Run("a recorded journey still asking its initiator to act is caught", func(t *testing.T) {
		j := h.journeyFor("hiring-manager", id)
		if j == nil {
			t.Fatal("the proposer cannot find its recorded promotion")
		}
		if err := promoux015ReviewedClosed(j); err != nil {
			t.Fatalf("the unmutated recorded journey fails the review oracle: %v", err)
		}
		for name, mutate := range map[string]func(*journeyv1.Journey){
			"initiator relationship dropped": func(m *journeyv1.Journey) { m.GetViewer().Relationships = nil },
			"stage not recorded":             func(m *journeyv1.Journey) { m.Stage = journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE },
		} {
			mutated := proto.Clone(j).(*journeyv1.Journey)
			mutate(mutated)
			if promoux015ReviewedClosed(mutated) == nil {
				t.Errorf("the review oracle accepted a mutated journey: %s", name)
			}
		}
	})
}

// TestTodo_PROMOUX_015_Performance holds the live read paths each persona uses
// to review the promotion to a latency budget over real gRPC and PostgreSQL,
// with 100 samples so a nearest-rank p95 breach needs six slow calls.
func TestTodo_PROMOUX_015_Performance(t *testing.T) {
	h := promoux015Compose(t)
	id := h.proposeAndExecute()
	for _, probe := range []struct {
		name string
		call func() error
	}{
		{"ListJourneys as the finance partner", func() error {
			_, err := h.client.ListJourneys(h.rpc("finance-partner"), &journeyv1.ListJourneysRequest{})
			return err
		}},
		{"InspectJourney as the proposer", func() error {
			_, err := h.client.InspectJourney(h.rpc("hiring-manager"), &journeyv1.InspectJourneyRequest{IntentId: id})
			return err
		}},
		{"ListWorkers as the proposer", func() error {
			_, err := h.client.ListWorkers(h.rpc("hiring-manager"), &journeyv1.ListWorkersRequest{})
			return err
		}},
	} {
		budget := latencygate.Budget{Name: "promoux015 " + probe.name, P95: 750 * time.Millisecond, Warmups: 3, Samples: 100}
		result, err := latencygate.Measure(budget, probe.call)
		if err != nil {
			t.Fatalf("%s: %v", probe.name, err)
		}
		t.Log(result)
		if err := latencygate.Check(budget, result); err != nil {
			t.Error(err)
		}
	}
}

// promoux015Pages is each persona's own reachable review surface, and one page
// its role must be refused.
var promoux015Pages = []struct {
	persona string
	pages   []string
	denied  string
}{
	{"hiring-manager", []string{"work", "journeys", "history", "people"}, "admin"},
	{"finance-partner", []string{"work", "history", "myself"}, "people"},
	{"admin", []string{"work", "journeys", "history", "people", "admin"}, ""},
	{"individual-contributor", []string{"myself"}, "work"},
}

// promoux015Locales are the fixture's three locales and the document direction
// each must publish.
var promoux015Locales = []struct{ locale, dir string }{{"en-US", "ltr"}, {"de-DE", "ltr"}, {"ar", "rtl"}}

// page fetches one served workspace document as a persona.
func (h *promoux015Harness) page(persona, page, locale string) (int, string) {
	h.t.Helper()
	url := "http://" + h.composed.HTTPAddr() + "/workspace/app/" + page + "?locale=" + locale
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		h.t.Fatalf("build %s: %v", url, err)
	}
	req.AddCookie(&http.Cookie{Name: "hcmnext_session", Value: h.tokens[persona]})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("GET %s as %s: %v", url, persona, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		h.t.Fatalf("read %s: %v", url, err)
	}
	return res.StatusCode, string(body)
}

// untranslatedKey matches a message catalog key rendered as text instead of
// its localized message.
var untranslatedKey = regexp.MustCompile(`>\s*(work|person|journey|shell|nav|history|myself|people)\.[a-z0-9_.]+\s*<`)

// TestTodo_PROMOUX_015_I18N serves every persona's review pages in en-US,
// de-DE and Arabic and proves each document publishes its resolved language
// and direction, renders no raw message key, and that a role's denied page is
// refused in every locale.
func TestTodo_PROMOUX_015_I18N(t *testing.T) {
	h := promoux015Compose(t)
	h.runSeparatedPromotion()
	for _, surface := range promoux015Pages {
		for _, loc := range promoux015Locales {
			for _, page := range surface.pages {
				status, body := h.page(surface.persona, page, loc.locale)
				if status != http.StatusOK {
					t.Fatalf("%s %s (%s) answered %d", surface.persona, page, loc.locale, status)
				}
				if !strings.Contains(body, `lang="`+loc.locale+`"`) || !strings.Contains(body, `dir="`+loc.dir+`"`) {
					t.Errorf("%s %s (%s) does not publish lang=%q dir=%q", surface.persona, page, loc.locale, loc.locale, loc.dir)
				}
				if m := untranslatedKey.FindString(body); m != "" {
					t.Errorf("%s %s (%s) renders the raw message key %s", surface.persona, page, loc.locale, m)
				}
			}
			if surface.denied != "" {
				if status, _ := h.page(surface.persona, surface.denied, loc.locale); status != http.StatusForbidden {
					t.Errorf("%s reached its denied page %s (%s) with %d, want 403", surface.persona, surface.denied, loc.locale, status)
				}
			}
		}
	}
}

// TestTodo_PROMOUX_015_Accessibility runs the repository's document
// qualification checks (tools/uxqual/qual) over every persona's served review
// pages in every locale.
func TestTodo_PROMOUX_015_Accessibility(t *testing.T) {
	h := promoux015Compose(t)
	h.runSeparatedPromotion()
	for _, surface := range promoux015Pages {
		for _, loc := range promoux015Locales {
			for _, page := range surface.pages {
				_, body := h.page(surface.persona, page, loc.locale)
				result := qual.RunDocumentChecks("promoux015-"+surface.persona+"-"+page+"-"+loc.locale, body, workspace.MaskedActionNeedles())
				for _, criterion := range result.All() {
					if criterion.Name != "" && !criterion.Pass {
						t.Errorf("%s %s (%s): %s: %s", surface.persona, page, loc.locale, criterion.Name, criterion.Detail)
					}
				}
			}
		}
	}
}

// TestTodo_PROMOUX_015_Browser pins what can be checked without a browser and
// names the live pass. The real-browser half is
// tools/uxqual/browser/promoux015_multi_persona.spec.mjs, which drives the
// running dev cell as the four personas at 1280, 390 and 320 px, light and
// dark, reduced motion, in en-US, de-DE and Arabic. This test proves that spec
// exists and covers every persona, width, theme and locale, and that the
// served documents it loads carry the viewport and landmark structure its
// assertions depend on.
func TestTodo_PROMOUX_015_Browser(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("..", "..", "tools", "uxqual", "browser", "promoux015_multi_persona.spec.mjs"))
	if err != nil {
		t.Fatalf("read the live browser spec: %v", err)
	}
	text := string(spec)
	required := []string{"1280", "390", "320", "dark", "light", "reduce", "en-US", "de-DE", `"ar"`}
	for _, surface := range promoux015Pages {
		required = append(required, `"`+surface.persona+`"`)
	}
	for _, want := range required {
		if !strings.Contains(text, want) {
			t.Errorf("the live browser spec does not cover %s", want)
		}
	}

	h := promoux015Compose(t)
	h.runSeparatedPromotion()
	for _, surface := range promoux015Pages {
		for _, page := range surface.pages {
			_, body := h.page(surface.persona, page, "en-US")
			for _, want := range []string{`name="viewport"`, "<main", "<nav"} {
				if !strings.Contains(body, want) {
					t.Errorf("%s %s document lacks %s, which the live spec's layout assertions depend on", surface.persona, page, want)
				}
			}
		}
	}
}
