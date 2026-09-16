// Package capability implements the P1A capability registry and governed
// gateway (owner: platform; phase: P1A; CAP-001, CAP-002).
//
// # BOOTSTRAP profile
//
// planning/specs/capability-registry-and-lifecycle.md declares two registry
// profiles. Under BOOTSTRAP (P1A and P1B) the registry is a compiled-in Go
// table: the build is the publication, and there is no separate propose,
// validate, review, publish or activate flow. [NewBootstrapRegistry] returns
// that compiled table. The full MANAGED lifecycle (signed Control Bundle,
// quarantine SLA, adoption tracking) is Gate C and is not implemented here.
//
// # Immutability
//
// A [Definition] is a plain value. Once [Registry.Register] accepts one, its
// (ID, Version) key can never be re-registered with different bytes, and
// every accessor returns a defensive copy so a caller can never reach back
// into the registry's own storage and mutate it.
//
// # Governed gateway
//
// [Gateway.Invoke] is the single path every transport must share (CAP-002).
// It resolves the exact capability version, refuses a capability a
// [SuspensionSource] reports suspended (WF-RUN-039), requires an already-made
// [Authorization] decision (this package never authenticates or authorizes;
// it only enforces the decision it is handed), refuses any capability that
// declares a write effect class — P1A ships zero-effect capabilities only —
// and records an [EvidenceSink] entry for every invocation and refusal.
package capability
