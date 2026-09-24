package workspace

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pageledger"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

type workspacePageLedger struct {
	revisions   map[string][]pageledger.RevisionRow
	rollouts    map[string][]pageledger.RolloutRow
	retirements map[string][]pageledger.RetirementRow
}

func (s *workspacePageLedger) PutRevision(_ context.Context, tenant, page string, version int64, digest string, payload []byte) error {
	s.revisions[tenant] = append(s.revisions[tenant], pageledger.RevisionRow{Page: page, Version: version, Digest: digest, Payload: append([]byte(nil), payload...)})
	return nil
}
func (s *workspacePageLedger) LoadRevisions(_ context.Context, tenant string) ([]pageledger.RevisionRow, error) {
	return append([]pageledger.RevisionRow(nil), s.revisions[tenant]...), nil
}
func (s *workspacePageLedger) PutRollout(_ context.Context, tenant, page string, recordVersion, targetVersion int64, digest string, payload []byte) error {
	s.rollouts[tenant] = append(s.rollouts[tenant], pageledger.RolloutRow{Page: page, RecordVersion: recordVersion, TargetVersion: targetVersion, Digest: digest, Payload: append([]byte(nil), payload...)})
	return nil
}
func (s *workspacePageLedger) LoadRollouts(_ context.Context, tenant string) ([]pageledger.RolloutRow, error) {
	return append([]pageledger.RolloutRow(nil), s.rollouts[tenant]...), nil
}
func (s *workspacePageLedger) PutRetirement(_ context.Context, tenant, page, digest string, payload []byte) error {
	s.retirements[tenant] = append(s.retirements[tenant], pageledger.RetirementRow{Page: page, Digest: digest, Payload: append([]byte(nil), payload...)})
	return nil
}
func (s *workspacePageLedger) LoadRetirements(_ context.Context, tenant string) ([]pageledger.RetirementRow, error) {
	return append([]pageledger.RetirementRow(nil), s.retirements[tenant]...), nil
}

func TestTodo_REV_067_02_DurablePublicationComposition(t *testing.T) {
	base, token := newShellHandler(t, false)
	store := &workspacePageLedger{revisions: map[string][]pageledger.RevisionRow{}, rollouts: map[string][]pageledger.RolloutRow{}, retirements: map[string][]pageledger.RetirementRow{}}
	newDurableHandler := func() *Handler {
		t.Helper()
		h, err := NewHandler(Options{Cell: base.cell, Config: base.config, Now: base.now, PageLedger: store})
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	firstHandler := newDurableHandler()
	governance, err := firstHandler.governanceForTenant(context.Background(), shellTenant)
	if err != nil {
		t.Fatal(err)
	}
	definition, ok := productui.LookupPage(productui.PageHome)
	if !ok {
		t.Fatal("home page missing from registry")
	}
	first, err := governance.PublishRevision(productui.PageHome, productui.SnapshotPageDefinition(definition), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := governance.PublishRollout(productui.PageRollout{Page: productui.PageHome, Version: 1, Digest: first.Digest, Scopes: []productui.RolloutScope{{Scope: governedScope}}}); err != nil {
		t.Fatal(err)
	}
	second, err := governance.PublishRevision(productui.PageHome, productui.SnapshotPageDefinition(definition), 2)
	if err != nil {
		t.Fatal(err)
	}
	live := productui.PageRollout{Page: productui.PageHome, Version: 2, Digest: second.Digest, Scopes: []productui.RolloutScope{{Scope: governedScope}}}
	if err := governance.PublishRollout(live); err != nil {
		t.Fatal(err)
	}
	rollback := productui.PageRollout{Page: productui.PageHome, Version: 1, Digest: first.Digest, Scopes: []productui.RolloutScope{{Scope: governedScope}}}
	if err := governance.PublishRollback(live, rollback); err != nil {
		t.Fatal(err)
	}
	retirement := productui.PageRetirement{Page: productui.PageHome, EffectiveFrom: shellNow.Add(time.Minute).Unix(), Reason: "scheduled closure"}
	if err := governance.PublishRetirement(retirement); err != nil {
		t.Fatal(err)
	}

	// A new handler proves the production composition recovers and serves the
	// durable history, including a rollback whose target version is older.
	restarted := newDurableHandler()
	response := getProductPage(t, restarted, token, PathProductHome)
	if response.Code != 200 {
		t.Fatalf("durable governed page status = %d: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("X-Page-Revision-Digest"); got != first.Digest {
		t.Fatalf("recovered rollback digest = %q, want %q", got, first.Digest)
	}
	recoveredGovernance, err := restarted.governanceForTenant(context.Background(), shellTenant)
	if err != nil {
		t.Fatal(err)
	}
	_, governed, servable, _ := recoveredGovernance.Resolve(productui.PageHome, governedScope, retirement.EffectiveFrom)
	if !governed || servable {
		t.Fatalf("retirement state lost after restart: governed=%v servable=%v", governed, servable)
	}
	rows := store.rollouts[shellTenant]
	if len(rows) != 3 || rows[2].RecordVersion <= rows[1].RecordVersion || rows[2].TargetVersion != 1 {
		t.Fatalf("persisted rollout event order = %#v", rows)
	}
	store.rollouts[shellTenant][0].Payload[len(store.rollouts[shellTenant][0].Payload)-2] ^= 1
	failedRecovery := getProductPage(t, newDurableHandler(), token, PathProductHome)
	if failedRecovery.Code != 503 {
		t.Fatalf("tampered durable rollout status = %d, want fail-closed 503", failedRecovery.Code)
	}
}
