package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestAgentUXAmbient_AutomaticAndNamedTasks_Security_Integration(t *testing.T) {
	s, _, e := agentUXAmbientFixture(t)
	ctx := t.Context()
	s.MemberNames = func(context.Context, string, string) (map[string]string, error) {
		return map[string]string{"luis": "Luis", "peer": "Priya"}, nil
	}
	if err := s.SetGrant(ctx, "tenant-a", "general", "author", AgentUXAmbientGrant{Agent: "task-catcher", Enabled: true, AutomaticPublic: true}); err != nil {
		t.Fatal(err)
	}
	for id, body := range map[string]string{"private-auto": "I'll send the deck", "named": "@Task Catcher add: renew the license, owner Priya, due the 15th", "public-auto": "Can someone book the room?"} {
		agentUXAmbientPost(t, s, id, "author", body)
		if err := s.ProcessMessage(ctx, "tenant-a", "general", id, "task-catcher", "UTC"); err != nil {
			t.Fatal(err)
		}
	}
	for _, person := range []string{"author", "peer"} {
		private, err := s.ListTasks(ctx, "tenant-a", person)
		if err != nil || len(private) != 0 {
			t.Fatalf("private automatic add for %s: %v %v", person, private, err)
		}
	}
	list, err := e.chat.ChannelTodo(ctx, "tenant-a", "tenant-a", "general", "author", func(context.Context) error { return nil })
	if err != nil || len(list.Items) != 1 || list.Items[0].Text != "book the room" {
		t.Fatalf("public auto add: %+v %v", list, err)
	}
	for person, wanted := range map[string]string{"author": "private-auto", "peer": "named", "luis": ""} {
		offers, err := s.ListOffers(ctx, "tenant-a", "general", person)
		if err != nil {
			t.Fatal(err)
		}
		found := ""
		for _, o := range offers {
			if o.Scope == "PRIVATE" {
				if found != "" || o.State != "OFFERED" || o.Person != person {
					t.Fatalf("private recipient/state: %+v", offers)
				}
				found = o.Source
			}
		}
		if found != wanted {
			t.Fatalf("%s private source=%q wanted=%q", person, found, wanted)
		}
	}
}

func TestAgentUXAmbient_SourceChangesAndDuplicates_Integration(t *testing.T) {
	s, _, e := agentUXAmbientFixture(t)
	ctx := t.Context()
	agentUXAmbientPost(t, s, "public-edit", "author", "Can someone book the room?")
	if err := s.ProcessMessage(ctx, "tenant-a", "general", "public-edit", "task-catcher", "UTC"); err != nil {
		t.Fatal(err)
	}
	offers, err := s.ListOffers(ctx, "tenant-a", "general", "author")
	if err != nil || len(offers) != 1 {
		t.Fatalf("initial offer %v %v", offers, err)
	}
	if _, err = s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: offers[0].ID, Action: "ADD", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	agentUXAmbientPost(t, s, "duplicate", "author", "Can someone book the room?")
	if err = s.ProcessMessage(ctx, "tenant-a", "general", "duplicate", "task-catcher", "UTC"); err != nil {
		t.Fatal(err)
	}
	offers, err = s.ListOffers(ctx, "tenant-a", "general", "author")
	if err != nil || len(offers) != 1 {
		t.Fatalf("duplicate open item offered: %v %v", offers, err)
	}
	if err = s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post SET body='Can someone send the invitations?',revision=2 WHERE tenant_id='tenant-a' AND id='public-edit'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.ProcessMessage(ctx, "tenant-a", "general", "public-edit", "task-catcher", "UTC"); err != nil {
		t.Fatal(err)
	}
	list, err := e.chat.ChannelTodo(ctx, "tenant-a", "tenant-a", "general", "author", func(context.Context) error { return nil })
	if err != nil || len(list.Items) != 0 {
		t.Fatalf("source edit kept unfinished task without reconfirmation: %+v %v", list, err)
	}
	offers, err = s.ListOffers(ctx, "tenant-a", "general", "author")
	if err != nil || len(offers) != 1 || offers[0].State != "SOURCE_CHANGED" || offers[0].Title != "send the invitations" {
		t.Fatalf("updated offer: %+v %v", offers, err)
	}
	o := offers[0]
	if _, err = s.Control(ctx, "tenant-a", "general", "author", AgentUXAmbientCommand{ID: o.ID, Action: "ADD", ExpectedRevision: o.Revision}); err != nil {
		t.Fatal(err)
	}
	list, err = e.chat.ChannelTodo(ctx, "tenant-a", "tenant-a", "general", "author", func(context.Context) error { return nil })
	if err != nil || len(list.Items) != 1 || list.Items[0].Text != "send the invitations" {
		t.Fatalf("new consent did not update task: %+v %v", list, err)
	}
	if err = s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post SET tombstoned=true,revision=3 WHERE tenant_id='tenant-a' AND id='public-edit'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.ProcessMessage(ctx, "tenant-a", "general", "public-edit", "task-catcher", "UTC"); err != nil {
		t.Fatal(err)
	}
	list, err = e.chat.ChannelTodo(ctx, "tenant-a", "tenant-a", "general", "author", func(context.Context) error { return nil })
	if err != nil || len(list.Items) != 0 {
		t.Fatalf("deleted source kept task: %+v %v", list, err)
	}
}

func TestAgentUXAmbient_ParentAndSourceVisibility_Security_Integration(t *testing.T) {
	s, m, _ := agentUXAmbientFixture(t)
	ctx := t.Context()
	agentUXAmbientPost(t, s, "parent", "peer", "Private parent context")
	agentUXAmbientPost(t, s, "child", "author", "I'll send the deck")
	if err := s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post SET parent_id='parent' WHERE tenant_id='tenant-a' AND id='child'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOptOut(ctx, "tenant-a", "general", "peer", true); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessMessage(ctx, "tenant-a", "general", "child", "task-catcher", "UTC"); err != nil {
		t.Fatal(err)
	}
	if len(m.inputs) != 1 || m.inputs[0].ThreadParent != "" || m.inputs[0].Message.Parent != "" {
		t.Fatalf("opted out parent read: %+v", m.inputs)
	}
	var parent string
	if err := s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT parent_id FROM agentux_ambient_read WHERE tenant_id='tenant-a' AND post_id='child'`).Scan(&parent)
	}); err != nil || parent != "" {
		t.Fatalf("run falsely claims parent read: %q %v", parent, err)
	}
	agentUXAmbientPost(t, s, "group", "author", "Can someone book the room?")
	if err := s.ProcessMessage(ctx, "tenant-a", "general", "group", "task-catcher", "UTC"); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_membership SET history_visibility='FROM_JOIN',joined_at=$1 WHERE tenant_id='tenant-a' AND conversation_id='general' AND member_id='luis'`, s.Now().Add(time.Hour))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	private, err := s.ListOffers(ctx, "tenant-a", "general", "luis")
	if err != nil || len(private) != 0 {
		t.Fatalf("unreadable source leaked through offer: %v %v", private, err)
	}
	offers, err := s.ListOffers(ctx, "tenant-a", "general", "author")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range offers {
		if o.Source == "group" {
			if _, err = s.Control(ctx, "tenant-a", "general", "luis", AgentUXAmbientCommand{ID: o.ID, Action: "ADD", ExpectedRevision: o.Revision}); !errors.Is(err, ErrAgentUXAmbientDenied) {
				t.Fatalf("unreadable source added: %v", err)
			}
		}
	}
}
