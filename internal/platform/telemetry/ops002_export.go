package telemetry

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
)

const ops002ReceiptVersion = 1

var (
	// ErrPolicyReceiptKey is returned when a receipt cannot be signed or
	// verified with the supplied Ed25519 key.
	ErrPolicyReceiptKey = errors.New("telemetry: policy receipt key is invalid")
	// ErrPolicyReceiptSignature is returned when a receipt signature does not
	// verify against the receipt's immutable fields.
	ErrPolicyReceiptSignature = errors.New("telemetry: policy receipt signature is invalid")
)

// PolicyReceipt is the payload-free, signed evidence for one export
// evaluation. It contains counts and digests, never attribute values, so the
// receipt can be retained independently of the signal without becoming a
// second payload channel.
type PolicyReceipt struct {
	Version           int
	PolicyVersion     int
	Signal            SignalKind
	AttributesDigest  string
	Kept              int
	Dropped           int
	CardinalityCapped int
	Retention         RetentionClass
	Retained          bool
	SamplingReason    string
	Signature         []byte
}

// ExportedSignal is the only projection allowed to leave the evaluator. Its
// attributes are copies of allow-listed values; metric values are already
// cardinality-capped by EvaluateAttribute.
type ExportedSignal struct {
	Attributes map[string]string
	Decisions  []Decision
	Receipt    PolicyReceipt
}

// EvaluateSignal applies classification, signal eligibility, metric
// cardinality and server-owned sampling as one export decision. A signed
// receipt is mandatory so a caller cannot claim that sensitive attributes or
// critical failures were handled without evidence.
func (e *Evaluator) EvaluateSignal(kind SignalKind, attrs map[string]string, correlationID string, retention RetentionClass, privateKey ed25519.PrivateKey) (ExportedSignal, error) {
	if !kind.valid() {
		return ExportedSignal{}, errors.New("telemetry: signal kind is not published")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return ExportedSignal{}, ErrPolicyReceiptKey
	}
	if e == nil || e.Allow == nil {
		return ExportedSignal{}, errors.New("telemetry: policy evaluator is unconfigured")
	}

	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := ExportedSignal{Attributes: make(map[string]string), Decisions: make([]Decision, 0, len(keys))}
	canonical := make([]string, 0, len(keys))
	for _, key := range keys {
		decision := e.EvaluateAttribute(kind, key, attrs[key])
		result.Decisions = append(result.Decisions, decision)
		if decision.Kept {
			result.Attributes[key] = decision.Value
			if kind == SignalMetric && decision.Value == cardinalityOverflowValue && attrs[key] != decision.Value {
				result.Receipt.CardinalityCapped++
			}
			result.Receipt.Kept++
		} else {
			result.Receipt.Dropped++
		}
		// Only policy outcomes and a digest of kept values enter the signed
		// receipt. The raw value itself is deliberately absent.
		canonical = append(canonical, strings.Join([]string{key, string(decision.Class), strconv.FormatBool(decision.Kept), digestValue(decision.Value), decision.DropReason}, "\x00"))
	}

	sampling := e.Decide(correlationID, retention)
	result.Receipt.Version = ops002ReceiptVersion
	result.Receipt.PolicyVersion = e.Sampling.Version
	result.Receipt.Signal = kind
	result.Receipt.Retention = retention
	result.Receipt.Retained = sampling.Retained
	result.Receipt.SamplingReason = sampling.Reason
	result.Receipt.AttributesDigest = digestValue(strings.Join(canonical, "\n"))
	if err := result.Receipt.Sign(privateKey); err != nil {
		return ExportedSignal{}, err
	}
	return result, nil
}

// Sign fills Signature over the receipt's immutable fields.
func (r *PolicyReceipt) Sign(privateKey ed25519.PrivateKey) error {
	if r == nil || len(privateKey) != ed25519.PrivateKeySize {
		return ErrPolicyReceiptKey
	}
	r.Signature = append([]byte(nil), ed25519.Sign(privateKey, r.signingBytes())...)
	return nil
}

// Verify checks the signature and all receipt fields covered by it.
func (r PolicyReceipt) Verify(publicKey ed25519.PublicKey) error {
	if len(publicKey) != ed25519.PublicKeySize || len(r.Signature) != ed25519.SignatureSize {
		return ErrPolicyReceiptKey
	}
	if !ed25519.Verify(publicKey, r.signingBytes(), r.Signature) {
		return ErrPolicyReceiptSignature
	}
	return nil
}

// VerifyPolicyReceipt is the function form for callers that do not retain a
// receipt method value at their transport boundary.
func VerifyPolicyReceipt(receipt PolicyReceipt, publicKey ed25519.PublicKey) error {
	return receipt.Verify(publicKey)
}

func (r PolicyReceipt) signingBytes() []byte {
	return []byte(strings.Join([]string{
		"hcmnext.telemetry.policy_receipt", strconv.Itoa(r.Version), strconv.Itoa(r.PolicyVersion),
		string(r.Signal), r.AttributesDigest, strconv.Itoa(r.Kept), strconv.Itoa(r.Dropped),
		strconv.Itoa(r.CardinalityCapped), string(r.Retention), strconv.FormatBool(r.Retained), r.SamplingReason,
	}, "\x00"))
}

func digestValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
