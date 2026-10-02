package agentrollout

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// conversationManagers gives each actor authority over named conversations
// only, and can lose it between approval and reconciliation, which is what a
// manager change in Chat looks like to a rollout.
type conversationManagers struct {
	allowManagers
	mu      sync.Mutex
	manages map[string]map[string]bool
}

func (m *conversationManagers) IsConversationManager(_ context.Context, actor, tenant, conversation string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.manages[actor][tenant+"/"+conversation], nil
}

func (m *conversationManagers) set(actor, key string, manages bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.manages[actor] == nil {
		m.manages[actor] = map[string]bool{}
	}
	m.manages[actor][key] = manages
}

func agent014Fixture(t *testing.T) (Service, *memoryCatalog, *memoryBoundary, *conversationManagers) {
	t.Helper()
	catalog := &memoryCatalog{conversations: map[string]Conversation{
		"tenant-a/room-a": {TenantID: "tenant-a", ID: "room-a", Class: "PRIVATE", MembershipRevision: 7, ClassificationRevision: 3, Eligible: true},
		"tenant-a/room-b": {TenantID: "tenant-a", ID: "room-b", Class: "PRIVATE", MembershipRevision: 2, ClassificationRevision: 9, Eligible: true},
		"tenant-a/room-c": {TenantID: "tenant-a", ID: "room-c", Class: "PRIVATE", MembershipRevision: 4, ClassificationRevision: 1, Eligible: true},
		"tenant-a/lobby":  {TenantID: "tenant-a", ID: "lobby", Class: "PUBLIC", MembershipRevision: 1, ClassificationRevision: 1, Eligible: true},
	}}
	managers := &conversationManagers{allowManagers: allowManagers{allowed: true}, manages: map[string]map[string]bool{}}
	for _, room := range []string{"tenant-a/room-a", "tenant-a/room-b", "tenant-a/room-c"} {
		managers.set("manager-a", room, true)
	}
	boundary := newMemoryBoundary()
	return Service{Catalog: catalog, Managers: managers, Boundary: boundary}, catalog, boundary, managers
}

func agent014Request(batch int) Request {
	return Request{ID: "roll-14", Selector: Selector{TenantID: "tenant-a", Classes: []string{"PRIVATE"}}, Desired: DesiredInstallation{AgentID: "agent-a", Version: 4, Grant: []string{"chat.read"}}, BatchLimit: batch}
}

func (c *memoryCatalog) change(key string, edit func(*Conversation)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	conversation := c.conversations[key]
	edit(&conversation)
	c.conversations[key] = conversation
}

// TestTodo_AGENT_014 follows one previewed plan to the end: what it installs,
// what it leaves for a new review, and what a second run of it does. Each
// admission is gated by the conversation's membership and classification as
// they are at that moment, not as they were at the preview.
func TestTodo_AGENT_014(t *testing.T) {
	ctx := context.Background()
	service, catalog, boundary, _ := agent014Fixture(t)
	plan, err := service.Preview(ctx, agent014Request(2))
	if err != nil {
		t.Fatal(err)
	}
	// The preview pins exactly the conversations the selector matched, in a
	// stable order, with the revisions they had; the public room is not one.
	if len(plan.Candidates) != 3 || plan.Candidates[0].ID != "room-a" || plan.Candidates[1].ID != "room-b" || plan.Candidates[2].ID != "room-c" || plan.Stage != StagePreviewed || plan.Cursor != 0 {
		t.Fatalf("preview = %+v", plan)
	}
	// Nothing is installed by a preview, or by a plan nobody approved.
	if _, _, err := service.Reconcile(ctx, plan); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("an unapproved preview was reconciled: %v", err)
	}
	if boundary.nextID != 0 {
		t.Fatalf("a preview created %d installations", boundary.nextID)
	}
	approved, err := service.Approve(ctx, plan, "manager-a")
	if err != nil || approved.Stage != StageApproved || approved.ApproverID != "manager-a" || approved.ApprovalDigest != plan.Digest {
		t.Fatalf("approval = %+v err=%v", approved, err)
	}

	// Between approval and admission: room-b gains a member, room-c is
	// reclassified out of the audience. Neither is admitted on the old review.
	catalog.change("tenant-a/room-b", func(c *Conversation) { c.MembershipRevision++ })
	catalog.change("tenant-a/room-c", func(c *Conversation) { c.Eligible = false; c.ClassificationRevision++ })

	first, results, err := service.Reconcile(ctx, approved)
	if err != nil || first.Cursor != 2 || first.Stage != StageReconciling || len(results) != 2 {
		t.Fatalf("first batch: plan=%+v results=%+v err=%v", first, results, err)
	}
	if results[0].ConversationID != "room-a" || results[0].Action != "INSTALLED" || results[0].InstallationID == "" {
		t.Fatalf("room-a = %+v, want an installation", results[0])
	}
	if results[1].ConversationID != "room-b" || results[1].Action != "PENDING_REVIEW" || results[1].InstallationID != "" {
		t.Fatalf("room-b changed members after the preview and was %+v, want held for review", results[1])
	}
	second, results, err := service.Reconcile(ctx, first)
	if err != nil || second.Cursor != 3 || second.Stage != StageComplete || len(results) != 1 || results[0].Action != "PENDING_REVIEW" {
		t.Fatalf("second batch: plan=%+v results=%+v err=%v", second, results, err)
	}
	// One installation exists: revisioned, bound to its conversation, with the
	// exact version and grant that were reviewed.
	installed, ok, _ := boundary.Find(ctx, "tenant-a", "agent-a", "room-a")
	if !ok || !installed.Active || installed.Revision != 1 || installed.Version != 4 || len(installed.Grant) != 1 || installed.Grant[0] != "chat.read" || installed.ConversationID != "room-a" {
		t.Fatalf("room-a installation = %+v", installed)
	}
	for _, room := range []string{"room-b", "room-c", "lobby"} {
		if _, exists, _ := boundary.Find(ctx, "tenant-a", "agent-a", room); exists {
			t.Errorf("%s has an installation it was never admitted to", room)
		}
	}
	if boundary.nextID != 1 {
		t.Fatalf("installations created = %d, want 1", boundary.nextID)
	}
	// Running the finished plan again changes nothing: there is no batch left.
	again, results, err := service.Reconcile(ctx, second)
	if err != nil || again.Cursor != 3 || again.Stage != StageComplete || len(results) != 0 || boundary.nextID != 1 {
		t.Fatalf("a finished plan run again: plan=%+v results=%+v created=%d err=%v", again, results, boundary.nextID, err)
	}
	// The held conversations are admitted only by a new preview of what they
	// are now, approved again.
	renewed, err := service.Preview(ctx, Request{ID: "roll-15", Selector: Selector{TenantID: "tenant-a", ConversationIDs: []string{"room-b"}}, Desired: plan.Desired, BatchLimit: 1})
	if err != nil || len(renewed.Candidates) != 1 || renewed.Candidates[0].MembershipRevision != 3 {
		t.Fatalf("new preview of room-b = %+v err=%v", renewed, err)
	}
	renewed, err = service.Approve(ctx, renewed, "manager-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, results, err = service.Reconcile(ctx, renewed); err != nil || len(results) != 1 || results[0].Action != "INSTALLED" {
		t.Fatalf("room-b after a new review: %+v err=%v", results, err)
	}
	roomB, _, _ := boundary.Find(ctx, "tenant-a", "agent-a", "room-b")
	if roomB.ID == installed.ID || roomB.Revision != 1 {
		t.Fatalf("room-b's installation %+v is not independent of room-a's %+v", roomB, installed)
	}
}

// TestTodo_AGENT_014_Security: authority is the manager's, per conversation,
// at the moment of each change.
func TestTodo_AGENT_014_Security(t *testing.T) {
	ctx := context.Background()
	t.Run("managing some of the conversations approves none", func(t *testing.T) {
		service, _, boundary, managers := agent014Fixture(t)
		managers.set("manager-b", "tenant-a/room-a", true)
		plan, err := service.Preview(ctx, agent014Request(3))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Approve(ctx, plan, "manager-b"); !errors.Is(err, ErrNotManager) {
			t.Fatalf("a manager of one room approved a plan for three: %v", err)
		}
		if len(managers.approvals) != 0 || boundary.nextID != 0 {
			t.Fatalf("a refused approval was recorded (%d) or installed (%d)", len(managers.approvals), boundary.nextID)
		}
	})
	t.Run("authority lost after approval admits nothing more", func(t *testing.T) {
		service, _, boundary, managers := agent014Fixture(t)
		plan, _ := service.Preview(ctx, agent014Request(1))
		approved, err := service.Approve(ctx, plan, "manager-a")
		if err != nil {
			t.Fatal(err)
		}
		next, _, err := service.Reconcile(ctx, approved)
		if err != nil {
			t.Fatal(err)
		}
		managers.set("manager-a", "tenant-a/room-b", false)
		stopped, results, err := service.Reconcile(ctx, next)
		if !errors.Is(err, ErrNotManager) || len(results) != 0 || stopped.Cursor != 1 {
			t.Fatalf("a former manager's approval still admitted room-b: plan=%+v results=%+v err=%v", stopped, results, err)
		}
		if _, exists, _ := boundary.Find(ctx, "tenant-a", "agent-a", "room-b"); exists || boundary.nextID != 1 {
			t.Fatalf("room-b was installed after its manager lost authority (created=%d)", boundary.nextID)
		}
	})
	t.Run("one person's approval cannot be used by another", func(t *testing.T) {
		service, _, boundary, managers := agent014Fixture(t)
		for _, room := range []string{"tenant-a/room-a", "tenant-a/room-b", "tenant-a/room-c"} {
			managers.set("manager-b", room, true)
		}
		plan, _ := service.Preview(ctx, agent014Request(3))
		approved, err := service.Approve(ctx, plan, "manager-a")
		if err != nil {
			t.Fatal(err)
		}
		borrowed := approved
		borrowed.ApproverID = "manager-b"
		if _, _, err := service.Reconcile(ctx, borrowed); !errors.Is(err, ErrApprovalRequired) {
			t.Fatalf("manager-b reconciled with manager-a's approval: %v", err)
		}
		// The approval of one plan does not carry to another plan.
		other, _ := service.Preview(ctx, Request{ID: "roll-other", Selector: plan.Selector, Desired: DesiredInstallation{AgentID: "agent-b", Version: 1, Grant: []string{"chat.read"}}, BatchLimit: 3})
		other.ApproverID, other.ApprovalRef, other.ApprovalDigest, other.Stage = "manager-a", approved.ApprovalRef, other.Digest, StageApproved
		if _, _, err := service.Reconcile(ctx, other); !errors.Is(err, ErrApprovalRequired) {
			t.Fatalf("an approval was reused for a different plan: %v", err)
		}
		if boundary.nextID != 0 {
			t.Fatalf("borrowed approvals installed %d placements", boundary.nextID)
		}
	})
	t.Run("only a current manager removes, and only the named installation", func(t *testing.T) {
		service, catalog, boundary, managers := agent014Fixture(t)
		plan, _ := service.Preview(ctx, agent014Request(3))
		approved, _ := service.Approve(ctx, plan, "manager-a")
		if _, _, err := service.Reconcile(ctx, approved); err != nil {
			t.Fatal(err)
		}
		roomA, _, _ := boundary.Find(ctx, "tenant-a", "agent-a", "room-a")
		if _, err := service.RevokeOne(ctx, "employee", roomA.ID, roomA.Revision); !errors.Is(err, ErrNotManager) {
			t.Fatalf("a non-manager removed an installation: %v", err)
		}
		managers.set("manager-a", "tenant-a/room-a", false)
		if _, err := service.RevokeOne(ctx, "manager-a", roomA.ID, roomA.Revision); !errors.Is(err, ErrNotManager) {
			t.Fatalf("a former manager removed an installation: %v", err)
		}
		// A conversation whose membership cannot be read (revision zero) is
		// never the basis of a removal either.
		managers.set("manager-a", "tenant-a/room-b", true)
		roomB, _, _ := boundary.Find(ctx, "tenant-a", "agent-a", "room-b")
		catalog.change("tenant-a/room-b", func(c *Conversation) { c.MembershipRevision = 0 })
		if _, err := service.RevokeOne(ctx, "manager-a", roomB.ID, roomB.Revision); !errors.Is(err, ErrPreviewStale) {
			t.Fatalf("removal on an unreadable membership: %v", err)
		}
		for _, room := range []string{"room-a", "room-b", "room-c"} {
			if current, ok, _ := boundary.Find(ctx, "tenant-a", "agent-a", room); !ok || !current.Active || current.Revision != 1 {
				t.Errorf("%s changed after refused removals: %+v", room, current)
			}
		}
	})
	t.Run("a catalog answering outside the selector is refused", func(t *testing.T) {
		service, _, _, _ := agent014Fixture(t)
		for name, stray := range map[string]Conversation{
			"another tenant":     {TenantID: "tenant-b", ID: "room-z", Class: "PRIVATE", MembershipRevision: 1, ClassificationRevision: 1, Eligible: true},
			"another class":      {TenantID: "tenant-a", ID: "lobby", Class: "PUBLIC", MembershipRevision: 1, ClassificationRevision: 1, Eligible: true},
			"no membership read": {TenantID: "tenant-a", ID: "room-a", Class: "PRIVATE", ClassificationRevision: 1, Eligible: true},
		} {
			service.Catalog = strayCatalog{stray}
			if _, err := service.Preview(ctx, agent014Request(1)); !errors.Is(err, ErrInvalid) {
				t.Errorf("%s was accepted into a preview: %v", name, err)
			}
		}
	})
}

type strayCatalog struct{ conversation Conversation }

func (c strayCatalog) Matching(context.Context, Selector) ([]Conversation, error) {
	return []Conversation{c.conversation}, nil
}

func (c strayCatalog) Current(context.Context, string, string) (Conversation, error) {
	return c.conversation, nil
}

// TestTodo_AGENT_014_Race removes installations from many goroutines at once.
// Each installation is removed exactly once at its revision, and removing one
// never touches another.
func TestTodo_AGENT_014_Race(t *testing.T) {
	ctx := context.Background()
	service, _, boundary, _ := agent014Fixture(t)
	plan, _ := service.Preview(ctx, agent014Request(3))
	approved, err := service.Approve(ctx, plan, "manager-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Reconcile(ctx, approved); err != nil {
		t.Fatal(err)
	}
	roomA, _, _ := boundary.Find(ctx, "tenant-a", "agent-a", "room-a")
	roomB, _, _ := boundary.Find(ctx, "tenant-a", "agent-a", "room-b")

	const contenders = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	outcomes := make(chan error, 2*contenders)
	for i := 0; i < contenders; i++ {
		for _, target := range []Installation{roomA, roomB} {
			wg.Add(1)
			go func(target Installation) {
				defer wg.Done()
				<-start
				removed, err := service.RevokeOne(ctx, "manager-a", target.ID, target.Revision)
				if err == nil && (removed.ID != target.ID || removed.Active || removed.Revision != target.Revision+1) {
					err = errors.New("removal returned another installation")
				}
				outcomes <- err
			}(target)
		}
	}
	// Reconciling the same plan while removals run must not put a removed
	// installation back: a removed placement needs a new review.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		_, _, _ = service.Reconcile(ctx, approved)
	}()
	close(start)
	wg.Wait()
	close(outcomes)
	removed, stale := 0, 0
	for err := range outcomes {
		switch {
		case err == nil:
			removed++
		case errors.Is(err, ErrPreviewStale):
			stale++
		default:
			t.Fatalf("concurrent removal failed with %v", err)
		}
	}
	if removed != 2 || stale != 2*contenders-2 {
		t.Fatalf("removals that succeeded = %d (want one per installation), refused as stale = %d", removed, stale)
	}
	for room, want := range map[string]struct {
		active   bool
		revision uint64
	}{"room-a": {false, 2}, "room-b": {false, 2}, "room-c": {true, 1}} {
		current, ok, _ := boundary.Find(ctx, "tenant-a", "agent-a", room)
		if !ok || current.Active != want.active || current.Revision != want.revision {
			t.Errorf("%s after the race = %+v, want active=%v revision=%d", room, current, want.active, want.revision)
		}
	}
	if boundary.nextID != 3 {
		t.Fatalf("installations created = %d; a removed placement was installed again without a review", boundary.nextID)
	}
}

// TestTodo_AGENT_014_Golden pins what an approval is bound to. The digest
// covers the plan's identity, selector, version, grant, batch size and every
// candidate's revisions; it does not depend on the order the catalog answered
// in, or on how far reconciliation has got.
func TestTodo_AGENT_014_Golden(t *testing.T) {
	ctx := context.Background()
	service, catalog, _, _ := agent014Fixture(t)
	request := Request{ID: "roll-14", Selector: Selector{TenantID: "tenant-a", ConversationIDs: []string{"room-c", "room-a"}, Classes: []string{"PRIVATE"}}, Desired: DesiredInstallation{AgentID: "agent-a", Version: 4, Grant: []string{"chat.write", "chat.read"}}, BatchLimit: 2}
	plan, err := service.Preview(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	const pinned = "6c6c9bf288d26d6040af1aaf28158f2bf555de743c598e8d81cc7258a267f9fd"
	if plan.Digest != pinned {
		t.Fatalf("preview digest = %s, want %s", plan.Digest, pinned)
	}
	// The same request written in another order is the same plan.
	reordered := request
	reordered.Selector.ConversationIDs = []string{"room-a", "room-c"}
	reordered.Desired.Grant = []string{"chat.read", "chat.write"}
	if again, err := service.Preview(ctx, reordered); err != nil || again.Digest != pinned {
		t.Fatalf("reordered request digest = %s err=%v", again.Digest, err)
	}
	// Progress and approval are not part of what was reviewed.
	approved, err := service.Approve(ctx, plan, "manager-a")
	if err != nil || approved.Digest != pinned {
		t.Fatalf("approved digest = %s err=%v", approved.Digest, err)
	}
	progressed, _, err := service.Reconcile(ctx, approved)
	if err != nil || progressed.Digest != pinned || progressed.Stage != StageComplete {
		t.Fatalf("reconciled plan = %+v err=%v", progressed, err)
	}
	// Anything that was reviewed changes it.
	for name, edit := range map[string]func(*Request){
		"version":    func(r *Request) { r.Desired.Version = 5 },
		"grant":      func(r *Request) { r.Desired.Grant = []string{"chat.read"} },
		"agent":      func(r *Request) { r.Desired.AgentID = "agent-b" },
		"batch size": func(r *Request) { r.BatchLimit = 1 },
		"selector":   func(r *Request) { r.Selector.ConversationIDs = []string{"room-a"} },
		"plan id":    func(r *Request) { r.ID = "roll-15" },
	} {
		changed := request
		changed.Selector.ConversationIDs = append([]string(nil), request.Selector.ConversationIDs...)
		changed.Desired.Grant = append([]string(nil), request.Desired.Grant...)
		edit(&changed)
		if other, err := service.Preview(ctx, changed); err != nil || other.Digest == pinned {
			t.Errorf("changing the %s left the digest unchanged (err=%v)", name, err)
		}
	}
	catalog.change("tenant-a/room-a", func(c *Conversation) { c.MembershipRevision++ })
	if later, err := service.Preview(ctx, request); err != nil || later.Digest == pinned {
		t.Errorf("a membership change left the digest unchanged (err=%v)", err)
	}
}
