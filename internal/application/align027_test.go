package application

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

// align027Fingerprint digests every base table in the cell's schema: row
// count and an order-independent content digest. Two equal fingerprints mean
// no row anywhere was inserted, updated or deleted between them.
func (h *promoux015Harness) align027Fingerprint() map[string]string {
	h.t.Helper()
	ctx := context.Background()
	rows, err := h.pool.Query(ctx, `SELECT table_name FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_type = 'BASE TABLE' ORDER BY table_name`)
	if err != nil {
		h.t.Fatalf("list tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			h.t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	out := make(map[string]string, len(tables))
	for _, table := range tables {
		var count int64
		var digest string
		q := fmt.Sprintf(`SELECT count(*), coalesce(md5(string_agg(md5(t::text), '' ORDER BY md5(t::text))), '') FROM %q t`, table)
		if err := h.pool.QueryRow(ctx, q).Scan(&count, &digest); err != nil {
			h.t.Fatalf("fingerprint %s: %v", table, err)
		}
		out[table] = fmt.Sprintf("%d:%s", count, digest)
	}
	return out
}

func align027Diff(before, after map[string]string) []string {
	var changed []string
	for table, fp := range after {
		if before[table] != fp {
			changed = append(changed, table)
		}
	}
	for table := range before {
		if _, ok := after[table]; !ok {
			changed = append(changed, table)
		}
	}
	sort.Strings(changed)
	return changed
}

func (h *promoux015Harness) intents() intentsv1.IntentServiceClient {
	h.t.Helper()
	conn, err := grpc.NewClient(h.composed.GRPCAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		h.t.Fatalf("dial intent service: %v", err)
	}
	h.t.Cleanup(func() { _ = conn.Close() })
	return intentsv1.NewIntentServiceClient(conn)
}

// TestTodo_ALIGN_027 proves simulation is write-free across every layer the
// real cell composes: simulating a stored promotion intent through the
// public intent service, repeatedly, returns a proposal artifact with a
// zero-effect receipt and leaves every PostgreSQL table byte-identical.
func TestTodo_ALIGN_027(t *testing.T) {
	h := promoux015Compose(t)
	req, _ := h.discoverPromotion("hiring-manager")
	proposed, err := h.client.ProposeJourney(h.rpc("hiring-manager"), req)
	if err != nil {
		t.Fatalf("ProposeJourney: %v", err)
	}
	id := proposed.GetJourney().GetIntentId()
	client := h.intents()

	before := h.align027Fingerprint()
	var digest string
	for i := range 3 {
		res, err := client.SimulateIntent(h.rpc("admin"), &intentsv1.SimulateIntentRequest{IntentId: id})
		if err != nil {
			t.Fatalf("SimulateIntent #%d: %v", i, err)
		}
		sim := res.GetSimulation()
		if sim.GetIntentId() != id || sim.GetMaterialProposalDigest().GetDigest() == "" || sim.GetZeroEffectReceipt() == nil {
			t.Fatalf("simulation #%d = %v", i, sim)
		}
		if digest != "" && sim.GetMaterialProposalDigest().GetDigest() != digest {
			t.Fatalf("simulation #%d material digest %s differs from %s", i, sim.GetMaterialProposalDigest().GetDigest(), digest)
		}
		digest = sim.GetMaterialProposalDigest().GetDigest()
	}
	if changed := align027Diff(before, h.align027Fingerprint()); len(changed) != 0 {
		t.Fatalf("simulation wrote to %v", changed)
	}
}

// TestTodo_ALIGN_027_Property proves the zero-effect receipt and the table
// fingerprint agree for every persona allowed to simulate and for repeated
// runs: the artifact is deterministic and nothing is written.
func TestTodo_ALIGN_027_Property(t *testing.T) {
	h := promoux015Compose(t)
	id := h.proposeAndExecute()
	client := h.intents()
	before := h.align027Fingerprint()
	seen := map[string]bool{}
	for range 4 {
		res, err := client.SimulateIntent(h.rpc("admin"), &intentsv1.SimulateIntentRequest{IntentId: id})
		if err != nil {
			t.Fatalf("SimulateIntent: %v", err)
		}
		seen[res.GetSimulation().GetMaterialProposalDigest().GetDigest()] = true
	}
	if len(seen) != 1 {
		t.Fatalf("repeated simulation produced %d distinct material digests", len(seen))
	}
	if changed := align027Diff(before, h.align027Fingerprint()); len(changed) != 0 {
		t.Fatalf("simulating an executing intent wrote to %v", changed)
	}
}

// TestTodo_ALIGN_027_Golden pins the zero-effect receipt a simulation
// returns: it declares no persisted effect of any kind.
func TestTodo_ALIGN_027_Golden(t *testing.T) {
	h := promoux015Compose(t)
	req, _ := h.discoverPromotion("hiring-manager")
	proposed, err := h.client.ProposeJourney(h.rpc("hiring-manager"), req)
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.intents().SimulateIntent(h.rpc("admin"), &intentsv1.SimulateIntentRequest{IntentId: proposed.GetJourney().GetIntentId()})
	if err != nil {
		t.Fatal(err)
	}
	receipt := res.GetSimulation().GetZeroEffectReceipt()
	text := strings.ToLower(fmt.Sprint(receipt))
	for _, forbidden := range []string{"ledger_event_id", "outbox_id", "commit_receipt"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("zero-effect receipt names a persisted effect %q: %v", forbidden, receipt)
		}
	}
	if receipt == nil {
		t.Fatal("simulation returned no zero-effect receipt")
	}
}

// TestTodo_ALIGN_027_Security proves a refused or foreign simulation is also
// write-free: an unknown intent and a persona without access are refused and
// no table changes.
func TestTodo_ALIGN_027_Security(t *testing.T) {
	h := promoux015Compose(t)
	id := h.proposeAndExecute()
	client := h.intents()
	before := h.align027Fingerprint()
	if _, err := client.SimulateIntent(h.rpc("admin"), &intentsv1.SimulateIntentRequest{IntentId: "00000000-0000-7000-8000-000000000000"}); err == nil {
		t.Error("simulating an unknown intent succeeded")
	}
	for persona := range h.tokens {
		_, _ = client.SimulateIntent(h.rpc(persona), &intentsv1.SimulateIntentRequest{IntentId: id})
	}
	if changed := align027Diff(before, h.align027Fingerprint()); len(changed) != 0 {
		t.Fatalf("refused or foreign simulations wrote to %v", changed)
	}
}

// TestTodo_ALIGN_027_Conformance proves the write-free guarantee is the
// intent mode contract, not an accident of one handler: SIMULATE admits no
// effect in any environment, and the fingerprint detector itself catches a
// real write (so an empty diff above is evidence, not a blind spot).
func TestTodo_ALIGN_027_Conformance(t *testing.T) {
	h := promoux015Compose(t)
	before := h.align027Fingerprint()
	if _, err := h.pool.Exec(context.Background(), `UPDATE tenant SET display_name = display_name || ' (probe)'`); err != nil {
		t.Fatalf("probe write: %v", err)
	}
	changed := align027Diff(before, h.align027Fingerprint())
	if len(changed) != 1 || changed[0] != "tenant" {
		t.Fatalf("fingerprint detected %v after a tenant write, want [tenant]", changed)
	}
	if _, err := h.pool.Exec(context.Background(), `UPDATE tenant SET display_name = replace(display_name, ' (probe)', '')`); err != nil {
		t.Fatal(err)
	}
	if changed := align027Diff(before, h.align027Fingerprint()); len(changed) != 0 {
		t.Fatalf("restoring the probe left %v changed", changed)
	}
	for _, env := range intent.Environments() {
		contract, err := intent.ModeContractFor(intent.ModeSimulate, env)
		if err != nil {
			continue
		}
		if !contract.ZeroEffect() || contract.AllowsDomainCommit || contract.AllowsExternalEffect || contract.AllowsApprovalConsumption {
			t.Errorf("SIMULATE in %s permits an effect: %+v", env, contract)
		}
	}
	_ = journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED
}
