package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/logging"
)

func fixedClock(t time.Time) logging.Clock {
	return func() time.Time { return t }
}

func newTestLogger(buf *bytes.Buffer, opts ...logging.Option) *slog.Logger {
	h := logging.NewHandler(buf, opts...)
	return slog.New(h)
}

var rfc3339UTC = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)

func decodeLine(t *testing.T, line []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("decoding envelope line %q: %v", line, err)
	}
	return m
}

func lastLine(t *testing.T, buf *bytes.Buffer) []byte {
	t.Helper()
	lines := bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n"))
	if len(lines) == 0 {
		t.Fatalf("no output lines written")
	}
	return lines[len(lines)-1]
}

// TestStructuredLogEnvelopeReturnsExactVersionedAllowlistedRecord proves the
// RED and GREEN clauses of planning/todos.md OBS-009 for the scoped
// internal/platform/logging building block: a denied key is never leaked in
// its raw form, a caller cannot use an attribute to overwrite an envelope's
// own top-level fields or smuggle a raw identity in as a principal
// reference, and a well-formed record carries every required envelope
// field with a canonical, non-caller-controlled timestamp.
func TestStructuredLogEnvelopeReturnsExactVersionedAllowlistedRecord(t *testing.T) {
	fixed := time.Date(2026, 9, 3, 12, 34, 56, 0, time.UTC)

	t.Run("RED_denied_key_value_not_leaked", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newTestLogger(&buf, logging.WithClock(fixedClock(fixed)), logging.WithService("svc"))
		logger.Info("login attempt", "password", "hunter2", "safe", "ok")

		if strings.Contains(buf.String(), "hunter2") {
			t.Fatalf("raw secret value leaked into output: %s", buf.String())
		}
		env := decodeLine(t, lastLine(t, &buf))
		attrs, _ := env["attrs"].(map[string]any)
		if attrs["password"] != "[REDACTED]" {
			t.Fatalf("password attr = %v, want [REDACTED]", attrs["password"])
		}
		if attrs["safe"] != "ok" {
			t.Fatalf("safe attr = %v, want ok", attrs["safe"])
		}
	})

	t.Run("RED_attr_cannot_overwrite_envelope_field", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newTestLogger(&buf, logging.WithClock(fixedClock(fixed)), logging.WithService("svc"))
		logger.Info("normal message", "level", "HACKED", "schema_version", 999, "timestamp", "not-a-time")

		env := decodeLine(t, lastLine(t, &buf))
		if env["level"] != "INFO" {
			t.Fatalf("top-level level = %v, want INFO (caller attr must not overwrite it)", env["level"])
		}
		if v, ok := env["schema_version"].(float64); !ok || int(v) != logging.SchemaVersion {
			t.Fatalf("top-level schema_version = %v, want %d", env["schema_version"], logging.SchemaVersion)
		}
		if env["timestamp"] != fixed.UTC().Format(time.RFC3339) {
			t.Fatalf("top-level timestamp = %v, want canonical clock value", env["timestamp"])
		}
		attrs, _ := env["attrs"].(map[string]any)
		if attrs["level"] != "HACKED" {
			t.Fatalf("caller-supplied level attr should survive namespaced under attrs, got %v", attrs["level"])
		}
	})

	t.Run("RED_duplicate_keys_collapse_to_last_writer", func(t *testing.T) {
		var buf bytes.Buffer
		h := logging.NewHandler(&buf, logging.WithClock(fixedClock(fixed)), logging.WithService("svc"))
		derived := h.WithAttrs([]slog.Attr{slog.String("stage", "handler-level")})
		logger := slog.New(derived)
		logger.Info("msg", "stage", "record-level")

		env := decodeLine(t, lastLine(t, &buf))
		attrs, _ := env["attrs"].(map[string]any)
		if attrs["stage"] != "record-level" {
			t.Fatalf("duplicate key %q = %v, want the record-level value to win deterministically", "stage", attrs["stage"])
		}
	})

	t.Run("RED_principal_ref_rejects_raw_identity", func(t *testing.T) {
		ctx := context.Background()
		if _, err := logging.WithPrincipalRef(ctx, "person@example.com"); err == nil {
			t.Fatalf("expected WithPrincipalRef to reject an email-shaped raw identity")
		}
		if _, err := logging.WithPrincipalRef(ctx, ""); err == nil {
			t.Fatalf("expected WithPrincipalRef to reject an empty value")
		}
	})

	t.Run("GREEN_well_formed_record_has_required_fields", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newTestLogger(&buf, logging.WithClock(fixedClock(fixed)), logging.WithService("hcm-test"))

		ctx := context.Background()
		ctx = logging.WithRequestID(ctx, "req-1")
		ctx = logging.WithCorrelationID(ctx, "corr-1")
		ctx, err := logging.WithPrincipalRef(ctx, "principal:worker:42")
		if err != nil {
			t.Fatalf("WithPrincipalRef: %v", err)
		}
		ctx = logging.WithEvidenceIDs(ctx, "ev-1", "ev-2")

		logger.InfoContext(ctx, "worker updated")

		env := decodeLine(t, lastLine(t, &buf))
		if got, ok := env["schema_version"].(float64); !ok || int(got) != logging.SchemaVersion {
			t.Fatalf("schema_version = %v, want %d", env["schema_version"], logging.SchemaVersion)
		}
		ts, _ := env["timestamp"].(string)
		if !rfc3339UTC.MatchString(ts) {
			t.Fatalf("timestamp %q is not canonical RFC3339 UTC", ts)
		}
		if env["level"] != "INFO" {
			t.Fatalf("level = %v, want INFO", env["level"])
		}
		if env["service"] != "hcm-test" {
			t.Fatalf("service = %v, want hcm-test", env["service"])
		}
		if env["message"] != "worker updated" {
			t.Fatalf("message = %v", env["message"])
		}
		if env["request_id"] != "req-1" {
			t.Fatalf("request_id = %v", env["request_id"])
		}
		if env["correlation_id"] != "corr-1" {
			t.Fatalf("correlation_id = %v", env["correlation_id"])
		}
		if env["principal_ref"] != "principal:worker:42" {
			t.Fatalf("principal_ref = %v", env["principal_ref"])
		}
		ev, _ := env["evidence_ids"].([]any)
		if len(ev) != 2 || ev[0] != "ev-1" || ev[1] != "ev-2" {
			t.Fatalf("evidence_ids = %v", env["evidence_ids"])
		}
	})
}

// TestTodo_OBS_009_Golden proves byte-stable output for a fixed clock: the
// same inputs must always produce the exact same envelope bytes, including
// deterministic map key ordering and truncation markers.
func TestTodo_OBS_009_Golden(t *testing.T) {
	fixed := time.Date(2026, 9, 3, 12, 34, 56, 0, time.UTC)
	var buf bytes.Buffer
	logger := newTestLogger(&buf,
		logging.WithClock(fixedClock(fixed)),
		logging.WithService("hcm-test"),
		logging.WithMaxAttrValueLen(8),
	)

	ctx := context.Background()
	ctx = logging.WithRequestID(ctx, "req-1")
	ctx = logging.WithCorrelationID(ctx, "corr-1")
	ctx, err := logging.WithPrincipalRef(ctx, "principal:worker:42")
	if err != nil {
		t.Fatalf("WithPrincipalRef: %v", err)
	}
	ctx = logging.WithEvidenceIDs(ctx, "ev-1", "ev-2")

	logger.InfoContext(ctx, "user updated",
		"safe_key", "ok",
		"password", "hunter2",
		"long_field", "0123456789ABCDEF",
	)

	want := `{"schema_version":1,"timestamp":"2026-09-03T12:34:56Z","level":"INFO","service":"hcm-test","message":"user updated","request_id":"req-1","correlation_id":"corr-1","principal_ref":"principal:worker:42","evidence_ids":["ev-1","ev-2"],"attrs":{"long_field":"01234567…[TRUNCATED]","password":"[REDACTED]","safe_key":"ok"},"truncated":["attr:long_field"]}
`
	if got := buf.String(); got != want {
		t.Fatalf("golden mismatch:\n got:  %q\n want: %q", got, want)
	}
}

// TestTodo_OBS_009_Property checks, over many randomized attribute sets,
// that the emitted envelope always satisfies the cardinality and redaction
// invariants regardless of caller input.
func TestTodo_OBS_009_Property(t *testing.T) {
	const maxAttrs = 5
	const maxValLen = 6

	deniedKeys := []string{"password", "ssn", "custom_secret"}
	safeKeys := []string{"a", "b", "c", "d", "e", "f", "g", "h"}

	seed := uint64(0xC0FFEE)
	next := func() uint64 {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		return seed
	}

	for i := 0; i < 200; i++ {
		var buf bytes.Buffer
		logger := newTestLogger(&buf,
			logging.WithService("prop"),
			logging.WithMaxAttrs(maxAttrs),
			logging.WithMaxAttrValueLen(maxValLen),
			logging.WithDeniedKeys("custom_secret"),
		)

		attrCount := int(next()%10) + 1
		args := make([]any, 0, attrCount*2)
		for j := 0; j < attrCount; j++ {
			var key string
			if next()%4 == 0 {
				key = deniedKeys[next()%uint64(len(deniedKeys))]
			} else {
				key = safeKeys[next()%uint64(len(safeKeys))]
			}
			valLen := int(next()%20) + 1
			value := strings.Repeat("x", valLen)
			args = append(args, key, value)
		}

		logger.Info("property message", args...)

		env := decodeLine(t, lastLine(t, &buf))
		if got, ok := env["schema_version"].(float64); !ok || int(got) != logging.SchemaVersion {
			t.Fatalf("iteration %d: schema_version = %v", i, env["schema_version"])
		}
		attrs, _ := env["attrs"].(map[string]any)
		if len(attrs) > maxAttrs {
			t.Fatalf("iteration %d: attrs count %d exceeds cap %d", i, len(attrs), maxAttrs)
		}
		for k, v := range attrs {
			for _, dk := range deniedKeys {
				if strings.EqualFold(k, dk) && v != "[REDACTED]" {
					t.Fatalf("iteration %d: denied key %q not redacted: %v", i, k, v)
				}
			}
			if s, ok := v.(string); ok && s != "[REDACTED]" {
				if n := len([]rune(s)); n > maxValLen+len([]rune("…[TRUNCATED]")) {
					t.Fatalf("iteration %d: attr %q value %q exceeds bounded length", i, k, s)
				}
			}
		}
	}
}

// TestTodo_OBS_009_Race exercises the Handler concurrently from many
// goroutines, including handlers derived via WithAttrs/WithGroup, to prove
// there is no data race on the shared writer or on handler construction.
// Run with -race.
func TestTodo_OBS_009_Race(t *testing.T) {
	var buf bytes.Buffer
	base := logging.NewHandler(&buf, logging.WithService("race"))

	const goroutines = 16
	const perGoroutine = 25

	done := make(chan struct{})
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			h := base.WithAttrs([]slog.Attr{slog.Int("worker", id)}).WithGroup("g")
			logger := slog.New(h)
			ctx := logging.WithRequestID(context.Background(), "req")
			for i := 0; i < perGoroutine; i++ {
				logger.InfoContext(ctx, "concurrent", "i", i)
			}
		}(g)
	}
	for g := 0; g < goroutines; g++ {
		<-done
	}

	got := bytes.Count(buf.Bytes(), []byte("\n"))
	want := goroutines * perGoroutine
	if got != want {
		t.Fatalf("wrote %d lines, want %d", got, want)
	}
}

// TestTodo_OBS_009_Security proves the redaction denylist, principal
// reference guard, and cardinality caps hold under adversarial input.
func TestTodo_OBS_009_Security(t *testing.T) {
	t.Run("default_denied_keys_redacted", func(t *testing.T) {
		for _, key := range []string{"password", "ssn", "email", "token", "credit_card"} {
			var buf bytes.Buffer
			logger := newTestLogger(&buf, logging.WithService("sec"))
			logger.Info("event", key, "super-secret-value")
			if strings.Contains(buf.String(), "super-secret-value") {
				t.Fatalf("denied key %q leaked its value: %s", key, buf.String())
			}
		}
	})

	t.Run("principal_ref_guard", func(t *testing.T) {
		cases := []struct {
			name    string
			ref     string
			wantErr bool
		}{
			{"opaque_reference_ok", "principal:worker:abc123", false},
			{"raw_email_rejected", "worker@example.com", true},
			{"empty_rejected", "", true},
			{"embedded_space_rejected", "worker 123", true},
			{"oversized_rejected", strings.Repeat("a", 200), true},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				_, err := logging.WithPrincipalRef(context.Background(), c.ref)
				if (err != nil) != c.wantErr {
					t.Fatalf("WithPrincipalRef(%q) error = %v, wantErr %v", c.ref, err, c.wantErr)
				}
			})
		}
	})

	t.Run("attrs_cardinality_capped_with_marker", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newTestLogger(&buf, logging.WithService("sec"), logging.WithMaxAttrs(2))
		logger.Info("event", "a", 1, "b", 2, "c", 3, "d", 4, "e", 5)

		env := decodeLine(t, lastLine(t, &buf))
		attrs, _ := env["attrs"].(map[string]any)
		if len(attrs) != 2 {
			t.Fatalf("attrs count = %d, want 2", len(attrs))
		}
		truncated, _ := env["truncated"].([]any)
		found := false
		for _, m := range truncated {
			if s, ok := m.(string); ok && strings.HasPrefix(s, "attrs:") && strings.HasSuffix(s, "_dropped") {
				found = true
			}
		}
		if !found {
			t.Fatalf("truncated markers = %v, want an attrs dropped marker", truncated)
		}
	})

	t.Run("evidence_ids_capped_with_marker", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newTestLogger(&buf, logging.WithService("sec"), logging.WithMaxEvidenceIDs(2))
		ctx := logging.WithEvidenceIDs(context.Background(), "e1", "e2", "e3", "e4")
		logger.InfoContext(ctx, "event")

		env := decodeLine(t, lastLine(t, &buf))
		ev, _ := env["evidence_ids"].([]any)
		if len(ev) != 2 {
			t.Fatalf("evidence_ids count = %d, want 2", len(ev))
		}
	})
}

// TestTodo_OBS_009_Mutation checks that deriving a handler cannot weaken the
// base handler's privacy policy: denied values remain redacted in inherited
// attributes and per-record attributes, including case variants.
func TestTodo_OBS_009_Mutation(t *testing.T) {
	var buf bytes.Buffer
	base := logging.NewHandler(&buf, logging.WithService("mutation"), logging.WithDeniedKeys("custom_secret"))
	derived := base.WithAttrs([]slog.Attr{slog.String("PASSWORD", "inherited-secret")})
	slog.New(derived).Info("record", "CuStOm_SeCrEt", "record-secret", "safe", "visible")
	line := buf.String()
	for _, secret := range []string{"inherited-secret", "record-secret"} {
		if strings.Contains(line, secret) {
			t.Fatalf("derived handler leaked %q: %s", secret, line)
		}
	}
	env := decodeLine(t, lastLine(t, &buf))
	attrs, _ := env["attrs"].(map[string]any)
	if attrs["PASSWORD"] != "[REDACTED]" || attrs["CuStOm_SeCrEt"] != "[REDACTED]" || attrs["safe"] != "visible" {
		t.Fatalf("derived handler did not preserve redaction policy: %#v", attrs)
	}
}

// FuzzTodo_OBS_009 proves the handler never panics and always emits valid,
// versioned JSON for arbitrary message and attribute content.
func FuzzTodo_OBS_009(f *testing.F) {
	f.Add("hello", "key", "value")
	f.Add("", "", "")
	f.Add("msg with \"quotes\" and \n newline", "password", "secret")
	f.Add(strings.Repeat("m", 2000), "k", strings.Repeat("v", 2000))

	f.Fuzz(func(t *testing.T, msg, key, val string) {
		var buf bytes.Buffer
		logger := newTestLogger(&buf, logging.WithService("fuzz"))
		if key == "" {
			logger.Info(msg)
		} else {
			logger.Info(msg, key, val)
		}

		line := bytes.TrimRight(buf.Bytes(), "\n")
		var env map[string]any
		if err := json.Unmarshal(line, &env); err != nil {
			t.Fatalf("invalid JSON emitted for msg=%q key=%q val=%q: %v (%s)", msg, key, val, err, line)
		}
		if _, ok := env["schema_version"]; !ok {
			t.Fatalf("missing schema_version for msg=%q key=%q val=%q", msg, key, val)
		}
	})
}
