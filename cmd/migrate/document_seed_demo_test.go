package main

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatroutestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestDemoAssetsAreValidAndStable(t *testing.T) {
	a, b := demoAssets(), demoAssets()
	if len(a) != 4 {
		t.Fatalf("assets = %d", len(a))
	}
	for key, asset := range a {
		if !bytes.Equal(asset.Content, b[key].Content) {
			t.Fatalf("%s is not deterministic", key)
		}
		switch asset.ContentType {
		case "image/png":
			if _, err := png.Decode(bytes.NewReader(asset.Content)); err != nil {
				t.Fatalf("%s: %v", key, err)
			}
		case "application/pdf":
			s := string(asset.Content)
			if !strings.HasPrefix(s, "%PDF-1.4\n") || !strings.HasSuffix(s, "%%EOF\n") || !strings.Contains(s, "/Type /Catalog") {
				t.Fatalf("%s is not a PDF: %q", key, s[:40])
			}
			// The xref offset must point at the xref keyword.
			i := strings.LastIndex(s, "startxref\n")
			var off int
			if _, err := fmt.Sscan(s[i+len("startxref\n"):], &off); err != nil || !strings.HasPrefix(s[off:], "xref\n") {
				t.Fatalf("%s startxref %d wrong: %v", key, off, err)
			}
		default:
			t.Fatalf("%s type %q", key, asset.ContentType)
		}
	}
	if got := pdfEscape(`a(b)\c é`); got != `a\(b\)\\c ` {
		t.Fatalf("pdfEscape = %q", got)
	}
	facts, err := documenthubstore.InspectMedia(a["leave-pdf"].Content)
	if err != nil || facts.MediaType != "application/pdf" || facts.Pages != 1 {
		t.Fatalf("media store reads the PDF as %+v, %v", facts, err)
	}
	if facts, err := documenthubstore.InspectMedia(a["orgchart"].Content); err != nil || facts.MediaType != "image/png" || facts.Width != 640 {
		t.Fatalf("media store reads the org chart as %+v, %v", facts, err)
	}
}

func TestDemoRefHelpers(t *testing.T) {
	p := seedPerson{Key: "hc-053-a-b", Name: "Ana Bell"}
	token, _ := chatcore.OpaqueLocator{}.Encode("t1", "c-1", "p-1")
	for got, want := range map[string]string{
		demoRefDoc("T", "doc-1"):                   "[T](doc:doc-1)",
		demoRefChannel("benefits", ""):             "#benefits",
		demoRefChannel("benefits", "c-1"):          "[#benefits](channel:c-1)",
		demoRefMention(p):                          "[@Ana Bell](person:hc-053-a-b)",
		demoRefMessage("Open", "t1", "c-1", "p-1"): "[Open](/workspace/app/chat#share=" + token + ")",
		demoRefMessage("Open", "", "c-1", "p-1"):   "Open",
		demoChatDocRef("doc-1"):                    "doc:doc-1",
		demoRefImage("Chart", "docm-1"):            "![Chart](attachment:docm-1)",
		demoRefImage("Chart", ""):                  "*Chart*",
		demoRefFile("Handout", "docm-2"):           "[Handout](attachment:docm-2)",
	} {
		if got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	}
}

// TestDocumentSeedDemoDocuments_Integration seeds chat (small profile) and
// then the showcase documents twice, proving every reference kind lands and
// the rerun writes nothing new.
func TestDocumentSeedDemoDocuments_Integration(t *testing.T) {
	store := documentSeedStore(t)
	chat, chatDB := chatSeedStore(t)
	ctx := context.Background()
	const tenant = "harborcare-demo"
	now := time.Now().UTC()
	if err := runChatSeedCommand(ctx, chat, nil, chatSeedOptions{Tenant: tenant, Scale: "small", MediaRoot: t.TempDir(), Now: now.Add(-3 * time.Hour)}, &bytes.Buffer{}); err != nil {
		t.Fatalf("chat seed: %v", err)
	}
	media := t.TempDir()
	people := seedTestPeople()
	opts := documentSeedOptions{Tenant: tenant, Now: now, MaxDocuments: 3, Demo: true, Chat: chat, MediaRoot: media}
	var out bytes.Buffer
	if err := runDocumentSeedCommand(ctx, store, people, opts, &out); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if !strings.Contains(out.String(), "showcase documents: 5 (5 created, 0 updated, 0 already current") {
		t.Fatalf("receipt = %q", out.String())
	}
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error { return tx.QueryRow(ctx, q, args...).Scan(&n) }); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	chatPosts := func() int {
		t.Helper()
		var n int
		if err := chatDB.SQL.QueryRowContext(ctx, `SELECT count(*) FROM chat_post WHERE client_key LIKE 'docseed:%'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	viewer := "hc-050-rafael-torres"
	find := func(title string) documenthubstore.DocumentSummary {
		t.Helper()
		rows, err := store.SearchPersonalDocuments(ctx, tenant, viewer, documenthubstore.ListOptions{Query: title, Mode: documenthubstore.SearchContains})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows.Rows {
			if r.Title == title {
				return r
			}
		}
		t.Fatalf("%q not readable by the viewer", title)
		return documenthubstore.DocumentSummary{}
	}
	highlights := find("People Ops chat highlights: this month")
	_, hv, err := store.ReadPersonalDocument(ctx, tenant, viewer, highlights.ID)
	if err != nil {
		t.Fatal(err)
	}
	charter := find("People Operations team charter")
	peopleOps := chatSeedConversationID(tenant, "people-ops")
	for _, want := range []string{"/workspace/app/chat#share=", "(doc:" + charter.ID + ")", "[#people-ops](channel:" + peopleOps + ")", "```mermaid\npie", "| --- |", "[@"} {
		if !strings.Contains(hv.Markdown, want) {
			t.Fatalf("highlights lacks %q:\n%s", want, hv.Markdown)
		}
	}
	// Rooms the small chat profile lacks are named, not linked.
	if !strings.Contains(hv.Markdown, "#benefits") || strings.Contains(hv.Markdown, "[#benefits]") {
		t.Fatalf("absent room handled wrong:\n%s", hv.Markdown)
	}
	_, cv, err := store.ReadPersonalDocument(ctx, tenant, viewer, charter.ID)
	if err != nil {
		t.Fatal(err)
	}
	charterMedia, err := store.ListMedia(ctx, tenant, viewer, charter.ID)
	if err != nil || len(charterMedia) != 1 || !strings.Contains(cv.Markdown, "![People Operations org chart](attachment:"+charterMedia[0].ID+")") {
		t.Fatalf("charter image missing: %+v %v\n%s", charterMedia, err, cv.Markdown)
	}
	blobs, err := documenthubstore.NewMediaFiles(media)
	if err != nil {
		t.Fatal(err)
	}
	if _, raw, err := store.OpenMedia(ctx, blobs, tenant, viewer, charter.ID, charterMedia[0].ID); err != nil || !bytes.Equal(raw, demoAssets()["orgchart"].Content) {
		t.Fatalf("org chart bytes not readable by the viewer: %v", err)
	}
	attachments := count(`SELECT count(*) FROM document_media WHERE tenant_id=$1`, tenant)
	if attachments != 4 {
		t.Fatalf("attachments = %d, want 4", attachments)
	}
	posts := chatPosts()
	var linked int
	if err := chatDB.SQL.QueryRowContext(ctx, `SELECT count(*) FROM chat_post WHERE conversation_id=$1 AND body LIKE $2`, peopleOps, "%doc:"+charter.ID+"%").Scan(&linked); err != nil || linked != 1 {
		t.Fatalf("people-ops charter posts = %d, %v", linked, err)
	}
	// general, people-ops x3 in the small profile.
	if posts != 4 {
		t.Fatalf("chat posts = %d, want 4", posts)
	}
	versions := count(`SELECT count(*) FROM document_version WHERE tenant_id=$1`, tenant)

	out.Reset()
	if err := runDocumentSeedCommand(ctx, store, people, opts, &out); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if !strings.Contains(out.String(), "showcase documents: 5 (0 created, 0 updated, 5 already current, 0 left with a pending draft); 0 attachments added") {
		t.Fatalf("rerun receipt = %q", out.String())
	}
	if chatPosts() != posts || count(`SELECT count(*) FROM document_version WHERE tenant_id=$1`, tenant) != versions || count(`SELECT count(*) FROM document_media WHERE tenant_id=$1`, tenant) != attachments {
		t.Fatal("rerun wrote new rows")
	}
}

// TestDocumentSeedDemoDocuments_RoutedChat pins the bug this fix addresses:
// once a live tenant's chat rooms are registered in the core route
// directory (as "migrate chat seed" always registers them), every write
// chatstore accepts carries a route_shard, and a write with no matching
// lease is refused with chatstore.ErrNoRouteLease ("chat write requires a
// route lease"). The document seed's showcase chat posts
// (postDemoMessages in document_seed_demo.go) wrote directly to the chat
// store adapter with no lease at all, so "migrate document seed" failed
// against any tenant "migrate chat seed" had already routed -- exactly the
// failure the live harborcare-demo review DBs hit. Without opts.Routes
// wired through (documentSeedOptions.Routes, threaded from
// runDocumentSeedAction in document.go), this test reproduces that
// ErrNoRouteLease failure; with it, the showcase posts succeed the same
// way the chat seeder's own posts do (chatSeedWriteLeaseContext in
// chat_seed.go).
func TestDocumentSeedDemoDocuments_RoutedChat(t *testing.T) {
	store := documentSeedStore(t)
	chat, chatDB := chatSeedStore(t)
	ctx := context.Background()
	const tenant = "harborcare-demo"
	now := time.Now().UTC()

	routeConn := pgtest.NewEmpty(t).NewConn(t)
	routes, err := chatroutestore.New(routeConn)
	if err != nil {
		t.Fatal(err)
	}
	if err := routes.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	if err := runChatSeedCommand(ctx, chat, routes, chatSeedOptions{Tenant: tenant, Scale: "small", MediaRoot: t.TempDir(), Now: now.Add(-3 * time.Hour)}, &bytes.Buffer{}); err != nil {
		t.Fatalf("chat seed: %v", err)
	}

	people := seedTestPeople()
	opts := documentSeedOptions{Tenant: tenant, Now: now, MaxDocuments: 3, Demo: true, Chat: chat, Routes: routes, MediaRoot: t.TempDir()}
	var out bytes.Buffer
	if err := runDocumentSeedCommand(ctx, store, people, opts, &out); err != nil {
		t.Fatalf("document seed against a routed chat tenant: %v", err)
	}
	if !strings.Contains(out.String(), "showcase documents: 5 (5 created") {
		t.Fatalf("receipt = %q", out.String())
	}
	var posts int
	if err := chatDB.SQL.QueryRowContext(ctx, `SELECT count(*) FROM chat_post WHERE client_key LIKE 'docseed:%'`).Scan(&posts); err != nil {
		t.Fatal(err)
	}
	if posts == 0 {
		t.Fatal("no showcase chat posts were written")
	}

	// Idempotent rerun against the same routed tenant.
	out.Reset()
	if err := runDocumentSeedCommand(ctx, store, people, opts, &out); err != nil {
		t.Fatalf("rerun against a routed chat tenant: %v", err)
	}
	if !strings.Contains(out.String(), "showcase documents: 5 (0 created, 0 updated, 5 already current") {
		t.Fatalf("rerun receipt = %q", out.String())
	}

	// "chat seed -reset=true" purges and recreates the same rooms, which
	// purges the posts the highlights document permalinks. Relinking must
	// still work: the document seed writes fresh posts under the same
	// route lease and rewrites the highlights document to point at them.
	viewer := "hc-050-rafael-torres"
	find := func(title string) documenthubstore.DocumentSummary {
		t.Helper()
		rows, err := store.SearchPersonalDocuments(ctx, tenant, viewer, documenthubstore.ListOptions{Query: title, Mode: documenthubstore.SearchContains})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows.Rows {
			if r.Title == title {
				return r
			}
		}
		t.Fatalf("%q not readable by the viewer", title)
		return documenthubstore.DocumentSummary{}
	}
	highlights := find("People Ops chat highlights: this month")
	_, before, err := store.ReadPersonalDocument(ctx, tenant, viewer, highlights.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := runChatSeedCommand(ctx, chat, routes, chatSeedOptions{Tenant: tenant, Scale: "small", MediaRoot: t.TempDir(), Now: now.Add(-3 * time.Hour), Reset: true}, &bytes.Buffer{}); err != nil {
		t.Fatalf("chat seed reset: %v", err)
	}
	var postsAfterReset int
	if err := chatDB.SQL.QueryRowContext(ctx, `SELECT count(*) FROM chat_post WHERE client_key LIKE 'docseed:%'`).Scan(&postsAfterReset); err != nil {
		t.Fatal(err)
	}
	if postsAfterReset != 0 {
		t.Fatalf("chat seed reset left %d showcase posts behind, want 0", postsAfterReset)
	}

	out.Reset()
	if err := runDocumentSeedCommand(ctx, store, people, opts, &out); err != nil {
		t.Fatalf("document seed after chat reset: %v", err)
	}
	if !strings.Contains(out.String(), "documents relinked") {
		t.Fatalf("relink receipt missing: %q", out.String())
	}
	_, after, err := store.ReadPersonalDocument(ctx, tenant, viewer, highlights.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sameMarkdown(before.Markdown, after.Markdown) {
		t.Fatal("highlights document was not relinked to the posts recreated by the chat reset")
	}
	if err := chatDB.SQL.QueryRowContext(ctx, `SELECT count(*) FROM chat_post WHERE client_key LIKE 'docseed:%'`).Scan(&posts); err != nil {
		t.Fatal(err)
	}
	if posts == 0 {
		t.Fatal("no showcase chat posts were rewritten after the chat reset")
	}
}
