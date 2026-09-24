package importgraph

import (
	"reflect"
	"testing"
)

func TestTodo_ARCH_GO_003_Golden(t *testing.T) {
	edges := []Edge{{Importer: "m/a", Imported: "m/b"}, {Importer: "m/b", Imported: "m/c"}}
	got := computeDigest(edges)
	const want = "8b65cc660475413aac2f165fab422be232fd338db3ce5f5213a0ad98e3facdf4"
	if got != want {
		t.Fatalf("digest = %s, want %s", got, want)
	}
}

func TestTodo_ARCH_GO_003_Conformance(t *testing.T) {
	cases := []struct {
		name  string
		edges []Edge
		want  []string
	}{
		{"acyclic chain", []Edge{{"m/a", "m/b"}, {"m/b", "m/c"}}, nil},
		{"self cycle", []Edge{{"m/a", "m/a"}}, []string{"m/a", "m/a"}},
		{"two node cycle", []Edge{{"m/a", "m/b"}, {"m/b", "m/a"}}, []string{"m/a", "m/b", "m/a"}},
		{"disconnected cycle", []Edge{{"m/a", "m/b"}, {"m/x", "m/y"}, {"m/y", "m/x"}}, []string{"m/x", "m/y", "m/x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := findCycle(tc.edges); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("findCycle() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTodo_ARCH_GO_003_Property(t *testing.T) {
	base := []Edge{{"m/a", "m/b"}, {"m/b", "m/c"}, {"m/a", "m/d"}}
	permuted := []Edge{base[2], base[0], base[1]}
	want := computeDigest(base)
	if got := computeDigest(permuted); got != want {
		t.Fatalf("permuted edges digest = %s, want %s", got, want)
	}
	changed := append(append([]Edge(nil), base...), Edge{"m/d", "m/e"})
	if got := computeDigest(changed); got == want {
		t.Fatalf("adding a distinct edge did not change digest %s", got)
	}
}
