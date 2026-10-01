package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

func TestChatSeedAudienceFactsCoverPrivateSeedRooms(t *testing.T) {
	for _, tenant := range []string{"harborcare-demo", "ironridge-demo"} {
		t.Run(tenant, func(t *testing.T) {
			pack, ok := demoworkforce.PackFor(tenant)
			if !ok {
				t.Fatal("demo workforce pack missing")
			}
			employees, err := pack.Plan(pgstore.TenantID(tenant))
			if err != nil {
				t.Fatal(err)
			}
			people := make([]string, len(employees))
			for i := range employees {
				people[i] = employees[i].Row.WorkerKey
			}
			rooms := chatSeedRooms("full", len(people))
			wantClassification := map[string]string{
				"leadership-private": "T3", "payroll-close": "T3", "incident-review": "T3",
				"dm-0": "T2", "dm-1": "T2", "dm-2": "T2", "dm-3": "T2",
				"dm-4": "T2", "dm-5": "T2", "dm-6": "T2", "dm-7": "T2",
				"group-comp-cycle": "T3", "group-q4-hiring": "T3",
			}
			for _, room := range rooms {
				if room.Kind == roomPublic {
					continue
				}
				conversation := chatSeedConversationID(tenant, room.Key)
				members := make([]chatcore.Membership, 0, len(room.Members))
				for i, person := range memberNames(people, room.Members) {
					role := chatcore.Member
					if i == 0 {
						role = chatcore.Manager
					}
					members = append(members, chatcore.Membership{ConversationID: conversation, TenantID: tenant, HomeTenantID: tenant, SubjectID: person, Role: role})
				}
				policy, required, err := chatSeedAudiencePolicy(tenant, room, members, employees)
				if err != nil {
					t.Fatalf("room %s: %v", room.Key, err)
				}
				if !required || policy.Classification != wantClassification[room.Key] || policy.Residency != "us" || (policy.RoleMode != 1 && policy.RoleMode != 2) {
					t.Fatalf("room %s policy = %+v required=%t", room.Key, policy, required)
				}
			}
		})
	}
}

func TestChatSeedAudienceFactsRefuseUnknownTenantAndNonlocalMembers(t *testing.T) {
	pack, _ := demoworkforce.PackFor("harborcare-demo")
	employees, err := pack.Plan(pgstore.TenantID("harborcare-demo"))
	if err != nil {
		t.Fatal(err)
	}
	room := roomSpec{Key: "dm-0", Kind: roomDirect}
	base := chatcore.Membership{ConversationID: "c", TenantID: "harborcare-demo", HomeTenantID: "harborcare-demo", SubjectID: employees[0].Row.WorkerKey, Role: chatcore.Manager}
	for _, tc := range []struct {
		name   string
		tenant string
		member chatcore.Membership
	}{{name: "unknown tenant", tenant: "unknown-demo", member: base}, {name: "external home tenant", tenant: "harborcare-demo", member: func() chatcore.Membership { m := base; m.HomeTenantID = "outside"; return m }()}, {name: "unknown subject", tenant: "harborcare-demo", member: func() chatcore.Membership { m := base; m.SubjectID = "outside-person"; return m }()}} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := chatSeedAudiencePolicy(tc.tenant, room, []chatcore.Membership{tc.member}, employees); err == nil {
				t.Fatal("unclassified audience was accepted")
			}
		})
	}
}

func TestChatSeedAudienceFactsRefuseUnclassifiedWorkforceKinds(t *testing.T) {
	pack, _ := demoworkforce.PackFor("harborcare-demo")
	employees, err := pack.Plan(pgstore.TenantID("harborcare-demo"))
	if err != nil {
		t.Fatal(err)
	}
	room := roomSpec{Key: "dm-0", Kind: roomDirect}
	member := chatcore.Membership{TenantID: "harborcare-demo", HomeTenantID: "harborcare-demo", SubjectID: employees[0].Row.WorkerKey, Role: chatcore.Manager}
	for _, tc := range []struct {
		name   string
		mutate func(*demoworkforce.Employee)
	}{{name: "contractor", mutate: func(e *demoworkforce.Employee) { e.Row.WorkerType = "contractor" }}, {name: "inactive", mutate: func(e *demoworkforce.Employee) { e.Row.LifecycleStatus = "inactive" }}} {
		t.Run(tc.name, func(t *testing.T) {
			classified := append([]demoworkforce.Employee(nil), employees...)
			tc.mutate(&classified[0])
			if _, _, err := chatSeedAudiencePolicy("harborcare-demo", room, []chatcore.Membership{member}, classified); err == nil {
				t.Fatal("non-employee or inactive audience member was accepted as internal")
			}
		})
	}
}

func TestChatSeedPersistsAudiencePolicyWithPrivateConversation(t *testing.T) {
	store, db := chatSeedStore(t)
	ctx := context.Background()
	tenant := "harborcare-demo"
	if err := runChatSeedCommand(ctx, store, nil, chatSeedOptions{Tenant: tenant, Scale: "small", MediaRoot: t.TempDir(), Now: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}, &bytes.Buffer{}); err != nil {
		t.Fatalf("seed chat: %v", err)
	}
	if err := runChatSeedCommand(ctx, store, nil, chatSeedOptions{Tenant: tenant, Scale: "small", MediaRoot: t.TempDir(), Now: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "already exist") {
		t.Fatalf("rerun with matching existing policies = %v; want ordinary duplicate-seed refusal", err)
	}
	rows, err := db.SQL.QueryContext(ctx, `SELECT c.id, c.kind, p.classification, p.residency, p.role_mode FROM chat_conversation c LEFT JOIN chat_channel_policy p ON p.tenant_id=c.tenant_id AND p.conversation_id=c.id ORDER BY c.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seenPolicy := 0
	for rows.Next() {
		var id, kind string
		var classification, residency *string
		var roleMode *int
		if err := rows.Scan(&id, &kind, &classification, &residency, &roleMode); err != nil {
			t.Fatal(err)
		}
		if kind == string(chatcore.PublicChannel) {
			if classification != nil || residency != nil || roleMode != nil {
				t.Fatalf("public room %s unexpectedly got a private audience policy", id)
			}
			continue
		}
		if classification == nil || *classification == "" || residency == nil || *residency != "us" || roleMode == nil || (*roleMode != 1 && *roleMode != 2) {
			t.Fatalf("private room %s has incomplete policy: class=%v residency=%v role_mode=%v", id, classification, residency, roleMode)
		}
		seenPolicy++
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if seenPolicy != 3 {
		t.Fatalf("private/group/direct policies = %d, want 3", seenPolicy)
	}
}

func TestChatSeedRepairsLegacyAudiencePoliciesWithoutChangingHistory(t *testing.T) {
	store, db := chatSeedStore(t)
	ctx := context.Background()
	tenant := "harborcare-demo"
	employees, err := chatSeedWorkforce(tenant)
	if err != nil {
		t.Fatal(err)
	}
	people := make([]string, 0, len(employees))
	names := make([]string, 0, len(employees))
	for _, employee := range employees {
		people = append(people, employee.Row.WorkerKey)
		names = append(names, employee.Row.LegalName)
	}
	rooms := chatSeedRoomsForTenant("small", tenant, people)
	adapter := chatstore.NewAdapter(store)
	for _, room := range rooms {
		conversationID := chatSeedConversationID(tenant, room.Key)
		owner, members, memberErr := chatSeedMemberships(tenant, conversationID, room, people)
		if memberErr != nil {
			t.Fatal(memberErr)
		}
		name := room.Name
		if name == "" {
			name = strings.Join(memberNames(names, room.Members), ", ")
		}
		if _, err := adapter.CreateConversation(ctx, chatcore.Conversation{ID: conversationID, TenantID: tenant, Kind: kindOf(room.Kind), Name: name, OwnerID: owner, Revision: 1}, members, "seed:"+room.Key); err != nil {
			t.Fatalf("create pre-policy room %s: %v", room.Key, err)
		}
	}
	var out bytes.Buffer
	opts := chatSeedOptions{Tenant: tenant, Scale: "small", MediaRoot: t.TempDir(), Now: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
	if err = runChatSeedCommand(ctx, store, nil, opts, &out); err != nil {
		t.Fatalf("repair old room policies: %v", err)
	}
	if !strings.Contains(out.String(), "repaired audience policies for 3 existing chat demo rooms") {
		t.Fatalf("repair receipt = %q", out.String())
	}
	var conversations, posts, policies, membershipCount int
	for query, into := range map[string]*int{
		`SELECT count(*) FROM chat_conversation`:                    &conversations,
		`SELECT count(*) FROM chat_post`:                            &posts,
		`SELECT count(*) FROM chat_channel_policy`:                  &policies,
		`SELECT count(*) FROM chat_membership WHERE state='active'`: &membershipCount,
	} {
		if err = db.SQL.QueryRowContext(ctx, query).Scan(into); err != nil {
			t.Fatal(err)
		}
	}
	if conversations != len(rooms) || posts != 0 || policies != 3 || membershipCount == 0 {
		t.Fatalf("post-repair state: conversations=%d posts=%d policies=%d memberships=%d", conversations, posts, policies, membershipCount)
	}
	var wrongRows int
	if err = db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM chat_channel_policy p JOIN chat_conversation c ON c.tenant_id=p.tenant_id AND c.id=p.conversation_id WHERE p.residency<>'us' OR p.role_mode NOT IN (1,2) OR p.classification='' OR c.audience_revision<2`).Scan(&wrongRows); err != nil {
		t.Fatal(err)
	}
	if wrongRows != 0 {
		t.Fatalf("%d repaired policy rows do not match explicit v1 facts", wrongRows)
	}
}

func TestChatSeedAudienceFixtureLoaderRejectsUnsupportedShape(t *testing.T) {
	facts, err := loadChatSeedAudienceFixture()
	if err != nil {
		t.Fatal(err)
	}
	if facts.SchemaVersion != 1 || len(facts.Tenants) != 2 || len(facts.Rooms) != 13 {
		t.Fatalf("fixture facts = version %d, %d tenants, %d rooms", facts.SchemaVersion, len(facts.Tenants), len(facts.Rooms))
	}
}
