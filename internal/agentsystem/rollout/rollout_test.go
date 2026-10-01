package agentrollout

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
)

type memoryCatalog struct {
	mu            sync.Mutex
	conversations map[string]Conversation
}

func (c *memoryCatalog) Matching(_ context.Context, selector Selector) ([]Conversation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var matches []Conversation
	for _, conversation := range c.conversations {
		if selector.Matches(conversation) {
			matches = append(matches, conversation)
		}
	}
	return matches, nil
}

func (c *memoryCatalog) Current(_ context.Context, tenant, id string) (Conversation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	conversation, ok := c.conversations[tenant+"/"+id]
	if !ok {
		return Conversation{}, ErrNotFound
	}
	return conversation, nil
}

type approvalRecord struct {
	actor, planID, digest string
}

type allowManagers struct {
	allowed   bool
	mu        sync.Mutex
	next      int
	approvals map[string]approvalRecord
}

func (m *allowManagers) IsConversationManager(context.Context, string, string, string) (bool, error) {
	return m.allowed, nil
}

func (m *allowManagers) RecordRolloutApproval(_ context.Context, actor, planID, digest string, candidates []Conversation) (string, error) {
	if !m.allowed || actor == "" || planID == "" || digest == "" || len(candidates) == 0 {
		return "", ErrNotManager
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	ref := "approval-" + strconv.Itoa(m.next)
	if m.approvals == nil {
		m.approvals = map[string]approvalRecord{}
	}
	m.approvals[ref] = approvalRecord{actor: actor, planID: planID, digest: digest}
	return ref, nil
}

func (m *allowManagers) ApprovedRolloutActor(_ context.Context, ref, planID, digest string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.approvals[ref]
	if !ok || record.planID != planID || record.digest != digest {
		return "", nil
	}
	return record.actor, nil
}

type memoryBoundary struct {
	mu             sync.Mutex
	byConversation map[string]Installation
	byID           map[string]Installation
	nextID         int
}

func newMemoryBoundary() *memoryBoundary {
	return &memoryBoundary{byConversation: map[string]Installation{}, byID: map[string]Installation{}}
}

func (b *memoryBoundary) Find(_ context.Context, tenant, agent, conversation string) (Installation, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	installation, ok := b.byConversation[tenant+"/"+agent+"/"+conversation]
	return installation, ok, nil
}

func (b *memoryBoundary) FindByID(_ context.Context, id string) (Installation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	installation, ok := b.byID[id]
	if !ok {
		return Installation{}, ErrNotFound
	}
	return installation, nil
}

func (b *memoryBoundary) Install(_ context.Context, _ string, rolloutID string, current Conversation, desired DesiredInstallation) (Installation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := current.TenantID + "/" + desired.AgentID + "/" + current.ID
	if existing, ok := b.byConversation[key]; ok {
		return existing, nil
	}
	b.nextID++
	installation := Installation{ID: rolloutID + "-install-" + strconv.Itoa(b.nextID), TenantID: current.TenantID, ConversationID: current.ID, AgentID: desired.AgentID, Version: desired.Version, Grant: append([]string(nil), desired.Grant...), Revision: 1, Active: true}
	b.byConversation[key] = installation
	b.byID[installation.ID] = installation
	return installation, nil
}

func (b *memoryBoundary) Suspend(_ context.Context, _ string, installation Installation, _ Conversation, _ string) (Installation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	current, ok := b.byID[installation.ID]
	if !ok || current.Revision != installation.Revision || !current.Active {
		return Installation{}, ErrPreviewStale
	}
	current.Active = false
	current.Revision++
	b.byID[current.ID] = current
	b.byConversation[current.TenantID+"/"+current.AgentID+"/"+current.ConversationID] = current
	return current, nil
}

func (b *memoryBoundary) Remove(_ context.Context, _ string, installation Installation, _ Conversation) (Installation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	current, ok := b.byID[installation.ID]
	if !ok || current.Revision != installation.Revision || !current.Active {
		return Installation{}, ErrPreviewStale
	}
	current.Active = false
	current.Revision++
	b.byID[current.ID] = current
	b.byConversation[current.TenantID+"/"+current.AgentID+"/"+current.ConversationID] = current
	return current, nil
}

func rolloutFixture(t *testing.T) (Service, *memoryCatalog, *memoryBoundary, Plan) {
	t.Helper()
	catalog := &memoryCatalog{conversations: map[string]Conversation{
		"tenant-a/room-a": {TenantID: "tenant-a", ID: "room-a", Class: "PRIVATE", MembershipRevision: 7, ClassificationRevision: 3, Eligible: true},
		"tenant-a/room-b": {TenantID: "tenant-a", ID: "room-b", Class: "PRIVATE", MembershipRevision: 2, ClassificationRevision: 9, Eligible: true},
	}}
	boundary := newMemoryBoundary()
	service := Service{Catalog: catalog, Managers: &allowManagers{allowed: true}, Boundary: boundary}
	plan, err := service.Preview(context.Background(), Request{ID: "roll-1", Selector: Selector{TenantID: "tenant-a", Classes: []string{"PRIVATE"}}, Desired: DesiredInstallation{AgentID: "agent-a", Version: 4, Grant: []string{"chat.read"}}, BatchLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	return service, catalog, boundary, plan
}

func TestTodo_AGENT_044(t *testing.T) {
	service, _, boundary, plan := rolloutFixture(t)
	approved, err := service.Approve(context.Background(), plan, "manager-a")
	if err != nil {
		t.Fatal(err)
	}
	first, results, err := service.Reconcile(context.Background(), approved)
	if err != nil {
		t.Fatal(err)
	}
	if first.Cursor != 1 || first.Stage != StageReconciling || len(results) != 1 || results[0].Action != "INSTALLED" {
		t.Fatalf("first batch did not install exactly one reviewed placement: plan=%+v results=%+v", first, results)
	}
	second, results, err := service.Reconcile(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	if second.Cursor != 2 || second.Stage != StageComplete || len(results) != 1 || results[0].Action != "INSTALLED" {
		t.Fatalf("second batch did not finish the preview: plan=%+v results=%+v", second, results)
	}
	a, okA, _ := boundary.Find(context.Background(), "tenant-a", "agent-a", "room-a")
	b, okB, _ := boundary.Find(context.Background(), "tenant-a", "agent-a", "room-b")
	if !okA || !okB || a.ID == b.ID || a.Revision != 1 || b.Revision != 1 {
		t.Fatalf("placements are not independently revisioned: a=%+v b=%+v", a, b)
	}
}

func TestTodo_AGENT_044_Security(t *testing.T) {
	t.Run("selector growth requires a fresh preview", func(t *testing.T) {
		service, catalog, _, plan := rolloutFixture(t)
		approved, err := service.Approve(context.Background(), plan, "manager-a")
		if err != nil {
			t.Fatal(err)
		}
		catalog.mu.Lock()
		catalog.conversations["tenant-a/room-c"] = Conversation{TenantID: "tenant-a", ID: "room-c", Class: "PRIVATE", MembershipRevision: 1, ClassificationRevision: 1, Eligible: true}
		catalog.mu.Unlock()
		if _, _, err := service.Reconcile(context.Background(), approved); !errors.Is(err, ErrPreviewStale) {
			t.Fatalf("new selector match was silently installed: %v", err)
		}
	})
	t.Run("version or grant change requires review", func(t *testing.T) {
		service, _, boundary, plan := rolloutFixture(t)
		approved, err := service.Approve(context.Background(), plan, "manager-a")
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = service.Reconcile(context.Background(), approved)
		if err != nil {
			t.Fatal(err)
		}
		upgrade, err := service.Preview(context.Background(), Request{ID: "roll-2", Selector: plan.Selector, Desired: DesiredInstallation{AgentID: "agent-a", Version: 5, Grant: []string{"chat.read", "chat.write"}}, BatchLimit: 1})
		if err != nil {
			t.Fatal(err)
		}
		upgrade, err = service.Approve(context.Background(), upgrade, "manager-a")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = service.Reconcile(context.Background(), upgrade); !errors.Is(err, ErrReviewRequired) {
			t.Fatalf("upgrade widened or changed an existing installation: %v", err)
		}
		stored, ok, _ := boundary.Find(context.Background(), "tenant-a", "agent-a", "room-a")
		if !ok || stored.Version != 4 || len(stored.Grant) != 1 || stored.Grant[0] != "chat.read" {
			t.Fatalf("review refusal mutated the existing grant: %+v", stored)
		}
	})
	t.Run("revocation targets one installation revision", func(t *testing.T) {
		service, _, boundary, plan := rolloutFixture(t)
		approved, err := service.Approve(context.Background(), plan, "manager-a")
		if err != nil {
			t.Fatal(err)
		}
		next, _, err := service.Reconcile(context.Background(), approved)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = service.Reconcile(context.Background(), next)
		if err != nil {
			t.Fatal(err)
		}
		installed, ok, _ := boundary.Find(context.Background(), "tenant-a", "agent-a", "room-a")
		if !ok {
			t.Fatal("expected room-a install")
		}
		removed, err := service.RevokeOne(context.Background(), "manager-a", installed.ID, installed.Revision)
		if err != nil {
			t.Fatal(err)
		}
		other, otherOK, _ := boundary.Find(context.Background(), "tenant-a", "agent-a", "room-b")
		if removed.ID != installed.ID || removed.Active || !otherOK || !other.Active {
			t.Fatalf("revocation crossed installation boundary: removed=%+v other=%+v", removed, other)
		}
		if _, err := service.RevokeOne(context.Background(), "manager-a", installed.ID, installed.Revision); !errors.Is(err, ErrPreviewStale) {
			t.Fatalf("stale installation revision was replayed: %v", err)
		}
		other, otherOK, _ = boundary.Find(context.Background(), "tenant-a", "agent-a", "room-b")
		if !otherOK || !other.Active {
			t.Fatalf("stale revoke affected the other install: %+v", other)
		}
	})
	t.Run("scope cannot broaden during preview", func(t *testing.T) {
		service, _, _, plan := rolloutFixture(t)
		plan.Desired.Grant = []string{"chat.read", "chat.write"}
		if _, _, err := service.Reconcile(context.Background(), plan); !errors.Is(err, ErrApprovalRequired) && !errors.Is(err, ErrInvalid) {
			t.Fatalf("unapproved grant mutation passed validation: %v", err)
		}
	})
	t.Run("invalid stage and cursor combinations are refused", func(t *testing.T) {
		service, _, _, plan := rolloutFixture(t)
		plan.Stage = Stage("UNKNOWN")
		if _, err := service.Approve(context.Background(), plan, "manager-a"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unknown stage was accepted: %v", err)
		}

		plan.Stage = StagePreviewed
		plan.Cursor = 1
		if _, err := service.Approve(context.Background(), plan, "manager-a"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("preview with a nonzero cursor was accepted: %v", err)
		}
	})
	t.Run("approval reference cannot be recomputed with a forged digest", func(t *testing.T) {
		service, _, boundary, plan := rolloutFixture(t)
		approved, err := service.Approve(context.Background(), plan, "manager-a")
		if err != nil {
			t.Fatal(err)
		}
		approved.Desired.Grant = []string{"chat.read", "chat.write"}
		approved.Digest, err = digest(approved)
		if err != nil {
			t.Fatal(err)
		}
		approved.ApprovalDigest = approved.Digest
		if _, _, err := service.Reconcile(context.Background(), approved); !errors.Is(err, ErrApprovalRequired) {
			t.Fatalf("caller minted approval for modified grant: %v", err)
		}
		if _, exists, _ := boundary.Find(context.Background(), "tenant-a", "agent-a", "room-a"); exists {
			t.Fatal("forged approval created an installation")
		}
	})
	t.Run("membership revision change suspends one installed target", func(t *testing.T) {
		service, catalog, boundary, plan := rolloutFixture(t)
		approved, err := service.Approve(context.Background(), plan, "manager-a")
		if err != nil {
			t.Fatal(err)
		}
		_, results, err := service.Reconcile(context.Background(), approved)
		if err != nil || len(results) != 1 || results[0].Action != "INSTALLED" {
			t.Fatalf("initial admission failed: results=%+v err=%v", results, err)
		}
		catalog.mu.Lock()
		changed := catalog.conversations["tenant-a/room-a"]
		changed.MembershipRevision++
		catalog.conversations["tenant-a/room-a"] = changed
		catalog.mu.Unlock()
		_, results, err = service.Reconcile(context.Background(), approved)
		if err != nil || len(results) != 1 || results[0].Action != "SUSPENDED" {
			t.Fatalf("stale target was not suspended: results=%+v err=%v", results, err)
		}
		installed, exists, _ := boundary.Find(context.Background(), "tenant-a", "agent-a", "room-a")
		if !exists || installed.Active || installed.Revision != 2 {
			t.Fatalf("membership change did not independently fence install: %+v", installed)
		}
	})
}

func TestTodo_AGENT_044_Race(t *testing.T) {
	service, _, boundary, plan := rolloutFixture(t)
	approved, err := service.Approve(context.Background(), plan, "manager-a")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan []Result, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, got, err := service.Reconcile(context.Background(), approved)
			results <- got
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent reconcile failed: %v", err)
		}
	}
	ids := map[string]bool{}
	for batch := range results {
		if len(batch) != 1 || batch[0].Action != "INSTALLED" && batch[0].Action != "UNCHANGED" {
			t.Fatalf("unexpected concurrent result: %+v", batch)
		}
		ids[batch[0].InstallationID] = true
	}
	if len(ids) != 1 || boundary.nextID != 1 {
		t.Fatalf("reconcile did not converge to one installation: ids=%v created=%d", ids, boundary.nextID)
	}
}

func TestTodo_AGENT_044_Golden(t *testing.T) {
	service, _, _, plan := rolloutFixture(t)
	if plan.Digest != "6e0a6119a47ce90d0c8a86abc640d5e9338aaaf1ee1c2ffeb8217fc370b5699f" {
		t.Fatalf("preview digest changed: %s", plan.Digest)
	}
	_, err := service.Approve(context.Background(), plan, "manager-a")
	if err != nil {
		t.Fatal(err)
	}
}
