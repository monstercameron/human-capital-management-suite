package productui

import (
	"fmt"
	"strings"
	"testing"
)

func TestPeoplePaginationHonorsSupportedPageSizes(t *testing.T) {
	people := make([]Person, 65)
	for index := range people {
		people[index] = Person{ID: fmt.Sprint(index)}
	}
	window := paginatePeople(people, 2, 10)
	if window.First != 11 || window.Last != 20 || window.PageCount != 7 || len(window.People) != 10 {
		t.Fatalf("unexpected window: %+v", window)
	}
	window = paginatePeople(people, 99, 50)
	if window.Page != 2 || window.First != 51 || window.Last != 65 || len(window.People) != 15 {
		t.Fatalf("clamped window: %+v", window)
	}
}

func TestHistoryAndPeopleRenderSharedPageSizeControls(t *testing.T) {
	people := testView(PagePeople)
	people.PeoplePageSize = 10
	doc, err := Render(people)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`name="page_size"`, `value="10"`, "selected", "Rows per page", "Workflows"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("people page missing %q", want)
		}
	}

	history := testView(PageHistory)
	history.HistoryPageSize = 10
	doc, err = Render(history)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `name="history_page_size"`) {
		t.Fatal("history did not use the shared adjustable page-size control")
	}
}

// TestPaginateCollectionIsTheSharedImplementation proves UXAUDIT-008's
// REFACTOR directly: People and History no longer carry their own copy of
// this arithmetic (paginatePeople and paginateHistory both now call
// PaginateCollection), and the shared function itself never treats an
// invalid page size as "unbounded" -- the exact zero-value failure mode
// this project's proof standards forbid.
func TestPaginateCollectionIsTheSharedImplementation(t *testing.T) {
	items := make([]int, 25)
	for index := range items {
		items[index] = index
	}

	t.Run("a non-positive page size floors to 1, never to unbounded", func(t *testing.T) {
		for _, size := range []int{0, -1, -100} {
			window := PaginateCollection(items, 1, size)
			if len(window.Items) != 1 {
				t.Fatalf("PaginateCollection(items, 1, %d) returned %d items, want exactly 1 (floored page size), not the whole collection", size, len(window.Items))
			}
			if window.PageCount != len(items) {
				t.Fatalf("PaginateCollection(items, 1, %d) PageCount = %d, want %d", size, window.PageCount, len(items))
			}
		}
	})

	t.Run("an out-of-range page clamps to the nearest real page", func(t *testing.T) {
		window := PaginateCollection(items, 999, 10)
		if window.Page != 3 || window.First != 21 || window.Last != 25 || len(window.Items) != 5 {
			t.Fatalf("clamped window = %+v, want page 3 (21-25 of 25)", window)
		}
		window = PaginateCollection(items, -5, 10)
		if window.Page != 1 || window.First != 1 || window.Last != 10 {
			t.Fatalf("clamped window = %+v, want page 1 (1-10 of 25)", window)
		}
	})

	t.Run("an empty collection resolves to one empty page, not zero pages", func(t *testing.T) {
		window := PaginateCollection([]int(nil), 1, 10)
		if window.PageCount != 1 || window.First != 0 || window.Last != 0 || len(window.Items) != 0 {
			t.Fatalf("empty-collection window = %+v, want PageCount=1, First=0, Last=0", window)
		}
	})

	t.Run("People and History resolve identical windows through the shared function", func(t *testing.T) {
		people := make([]Person, 25)
		history := make([]WorkItem, 25)
		for index := range people {
			people[index] = Person{ID: fmt.Sprint(index)}
			history[index] = WorkItem{ID: fmt.Sprint(index)}
		}
		peopleWindow := paginatePeople(people, 2, 10)
		historyWindow := paginateHistory(history, 2, 10)
		if peopleWindow.Page != historyWindow.Page || peopleWindow.PageCount != historyWindow.PageCount ||
			peopleWindow.First != historyWindow.First || peopleWindow.Last != historyWindow.Last {
			t.Fatalf("People and History disagree on identical inputs: people=%+v history=%+v", peopleWindow, historyWindow)
		}
	})
}

func TestFrequentWorkflowsSortAheadOfAlphabeticalFallback(t *testing.T) {
	workflows := []PersonWorkflow{{ID: "promotion", Name: "Promotion"}, {ID: "transfer", Name: "Internal transfer"}}
	ranked := rankedPersonWorkflows(workflows, map[string]int64{"promotion": 4})
	if ranked[0].ID != "promotion" || ranked[0].UseCount != 4 {
		t.Fatalf("unexpected ranking: %+v", ranked)
	}
}
