package logging

// defaultDeniedKeys returns the built-in set of attribute keys (matched
// case-insensitively) whose values are always replaced with redactedValue.
// It is a narrow, name-based default for this scoped platform logger, not
// the owned content classifier the structured-logging spec assigns to
// internal/platform/telemetry (which classifies by data shape and
// business meaning, not just key name). Callers add to this set with
// WithDeniedKeys.
func defaultDeniedKeys() map[string]struct{} {
	keys := []string{
		"password",
		"passwd",
		"secret",
		"token",
		"access_token",
		"refresh_token",
		"api_key",
		"authorization",
		"ssn",
		"social_security_number",
		"bank_account",
		"bank_account_number",
		"bank_routing",
		"routing_number",
		"credit_card",
		"card_number",
		"cvv",
		"medical",
		"medical_record",
		"diagnosis",
		"prompt",
		"raw_identity",
		"email",
		"email_address",
		"phone",
		"phone_number",
		"date_of_birth",
		"dob",
		"salary",
		"compensation",
		"base_pay",
	}
	set := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		set[k] = struct{}{}
	}
	return set
}
