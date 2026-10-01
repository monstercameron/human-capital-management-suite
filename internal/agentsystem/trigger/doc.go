// Package trigger validates autonomous chat-trigger candidates against current
// Chat-owned subscription, committed-outbox, and audience evidence, then
// carries bounded policy into a required atomic ledger port. It does not yet
// build the full AGENT-015 request or provide a served dispatcher or durable
// adapter, so this package alone cannot start inference.
package trigger
