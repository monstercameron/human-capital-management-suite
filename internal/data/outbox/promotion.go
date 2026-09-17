package outbox

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrPromotionInvalid reports a malformed promotion dossier: an
	// unknown or padded broker kind, or a negative envelope or load
	// axis. Malformed input can never promote.
	ErrPromotionInvalid = errors.New("outbox: invalid promotion dossier")

	// ErrPromotionEvidence reports a dossier that names no measured
	// need or proves no parity: missing owner, missing exit plan,
	// cost above cap, or absent conformance/restore/operability
	// evidence. Without it Postgres stays authoritative.
	ErrPromotionEvidence = errors.New("outbox: promotion evidence incomplete")

	// ErrPromotionUnsigned reports a dossier whose seal is absent or
	// does not match its material fields under the authority key: an
	// unsigned or tampered decision never promotes.
	ErrPromotionUnsigned = errors.New("outbox: promotion decision unsigned")
)

// Low-cost OSS broker options eligible for promotion. The gate admits
// only a named option from this list, so a broker is never introduced
// on unreviewed cost or operability grounds.
const (
	BrokerRedpandaLite  = "oss-redpanda-lite"
	BrokerNATSJetStream = "oss-nats-jetstream"
)

// Transport is the authoritative event transport. The zero value is
// Postgres: a refused or unevaluated decision keeps Postgres
// authoritative without any explicit fallback wiring.
type Transport int

const (
	TransportPostgres Transport = iota
	TransportBroker
)

// String renders a transport for diagnostics.
func (t Transport) String() string {
	switch t {
	case TransportPostgres:
		return "postgres"
	case TransportBroker:
		return "broker"
	default:
		return fmt.Sprintf("unknown-transport(%d)", int(t))
	}
}

// OutboxEnvelope is the measured capacity envelope of the Postgres
// outbox transport: the sustained event rate, backlog and consumer lag
// it absorbs. Promotion requires measured load past at least one axis.
type OutboxEnvelope struct {
	MaxSustainedEPS float64
	MaxBacklog      int64
	MaxLag          time.Duration
}

// MeasuredLoad is one observed outbox sample against the envelope.
type MeasuredLoad struct {
	SustainedEPS float64
	Backlog      int64
	P99Lag       time.Duration
}

// ParityEvidence is the recovery/ordering parity and operability proof
// for the selected broker: the digest of its passed conformance run,
// a passed restore/replay drill, and a passed operability check.
type ParityEvidence struct {
	ConformanceDigest  string
	RestoreDrillPassed bool
	OperabilityPassed  bool
}

// PromotionDossier is the signed promotion case: the selected broker,
// its ownership, cost bound and exit plan, the envelope, the measured
// load, and the parity evidence. Signature seals every material field
// under the offline authority key.
type PromotionDossier struct {
	BrokerKind          string
	Owner               string
	EstimatedMonthlyUSD float64
	MonthlyCapUSD       float64
	ExitPlan            string
	Envelope            OutboxEnvelope
	Load                MeasuredLoad
	Parity              ParityEvidence
	Signature           string
}

// PromotionDecision is the gate outcome. Transport is Postgres unless
// every check passes and the envelope is breached.
type PromotionDecision struct {
	Transport     Transport
	Promote       bool
	BrokerKind    string
	BreachReasons []string
}

// EnvelopeBreached reports whether any measured axis exceeds the
// envelope, with one reason per breached axis.
func EnvelopeBreached(envelope OutboxEnvelope, load MeasuredLoad) (bool, []string) {
	var reasons []string
	if load.SustainedEPS > envelope.MaxSustainedEPS {
		reasons = append(reasons, fmt.Sprintf("sustained-eps %.2f exceeds envelope %.2f", load.SustainedEPS, envelope.MaxSustainedEPS))
	}
	if load.Backlog > envelope.MaxBacklog {
		reasons = append(reasons, fmt.Sprintf("backlog %d exceeds envelope %d", load.Backlog, envelope.MaxBacklog))
	}
	if load.P99Lag > envelope.MaxLag {
		reasons = append(reasons, fmt.Sprintf("p99-lag %s exceeds envelope %s", load.P99Lag, envelope.MaxLag))
	}
	return len(reasons) > 0, reasons
}

// SignDossier seals a dossier's material fields under the authority
// key. Verification recomputes the same seal, so any later edit —
// however small — voids the signature.
func SignDossier(dossier *PromotionDossier, key string) {
	dossier.Signature = promotionSeal(*dossier, key)
}

func promotionSeal(dossier PromotionDossier, key string) string {
	canonical := strings.Join([]string{
		dossier.BrokerKind,
		dossier.Owner,
		strconv.FormatFloat(dossier.EstimatedMonthlyUSD, 'g', -1, 64),
		strconv.FormatFloat(dossier.MonthlyCapUSD, 'g', -1, 64),
		dossier.ExitPlan,
		strconv.FormatFloat(dossier.Envelope.MaxSustainedEPS, 'g', -1, 64),
		strconv.FormatInt(dossier.Envelope.MaxBacklog, 10),
		strconv.FormatInt(int64(dossier.Envelope.MaxLag), 10),
		strconv.FormatFloat(dossier.Load.SustainedEPS, 'g', -1, 64),
		strconv.FormatInt(dossier.Load.Backlog, 10),
		strconv.FormatInt(int64(dossier.Load.P99Lag), 10),
		dossier.Parity.ConformanceDigest,
		strconv.FormatBool(dossier.Parity.RestoreDrillPassed),
		strconv.FormatBool(dossier.Parity.OperabilityPassed),
		key,
	}, "\x00")
	sum := sha256.Sum256([]byte(canonical))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validBrokerKind(kind string) bool {
	switch kind {
	case BrokerRedpandaLite, BrokerNATSJetStream:
		return true
	default:
		return false
	}
}

// EvaluatePromotion gates the move from the Postgres outbox to a
// broker. It fails closed: any malformed, unevidenced or unsigned
// dossier is refused with its sentinel error and a Postgres decision,
// an intact envelope stays on Postgres without error, and only a
// signed, fully evidenced dossier over a breached envelope promotes.
func EvaluatePromotion(dossier PromotionDossier, key string) (PromotionDecision, error) {
	stay := PromotionDecision{Transport: TransportPostgres}
	if !validBrokerKind(dossier.BrokerKind) || dossier.BrokerKind != strings.TrimSpace(dossier.BrokerKind) {
		return stay, fmt.Errorf("%w: broker kind %q is not a listed low-cost OSS option", ErrPromotionInvalid, dossier.BrokerKind)
	}
	if dossier.Envelope.MaxSustainedEPS < 0 || dossier.Envelope.MaxBacklog < 0 || dossier.Envelope.MaxLag < 0 ||
		dossier.Load.SustainedEPS < 0 || dossier.Load.Backlog < 0 || dossier.Load.P99Lag < 0 {
		return stay, fmt.Errorf("%w: envelope and load axes must be non-negative", ErrPromotionInvalid)
	}
	if strings.TrimSpace(dossier.Owner) == "" {
		return stay, fmt.Errorf("%w: owner is required", ErrPromotionEvidence)
	}
	if strings.TrimSpace(dossier.ExitPlan) == "" {
		return stay, fmt.Errorf("%w: exit plan back to postgres is required", ErrPromotionEvidence)
	}
	if dossier.MonthlyCapUSD <= 0 || dossier.EstimatedMonthlyUSD < 0 || dossier.EstimatedMonthlyUSD > dossier.MonthlyCapUSD {
		return stay, fmt.Errorf("%w: cost %.2f exceeds cap %.2f", ErrPromotionEvidence, dossier.EstimatedMonthlyUSD, dossier.MonthlyCapUSD)
	}
	if strings.TrimSpace(dossier.Parity.ConformanceDigest) == "" {
		return stay, fmt.Errorf("%w: broker conformance digest is required", ErrPromotionEvidence)
	}
	if !dossier.Parity.RestoreDrillPassed {
		return stay, fmt.Errorf("%w: restore drill evidence is required", ErrPromotionEvidence)
	}
	if !dossier.Parity.OperabilityPassed {
		return stay, fmt.Errorf("%w: operability evidence is required", ErrPromotionEvidence)
	}
	if dossier.Signature == "" {
		return stay, fmt.Errorf("%w: dossier carries no authority seal", ErrPromotionUnsigned)
	}
	if subtle.ConstantTimeCompare([]byte(dossier.Signature), []byte(promotionSeal(dossier, key))) != 1 {
		return stay, fmt.Errorf("%w: dossier seal does not match its material fields", ErrPromotionUnsigned)
	}
	breached, reasons := EnvelopeBreached(dossier.Envelope, dossier.Load)
	if !breached {
		return stay, nil
	}
	return PromotionDecision{Transport: TransportBroker, Promote: true, BrokerKind: dossier.BrokerKind, BreachReasons: reasons}, nil
}
