package productdurability

import (
	"errors"
	"testing"
)

func align038Registry(t *testing.T) *ClassificationRegistry {
	t.Helper()
	registry := NewClassificationRegistry()
	for field, class := range map[string]Classification{
		"worker_number":       ClassificationInternal,
		"base_salary":         ClassificationConfidential,
		"bank_account_number": ClassificationRestricted,
		"display_name":        ClassificationPublic,
	} {
		if err := registry.Register(field, class); err != nil {
			t.Fatalf("Register(%s): %v", field, err)
		}
	}
	return registry
}

// TestTodo_ALIGN_038 proves storage classification and encryption policy:
// classified fields land only with their required protection, and anything
// unclassified or under-protected is refused.
func TestTodo_ALIGN_038(t *testing.T) {
	registry := align038Registry(t)
	stored := []StoredField{
		{Field: "worker_number", Classification: ClassificationInternal, AuditOnAccess: true},
		{Field: "base_salary", Classification: ClassificationConfidential, EncryptedAtRest: true, AuditOnAccess: true},
		{Field: "bank_account_number", Classification: ClassificationRestricted, EncryptedAtRest: true, AuditOnAccess: true},
		{Field: "display_name", Classification: ClassificationPublic},
	}
	if err := registry.Enforce(stored); err != nil {
		t.Fatalf("Enforce(compliant): %v", err)
	}
}

func TestTodo_ALIGN_038_Property(t *testing.T) {
	registry := NewClassificationRegistry()
	// Re-registering the same class is idempotent; changing it is refused
	// so policy cannot drift under stored data.
	if err := registry.Register("base_salary", ClassificationConfidential); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("base_salary", ClassificationConfidential); err != nil {
		t.Fatalf("idempotent re-register failed: %v", err)
	}
	if err := registry.Register("base_salary", ClassificationPublic); !errors.Is(err, ErrClassificationInvalid) {
		t.Fatalf("Register(drift) = %v, want ErrClassificationInvalid", err)
	}
}

func TestTodo_ALIGN_038_Golden(t *testing.T) {
	registry := align038Registry(t)
	const wantDigest = "sha256:4168ec98a80188646c5158e58b46595cb12306998628f81f108a582047536821"
	if got := registry.Digest(); got != wantDigest {
		t.Fatalf("registry digest=%q want=%q", got, wantDigest)
	}
}

func TestTodo_ALIGN_038_Security(t *testing.T) {
	registry := align038Registry(t)
	// Plaintext confidential storage is a breach.
	plaintext := []StoredField{{Field: "base_salary", Classification: ClassificationConfidential, AuditOnAccess: true}}
	if err := registry.Enforce(plaintext); !errors.Is(err, ErrClassificationBreach) {
		t.Fatalf("Enforce(plaintext confidential) = %v, want ErrClassificationBreach", err)
	}
	// Restricted fields without an access audit are a breach.
	unaudited := []StoredField{{Field: "bank_account_number", Classification: ClassificationRestricted, EncryptedAtRest: true}}
	if err := registry.Enforce(unaudited); !errors.Is(err, ErrClassificationBreach) {
		t.Fatalf("Enforce(unaudited restricted) = %v, want ErrClassificationBreach", err)
	}
	// Unclassified fields never land with a guessed policy.
	unknown := []StoredField{{Field: "shadow_column", Classification: ClassificationPublic}}
	if err := registry.Enforce(unknown); !errors.Is(err, ErrClassificationInvalid) {
		t.Fatalf("Enforce(unclassified) = %v, want ErrClassificationInvalid", err)
	}
	// A field stored under the wrong class is a breach, not a downgrade.
	misclassed := []StoredField{{Field: "base_salary", Classification: ClassificationPublic}}
	if err := registry.Enforce(misclassed); !errors.Is(err, ErrClassificationBreach) {
		t.Fatalf("Enforce(misclassed) = %v, want ErrClassificationBreach", err)
	}
}

func TestTodo_ALIGN_038_Integration(t *testing.T) {
	registry := align038Registry(t)
	// A compliant write lands; dropping its encryption afterwards is
	// caught on the next enforcement, not grandfathered in.
	stored := []StoredField{
		{Field: "base_salary", Classification: ClassificationConfidential, EncryptedAtRest: true, AuditOnAccess: true},
	}
	if err := registry.Enforce(stored); err != nil {
		t.Fatalf("Enforce(compliant): %v", err)
	}
	stored[0].EncryptedAtRest = false
	if err := registry.Enforce(stored); !errors.Is(err, ErrClassificationBreach) {
		t.Fatalf("Enforce(degraded) = %v, want ErrClassificationBreach", err)
	}
}

func TestTodo_ALIGN_038_Fault(t *testing.T) {
	registry := NewClassificationRegistry()
	// Unknown classifications and empty names never register.
	if err := registry.Register("x", Classification("topsecret")); !errors.Is(err, ErrClassificationInvalid) {
		t.Fatalf("Register(unknown class) = %v, want ErrClassificationInvalid", err)
	}
	if err := registry.Register("", ClassificationPublic); !errors.Is(err, ErrClassificationInvalid) {
		t.Fatalf("Register(empty) = %v, want ErrClassificationInvalid", err)
	}
	// Enforcing nothing is vacuously compliant.
	if err := registry.Enforce(nil); err != nil {
		t.Fatalf("Enforce(nil): %v", err)
	}
}

func TestTodo_ALIGN_038_Conformance(t *testing.T) {
	// The classification set is closed and total: every admitted class
	// resolves to a policy, and anything else is refused.
	for _, class := range []Classification{
		ClassificationPublic, ClassificationInternal,
		ClassificationConfidential, ClassificationRestricted,
	} {
		policy, err := PolicyFor(class)
		if err != nil {
			t.Fatalf("PolicyFor(%s): %v", class, err)
		}
		if class == ClassificationConfidential || class == ClassificationRestricted {
			if !policy.EncryptedAtRest || !policy.AuditOnAccess {
				t.Fatalf("PolicyFor(%s) = %+v, want encryption and audit", class, policy)
			}
		}
	}
	if _, err := PolicyFor(Classification("topsecret")); !errors.Is(err, ErrClassificationInvalid) {
		t.Fatalf("PolicyFor(topsecret) = %v, want ErrClassificationInvalid", err)
	}
}

func FuzzTodo_ALIGN_038_Fuzz(f *testing.F) {
	f.Add("base_salary", "confidential", true, true)
	f.Fuzz(func(t *testing.T, field, class string, encrypted, audited bool) {
		registry := NewClassificationRegistry()
		if err := registry.Register("base_salary", ClassificationConfidential); err != nil {
			t.Fatal(err)
		}
		stored := []StoredField{{
			Field: field, Classification: Classification(class),
			EncryptedAtRest: encrypted, AuditOnAccess: audited,
		}}
		first := registry.Enforce(stored)
		second := registry.Enforce(stored)
		if (first == nil) != (second == nil) {
			t.Fatalf("enforcement is not deterministic: %v vs %v", first, second)
		}
	})
}
