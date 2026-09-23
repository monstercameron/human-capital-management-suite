// Package errlint flags discarded call results in non-test Go code: every
// `_ = f()` (or `_, _ = f()`) that discards every result of a call,
// unless the statement sits inside a deferred rollback or calls a method
// named Rollback. Rollbacks are the one idiom the policy tolerates, because a
// rollback runs only after the outcome it cleans up was already decided
// by the Commit whose error the caller did check (or by a setup failure
// whose error the caller returns). Everything else -- a dropped
// fence query, an ignored decode, an unlogged commit -- must be returned,
// logged, or named in Exceptions with the reason it is safe (REV-103-06).
//
// The checker is intentionally kernel-pure: it uses go/parser for static
// inspection and never starts a database, container, or network service.
// Without type information it cannot tell an error from any other
// discarded value, so it flags every blanked call and lets the exceptions
// registry record which ones are safe and why.
package errlint
