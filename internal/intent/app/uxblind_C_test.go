package app

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/positionfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
)

func TestTodo_UXBLIND_012(t *testing.T) {
	if got := journeySubjects("worker-1", ""); len(got) != 1 || got[0].GetSubjectId() != "worker-1" {
		t.Fatalf("unchosen target position changed the subjects: %+v", got)
	}
	chosen := journeySubjects("worker-1", "position-revision-1")
	if len(chosen) != 2 || chosen[1].GetSubjectId() != "position-revision-1" {
		t.Fatalf("chosen target position was not recorded: %+v", chosen)
	}
}

func TestTodo_UXBLIND_012_Regression(t *testing.T) {
	cases := []struct {
		name, target string
		want         int
	}{
		{name: "omitted", target: "", want: 1},
		{name: "chosen", target: "position-revision-1", want: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(journeySubjects("worker-1", tc.target)); got != tc.want {
				t.Fatalf("journeySubjects(%q) returned %d subjects, want %d", tc.target, got, tc.want)
			}
		})
	}
}

func TestTodo_UXBLIND_007_Property(t *testing.T) {
	id := "11111111-1111-4111-8111-000000000004"
	facts := &vacancyFacts{revisions: map[string]position.PositionRevision{
		id: vacancyRevision(t, id, "PPL-DIR", "PEOPLE", "1.0000", position.LifecycleOpen),
	}}
	directory := &vacancyDirectory{rows: []positionfacts.DirectoryRow{
		vacancyRow(id, "Director of People Operations", "PPL-DIR", "PEOPLE", vacancyOccupant(t, "worker-occupied", "1.0000")),
	}}
	options, err := vacancyEngine(directory, facts).positionVacancies(context.Background(), gatePrincipal(t, "promotion_operator"))
	if err != nil {
		t.Fatalf("positionVacancies: %v", err)
	}
	if len(options) != 0 {
		t.Fatalf("an actively occupied position was projected as open: %+v", options)
	}
}
