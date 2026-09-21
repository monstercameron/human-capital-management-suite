package productui

import (
	"strings"
	"testing"
)

// TestTodo_REV_091_01 proves the live proposal form's refusals are mapped by
// this package's refusal mapper: every wire field path the proposal write
// paths emit lands on one canonical control with a catalog message, typed
// rule references pick their exact corrections regardless of order, and no
// message in any locale carries a field path, rule reference or amount the
// server did not validate.
func TestTodo_REV_091_01(t *testing.T) {
	paths := map[string]PromotionFormField{
		"subject_worker_ref": PromotionFieldWorker, "worker_ref": PromotionFieldWorker,
		"desired_job_code": PromotionFieldJobCode, "target_job_code": PromotionFieldJobCode, "job_code": PromotionFieldJobCode,
		"desired_grade": PromotionFieldGrade, "target_grade": PromotionFieldGrade, "grade": PromotionFieldGrade,
		"desired_position_id": PromotionFieldPosition, "target_position_id": PromotionFieldPosition, "position_id": PromotionFieldPosition,
		"desired_base_pay": PromotionFieldBase, "proposed_base": PromotionFieldBase, "proposed_base_pay": PromotionFieldBase, "base_pay": PromotionFieldBase,
		"effective_date": PromotionFieldEffective,
		"reason":         PromotionFieldReason, "business_reason": PromotionFieldReason,
	}
	for path, want := range paths {
		field, key := PromotionRefusalField(path)
		if field != want || key == "" {
			t.Fatalf("PromotionRefusalField(%q) = %q/%q, want %q with a key", path, field, key, want)
		}
	}
	for _, unknown := range []string{"", "(request)", "idempotency_key", "internal_sql_column", "body"} {
		if field, key := PromotionRefusalField(unknown); field != "" || key != "" {
			t.Fatalf("unknown path %q mapped to %q/%q", unknown, field, key)
		}
	}

	exact := PromotionRefusalViolation{FieldPath: "proposed_base", RuleRef: "promotion.base_pay.not_exact"}
	ranged := PromotionRefusalViolation{FieldPath: "desired_base_pay", RuleRef: "promotion.ladder.base_increase_out_of_range", Minimum: "105.04", Maximum: "115.03", Currency: "USD"}
	generic := PromotionRefusalViolation{FieldPath: "base_pay", RuleRef: "journey.input.invalid"}
	for _, order := range [][]PromotionRefusalViolation{{generic, exact, ranged}, {ranged, generic, exact}, {exact, ranged, generic}} {
		got := MapPromotionWireRefusal(order)[PromotionFieldBase]
		if got.Key != "journey.field_base_range_error" || got.PayRange == nil {
			t.Fatalf("order %v lost the exact range correction: %+v", order, got)
		}
		if msg := got.Message(ResolveProductLocale("en-US")); msg != "Enter an amount from USD 105.04 to USD 115.03, inclusive." {
			t.Fatalf("range message = %q", msg)
		}
	}
	if got := MapPromotionWireRefusal([]PromotionRefusalViolation{generic, exact})[PromotionFieldBase]; got.Key != "journey.field_base_exact_error" {
		t.Fatalf("not-exact refusal lost to the generic one: %+v", got)
	}
	// A token-shaped reason refusal (REV-095-02) lands on the reason control.
	reason := MapPromotionWireRefusal([]PromotionRefusalViolation{{FieldPath: "business_reason", RuleRef: "journey.input.reason_not_prose"}})
	if got := reason[PromotionFieldReason]; got.Key != "journey.field_reason_error" {
		t.Fatalf("reason refusal = %+v", got)
	}

	// Incoherent or inexact ranges are never quoted.
	for _, bad := range []PromotionRefusalViolation{
		{FieldPath: "proposed_base", RuleRef: "promotion.ladder.base_increase_out_of_range", Minimum: "115.03", Maximum: "105.04", Currency: "USD"},
		{FieldPath: "proposed_base", RuleRef: "promotion.ladder.base_increase_out_of_range", Minimum: "0", Maximum: "105.04", Currency: "USD"},
		{FieldPath: "proposed_base", RuleRef: "promotion.ladder.base_increase_out_of_range", Minimum: "105.041", Maximum: "115.03", Currency: "USD"},
		{FieldPath: "proposed_base", RuleRef: "promotion.ladder.base_increase_out_of_range", Minimum: "105.04", Maximum: "115.03"},
	} {
		got := MapPromotionWireRefusal([]PromotionRefusalViolation{bad})[PromotionFieldBase]
		if got.PayRange != nil || got.Key != "journey.field_base_error" {
			t.Fatalf("bad range %+v was quoted: %+v", bad, got)
		}
	}
	if MapPromotionWireRefusal(nil) != nil || MapPromotionWireRefusal([]PromotionRefusalViolation{{FieldPath: "secret_column"}}) != nil {
		t.Fatal("no known field must map to no corrections")
	}

	// Leak test, ported from PROMOUX-007: no localized message names a wire
	// path, rule reference or catalog key.
	var all []PromotionRefusalViolation
	for path := range paths {
		all = append(all, PromotionRefusalViolation{FieldPath: path, RuleRef: "promotion.ladder.base_increase_out_of_range", Minimum: "105.04", Maximum: "115.03", Currency: "USD"})
	}
	for _, tag := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(tag)
		for field, correction := range MapPromotionWireRefusal(all) {
			msg := correction.Message(locale)
			if strings.TrimSpace(msg) == "" || msg == correction.Key {
				t.Fatalf("%s %s has no localized message (%q)", tag, field, msg)
			}
			for _, leak := range []string{"promotion.base_pay", "promotion.ladder", "journey.field", "journey.input", "_ref", "desired_", "business_reason"} {
				if strings.Contains(msg, leak) {
					t.Fatalf("%s %s message %q leaks %q", tag, field, msg, leak)
				}
			}
		}
	}
}
