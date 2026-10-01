package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_AGENT_015_AudienceSnapshotIncludesExactMembersAndPolicy(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "private", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{
		{MemberID: "owner", HomeTenantID: "tenant-a", Role: "manager", State: "active"},
		{MemberID: "guest", HomeTenantID: "tenant-b", Role: "member", State: "active"},
		{MemberID: "left", HomeTenantID: "tenant-a", Role: "member", State: "removed"},
	})
	if err := s.RunTenantTx(context.Background(), "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO chat_channel_policy(tenant_id,conversation_id,revision,required_roles,role_mode,classification) VALUES('tenant-a','private',3,'{manager}',1,'T2')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.CaptureAudienceSnapshot(context.Background(), "tenant-a", "private")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PolicyRevision != 3 || snapshot.Classification != "T2" || len(snapshot.Members) != 2 {
		t.Fatalf("snapshot = %+v, want two active members and policy revision 3", snapshot)
	}
	if snapshot.Members[1].HomeTenantID != "tenant-b" || snapshot.Members[1].MemberID != "guest" || !snapshot.Members[1].External {
		t.Fatalf("guest member was not preserved: %+v", snapshot.Members)
	}
	if snapshot.Digest == "" || snapshot.SnapshotID != "chat-audience-"+snapshot.Digest {
		t.Fatalf("snapshot identity = %q/%q", snapshot.SnapshotID, snapshot.Digest)
	}
	if err := s.WithAudienceFence(context.Background(), snapshot, func(tx dbport.Tx) error { return nil }); err != nil {
		t.Fatalf("unchanged audience fence: %v", err)
	}
}

func TestTodo_AGENT_015_AudienceFenceRefusesMembershipOrPolicyMutation(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "private", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", HomeTenantID: "tenant-a", Role: "manager", State: "active"}})
	if err := s.RunTenantTx(context.Background(), "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO chat_channel_policy(tenant_id,conversation_id,revision,role_mode,classification) VALUES('tenant-a','private',1,1,'T2')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.CaptureAudienceSnapshot(context.Background(), "tenant-a", "private")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(context.Background(), "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE chat_membership SET role='member' WHERE tenant_id='tenant-a' AND conversation_id='private' AND member_id='owner'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WithAudienceFence(context.Background(), snapshot, func(tx dbport.Tx) error { return nil }); !errors.Is(err, ErrAudienceChanged) {
		t.Fatalf("membership mutation error = %v, want ErrAudienceChanged", err)
	}
	updated, err := s.CaptureAudienceSnapshot(context.Background(), "tenant-a", "private")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(context.Background(), "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO chat_channel_policy(tenant_id,conversation_id,revision,classification,role_mode) VALUES('tenant-a','private',2,'T3',1) ON CONFLICT (tenant_id,conversation_id) DO UPDATE SET revision=2,classification='T3'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WithAudienceFence(context.Background(), updated, func(tx dbport.Tx) error { return nil }); !errors.Is(err, ErrAudienceChanged) {
		t.Fatalf("policy mutation error = %v, want ErrAudienceChanged", err)
	}
}

func TestTodo_AGENT_015_PublicAudienceFailsClosedWithoutExternalFence(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "public", TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", HomeTenantID: "tenant-a", Role: "manager", State: "active"}})
	_, err := s.CaptureAudienceSnapshot(context.Background(), "tenant-a", "public")
	if !errors.Is(err, ErrAudienceEligibilityUnavailable) {
		t.Fatalf("public snapshot error = %v, want ErrAudienceEligibilityUnavailable", err)
	}
}

func TestTodo_AGENT_015_RacePolicyUpdateWaitsForAudienceFence(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "private", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", HomeTenantID: "tenant-a", Role: "manager", State: "active"}})
	if err := s.RunTenantTx(context.Background(), "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO chat_channel_policy(tenant_id,conversation_id,revision,role_mode,classification) VALUES('tenant-a','private',1,1,'T2')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.CaptureAudienceSnapshot(context.Background(), "tenant-a", "private")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	fenceDone := make(chan error, 1)
	go func() {
		fenceDone <- s.WithAudienceFence(context.Background(), snapshot, func(tx dbport.Tx) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered

	updateStarted := make(chan struct{})
	updateDone := make(chan error, 1)
	go func() {
		tx, beginErr := s.pool.Begin(context.Background())
		if beginErr != nil {
			updateDone <- beginErr
			return
		}
		defer tx.Rollback(context.Background())
		if err := tenant(context.Background(), tx, "tenant-a"); err != nil {
			updateDone <- err
			return
		}
		close(updateStarted)
		if _, err := tx.Exec(context.Background(), `UPDATE chat_channel_policy SET revision=2,classification='T3' WHERE tenant_id='tenant-a' AND conversation_id='private'`); err != nil {
			updateDone <- err
			return
		}
		updateDone <- tx.Commit(context.Background())
	}()
	<-updateStarted
	select {
	case err := <-updateDone:
		t.Fatalf("policy update committed while fence callback was active: %v", err)
	default:
	}
	close(release)
	if err := <-fenceDone; err != nil {
		t.Fatalf("fence callback: %v", err)
	}
	if err := <-updateDone; err != nil {
		t.Fatalf("policy update after fence release: %v", err)
	}
	updated, err := s.CaptureAudienceSnapshot(context.Background(), "tenant-a", "private")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Digest == snapshot.Digest || updated.PolicyRevision != 2 || updated.Classification != "T3" {
		t.Fatalf("updated snapshot = %+v, want changed policy authority", updated)
	}
	if err := s.WithAudienceFence(context.Background(), snapshot, func(tx dbport.Tx) error { return nil }); !errors.Is(err, ErrAudienceChanged) {
		t.Fatalf("stale snapshot fence = %v, want ErrAudienceChanged", err)
	}
}
