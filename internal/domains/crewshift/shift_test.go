package crewshift

import (
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var update = flag.Bool("update", false, "rewrite the crewshift golden files")

// TestTodo_FTIME_006 is the primary contract test: a draft publishes into a
// PUBLISHED shift carrying a recorded approver and an incremented revision,
// and a draft that fails a check never becomes published.
func TestTodo_FTIME_006(t *testing.T) {
	draft := fixtureDraft("shift-1")
	if err := draft.Validate(); err != nil {
		t.Fatalf("fixture draft should validate: %v", err)
	}

	out, err := Publish(fixturePublishInput(draft), DefaultPublishChecks())
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if out.Shift.Status != StatusPublished {
		t.Fatalf("published shift status = %s, want PUBLISHED", out.Shift.Status)
	}
	if out.Shift.Revision != draft.Revision+1 {
		t.Fatalf("published revision = %d, want %d", out.Shift.Revision, draft.Revision+1)
	}
	if out.Shift.ApprovedBy.String() != fixtureApprover.String() {
		t.Fatalf("approved by = %s, want %s", out.Shift.ApprovedBy.String(), fixtureApprover.String())
	}
	if out.Shift.ApprovedAt.IsZero() || out.Shift.PublishedAt.IsZero() {
		t.Fatalf("published shift must record approval and publication instants")
	}

	// RED: a draft assignment must never appear as a published shift when a
	// check fails -- here, no project access.
	blocked := fixtureDraft("shift-2")
	input := fixturePublishInput(blocked)
	input.ProjectAccess = map[string]bool{} // no access granted
	if _, err := Publish(input, DefaultPublishChecks()); !errors.Is(err, ErrPublishRejected) {
		t.Fatalf("Publish without project access = %v, want ErrPublishRejected", err)
	}

	// RED: one worker double-booked across two different projects must be
	// refused, not silently accepted because the projects differ.
	first := mustPublish(t, fixtureDraft("shift-3"))
	second := fixtureDraft("shift-4")
	second.ProjectRef = fixtureProject2
	secondInput := fixturePublishInput(second)
	secondInput.ProjectAccess[fixtureProject2.String()] = true
	secondInput.Existing = []Shift{first}
	if _, err := Publish(secondInput, DefaultPublishChecks()); !errors.Is(err, ErrPublishRejected) {
		t.Fatalf("Publish overlapping a different project = %v, want ErrPublishRejected", err)
	}
}

// TestTodo_FTIME_006_Security proves every publication check fails closed:
// missing eligibility, a revoked worker, a missing qualification, a denied
// project, insufficient rest, and an optimizer proposal outside the one
// authority path are all rejected, never silently accepted.
func TestTodo_FTIME_006_Security(t *testing.T) {
	t.Run("revoked eligibility", func(t *testing.T) {
		draft := fixtureDraft("sec-revoked")
		in := fixturePublishInput(draft)
		in.Eligibility = EligibilityFacts{Active: true, Revoked: true}
		if _, err := Publish(in, DefaultPublishChecks()); !errors.Is(err, ErrPublishRejected) {
			t.Fatalf("revoked worker published: %v", err)
		}
	})
	t.Run("missing qualification", func(t *testing.T) {
		draft := fixtureDraft("sec-qual")
		in := fixturePublishInput(draft)
		in.RequiredQualifications = []values.EntityRef{fixtureQualA}
		if _, err := Publish(in, DefaultPublishChecks()); !errors.Is(err, ErrPublishRejected) {
			t.Fatalf("published without required qualification: %v", err)
		}
		in.HeldQualifications = []values.EntityRef{fixtureQualA}
		if _, err := Publish(in, DefaultPublishChecks()); err != nil {
			t.Fatalf("Publish with the held qualification: %v", err)
		}
	})
	t.Run("denied project access", func(t *testing.T) {
		draft := fixtureDraft("sec-access")
		in := fixturePublishInput(draft)
		in.ProjectAccess = map[string]bool{draft.ProjectRef.String(): false}
		if _, err := Publish(in, DefaultPublishChecks()); !errors.Is(err, ErrPublishRejected) {
			t.Fatalf("published without project access: %v", err)
		}
	})
	t.Run("insufficient rest", func(t *testing.T) {
		first := mustPublish(t, fixtureDraft("sec-rest-1"))
		second := fixtureDraft("sec-rest-2")
		second.Work = Interval{Start: first.Work.End.Add(2 * time.Hour), End: first.Work.End.Add(10 * time.Hour)}
		in := fixturePublishInput(second)
		in.Existing = []Shift{first}
		in.Rest = RestPolicy{MinimumRest: 8 * time.Hour}
		if _, err := Publish(in, DefaultPublishChecks()); !errors.Is(err, ErrPublishRejected) {
			t.Fatalf("published with only 2h rest against an 8h policy: %v", err)
		}
	})
	t.Run("notice policy", func(t *testing.T) {
		draft := fixtureDraft("sec-notice")
		in := fixturePublishInput(draft)
		in.Notice = NoticePolicy{MinimumNotice: 7 * 24 * time.Hour}
		in.Now = draft.Work.Start.Add(-time.Hour) // one hour of notice, policy wants a week
		if _, err := Publish(in, DefaultPublishChecks()); !errors.Is(err, ErrPublishRejected) {
			t.Fatalf("published with one hour notice against a week policy: %v", err)
		}
	})
	t.Run("optimizer cannot use a different authority path", func(t *testing.T) {
		draft := fixtureDraft("sec-optimizer")
		draft.Source = SourceOptimizer
		in := fixturePublishInput(draft)
		in.AuthorityPath = "crewshift.optimizer.shortcut.v1"
		if _, err := Publish(in, DefaultPublishChecks()); !errors.Is(err, ErrPublishRejected) {
			t.Fatalf("optimizer proposal on a different authority path published: %v", err)
		}
		// The identical proposal on the one true path succeeds.
		in.AuthorityPath = AuthorityPathCrewShiftPublish
		if _, err := Publish(in, DefaultPublishChecks()); err != nil {
			t.Fatalf("optimizer proposal on the shared authority path: %v", err)
		}
	})
}

// TestTodo_FTIME_006_Property publishes many optimizer-sourced proposals
// carrying random authority-path strings. None of them equal the one
// declared path, so every single one must be rejected -- an optimizer never
// gets a wider door than a manual draft by accident of string content.
func TestTodo_FTIME_006_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(20260928))
	const trials = 200
	alphabet := "abcdefghijklmnopqrstuvwxyz.-_0123456789"
	for i := 0; i < trials; i++ {
		n := 1 + rng.Intn(40)
		b := make([]byte, n)
		for j := range b {
			b[j] = alphabet[rng.Intn(len(alphabet))]
		}
		path := string(b)
		if path == AuthorityPathCrewShiftPublish {
			continue // the one legitimate value; skip this trial
		}
		draft := fixtureDraft(fmt.Sprintf("prop-%d", i))
		draft.Source = SourceOptimizer
		in := fixturePublishInput(draft)
		in.AuthorityPath = path
		if _, err := Publish(in, DefaultPublishChecks()); !errors.Is(err, ErrPublishRejected) {
			t.Fatalf("trial %d: optimizer path %q published, want rejection", i, path)
		}
	}

	// The same property holds for a manual draft: only the one path works.
	for i := 0; i < trials; i++ {
		n := 1 + rng.Intn(40)
		b := make([]byte, n)
		for j := range b {
			b[j] = alphabet[rng.Intn(len(alphabet))]
		}
		path := string(b)
		if path == AuthorityPathCrewShiftPublish {
			continue
		}
		draft := fixtureDraft(fmt.Sprintf("prop-manual-%d", i))
		in := fixturePublishInput(draft)
		in.AuthorityPath = path
		if _, err := Publish(in, DefaultPublishChecks()); !errors.Is(err, ErrPublishRejected) {
			t.Fatalf("manual trial %d: path %q published, want rejection", i, path)
		}
	}
}

// TestTodo_FTIME_006_Golden pins WorkedMinutes across both daylight-saving
// transitions in America/New_York for 2024: a spring-forward shift loses an
// hour of elapsed instant time and a fall-back shift gains one. Both numbers
// come from time.Time/time.Date's own correct handling of the transition,
// never from a naive clock-face subtraction, and the golden file is what
// proves that stays true if this file is ever refactored.
func TestTodo_FTIME_006_Golden(t *testing.T) {
	loc := mustLoc()
	cases := []struct {
		name  string
		start time.Time
		end   time.Time
	}{
		{
			name:  "spring_forward_2024-03-10",
			start: time.Date(2024, 3, 10, 1, 30, 0, 0, loc),
			end:   time.Date(2024, 3, 10, 4, 30, 0, 0, loc),
		},
		{
			name:  "fall_back_2024-11-03",
			start: time.Date(2024, 11, 3, 0, 30, 0, 0, loc),
			end:   time.Date(2024, 11, 3, 3, 30, 0, 0, loc),
		},
		{
			name:  "no_transition_2024-06-10",
			start: time.Date(2024, 6, 10, 9, 0, 0, 0, loc),
			end:   time.Date(2024, 6, 10, 17, 0, 0, 0, loc),
		},
	}

	var b strings.Builder
	for _, c := range cases {
		shift := fixtureDraft("golden-" + c.name)
		shift.Work = Interval{Start: c.start, End: c.end}
		if err := shift.Validate(); err != nil {
			t.Fatalf("%s: shift should validate: %v", c.name, err)
		}
		fmt.Fprintf(&b, "%s: start=%s end=%s worked_minutes=%d\n",
			c.name, c.start.Format(time.RFC3339), c.end.Format(time.RFC3339), shift.WorkedMinutes())
	}
	assertShiftGolden(t, "worked-minutes.txt", b.String())
}

func assertShiftGolden(t *testing.T, name, rendered string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (run with -update to create it): %v", path, err)
	}
	if got := rendered; got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("golden %s does not match.\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}
