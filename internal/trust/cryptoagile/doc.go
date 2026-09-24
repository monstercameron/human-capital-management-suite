// Package cryptoagile proves that Human Capital Management Suite can rotate a signing or MAC
// algorithm across its ledger, document, token and connector evidence
// without a flag day and without making history unverifiable.
//
// Semantic owner: governance-and-trust. Phase: P1A. Todo: CRYPTO-001.
//
// # The shape of the problem
//
// Signing already happens in several places today: internal/trust's
// HMAC-SHA256 development bearer token (internal/trust/hmactoken.go),
// tools/planning/gateevidence's Ed25519 manifest signatures, and the
// tamper-evident hash chain migrations/00014 adds over the ledger
// (internal/ledger). Each of those call sites bakes in one concrete
// algorithm. The day one of them needs to rotate - a weaker MAC key
// retired, an Ed25519 key rolled after a suspected compromise, a future
// post-quantum signature scheme phased in - every already-recorded
// signature under the old algorithm must stay verifiable, every
// newly-issued signature must move to the new algorithm on a declared
// schedule, and nothing in between may accept a signature under an
// algorithm that has been formally withdrawn.
//
// # What this package adds
//
// [AlgorithmSuite] and [Registry] hold algorithm identity as configuration
// - an id, a [Kind] (signature, mac or digest), a [Status]
// (ACTIVE/DUAL/RETIRED) and its activation/retirement instants - never as a
// scattered string constant. [Envelope] carries a signature's suite id as
// data, with the id folded into the signed bytes themselves (see
// envelope.go's bindSuite), so relabeling an envelope after the fact is a
// detectable forgery rather than a silent downgrade.
//
// [DualSigner] implements dual-sign: during a migration window it signs a
// message under both the outgoing (ACTIVE) and incoming (DUAL) suite.
// [EnvelopeVerifier.Verify] implements current dual-read: it accepts a
// signature under any suite the registry has not marked RETIRED, and refuses
// a RETIRED suite with a typed [RetiredSuiteError].
// [EnvelopeVerifier.VerifyHistorical] checks evidence under a retired suite
// only when the caller supplies its original signing time, before retirement.
// A signature alone does not authenticate the caller-supplied signing time.
// The caller must derive it from a timestamp covered by message or from an
// independently protected append-only record; otherwise post-cutoff material
// could be misrepresented as historical. Historical verification proves
// authenticity; it does not authorize a current action under a retired suite.
//
// [MigrationPlan] declares the windows a migration moves through and
// [MigrationPlan.Validate] refuses a plan whose windows overlap or leave a
// gap - the two ways a hand-edited schedule silently produces a moment with
// two conflicting "current" algorithms or none at all. [Evidence] and
// [Resume] give the migration a durable trail: a process interrupted
// mid-window picks the migration back up from what was actually recorded,
// not from wall-clock time alone.
//
// # Key material
//
// This package performs its own cryptography with only crypto/ed25519 and
// crypto/hmac from the standard library (no new dependency). It never
// generates, stores or exports raw key material itself: [SignerPort] and
// [VerifierPort] are the narrow ports [EnvelopeSigner] and [EnvelopeVerifier]
// use, and [CustodyKeySource] adapts internal/trust/custody's Provider - the
// key custody port - to them, matching custody's own rule that a caller
// never receives raw credential bytes. [FakeKeySource] is the in-memory test
// double used where a real custody provider is not available.
//
// A production migration caller must persist migration receipts and additional
// signatures as append-only records, and must compose a suite registry and
// custody handles from governed policy. The existing ledger checkpoint epoch
// stores one Ed25519 signature and is immutable; this package does not provide
// a production receipt store or a production suite-policy source.
package cryptoagile
