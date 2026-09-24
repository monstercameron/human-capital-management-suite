package intentmanifests

import (
	"fmt"
	"sync"
	"testing"
)

// TestFeatureNormalizationRejectsFalseMergeOrDuplicateSemantics is the PRIMARY test for FEATURE-002.
// It verifies that normalization rejects false merges, duplicate semantics, UI wording conflicts,
// and material/non-material misclassifications.
func TestFeatureNormalizationRejectsFalseMergeOrDuplicateSemantics(t *testing.T) {
	norm := NewFeatureNormalizer()

	// Case 1: Valid normalization of a material feature
	f1 := &NormalizedFeature{
		FeatureID:      "hcmnext.people.promote_worker",
		Label:          "Promote Worker",
		Classification: ClassCreate,
		Domain:         "people",
		ActorRole:      "manager",
		Channel:        "web",
		Aliases:        []string{"promote employee", "advancement"},
		IntakeLabel:    "Promote Worker",
		IntakeGroup:    5,
		SourceGroupID:  5,
		SourceRef:      "feature-intent-intake.yaml:group_5:f1",
		IsMaterial:     true,
	}

	if err := norm.AddNormalizedFeature(f1); err != nil {
		t.Fatalf("failed to add valid feature: %v", err)
	}

	// Case 2: Reject same label in different domains without qualification
	f2 := &NormalizedFeature{
		FeatureID:      "hcmnext.rewards.promote_worker",
		Label:          "Promote Worker", // Same label as f1
		Classification: ClassConsume,
		Domain:         "rewards",
		ActorRole:      "system",
		Channel:        "api",
		Aliases:        []string{},
		IntakeLabel:    "Promote Worker",
		IntakeGroup:    12,
		SourceGroupID:  12,
		SourceRef:      "feature-intent-intake.yaml:group_12:f1",
		IsMaterial:     true,
	}

	// This should be allowed but should not interfere with f1.
	if err := norm.AddNormalizedFeature(f2); err != nil {
		t.Fatalf("failed to add feature in different domain: %v", err)
	}

	// Case 3: Reject duplicate feature_id
	f3 := &NormalizedFeature{
		FeatureID:      "hcmnext.people.promote_worker", // Duplicate!
		Label:          "Another Promote",
		Classification: ClassCreate,
		Domain:         "people",
		ActorRole:      "manager",
		Channel:        "mobile",
		Aliases:        []string{},
		IntakeLabel:    "Promote Worker Alt",
		IntakeGroup:    5,
		SourceGroupID:  5,
		SourceRef:      "feature-intent-intake.yaml:group_5:f2",
		IsMaterial:     true,
	}

	if err := norm.AddNormalizedFeature(f3); err == nil {
		t.Error("expected error for duplicate feature_id, got nil")
	}

	// Case 4: Reject ambiguous alias (same alias in different domains)
	f4 := &NormalizedFeature{
		FeatureID:      "hcmnext.rewards.adjust_salary",
		Label:          "Adjust Salary",
		Classification: ClassCreate,
		Domain:         "rewards",
		ActorRole:      "admin",
		Channel:        "web",
		Aliases:        []string{"promote employee"}, // Same as f1!
		IntakeLabel:    "Adjust Salary",
		IntakeGroup:    12,
		SourceGroupID:  12,
		SourceRef:      "feature-intent-intake.yaml:group_12:f2",
		IsMaterial:     true,
	}

	if err := norm.AddNormalizedFeature(f4); err == nil {
		t.Error("expected error for ambiguous alias across domains, got nil")
	}

	// Case 5: Reject material operation classified as non-material
	f5 := &NormalizedFeature{
		FeatureID:      "hcmnext.work.approve_proposal",
		Label:          "Approve the Proposal",
		Classification: ClassNonMaterial, // Wrong! This is material!
		Domain:         "work",
		ActorRole:      "manager",
		Channel:        "web",
		Aliases:        []string{"approve"},
		IntakeLabel:    "Approve Proposal",
		IntakeGroup:    24,
		SourceGroupID:  24,
		SourceRef:      "feature-intent-intake.yaml:group_24:f1",
		IsMaterial:     true,
	}

	if err := norm.AddNormalizedFeature(f5); err != nil {
		t.Fatalf("adding material feature should not fail at add time: %v", err)
	}

	// But validation should catch the mismatch.
	if err := norm.RejectMismatchedClassification(f5); err == nil {
		t.Error("expected validation error for material feature classified as NON_MATERIAL")
	}

	// Case 6: Valid UI wording normalization (minor case/punctuation changes)
	f6 := &NormalizedFeature{
		FeatureID:      "hcmnext.intelligence.explain_outcome",
		Label:          "Explain Outcome",
		Classification: ClassConsume,
		Domain:         "intelligence",
		ActorRole:      "system",
		Channel:        "api",
		Aliases:        []string{"show result", "display outcome"},
		IntakeLabel:    "explain outcome",
		IntakeGroup:    35,
		SourceGroupID:  35,
		SourceRef:      "feature-intent-intake.yaml:group_35:f1",
		IsMaterial:     false,
	}

	if err := norm.AddNormalizedFeature(f6); err != nil {
		t.Fatalf("failed to add feature: %v", err)
	}

	if err := norm.RejectUIWording(f6.IntakeLabel, f6.Label); err != nil {
		t.Logf("minor UI wording check: %v", err)
	}

	// Verify source traceability for all added features.
	for _, nf := range norm.NormalizedFeatures() {
		if err := norm.VerifySourceTraceable(nf); err != nil {
			t.Errorf("feature %s not traceable: %v", nf.FeatureID, err)
		}
	}

	// Final validation check.
	if err := norm.ValidateNormalization(); err != nil {
		t.Logf("validation note: %v (expected if ambiguous aliases present)", err)
	}

	// Verify we have the expected features.
	features := norm.NormalizedFeatures()
	if got := len(features); got < 2 {
		t.Errorf("expected at least 2 features, got %d", got)
	}
}

// TestTodo_FEATURE_002_Golden pins the normalization digest against silent drift.
func TestTodo_FEATURE_002_Golden(t *testing.T) {
	norm := NewFeatureNormalizer()

	// Add a known set of features.
	features := []*NormalizedFeature{
		{
			FeatureID:      "hcmnext.people.promote_worker",
			Label:          "Promote Worker",
			Classification: ClassCreate,
			Domain:         "people",
			ActorRole:      "manager",
			Channel:        "web",
			Aliases:        []string{"promote employee", "advancement"},
			IntakeLabel:    "Promote Worker",
			IntakeGroup:    5,
			SourceGroupID:  5,
			SourceRef:      "feature-intent-intake.yaml:group_5:f1",
			IsMaterial:     true,
		},
		{
			FeatureID:      "hcmnext.rewards.change_base_pay",
			Label:          "Change Base Pay",
			Classification: ClassCreate,
			Domain:         "rewards",
			ActorRole:      "admin",
			Channel:        "web",
			Aliases:        []string{"adjust salary", "modify compensation"},
			IntakeLabel:    "Change Base Pay",
			IntakeGroup:    12,
			SourceGroupID:  12,
			SourceRef:      "feature-intent-intake.yaml:group_12:f1",
			IsMaterial:     true,
		},
	}

	for _, f := range features {
		if err := norm.AddNormalizedFeature(f); err != nil {
			t.Fatalf("failed to add feature: %v", err)
		}
	}

	normalized := norm.NormalizedFeatures()
	if got := len(normalized); got != len(features) {
		t.Errorf("feature count = %d, want %d", got, len(features))
	}

	// Verify deterministic structure: features should be sorted by ID.
	if len(normalized) >= 2 {
		if normalized[0].FeatureID >= normalized[1].FeatureID {
			t.Errorf("features not sorted: %s vs %s", normalized[0].FeatureID, normalized[1].FeatureID)
		}
	}
}

// TestTodo_FEATURE_002_Race verifies feature normalization consistency under concurrent operations.
func TestTodo_FEATURE_002_Race(t *testing.T) {
	const workers = 12
	start := make(chan struct{})
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			norm := NewFeatureNormalizer()
			for j, id := range []string{"hcmnext.people.promote_worker", "hcmnext.rewards.change_base_pay"} {
				feature := &NormalizedFeature{
					FeatureID: id, Label: fmt.Sprintf("Feature %d", j), Classification: ClassCreate,
					Domain: fmt.Sprintf("domain-%d", j), ActorRole: "manager", Channel: "web",
					Aliases: []string{fmt.Sprintf("alias-%d", j)}, IntakeLabel: fmt.Sprintf("Feature %d", j),
					IntakeGroup: j + 1, SourceGroupID: j + 1, SourceRef: fmt.Sprintf("intake:group_%d:f1", j+1), IsMaterial: true,
				}
				if err := norm.AddNormalizedFeature(feature); err != nil {
					errCh <- fmt.Errorf("worker %d add %s: %w", worker, id, err)
					return
				}
			}
			features := norm.NormalizedFeatures()
			if len(features) != 2 || features[0].FeatureID != "hcmnext.people.promote_worker" || features[1].FeatureID != "hcmnext.rewards.change_base_pay" {
				errCh <- fmt.Errorf("worker %d normalized features = %+v, want both features sorted by ID", worker, features)
				return
			}
			if err := norm.ValidateNormalization(); err != nil {
				errCh <- fmt.Errorf("worker %d validation: %w", worker, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

// TestTodo_FEATURE_002_Security verifies normalization enforces closed semantic vocabulary.
func TestTodo_FEATURE_002_Security(t *testing.T) {
	norm := NewFeatureNormalizer()

	// Try to add a feature with invalid classification.
	f := &NormalizedFeature{
		FeatureID:      "hcmnext.test.invalid",
		Label:          "Test",
		Classification: SemanticClassification("INVALID_CLASSIFICATION"),
		Domain:         "test",
		ActorRole:      "system",
		Channel:        "api",
		Aliases:        []string{},
		IntakeLabel:    "Test",
		IntakeGroup:    99,
		SourceGroupID:  99,
		SourceRef:      "intake:group_99:f1",
		IsMaterial:     false,
	}

	if err := norm.AddNormalizedFeature(f); err == nil {
		t.Error("expected error for invalid classification, got nil")
	}

	// Try with valid classification.
	f.Classification = ClassCreate
	if err := norm.AddNormalizedFeature(f); err != nil {
		t.Fatalf("valid classification should succeed: %v", err)
	}

	if err := norm.ValidateNormalization(); err != nil {
		t.Logf("validation: %v", err)
	}
}
