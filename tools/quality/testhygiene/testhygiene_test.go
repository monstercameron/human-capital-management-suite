package testhygiene

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestTodo_REV_103_03 is the REV-103-03 primary test. It proves the lint
// rejects each of the three RED shapes (a test whose body only calls
// another test, a _Race test with no goroutine and no concurrency helper,
// a Golden test that skips) while accepting the legitimate neighbors:
// real tests with assertions, TestMain, non-Test helpers, Race tests with
// go statements/t.Parallel/sync/atomic/errgroup/WaitGroup, Golden tests
// that fail, and skips outside Golden tests.
func TestTodo_REV_103_03(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "alias single call is rejected",
			src: `package p
import "testing"
func TestAlias(t *testing.T) { TestReal(t) }
func TestReal(t *testing.T) { if 1 != 1 { t.Fatal("x") } }
`,
			want: []string{RuleAliasTest},
		},
		{
			name: "alias multiple calls is rejected",
			src: `package p
import "testing"
func TestAlias(t *testing.T) {
	TestA(t)
	TestB(t)
}
`,
			want: []string{RuleAliasTest},
		},
		{
			name: "deferred selector test forwarding is rejected",
			src: `package p
import "testing"
func TestAlias(t *testing.T) { defer suite.TestTarget(t) }
`,
			want: []string{RuleAliasTest},
		},
		{
			name: "conditional test forwarding is rejected",
			src: `package p
import "testing"
func TestAlias(t *testing.T) { if testing.Short() { TestTarget(t) } else { (TestOther)(t) } }
`,
			want: []string{RuleAliasTest},
		},
		{
			name: "conditional test forwarding without else is rejected",
			src: `package p
import "testing"
func TestAlias(t *testing.T) { if testing.Short() { TestTarget(t) } }
`,
			want: []string{RuleAliasTest},
		},
		{
			name: "real test with assertions is accepted",
			src: `package p
import "testing"
func TestReal(t *testing.T) {
	if got, want := 1+1, 2; got != want {
		t.Fatalf("got %d want %d", got, want)
	}
}
`,
			want: nil,
		},
		{
			name: "TestMain is not a test",
			src: `package p
import "testing"
func TestMain(m *testing.M) { m.Run() }
`,
			want: nil,
		},
		{
			name: "non-Test helper calling a test is accepted",
			src: `package p
import "testing"
func runAll(t *testing.T) { TestReal(t) }
`,
			want: nil,
		},
		{
			name: "race sequential loop is rejected",
			src: `package p
import "testing"
func TestTodo_SVC_006_Race(t *testing.T) {
	for i := 0; i < 32; i++ {
		if i < 0 {
			t.Fatalf("iteration %d", i)
		}
	}
}
`,
			want: []string{RuleRaceWithoutConcurrency},
		},
		{
			name: "race mutex alone is rejected",
			src: `package p
import (
	"sync"
	"testing"
)
func TestTodo_SVC_006_Race(t *testing.T) {
	var mu sync.Mutex
	mu.Lock()
	mu.Unlock()
}
`,
			want: []string{RuleRaceWithoutConcurrency},
		},
		{
			name: "race atomic alone is rejected",
			src: `package p
import (
	"sync/atomic"
	"testing"
)
func TestTodo_SVC_006_Race(t *testing.T) {
	var n atomic.Int64
	n.Add(1)
}
`,
			want: []string{RuleRaceWithoutConcurrency},
		},
		{
			name: "race unused errgroup is rejected",
			src: `package p
import (
	"testing"
	"golang.org/x/sync/errgroup"
)
func TestTodo_SVC_006_Race(t *testing.T) {
	var g errgroup.Group
	_ = g
}
`,
			want: []string{RuleRaceWithoutConcurrency},
		},
		{
			name: "dead function literal with go is rejected",
			src: `package p
import "testing"
func TestTodo_SVC_006_Race(t *testing.T) { unused := func() { go work() }; _ = unused }
`,
			want: []string{RuleRaceWithoutConcurrency},
		},
		{
			name: "go in constant false branch is rejected",
			src: `package p
import "testing"
func TestTodo_SVC_006_Race(t *testing.T) { if false { go work() } }
`,
			want: []string{RuleRaceWithoutConcurrency},
		},
		{
			name: "arbitrary Go method is rejected",
			src: `package p
import "testing"
type runner struct{}
func (runner) Go(func()) {}
func TestTodo_SVC_006_Race(t *testing.T) { runner{}.Go(func(){}) }
`,
			want: []string{RuleRaceWithoutConcurrency},
		},
		{
			name: "race alias is rejected by both rules",
			src: `package p
import "testing"
func TestTodo_TOOL_012_Race(t *testing.T) { TestTodo_TOOL_012(t) }
`,
			want: []string{RuleAliasTest, RuleRaceWithoutConcurrency},
		},
		{
			name: "race with go statement is accepted",
			src: `package p
import "testing"
func TestTodo_X_Race(t *testing.T) {
	done := make(chan int, 1)
	go func() { done <- 1 }()
	if <-done != 1 {
		t.Fatal("goroutine did not run")
	}
}
`,
			want: nil,
		},
		{
			name: "race with t.Parallel is accepted",
			src: `package p
import "testing"
func TestTodo_X_Race(t *testing.T) {
	t.Parallel()
	if 1 != 1 {
		t.Fatal("x")
	}
}
`,
			want: nil,
		},
		{
			name: "race with WaitGroup is accepted",
			src: `package p
import (
	"sync"
	"testing"
)
func TestTodo_X_Race(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done() }()
	wg.Wait()
}
`,
			want: nil,
		},
		{
			name: "race with atomic is accepted",
			src: `package p
import (
	"sync"
	"sync/atomic"
	"testing"
)
func TestTodo_X_Race(t *testing.T) {
	var n atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.Add(1)
		}()
	}
	wg.Wait()
	if n.Load() != 2 {
		t.Fatalf("atomic count = %d, want 2", n.Load())
	}
}
`,
			want: nil,
		},
		{
			name: "race with errgroup is accepted",
			src: `package p
import (
	"testing"
	"golang.org/x/sync/errgroup"
)
func TestTodo_X_Race(t *testing.T) {
	var g errgroup.Group
	g.Go(func() error { return nil })
	if err := g.Wait(); err != nil {
		t.Fatal(err)
	}
}
`,
			want: nil,
		},
		{
			name: "golden skipf on missing file is rejected",
			src: `package p
import (
	"os"
	"testing"
)
func TestTodo_X_Golden(t *testing.T) {
	data, err := os.ReadFile("testdata/x.golden")
	if err != nil {
		t.Skipf("golden file missing: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("empty")
	}
}
`,
			want: []string{RuleGoldenSkip},
		},
		{
			name: "golden SkipNow is rejected",
			src: `package p
import "testing"
func TestTodo_X_Golden(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
}
`,
			want: []string{RuleGoldenSkip},
		},
		{
			name: "dot-imported golden skip is rejected",
			src: `package p
import . "testing"
func TestTodo_X_Golden(t *T) { Skip("missing oracle") }
`,
			want: []string{RuleGoldenSkip},
		},
		{
			name: "golden skip inside subtest closure is rejected",
			src: `package p
import "testing"
func TestTodo_X_Golden(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		t.Skip("no oracle checked in yet")
	})
}
`,
			want: []string{RuleGoldenSkip},
		},
		{
			name: "golden skip through helper is rejected",
			src: `package p
import "testing"
func skipMissing(t *testing.T) { t.Skip("missing") }
func TestTodo_X_Golden(t *testing.T) { skipMissing(t) }
`,
			want: []string{RuleGoldenSkip},
		},
		{
			name: "uninvoked skip helper is accepted",
			src: `package p
import "testing"
func skipMissing(t *testing.T) { t.Skip("missing") }
func TestTodo_X_Golden(t *testing.T) { t.Fatal("missing") }
`,
			want: nil,
		},
		{
			name: "golden test that fails is accepted",
			src: `package p
import "testing"
func TestTodo_X_Golden(t *testing.T) {
	if got, want := 2+2, 4; got != want {
		t.Fatalf("got %d want %d", got, want)
	}
}
`,
			want: nil,
		},
		{
			name: "skip outside a golden test is accepted",
			src: `package p
import "testing"
func TestTodo_X_Race(t *testing.T) {
	t.Skip("race detector unsupported here")
}
`,
			want: []string{RuleRaceWithoutConcurrency},
		},
		{
			name: "non-test file is ignored",
			src: `package p
func Helper() {}
`,
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filename := "case_test.go"
			src := tc.src
			if tc.name == "non-test file is ignored" {
				filename = "helper.go"
			}
			found, err := CheckSource(filename, []byte(src))
			if err != nil {
				t.Fatalf("CheckSource: %v", err)
			}
			var got []string
			for _, v := range found {
				got = append(got, v.Rule)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("rules = %q, want %q", got, tc.want)
			}
			for _, v := range found {
				if v.Test == "" || v.Line <= 0 || v.File == "" || v.Detail == "" {
					t.Fatalf("violation missing fields: %#v", v)
				}
				if !strings.Contains(v.String(), v.Rule) {
					t.Fatalf("String() drops rule: %q", v.String())
				}
			}
		})
	}

	t.Run("invalid source is an error, never a pass", func(t *testing.T) {
		if _, err := CheckSource("broken_test.go", []byte("package p\nfunc TestX( {}\n")); err == nil {
			t.Fatal("expected a parse error for invalid source, got nil")
		}
	})

	t.Run("missing scan dir is an error, never a pass", func(t *testing.T) {
		if _, err := CheckDir(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
			t.Fatal("expected an error for a missing scan dir, got nil")
		}
	})
}

// TestTodo_REV_103_03_Golden pins the lint's exact report bytes over a
// versioned fixture tree. The oracle is checked in; a missing oracle fails
// this test outright (a golden test must never skip on a missing file, per
// the very rule under test).
func TestTodo_REV_103_03_Golden(t *testing.T) {
	dir := filepath.Join("testdata", "golden")
	found, err := CheckTree(dir)
	if err != nil {
		t.Fatalf("CheckTree: %v", err)
	}
	oraclePath := filepath.Join(dir, "expected.txt")
	want, err := os.ReadFile(oraclePath)
	if err != nil {
		t.Fatalf("golden oracle %s is missing or unreadable; a missing golden file must fail, never skip: %v", oraclePath, err)
	}
	if got := FormatReport(found); got != string(want) {
		t.Fatalf("report mismatch:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
	if got := FormatReport(nil); got != "" {
		t.Fatalf("empty report = %q, want empty string", got)
	}
}
