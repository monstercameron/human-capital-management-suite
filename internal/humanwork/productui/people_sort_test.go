package productui

import (
	"fmt"
	"math/rand"
	"net/url"
	"reflect"
	"slices"
	"testing"
)

func TestSortedPeopleEveryColumnAndDirection(t *testing.T) {
	people := []Person{
		{ID: "4", Name: "zoe", Role: "Engineer", Team: "Platform", Manager: "Marta", Location: "Denver"},
		{ID: "2", Name: "Avery", Role: "Designer", Team: "Product", Manager: "Nora", Location: "Boston"},
		{ID: "3", Name: "bianca", Role: "Engineer", Team: "Platform", Manager: "Marta", Location: "Atlanta"},
		{ID: "1", Name: "Álvaro", Role: "", Team: "", Manager: "", Location: ""},
	}
	for _, tc := range []struct {
		field, direction string
		want             []string
	}{
		{peopleSortName, peopleSortAscending, []string{"Avery", "bianca", "zoe", "Álvaro"}},
		{peopleSortName, peopleSortDescending, []string{"Álvaro", "zoe", "bianca", "Avery"}},
		{peopleSortRole, peopleSortAscending, []string{"Avery", "bianca", "zoe", "Álvaro"}},
		{peopleSortRole, peopleSortDescending, []string{"bianca", "zoe", "Avery", "Álvaro"}},
		{peopleSortTeam, peopleSortAscending, []string{"bianca", "zoe", "Avery", "Álvaro"}},
		{peopleSortManager, peopleSortDescending, []string{"Avery", "bianca", "zoe", "Álvaro"}},
		{peopleSortLocation, peopleSortAscending, []string{"bianca", "Avery", "zoe", "Álvaro"}},
	} {
		t.Run(tc.field+"_"+tc.direction, func(t *testing.T) {
			original := append([]Person(nil), people...)
			got := sortedPeople(people, tc.field, tc.direction)
			names := make([]string, len(got))
			for index := range got {
				names[index] = got[index].Name
			}
			if !slices.Equal(names, tc.want) {
				t.Fatalf("names=%v want=%v", names, tc.want)
			}
			// Person is not comparable (its equality function is cut from the
			// browser module); a field-wise comparison asserts the same thing.
			if !slices.EqualFunc(people, original, func(a, b Person) bool { return reflect.DeepEqual(a, b) }) {
				t.Fatal("sorting mutated the service projection")
			}
		})
	}
}

func TestSortedPeopleIsDeterministicForEqualAndMissingValues(t *testing.T) {
	people := []Person{
		{ID: "b", Name: "Same", Role: "Engineer"},
		{ID: "a", Name: "same", Role: "engineer"},
		{ID: "d", Name: "Zulu"},
		{ID: "c", Name: "Alpha"},
	}
	for _, direction := range []string{peopleSortAscending, peopleSortDescending} {
		got := sortedPeople(people, peopleSortRole, direction)
		ids := make([]string, len(got))
		for index := range got {
			ids[index] = got[index].ID
		}
		if want := []string{"a", "b", "c", "d"}; !slices.Equal(ids, want) {
			t.Fatalf("%s ids=%v want=%v", direction, ids, want)
		}
	}
}

func TestSortedPeopleRandomizedOrderingInvariant(t *testing.T) {
	random := rand.New(rand.NewSource(42))
	fields := []string{peopleSortName, peopleSortRole, peopleSortTeam, peopleSortManager, peopleSortLocation}
	for iteration := 0; iteration < 200; iteration++ {
		people := make([]Person, 1+random.Intn(500))
		for index := range people {
			people[index] = Person{
				ID: fmt.Sprintf("id-%06d", index), Name: fmt.Sprintf("Name %03d", random.Intn(90)),
				Role: fmt.Sprintf("Role %02d", random.Intn(20)), Team: fmt.Sprintf("Team %02d", random.Intn(12)),
				Manager: fmt.Sprintf("Manager %02d", random.Intn(30)), Location: fmt.Sprintf("Location %02d", random.Intn(8)),
			}
		}
		field := fields[random.Intn(len(fields))]
		descending := random.Intn(2) == 1
		direction := peopleSortAscending
		if descending {
			direction = peopleSortDescending
		}
		got := sortedPeople(people, field, direction)
		if len(got) != len(people) {
			t.Fatalf("iteration %d lost rows", iteration)
		}
		seen := make(map[string]bool, len(got))
		for index, person := range got {
			if seen[person.ID] {
				t.Fatalf("iteration %d duplicated %s", iteration, person.ID)
			}
			seen[person.ID] = true
			if index == 0 {
				continue
			}
			left, right := got[index-1], person
			comparison := compareSortText(normalizedSortText(peopleSortValue(left, field)), normalizedSortText(peopleSortValue(right, field)), descending)
			if comparison > 0 {
				t.Fatalf("iteration %d not monotonic at %d: %q before %q", iteration, index, peopleSortValue(left, field), peopleSortValue(right, field))
			}
			if comparison == 0 && compareSortText(normalizedSortText(left.Name), normalizedSortText(right.Name), false) > 0 {
				t.Fatalf("iteration %d unstable secondary order at %d", iteration, index)
			}
		}
	}
}

func TestSortThenPaginateProducesStableNonOverlappingWindows(t *testing.T) {
	people := make([]Person, 125)
	for index := range people {
		people[index] = Person{ID: fmt.Sprintf("id-%03d", index), Name: fmt.Sprintf("Person %03d", 124-index), Team: fmt.Sprintf("Team %d", index%4)}
	}
	ordered := sortedPeople(people, peopleSortName, peopleSortAscending)
	first := paginatePeople(ordered, 1, 50)
	second := paginatePeople(ordered, 2, 50)
	third := paginatePeople(ordered, 3, 50)
	if len(first.People) != 50 || len(second.People) != 50 || len(third.People) != 25 {
		t.Fatalf("unexpected page sizes: %d %d %d", len(first.People), len(second.People), len(third.People))
	}
	seen := map[string]bool{}
	for _, window := range []peoplePageWindow{first, second, third} {
		for _, person := range window.People {
			if seen[person.ID] {
				t.Fatalf("row %s appears on multiple pages", person.ID)
			}
			seen[person.ID] = true
		}
	}
}

func TestPeopleDirectoryResolvesSortWithoutReloadingThePage(t *testing.T) {
	view := testView(PagePeople)
	var committed PeopleDirectoryChange
	view.UpdatePeopleDirectory = func(change PeopleDirectoryChange) {
		committed = change
	}
	window := paginatePeople(sortedPeople(filteredPeople(view), view.PeopleSort, view.PeopleDirection), view.PeoplePage, view.PeoplePageSize)
	props := peopleDirectoryProps(view, window)
	if props.ResolveSort == nil || props.CommitSort == nil {
		t.Fatal("people directory did not expose its component-local sort contract")
	}

	next := props.ResolveSort(peopleSortRole, true)
	var roleColumn PeopleSortColumnProps
	for _, column := range next.Columns {
		if column.ID == peopleSortRole {
			roleColumn = column
			break
		}
	}
	if !roleColumn.Active || !roleColumn.Descending {
		t.Fatalf("role column active=%t descending=%t", roleColumn.Active, roleColumn.Descending)
	}
	wantRows := sortedPeople(filteredPeople(view), peopleSortRole, peopleSortDescending)
	if len(next.Rows) != len(wantRows) {
		t.Fatalf("rows=%d want=%d", len(next.Rows), len(wantRows))
	}
	for index, row := range next.Rows {
		if row.ID != wantRows[index].ID {
			t.Fatalf("row %d=%q want=%q", index, row.ID, wantRows[index].ID)
		}
	}

	change := PeopleDirectoryChange{Href: roleColumn.Href, Sort: peopleSortRole, Descending: true}
	props.CommitSort(change)
	if committed != change {
		t.Fatalf("committed=%+v want=%+v", committed, change)
	}
}

func TestPeopleDirectoryKeepsLocalSortUntilParentProjectionChanges(t *testing.T) {
	view := testView(PagePeople)
	view.UpdatePeopleDirectory = func(PeopleDirectoryChange) {}
	window := paginatePeople(sortedPeople(filteredPeople(view), view.PeopleSort, view.PeopleDirection), view.PeoplePage, view.PeoplePageSize)
	incoming := *peopleDirectoryProps(view, window)
	local := incoming.ResolveSort(peopleSortTeam, true)
	state := peopleDirectoryState{InputKey: incoming.InputKey, Current: local}

	kept, reset := reconcilePeopleDirectoryState(incoming, state)
	if reset || kept.Current.InputKey != local.InputKey {
		t.Fatal("unchanged parent projection discarded the component-local sort")
	}

	view.PeopleTeam = "Product"
	filtered := filteredPeople(view)
	changed := *peopleDirectoryProps(view, paginatePeople(sortedPeople(filtered, view.PeopleSort, view.PeopleDirection), 1, view.PeoplePageSize))
	reconciled, reset := reconcilePeopleDirectoryState(changed, state)
	if !reset || reconciled.InputKey != changed.InputKey || len(reconciled.Current.Rows) != len(filtered) {
		t.Fatal("new parent projection did not replace component-local directory state")
	}
}

// TestTodo_3624_HeaderHrefsNameDefaultSortExplicitly guards the header links:
// the Person/ascending link must carry sort and dir, or a saved non-default
// preference answers for the missing keys and the table keeps its old sort.
func TestTodo_3624_HeaderHrefsNameDefaultSortExplicitly(t *testing.T) {
	view := testView(PagePeople)
	view.PeopleSort, view.PeopleDirection = peopleSortRole, peopleSortDescending
	view.PeopleTeam = "Platform"
	columns := peopleSortColumns(view)
	var personHref string
	for _, column := range columns {
		if column.ID == peopleSortName {
			personHref = column.Href
		}
	}
	if personHref == "" {
		t.Fatal("person column missing from header links")
	}
	parsed, err := url.Parse(personHref)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("sort"); got != peopleSortName {
		t.Fatalf("sort=%q want %q in %s", got, peopleSortName, personHref)
	}
	if got := parsed.Query().Get("dir"); got != peopleSortAscending {
		t.Fatalf("dir=%q want %q in %s", got, peopleSortAscending, personHref)
	}
	if got := parsed.Query().Get("team"); got != "Platform" {
		t.Fatalf("team facet dropped: %s", personHref)
	}

	view.PeopleSort, view.PeopleDirection = peopleSortName, peopleSortDescending
	for _, column := range peopleSortColumns(view) {
		if column.ID != peopleSortName {
			continue
		}
		parsed, err = url.Parse(column.Href)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Query().Get("sort") != peopleSortName || parsed.Query().Get("dir") != peopleSortAscending {
			t.Fatalf("descending person header must request explicit ascending: %s", column.Href)
		}
	}
}

func BenchmarkSortedPeople(b *testing.B) {
	for _, size := range []int{100, 10_000, 100_000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			people := make([]Person, size)
			for index := range people {
				people[index] = Person{
					ID: fmt.Sprintf("id-%09d", index), Name: fmt.Sprintf("Person %09d", size-index),
					Role: fmt.Sprintf("Role %03d", index%200), Team: fmt.Sprintf("Team %03d", index%75),
					Manager: fmt.Sprintf("Manager %04d", index%1000), Location: fmt.Sprintf("Location %03d", index%120),
				}
			}
			IndexPeople(people)
			b.ReportAllocs()
			for b.Loop() {
				_ = sortedPeople(people, peopleSortManager, peopleSortDescending)
			}
		})
	}
}
