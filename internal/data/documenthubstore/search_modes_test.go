package documenthubstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func titlesOf(rows []DocumentSummary) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Title)
	}
	return out
}

func hasTitle(rows []DocumentSummary, title string) bool {
	for _, r := range rows {
		if r.Title == title {
			return true
		}
	}
	return false
}

// payrollEmbed is a two-axis stand-in model: payroll text points one way,
// everything else the other.
func payrollEmbed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if strings.Contains(strings.ToLower(t), "payroll") {
			out[i] = []float32{1, 0.05}
		} else {
			out[i] = []float32{0.05, 1}
		}
	}
	return out, nil
}

func TestDocumentSearchModes_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner, reader, stranger = "tenant-search", "owner-s", "reader-s", "stranger-s"
	create := func(actor, title, body string, shareWith ...string) (string, string) {
		t.Helper()
		id, v, err := s.CreatePersonalDocument(ctx, tenant, actor, title, body)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range shareWith {
			if err := s.SharePersonalDocumentRole(ctx, tenant, id, actor, r, RoleViewer); err != nil {
				t.Fatal(err)
			}
		}
		return id, v.ID
	}
	nurses, nursesV := create(owner, "Onboarding checklist for nurses", "# Onboarding checklist for nurses\n\n## Day one\n\n- Complete the **shift handover** training with your [preceptor](https://example.com/p)\n", reader)
	secret, secretV := create(owner, "Secret salary bands", "# Secret salary bands\n\nOnboarding bonus and payroll adjustments for secret hires.\n")
	foreign, foreignV := create(stranger, "Onboarding plan for executives", "# Onboarding plan for executives\n\nPayroll setup and handover notes.\n")
	payroll, payrollV := create(owner, "Payroll close runbook", "# Payroll close runbook\n\n## Steps\n\nClose the payroll on Friday and reconcile the bank file.\n", reader)
	for i := 0; i < 25; i++ {
		create(owner, fmt.Sprintf("Policy note %02d", i), fmt.Sprintf("# Policy note %02d\n\nAnnual leave accrues monthly for group %d.\n", i, i), reader)
	}
	search := func(actor, mode, query string, opts ListOptions) SearchResult {
		t.Helper()
		opts.Mode, opts.Query = mode, query
		res, err := s.SearchPersonalDocuments(ctx, tenant, actor, opts)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	noLeak := func(res SearchResult, label string) {
		t.Helper()
		for _, r := range res.Rows {
			if r.ID == secret || r.ID == foreign {
				t.Fatalf("%s leaked %q", label, r.Title)
			}
		}
	}

	// Contains: a substring of the plain text, snippet stripped of Markdown.
	res := search(reader, SearchContains, "handov", ListOptions{})
	if res.Mode != SearchContains || res.Total != 1 || len(res.Rows) != 1 || res.Rows[0].ID != nurses || res.Rows[0].Match != MatchText {
		t.Fatalf("contains handov = %+v", res)
	}
	if sn := res.Rows[0].Snippet; !strings.Contains(sn, "shift handover training with your preceptor") || strings.ContainsAny(sn, "*[]#") {
		t.Fatalf("snippet = %q", sn)
	}
	if res := search(owner, SearchContains, "SALARY", ListOptions{}); res.Total != 1 || res.Rows[0].Match != MatchTitle {
		t.Fatalf("owner contains own draft title = %+v", res)
	}
	if res := search(reader, SearchContains, "100%_", ListOptions{}); res.Total != 0 {
		t.Fatalf("wildcards matched = %+v", res)
	}

	// Fuzzy: a typo finds the word, only in readable documents.
	res = search(reader, SearchFuzzy, "onbaording", ListOptions{})
	if res.Total != 1 || res.Rows[0].ID != nurses || res.Rows[0].Match != MatchFuzzy || res.Rows[0].Snippet == "" {
		t.Fatalf("reader fuzzy = %+v", res)
	}
	res = search(owner, SearchFuzzy, "onbaording", ListOptions{})
	if res.Total != 2 || res.Rows[0].ID != nurses || res.Rows[1].Title != "Secret salary bands" || hasTitle(res.Rows, "Onboarding plan for executives") {
		t.Fatalf("owner fuzzy = %v", titlesOf(res.Rows))
	}

	// Smart: exact words, substrings and typos fused; the strongest reason wins.
	res = search(reader, SearchSmart, "onboarding", ListOptions{})
	if res.Total != 1 || res.Rows[0].Match != MatchTitle {
		t.Fatalf("smart title = %+v", res)
	}
	if res := search(reader, SearchSmart, "onbaording", ListOptions{}); res.Total != 1 || res.Rows[0].ID != nurses {
		t.Fatalf("smart typo = %+v", res)
	}
	if res := search(reader, "", "reconcile bank", ListOptions{}); res.Mode != SearchSmart || res.Total != 1 || res.Rows[0].Match != MatchText {
		t.Fatalf("default smart = %+v", res)
	}

	// Meaning without a vector runs smart and says so.
	if res := search(reader, SearchMeaning, "payroll", ListOptions{}); res.Mode != SearchSmart {
		t.Fatalf("meaning without vectors ran %q", res.Mode)
	}

	// Vectors: deployed versions and the owner's own draft (in-deployment
	// model only) are indexed; an external model never sees a draft.
	local := EmbeddingModel{ID: "test-local", Version: "1"}
	external := EmbeddingModel{ID: "test-remote", Version: "1", External: true}
	if _, err := s.IndexVersionVectors(ctx, tenant, secret, secretV, external, payrollEmbed); !errors.Is(err, ErrDraftEgress) {
		t.Fatalf("external draft embedding = %v", err)
	}
	refs, err := s.VersionsToIndex(ctx, tenant)
	if err != nil || len(refs) != 29 {
		t.Fatalf("versions to index = %d, %v", len(refs), err)
	}
	for _, ref := range refs {
		if n, err := s.IndexVersionVectors(ctx, tenant, ref.DocumentID, ref.VersionID, local, payrollEmbed); err != nil || n == 0 {
			t.Fatalf("index %s = %d, %v", ref.VersionID, n, err)
		}
	}
	if n, err := s.IndexVersionVectors(ctx, tenant, payroll, payrollV, local, payrollEmbed); err != nil || n != 0 {
		t.Fatalf("reindex = %d, %v", n, err)
	}
	for _, v := range []string{nursesV, foreignV, secretV} {
		var n int
		if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM document_section_vector WHERE tenant_id=$1 AND version_id=$2`, tenant, v).Scan(&n)
		}); err != nil || n == 0 {
			t.Fatalf("version %s has %d vectors, %v", v, n, err)
		}
	}
	vectorOpts := ListOptions{QueryVector: []float32{1, 0}, VectorModel: local.ID}
	res = search(reader, SearchMeaning, "wages", vectorOpts)
	if res.Mode != SearchMeaning || res.Total != 1 || res.Rows[0].ID != payroll || res.Rows[0].Match != MatchMeaning || !strings.Contains(res.Rows[0].Snippet, "Close the payroll") {
		t.Fatalf("reader meaning = %+v", res)
	}
	res = search(owner, SearchMeaning, "wages", vectorOpts)
	if res.Total != 2 || !hasTitle(res.Rows, "Secret salary bands") {
		t.Fatalf("owner meaning = %v", titlesOf(res.Rows))
	}
	res = search(reader, SearchSmart, "wages", vectorOpts)
	if res.Mode != SearchSmart || res.Total != 1 || res.Rows[0].Match != MatchMeaning {
		t.Fatalf("smart with meaning = %+v", res)
	}
	res = search(reader, SearchSmart, "payroll", vectorOpts)
	if res.Total != 1 || res.Rows[0].Match != MatchTitle {
		t.Fatalf("smart strongest reason = %+v", res)
	}
	for _, mode := range []string{SearchSmart, SearchContains, SearchFuzzy, SearchMeaning} {
		for _, q := range []string{"secret", "executives", "onboarding", "payroll", "handover", "salary"} {
			noLeak(search(reader, mode, q, vectorOpts), mode+" "+q)
		}
	}
	if res := search(owner, SearchSmart, "executives", vectorOpts); hasTitle(res.Rows, "Onboarding plan for executives") {
		t.Fatalf("owner found a stranger's private draft: %v", titlesOf(res.Rows))
	}

	// Paging: disjoint pages whose union is the total, in every order.
	for _, order := range []string{SortRelevance, SortUpdated, SortUpdatedAsc, SortTitle, SortTitleDesc} {
		seen := map[string]bool{}
		var total int
		for offset := 0; offset < 40; offset += 10 {
			page := search(reader, SearchContains, "leave", ListOptions{Limit: 10, Offset: offset, Sort: order})
			total = page.Total
			for _, r := range page.Rows {
				if seen[r.ID] {
					t.Fatalf("%s: %q on two pages", order, r.Title)
				}
				seen[r.ID] = true
			}
		}
		if total != 25 || len(seen) != 25 {
			t.Fatalf("%s: total %d, walked %d", order, total, len(seen))
		}
	}
	asc := search(reader, SearchContains, "leave", ListOptions{Limit: 3, Sort: SortTitle})
	desc := search(reader, SearchContains, "leave", ListOptions{Limit: 3, Sort: SortTitleDesc})
	if asc.Rows[0].Title != "Policy note 00" || desc.Rows[0].Title != "Policy note 24" {
		t.Fatalf("title orders = %v / %v", titlesOf(asc.Rows), titlesOf(desc.Rows))
	}
	if res := search(reader, SearchContains, "leave", ListOptions{Limit: 10, Offset: 500}); res.Total != 25 || len(res.Rows) != 0 {
		t.Fatalf("past the end = %+v", res)
	}
	if res := search(reader, SearchSmart, "leave", ListOptions{Starred: true}); res.Total != 0 {
		t.Fatalf("starred filter ignored = %+v", res)
	}
}

func TestDocumentPlainListingOrdersAndOffset_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner = "tenant-orders", "owner-o"
	for i := 0; i < 12; i++ {
		if _, _, err := s.CreatePersonalDocument(ctx, tenant, owner, fmt.Sprintf("Doc %02d", i), "# Body\n"); err != nil {
			t.Fatal(err)
		}
	}
	for _, order := range []string{SortUpdated, SortUpdatedAsc, SortTitle, SortTitleDesc, SortRelevance} {
		seen := map[string]bool{}
		for offset := 0; offset < 12; offset += 5 {
			rows, err := s.ListPersonalDocumentsPage(ctx, tenant, owner, ListOptions{Limit: 5, Offset: offset, Sort: order})
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range rows {
				if seen[r.ID] {
					t.Fatalf("%s duplicate %q", order, r.Title)
				}
				seen[r.ID] = true
			}
		}
		if len(seen) != 12 {
			t.Fatalf("%s walked %d", order, len(seen))
		}
	}
	first, _ := s.ListPersonalDocumentsPage(ctx, tenant, owner, ListOptions{Limit: 1, Sort: SortTitleDesc})
	oldest, _ := s.ListPersonalDocumentsPage(ctx, tenant, owner, ListOptions{Limit: 1, Sort: SortUpdatedAsc})
	if first[0].Title != "Doc 11" || oldest[0].Title != "Doc 00" {
		t.Fatalf("first = %q, oldest = %q", first[0].Title, oldest[0].Title)
	}
	key := first[0].TitleKey
	next, _ := s.ListPersonalDocumentsPage(ctx, tenant, owner, ListOptions{Limit: 1, Sort: SortTitleDesc, AfterTitle: &key, BeforeID: first[0].ID})
	if next[0].Title != "Doc 10" {
		t.Fatalf("title_desc keyset = %q", next[0].Title)
	}
	after, _ := s.ListPersonalDocumentsPage(ctx, tenant, owner, ListOptions{Limit: 1, Sort: SortUpdatedAsc, BeforeTime: oldest[0].UpdatedAt, BeforeID: oldest[0].ID})
	if after[0].Title != "Doc 01" {
		t.Fatalf("updated_asc keyset = %q", after[0].Title)
	}
	if res, err := s.SearchPersonalDocuments(ctx, tenant, owner, ListOptions{}); err != nil || res.Total != 0 || res.Mode != SearchSmart {
		t.Fatalf("empty query search = %+v, %v", res, err)
	}
	if _, err := s.SearchPersonalDocuments(ctx, tenant, "", ListOptions{Query: "x"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("anonymous search = %v", err)
	}
}

func TestStripMarkdownAndSnippets(t *testing.T) {
	md := "# Title\n\n> Quote with **bold** and _em_ and `code`\n\n1. First [link label](doc:doc-1) item\n- ![alt text](https://x/y.png) image\n\n| A | B |\n| --- | --- |\n| one | two |\n\n---\n"
	got := stripMarkdown(md)
	want := "Title Quote with bold and em and code First link label item alt text image A B one two"
	if got != want {
		t.Fatalf("stripMarkdown = %q", got)
	}
	if got := stripMarkdown("Before\n```mermaid\nflowchart TD\n  A-->B\n```\n```go\nx := 1\n```\nAfter"); got != "Before x := 1 After" {
		t.Fatalf("fences = %q", got)
	}
	if got := stripMarkdown("keep snake_case_names intact"); got != "keep snake_case_names intact" {
		t.Fatalf("underscores = %q", got)
	}
	if plainBody("Title", "# Title\n\nBody text") != "Body text" {
		t.Fatal("plainBody kept the title")
	}
	long := strings.Repeat("alpha beta gamma delta ", 40) + "NEEDLE " + strings.Repeat("omega psi chi ", 40)
	pos := strings.Index(long, "NEEDLE")
	sn := snippetAround(long, pos)
	if !strings.Contains(sn, "NEEDLE") || !strings.HasPrefix(sn, "…") || !strings.HasSuffix(sn, "…") || utf8.RuneCountInString(sn) > snippetMax {
		t.Fatalf("snippet = %q", sn)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(sn, "…"), "…")
	if strings.HasPrefix(inner, " ") || !strings.Contains(long, inner) {
		t.Fatalf("snippet not cut at words: %q", sn)
	}
	if sn := snippetAround("short text", -1); sn != "short text" {
		t.Fatalf("short = %q", sn)
	}
	if snippetAround("", 3) != "" {
		t.Fatal("empty text snippet")
	}
	if s := trigramSimilarity(trigrams("onbaording"), trigrams("onboarding")); s < 0.3 {
		t.Fatalf("typo similarity = %v", s)
	}
	if s := trigramSimilarity(trigrams("payroll"), trigrams("onboarding")); s > 0.1 {
		t.Fatalf("unrelated similarity = %v", s)
	}
	for _, tc := range []struct {
		a, b string
		want float64
	}{
		{"clsoe", "close", 0.8}, {"onbaording", "onboarding", 0.9}, {"payrol", "payroll", 1 - 1.0/7},
		{"recording", "onbaording", 0}, {"cat", "act", 0}, {"leave", "least", 0}, {"handbok", "handbook", 1 - 1.0/8},
	} {
		if got := editSimilarity(tc.a, tc.b); got < tc.want-1e-9 || got > tc.want+1e-9 {
			t.Fatalf("editSimilarity(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
	if wordPosition("the handover handle", "hand") != 4 || wordPosition("xhand", "hand") != -1 || wordPosition("abc", "") != -1 {
		t.Fatal("wordPosition is wrong")
	}
	if indexLower("İx", strings.ToLower("İx"), "x") != 0 {
		t.Fatal("length-changing lower case must fall back to the start")
	}
}

func TestDocumentOwnerSortPaging_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, viewer = "tenant-owner-sort", "viewer-o"
	owners := []string{"owner-c", "owner-a", "owner-b", "owner-d"}
	names := map[string]string{"owner-a": "Zoe Alder", "owner-b": "amir Brooks", "owner-c": "Maya Chen"}
	for i := 0; i < 12; i++ {
		owner := owners[i%len(owners)]
		id, _, err := s.CreatePersonalDocument(ctx, tenant, owner, fmt.Sprintf("Shared %02d", i), "# Shared\n\nOnboarding notes.\n")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SharePersonalDocumentRole(ctx, tenant, id, owner, viewer, RoleViewer); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.SelectionOwners(ctx, tenant, viewer, ListOptions{Query: "ignored"})
	if err != nil || strings.Join(got, ",") != "owner-a,owner-b,owner-c,owner-d" {
		t.Fatalf("selection owners = %v, %v", got, err)
	}
	// Names sort case-insensitively; an unnamed owner sorts by ID.
	wantAsc := []string{"owner-b", "owner-c", "owner-d", "owner-a"}
	for _, tc := range []struct {
		order string
		want  []string
	}{{SortOwner, wantAsc}, {SortOwnerDesc, []string{"owner-a", "owner-d", "owner-c", "owner-b"}}} {
		var walked []DocumentSummary
		for offset := 0; offset < 12; offset += 5 {
			rows, err := s.ListPersonalDocumentsPage(ctx, tenant, viewer, ListOptions{Limit: 5, Offset: offset, Sort: tc.order, OwnerNames: names})
			if err != nil {
				t.Fatal(err)
			}
			walked = append(walked, rows...)
		}
		seen := map[string]bool{}
		for i, r := range walked {
			if seen[r.ID] {
				t.Fatalf("%s: %q twice", tc.order, r.Title)
			}
			seen[r.ID] = true
			if want := tc.want[i/3]; r.OwnerID != want {
				t.Fatalf("%s row %d owner %s, want %s", tc.order, i, r.OwnerID, want)
			}
			if i%3 > 0 && r.UpdatedAt.After(walked[i-1].UpdatedAt) {
				t.Fatalf("%s: ties not newest first", tc.order)
			}
		}
		if len(walked) != 12 {
			t.Fatalf("%s walked %d", tc.order, len(walked))
		}
		res, err := s.SearchPersonalDocuments(ctx, tenant, viewer, ListOptions{Query: "onboarding", Sort: tc.order, OwnerNames: names, Limit: 12})
		if err != nil || res.Total != 12 {
			t.Fatalf("%s search = %+v, %v", tc.order, res, err)
		}
		for i, r := range res.Rows {
			if r.OwnerID != tc.want[i/3] {
				t.Fatalf("%s search row %d owner %s", tc.order, i, r.OwnerID)
			}
		}
	}
}
