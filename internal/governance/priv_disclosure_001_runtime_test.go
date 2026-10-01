package governance

import (
	"errors"
	"testing"

	privacy "github.com/monstercameron/human-capital-management-suite/internal/governance/privacy"
)

func TestTodo_DISCLOSURE_001_Serving(t *testing.T) {
	runtime := Compose(baseReq()).PrivacyRuntime()
	budget := privacy.NewDisclosureBudget()
	policy := privacy.DisclosurePolicy{
		ID:                  "analytics",
		Version:             "1",
		MinCell:             5,
		Complementary:       true,
		Budget:              3,
		RepeatedQueryBudget: 2,
	}
	query := privacy.AnalyticsQuery{
		TenantID:   "tenant-a",
		Principal:  "analyst",
		Purpose:    "analytics",
		Dimensions: []string{"department"},
	}
	cells := []privacy.AnalyticsCell{
		{Key: "small", Dimension: "department", Value: 2},
		{Key: "large", Dimension: "department", Value: 8},
	}

	for i := 0; i < policy.RepeatedQueryBudget; i++ {
		result, err := runtime.DiscloseAnalytics(policy, query, cells, budget)
		if err != nil {
			t.Fatalf("serving disclosure attempt %d: %v", i+1, err)
		}
		if result.Evidence.QueryDigest != privacy.QueryDigest(query) {
			t.Fatalf("query digest = %q, want %q", result.Evidence.QueryDigest, privacy.QueryDigest(query))
		}
		if result.Evidence.Status != privacy.DisclosureSuppressed || len(result.Evidence.Suppressed) != 2 {
			t.Fatalf("disclosure evidence = %+v, want complementary suppression", result.Evidence)
		}
	}

	if _, err := runtime.DiscloseAnalytics(policy, query, cells, budget); !errors.Is(err, privacy.ErrBudgetExceeded) {
		t.Fatalf("third repeated query error = %v, want ErrBudgetExceeded", err)
	}

	otherTenant := query
	otherTenant.TenantID = "tenant-b"
	if _, err := runtime.DiscloseAnalytics(policy, otherTenant, cells, budget); err != nil {
		t.Fatalf("other tenant should have an independent budget: %v", err)
	}
}
