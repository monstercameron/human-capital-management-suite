// Package clockinject rejects direct wall-clock reads in engine and
// domain packages: every timestamp under internal/engines and
// internal/domains must come from an injected clock (the sibling
// convention is a func() time.Time field or parameter defaulting to the
// wall clock when nil or absent), so engine and domain output stays
// deterministic and replayable (REV-101-07).
//
// The checker is intentionally kernel-pure: it uses go/parser for static
// inspection and never starts a database, container, or network service.
// A time.Now call is allowed only inside a func() time.Time literal (the
// wall-default adapter idiom) or at a site named in DeclaredAdapters,
// which records why that fallback exists.
package clockinject
