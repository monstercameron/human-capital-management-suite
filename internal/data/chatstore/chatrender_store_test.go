package chatstore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type chatrenderPresentFixture struct{ readers []chatrender.Preference }

func (p chatrenderPresentFixture) RenderingReaders(context.Context, string, string) ([]chatrender.Preference, error) {
	return p.readers, nil
}
func chatrenderDB(t *testing.T) (*RenderingAdapter, RenderingScope, chat.Post) {
	t.Helper()
	base := adapterDB(t)
	s := &RenderingAdapter{Adapter: base}
	ctx := context.Background()
	c := chat.Conversation{ID: "render-room", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "Languages", OwnerID: "alice", Revision: 1}
	members := []chat.Membership{{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}, {ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "bob", Role: chat.Member, HistoryVisibility: chat.FullHistory}}
	if _, err := s.CreateConversation(ctx, c, members, ""); err != nil {
		t.Fatal(err)
	}
	scope := RenderingScope{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, Tenant: c.TenantID, Conversation: c.ID}
	p, err := s.SendPost(ctx, chat.SendPostRequest{Principal: scope.Principal, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "render-post"}, chat.Post{ID: "render-post", TenantID: c.TenantID, ConversationID: c.ID, AuthorID: "alice", AuthorHomeTenantID: c.TenantID, Body: "Bitte lesen wir die Nachricht heute mit dem Team.", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	return s, scope, p
}
func chatrenderTarget(scope RenderingScope, p chat.Post) chatrender.Rendering {
	return chatrender.Rendering{Tenant: scope.Tenant, Message: p.ID, Revision: p.Revision, Tone: chatrender.AsWritten, Language: "en"}
}
func chatrenderProduce(t *testing.T, s *Store, scope RenderingScope, p chat.Post) chatrender.Rendering {
	t.Helper()
	ctx := context.Background()
	r := chatrenderTarget(scope, p)
	if err := s.RequestRendering(ctx, scope, r); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimRendering(ctx, scope.Tenant, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r, err = (chatrender.FixtureProducer{Text: "Please read this message with your team today.", Language: "en", Identity: chatrender.ProducerIdentity{Provider: "fixture", Model: "fixed", InstructionDigest: "sha256:fixture", GlossaryVersion: "v1"}}).Produce(ctx, job.Request)
	r.CostReference = "fixture:no-charge"
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteRendering(ctx, job, r); err != nil {
		t.Fatal(err)
	}
	return r
}
func TestTodo_CHATRENDER_001_Integration(t *testing.T) {
	s, scope, p := chatrenderDB(t)
	ctx := context.Background()
	target := chatrenderTarget(scope, p)
	var requests sync.WaitGroup
	requestErrors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		requests.Add(1)
		go func() { defer requests.Done(); requestErrors <- s.RequestRendering(ctx, scope, target) }()
	}
	requests.Wait()
	close(requestErrors)
	for err := range requestErrors {
		if err != nil {
			t.Fatal(err)
		}
	}
	a, err := s.RenderingAvailability(ctx, scope, p.ID)
	if err != nil || !a.Pending || a.Failed {
		t.Fatal(a, err)
	}
	r := chatrenderProduce(t, s.Store, scope, p)
	listed, err := s.ListRenderings(ctx, scope, []string{p.ID})
	if err != nil || len(listed) != 1 || listed[0].Text != r.Text || listed[0].SourceLanguage != "de" || listed[0].CostReference != "fixture:no-charge" {
		t.Fatal(listed, err)
	}
	search, err := s.SearchRenderings(ctx, scope, "team")
	if err != nil || len(search) != 1 {
		t.Fatal(search, err)
	}
	if err = s.ReportRendering(ctx, scope, r, "Meaning is unclear"); err != nil {
		t.Fatal(err)
	}
	var reports int
	if err = s.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM chatrender_report WHERE tenant_id=$1`, scope.Tenant).Scan(&reports)
	}); err != nil || reports != 1 {
		t.Fatal(reports, err)
	}
	edit, err := s.EditPost(ctx, chat.EditPostRequest{Principal: scope.Principal, TenantID: scope.Tenant, ConversationID: scope.Conversation, PostID: p.ID, Body: "Please read this message with your team today.", ExpectedRevision: p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	listed, err = s.ListRenderings(ctx, scope, []string{edit.ID})
	if err != nil || len(listed) != 0 {
		t.Fatal("old revision leaked", listed, err)
	}
	d, err := s.RevisionLanguage(ctx, scope, p.ID, edit.Revision)
	if err != nil || d.Language != "en" {
		t.Fatal(d, err)
	}
	if _, err = s.DeletePost(ctx, chat.DeletePostRequest{Principal: scope.Principal, TenantID: scope.Tenant, ConversationID: scope.Conversation, PostID: p.ID, ExpectedRevision: edit.Revision}); err != nil {
		t.Fatal(err)
	}
	var views, jobs int
	err = s.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM chatrender_rendering WHERE tenant_id=$1),(SELECT count(*) FROM chatrender_job WHERE tenant_id=$1)`, scope.Tenant).Scan(&views, &jobs)
	})
	if err != nil || views != 0 || jobs != 0 {
		t.Fatal(views, jobs, err)
	}
}
func TestTodo_CHATRENDER_001_Security(t *testing.T) {
	s, scope, p := chatrenderDB(t)
	ctx := context.Background()
	r := chatrenderProduce(t, s.Store, scope, p)
	bad := scope
	bad.Principal.SubjectID = "mallory"
	if _, err := s.ListRenderings(ctx, bad, []string{p.ID}); !errors.Is(err, chatrender.ErrDenied) {
		t.Fatal(err)
	}
	if _, err := s.SearchRenderings(ctx, bad, "team"); !errors.Is(err, chatrender.ErrDenied) {
		t.Fatal(err)
	}
	if err := s.RequestRendering(ctx, bad, r); !errors.Is(err, chatrender.ErrDenied) {
		t.Fatal(err)
	}
	if err := s.ReportRendering(ctx, bad, r, "bad"); !errors.Is(err, chatrender.ErrDenied) {
		t.Fatal(err)
	}
	bad = scope
	bad.Tenant = "tenant-b"
	if _, err := s.ListRenderings(ctx, bad, []string{p.ID}); !errors.Is(err, chatrender.ErrDenied) {
		t.Fatal(err)
	}
	// History exclusion applies before any rendering or search output.
	if err := s.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_membership SET history_visibility='FROM_JOIN',joined_at=now()+interval '1 hour' WHERE tenant_id=$1 AND member_id='bob'`, scope.Tenant)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	bob := scope
	bob.Principal.SubjectID = "bob"
	list, err := s.ListRenderings(ctx, bob, []string{p.ID})
	if err != nil || len(list) != 0 {
		t.Fatal(list, err)
	}
	// RLS is forced on every new table, including the production queue.
	var n int
	err = s.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM pg_class WHERE relnamespace=current_schema()::regnamespace AND relname IN ('chatrender_rendering','chatrender_job','chatrender_report') AND relrowsecurity AND relforcerowsecurity`).Scan(&n)
	})
	if err != nil || n != 3 {
		t.Fatal(n, err)
	}
	var schema string
	if err = s.RunTx(ctx, func(tx dbport.Tx) error { return tx.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema) }); err != nil {
		t.Fatal(err)
	}
	role := createChatRLSRole(t, s.Store, schema)
	for _, table := range []string{"chatrender_rendering", "chatrender_job", "chatrender_report"} {
		var visible int
		if err = runChatAsTenantRole(ctx, s.Store, role, "tenant-b", func(tx dbport.Tx) error { return tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&visible) }); err != nil || visible != 0 {
			t.Fatal(table, visible, err)
		}
	}
	if err = runChatAsTenantRole(ctx, s.Store, role, "tenant-b", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chatrender_rendering(tenant_id,post_id,revision,tone,language,rendering) VALUES('tenant-a',$1,1,'as-written','ar','{}')`, p.ID)
		return err
	}); err == nil {
		t.Fatal("RLS accepted cross-tenant write")
	}
	if err = runChatAsTenantRole(ctx, s.Store, role, "tenant-b", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chatrender_rendering(tenant_id,post_id,revision,tone,language,rendering) VALUES('tenant-b',$1,1,'as-written','ar','{}')`, p.ID)
		return err
	}); err == nil {
		t.Fatal("foreign key accepted cross-tenant parent")
	}
	target := chatrenderTarget(scope, p)
	target.Language = "ar"
	if err = s.RequestRendering(ctx, scope, target); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimRendering(ctx, scope.Tenant, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	forged := job
	forged.Request.Kinds = []chatrender.Kind{chatrender.Mask}
	forged.Request.SourceLanguage = "fr"
	produced, err := (chatrender.FixtureProducer{Text: "مرحبًا", Language: "ar"}).Produce(ctx, job.Request)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteRendering(ctx, forged, produced); err != nil {
		t.Fatal(err)
	}
	views, err := s.ListRenderings(ctx, scope, []string{p.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range views {
		if view.Language == "ar" && (view.SourceLanguage != "de" || len(view.Kinds) != 1 || view.Kinds[0] != chatrender.Translate) {
			t.Fatal("forged job metadata trusted", view)
		}
	}
	if err = s.CompleteRendering(ctx, job, produced); !errors.Is(err, chatrender.ErrLease) {
		t.Fatal("completed job replay", err)
	}
}
func TestTodo_CHATRENDER_001_Fault(t *testing.T) {
	s, scope, p := chatrenderDB(t)
	ctx := context.Background()
	r := chatrenderTarget(scope, p)
	if err := s.RequestRendering(ctx, scope, r); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		job, err := s.ClaimRendering(ctx, scope.Tenant, time.Minute)
		if err != nil || job.Attempts != attempt {
			t.Fatal(job, err)
		}
		forged := job
		forged.Lease = "forged"
		if err = s.FailRendering(ctx, forged); !errors.Is(err, chatrender.ErrLease) {
			t.Fatal(err)
		}
		if err = s.FailRendering(ctx, job); err != nil {
			t.Fatal(err)
		}
		if err = s.FailRendering(ctx, job); !errors.Is(err, chatrender.ErrLease) {
			t.Fatal("replay", err)
		}
	}
	if _, err := s.ClaimRendering(ctx, scope.Tenant, time.Minute); !errors.Is(err, dbport.ErrNoRows) {
		t.Fatal(err)
	}
	a, err := s.RenderingAvailability(ctx, scope, p.ID)
	if err != nil || !a.Failed || a.Pending {
		t.Fatal(a, err)
	}
	if _, err = s.ClaimRendering(ctx, scope.Tenant, 0); !errors.Is(err, chatrender.ErrInvalid) {
		t.Fatal(err)
	}
	r.Language = "ar"
	if err = s.RequestRendering(ctx, scope, r); err != nil {
		t.Fatal(err)
	}
	var prior RenderingJob
	for attempt := 1; attempt <= 3; attempt++ {
		job, err := s.ClaimRendering(ctx, scope.Tenant, time.Minute)
		if err != nil || job.Attempts != attempt {
			t.Fatal(job, err)
		}
		if prior.Lease != "" && job.Lease == prior.Lease {
			t.Fatal("lease token reused")
		}
		prior = job
		if err = s.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE chatrender_job SET lease_until=now()-interval '1 second' WHERE tenant_id=$1 AND language='ar'`, scope.Tenant)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		out, _ := (chatrender.FixtureProducer{Text: "Arabic fixture", Language: "ar"}).Produce(ctx, job.Request)
		if err = s.CompleteRendering(ctx, job, out); !errors.Is(err, chatrender.ErrLease) {
			t.Fatal("expired lease completed", err)
		}
	}
	if _, err = s.ClaimRendering(ctx, scope.Tenant, time.Minute); !errors.Is(err, dbport.ErrNoRows) {
		t.Fatal(err)
	}
	a, err = s.RenderingAvailabilityForTarget(ctx, scope, p.ID, chatrender.AsWritten, "ar")
	if err != nil || !a.Failed || a.Pending {
		t.Fatal("expired job not terminal", a, err)
	}
	a, err = s.RenderingAvailabilityForTarget(ctx, scope, p.ID, chatrender.AsWritten, "fr")
	if err != nil || a.Failed || a.Pending {
		t.Fatal("another language's failure applied", a, err)
	}
}
func TestTodo_CHATLANG_002_Integration(t *testing.T) {
	s, scope, p := chatrenderDB(t)
	ctx := context.Background()
	d, err := s.RevisionLanguage(ctx, scope, p.ID, p.Revision)
	if err != nil || d.Language != "de" || d.Confidence <= 0 {
		t.Fatal(d, err)
	}
	if err = s.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post_revision SET source_language='und' WHERE tenant_id=$1 AND post_id=$2`, scope.Tenant, p.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	reader := chatrender.DefaultPreference("en")
	reader.Translate = true
	rollback := errors.New("fixture rollback")
	err = s.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
		if err := RecordRenderingRevisionTx(ctx, tx, scope.Tenant, p.ID, p.Revision, []chatrender.Preference{reader}); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	d, err = s.RevisionLanguage(ctx, scope, p.ID, p.Revision)
	availability, availabilityErr := s.RenderingAvailability(ctx, scope, p.ID)
	if err != nil || availabilityErr != nil || d.Language != "und" || availability.Pending {
		t.Fatal("observer escaped transaction", d, availability, err, availabilityErr)
	}
	if err = s.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
		return RecordRenderingRevisionTx(ctx, tx, scope.Tenant, p.ID, p.Revision, []chatrender.Preference{reader})
	}); err != nil {
		t.Fatal(err)
	}
	pref, err := s.LanguageSettings(ctx, scope, "de-DE")
	if err != nil || pref.ReadingLanguage != "de" {
		t.Fatal(pref, err)
	}
	global := scope
	global.Conversation = ""
	pref = chatrender.DefaultPreference("en-US")
	pref.Translate = true
	pref.FurtherLanguages = []string{"fr"}
	pref.SourceOverrides = map[string]bool{"es": false}
	if err = s.PutLanguageSettings(ctx, global, pref); err != nil {
		t.Fatal(err)
	}
	got, err := s.LanguageSettings(ctx, scope, "ar")
	if err != nil || got.ReadingLanguage != "en" || !got.Translate || len(got.FurtherLanguages) != 1 {
		t.Fatal(got, err)
	}
	pref.ReadingLanguage = "ar"
	if err = s.PutLanguageSettings(ctx, scope, pref); err != nil {
		t.Fatal(err)
	}
	got, err = s.LanguageSettings(ctx, scope, "en")
	if err != nil || got.ReadingLanguage != "ar" {
		t.Fatal(got, err)
	}
	counts, err := s.ConversationLanguages(ctx, scope)
	if err != nil || counts["ar"] != 1 || counts["und"] != 1 {
		t.Fatal(counts, err)
	}
	_ = chatrenderProduce(t, s.Store, scope, p)
	if err = s.CorrectRevisionLanguage(ctx, scope, p.ID, p.Revision, "fr"); err != nil {
		t.Fatal(err)
	}
	d, err = s.RevisionLanguage(ctx, scope, p.ID, p.Revision)
	if err != nil || d.Language != "fr" || !d.Corrected {
		t.Fatal(d, err)
	}
	if err = s.RecordRevisionLanguage(ctx, scope.Tenant, p.ID, p.Revision); err != nil {
		t.Fatal(err)
	}
	d, err = s.RevisionLanguage(ctx, scope, p.ID, p.Revision)
	if err != nil || d.Language != "fr" {
		t.Fatal("correction overwritten", d, err)
	}
	found, err := s.SearchMessageLanguages(ctx, scope, "fr")
	if err != nil || len(found) != 1 || found[0].Message != p.ID || !found[0].Detection.Corrected {
		t.Fatal(found, err)
	}
	views, err := s.ListRenderings(ctx, scope, []string{p.ID})
	if err != nil || len(views) != 0 {
		t.Fatal(views, err)
	}
	// Exercise the eager production observer on a subsequent send.
	eager := chatrender.DefaultPreference("en")
	eager.Translate = true
	s.Present = chatrenderPresentFixture{[]chatrender.Preference{eager, eager}}
	next, err := s.SendPost(ctx, chat.SendPostRequest{Principal: scope.Principal, TenantID: scope.Tenant, ConversationID: scope.Conversation, IdempotencyKey: "eager"}, chat.Post{ID: "eager", TenantID: scope.Tenant, ConversationID: scope.Conversation, AuthorID: "alice", AuthorHomeTenantID: scope.Tenant, Body: "Bitte lesen wir die Nachricht heute mit dem Team.", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimRendering(ctx, scope.Tenant, time.Minute)
	if err != nil || job.Request.Message != next.ID {
		t.Fatal(job, err)
	}
	if _, err = s.ClaimRendering(ctx, scope.Tenant, time.Minute); !errors.Is(err, dbport.ErrNoRows) {
		t.Fatal("duplicate eager job", err)
	}
	service := chat.NewService(s, time.Now)
	service.SetAuthority(forwardingAuthority{store: s.Adapter})
	short, err := service.SendPost(ctx, chat.SendPostRequest{Principal: scope.Principal, TenantID: scope.Tenant, ConversationID: scope.Conversation, IdempotencyKey: "short", Body: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	d, err = s.RevisionLanguage(ctx, scope, short.ID, short.Revision)
	if err != nil || d.Language != "und" {
		t.Fatal(d, err)
	}
	if err = s.RequestRendering(ctx, scope, chatrenderTarget(scope, short)); !errors.Is(err, chatrender.ErrInvalid) {
		t.Fatal("undetermined message translated", err)
	}
	edited, err := service.EditPost(ctx, chat.EditPostRequest{Principal: scope.Principal, TenantID: scope.Tenant, ConversationID: scope.Conversation, PostID: short.ID, ExpectedRevision: short.Revision, Body: "Please read this message with your team today."})
	if err != nil {
		t.Fatal(err)
	}
	d, err = s.RevisionLanguage(ctx, scope, edited.ID, edited.Revision)
	if err != nil || d.Language != "en" {
		t.Fatal("service edit hook", d, err)
	}
}
func TestTodo_CHATLANG_002_Security(t *testing.T) {
	s, scope, p := chatrenderDB(t)
	ctx := context.Background()
	bob := scope
	bob.Principal.SubjectID = "bob"
	if err := s.CorrectRevisionLanguage(ctx, bob, p.ID, p.Revision, "en"); !errors.Is(err, chatrender.ErrDenied) {
		t.Fatal(err)
	}
	bad := scope
	bad.Tenant = "tenant-b"
	if _, err := s.LanguageSettings(ctx, bad, "en"); !errors.Is(err, chatrender.ErrDenied) {
		t.Fatal(err)
	}
	if err := s.PutLanguageSettings(ctx, bad, chatrender.DefaultPreference("en")); !errors.Is(err, chatrender.ErrDenied) {
		t.Fatal(err)
	}
	var err error
	err = s.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post_revision SET body='forged' WHERE tenant_id=$1 AND post_id=$2`, scope.Tenant, p.ID)
		return err
	})
	if err == nil {
		t.Fatal("revision body mutable")
	}
	bob.Principal.SubjectID = "mallory"
	if _, err = s.ConversationLanguages(ctx, bob); !errors.Is(err, chatrender.ErrDenied) {
		t.Fatal(err)
	}
	if _, err = s.PutMembership(ctx, scope.Principal, chat.Membership{TenantID: scope.Tenant, ConversationID: scope.Conversation, HomeTenantID: "tenant-b", SubjectID: "guest", Role: chat.Member, HistoryVisibility: chat.FullHistory}); err != nil {
		t.Fatal(err)
	}
	guest := scope
	guest.Principal = chat.Principal{TenantID: "tenant-b", SubjectID: "guest"}
	globalGuest := guest
	globalGuest.Tenant = "tenant-b"
	globalGuest.Conversation = ""
	reading := chatrender.DefaultPreference("fr")
	if err = s.PutLanguageSettings(ctx, globalGuest, reading); err != nil {
		t.Fatal(err)
	}
	reading, err = s.LanguageSettings(ctx, guest, "ar")
	if err != nil || reading.ReadingLanguage != "fr" {
		t.Fatal("guest default unavailable", reading, err)
	}
	reading.ReadingLanguage = "de"
	if err = s.PutLanguageSettings(ctx, guest, reading); err != nil {
		t.Fatal(err)
	}
	reading, err = s.LanguageSettings(ctx, guest, "ar")
	if err != nil || reading.ReadingLanguage != "de" {
		t.Fatal("guest override unavailable", reading, err)
	}
	if _, err = s.RevisionLanguage(ctx, guest, p.ID, p.Revision); err != nil {
		t.Fatal("original audience not inherited", err)
	}
	guest.Principal.SubjectID = "unknown"
	if _, err = s.LanguageSettings(ctx, guest, "en"); !errors.Is(err, chatrender.ErrDenied) {
		t.Fatal("non-member settings exposed", err)
	}
}
