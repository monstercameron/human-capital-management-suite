package productui

import (
	"strings"
	"testing"
)

func TestSortProjectRows(t *testing.T) {
	rows := func() []projectListRow {
		return []projectListRow{
			{ID: "b", Name: "beta", Status: "Active", Owner: "Zoe", TaskCount: 3, TaskKnown: true},
			{ID: "a", Name: "Alpha", Status: "Paused", Owner: "Adam", TaskCount: 9, TaskKnown: true},
			{ID: "c", Name: "gamma", Status: "Active", Owner: "Mia"},
		}
	}
	names := func(in []projectListRow) string {
		out := make([]string, 0, len(in))
		for _, row := range in {
			out = append(out, row.ID)
		}
		return strings.Join(out, ",")
	}
	cases := []struct {
		order projectListSort
		want  string
	}{
		{projectListSort{Key: projectSortName}, "a,b,c"},
		{projectListSort{Key: projectSortName, Desc: true}, "c,b,a"},
		{projectListSort{Key: projectSortStatus}, "b,c,a"},             // Active ties break by name
		{projectListSort{Key: projectSortStatus, Desc: true}, "a,b,c"}, // ties stay name-ascending
		{projectListSort{Key: projectSortOwner}, "a,c,b"},
		{projectListSort{Key: projectSortTasks, Desc: true}, "a,b,c"}, // unknown count last
		{projectListSort{Key: projectSortTasks}, "b,a,c"},             // unknown count last either way
	}
	for _, tc := range cases {
		got := rows()
		sortProjectRows(got, tc.order)
		if names(got) != tc.want {
			t.Errorf("sort %+v = %s, want %s", tc.order, names(got), tc.want)
		}
	}
}
