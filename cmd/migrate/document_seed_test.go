package main

import (
	"bytes"
	"context"
	"fmt"
	"math/rand/v2"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/application/documentembed"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

var seedTestUnits = []string{"executive-office", "clinical-operations", "care-coordination", "quality-safety", "engineering-platform", "product-management", "data-analytics", "security-it", "customer-success", "sales", "marketing", "people-operations", "finance", "legal-compliance", "workplace-services"}

func seedTestPeople() []seedPerson {
	people := make([]seedPerson, 0, 60)
	for i := 1; i <= 60; i++ {
		unit := seedTestUnits[i%len(seedTestUnits)]
		if i >= 50 && i <= 53 {
			unit = "people-operations"
		}
		key := fmt.Sprintf("hc-%03d-person-%d", i, i)
		if i == 50 {
			key = "hc-050-rafael-torres"
		}
		people = append(people, seedPerson{Key: key, Name: fmt.Sprintf("Person %d", i), Title: "Title", Unit: unit})
	}
	return people
}

func TestDocumentSeedPlanShape(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	plan, err := planDocumentSeed(seedTestPeople(), now)
	if err != nil {
		t.Fatal(err)
	}
	again, err := planDocumentSeed(seedTestPeople(), now)
	if err != nil || !reflect.DeepEqual(plan, again) {
		t.Fatalf("plan is not deterministic: %v", err)
	}
	if len(plan.Docs) != len(seedCatalog()) || len(plan.Docs) < 300 {
		t.Fatalf("plan has %d documents", len(plan.Docs))
	}
	viewer := plan.Viewer.Key
	if viewer != "hc-050-rafael-torres" {
		t.Fatalf("viewer = %q", viewer)
	}
	owners := map[string]bool{}
	titles := map[string]bool{}
	var owned, private, sharedWithViewer, asViewer, filed, stars, tables int
	folders := map[string]int{}
	for i, d := range plan.Docs {
		owners[d.Owner.Key] = true
		key := d.Owner.Key + "\x00" + d.Title
		if titles[key] {
			t.Fatalf("duplicate owner title %q", d.Title)
		}
		titles[key] = true
		if i > 0 && d.Created.Before(plan.Docs[i-1].Created) {
			t.Fatal("plan is not oldest first")
		}
		if age := now.Sub(d.Created); age <= 0 || age > 305*24*time.Hour {
			t.Fatalf("%q is %v old", d.Title, age)
		}
		if !d.Revised.IsZero() && (!d.Revised.After(d.Created) || d.Revised.After(now)) {
			t.Fatalf("%q revised at %v", d.Title, d.Revised)
		}
		for _, j := range d.Links {
			if j >= i {
				t.Fatalf("%q links forward to %d", d.Title, j)
			}
		}
		for _, s := range d.Shares {
			if s.Recipient == d.Owner.Key || (s.Role != documenthubstore.RoleViewer && s.Role != documenthubstore.RoleCommenter) {
				t.Fatalf("bad share %+v on %q", s, d.Title)
			}
		}
		if d.Owner.Key == viewer {
			owned++
			if len(d.Shares) == 0 {
				private++
			}
		}
		if sharesWith(d.Shares, viewer) {
			sharedWithViewer++
			for _, s := range d.Shares {
				if s.Recipient == viewer && s.Role == documenthubstore.RoleViewer {
					asViewer++
				}
			}
		}
		if d.Folder != "" {
			if d.Owner.Key != viewer && !sharesWith(d.Shares, viewer) {
				t.Fatalf("filed a document the viewer cannot read: %q", d.Title)
			}
			filed++
			folders[d.Folder]++
		}
		if d.Star {
			stars++
		}
		if d.Spec.Table {
			tables++
		}
		if ownedInDomain[d.Spec.Kind] && d.Owner.Key != viewer && !containsString(domainUnits[d.Spec.Domain], d.Owner.Unit) {
			t.Fatalf("%q is owned outside its function by %+v", d.Title, d.Owner)
		}
		if d.Spec.Dated && (d.Created.Weekday() == time.Saturday || d.Created.Weekday() == time.Sunday) {
			t.Fatalf("%q meets on a weekend", d.Title)
		}
	}
	if team := seedTeam(seedTestPeople(), domainPeople); len(team) != 8 || team[0].Unit != "people-operations" || team[7].Unit != "people-operations" {
		t.Fatalf("people team = %+v", team)
	}
	if team := seedTeam(seedTestPeople()[:3], domainFinance); len(team) != 3 {
		t.Fatalf("small team fallback = %+v", team)
	}
	if owned != seedViewerOwned || private != seedViewerPrivate || sharedWithViewer != seedSharedWithViewer {
		t.Fatalf("viewer owns %d (%d private), shared with %d", owned, private, sharedWithViewer)
	}
	if asViewer == 0 || asViewer == sharedWithViewer {
		t.Fatalf("viewer roles are not mixed: %d of %d as viewer", asViewer, sharedWithViewer)
	}
	if len(owners) < 35 || len(owners) > seedAuthors+1 {
		t.Fatalf("%d owners", len(owners))
	}
	if filed != seedViewerFiled || len(folders) != len(seedViewerFolders) || stars != seedViewerStars {
		t.Fatalf("filed %d into %v, %d stars", filed, folders, stars)
	}
	if tables == 0 || tables > 5 {
		t.Fatalf("%d documents carry tables", tables)
	}
	if err := validateDocumentCommand("seed", "postgres://docs@db:5432/docs", "postgres://core@db:5432/core", ""); err != nil {
		t.Fatalf("document seed refused: %v", err)
	}
	if _, err := planDocumentSeed(seedTestPeople()[:5], now); err == nil {
		t.Fatal("tiny population accepted")
	}
	if _, err := planDocumentSeed([]seedPerson{{Key: "hc-001-x"}}, now); err == nil {
		t.Fatal("population without the viewer accepted")
	}
}

func TestDocumentSeedBodiesRender(t *testing.T) {
	people := seedTestPeople()
	written := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	for i, spec := range seedCatalog() {
		in := seedBodyInput{Spec: spec, Title: spec.Title, Owner: people[i%len(people)], People: people, Written: written, Rand: rand.New(rand.NewPCG(uint64(i), 1)), Links: []seedLink{{Title: "Related", ID: "doc-1"}}}
		body := documentSeedBody(in)
		if !strings.HasPrefix(body, "# "+spec.Title+"\n") || !strings.Contains(body, "](doc:doc-1)") || len(body) < 300 {
			t.Fatalf("%s body = %q", spec.Title, body)
		}
		if hasTable := strings.Contains(body, "| --- |"); hasTable != spec.Table {
			t.Fatalf("%s table = %v, want %v", spec.Title, hasTable, spec.Table)
		}
		if spec.Kind == kindPolicy && !strings.Contains(body, "## Policy") {
			t.Fatalf("%s lacks its policy section", spec.Title)
		}
		in.IsRevision, in.RevisionNote = true, "Draft note text"
		if revised := documentSeedBody(in); !strings.Contains(revised, "Draft note text") {
			t.Fatalf("%s revision lacks its note", spec.Title)
		}
	}
}

func documentSeedStore(t *testing.T) *documenthubstore.Store {
	t.Helper()
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := runDocumentMigrateCommand(ctx, "up", db.SQL, &bytes.Buffer{}); err != nil {
		t.Fatalf("document migrations: %v", err)
	}
	u, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", db.Schema)
	u.RawQuery = q.Encode()
	store, err := documenthubstore.New(ctx, documenthubstore.Config{DSN: u.String()})
	if err != nil {
		t.Fatalf("open document store: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

func TestDocumentSeedCommand_Integration(t *testing.T) {
	store := documentSeedStore(t)
	ctx := context.Background()
	const tenant = "harborcare-demo"
	now := time.Now().UTC()
	people := seedTestPeople()
	var out bytes.Buffer
	if err := runDocumentSeedCommand(ctx, store, people, documentSeedOptions{Tenant: tenant, Now: now, MaxDocuments: 60}, &out); err != nil {
		t.Fatalf("document seed: %v", err)
	}
	if !strings.Contains(out.String(), "60 documents (60 created, 0 already present)") {
		t.Fatalf("receipt = %q", out.String())
	}
	plan, err := planDocumentSeed(people, now)
	if err != nil {
		t.Fatal(err)
	}
	plan.Docs = plan.Docs[:60]
	viewer := plan.Viewer.Key
	wantReadable := 0
	for _, d := range plan.Docs {
		if d.Owner.Key == viewer || sharesWith(d.Shares, viewer) {
			wantReadable++
		}
	}
	lib, err := store.GetLibrary(ctx, tenant, viewer)
	if err != nil || lib.All != wantReadable || len(lib.Folders) != len(seedViewerFolders) {
		t.Fatalf("viewer library = %+v, %v; want %d readable", lib, err, wantReadable)
	}
	rows, err := store.ListPersonalDocumentsPage(ctx, tenant, viewer, documenthubstore.ListOptions{Limit: 100})
	if err != nil || len(rows) != wantReadable {
		t.Fatalf("viewer rows = %d, %v", len(rows), err)
	}
	for _, row := range rows {
		if now.Sub(row.UpdatedAt) < 24*time.Hour {
			t.Fatalf("%q was not backdated: %v", row.Title, row.UpdatedAt)
		}
	}
	oldest := plan.Docs[0]
	oldestRows, err := store.ListPersonalDocumentsPage(ctx, tenant, oldest.Owner.Key, documenthubstore.ListOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range oldestRows {
		if row.Title == oldest.Title {
			found = true
			wantAt := oldest.Created
			if !oldest.Revised.IsZero() {
				wantAt = oldest.Revised
			}
			if !row.UpdatedAt.Equal(wantAt) {
				t.Fatalf("oldest document updated at %v, want %v", row.UpdatedAt, wantAt)
			}
		}
	}
	if !found {
		t.Fatalf("oldest document %q missing for its owner", oldest.Title)
	}

	out.Reset()
	if err := runDocumentSeedCommand(ctx, store, people, documentSeedOptions{Tenant: tenant, Now: now, MaxDocuments: 60}, &out); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if !strings.Contains(out.String(), "(0 created, 60 already present)") {
		t.Fatalf("rerun receipt = %q", out.String())
	}
	again, err := store.GetLibrary(ctx, tenant, viewer)
	if err != nil || again.All != lib.All || again.Starred != lib.Starred || len(again.Folders) != len(lib.Folders) {
		t.Fatalf("rerun changed the library: %+v vs %+v, %v", again, lib, err)
	}
	if err := runDocumentSeedCommand(ctx, nil, people, documentSeedOptions{}, &out); err == nil {
		t.Fatal("nil store accepted")
	}
}

func TestDocumentSeedLoadsPersonas_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := pgstore.TenantID("harborcare-demo")
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureDemoTenant(ctx, tx, tenantID, "harborcare-demo"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	insert := func(key, name, title, unit, status string, sequence int) {
		db.Exec(t, `INSERT INTO journey_worker (
			tenant_id,worker_id,worker_key,legal_name,preferred_name,worker_number,
			employment_id,assignment_id,job_code,grade,org_unit,position_id,location,pay_zone,
			fte,manager_relationship_ref,hire_date,effective_from,base_pay,currency,pay_basis,
			bonus_target,revision_stream,revision_sequence,known_at,created_by,job_title,lifecycle_status)
			VALUES ($1,$2,$3,$4,$4,$5,'employment','assignment','JOB','P1',$6,'POS','NYC','US',
			1,'manager',date '2026-01-01',date '2026-01-01',50000,'USD','ANNUAL',0.1,$7,$8,timestamptz '2026-01-01T00:00:00Z','test',$9,$10)`,
			tenantID, uuid.New(), key, name, key+"-number-"+fmt.Sprint(sequence), unit, "stream-"+key+fmt.Sprint(sequence), sequence, title, status)
	}
	insert("hc-050-rafael-torres", "Rafael Torres", "Director of People Operations", "people-operations", "active", 1)
	insert("hc-051-linh-tran", "Linh Tran", "Senior People Partner", "people-operations", "ACTIVE", 1)
	insert("hc-099-gone", "Former Person", "Analyst", "finance", "terminated", 1)
	people, err := loadSeedPeople(ctx, db.Conn, "harborcare-demo")
	if err != nil {
		t.Fatalf("load personas: %v", err)
	}
	want := []seedPerson{
		{Key: "hc-050-rafael-torres", Name: "Rafael Torres", Title: "Director of People Operations", Unit: "people-operations"},
		{Key: "hc-051-linh-tran", Name: "Linh Tran", Title: "Senior People Partner", Unit: "people-operations"},
	}
	if !reflect.DeepEqual(people, want) {
		t.Fatalf("personas = %+v", people)
	}
}

type unitEmbedder struct {
	remote bool
	model  string
}

func (e unitEmbedder) Model() string {
	if e.model != "" {
		return e.model
	}
	return "unit"
}
func (e unitEmbedder) Local() bool { return !e.remote }
func (e unitEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = []float32{float32(len(text)%7) + 1, 1}
	}
	return out, nil
}

func TestDocumentEmbedCommand_Integration(t *testing.T) {
	store := documentSeedStore(t)
	ctx := context.Background()
	const tenant = "harborcare-demo"
	if err := runDocumentSeedCommand(ctx, store, seedTestPeople(), documentSeedOptions{Tenant: tenant, MaxDocuments: 30}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	refs, err := store.VersionsToIndex(ctx, tenant)
	if err != nil || len(refs) < 30 {
		t.Fatalf("versions = %d, %v", len(refs), err)
	}
	cfg := documentembed.DefaultIndexerConfig()
	var queued bytes.Buffer
	if err := runDocumentEmbedCommand(ctx, store, unitEmbedder{}, cfg, tenant, false, &queued); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(queued.String(), fmt.Sprintf("enqueued %d; queue: %d queued, 0 running, 0 done", len(refs), len(refs))) {
		t.Fatalf("enqueue run = %q", queued.String())
	}
	var drained bytes.Buffer
	if err := runDocumentEmbedCommand(ctx, store, unitEmbedder{}, cfg, tenant, true, &drained); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(drained.String(), "enqueued 0;") || !strings.Contains(drained.String(), fmt.Sprintf("drain: %d done, 0 remaining, 0 failed", len(refs))) {
		t.Fatalf("drain run = %q", drained.String())
	}
	var again bytes.Buffer
	if err := runDocumentEmbedCommand(ctx, store, unitEmbedder{}, cfg, tenant, true, &again); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(again.String(), "enqueued 0;") || !strings.Contains(again.String(), "0 jobs in") {
		t.Fatalf("rerun = %q", again.String())
	}
	var remote bytes.Buffer
	if err := runDocumentEmbedCommand(ctx, store, unitEmbedder{remote: true, model: "unit-remote"}, cfg, tenant, true, &remote); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(remote.String(), " 0 skipped;") {
		t.Fatalf("remote model embedded drafts: %q", remote.String())
	}
	if err := runDocumentEmbedAction(ctx, "postgres://x@127.0.0.1:1/docs", "postgres://x@127.0.0.1:1/core", "", "", false, func(string) string { return "" }, &again); err == nil {
		t.Fatal("embed without a model accepted")
	}
	if err := runDocumentEmbedAction(ctx, "postgres://x@127.0.0.1:1/docs", "postgres://x@127.0.0.1:1/core", "", "", false, func(k string) string {
		return map[string]string{documentembed.EnvURL: "http://127.0.0.1:1", documentembed.EnvModel: "m", documentembed.EnvBatch: "none"}[k]
	}, &again); err == nil {
		t.Fatal("bad batch size accepted")
	}
	if err := validateDocumentCommand("embed", "postgres://docs@db:5432/docs", "postgres://core@db:5432/core", ""); err != nil {
		t.Fatalf("embed refused: %v", err)
	}
}

func TestDocumentSeedRichDocuments_Integration(t *testing.T) {
	store := documentSeedStore(t)
	ctx := context.Background()
	const tenant = "harborcare-demo"
	people := seedTestPeople()
	var out bytes.Buffer
	if err := runDocumentSeedCommand(ctx, store, people, documentSeedOptions{Tenant: tenant, MaxDocuments: 5, Rich: true}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), fmt.Sprintf("rich documents: %d (%d created, 0 already present)", len(seedRichDocs), len(seedRichDocs))) || !strings.Contains(out.String(), "3 filed") {
		t.Fatalf("rich receipt = %q", out.String())
	}
	viewer := "hc-050-rafael-torres"
	rows, err := store.SearchPersonalDocuments(ctx, tenant, viewer, documenthubstore.ListOptions{Query: "Hiring approval workflow", Mode: documenthubstore.SearchContains})
	if err != nil || rows.Total != 1 {
		t.Fatalf("hiring doc = %+v, %v", rows, err)
	}
	doc := rows.Rows[0]
	if strings.Contains(rows.Rows[0].Snippet, "flowchart") {
		t.Fatalf("diagram source leaked into the snippet: %q", doc.Snippet)
	}
	thread, err := store.ListThread(ctx, tenant, doc.ID, doc.VersionID, viewer)
	if err != nil || len(thread) != 3 {
		t.Fatalf("thread = %+v, %v", thread, err)
	}
	if !thread[0].Resolved || thread[0].Start < 0 || thread[0].AnchorBlock != "notes" || thread[1].ParentID != thread[0].ID || thread[2].Quote != "7 business days" || thread[2].Start < 0 {
		t.Fatalf("seeded thread = %+v", thread)
	}
	lib, err := store.GetLibrary(ctx, tenant, viewer)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range lib.Folders {
		if f.Name == "Comp cycle 2026" && f.DocumentCount != 1 {
			t.Fatalf("comp folder = %+v", f)
		}
	}
	out.Reset()
	if err := runDocumentSeedCommand(ctx, store, people, documentSeedOptions{Tenant: tenant, MaxDocuments: 5, Rich: true}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), fmt.Sprintf("(0 created, %d already present); 0 shares, 0 comments", len(seedRichDocs))) {
		t.Fatalf("rich rerun = %q", out.String())
	}
	for _, d := range seedRichDocs {
		if !strings.Contains(d.Body, "'''mermaid") && !strings.Contains(d.Body, "| --- |") {
			t.Fatalf("%q has neither a diagram nor a table", d.Title)
		}
	}
}

func TestDocumentSeedLinkPlanCoversAudience(t *testing.T) {
	plan, err := planDocumentSeed(seedTestPeople(), time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	total, covered := seedLinkCoverage(plan)
	restricted := 0
	for i, d := range plan.Docs {
		if d.Restricted {
			restricted++
			continue
		}
		for _, j := range d.Links {
			if j >= i || !audienceCovered(d, plan.Docs[j]) {
				t.Fatalf("%q links to %q outside its audience", d.Title, plan.Docs[j].Title)
			}
		}
	}
	if restricted != seedRestrictedLinks || total < 100 || float64(covered) < 0.9*float64(total) || covered == total {
		t.Fatalf("links %d of %d covered, %d restricted documents", covered, total, restricted)
	}
	if withRelated("# T\n\nBody\n\n## Related documents\n\n- [x](doc:a)\n", "") != "# T\n\nBody\n" {
		t.Fatal("withRelated did not drop the section")
	}
}

func TestDocumentSeedRelinksExistingDocuments_Integration(t *testing.T) {
	store := documentSeedStore(t)
	ctx := context.Background()
	const tenant = "harborcare-demo"
	people := seedTestPeople()
	now := time.Now().UTC()
	opts := documentSeedOptions{Tenant: tenant, Now: now, MaxDocuments: 80}
	if err := runDocumentSeedCommand(ctx, store, people, opts, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	plan, err := planDocumentSeed(people, now)
	if err != nil {
		t.Fatal(err)
	}
	plan.Docs = plan.Docs[:80]
	existing, err := existingSeedDocuments(ctx, store, tenant)
	if err != nil {
		t.Fatal(err)
	}
	// Break one published, shared document's links the way the old seed
	// did, and give its owner a pending draft on top.
	target := -1
	for i, d := range plan.Docs {
		if len(d.Links) > 0 && len(d.Shares) > 0 && !d.Restricted {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatal("no shared document with links in the plan prefix")
	}
	d := plan.Docs[target]
	id := existing[d.Owner.Key+"\x00"+d.Title]
	// A link to the owner's own private note: the owner can publish it,
	// readers cannot follow it.
	stranger, _, err := store.CreatePersonalDocument(ctx, tenant, d.Owner.Key, "Owner private note", "# Private\n")
	if err != nil {
		t.Fatal(err)
	}
	_, latest, err := store.ReadPersonalDocument(ctx, tenant, d.Owner.Key, id)
	if err != nil {
		t.Fatal(err)
	}
	live, err := store.ResolveDeployment(ctx, tenant, id, "default", "")
	if err != nil {
		t.Fatal(err)
	}
	if live.VersionID != latest.ID {
		t.Fatalf("picked a document with a pending draft: %q", d.Title)
	}
	broken := withRelated(latest.Markdown, relatedSection([]seedLink{{Title: "Owner private note", ID: stranger}}))
	v, err := store.CreatePersonalDocumentVersion(ctx, tenant, id, d.Owner.Key, latest.ID, latest.Title, broken)
	if err != nil {
		t.Fatal(err)
	}
	if err := publishSeedVersion(ctx, store, tenant, d, id, v.ID, live.VersionID); err != nil {
		t.Fatal(err)
	}
	draft, err := store.CreatePersonalDocumentVersion(ctx, tenant, id, d.Owner.Key, v.ID, latest.Title, strings.Replace(broken, "\n## Related documents\n", "\nA pending owner edit.\n\n## Related documents\n", 1))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runDocumentSeedCommand(ctx, store, people, opts, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "1 documents relinked (2 versions), 0 shares added") {
		t.Fatalf("relink receipt = %q", out.String())
	}
	reader := d.Shares[len(d.Shares)-1].Recipient
	_, published, err := store.ReadPersonalDocument(ctx, tenant, reader, id)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := store.LinkTargets(ctx, tenant, reader, published.Markdown)
	if err != nil || len(targets) != len(d.Links) {
		t.Fatalf("reader targets = %+v, %v", targets, err)
	}
	for _, target := range targets {
		if !target.Readable || target.DocumentID == stranger {
			t.Fatalf("reader still has a broken link: %+v", target)
		}
	}
	_, ownerLatest, err := store.ReadPersonalDocument(ctx, tenant, d.Owner.Key, id)
	if err != nil || !strings.Contains(ownerLatest.Markdown, "A pending owner edit.") || strings.Contains(ownerLatest.Markdown, stranger) || ownerLatest.ID == draft.ID {
		t.Fatalf("owner draft = %q, %v", ownerLatest.Markdown, err)
	}
	stored, err := store.ReadVersion(ctx, tenant, id, draft.ID, "person", d.Owner.Key)
	if err != nil {
		t.Fatal(err)
	}
	if gap := ownerLatest.CreatedAt.Sub(stored.CreatedAt); gap <= 0 || gap > 3*time.Second {
		t.Fatalf("relink not placed right after the previous version: %v", gap)
	}
	out.Reset()
	if err := runDocumentSeedCommand(ctx, store, people, opts, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "0 documents relinked (0 versions), 0 shares added") {
		t.Fatalf("second relink = %q", out.String())
	}
}
