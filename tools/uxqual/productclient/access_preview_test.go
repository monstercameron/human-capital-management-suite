package productclient

import (
	"reflect"
	"strings"
	"testing"
)

func uxscan008Fixture() AccessPreview {
	return AccessPreview{
		RoleName:       "manager",
		ExplicitRoles:  []string{"manager"},
		InheritedRoles: []string{"hr_partner"},
		EffectiveScope: "Engineering, Finance",
		CurrentUnits:   []string{"Engineering"},
		ProposedUnits:  []string{"Engineering", "Finance"},
		WithheldNote:   "Pay facts stay hidden outside compensation review.",
		NextAction:     "Review the proposed role before saving.",
	}
}

func TestTodo_UXSCAN_008(t *testing.T) {
	preview := uxscan008Fixture()
	if err := preview.Validate(); err != nil {
		t.Fatalf("valid effective-access preview rejected: %v", err)
	}
	explanation := preview.Explain()
	for _, want := range []string{"manager", "Engineering", "Finance", "Review the proposed role before saving."} {
		if !strings.Contains(explanation, want) {
			t.Errorf("preview explanation omits %q:\n%s", want, explanation)
		}
	}
	// RED: Roles showed "No explicit assignment" with no effective-role
	// context. A preview that reveals units while naming no role source is
	// still that same gap, so validation must refuse it.
	anonymous := uxscan008Fixture()
	anonymous.ExplicitRoles, anonymous.InheritedRoles = nil, nil
	if err := anonymous.Validate(); err == nil {
		t.Fatal("preview with visible units but no role source was accepted")
	}
	// RED: Organization reported "Access scope: Not reported". A preview
	// with visible units and no resolved scope is that same gap.
	scopeless := uxscan008Fixture()
	scopeless.EffectiveScope = "Not reported"
	if err := scopeless.Validate(); err == nil {
		t.Fatal("preview with an unreported scope was accepted")
	}
	// Additive role effects only: a proposed set that silently drops a
	// currently visible unit is a replacement, not the additive preview
	// this contract explains.
	shrinking := uxscan008Fixture()
	shrinking.ProposedUnits = []string{"Finance"}
	if err := shrinking.Validate(); err == nil {
		t.Fatal("non-additive proposed scope was accepted")
	}
	// A change that reveals new units without a task-specific next action
	// leaves the administrator with no deliberate next step.
	aimless := uxscan008Fixture()
	aimless.NextAction = ""
	if err := aimless.Validate(); err == nil {
		t.Fatal("additive preview without a next action was accepted")
	}
}

func TestTodo_UXSCAN_008_Golden(t *testing.T) {
	got := uxscan008Fixture().Explain()
	want := "Role \"manager\" would reveal 2 organization units.\n" +
		"Explicitly granted: manager.\n" +
		"Inherited: hr_partner.\n" +
		"Effective scope: Engineering, Finance.\n" +
		"Currently visible: Engineering.\n" +
		"Newly visible: Finance.\n" +
		"Withheld: Pay facts stay hidden outside compensation review.\n" +
		"Next: Review the proposed role before saving.\n"
	if got != want {
		t.Fatalf("preview explanation drifted:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestTodo_UXSCAN_008_Security(t *testing.T) {
	// The preview carries no worker records by construction: no field may
	// smuggle a worker, subject, principal, person or employee identity.
	previewType := reflect.TypeOf(AccessPreview{})
	for i := 0; i < previewType.NumField(); i++ {
		name := strings.ToLower(previewType.Field(i).Name)
		for _, needle := range []string{"worker", "subject", "principal", "person", "employee"} {
			if strings.Contains(name, needle) {
				t.Fatalf("preview field %q can carry a worker record", previewType.Field(i).Name)
			}
		}
	}
	// The browser never decides access: the explanation states what a role
	// would reveal and defers enforcement to the server, so it must not
	// issue allow/deny verdicts or leak implementation vocabulary.
	explanation := uxscan008Fixture().Explain()
	lowered := strings.ToLower(explanation)
	for _, verdict := range []string{"allowed", "denied", "authorized", "forbidden", "grpc", "protobuf", "rpc"} {
		if strings.Contains(lowered, verdict) {
			t.Errorf("preview explanation decides or leaks with %q:\n%s", verdict, explanation)
		}
	}
}

func TestTodo_UXSCAN_008_Accessibility(t *testing.T) {
	// Effective access is explained in words, never by glyph or color
	// alone: the text must name its explicit and inherited sources and
	// carry no Unicode arrows or dingbats a screen reader would garble.
	explanation := uxscan008Fixture().Explain()
	for _, want := range []string{"Explicitly granted:", "Inherited:", "Effective scope:"} {
		if !strings.Contains(explanation, want) {
			t.Errorf("preview explanation does not name its source in words (%q):\n%s", want, explanation)
		}
	}
	for _, glyph := range []string{"→", "←", "✓", "★", "☆", "●", "›", "⌄"} {
		if strings.Contains(explanation, glyph) {
			t.Errorf("preview explanation relies on glyph %q:\n%s", glyph, explanation)
		}
	}
}

func TestTodo_UXSCAN_008_Regression(t *testing.T) {
	first := uxscan008Fixture().Explain()
	// Field order and unit order must not change the served explanation.
	reordered := uxscan008Fixture()
	reordered.ProposedUnits = []string{"Finance", "Engineering"}
	reordered.CurrentUnits = []string{"Engineering"}
	if second := reordered.Explain(); second != first {
		t.Fatalf("equivalent preview rendered unstably:\n%s\n%s", first, second)
	}
	// An unchanged role still explains itself: no additions means no next
	// action is required, but the summary must stay explicit.
	steady := uxscan008Fixture()
	steady.ProposedUnits = []string{"Engineering"}
	steady.NextAction = ""
	if err := steady.Validate(); err != nil {
		t.Fatalf("steady preview should need no next action: %v", err)
	}
	if got := steady.Explain(); !strings.Contains(got, "Currently visible: Engineering.") {
		t.Errorf("steady preview lost its visible scope:\n%s", got)
	}
}
