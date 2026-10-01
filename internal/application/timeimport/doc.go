// Package timeimport is the application boundary for third-party time-app
// imports.
//
// Provider-specific admission remains in connectivity/timeapp. This package
// exposes that connector at the application composition boundary so callers
// cannot accidentally turn an external timesheet into a first-party punch:
// identity resolution, webhook receipts, provider record identity, trust
// ceiling and append-only corrections remain part of the imported observation
// contract.
package timeimport
