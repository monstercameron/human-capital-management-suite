package telemetry_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
)

func testEgressSanitizer(t *testing.T) *telemetry.BaggageSanitizer {
	t.Helper()
	allow := testAllowlist(t)
	eval := telemetry.NewEvaluator(allow, telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion), telemetry.DefaultSamplingPolicy())
	return telemetry.NewBaggageSanitizer(allow, eval)
}

func TestTelemetryBaggageAllowlistStripsSensitiveUntrustedAndThirdPartyFields(t *testing.T) {
	s := testEgressSanitizer(t)
	t.Run("RED_sensitive_keys_stripped", func(t *testing.T) {
		for _, k := range []string{"worker_id", "person_email", "medical_diagnosis", "pay_amount", "bank_routing", "free_text", "attacker_key", "case_notes", "prompt_text"} {
			bag := map[string]string{k: "evil", "correlation_id": "corr-1"}
			res := s.Sanitize(bag)
			if _, ok := res.Kept[k]; ok {
				t.Fatalf("sensitive key %q kept", k)
			}
			if res.Stripped == 0 {
				t.Fatalf("sensitive key %q not counted as stripped", k)
			}
			if len(res.Signals) == 0 || res.Signals[0].Code != telemetry.SecuritySignalCode {
				t.Fatalf("no bounded security signal for %q", k)
			}
			if res.Signals[0].Bounded == false {
				t.Fatal("signal not bounded")
			}
		}
	})
	t.Run("RED_authority_bearing_keys_stripped", func(t *testing.T) {
		for _, k := range []string{"authz_role", "authorization", "tenant_context", "tenant_id", "routing_hint", "principal_id", "session_token", "scope_admin"} {
			bag := map[string]string{k: "evil", "correlation_id": "corr-1"}
			res := s.Sanitize(bag)
			if _, ok := res.Kept[k]; ok {
				t.Fatalf("authority key %q kept", k)
			}
		}
	})
	t.Run("RED_baggage_never_authority_input", func(t *testing.T) {
		bag := map[string]string{"authz_role": "admin", "tenant_context": "tenant-evil", "correlation_id": "corr-1"}
		res := s.Sanitize(bag)
		for k := range res.Kept {
			if telemetry.IsAuthorityBearingBaggageKey(k) {
				t.Fatalf("authority bearing key %q survived sanitization", k)
			}
		}
	})
	t.Run("RED_third_party_egress_strips_all_but_correlation", func(t *testing.T) {
		bag := map[string]string{"correlation_id": "corr-1", "request_id": "req-1", "worker_id": "w1", "pay_amount": "100"}
		res := s.SanitizeForThirdParty(bag)
		if _, ok := res.Kept["worker_id"]; ok {
			t.Fatal("third party kept sensitive")
		}
		if _, ok := res.Kept["request_id"]; ok {
			t.Fatal("third party kept request_id, want only correlation_id")
		}
		if _, ok := res.Kept["correlation_id"]; !ok {
			t.Fatal("third party stripped correlation_id")
		}
	})
	t.Run("RED_high_cardinality_bounded", func(t *testing.T) {
		bag := make(map[string]string)
		for i := 0; i < telemetry.MaxBaggageEntries+5; i++ {
			bag["correlation_id"] = "corr-1"
			_ = bag
		}
		many := map[string]string{}
		for i := 0; i < telemetry.MaxBaggageEntries+3; i++ {
			k := "correlation_id"
			if i > 0 {
				k = strings.Repeat("x", i) + "correlation_id_extra"
			}
			many[k] = "v"
		}
		res := s.Sanitize(many)
		if len(res.Kept) > telemetry.MaxBaggageEntries {
			t.Fatalf("kept %d exceeds max %d", len(res.Kept), telemetry.MaxBaggageEntries)
		}
	})
	t.Run("RED_redaction_failure_not_silent", func(t *testing.T) {
		bag := map[string]string{"worker_id": "leak", "correlation_id": "corr-1"}
		res := s.Sanitize(bag)
		if len(res.Signals) == 0 {
			t.Fatal("no signal on redaction")
		}
		for _, sig := range res.Signals {
			if sig.Code == "" || sig.Reason == "" {
				t.Fatal("signal missing code/reason")
			}
			if strings.Contains(sig.Reason, "leak") || strings.Contains(sig.Reason, "worker") {
				t.Fatal("signal contains payload")
			}
		}
	})
	t.Run("GREEN_allowlisted_correlation_survives", func(t *testing.T) {
		bag := map[string]string{"correlation_id": "corr-42", "request_id": "req-7"}
		res := s.Sanitize(bag)
		if res.Kept["correlation_id"] != "corr-42" || res.Kept["request_id"] != "req-7" {
			t.Fatalf("allowlisted kept %+v", res.Kept)
		}
	})
	t.Run("GREEN_collector_redaction_defense_in_depth", func(t *testing.T) {
		attrs := map[string]string{"cell_id": "cell-p1a", "salary": "100000", "outcome": "SUCCESS"}
		out := s.CollectorRedact(attrs, telemetry.SignalLog)
		if _, ok := out["salary"]; ok {
			t.Fatal("collector kept prohibited salary")
		}
		if out["cell_id"] != "cell-p1a" {
			t.Fatalf("collector dropped allowlisted cell_id %+v", out)
		}
	})
	t.Run("GREEN_leak_signal_bounded_without_payload", func(t *testing.T) {
		bag := map[string]string{"bank_routing": "12345678901234567890", "correlation_id": "corr-1"}
		res := s.Sanitize(bag)
		if len(res.Signals) == 0 {
			t.Fatal("no signal")
		}
		sig := res.Signals[0]
		if sig.Code != telemetry.SecuritySignalCode || !sig.Bounded {
			t.Fatalf("signal %+v", sig)
		}
	})
}

func TestTodo_OBS_022(t *testing.T) {
	s := testEgressSanitizer(t)
	input := map[string]string{"correlation_id": "corr-safe", "request_id": "req-safe", "worker_id": "person-42", "authz_role": "admin", "vendor_trace": "opaque"}
	result := s.Sanitize(input)
	if result.Kept["correlation_id"] != "corr-safe" || result.Kept["request_id"] != "req-safe" {
		t.Fatalf("safe correlation fields were lost: %#v", result.Kept)
	}
	for _, key := range []string{"worker_id", "authz_role", "vendor_trace"} {
		if _, ok := result.Kept[key]; ok {
			t.Fatalf("untrusted field %q survived: %#v", key, result.Kept)
		}
	}
	thirdParty := s.SanitizeForThirdParty(result.Kept)
	if len(thirdParty.Kept) != 1 || thirdParty.Kept["correlation_id"] != "corr-safe" {
		t.Fatalf("third-party allowlist = %#v, want correlation ID only", thirdParty.Kept)
	}
}

func TestTodo_OBS_022_Property(t *testing.T) {
	s := testEgressSanitizer(t)
	for _, k := range []string{"worker_id", "person", "email", "medical", "pay", "bank", "free_text", "attacker"} {
		if !telemetry.IsSensitiveBaggageKey(k + "_suffix") {
			t.Fatalf("key %q not classified sensitive", k)
		}
	}
	for _, k := range []string{"correlation_id", "request_id"} {
		if !telemetry.IsAllowedBaggageKey(k) {
			t.Fatalf("allowlisted %q not allowed", k)
		}
	}
	_ = s
}

func FuzzTodo_OBS_022(f *testing.F) {
	f.Add("worker_id", "evil")
	f.Add("correlation_id", "corr-1")
	f.Add("authz_role", "admin")
	f.Fuzz(func(t *testing.T, k, v string) {
		s := testEgressSanitizer(t)
		bag := map[string]string{k: v, "correlation_id": "corr-1"}
		res := s.Sanitize(bag)
		for kept := range res.Kept {
			if telemetry.IsSensitiveBaggageKey(kept) || telemetry.IsAuthorityBearingBaggageKey(kept) {
				t.Fatalf("fuzz kept sensitive/authority %q", kept)
			}
		}
		res2 := s.SanitizeForThirdParty(bag)
		for kept := range res2.Kept {
			if !telemetry.IsThirdPartySafeBaggageKey(kept) {
				t.Fatalf("third party kept unsafe %q", kept)
			}
		}
	})
}

func TestTodo_OBS_022_Integration(t *testing.T) {
	s := testEgressSanitizer(t)
	bag := map[string]string{"correlation_id": "corr-int-1", "request_id": "req-int-1", "worker_id": "leak", "tenant_id": "t1"}
	sanitized := s.Sanitize(bag)
	if _, ok := sanitized.Kept["worker_id"]; ok {
		t.Fatal("integration kept worker_id")
	}
	tp := s.SanitizeForThirdParty(sanitized.Kept)
	if len(tp.Kept) != 1 || tp.Kept["correlation_id"] != "corr-int-1" {
		t.Fatalf("third party integration %+v", tp.Kept)
	}
	headers := map[string]string{"Authorization": "Bearer token", "baggage": "worker_id=leak,correlation_id=corr-1", "Content-Type": "application/json"}
	stripped := s.StripHeadersForThirdParty(headers)
	if _, ok := stripped["baggage"]; ok {
		t.Fatal("baggage header not stripped for third party")
	}
	if stripped["Authorization"] != "Bearer token" {
		t.Fatal("non-baggage header stripped")
	}
	attrs := map[string]string{"cell_id": "cell-p1a", "worker_id": "w1"}
	redacted := s.CollectorRedact(attrs, telemetry.SignalLog)
	if _, ok := redacted["worker_id"]; ok {
		t.Fatal("collector kept worker_id")
	}
}

func TestTodo_OBS_022_Security(t *testing.T) {
	s := testEgressSanitizer(t)
	t.Run("baggage_not_authority", func(t *testing.T) {
		bag := map[string]string{"correlation_id": "corr-1", "tenant_context": "evil", "authz_role": "admin"}
		res := s.Sanitize(bag)
		for k := range res.Kept {
			if strings.Contains(strings.ToLower(k), "tenant") || strings.Contains(strings.ToLower(k), "authz") {
				t.Fatalf("authority baggage survived %q", k)
			}
		}
	})
	t.Run("oversized_baggage_stripped", func(t *testing.T) {
		bag := map[string]string{"correlation_id": strings.Repeat("a", telemetry.MaxBaggageValueLen+1)}
		res := s.Sanitize(bag)
		if _, ok := res.Kept["correlation_id"]; ok {
			t.Fatal("oversized value kept")
		}
	})
	t.Run("signal_has_no_payload", func(t *testing.T) {
		bag := map[string]string{"bank_routing": "secret-12345"}
		res := s.Sanitize(bag)
		for _, sig := range res.Signals {
			if strings.Contains(sig.Reason, "secret") {
				t.Fatal("signal leaked payload")
			}
		}
	})
}

func TestTodo_OBS_022_Conformance(t *testing.T) {
	s := testEgressSanitizer(t)
	cases := []struct {
		bag      map[string]string
		keepKeys []string
	}{
		{map[string]string{"correlation_id": "c1"}, []string{"correlation_id"}},
		{map[string]string{"correlation_id": "c1", "request_id": "r1"}, []string{"correlation_id", "request_id"}},
		{map[string]string{"correlation_id": "c1", "worker_id": "w1"}, []string{"correlation_id"}},
	}
	for _, tc := range cases {
		res := s.Sanitize(tc.bag)
		if len(res.Kept) != len(tc.keepKeys) {
			t.Fatalf("case %+v kept %+v want %v", tc.bag, res.Kept, tc.keepKeys)
		}
	}
	tpCases := []struct {
		bag      map[string]string
		keepKeys []string
	}{
		{map[string]string{"correlation_id": "c1", "request_id": "r1"}, []string{"correlation_id"}},
		{map[string]string{"correlation_id": "c1"}, []string{"correlation_id"}},
	}
	for _, tc := range tpCases {
		res := s.SanitizeForThirdParty(tc.bag)
		if len(res.Kept) != len(tc.keepKeys) {
			t.Fatalf("third party case %+v kept %+v", tc.bag, res.Kept)
		}
	}
}

func BenchmarkTodo_OBS_022(b *testing.B) {
	s := telemetry.NewBaggageSanitizer(nil, nil)
	allow, _ := telemetry.DefaultAllowlist()
	eval := telemetry.NewEvaluator(allow, telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion), telemetry.DefaultSamplingPolicy())
	s2 := telemetry.NewBaggageSanitizer(allow, eval)
	_ = s
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bag := map[string]string{"correlation_id": "corr-1", "request_id": "req-1", "worker_id": "leak"}
		_ = s2.Sanitize(bag)
		_ = s2.SanitizeForThirdParty(bag)
	}
}
