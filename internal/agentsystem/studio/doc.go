// Package studio owns the Agent Studio draft, validation, evaluation, and
// publication flow. Drafts stay tenant-owned and revision-fenced; validation
// builds the canonical agent manifest, resolves the layered business charter,
// previews effective authority with denied items explained, and binds exact
// sandbox evaluation evidence. Review and publication reuse the governed
// publication service, so the UI in internal/humanwork/productui renders this
// package's status view instead of a separate browser-only agent model.
package studio
