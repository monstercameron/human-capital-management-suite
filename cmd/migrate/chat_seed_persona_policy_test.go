package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func TestTodo_AGENTP_007_LocalDemoBootstrapIntegration(t *testing.T) {
	store, db := chatSeedStore(t)
	ctx := context.Background()
	tenant := "ironridge-demo"
	if err := runChatSeedCommand(ctx, store, nil, chatSeedOptions{Tenant: tenant, Scale: "small", MediaRoot: t.TempDir(), Now: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	pack, _ := demoworkforce.PackFor(tenant)
	employees, err := pack.Plan(pgstore.TenantID(tenant))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, employee := range employees {
		if employee.Row.WorkerType == "employee" && employee.Row.LifecycleStatus == "active" {
			count++
		}
	}
	general := chatSeedConversationID(tenant, "general")
	population, err := store.CapturePublicAudienceSnapshot(ctx, tenant, general)
	if err != nil || len(population.Eligible) != count || len(population.Current) == 0 || population.Classification != "INTERNAL" {
		t.Fatalf("population eligible=%d want=%d current=%d classification=%s err=%v", len(population.Eligible), count, len(population.Current), population.Classification, err)
	}
	ceiling, err := store.CapturePersonaChannelPolicy(ctx, tenant, general, "")
	if err != nil || ceiling.Policy.MaxTier != "T0" || ceiling.Policy.PlacementClass != "ANY_INTERNAL" || ceiling.Policy.AlwaysPrivate || ceiling.Revision != population.Revision {
		t.Fatalf("public ceiling=%+v population revision=%d err=%v", ceiling, population.Revision, err)
	}
	private, err := store.CapturePersonaChannelPolicy(ctx, tenant, chatSeedConversationID(tenant, "leadership-private"), "")
	if err != nil || !private.Policy.AlwaysPrivate || private.Policy.MaxTier != "T1" || private.Policy.PlacementClass != "MANAGER" {
		t.Fatalf("private ceiling=%+v err=%v", private, err)
	}
	var versions int
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM chat_persona_channel_policy WHERE tenant_id=$1`, tenant).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 6 {
		t.Fatalf("room policies=%d want6", versions)
	}
	var publicPosts, classifiedPosts int
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM chat_post p JOIN chat_conversation c ON c.tenant_id=p.tenant_id AND c.id=p.conversation_id WHERE p.tenant_id=$1 AND c.kind=$2`, tenant, chat.PublicChannel).Scan(&publicPosts); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM chat_persona_source_classification c
		JOIN chat_post p ON p.tenant_id=c.tenant_id AND p.conversation_id=c.conversation_id AND p.id=c.post_id
		JOIN chat_conversation conv ON conv.tenant_id=p.tenant_id AND conv.id=p.conversation_id
		WHERE c.tenant_id=$1 AND conv.kind=$2`, tenant, chat.PublicChannel).Scan(&classifiedPosts); err != nil {
		t.Fatal(err)
	}
	if publicPosts == 0 || classifiedPosts != publicPosts {
		t.Fatalf("public posts=%d classified=%d", publicPosts, classifiedPosts)
	}
	assertClass := func(room string, scenario moderationScenario, want dlp.DataClass) {
		t.Helper()
		var got string
		query := `SELECT c.data_class FROM chat_post p JOIN chat_persona_source_classification c ON c.tenant_id=p.tenant_id AND c.conversation_id=p.conversation_id AND c.post_id=p.id WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.body=$3`
		if err := db.SQL.QueryRowContext(ctx, query, tenant, chatSeedConversationID(tenant, room), scenario.Body).Scan(&got); err != nil {
			t.Fatalf("classification for %s: %v", scenario.Key, err)
		}
		if got != string(want) {
			t.Fatalf("classification for %s=%s want=%s", scenario.Key, got, want)
		}
	}
	assertClass("people-ops", modHarassment, dlp.ClassInternal)
	assertClass("general", modRemoved, dlp.ClassPII)
	assertClass("leadership-private", modPrivate, dlp.ClassCompensation)
	if _, err = provisionChatSeedPersonaPolicies(ctx, store, tenant, chatSeedRoomsForTenant("small", tenant, func() []string {
		out := make([]string, len(employees))
		for i := range employees {
			out[i] = employees[i].Row.WorkerKey
		}
		return out
	}())); err != nil {
		t.Fatal(err)
	}
	replay, err := store.CapturePersonaChannelPolicy(ctx, tenant, general, "")
	if err != nil || replay.PolicyRevision != ceiling.PolicyRevision || replay.Revision != ceiling.Revision {
		t.Fatalf("replay changed authority %+v err=%v", replay, err)
	}
}

func TestTodo_AGENTP_007_LocalDemoCorpusClassificationSecurity(t *testing.T) {
	in := seedRoomInput{opts: chatSeedOptions{Tenant: "ironridge-demo"}, room: roomSpec{Key: "general", Kind: roomPublic, Members: []int{0, 1}, Topics: []string{"Checked-in fictional policy."}}, people: []string{"ir-001-walt-brennan", "ir-003-loretta-haynes"}, names: []string{"Walt Brennan", "Loretta Haynes"}, conversation: chatSeedConversationID("ironridge-demo", "general")}
	source := chatSeedPersonaPostSource{in: in, index: 0, reply: -1, id: "accepted-post"}
	post := chat.Post{ID: "accepted-post", TenantID: in.opts.Tenant, ConversationID: in.conversation, AuthorID: in.people[0], AuthorHomeTenantID: in.opts.Tenant, Body: in.room.Topics[0]}
	if !source.IsKnownSyntheticPersonaPost(context.Background(), post) {
		t.Fatal("exact fictional corpus post was refused")
	}
	for _, change := range []func(*chat.Post){func(p *chat.Post) { p.Body += " altered" }, func(p *chat.Post) { p.AuthorID = in.people[1] }, func(p *chat.Post) { p.TenantID = "production" }, func(p *chat.Post) { p.ConversationID = "other" }, func(p *chat.Post) { p.ParentID = "other" }, func(p *chat.Post) { p.ID = "other" }, func(p *chat.Post) { p.Deleted = true }} {
		altered := post
		change(&altered)
		if source.IsKnownSyntheticPersonaPost(context.Background(), altered) {
			t.Fatalf("altered source was classified: %+v", altered)
		}
	}
	in.room.Kind = roomPrivate
	source.in = in
	if source.IsKnownSyntheticPersonaPost(context.Background(), post) {
		t.Fatal("private corpus was admitted as public")
	}
}

func TestTodo_AGENTP_007_ModerationSourceValidation(t *testing.T) {
	const tenant = "ironridge-demo"
	employees, err := chatSeedWorkforce(tenant)
	if err != nil {
		t.Fatal(err)
	}
	people := make([]string, len(employees))
	for i := range employees {
		people[i] = employees[i].Row.WorkerKey
	}
	adminIndex := adminIndexForTenant(tenant, people)
	if adminIndex < 0 {
		t.Fatal("demo administrator missing")
	}
	admin := people[adminIndex]
	for _, scenario := range []moderationScenario{modHarassment, modPrivate} {
		t.Run(scenario.Key, func(t *testing.T) {
			var room roomSpec
			for _, candidate := range chatSeedRoomsForTenant("full", tenant, people) {
				if candidate.Key == scenario.Room {
					room = candidate
					break
				}
			}
			if room.Key == "" {
				t.Fatal("moderation room missing")
			}
			source := chatSeedModerationPersonaPostSource{tenant: tenant, people: people, admin: admin, scenario: scenario, id: "seeded-post"}
			post := chat.Post{ID: "seeded-post", TenantID: tenant, ConversationID: chatSeedConversationID(tenant, scenario.Room), AuthorID: chatSeedModerationMember(room, people, admin, scenario.Slot), AuthorHomeTenantID: tenant, Body: scenario.Body}
			if !source.IsKnownSyntheticPersonaPost(context.Background(), post) {
				t.Fatal("exact public/private moderation fixture was refused")
			}
			for _, change := range []func(*chat.Post){
				func(p *chat.Post) { p.ID = "other-post" },
				func(p *chat.Post) { p.TenantID = "other-tenant" },
				func(p *chat.Post) { p.ConversationID = "other-room" },
				func(p *chat.Post) { p.AuthorID = admin },
				func(p *chat.Post) { p.AuthorHomeTenantID = "other-tenant" },
				func(p *chat.Post) { p.Body += " altered" },
				func(p *chat.Post) { p.ParentID = "unexpected-parent" },
				func(p *chat.Post) { p.Deleted = true },
			} {
				altered := post
				change(&altered)
				if source.IsKnownSyntheticPersonaPost(context.Background(), altered) {
					t.Fatalf("altered moderation fixture was classified: %+v", altered)
				}
			}
			wrongScenario := source
			wrongScenario.scenario.Body += " altered"
			if wrongScenario.IsKnownSyntheticPersonaPost(context.Background(), post) {
				t.Fatal("altered scenario definition was accepted")
			}
		})
	}
}
