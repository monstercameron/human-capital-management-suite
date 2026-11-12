package documents

import (
	"errors"
	"testing"
)

func TestParseWorkflowDocumentReferenceAcceptsIdentifiers(t *testing.T) {
	cases := []struct {
		in   string
		want DocumentReference
	}{
		{"doc:handbook", DocumentReference{DocumentID: "handbook"}},
		{"  document:handbook@v2#intro  ", DocumentReference{DocumentID: "handbook", VersionID: "v2", Block: "intro"}},
		{"doc:handbook#", DocumentReference{DocumentID: "handbook"}},
	}
	for _, c := range cases {
		got, err := ParseWorkflowDocumentReference(c.in)
		if err != nil || got != c.want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v", c.in, got, err, c.want)
		}
	}
}

func TestParseWorkflowDocumentReferenceRefusesMalformedValues(t *testing.T) {
	for _, in := range []string{"", "handbook", "other:handbook", "doc:", "doc:a/b", "doc:a b", "doc:a@", "doc:a#bad!", "doc:x?y"} {
		if _, err := ParseWorkflowDocumentReference(in); !errors.Is(err, ErrInvalidDocumentReference) {
			t.Errorf("Parse(%q) err = %v, want ErrInvalidDocumentReference", in, err)
		}
	}
}

func referenceCatalog() []WorkflowDocument {
	return []WorkflowDocument{
		{ID: "handbook", Title: "Handbook", Readable: true, Versions: []WorkflowDocumentVersion{
			{ID: "v1", Reviewed: true, Deployed: false, Status: "retired"},
			{ID: "v2", Reviewed: true, Deployed: true, Status: "active"},
		}},
		{ID: "secret", Title: "Secret", Readable: false, Versions: []WorkflowDocumentVersion{{ID: "s1", Reviewed: true, Deployed: true, Status: "active"}}},
		{ID: "draft", Title: "Draft", Readable: true, Versions: []WorkflowDocumentVersion{{ID: "d1", Reviewed: false, Deployed: false, Status: "draft"}}},
		{ID: "pending", Title: "Pending", Readable: true, Versions: []WorkflowDocumentVersion{{ID: "p3", Reviewed: false, Deployed: true, Status: "active"}}},
	}
}

func TestResolveWorkflowDocumentLinkLatestUsesDeployedVersion(t *testing.T) {
	link, err := ResolveWorkflowDocumentLink(WorkflowDocumentLinkSlot{ID: "s1", SectionID: "sec", DocumentRef: "doc:handbook", Mode: LinkLatest}, referenceCatalog())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !link.Readable || link.DocumentID != "handbook" || link.Title != "Handbook" || link.VersionID != "v2" {
		t.Fatalf("link = %+v, want readable handbook v2", link)
	}
	if link.Stale || link.RequestAccess {
		t.Fatalf("deployed latest must not be stale or request access: %+v", link)
	}
	if want := "/workspace/app/docs?document=handbook&version=v2"; link.Href != want {
		t.Fatalf("Href = %q, want %q", link.Href, want)
	}
	if link.SlotID != "s1" || link.SectionID != "sec" {
		t.Fatalf("slot identity lost: %+v", link)
	}
}

func TestResolveWorkflowDocumentLinkPinnedRetiredIsStaleWithBlock(t *testing.T) {
	link, err := ResolveWorkflowDocumentLink(WorkflowDocumentLinkSlot{ID: "s1", SectionID: "sec", DocumentRef: "document:handbook@v1#intro", Mode: LinkPinned}, referenceCatalog())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if link.VersionID != "v1" || !link.Stale {
		t.Fatalf("pinned retired v1 must resolve as stale: %+v", link)
	}
	if want := "/workspace/app/docs?document=handbook&version=v1#intro"; link.Href != want {
		t.Fatalf("Href = %q, want %q", link.Href, want)
	}
}

func TestResolveWorkflowDocumentLinkPinnedUnreviewedIsRefused(t *testing.T) {
	for _, ref := range []string{"doc:pending@p3", "doc:handbook@nope"} {
		link, err := ResolveWorkflowDocumentLink(WorkflowDocumentLinkSlot{ID: "s1", SectionID: "sec", DocumentRef: ref, Mode: LinkPinned}, referenceCatalog())
		if !errors.Is(err, ErrPinnedVersionUnavailable) {
			t.Errorf("Resolve(%q) err = %v, want ErrPinnedVersionUnavailable", ref, err)
		}
		if link.Href != "" {
			t.Errorf("Resolve(%q) leaked Href %q on refusal", ref, link.Href)
		}
	}
}

func TestResolveWorkflowDocumentLinkLatestWithoutDeployedVersionIsStale(t *testing.T) {
	link, err := ResolveWorkflowDocumentLink(WorkflowDocumentLinkSlot{ID: "s1", SectionID: "sec", DocumentRef: "doc:draft", Mode: LinkLatest}, referenceCatalog())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !link.Stale || link.VersionID != "" || link.Href != "" || !link.Readable {
		t.Fatalf("draft with nothing deployed must be stale and hrefless: %+v", link)
	}
}

func TestResolveWorkflowDocumentLinkHidesUnreadableAndMissingTargets(t *testing.T) {
	for _, ref := range []string{"doc:secret", "doc:absent"} {
		link, err := ResolveWorkflowDocumentLink(WorkflowDocumentLinkSlot{ID: "s1", SectionID: "sec", DocumentRef: ref, Mode: LinkLatest}, referenceCatalog())
		if err != nil {
			t.Fatalf("Resolve(%q): %v", ref, err)
		}
		if link.Readable || !link.RequestAccess || link.DocumentID != "" || link.Title != "" || link.Href != "" {
			t.Errorf("Resolve(%q) disclosed the target: %+v", ref, link)
		}
	}
}

func TestResolveWorkflowDocumentLinkRefusesInvalidInput(t *testing.T) {
	cases := []WorkflowDocumentLinkSlot{
		{ID: " ", SectionID: "sec", DocumentRef: "doc:handbook", Mode: LinkLatest},
		{ID: "s1", SectionID: "sec", DocumentRef: "doc:handbook", Mode: "bogus"},
		{ID: "s1", SectionID: "sec", DocumentRef: "handbook", Mode: LinkLatest},
	}
	for _, slot := range cases {
		if _, err := ResolveWorkflowDocumentLink(slot, referenceCatalog()); !errors.Is(err, ErrInvalidDocumentReference) {
			t.Errorf("Resolve(%+v) err = %v, want ErrInvalidDocumentReference", slot, err)
		}
	}
}

func TestWorkflowDocumentHrefBuildsOnlyValidIdentifiers(t *testing.T) {
	cases := []struct{ doc, version, block, want string }{
		{"handbook", "v2", "intro", "/workspace/app/docs?document=handbook&version=v2#intro"},
		{"handbook", "", "", "/workspace/app/docs?document=handbook"},
		{"bad/id", "", "", ""},
		{"handbook", "bad!", "", ""},
		{"handbook", "", "bad!", ""},
	}
	for _, c := range cases {
		if got := WorkflowDocumentHref(c.doc, c.version, c.block); got != c.want {
			t.Errorf("Href(%q,%q,%q) = %q, want %q", c.doc, c.version, c.block, got, c.want)
		}
	}
}

func TestWorkflowDocumentPickerKeepsOnlyFirstReadableIdentifier(t *testing.T) {
	input := []WorkflowDocument{
		{ID: "a", Title: "A", Readable: true, Versions: []WorkflowDocumentVersion{{ID: "v1"}}},
		{ID: "b", Title: "B", Readable: false},
		{ID: "a", Title: "duplicate", Readable: true},
		{ID: "bad id", Title: "bad", Readable: true},
		{ID: "c", Title: "C", Readable: true},
	}
	got := WorkflowDocumentPicker(input)
	if len(got) != 2 || got[0].ID != "a" || got[0].Title != "A" || got[1].ID != "c" {
		t.Fatalf("picker = %+v, want a (first wins) and c", got)
	}
	got[0].Versions[0].ID = "changed"
	if input[0].Versions[0].ID != "v1" {
		t.Fatal("picker must copy versions, not alias the caller's catalog")
	}
}

func TestWorkflowBacklinkIndexRecordsValidDistinctRows(t *testing.T) {
	var index WorkflowBacklinkIndex
	valid := WorkflowBacklink{TargetDocumentID: "handbook", WorkflowID: "wf1", PageID: "p1", SectionID: "s1", SlotID: "sl1", PageVersion: 1}
	index.Record(valid)
	index.Record(valid)
	index.Record(WorkflowBacklink{TargetDocumentID: "handbook", WorkflowID: "wf1", PageID: "p2", SectionID: "s1", SlotID: "sl2", PageVersion: 1})

	for _, rejected := range []WorkflowBacklink{
		{TargetDocumentID: "bad/id", WorkflowID: "wf1", PageID: "p3", PageVersion: 1},
		{TargetDocumentID: "handbook", WorkflowID: "", PageID: "p3", PageVersion: 1},
		{TargetDocumentID: "handbook", WorkflowID: "wf1", PageID: "", PageVersion: 1},
		{TargetDocumentID: "handbook", WorkflowID: "wf1", PageID: "p3", PageVersion: 0},
	} {
		index.Record(rejected)
	}

	rows := index.ForDocument("handbook")
	if len(rows) != 2 || rows[0] != valid {
		t.Fatalf("rows = %+v, want the valid row once and the second page", rows)
	}
	if none := index.ForDocument("other"); none == nil || len(none) != 0 {
		t.Fatalf("ForDocument(other) = %+v, want an empty non-nil slice", none)
	}
}

// Record is nil-safe by contract; the nil record must be dropped, not panic.
// ForDocument has a value receiver and is not nil-safe; that is recorded as a
// defect rather than asserted here.
func TestWorkflowBacklinkIndexNilReceiverIsSafe(t *testing.T) {
	var index *WorkflowBacklinkIndex
	index.Record(WorkflowBacklink{TargetDocumentID: "handbook", WorkflowID: "wf1", PageID: "p1", PageVersion: 1})
	if rows := index.ForDocument("handbook"); rows == nil || len(rows) != 0 {
		t.Fatalf("nil index ForDocument = %+v, want an empty non-nil slice", rows)
	}
	var empty WorkflowBacklinkIndex
	if rows := empty.ForDocument("handbook"); len(rows) != 0 {
		t.Fatalf("a nil-receiver Record leaked rows into an index: %+v", rows)
	}
}

func TestCeremonyRegistryGetReturnsStoredCeremony(t *testing.T) {
	reg := NewCeremonyRegistry()
	sig, err := BeginSignature(SignatureRequest{ID: "sig-get", ArtifactHash: "sha256:a", SignerID: "w1", AssuranceLevel: AssuranceBasic, TrustedNow: 1, ExpiresAt: 10})
	if err != nil {
		t.Fatal(err)
	}
	reg.Store(sig)
	got, ok := reg.Get("sig-get")
	if !ok || got.Ceremony.Digest != sig.Ceremony.Digest || got.AssuranceLevel != AssuranceBasic {
		t.Fatalf("Get = %+v, %v; want the stored ceremony", got, ok)
	}
	if _, ok := reg.Get("missing"); ok {
		t.Fatal("Get returned a ceremony that was never stored")
	}
}
