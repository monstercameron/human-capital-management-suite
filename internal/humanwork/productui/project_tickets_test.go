package productui

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
)

// TestTodo_3612_MyTicketsEmptyKind pins the My tickets empty classification:
// an assignment hidden only by an address filter or search must not read as
// "nothing is assigned to you", and loading must not read as emptiness.
func TestTodo_3612_MyTicketsEmptyKind(t *testing.T) {
	cases := []struct {
		name string
		list projectui.TicketList
		want projectMyTicketsEmptyKindValue
	}{
		{name: "genuinely nothing assigned", list: projectui.TicketList{}, want: projectMyTicketsNothingAssigned},
		{name: "assigned work hidden by a filter", list: projectui.TicketList{AssignedOpen: 1}, want: projectMyTicketsFilteredOut},
		{name: "assigned work while still loading", list: projectui.TicketList{AssignedOpen: 1, Loading: true}, want: projectMyTicketsLoading},
		{name: "no assignment while still loading", list: projectui.TicketList{Loading: true}, want: projectMyTicketsLoading},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := projectMyTicketsEmptyKind(tc.list); got != tc.want {
				t.Fatalf("projectMyTicketsEmptyKind() = %d, want %d", got, tc.want)
			}
		})
	}
}
