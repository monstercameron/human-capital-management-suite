// Package memory governs agent memory and other agent-derived copies.
//
// It keeps every value subordinate to an owner-issued source, audience,
// purpose, classification, retention decision, and revocation state. It does
// not make derived material authoritative and does not own persistence; its
// ports must be implemented by the agent store and each derived index.
package memory
