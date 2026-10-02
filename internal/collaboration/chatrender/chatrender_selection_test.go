package chatrender

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func renderingFixture() (Policy, Preference, Rendering) {
	original := Rendering{Tenant: "tenant", Message: "post", Revision: 2, Tone: AsWritten, Language: "de", SourceLanguage: "de", Text: "original"}
	r := original
	r.Language = "en"
	r.Text = "translation"
	r.Kinds = []Kind{Translate}
	r.Checks = Checks{Meaning: true, Placeholders: true}
	return Policy{Original: original, AllowOriginal: true, AllowedKinds: []Kind{Mask, Reword, Translate}}, Preference{Tone: AsWritten, ReadingLanguage: "en", Translate: true}, r
}
func TestTodo_CHATRENDER_001(t *testing.T) {
	policy, pref, r := renderingFixture()
	got, mark := SelectForReader(policy, pref, Available{Renderings: []Rendering{r}})
	if got.Text != "translation" || mark.State != "ready" || mark.SourceLanguage != "de" || !mark.CanShowOriginal {
		t.Fatalf("selection=%+v %+v", got, mark)
	}
	r.Tone = Reworded
	r.Kinds = []Kind{Reword, Translate}
	pref.Tone = Reworded
	got, mark = Select(policy, pref, Available{Renderings: []Rendering{r}})
	if got.Tone != Reworded || len(mark.Kinds) != 2 {
		t.Fatal(got, mark)
	}
	pref.Translate = false
	pref.Tone = AsWritten
	got, mark = Select(policy, pref, Available{Pending: true})
	if got.Text != "original" || mark.State != "original" {
		t.Fatal(got, mark)
	}
	testRenderingRegistry(t)
	testRenderingWorker(t)
}
func TestTodo_CHATRENDER_001_Property(t *testing.T) {
	policy, pref, r := renderingFixture()
	before := r
	for _, allow := range []bool{false, true} {
		for _, mask := range []bool{false, true} {
			for _, reword := range []bool{false, true} {
				policy.AllowOriginal = allow
				policy.RequireMask = mask
				policy.RequireReworded = reword
				for _, a := range []Available{{}, {Pending: true}, {Pending: true, WaitExpired: true}, {Failed: true}, {Renderings: []Rendering{r}}} {
					one, m1 := Select(policy, pref, a)
					two, m2 := Select(policy, pref, a)
					if !reflect.DeepEqual(one, two) || !reflect.DeepEqual(m1, m2) {
						t.Fatal("non deterministic")
					}
					if one.Text == "original" && (!allow || mask || reword) {
						t.Fatal("forbidden original")
					}
					if one.Text != "" && one.Text != "original" && (mask && !hasKind(one.Kinds, Mask) || reword && one.Tone != Reworded) {
						t.Fatal("forbidden derived text")
					}
				}
			}
		}
	}
	if !reflect.DeepEqual(before, r) {
		t.Fatal("input mutated")
	}
}
func TestTodo_CHATRENDER_001_Security(t *testing.T) {
	policy, pref, r := renderingFixture()
	policy.AllowOriginal = false
	mutations := []func(*Rendering){func(r *Rendering) { r.Tenant = "other" }, func(r *Rendering) { r.Message = "other" }, func(r *Rendering) { r.Revision++ }, func(r *Rendering) { r.Checks.Meaning = false }, func(r *Rendering) { r.Checks.Placeholders = false }, func(r *Rendering) { r.Checks.Failures = []string{"unsafe"} }, func(r *Rendering) { r.Kinds = []Kind{"unregistered"} }}
	for i, mutate := range mutations {
		bad := r
		mutate(&bad)
		got, _ := Select(policy, pref, Available{Renderings: []Rendering{bad}, Failed: true})
		if got.Text != "" {
			t.Fatalf("mutation %d leaked %q", i, got.Text)
		}
	}
	policy.RequireMask = true
	masked := r
	masked.Kinds = []Kind{Mask, Translate}
	got, _ := Select(policy, pref, Available{Renderings: []Rendering{r, masked}})
	if !hasKind(got.Kinds, Mask) {
		t.Fatal(got)
	}
}
func TestTodo_CHATRENDER_001_Fault(t *testing.T) {
	testRenderingWorkerFault(t)
	policy, pref, _ := renderingFixture()
	got, mark := Select(policy, pref, Available{Pending: true})
	if got.Text != "" || mark.State != "pending" {
		t.Fatal(got, mark)
	}
	for _, a := range []Available{{Pending: true, WaitExpired: true}, {Failed: true}} {
		got, mark = Select(policy, pref, a)
		if got.Text != "original" || mark.State != "fallback" {
			t.Fatal(got, mark)
		}
		policy.AllowOriginal = false
		got, mark = Select(policy, pref, a)
		if got.Text != "" || mark.State != "unavailable" {
			t.Fatal(got, mark)
		}
		policy.AllowOriginal = true
	}
	empty, _ := NewRegistry()
	if _, err := empty.Produce(context.Background(), policy.Original, []Kind{Translate}); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := (FixtureProducer{}).Produce(context.Background(), policy.Original); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}
func TestTodo_CHATRENDER_001_Performance(t *testing.T) {
	policy, pref, r := renderingFixture()
	start := time.Now()
	const count = 10000
	for i := 0; i < count; i++ {
		got, _ := Select(policy, pref, Available{Renderings: []Rendering{r}})
		if got.Text != "translation" {
			t.Fatal(got)
		}
	}
	if elapsed := time.Since(start) / count; elapsed >= time.Millisecond {
		t.Fatalf("selection %v >= 1ms", elapsed)
	}
}

type orderedProducer struct {
	calls *[]string
	kind  string
}

func (p orderedProducer) Produce(_ context.Context, r Rendering) (Rendering, error) {
	*p.calls = append(*p.calls, p.kind)
	r.Checks = Checks{Meaning: true, Placeholders: true}
	return r, nil
}
func testRenderingRegistry(t *testing.T) {
	policy, pref, _ := renderingFixture()
	calls := []string{}
	registry, err := NewRegistry(Registration{Translate, orderedProducer{&calls, "translate"}}, Registration{Reword, orderedProducer{&calls, "reword"}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := registry.Produce(context.Background(), policy.Original, []Kind{Translate, Reword})
	if err != nil || !reflect.DeepEqual(calls, []string{"reword", "translate"}) || !reflect.DeepEqual(out.Kinds, []Kind{Reword, Translate}) {
		t.Fatal(out, err, calls)
	}
	if len(out.Producers) != 2 {
		t.Fatal("chain provenance missing", out)
	}
	pref.Tone = Reworded
	targets := RequestsForReaders(policy.Original, []Preference{pref, pref})
	if len(targets) != 1 || targets[0].Text != "" || targets[0].Language != "en" {
		t.Fatal(targets)
	}
	if _, err = NewRegistry(Registration{Translate, nil}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err = NewRegistry(Registration{Translate, FixtureProducer{Text: "a"}}, Registration{Translate, FixtureProducer{Text: "b"}}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	fixture := FixtureProducer{Text: "hello", Tone: Reworded, Language: "en", Identity: ProducerIdentity{Provider: "fixture", Model: "deterministic"}}
	out, err = fixture.Produce(context.Background(), policy.Original)
	if err != nil || out.Text != "hello" || out.Tone != Reworded || out.Language != "en" || out.Producer.Provider != "fixture" {
		t.Fatal(out, err)
	}
}
