// Processor acknowledgement reconciliation and fulfillment certification
// (PRIV-007).
//
// When governed fulfillment work fans out to processors -- delete this copy,
// return that archive -- each processor answers with an acknowledgement and
// this package reconciles those answers into one signed fulfillment package:
//
//  1. Request: a [FulfillmentRequest] names every copy, its processor, the
//     required action and the instant by which the acknowledgement is due.
//  2. Reconcile: [ReconcileFulfillment] derives one [ItemResolution] per
//     item from the collected [ProcessorAck] evidence. A FULFILLED ack with
//     a receipt digest resolves; a REFUSED or UNKNOWN_COPY ack becomes a
//     visible EXCEPTION carrying the processor's reason; silence before the
//     due instant is PENDING_RETRY and silence at or after it is ESCALATED.
//     Nothing unacknowledged is ever marked resolved.
//  3. Certify: [CertifyFulfillment] derives a [FulfillmentCertificate] from
//     a reconciliation. Pending or escalated items refuse certification
//     with [ErrFulfillmentIncomplete]; exceptions are preserved verbatim in
//     the package but never enter CertifiedItems, so only fully resolved
//     items are ever certified complete.
//
// The certificate is derived from evidence, never a manual status toggle:
// there is no constructor that accepts caller-supplied statuses, and
// [FulfillmentCertificate.Validate] re-derives the package digest, so a
// flipped flag or a dropped exception fails validation. Times arrive as
// injected [values.Instant] arguments; this file reads no clock, database
// or network.
package privacy

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Processor-fulfillment errors.
var (
	// ErrProcessorCertBlocked is returned when fulfillment input cannot
	// prove what it claims: a malformed request, a misaddressed, duplicate,
	// future-dated or receiptless acknowledgement, or an uncertifiable
	// package (anonymous certifier, forged reconciliation).
	ErrProcessorCertBlocked = errors.New("privacy: processor fulfillment blocked")
	// ErrFulfillmentIncomplete is returned by [CertifyFulfillment] when a
	// pending-retry or escalated item would have to be papered over. The
	// error names every unresolved item so the retry/escalation stays
	// visible. It is distinct from [ErrProcessorCertBlocked] so callers can
	// separate "evidence not yet complete" (retry, escalate, wait) from
	// "evidence malformed" (reject, investigate).
	ErrFulfillmentIncomplete = errors.New("privacy: fulfillment incomplete")
)

const processorCertEvidencePrefix = "ev:privacy:processorcert:"

// ProcessorAction is the closed fulfillment-action vocabulary.
type ProcessorAction string

const (
	ProcessorActionDelete ProcessorAction = "DELETE"
	ProcessorActionReturn ProcessorAction = "RETURN"
)

func (a ProcessorAction) valid() bool {
	return a == ProcessorActionDelete || a == ProcessorActionReturn
}

// FulfillmentItem is one copy at one processor that must be deleted or
// returned, with the instant by which the processor must acknowledge it.
type FulfillmentItem struct {
	ItemID    string
	Processor string
	Action    ProcessorAction
	CopyRef   string
	DueAt     values.Instant
}

// FulfillmentRequest is the governed instruction set: every copy in scope.
// SubjectRef is an opaque subject reference, never subject payload.
type FulfillmentRequest struct {
	RequestID  string
	SubjectRef string
	Items      []FulfillmentItem
}

// AckOutcome is the closed acknowledgement-outcome vocabulary. There is no
// "maybe": a processor either fulfilled with a receipt, refused with a
// reason, or denies holding the copy (also with a reason). Silence is not
// an outcome -- it is the absence of an ack, reconciled by the due instant.
type AckOutcome string

const (
	AckFulfilled   AckOutcome = "FULFILLED"
	AckRefused     AckOutcome = "REFUSED"
	AckUnknownCopy AckOutcome = "UNKNOWN_COPY"
)

func (o AckOutcome) valid() bool {
	return o == AckFulfilled || o == AckRefused || o == AckUnknownCopy
}

// ProcessorAck is one processor's answer for one item. A fulfilled ack must
// carry the receipt digest (sha256 hex, never payload); a refusal or
// unknown-copy ack must carry the reason in Detail and no receipt.
type ProcessorAck struct {
	ItemID        string
	Processor     string
	Outcome       AckOutcome
	At            values.Instant
	ReceiptDigest string
	Detail        string
}

// Digest binds the ack's exact content: re-deriving it over the same ack
// yields the same digest, and any altered field yields another.
func (a ProcessorAck) Digest() string {
	sec, nsec := a.At.Unix()
	return digestHex(appendFields(nil,
		"item", a.ItemID,
		"processor", a.Processor,
		"outcome", string(a.Outcome),
		"at_sec", itoa(sec),
		"at_nsec", itoa(int64(nsec)),
		"receipt", a.ReceiptDigest,
		"detail", a.Detail,
	))
}

// ItemStatus is the closed per-item reconciliation vocabulary.
type ItemStatus string

const (
	// ItemResolved marks an item with a receipted FULFILLED ack: the only
	// status that can enter a certificate's CertifiedItems.
	ItemResolved ItemStatus = "RESOLVED"
	// ItemPendingRetry marks a silent item before its due instant: a
	// visible retry, never completion.
	ItemPendingRetry ItemStatus = "PENDING_RETRY"
	// ItemEscalated marks a silent item at or after its due instant: a
	// visible escalation, never completion.
	ItemEscalated ItemStatus = "ESCALATED"
	// ItemException marks a refused or unknown-copy item: preserved in the
	// package with its reason, never certified.
	ItemException ItemStatus = "EXCEPTION"
)

func (s ItemStatus) valid() bool {
	switch s {
	case ItemResolved, ItemPendingRetry, ItemEscalated, ItemException:
		return true
	default:
		return false
	}
}

// ItemResolution is the derived state of one item: its status plus the
// digest of the ack that resolved or excepted it (empty when no ack
// arrived) and the preserved reason for exceptions.
type ItemResolution struct {
	ItemID    string     `json:"item_id"`
	Processor string     `json:"processor"`
	Status    ItemStatus `json:"status"`
	AckDigest string     `json:"ack_digest,omitempty"`
	Detail    string     `json:"detail,omitempty"`
}

// Reconciliation is the derived, digest-bound outcome of reconciling one
// request against the collected acks at one instant. It is built only by
// [ReconcileFulfillment]: statuses are computed, never supplied.
// RequestDigest binds the exact instruction set reconciled, so Validate can
// re-derive Digest from the package's own fields: any added, dropped or
// altered resolution fails validation.
type Reconciliation struct {
	RequestID     string           `json:"request_id"`
	RequestDigest string           `json:"request_digest"`
	At            values.Instant   `json:"at"`
	Resolutions   []ItemResolution `json:"resolutions"`
	Digest        string           `json:"digest"`
}

// reconciliationDigest derives the package digest over the request binding
// and every resolution in request order.
func reconciliationDigest(requestDigest string, at values.Instant, resolutions []ItemResolution) string {
	sec, nsec := at.Unix()
	dst := appendFields(nil,
		"request_digest", requestDigest,
		"at_sec", itoa(sec),
		"at_nsec", itoa(int64(nsec)),
	)
	for _, r := range resolutions {
		dst = appendFields(dst,
			"item", r.ItemID,
			"processor", r.Processor,
			"status", string(r.Status),
			"ack_digest", r.AckDigest,
			"detail", r.Detail,
		)
	}
	return digestHex(dst)
}

// requestDigest binds the exact instruction set reconciled: request id,
// subject reference and every item in order.
func requestDigest(req FulfillmentRequest) string {
	dst := appendFields(nil,
		"request_id", req.RequestID,
		"subject_ref", req.SubjectRef,
	)
	for _, it := range req.Items {
		sec, nsec := it.DueAt.Unix()
		dst = appendFields(dst,
			"item", it.ItemID,
			"processor", it.Processor,
			"action", string(it.Action),
			"copy_ref", it.CopyRef,
			"due_sec", itoa(sec),
			"due_nsec", itoa(int64(nsec)),
		)
	}
	return digestHex(dst)
}

// Validate reports whether r is structurally sound: named request, set
// instant, well-formed digest, one vocabulary-valid resolution per entry,
// and ack digests exactly where the status demands them (present for
// resolved/excepted, absent for pending/escalated). It cannot re-derive the
// request binding without the request; that check belongs to the caller
// holding the instructions.
func (r Reconciliation) Validate() error {
	if strings.TrimSpace(r.RequestID) == "" {
		return fmt.Errorf("%w: reconciliation names no request", ErrProcessorCertBlocked)
	}
	if !r.At.IsSet() {
		return fmt.Errorf("%w: reconciliation %q records no instant", ErrProcessorCertBlocked, r.RequestID)
	}
	if !isPayloadDigest(r.Digest) {
		return fmt.Errorf("%w: reconciliation %q carries no digest", ErrProcessorCertBlocked, r.RequestID)
	}
	if len(r.Resolutions) == 0 {
		return fmt.Errorf("%w: reconciliation %q resolves no items", ErrProcessorCertBlocked, r.RequestID)
	}
	if !isPayloadDigest(r.RequestDigest) {
		return fmt.Errorf("%w: reconciliation %q binds no request", ErrProcessorCertBlocked, r.RequestID)
	}
	for _, res := range r.Resolutions {
		if strings.TrimSpace(res.ItemID) == "" || strings.TrimSpace(res.Processor) == "" {
			return fmt.Errorf("%w: reconciliation %q holds an unbound resolution", ErrProcessorCertBlocked, r.RequestID)
		}
		if !res.Status.valid() {
			return fmt.Errorf("%w: reconciliation %q holds invalid status %q for item %q", ErrProcessorCertBlocked, r.RequestID, string(res.Status), res.ItemID)
		}
		switch res.Status {
		case ItemResolved, ItemException:
			if !isPayloadDigest(res.AckDigest) {
				return fmt.Errorf("%w: item %q is %s with no ack digest", ErrProcessorCertBlocked, res.ItemID, string(res.Status))
			}
		case ItemPendingRetry, ItemEscalated:
			if res.AckDigest != "" {
				return fmt.Errorf("%w: item %q is %s yet cites an ack", ErrProcessorCertBlocked, res.ItemID, string(res.Status))
			}
		}
	}
	if want := reconciliationDigest(r.RequestDigest, r.At, r.Resolutions); want != r.Digest {
		return fmt.Errorf("%w: reconciliation digest mismatch: package was altered after derivation", ErrProcessorCertBlocked)
	}
	return nil
}

// ReconcileFulfillment derives one [ItemResolution] per request item from
// the collected acks at now. now is the injected clock: silence before an
// item's due instant reconciles to PENDING_RETRY, silence at or after it to
// ESCALATED. Every validation failure returns [ErrProcessorCertBlocked];
// the returned Reconciliation, when non-error, is fully resolved,
// retried, escalated or excepted -- never silently completed.
func ReconcileFulfillment(req FulfillmentRequest, acks []ProcessorAck, now values.Instant) (Reconciliation, error) {
	blocked := func(format string, args ...any) (Reconciliation, error) {
		return Reconciliation{}, fmt.Errorf("%w: %s", ErrProcessorCertBlocked, fmt.Sprintf(format, args...))
	}
	if strings.TrimSpace(req.RequestID) == "" {
		return blocked("request names no id")
	}
	if len(req.Items) == 0 {
		return blocked("request %q names no items", req.RequestID)
	}
	if !now.IsSet() {
		return blocked("request %q reconciles at no instant", req.RequestID)
	}
	items := make(map[string]FulfillmentItem, len(req.Items))
	for _, it := range req.Items {
		if strings.TrimSpace(it.ItemID) == "" {
			return blocked("request %q holds an item with no id", req.RequestID)
		}
		if _, dup := items[it.ItemID]; dup {
			return blocked("request %q names item %q twice", req.RequestID, it.ItemID)
		}
		if strings.TrimSpace(it.Processor) == "" {
			return blocked("item %q names no processor", it.ItemID)
		}
		if !it.Action.valid() {
			return blocked("item %q carries invalid action %q", it.ItemID, string(it.Action))
		}
		if strings.TrimSpace(it.CopyRef) == "" {
			return blocked("item %q names no copy", it.ItemID)
		}
		if !it.DueAt.IsSet() {
			return blocked("item %q sets no acknowledgement due instant", it.ItemID)
		}
		items[it.ItemID] = it
	}
	byItem := make(map[string]ProcessorAck, len(acks))
	for _, ack := range acks {
		it, known := items[ack.ItemID]
		if !known {
			return blocked("ack answers for item %q, which request %q never named", ack.ItemID, req.RequestID)
		}
		if ack.Processor != it.Processor {
			return blocked("ack for item %q comes from %q, not its processor %q", ack.ItemID, ack.Processor, it.Processor)
		}
		if _, dup := byItem[ack.ItemID]; dup {
			return blocked("item %q is acknowledged twice: refusing to merge", ack.ItemID)
		}
		if !ack.Outcome.valid() {
			return blocked("ack for item %q carries invalid outcome %q", ack.ItemID, string(ack.Outcome))
		}
		if !ack.At.IsSet() {
			return blocked("ack for item %q records no instant", ack.ItemID)
		}
		if ack.At.After(now) {
			return blocked("ack for item %q is dated after the reconciliation instant", ack.ItemID)
		}
		switch ack.Outcome {
		case AckFulfilled:
			if !isPayloadDigest(ack.ReceiptDigest) {
				return blocked("item %q is fulfilled with no receipt digest: raw payload never stands in", ack.ItemID)
			}
		case AckRefused, AckUnknownCopy:
			if ack.ReceiptDigest != "" {
				return blocked("item %q is %s yet smuggles a receipt", ack.ItemID, string(ack.Outcome))
			}
			if strings.TrimSpace(ack.Detail) == "" {
				return blocked("item %q is %s with no reason: exceptions must stay reviewable", ack.ItemID, string(ack.Outcome))
			}
		}
		byItem[ack.ItemID] = ack
	}
	resolutions := make([]ItemResolution, 0, len(req.Items))
	for _, it := range req.Items {
		ack, answered := byItem[it.ItemID]
		switch {
		case answered && ack.Outcome == AckFulfilled:
			resolutions = append(resolutions, ItemResolution{
				ItemID: it.ItemID, Processor: it.Processor,
				Status: ItemResolved, AckDigest: ack.Digest(),
			})
		case answered:
			resolutions = append(resolutions, ItemResolution{
				ItemID: it.ItemID, Processor: it.Processor,
				Status: ItemException, AckDigest: ack.Digest(), Detail: ack.Detail,
			})
		case now.Before(it.DueAt):
			resolutions = append(resolutions, ItemResolution{
				ItemID: it.ItemID, Processor: it.Processor, Status: ItemPendingRetry,
			})
		default:
			resolutions = append(resolutions, ItemResolution{
				ItemID: it.ItemID, Processor: it.Processor, Status: ItemEscalated,
			})
		}
	}
	reqDigest := requestDigest(req)
	return Reconciliation{
		RequestID:     req.RequestID,
		RequestDigest: reqDigest,
		At:            now,
		Resolutions:   resolutions,
		Digest:        reconciliationDigest(reqDigest, now, resolutions),
	}, nil
}

// FulfillmentCertificate is the signed fulfillment package derived from one
// reconciliation. CertifiedItems holds only RESOLVED item ids;
// Exceptions preserves every EXCEPTION resolution verbatim; Complete is
// true only when no exception remains. Digest binds the whole package.
type FulfillmentCertificate struct {
	RequestID            string           `json:"request_id"`
	Certifier            string           `json:"certifier"`
	At                   values.Instant   `json:"at"`
	ReconciliationDigest string           `json:"reconciliation_digest"`
	CertifiedItems       []string         `json:"certified_items"`
	Exceptions           []ItemResolution `json:"exceptions"`
	Complete             bool             `json:"complete"`
	EvidenceID           string           `json:"evidence_id"`
	Digest               string           `json:"digest"`
}

// certificateDigest derives the package signature over every certified and
// preserved field.
func certificateDigest(recDigest, certifier string, at values.Instant, certified []string, exceptions []ItemResolution, complete bool) string {
	sec, nsec := at.Unix()
	dst := appendFields(nil,
		"reconciliation_digest", recDigest,
		"certifier", certifier,
		"at_sec", itoa(sec),
		"at_nsec", itoa(int64(nsec)),
		"complete", boolString(complete),
	)
	dst = appendStringSlice(dst, "certified_item", certified)
	for _, ex := range exceptions {
		dst = appendFields(dst,
			"exception_item", ex.ItemID,
			"exception_processor", ex.Processor,
			"exception_status", string(ex.Status),
			"exception_ack", ex.AckDigest,
			"exception_detail", ex.Detail,
		)
	}
	return digestHex(dst)
}

// Validate re-derives the package digest and checks the completion
// invariant: a Complete package preserves no exceptions, and every
// exception carries its ack digest and reason. A flipped flag or a dropped
// exception fails here.
func (c FulfillmentCertificate) Validate() error {
	if strings.TrimSpace(c.RequestID) == "" {
		return fmt.Errorf("%w: certificate names no request", ErrProcessorCertBlocked)
	}
	if strings.TrimSpace(c.Certifier) == "" {
		return fmt.Errorf("%w: certificate names no certifier", ErrProcessorCertBlocked)
	}
	if !c.At.IsSet() {
		return fmt.Errorf("%w: certificate records no instant", ErrProcessorCertBlocked)
	}
	if !isPayloadDigest(c.ReconciliationDigest) {
		return fmt.Errorf("%w: certificate binds no reconciliation", ErrProcessorCertBlocked)
	}
	if c.Complete && len(c.Exceptions) != 0 {
		return fmt.Errorf("%w: certificate claims completion with %d preserved exceptions", ErrProcessorCertBlocked, len(c.Exceptions))
	}
	for _, ex := range c.Exceptions {
		if ex.Status != ItemException {
			return fmt.Errorf("%w: certificate preserves non-exception status %q for item %q", ErrProcessorCertBlocked, string(ex.Status), ex.ItemID)
		}
		if !isPayloadDigest(ex.AckDigest) || strings.TrimSpace(ex.Detail) == "" {
			return fmt.Errorf("%w: certificate exception for item %q lost its evidence", ErrProcessorCertBlocked, ex.ItemID)
		}
	}
	if want := certificateDigest(c.ReconciliationDigest, c.Certifier, c.At, c.CertifiedItems, c.Exceptions, c.Complete); want != c.Digest {
		return fmt.Errorf("%w: certificate digest mismatch: package was altered after derivation", ErrProcessorCertBlocked)
	}
	return nil
}

// CertifyFulfillment derives the signed package from rec. Any PENDING_RETRY
// or ESCALATED item refuses with [ErrFulfillmentIncomplete] naming every
// unresolved item; EXCEPTION items are preserved verbatim and keep the
// package incomplete but reviewable. Only fully resolved items enter
// CertifiedItems, and Complete requires zero exceptions.
func CertifyFulfillment(rec Reconciliation, certifier string, at values.Instant) (FulfillmentCertificate, error) {
	if err := rec.Validate(); err != nil {
		return FulfillmentCertificate{}, err
	}
	if strings.TrimSpace(certifier) == "" {
		return FulfillmentCertificate{}, fmt.Errorf("%w: certification names no certifier", ErrProcessorCertBlocked)
	}
	if !at.IsSet() {
		return FulfillmentCertificate{}, fmt.Errorf("%w: certification records no instant", ErrProcessorCertBlocked)
	}
	var unresolved []string
	for _, r := range rec.Resolutions {
		if r.Status == ItemPendingRetry || r.Status == ItemEscalated {
			unresolved = append(unresolved, r.ItemID)
		}
	}
	if len(unresolved) != 0 {
		return FulfillmentCertificate{}, fmt.Errorf("%w: items awaiting acknowledgement cannot certify: %s", ErrFulfillmentIncomplete, strings.Join(unresolved, ", "))
	}
	cert := FulfillmentCertificate{
		RequestID:            rec.RequestID,
		Certifier:            certifier,
		At:                   at,
		ReconciliationDigest: rec.Digest,
	}
	for _, r := range rec.Resolutions {
		if r.Status == ItemResolved {
			cert.CertifiedItems = append(cert.CertifiedItems, r.ItemID)
		} else {
			cert.Exceptions = append(cert.Exceptions, r)
		}
	}
	cert.Complete = len(cert.Exceptions) == 0
	cert.Digest = certificateDigest(cert.ReconciliationDigest, cert.Certifier, cert.At, cert.CertifiedItems, cert.Exceptions, cert.Complete)
	cert.EvidenceID = processorCertEvidencePrefix + cert.Digest
	return cert, nil
}
