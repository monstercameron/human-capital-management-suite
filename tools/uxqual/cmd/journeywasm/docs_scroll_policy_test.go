package main

import "testing"

const (
	docsTestList = "/workspace/app/docs?collection=shared&docs_page=2&docs_size=25&docs_sort=title"
	docsTestDoc  = "/workspace/app/docs?document=doc-7"
)

// H7: opening a document starts it at the top, and every way back to the
// list -- "Back to documents" (a new entry), Back and Forward -- returns
// to where the reader left that list.
func TestDocsListScrollSurvivesOpeningADocument(t *testing.T) {
	ledger, _ := uxlive028Ledger()
	uxlive028Settle(ledger, 0, docsTestList)
	ledger.Observe(700)

	ledger.BeginNavigation(false)
	if action, top := uxlive028Settle(ledger, 1, docsTestDoc); action != productScrollTop || top != 0 {
		t.Fatalf("open document = %v %v, want top 0", action, top)
	}
	ledger.Observe(320)

	// "Back to documents" pushes the list address as a new entry.
	ledger.BeginNavigation(false)
	if action, top := uxlive028Settle(ledger, 2, docsTestList); action != productScrollRestore || top != 700 {
		t.Fatalf("Back to documents = %v %v, want restore 700", action, top)
	}

	// Browser Back to the document, then Back again to the first list entry.
	ledger.BeginNavigation(true)
	if action, top := uxlive028Settle(ledger, 1, docsTestDoc); action != productScrollRestore || top != 320 {
		t.Fatalf("Back to document = %v %v, want restore 320", action, top)
	}
	ledger.BeginNavigation(true)
	if action, top := uxlive028Settle(ledger, 0, docsTestList); action != productScrollRestore || top != 700 {
		t.Fatalf("Back to list = %v %v, want restore 700", action, top)
	}
}

// A list reached from elsewhere (not from a document) still starts at the
// top: only a return from a document restores by address.
func TestDocsListAddressRestoreOnlyFromADocument(t *testing.T) {
	ledger, _ := uxlive028Ledger()
	uxlive028Settle(ledger, 0, docsTestList)
	ledger.Observe(700)
	ledger.BeginNavigation(false)
	uxlive028Settle(ledger, 1, "/workspace/app/people?")
	ledger.BeginNavigation(false)
	if action, _ := uxlive028Settle(ledger, 2, docsTestList); action != productScrollTop {
		t.Fatalf("list from People = %v, want top", action)
	}
}

// M17: a new page of the list starts at the top and focuses its heading;
// Back to the earlier page restores that entry.
func TestDocsPagerStartsAtTheTop(t *testing.T) {
	ledger, _ := uxlive028Ledger()
	page1 := "/workspace/app/docs?collection=shared"
	page2 := "/workspace/app/docs?collection=shared&docs_page=2"
	uxlive028Settle(ledger, 0, page1)
	ledger.Observe(818)
	ledger.BeginNavigation(false)
	if action, top := uxlive028Settle(ledger, 1, page2); action != productScrollTop || top != 0 {
		t.Fatalf("pager = %v %v, want top", action, top)
	}
	ledger.BeginNavigation(true)
	if action, top := uxlive028Settle(ledger, 0, page1); action != productScrollRestore || top != 818 {
		t.Fatalf("Back to page 1 = %v %v, want restore 818", action, top)
	}
	if selector, caret := productRouteFocusTarget(page1, page2); selector != docsListHeadingSelector || caret {
		t.Fatalf("pager focus = %q %v, want the list heading", selector, caret)
	}
	// Sorting is not paging: it keeps the reader's place.
	if selector, _ := productRouteFocusTarget(page1, page1+"&docs_sort=title"); selector != "" {
		t.Fatalf("sort focus = %q, want none", selector)
	}
	if docsPagerOnlyRouteChange(page1, "/workspace/app/docs?collection=shared&docs_page=2&docs_q=x") {
		t.Fatal("a search that also resets the page was taken for paging")
	}
}

func TestDocsRouteClassification(t *testing.T) {
	if !docsListRoute("/workspace/app/docs?") || docsListRoute(docsTestDoc) || docsListRoute("/workspace/app/people") {
		t.Fatal("docsListRoute misclassified")
	}
	if !docsDocumentRoute(docsTestDoc) || docsDocumentRoute(docsTestList) {
		t.Fatal("docsDocumentRoute misclassified")
	}
	if docsListAddressKey("/workspace/app/docs?nav=collapsed&docs_sort=title&collection=shared") != docsListAddressKey("/workspace/app/docs?collection=shared&docs_sort=title") {
		t.Fatal("list address key depends on query order or the navigation rail")
	}
	if !productRouteDestinationChanged(docsTestList, docsTestDoc) {
		t.Fatal("opening a document was not a destination change")
	}
	var memory docsListMemory
	for i := 0; i < docsListAddressCapacity+5; i++ {
		memory.save("/workspace/app/docs?docs_page="+string(rune('a'+i%26))+string(rune('a'+i/26)), float64(i+1))
	}
	if len(memory.positions) > docsListAddressCapacity || len(memory.order) != len(memory.positions) {
		t.Fatalf("list memory grew to %d", len(memory.positions))
	}
	memory.save(docsTestDoc, 50)
	if _, ok := memory.lookup(docsTestDoc); ok {
		t.Fatal("a document address was remembered as a list")
	}
}
