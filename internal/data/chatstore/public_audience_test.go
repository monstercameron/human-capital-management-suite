package chatstore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func publicAudienceFixture(t *testing.T) (*Adapter, chat.Principal, PublicAudiencePolicy) {
	t.Helper()
	store, owner := personaReplyFixture(t)
	population := PublicAudiencePolicy{Classification: "INTERNAL", Principals: []PublicAudiencePrincipal{{HomeTenantID: owner.TenantID, SubjectID: owner.SubjectID}, {HomeTenantID: owner.TenantID, SubjectID: "future"}, {HomeTenantID: "tenant-b", SubjectID: "external", Guest: true}}}
	if _, err := store.Store.PutPublicAudiencePolicy(context.Background(), owner.TenantID, "persona-reply", 0, population); err != nil {
		t.Fatal(err)
	}
	ceiling := PersonaChannelPolicy{PlacementClass: "ANY_INTERNAL", MaxTier: "T2", AllowedDataClasses: []string{"PUBLIC", "INTERNAL", "POLICY_DOCUMENT"}, AllowedChannelClasses: []string{"PUBLIC"}}
	if _, err := store.Store.PutPersonaChannelPolicy(context.Background(), owner.TenantID, "persona-reply", 0, ceiling); err != nil {
		t.Fatal(err)
	}
	return store, owner, population
}

func TestTodo_AGENTP_012_PublicAuthority_Integration(t *testing.T) {
	store, owner, population := publicAudienceFixture(t)
	ctx := context.Background()
	snapshot, err := store.Store.CapturePublicAudienceSnapshot(ctx, owner.TenantID, "persona-reply")
	if err != nil || snapshot.Revision == 0 || len(snapshot.Current) != 1 || len(snapshot.Eligible) != 3 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if _, err = store.Store.PutPublicAudiencePolicy(ctx, owner.TenantID, "persona-reply", 0, population); !errors.Is(err, ErrAudiencePolicyConflict) {
		t.Fatalf("create replay=%v", err)
	}
	ceiling, err := store.Store.CapturePersonaChannelPolicy(ctx, owner.TenantID, "persona-reply", owner.SubjectID)
	if err != nil || ceiling.ManagerID != owner.SubjectID || ceiling.Revision != snapshot.Revision || ceiling.Policy.MaxTier != "T2" {
		t.Fatalf("ceiling=%+v err=%v", ceiling, err)
	}
	if _, err = store.Store.CapturePersonaChannelPolicy(ctx, owner.TenantID, "persona-reply", "future"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("nonmanager=%v", err)
	}
	if _, err = store.Store.CapturePublicAudienceSnapshot(ctx, "tenant-b", "persona-reply"); err == nil {
		t.Fatal("foreign tenant read admitted")
	}
	post, err := store.SendPost(ctx, chat.SendPostRequest{Principal: owner, TenantID: owner.TenantID, ConversationID: "persona-reply", IdempotencyKey: "public-source"}, chat.Post{AuthorID: owner.SubjectID, Body: "Vacation policy"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Store.PutPublicChatPostClassification(ctx, owner.TenantID, "persona-reply", post.ID, publicChatBodyDigest(post.Body), dlp.ClassPublic); err != nil {
		t.Fatal(err)
	}
	for _, member := range population.Principals {
		err = store.Store.AuthorizePublicChatDisclosure(ctx, owner.TenantID, "persona-reply", member.HomeTenantID, member.SubjectID, post.ID, publicChatBodyDigest(post.Body), "body", "", "PUBLIC")
		if err != nil {
			t.Fatalf("PUBLIC recipient %s:%s=%v", member.HomeTenantID, member.SubjectID, err)
		}
	}
	for _, tc := range []struct{ home, subject, room, field, title, class string }{
		{owner.TenantID, owner.SubjectID, "another-room", "body", "", "PUBLIC"},
		{owner.TenantID, "stranger", "persona-reply", "body", "", "PUBLIC"},
		{owner.TenantID, owner.SubjectID, "persona-reply", "salary", "", "PUBLIC"},
		{owner.TenantID, owner.SubjectID, "persona-reply", "body", "private title", "PUBLIC"},
		{"tenant-b", "external", "persona-reply", "body", "", "INTERNAL"},
		{owner.TenantID, owner.SubjectID, "persona-reply", "body", "", "COMPENSATION"},
	} {
		if err = store.Store.AuthorizePublicChatDisclosure(ctx, owner.TenantID, tc.room, tc.home, tc.subject, post.ID, publicChatBodyDigest(post.Body), tc.field, tc.title, tc.class); err == nil {
			t.Fatalf("unsafe disclosure admitted: %+v", tc)
		}
	}
	if _, err = store.PutMembership(ctx, owner, chat.Membership{TenantID: owner.TenantID, HomeTenantID: owner.TenantID, ConversationID: "persona-reply", SubjectID: "future", Role: chat.Member, HistoryVisibility: chat.FromJoin}); err != nil {
		t.Fatal(err)
	}
	if err = store.Store.AuthorizePublicChatDisclosure(ctx, owner.TenantID, "persona-reply", owner.TenantID, "future", post.ID, publicChatBodyDigest(post.Body), "body", "", "PUBLIC"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("from-join source disclosure=%v", err)
	}
	if _, err = store.PutMembership(ctx, owner, chat.Membership{TenantID: owner.TenantID, HomeTenantID: owner.TenantID, ConversationID: "persona-reply", SubjectID: "unlisted", Role: chat.Member, HistoryVisibility: chat.FullHistory}); err == nil {
		t.Fatal("unlisted future membership admitted")
	}
}

func TestTodo_AGENTP_012_PublicAuthority_Race(t *testing.T) {
	store, owner, population := publicAudienceFixture(t)
	ctx := context.Background()
	before, err := store.Store.CapturePublicAudienceSnapshot(ctx, owner.TenantID, "persona-reply")
	if err != nil {
		t.Fatal(err)
	}
	locked := make(chan struct{})
	release := make(chan struct{})
	changed := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	var fenceErr error
	go func() {
		defer wg.Done()
		fenceErr = store.Store.WithConversationAuthorityFence(ctx, owner.TenantID, "persona-reply", before.Revision, func(dbport.Tx) error { close(locked); <-release; return nil })
	}()
	<-locked
	population.Principals = append(population.Principals, PublicAudiencePrincipal{HomeTenantID: owner.TenantID, SubjectID: "new-worker"})
	go func() {
		_, err := store.Store.PutPublicAudiencePolicy(ctx, owner.TenantID, "persona-reply", 1, population)
		changed <- err
	}()
	select {
	case err := <-changed:
		close(release)
		wg.Wait()
		t.Fatalf("eligibility crossed held fence: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	if fenceErr != nil {
		t.Fatal(fenceErr)
	}
	if err := <-changed; err != nil {
		t.Fatal(err)
	}
	called := false
	if err := store.Store.WithConversationAuthorityFence(ctx, owner.TenantID, "persona-reply", before.Revision, func(dbport.Tx) error { called = true; return nil }); !errors.Is(err, ErrAudienceChanged) || called {
		t.Fatalf("stale fence err=%v called=%v", err, called)
	}
	current, err := store.Store.CapturePublicAudienceSnapshot(ctx, owner.TenantID, "persona-reply")
	if err != nil || current.Revision <= before.Revision || len(current.Eligible) != 4 {
		t.Fatalf("changed snapshot=%+v err=%v", current, err)
	}
}

func TestTodo_AGENTP_012_PublicAuthority_Property(t *testing.T) {
	for size := 1; size < 60; size++ {
		policy := PublicAudiencePolicy{Classification: "INTERNAL"}
		for i := 0; i < size; i++ {
			policy.Principals = append(policy.Principals, PublicAudiencePrincipal{HomeTenantID: fmt.Sprintf("tenant-%d", i%3), SubjectID: fmt.Sprintf("member-%d", i), Guest: i%7 == 0})
		}
		if !validPublicAudiencePolicy(policy) {
			t.Fatalf("complete explicit population rejected size=%d", size)
		}
		policy.Principals = append(policy.Principals, policy.Principals[size-1])
		if validPublicAudiencePolicy(policy) {
			t.Fatalf("duplicate population admitted size=%d", size)
		}
	}
	for _, policy := range []PersonaChannelPolicy{{}, {MaxTier: "T4", AllowedDataClasses: []string{"PUBLIC"}, AllowedChannelClasses: []string{"PUBLIC"}}, {MaxTier: "T2", AllowedDataClasses: []string{"unknown"}, AllowedChannelClasses: []string{"PUBLIC"}}, {MaxTier: "T2", AllowedDataClasses: []string{"PUBLIC"}, AllowedChannelClasses: []string{"unknown"}}} {
		if validPersonaChannelPolicy(policy) {
			t.Fatalf("malformed ceiling admitted %+v", policy)
		}
	}
}

func TestTodo_AGENTP_012_PublicAuthority_PolicyAndSourceFence(t *testing.T) {
	store, owner, _ := publicAudienceFixture(t)
	ctx := context.Background()
	post, err := store.SendPost(ctx, chat.SendPostRequest{Principal: owner, TenantID: owner.TenantID, ConversationID: "persona-reply", IdempotencyKey: "source"}, chat.Post{AuthorID: owner.SubjectID, Body: "Policy"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Store.CapturePublicAudienceSnapshot(ctx, owner.TenantID, "persona-reply")
	if err != nil {
		t.Fatal(err)
	}
	err = store.Store.RunTenantTx(ctx, owner.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post SET body='edited' WHERE tenant_id=$1 AND id=$2`, owner.TenantID, post.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := store.Store.CapturePublicAudienceSnapshot(ctx, owner.TenantID, "persona-reply")
	if err != nil || after.Revision <= before.Revision {
		t.Fatalf("source edit not fenced %+v %v", after, err)
	}
	post.Body = "edited"
	if err = store.Store.PutPublicChatPostClassification(ctx, owner.TenantID, "persona-reply", post.ID, publicChatBodyDigest(post.Body), dlp.ClassPublic); err != nil {
		t.Fatal(err)
	}
	policy, err := store.Store.CapturePersonaChannelPolicy(ctx, owner.TenantID, "persona-reply", "")
	if err != nil {
		t.Fatal(err)
	}
	policy.Policy.AlwaysPrivate = true
	if _, err = store.Store.PutPersonaChannelPolicy(ctx, owner.TenantID, "persona-reply", policy.PolicyRevision, policy.Policy); err != nil {
		t.Fatal(err)
	}
	if err = store.Store.AuthorizePublicChatDisclosure(ctx, owner.TenantID, "persona-reply", owner.TenantID, owner.SubjectID, post.ID, publicChatBodyDigest(post.Body), "body", "", "PUBLIC"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("always private disclosure=%v", err)
	}
	if _, err = store.Store.PutPersonaChannelPolicy(ctx, owner.TenantID, "persona-reply", policy.PolicyRevision, policy.Policy); !errors.Is(err, ErrAudiencePolicyConflict) {
		t.Fatalf("stale ceiling=%v", err)
	}
	before, err = store.Store.CapturePublicAudienceSnapshot(ctx, owner.TenantID, "persona-reply")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Store.RunTenantTx(ctx, owner.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_conversation SET name='New policy title' WHERE tenant_id=$1 AND id='persona-reply'`, owner.TenantID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	after, err = store.Store.CapturePublicAudienceSnapshot(ctx, owner.TenantID, "persona-reply")
	if err != nil || after.Revision <= before.Revision {
		t.Fatalf("title mutation not fenced %+v %v", after, err)
	}
	if _, err = store.Store.PutPublicAudiencePolicy(ctx, owner.TenantID, "persona-reply", 1, PublicAudiencePolicy{Classification: "PUBLIC", Principals: []PublicAudiencePrincipal{{HomeTenantID: owner.TenantID, SubjectID: "future"}}}); !errors.Is(err, ErrAudienceEligibilityUnavailable) {
		t.Fatalf("omitted current member=%v", err)
	}
}

func TestTodo_AGENTP_012_PublicAuthority_TenantIsolation(t *testing.T) {
	store, owner, _ := publicAudienceFixture(t)
	ctx := context.Background()
	var schema string
	if err := store.Store.RunTx(ctx, func(tx dbport.Tx) error { return tx.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema) }); err != nil {
		t.Fatal(err)
	}
	role := createChatRLSRole(t, store.Store, schema)
	for _, table := range []string{"chat_persona_channel_policy", "chat_persona_source_classification", "chat_public_audience_policy", "chat_public_audience_principal"} {
		if err := runChatAsTenantRole(ctx, store.Store, role, "tenant-b", func(tx dbport.Tx) error {
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE tenant_id=$1`, owner.TenantID).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return fmt.Errorf("foreign %s rows=%d", table, count)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := runChatAsTenantRole(ctx, store.Store, role, "tenant-b", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_public_audience_principal(tenant_id,conversation_id,home_tenant_id,subject_id) VALUES($1,'persona-reply','tenant-b','forged')`, owner.TenantID)
		return err
	}); err == nil {
		t.Fatal("foreign authority mutation admitted")
	}
	if err := store.Store.RunTenantTx(ctx, owner.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,role,state) VALUES($1,'persona-reply',$1,'raw-unlisted','MEMBER','active')`, owner.TenantID)
		return err
	}); err == nil {
		t.Fatal("direct SQL bypassed admission population")
	}
}
