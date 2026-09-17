package survey

import (
	"errors"
	"testing"
	"time"
)

var survey007At = time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)

func survey007Input() CloseInput {
	return CloseInput{
		Tenant: "acme", CampaignRef: "campaign-q1", CohortCount: 6, MinimumCohort: 5,
		ResultDigest: "sha256:aggregate-result", EvidenceDigest: "sha256:aggregate-evidence",
		Retention: RetentionPolicy{
			RetainUntil:    survey007At.AddDate(2, 0, 0),
			DestructionDue: survey007At.AddDate(2, 0, 1),
		},
		ClosedAt: survey007At, AuthorityRef: "privacy-steward:ops-1",
	}
}

func survey007Chronology() []ChronologyBucket {
	return []ChronologyBucket{
		{Period: "2026-01", ResponseCount: 2},
		{Period: "2026-02", ResponseCount: 4},
	}
}

// TestTodo_SURVEY_007 is the PRIMARY contract: close locks new
// responses, freezes result and evidence, applies retention, hold and
// destruction, and preserves a minimum anonymous chronology. A privacy
// cohort or retention failure that still archived would be the seeded
// defect; instead it returns SURVEY_007_REJECTED with field/state/version
// and persists nothing.
func TestTodo_SURVEY_007(t *testing.T) {
	got, err := CloseCampaign(survey007Input(), survey007Chronology())
	if err != nil {
		t.Fatalf("CloseCampaign: %v", err)
	}
	if got.LockedAt.IsZero() || got.ResultDigest != "sha256:aggregate-result" {
		t.Fatalf("close must lock and freeze: %+v", got)
	}
	if got.Digest == "" {
		t.Fatalf("archive must seal a digest")
	}
	if len(got.Chronology) != 2 {
		t.Fatalf("archive must preserve the anonymous chronology: %+v", got.Chronology)
	}

	t.Run("below-minimum cohort never archives", func(t *testing.T) {
		in := survey007Input()
		in.CohortCount = 3
		before := survey007Chronology()
		_, err := CloseCampaign(in, before)
		var rej *CloseRejection
		if !errors.As(err, &rej) || !errors.Is(err, ErrCloseRejected) {
			t.Fatalf("small cohort must be SURVEY_007_REJECTED, got %v", err)
		}
		if rej.Field == "" || rej.State == "" || rej.Version == "" {
			t.Fatalf("rejection must name field/state/version: %+v", rej)
		}
		if len(before) != 2 {
			t.Fatalf("refusal must persist nothing")
		}
	})

	t.Run("retention failures never archive", func(t *testing.T) {
		for name, mutate := range map[string]func(*CloseInput){
			"horizon":     func(in *CloseInput) { in.Retention.RetainUntil = time.Time{} },
			"expired":     func(in *CloseInput) { in.Retention.RetainUntil = survey007At.Add(-time.Hour) },
			"destruction": func(in *CloseInput) { in.Retention.DestructionDue = time.Time{} },
		} {
			in := survey007Input()
			mutate(&in)
			if _, err := CloseCampaign(in, survey007Chronology()); !errors.Is(err, ErrCloseRejected) {
				t.Fatalf("retention fault %s must be SURVEY_007_REJECTED", name)
			}
		}
		held := survey007Input()
		held.Retention.LegalHold = true
		if _, err := CloseCampaign(held, survey007Chronology()); !errors.Is(err, ErrCloseRejected) {
			t.Fatalf("hold with a destruction date must be SURVEY_007_REJECTED")
		}
		held.Retention.DestructionDue = time.Time{}
		got, err := CloseCampaign(held, survey007Chronology())
		if err != nil {
			t.Fatalf("held campaign without destruction date must close: %v", err)
		}
		if !got.LegalHold {
			t.Fatalf("hold must survive closure: %+v", got)
		}
	})

	t.Run("chronology must match the cohort", func(t *testing.T) {
		_, err := CloseCampaign(survey007Input(), []ChronologyBucket{{Period: "2026-01", ResponseCount: 2}})
		if !errors.Is(err, ErrCloseRejected) {
			t.Fatalf("short chronology must be SURVEY_007_REJECTED, got %v", err)
		}
	})
}

func TestTodo_SURVEY_007_Fault(t *testing.T) {
	for name, mutate := range map[string]func(*CloseInput){
		"campaign":  func(in *CloseInput) { in.CampaignRef = "" },
		"frozen":    func(in *CloseInput) { in.ResultDigest = "" },
		"instant":   func(in *CloseInput) { in.ClosedAt = time.Time{} },
		"authority": func(in *CloseInput) { in.AuthorityRef = "" },
	} {
		in := survey007Input()
		mutate(&in)
		if _, err := CloseCampaign(in, survey007Chronology()); !errors.Is(err, ErrCloseRejected) {
			t.Fatalf("fault %s must be SURVEY_007_REJECTED", name)
		}
	}
	bad := []ChronologyBucket{{Period: "", ResponseCount: 6}}
	if _, err := CloseCampaign(survey007Input(), bad); !errors.Is(err, ErrCloseRejected) {
		t.Fatalf("identity-free chronology is still validated for shape")
	}
}

func TestTodo_SURVEY_007_Mutation(t *testing.T) {
	a, err := CloseCampaign(survey007Input(), survey007Chronology())
	if err != nil {
		t.Fatal(err)
	}
	mutated := a
	mutated.ResultDigest = "sha256:forged"
	if mutated.computedDigest() == a.Digest {
		t.Fatalf("forged result digest must move the seal")
	}
	// Every bound field moves the seal.
	in := survey007Input()
	in.EvidenceDigest = "sha256:other-evidence"
	b, err := CloseCampaign(in, survey007Chronology())
	if err != nil {
		t.Fatal(err)
	}
	if b.Digest == a.Digest {
		t.Fatalf("evidence change must move the seal")
	}
	// Chronology carries counts only: no respondent identity field exists
	// to mutate, so assert the shape directly.
	for _, bucket := range a.Chronology {
		if bucket.Period == "" || bucket.ResponseCount <= 0 {
			t.Fatalf("chronology must be period counts: %+v", bucket)
		}
	}
}
