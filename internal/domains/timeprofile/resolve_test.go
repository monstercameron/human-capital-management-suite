package timeprofile

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestTodo_WTIME_002 proves the RED scenarios named in the todo: plan
// resolution reading only tenant and intent type (here: a contractor
// resolving to the punch template it must never run), a profile change
// mid-period repinning an open run, and two templates matching one profile.
func TestTodo_WTIME_002(t *testing.T) {
	t.Run("resolution reads the profile, not just tenant and intent type", func(t *testing.T) {
		contractor := baseProfile(t)
		contractor.Category = CategoryContractor
		contractor.PayBasis = PayContract
		contractor.Exemption = NotApplicable
		contractor.Destination = DestinationInvoice
		contractor.Capture = CapturePunch // a punch-shaped capture mode does not steer a contractor

		tmpl, err := TemplateFor(contractor)
		if err != nil {
			t.Fatalf("TemplateFor: %v", err)
		}
		if tmpl != TemplateContractorTime {
			t.Fatalf("a contractor's entry ran template %q, not the contractor template", tmpl)
		}
	})

	t.Run("agency-temp resolves to the agency template regardless of capture mode", func(t *testing.T) {
		agency := baseProfile(t)
		agency.Category = CategoryAgencyTemp
		agency.Exemption = NotApplicable
		agency.Destination = DestinationAgency
		agency.Capture = CaptureNone

		tmpl, err := TemplateFor(agency)
		if err != nil {
			t.Fatalf("TemplateFor: %v", err)
		}
		if tmpl != TemplateAgencyTime {
			t.Fatalf("got template %q, want %q", tmpl, TemplateAgencyTime)
		}
	})

	t.Run("exactly one profile resolves per assignment; two equal-priority matches are ambiguous", func(t *testing.T) {
		tenant := valuesTenant("acme-co")
		facts := AssignmentFacts{TenantRef: tenant, Category: CategoryEmployee}
		at := instant(t, "2026-03-01T00:00:00Z")

		profileA := baseProfile(t)
		profileA.ID = "tp-a"
		profileB := baseProfile(t)
		profileB.ID = "tp-b"

		ruleA := EligibilityRule{ID: "rule-a", TenantRef: tenant, Category: CategoryEmployee, Priority: 5, Profile: profileA}
		ruleB := EligibilityRule{ID: "rule-b", TenantRef: tenant, Category: CategoryEmployee, Priority: 5, Profile: profileB}

		_, err := Resolve([]EligibilityRule{ruleA, ruleB}, facts, at)
		if !errors.Is(err, ErrAmbiguousProfile) {
			t.Fatalf("expected ErrAmbiguousProfile, got %v", err)
		}

		ruleB.Priority = 1 // break the tie
		got, err := Resolve([]EligibilityRule{ruleA, ruleB}, facts, at)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got.ID != profileA.ID {
			t.Fatalf("expected the higher-priority rule to win, got profile %q", got.ID)
		}
	})

	t.Run("a profile version change applies from the next resolution, an already-resolved run is unaffected", func(t *testing.T) {
		tenant := valuesTenant("acme-co")
		facts := AssignmentFacts{TenantRef: tenant, Category: CategoryEmployee}

		v1 := baseProfile(t)
		v1.ID, v1.Version = "tp-emp", 1
		v1.EffectiveFrom = instant(t, "2026-01-01T00:00:00Z")
		v1.EffectiveTo = instant(t, "2026-04-01T00:00:00Z")

		v2 := baseProfile(t)
		v2.ID, v2.Version = "tp-emp", 2
		v2.Capture = CaptureDuration
		v2.Exemption, v2.PayBasis = Exempt, PaySalary
		v2.OvertimeMethod = OvertimeNone
		v2.EffectiveFrom = instant(t, "2026-04-01T00:00:00Z")

		rules := []EligibilityRule{
			{ID: "r-v1", TenantRef: tenant, Category: CategoryEmployee, Priority: 1, Profile: v1},
			{ID: "r-v2", TenantRef: tenant, Category: CategoryEmployee, Priority: 1, Profile: v2},
		}

		// A run whose session started before the boundary resolves once, at
		// its own start, and keeps that answer: it is never asked to
		// resolve again mid-session, so it can never observe v2.
		sessionStart := instant(t, "2026-03-15T00:00:00Z")
		pinned, err := Resolve(rules, facts, sessionStart)
		if err != nil {
			t.Fatalf("Resolve at session start: %v", err)
		}
		if pinned.Version != 1 {
			t.Fatalf("an open run must resolve to the version effective at its own start, got v%d", pinned.Version)
		}

		// The next period, resolved fresh, sees the new version.
		nextPeriodStart := instant(t, "2026-04-02T00:00:00Z")
		next, err := Resolve(rules, facts, nextPeriodStart)
		if err != nil {
			t.Fatalf("Resolve at next period start: %v", err)
		}
		if next.Version != 2 {
			t.Fatalf("the next period must resolve to the new version, got v%d", next.Version)
		}
	})

	t.Run("no matching rule leaves the assignment unable to record time", func(t *testing.T) {
		tenant := valuesTenant("acme-co")
		facts := AssignmentFacts{TenantRef: tenant, Category: CategoryContractor}
		rules := []EligibilityRule{{ID: "r-emp", TenantRef: tenant, Category: CategoryEmployee, Priority: 1, Profile: baseProfile(t)}}
		_, err := Resolve(rules, facts, instant(t, "2026-02-01T00:00:00Z"))
		if !errors.Is(err, ErrNoProfile) {
			t.Fatalf("expected ErrNoProfile, got %v", err)
		}
	})

	t.Run("CaptureNone outside contractor and agency has no time template", func(t *testing.T) {
		p := baseProfile(t)
		p.Capture = CaptureNone
		p.Exemption, p.PayBasis = Exempt, PaySalary
		_, err := TemplateFor(p)
		if !errors.Is(err, ErrNoTimeTemplate) {
			t.Fatalf("expected ErrNoTimeTemplate, got %v", err)
		}
	})
}

// TestTodo_WTIME_002_Property checks, over every declared capture mode and
// category pair that Validate accepts, that TemplateFor's category
// override and mode mapping agree with an independent reference mapping.
func TestTodo_WTIME_002_Property(t *testing.T) {
	categories := []WorkerCategory{CategoryEmployee, CategoryPlatform, CategoryContractor, CategoryAgencyTemp}
	captures := []CaptureMode{CapturePunch, CaptureDuration, CaptureException, CaptureNone}

	for _, category := range categories {
		for _, capture := range captures {
			p := baseProfile(t)
			p.Category = category
			p.Capture = capture
			switch category {
			case CategoryContractor:
				p.Destination, p.PayBasis, p.Exemption = DestinationInvoice, PayContract, NotApplicable
			case CategoryAgencyTemp:
				p.Destination, p.Exemption = DestinationAgency, NotApplicable
			}
			if capture == CaptureException || capture == CaptureNone {
				// Non-exempt is forbidden from these modes; move exempt
				// employees/platform workers into a mode Validate allows.
				if category == CategoryEmployee || category == CategoryPlatform {
					p.Exemption, p.PayBasis, p.OvertimeMethod = Exempt, PaySalary, OvertimeNone
				}
			}
			if err := p.Validate(); err != nil {
				t.Fatalf("category=%s capture=%s: fixture is invalid: %v", category, capture, err)
			}

			got, gotErr := TemplateFor(p)
			want, wantErr := referenceTemplateFor(category, capture)
			if wantErr && gotErr == nil {
				t.Fatalf("category=%s capture=%s: expected ErrNoTimeTemplate, got template %q", category, capture, got)
			}
			if !wantErr && gotErr != nil {
				t.Fatalf("category=%s capture=%s: expected template %q, got error %v", category, capture, want, gotErr)
			}
			if !wantErr && got != want {
				t.Fatalf("category=%s capture=%s: got template %q, want %q", category, capture, got, want)
			}
		}
	}
}

func referenceTemplateFor(category WorkerCategory, capture CaptureMode) (Template, bool) {
	switch category {
	case CategoryContractor:
		return TemplateContractorTime, false
	case CategoryAgencyTemp:
		return TemplateAgencyTime, false
	}
	switch capture {
	case CapturePunch:
		return TemplatePunchSession, false
	case CaptureDuration:
		return TemplateDurationSheet, false
	case CaptureException:
		return TemplateExceptionOnly, false
	default:
		return "", true
	}
}

// TestTodo_WTIME_002_Golden pins the resolved template for one profile per
// category so a change to the category/capture mapping shows as a diff.
func TestTodo_WTIME_002_Golden(t *testing.T) {
	cases := []struct {
		name    string
		profile TimeProfile
	}{
		{"employee_punch", baseProfile(t)},
		{"employee_exception", func() TimeProfile {
			p := baseProfile(t)
			p.Capture, p.Exemption, p.PayBasis, p.OvertimeMethod = CaptureException, Exempt, PaySalary, OvertimeNone
			return p
		}()},
		{"contractor", func() TimeProfile {
			p := baseProfile(t)
			p.Category, p.Destination, p.PayBasis, p.Exemption = CategoryContractor, DestinationInvoice, PayContract, NotApplicable
			return p
		}()},
		{"agency_temp", func() TimeProfile {
			p := baseProfile(t)
			p.Category, p.Destination, p.Exemption = CategoryAgencyTemp, DestinationAgency, NotApplicable
			return p
		}()},
	}
	var b strings.Builder
	for _, tc := range cases {
		tmpl, err := TemplateFor(tc.profile)
		if err != nil {
			fmt.Fprintf(&b, "%s: error=%v\n", tc.name, err)
			continue
		}
		fmt.Fprintf(&b, "%s: %s\n", tc.name, tmpl)
	}
	assertGolden(t, "template-resolution.txt", b.String())
}

// TestTodo_WTIME_002_Security proves a cross-tenant rule never resolves a
// profile for another tenant's assignment, and that a rule whose profile
// was forged to belong to a different tenant than the rule itself is never
// a candidate.
func TestTodo_WTIME_002_Security(t *testing.T) {
	t.Run("a rule scoped to another tenant never matches", func(t *testing.T) {
		other := valuesTenant("globex-inc")
		mine := valuesTenant("acme-co")
		facts := AssignmentFacts{TenantRef: mine, Category: CategoryEmployee}

		theirProfile := baseProfile(t)
		theirProfile.TenantRef = other
		theirRule := EligibilityRule{ID: "r-theirs", TenantRef: other, Category: CategoryEmployee, Priority: 100, Profile: theirProfile}

		myProfile := baseProfile(t)
		myProfile.TenantRef = mine
		myRule := EligibilityRule{ID: "r-mine", TenantRef: mine, Category: CategoryEmployee, Priority: 1, Profile: myProfile}

		got, err := Resolve([]EligibilityRule{theirRule, myRule}, facts, instant(t, "2026-02-01T00:00:00Z"))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got.TenantRef != mine {
			t.Fatalf("resolved a profile from tenant %q for a %q assignment", got.TenantRef, mine)
		}
	})

	t.Run("a rule whose profile tenant was forged to differ from the rule's own tenant is never a candidate", func(t *testing.T) {
		mine := valuesTenant("acme-co")
		other := valuesTenant("globex-inc")
		facts := AssignmentFacts{TenantRef: mine, Category: CategoryEmployee}

		forged := baseProfile(t)
		forged.TenantRef = other // the rule claims "mine" but hands out someone else's profile
		rule := EligibilityRule{ID: "r-forged", TenantRef: mine, Category: CategoryEmployee, Priority: 1, Profile: forged}

		_, err := Resolve([]EligibilityRule{rule}, facts, instant(t, "2026-02-01T00:00:00Z"))
		if !errors.Is(err, ErrNoProfile) {
			t.Fatalf("expected the forged rule to be excluded and ErrNoProfile returned, got %v", err)
		}
	})
}
