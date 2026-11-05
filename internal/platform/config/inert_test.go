package config

import (
	"errors"
	"math/rand"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestTodo_WF_DATA_042 is the PRIMARY test: parameter values stay inert
// data. Each consumer encodes for its context, oversized values are
// refused, and a tenant cannot grow keys past its budget.
func TestTodo_WF_DATA_042(t *testing.T) {
	t.Run("header rejects response splitting", func(t *testing.T) {
		if _, err := EncodeParameterValue("ok-value", UseHeader); err != nil {
			t.Fatalf("plain header value: %v", err)
		}
		for _, bad := range []string{"a\r\nInjected: yes", "a\nb", "a\rb"} {
			if _, err := EncodeParameterValue(bad, UseHeader); !errors.Is(err, ErrParameterValueUnsafe) {
				t.Fatalf("header value %q: got %v, want ErrParameterValueUnsafe", bad, err)
			}
		}
	})

	t.Run("query escapes and round-trips", func(t *testing.T) {
		encoded, err := EncodeParameterValue("a&b=c d/x?", UseQueryParam)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := url.ParseQuery("v=" + encoded)
		if err != nil || parsed.Get("v") != "a&b=c d/x?" {
			t.Fatalf("query round-trip = %q, %v", parsed.Get("v"), err)
		}
	})

	t.Run("address accepts only a bare addr-spec", func(t *testing.T) {
		if _, err := EncodeParameterValue("payroll@example.test", UseAddress); err != nil {
			t.Fatalf("bare address: %v", err)
		}
		for _, bad := range []string{"Payroll <payroll@example.test>", "a@b@c", "a b@c", "a@b c", "@example.test", "payroll@", "a\n@example.test"} {
			if _, err := EncodeParameterValue(bad, UseAddress); !errors.Is(err, ErrParameterValueUnsafe) {
				t.Fatalf("address %q: got %v, want ErrParameterValueUnsafe", bad, err)
			}
		}
	})

	t.Run("message neutralizes templates and markup", func(t *testing.T) {
		encoded, err := EncodeParameterValue(`{{.Password}} <script>alert(1)</script>`, UseMessage)
		if err != nil {
			t.Fatal(err)
		}
		for _, hostile := range []string{"{{", "}}", "<script>", "<", ">"} {
			if strings.Contains(encoded, hostile) {
				t.Fatalf("message encoding %q still contains %q", encoded, hostile)
			}
		}
	})

	t.Run("unknown context fails closed", func(t *testing.T) {
		if _, err := EncodeParameterValue("v", ValueUseContext("EVAL")); !errors.Is(err, ErrParameterValueUnsafe) {
			t.Fatalf("unknown context: got %v, want ErrParameterValueUnsafe", err)
		}
	})

	t.Run("oversized values are refused", func(t *testing.T) {
		limits := DefaultTenantValueLimits()
		big := strings.Repeat("x", limits.MaxValueBytes+1)
		if err := CheckParameterValueSize(big, limits); !errors.Is(err, ErrParameterValueTooLarge) {
			t.Fatalf("oversized value: got %v, want ErrParameterValueTooLarge", err)
		}
		store := NewParameterValueStore()
		snapshot := wfData037Snapshot(t, true)
		root := ParameterScopePath{{Kind: ScopeTenant, ID: "tenant-1"}}
		change := ParameterValueChange{
			Key: "payroll.currency", Scope: root[0], Environment: EnvironmentProduction,
			Value: big, ValueType: wfData037StringType, Author: "a", Reason: "r",
			RecordedAt: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC), Path: root,
		}
		if _, err := store.Append(snapshot, change); !errors.Is(err, ErrParameterValueTooLarge) {
			t.Fatalf("append oversized: got %v, want ErrParameterValueTooLarge", err)
		}
	})

	t.Run("tenant key budget caps new keys, not revisions", func(t *testing.T) {
		store := NewParameterValueStore()
		store.Limits = TenantValueLimits{MaxKeysPerTenant: 1, MaxValueBytes: 1024}
		definitions := []ParameterDefinition{
			{Key: "k.one", Type: wfData037StringType, Classification: "TENANT_OPERATIONAL", Owner: "o", AllowedConsumers: []Consumer{wfData037Consumer}},
			{Key: "k.two", Type: wfData037StringType, Classification: "TENANT_OPERATIONAL", Owner: "o", AllowedConsumers: []Consumer{wfData037Consumer}},
		}
		snapshot, err := NewSnapshotWithDefinitions("tenant-config", "v7", nil, definitions)
		if err != nil {
			t.Fatal(err)
		}
		root := ParameterScopePath{{Kind: ScopeTenant, ID: "tenant-1"}}
		at := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
		first := ParameterValueChange{Key: "k.one", Scope: root[0], Environment: EnvironmentProduction, Value: "v", ValueType: wfData037StringType, Author: "a", Reason: "r", RecordedAt: at, Path: root}
		if _, err := store.Append(snapshot, first); err != nil {
			t.Fatalf("first key: %v", err)
		}
		second := first
		second.Key, second.RecordedAt = "k.two", at.Add(time.Minute)
		if _, err := store.Append(snapshot, second); !errors.Is(err, ErrParameterKeyBudgetExceeded) {
			t.Fatalf("second key: got %v, want ErrParameterKeyBudgetExceeded", err)
		}
		revision := first
		revision.ExpectedRevision, revision.Value, revision.RecordedAt = 1, "v2", at.Add(2*time.Minute)
		if _, err := store.Append(snapshot, revision); err != nil {
			t.Fatalf("new revision of the budgeted key: %v", err)
		}
	})
}

// TestTodo_WF_DATA_042_Security proves hostile values cannot survive any
// consumer context: each is rejected or neutralized, never passed through.
func TestTodo_WF_DATA_042_Security(t *testing.T) {
	hostile := []string{
		"a\r\nBcc: evil@example.test",
		"{{.Secrets}}",
		"${jndi:ldap://evil}",
		"#{7*7}",
		"<img src=x onerror=alert(1)>",
		"'; DROP TABLE tenants; --",
		"a\x00b",
		"%0d%0aInjected: yes",
	}
	contexts := []ValueUseContext{UseHeader, UseAddress, UseMessage, UseQueryParam}
	for _, value := range hostile {
		for _, ctx := range contexts {
			encoded, err := EncodeParameterValue(value, ctx)
			if err != nil {
				if !errors.Is(err, ErrParameterValueUnsafe) {
					t.Fatalf("value %q in %s: got %v, want ErrParameterValueUnsafe", value, ctx, err)
				}
				continue
			}
			switch ctx {
			case UseHeader:
				if strings.ContainsAny(encoded, "\r\n") {
					t.Fatalf("header encoding %q kept a line break", encoded)
				}
			case UseMessage:
				for _, raw := range []string{"{{", "}}", "<", ">"} {
					if strings.Contains(encoded, raw) {
						t.Fatalf("message encoding %q kept %q", encoded, raw)
					}
				}
			case UseQueryParam:
				parsed, perr := url.ParseQuery("v=" + encoded)
				if perr != nil || parsed.Get("v") != value {
					t.Fatalf("query encoding %q does not round-trip to %q", encoded, value)
				}
			case UseAddress:
				if strings.ContainsAny(encoded, " \t\r\n<>\"") || strings.Count(encoded, "@") != 1 {
					t.Fatalf("address encoding %q is not a bare addr-spec", encoded)
				}
			}
		}
	}
}

// TestTodo_WF_DATA_042_Fuzz is the deterministic leg of the fuzz proof:
// generated hostile values never alter the structure they are placed in.
func TestTodo_WF_DATA_042_Fuzz(t *testing.T) {
	fuzzParameterValueEncoding(t, 3000, 20260402)
}

// FuzzParameterValueEncoding is the go fuzz entry for the same property.
func FuzzParameterValueEncoding(f *testing.F) {
	for _, seed := range []string{"", "a", "a\r\nb", "{{.X}}", "a&b", "a@b.c", "<x>", "%0d", "x\x00y", strings.Repeat("k", 70000)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		checkFuzzValue(t, value)
	})
}

func fuzzParameterValueEncoding(t *testing.T, trials int, seed int64) {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	const alphabet = "ab\r\n@<>{}$#%\"';&=/?:! \x00\t-_.+"
	for i := 0; i < trials; i++ {
		var b strings.Builder
		for n := rng.Intn(48); n > 0; n-- {
			b.WriteByte(alphabet[rng.Intn(len(alphabet))])
		}
		checkFuzzValue(t, b.String())
	}
}

func checkFuzzValue(t *testing.T, value string) {
	t.Helper()
	if encoded, err := EncodeParameterValue(value, UseHeader); err == nil && strings.ContainsAny(encoded, "\r\n") {
		t.Fatalf("header %q kept a line break", encoded)
	}
	if encoded, err := EncodeParameterValue(value, UseMessage); err == nil {
		for _, raw := range []string{"{{", "}}", "<", ">"} {
			if strings.Contains(encoded, raw) {
				t.Fatalf("message %q kept %q", encoded, raw)
			}
		}
	}
	if encoded, err := EncodeParameterValue(value, UseQueryParam); err != nil {
		t.Fatalf("query rejected %q: %v", value, err)
	} else if parsed, perr := url.ParseQuery("v=" + encoded); perr != nil || parsed.Get("v") != value {
		t.Fatalf("query %q does not round-trip to %q", encoded, value)
	}
	if encoded, err := EncodeParameterValue(value, UseAddress); err == nil {
		if strings.ContainsAny(encoded, " \t\r\n<>\"") || strings.Count(encoded, "@") != 1 {
			t.Fatalf("address %q is not a bare addr-spec", encoded)
		}
	}
}
