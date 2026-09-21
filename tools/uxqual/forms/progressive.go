package forms

// This file owns only the browser-transport part of progressive form
// submission. The server gives it an already-authorized field projection and
// server-issued hidden values; it renders a native POST form and normalizes
// either a normal browser POST or the map collected by the GWC/WASM client.
//
// It deliberately does not define another business request, authorize an
// actor, mint an idempotency key, dispatch an effect, or persist idempotency.
// Callers pass the returned values to the existing typed capability request
// (for Promotion, FromFormSubmission). Authentication, authorization, CSRF
// session binding, validation, and durable idempotency remain in their owning
// server layers.

import (
	"bytes"
	"crypto/subtle"
	"errors"
	"html/template"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/contract"
)

// Stable form-control names shared with the normal workspace POST. These are
// transport evidence, not trusted identity or authority.
const (
	ProgressiveCSRFField        = "csrf_token"
	ProgressiveIdempotencyField = "idempotency_key"
	ProgressiveLocaleField      = "locale"
	ProgressiveTransitionField  = "transition"
)

const (
	maxProgressiveBodyBytes  = 64 << 10
	maxProgressiveControls   = 128
	maxProgressiveHidden     = 32
	maxProgressiveIdentifier = 128
	maxProgressiveLocale     = 64
	maxProgressiveOpaque     = 512
	maxProgressiveValue      = 16 << 10
	maxProgressiveText       = 4 << 10
)

var (
	ErrInvalidForm        = errors.New("forms: invalid progressive form")
	ErrInvalidSubmission  = errors.New("forms: invalid progressive form submission")
	ErrSubmissionTooLarge = errors.New(
		"forms: progressive form submission exceeds the bounded input limit",
	)
)

// ProgressiveError is a sanitized, errors.Is-compatible refusal. Field is a
// fixed schema location selected by this package; it never contains a
// submitted name, value, URL, token, or parser diagnostic.
type ProgressiveError struct {
	kind  error
	Field string
}

func (e *ProgressiveError) Error() string { return e.kind.Error() + ": " + e.Field }
func (e *ProgressiveError) Unwrap() error { return e.kind }

func invalidForm(field string) error { return &ProgressiveError{kind: ErrInvalidForm, Field: field} }
func invalidSubmission(field string) error {
	return &ProgressiveError{kind: ErrInvalidSubmission, Field: field}
}
func oversizedSubmission() error {
	return &ProgressiveError{kind: ErrSubmissionTooLarge, Field: "submission.body"}
}

// ProgressiveForm is a transport binding around the existing, already-masked
// RequestField contract. Hidden values are server-issued expected values. In
// particular, a caller cannot select tenant, principal, role, purpose,
// capability, or other trusted context through this type.
//
// The idempotency key must identify one logical attempt and remain stable for
// refresh/back/repeated submission of that attempt. This package preserves and
// verifies the key; it never mints one or treats it as sufficient authority.
type ProgressiveForm struct {
	ID                string
	Action            string
	Method            string
	Locale            string
	FocusID           string
	Hidden            map[string]string
	Fields            []contract.RequestField
	SubmitLabel       string
	ErrorSummaryLabel string
}

// Validate checks the presentation/transport contract without inspecting or
// deriving any business authority.
func (f ProgressiveForm) Validate() error {
	if !validIdentifier(f.ID) {
		return invalidForm("form.id")
	}
	if f.Method != http.MethodPost {
		return invalidForm("form.method")
	}
	if !validRoute(f.Action) {
		return invalidForm("form.action")
	}
	if !validLocale(f.Locale) {
		return invalidForm("form.locale")
	}
	if !validRequiredText(f.SubmitLabel) {
		return invalidForm("form.submit_label")
	}
	if len(f.Hidden) > maxProgressiveHidden {
		return invalidForm("form.hidden")
	}
	if len(f.Fields) == 0 || len(f.Hidden)+len(f.Fields) > maxProgressiveControls {
		return invalidForm("form.fields")
	}

	for _, required := range []string{
		ProgressiveCSRFField,
		ProgressiveIdempotencyField,
		ProgressiveLocaleField,
		ProgressiveTransitionField,
	} {
		value, ok := f.Hidden[required]
		if !ok || !validOpaqueValue(value) {
			return invalidForm("form.hidden.required")
		}
	}
	if f.Hidden[ProgressiveLocaleField] != f.Locale {
		return invalidForm("form.hidden.locale")
	}

	controlNames := make(map[string]bool, len(f.Hidden)+len(f.Fields))
	for name, value := range f.Hidden {
		if !validIdentifier(name) || callerAuthorityField(name) {
			return invalidForm("form.hidden.name")
		}
		if controlNames[name] || !validControlValue(value) {
			return invalidForm("form.hidden")
		}
		controlNames[name] = true
	}

	// IDs include generated error and summary IDs so the rendered DOM cannot
	// contain an ambiguous label, focus target, or error relationship.
	domIDs := map[string]bool{
		f.ID:                   true,
		f.ID + "-errors":       true,
		f.ID + "-errors-title": true,
	}
	focusFound := f.FocusID == ""
	hasErrors := false
	totalText := len(f.SubmitLabel) + len(f.ErrorSummaryLabel)
	for _, field := range f.Fields {
		if !validIdentifier(field.ID) || callerAuthorityField(field.ID) {
			return invalidForm("form.fields.id")
		}
		if domIDs[field.ID] || domIDs[field.ID+"-label"] || domIDs[field.ID+"-error"] {
			return invalidForm("form.fields.id")
		}
		domIDs[field.ID] = true
		domIDs[field.ID+"-label"] = true
		domIDs[field.ID+"-error"] = true
		if !validRequiredText(field.Label) || !validControlValue(field.Value) || !validOptionalText(field.Validation.Message) {
			return invalidForm("form.fields.content")
		}
		totalText += len(field.Label) + len(field.Value) + len(field.Validation.Message)
		if totalText > maxProgressiveBodyBytes {
			return invalidForm("form.fields.content")
		}
		if !validFieldKind(field.Kind) {
			return invalidForm("form.fields.kind")
		}
		if field.Kind == contract.FieldKindReadOnly {
			if field.Validation.Required || field.Validation.Message != "" {
				return invalidForm("form.fields.readonly")
			}
			continue
		}
		if controlNames[field.ID] {
			return invalidForm("form.fields.name")
		}
		controlNames[field.ID] = true
		if field.Kind == contract.FieldKindDate && field.Value != "" && !canonicalDate(field.Value) {
			return invalidForm("form.fields.date")
		}
		if f.FocusID == field.ID {
			focusFound = true
		}
		if field.Validation.Message != "" {
			hasErrors = true
		}
	}
	if !focusFound {
		return invalidForm("form.focus_id")
	}
	if hasErrors && !validRequiredText(f.ErrorSummaryLabel) {
		return invalidForm("form.error_summary_label")
	}
	return nil
}

// NormalizeEnhanced normalizes the single-valued map collected by the real
// GWC form path. The returned map is a fresh copy. It is the same transport
// value map NormalizeHTTP returns, not a second business request schema.
func (f ProgressiveForm) NormalizeEnhanced(values map[string]string) (map[string]string, error) {
	if len(values) > maxProgressiveControls {
		return nil, oversizedSubmission()
	}
	multi := make(url.Values, len(values))
	for name, value := range values {
		multi[name] = []string{value}
	}
	return f.normalize(multi)
}

// NormalizeHTTP accepts exactly the native application/x-www-form-urlencoded
// POST emitted by NativeHTML. It reads through a hard byte limit and bounds
// the number of pairs before url.ParseQuery allocates a value map.
func (f ProgressiveForm) NormalizeHTTP(r *http.Request) (map[string]string, error) {
	if r == nil || r.Method != http.MethodPost || r.URL == nil || !routeMatches(f.Action, r.URL) {
		return nil, invalidSubmission("submission.route")
	}
	if r.Body == nil {
		return nil, invalidSubmission("submission.body")
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/x-www-form-urlencoded") {
		return nil, invalidSubmission("submission.content_type")
	}
	if r.ContentLength > maxProgressiveBodyBytes {
		return nil, oversizedSubmission()
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxProgressiveBodyBytes+1))
	if err != nil {
		return nil, invalidSubmission("submission.body")
	}
	if len(body) > maxProgressiveBodyBytes {
		return nil, oversizedSubmission()
	}
	if len(body) != 0 && bytes.Count(body, []byte{'&'})+1 > maxProgressiveControls {
		return nil, oversizedSubmission()
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, invalidSubmission("submission.encoding")
	}
	return f.normalize(values)
}

func (f ProgressiveForm) normalize(values url.Values) (map[string]string, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	if len(values) > maxProgressiveControls {
		return nil, oversizedSubmission()
	}

	allowed := make(map[string]bool, len(f.Hidden)+len(f.Fields))
	for name := range f.Hidden {
		allowed[name] = true
	}
	for _, field := range f.Fields {
		if field.Kind != contract.FieldKindReadOnly {
			allowed[field.ID] = true
		}
	}

	total := 0
	for name, got := range values {
		if !validIdentifier(name) || !allowed[name] || len(got) != 1 {
			return nil, invalidSubmission("submission.fields")
		}
		if !validControlValue(got[0]) {
			return nil, invalidSubmission("submission.values")
		}
		total += len(name) + len(got[0])
		if total > maxProgressiveBodyBytes {
			return nil, oversizedSubmission()
		}
	}

	out := make(map[string]string, len(allowed))
	for name, expected := range f.Hidden {
		got, ok := one(values, name)
		if !ok || !equalHidden(name, got, expected) {
			return nil, invalidSubmission("submission.hidden")
		}
		out[name] = expected
	}
	for _, field := range f.Fields {
		if field.Kind == contract.FieldKindReadOnly {
			continue
		}
		value, ok := one(values, field.ID)
		if !ok {
			return nil, invalidSubmission("submission.fields")
		}
		switch field.Kind {
		case contract.FieldKindTextarea:
			value = normalizeLineEndings(value)
		case contract.FieldKindDate:
			if value != "" && !canonicalDate(value) {
				return nil, invalidSubmission("submission.values")
			}
		default:
			if strings.ContainsAny(value, "\r\n") {
				return nil, invalidSubmission("submission.values")
			}
		}
		if field.Validation.Required && value == "" {
			return nil, invalidSubmission("submission.required")
		}
		out[field.ID] = value
	}
	return out, nil
}

func one(values url.Values, name string) (string, bool) {
	got, ok := values[name]
	if ok && len(got) == 1 {
		return got[0], true
	}
	return "", false
}

func equalHidden(name, got, expected string) bool {
	if name == ProgressiveCSRFField {
		return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
	}
	return got == expected
}

// NativeHTML renders a complete script-free semantic POST form. It uses the
// existing RequestField projection directly, so masking and field order are
// identical to the GWC contract and no second form-field schema exists.
func (f ProgressiveForm) NativeHTML() (string, error) {
	if err := f.Validate(); err != nil {
		return "", err
	}
	escape := template.HTMLEscapeString
	var b strings.Builder
	b.Grow(1024)
	b.WriteString(`<form accept-charset="UTF-8" method="post" action="` + escape(f.Action) + `" id="` + escape(f.ID) + `" lang="` + escape(f.Locale) + `" data-progressive-form="true" data-locale="` + escape(f.Locale) + `"`)
	if hasFieldErrors(f.Fields) {
		b.WriteString(` aria-describedby="` + escape(f.ID) + `-errors"`)
	}
	b.WriteString(`>`)

	keys := sortedKeys(f.Hidden)
	for _, key := range keys {
		b.WriteString(`<input type="hidden" name="` + escape(key) + `" value="` + escape(f.Hidden[key]) + `">`)
	}
	if hasFieldErrors(f.Fields) {
		b.WriteString(`<div id="` + escape(f.ID) + `-errors" class="form-error-summary" role="alert" aria-labelledby="` + escape(f.ID) + `-errors-title" tabindex="-1"><h2 id="` + escape(f.ID) + `-errors-title">` + escape(f.ErrorSummaryLabel) + `</h2><ul>`)
		for _, field := range f.Fields {
			if field.Validation.Message != "" {
				b.WriteString(`<li><a href="#` + escape(field.ID) + `">` + escape(field.Label) + `: ` + escape(field.Validation.Message) + `</a></li>`)
			}
		}
		b.WriteString(`</ul></div>`)
	}

	for _, field := range f.Fields {
		id := escape(field.ID)
		b.WriteString(`<div class="field">`)
		if field.Kind == contract.FieldKindReadOnly {
			b.WriteString(`<span id="` + id + `-label" class="field-label">` + escape(field.Label) + `</span><p id="` + id + `" class="field-static" aria-labelledby="` + id + `-label">` + escape(field.Value) + `</p>`)
			b.WriteString(`</div>`)
			continue
		}
		b.WriteString(`<label id="` + id + `-label" for="` + id + `">` + escape(field.Label))
		if field.Validation.Required {
			b.WriteString(`<span aria-hidden="true"> *</span>`)
		}
		b.WriteString(`</label>`)

		attrs := ` id="` + id + `" name="` + id + `"`
		if field.Validation.Required {
			attrs += ` required aria-required="true"`
		}
		if field.Validation.Message != "" {
			attrs += ` aria-invalid="true" aria-describedby="` + id + `-error"`
		}
		if f.FocusID == field.ID {
			attrs += ` data-hydration-focus="` + id + `"`
		}
		switch field.Kind {
		case contract.FieldKindTextarea:
			b.WriteString(`<textarea` + attrs + `>` + escape(normalizeLineEndings(field.Value)) + `</textarea>`)
		case contract.FieldKindDate:
			b.WriteString(`<input type="date"` + attrs + ` value="` + escape(field.Value) + `">`)
		default:
			b.WriteString(`<input type="text"` + attrs + ` value="` + escape(field.Value) + `">`)
		}
		if field.Validation.Message != "" {
			b.WriteString(`<p id="` + id + `-error" class="error" role="alert">` + escape(field.Validation.Message) + `</p>`)
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`<button type="submit">` + escape(f.SubmitLabel) + `</button></form>`)
	return b.String(), nil
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func hasFieldErrors(fields []contract.RequestField) bool {
	for _, field := range fields {
		if field.Validation.Message != "" {
			return true
		}
	}
	return false
}

func validIdentifier(value string) bool {
	if value == "" || len(value) > maxProgressiveIdentifier || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == ':' {
			continue
		}
		return false
	}
	return true
}

func validLocale(value string) bool {
	if value == "" || len(value) > maxProgressiveLocale || strings.TrimSpace(value) != value {
		return false
	}
	tag, ok := values.CanonicalLanguageTag(value)
	return ok && tag == value
}

func validRequiredText(value string) bool {
	return value != "" && strings.TrimSpace(value) != "" && validOptionalText(value)
}

func validOptionalText(value string) bool {
	return len(value) <= maxProgressiveText && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func validControlValue(value string) bool {
	return len(value) <= maxProgressiveValue && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func validOpaqueValue(value string) bool {
	if value == "" || len(value) > maxProgressiveOpaque || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func callerAuthorityField(name string) bool {
	var compact strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			compact.WriteRune(r)
		}
	}
	switch compact.String() {
	case "tenant", "tenantid", "principal", "principalid", "subject", "subjectid",
		"role", "roles", "authority", "authorityref", "authorityrefs",
		"delegation", "delegationref", "delegationrefs", "purpose", "assurance",
		"organizationscope", "organizationscopeid", "legalcontext", "placement",
		"processingplacement", "policyversion", "capability", "capabilityid", "effectscope":
		return true
	default:
		return strings.HasPrefix(compact.String(), "xhcm")
	}
}

func validRoute(action string) bool {
	if action == "" || len(action) > maxProgressiveIdentifier*4 || !utf8.ValidString(action) || !strings.HasPrefix(action, "/") || strings.HasPrefix(action, "//") || strings.ContainsAny(action, "\\\r\n\t") {
		return false
	}
	u, err := url.Parse(action)
	return err == nil && u.Path != "" && u.Fragment == "" && u.Scheme == "" && u.Host == "" && u.User == nil && u.Opaque == ""
}

func routeMatches(action string, got *url.URL) bool {
	want, err := url.Parse(action)
	return err == nil && got != nil && got.EscapedPath() == want.EscapedPath() && got.RawQuery == want.RawQuery
}

func validFieldKind(kind contract.FieldKind) bool {
	switch kind {
	case contract.FieldKindText, contract.FieldKindTextarea, contract.FieldKindDate,
		contract.FieldKindMoney, contract.FieldKindLookup, contract.FieldKindReadOnly:
		return true
	default:
		return false
	}
}

func canonicalDate(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func normalizeLineEndings(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}
