package config

import (
	"errors"
	"fmt"
	"html"
	"net/url"
	"strings"
)

var (
	// ErrParameterValueUnsafe refuses a value a consumer context cannot
	// carry inertly.
	ErrParameterValueUnsafe = errors.New("config: parameter value is unsafe for its consumer context")
	// ErrParameterValueTooLarge refuses a value past the tenant size budget.
	ErrParameterValueTooLarge = errors.New("config: parameter value exceeds the tenant size budget")
	// ErrParameterKeyBudgetExceeded refuses a new key past the tenant key budget.
	ErrParameterKeyBudgetExceeded = errors.New("config: tenant parameter key budget exceeded")
)

// ValueUseContext names where a resolved parameter value is placed. A value
// is inert data: it is never evaluated as an expression, template or code,
// and every placement encodes it for exactly one of these contexts.
type ValueUseContext string

const (
	// UseHeader places the value in a protocol header or trailer.
	UseHeader ValueUseContext = "HEADER"
	// UseAddress places the value where a bare addr-spec is required.
	UseAddress ValueUseContext = "ADDRESS"
	// UseMessage places the value in human-readable message content.
	UseMessage ValueUseContext = "MESSAGE"
	// UseQueryParam places the value in a URL query parameter.
	UseQueryParam ValueUseContext = "QUERY_PARAM"
)

// TenantValueLimits bounds one tenant's parameter footprint so no tenant
// exhausts shared storage.
type TenantValueLimits struct {
	// MaxKeysPerTenant caps distinct parameter keys with values.
	MaxKeysPerTenant int
	// MaxValueBytes caps one canonical value in bytes.
	MaxValueBytes int
}

// DefaultTenantValueLimits returns the platform budgets.
func DefaultTenantValueLimits() TenantValueLimits {
	return TenantValueLimits{MaxKeysPerTenant: 10000, MaxValueBytes: 65536}
}

func (l TenantValueLimits) withDefaults() TenantValueLimits {
	defaults := DefaultTenantValueLimits()
	if l.MaxKeysPerTenant <= 0 {
		l.MaxKeysPerTenant = defaults.MaxKeysPerTenant
	}
	if l.MaxValueBytes <= 0 {
		l.MaxValueBytes = defaults.MaxValueBytes
	}
	return l
}

// CheckParameterValueSize refuses a value past the size budget.
func CheckParameterValueSize(value string, limits TenantValueLimits) error {
	if len(value) > limits.withDefaults().MaxValueBytes {
		return fmt.Errorf("%w: %d bytes", ErrParameterValueTooLarge, len(value))
	}
	return nil
}

// EncodeParameterValue renders an inert parameter value for one consumer
// context. It never interprets the value: contexts that cannot carry a
// value inertly are refused rather than escaped into a new meaning.
func EncodeParameterValue(value string, use ValueUseContext) (string, error) {
	switch use {
	case UseHeader:
		return encodeHeaderValue(value)
	case UseAddress:
		return encodeAddressValue(value)
	case UseMessage:
		return encodeMessageValue(value)
	case UseQueryParam:
		return url.QueryEscape(value), nil
	default:
		return "", fmt.Errorf("%w: unknown consumer context %q", ErrParameterValueUnsafe, use)
	}
}

// encodeHeaderValue refuses any value that could split a header block. A
// header carries the value verbatim or not at all.
func encodeHeaderValue(value string) (string, error) {
	if strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("%w: header values cannot carry line breaks", ErrParameterValueUnsafe)
	}
	return value, nil
}

// encodeAddressValue accepts only a bare addr-spec: one @, no spaces,
// controls or display-name punctuation, and non-empty local and domain
// parts. Anything else is refused rather than quoted into a new meaning.
func encodeAddressValue(value string) (string, error) {
	if strings.Count(value, "@") != 1 {
		return "", fmt.Errorf("%w: address needs exactly one @", ErrParameterValueUnsafe)
	}
	if strings.ContainsAny(value, " \t\r\n<>\"(),;:[]\\") || strings.ContainsRune(value, 0) {
		return "", fmt.Errorf("%w: address carries display or control characters", ErrParameterValueUnsafe)
	}
	local, domain, _ := strings.Cut(value, "@")
	if local == "" || domain == "" || !strings.Contains(domain, ".") {
		return "", fmt.Errorf("%w: address needs local and dotted domain parts", ErrParameterValueUnsafe)
	}
	return value, nil
}

// encodeMessageValue neutralizes template delimiters and markup so the
// value renders as inert text in both HTML and template contexts. HTML is
// escaped first so the brace entities below survive as entities: they
// render as braces but never execute as delimiters anywhere downstream.
func encodeMessageValue(value string) (string, error) {
	neutralized := html.EscapeString(value)
	neutralized = strings.ReplaceAll(neutralized, "{{", "&#123;&#123;")
	neutralized = strings.ReplaceAll(neutralized, "}}", "&#125;&#125;")
	return neutralized, nil
}
