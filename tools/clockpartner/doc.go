// Package clockpartner provides the bounded TCLOCK-018 partner conformance kit.
//
// The kit is transport-neutral: callers supply the generated
// ClockDeviceService client (or an HTTP client implementing the same narrow
// interface). The simulator only records observations returned by that client;
// it never treats fixture data or a claimed status as evidence.
package clockpartner
