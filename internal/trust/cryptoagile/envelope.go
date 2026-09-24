package cryptoagile

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Envelope pairs a signature with the id of the [AlgorithmSuite] that
// produced it. Every signature this package emits carries its suite id, so
// a verifier never has to guess, configure, or infer which algorithm made a
// given signature; see [EnvelopeVerifier.Verify] for how that id is
// checked, not merely trusted.
type Envelope struct {
	SuiteID   string
	Signature []byte
}

// envelopeEncodingTag versions Envelope's wire form. A future incompatible
// change to the encoding gets a new tag; this one never changes shape, so
// evidence already recorded against it stays decodable forever.
const envelopeEncodingTag = "cryptoagile.v1"

// Encode returns the envelope's fixed, canonical wire form:
// "cryptoagile.v1:<suite id>:<hex signature>". This is the GOLDEN surface:
// the exact bytes matter because evidence records and interoperating
// verifiers depend on the format never drifting.
func (e Envelope) Encode() string {
	return fmt.Sprintf("%s:%s:%s", envelopeEncodingTag, e.SuiteID, hex.EncodeToString(e.Signature))
}

// ErrEnvelopeFormat reports a malformed encoded envelope.
var ErrEnvelopeFormat = errors.New("cryptoagile: envelope is not well-formed")

// DecodeEnvelope parses the wire form [Envelope.Encode] produces.
func DecodeEnvelope(s string) (Envelope, error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 || parts[0] != envelopeEncodingTag {
		return Envelope{}, fmt.Errorf("%w: %q", ErrEnvelopeFormat, s)
	}
	if parts[1] == "" {
		return Envelope{}, fmt.Errorf("%w: empty suite id in %q", ErrEnvelopeFormat, s)
	}
	sig, err := hex.DecodeString(parts[2])
	if err != nil {
		return Envelope{}, fmt.Errorf("%w: signature is not hex in %q: %v", ErrEnvelopeFormat, s, err)
	}
	return Envelope{SuiteID: parts[1], Signature: sig}, nil
}

// SignerPort produces a raw signature over exactly the bytes it is given,
// for the named suite. It never sees an [Envelope] or the [Registry]'s
// status for that suite: suite-id binding is [bindSuite]'s job and status
// enforcement is [EnvelopeVerifier]'s job, both one layer above, so a
// SignerPort implementation cannot accidentally skip either.
//
// [CustodyKeySource] implements it against internal/trust/custody's key
// custody port; [FakeKeySource] implements it in memory for tests.
type SignerPort interface {
	Sign(suiteID string, message []byte) ([]byte, error)
}

// VerifierPort is [SignerPort]'s verification counterpart.
type VerifierPort interface {
	Verify(suiteID string, message, signature []byte) (bool, error)
}

// bindSuite folds suiteID into the bytes that are actually signed, with a
// fixed-width length prefix so that, e.g., suite "ab" + message "c" cannot
// collide with suite "a" + message "bc". This is what makes a "signature
// over a different suite id" (the Security matrix's third case) a
// detectable forgery: relabeling env.SuiteID after signing changes the
// bytes the verifier recomputes, so a signature made for one suite id does
// not verify under a different one even if the same key were reused.
func bindSuite(suiteID string, message []byte) []byte {
	out := make([]byte, 0, 8+len(suiteID)+len(message))
	var lenBuf [8]byte
	binary.BigEndian.PutUint64(lenBuf[:], uint64(len(suiteID)))
	out = append(out, lenBuf[:]...)
	out = append(out, suiteID...)
	out = append(out, message...)
	return out
}

// EnvelopeSigner signs a message under one named suite and returns the
// resulting [Envelope]. [DualSigner] is the multi-suite orchestrator built
// on top of it.
type EnvelopeSigner struct {
	keys SignerPort
}

// NewEnvelopeSigner returns a signer backed by keys.
func NewEnvelopeSigner(keys SignerPort) *EnvelopeSigner {
	return &EnvelopeSigner{keys: keys}
}

// Sign returns message's signature under suiteID, with suiteID bound into
// the signed bytes (see [bindSuite]).
func (s *EnvelopeSigner) Sign(suiteID string, message []byte) (Envelope, error) {
	if strings.TrimSpace(suiteID) == "" {
		return Envelope{}, fmt.Errorf("%w: empty suite id", ErrEnvelopeFormat)
	}
	sig, err := s.keys.Sign(suiteID, bindSuite(suiteID, message))
	if err != nil {
		return Envelope{}, fmt.Errorf("cryptoagile: signing under suite %q: %w", suiteID, err)
	}
	return Envelope{SuiteID: suiteID, Signature: sig}, nil
}

// RetiredSuiteError is the typed reason a RETIRED suite's signature is
// refused. It states the suite and its retirement instant, rather than
// collapsing to a bare "verification failed", so telemetry and a migration
// runbook can distinguish "wrong signature" from "the algorithm was
// deliberately withdrawn on schedule".
type RetiredSuiteError struct {
	SuiteID   string
	RetiredAt time.Time
}

func (e *RetiredSuiteError) Error() string {
	return fmt.Sprintf("cryptoagile: suite %q was retired at %s and no longer verifies", e.SuiteID, e.RetiredAt.Format(time.RFC3339))
}

// Verification refusal reasons other than [RetiredSuiteError].
var (
	ErrStrippedSuiteID           = errors.New("cryptoagile: envelope carries no suite id")
	ErrSuiteNotFound             = errors.New("cryptoagile: envelope names a suite id that is not registered")
	ErrSignatureInvalid          = errors.New("cryptoagile: signature does not verify under its stated suite")
	ErrSuiteNotValidAtSignedTime = errors.New("cryptoagile: suite was not valid at the evidence signing time")
)

// EnvelopeVerifier implements dual-read: [EnvelopeVerifier.Verify] accepts a
// signature under any suite the [Registry] has not marked RETIRED.
type EnvelopeVerifier struct {
	registry *Registry
	keys     VerifierPort
}

// NewEnvelopeVerifier returns a verifier resolving suite status through
// registry and checking signatures through keys.
func NewEnvelopeVerifier(registry *Registry, keys VerifierPort) *EnvelopeVerifier {
	return &EnvelopeVerifier{registry: registry, keys: keys}
}

// Verify checks env against message. It refuses, in order: a stripped
// (empty) suite id, an id the registry does not know, a RETIRED suite (with
// [RetiredSuiteError] naming it), and finally a signature that does not
// recompute - which also catches a signature minted for a different suite
// id, since [bindSuite] folds the claimed id into the bytes being verified.
func (v *EnvelopeVerifier) Verify(message []byte, env Envelope) error {
	if strings.TrimSpace(env.SuiteID) == "" {
		return ErrStrippedSuiteID
	}
	suite, ok := v.registry.Get(env.SuiteID)
	if !ok {
		return fmt.Errorf("%w: %q", ErrSuiteNotFound, env.SuiteID)
	}
	if suite.Status == StatusRetired {
		return &RetiredSuiteError{SuiteID: suite.ID, RetiredAt: suite.RetiredAt}
	}
	ok2, err := v.keys.Verify(env.SuiteID, bindSuite(env.SuiteID, message), env.Signature)
	if err != nil {
		return fmt.Errorf("cryptoagile: verifying under suite %q: %w", env.SuiteID, err)
	}
	if !ok2 {
		return fmt.Errorf("%w: suite %q", ErrSignatureInvalid, env.SuiteID)
	}
	return nil
}

// VerifyHistorical verifies the cryptographic authenticity of evidence that
// was signed while its suite was active or dual, including evidence whose
// suite has since been retired. It is for historical verification only;
// callers making a current trust decision must use [EnvelopeVerifier.Verify],
// which continues to refuse retired suites.
//
// signedAt must come from a timestamp bound by message or from an independently
// protected append-only record. This method does not authenticate signedAt;
// passing a fabricated pre-retirement time can make a post-retirement signature
// appear historical. The caller must establish the timestamp's provenance.
func (v *EnvelopeVerifier) VerifyHistorical(message []byte, env Envelope, signedAt time.Time) error {
	if strings.TrimSpace(env.SuiteID) == "" {
		return ErrStrippedSuiteID
	}
	suite, ok := v.registry.Get(env.SuiteID)
	if !ok {
		return fmt.Errorf("%w: %q", ErrSuiteNotFound, env.SuiteID)
	}
	if signedAt.IsZero() || suite.ActivatedAt.IsZero() || signedAt.Before(suite.ActivatedAt) ||
		(suite.Status == StatusRetired && !signedAt.Before(suite.RetiredAt)) {
		return fmt.Errorf("%w: suite %q at %s", ErrSuiteNotValidAtSignedTime, suite.ID, signedAt.Format(time.RFC3339))
	}
	valid, err := v.keys.Verify(env.SuiteID, bindSuite(env.SuiteID, message), env.Signature)
	if err != nil {
		return fmt.Errorf("cryptoagile: verifying historical evidence under suite %q: %w", env.SuiteID, err)
	}
	if !valid {
		return fmt.Errorf("%w: suite %q", ErrSignatureInvalid, env.SuiteID)
	}
	return nil
}

// VerifyAny is dual-read's entry point for a dual-signed message: it tries
// every envelope in order and returns the first one that verifies under a
// non-retired suite. It fails only when every envelope fails, joining each
// envelope's own refusal reason with [errors.Join] so the caller can still
// inspect a specific one (e.g. with [errors.As] for [RetiredSuiteError]).
func (v *EnvelopeVerifier) VerifyAny(message []byte, envs []Envelope) (Envelope, error) {
	if len(envs) == 0 {
		return Envelope{}, fmt.Errorf("cryptoagile: no envelopes to verify")
	}
	var errs []error
	for _, env := range envs {
		if err := v.Verify(message, env); err == nil {
			return env, nil
		} else {
			errs = append(errs, err)
		}
	}
	return Envelope{}, fmt.Errorf("cryptoagile: no envelope verified: %w", errors.Join(errs...))
}
