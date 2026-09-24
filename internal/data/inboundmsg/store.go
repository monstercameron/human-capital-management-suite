// Package inboundmsg is the tenant- and thread-scoped store for the governed
// inbound-reply tables migration 00035 creates (MSG-011).
//
// It owns two tables: inbound_message, the append-only record of what a
// channel delivered, and reply_binding, the one compare-and-swap-fenced
// resolution attempt that decides which recipient_message (migration
// 00031's per-recipient copy of a governed MessageIntent) an inbound message
// actually replies to.
//
// # Content stays a reference, never a body
//
// inbound_message never stores the raw inbound content. It carries a sha256
// content_digest and a content_ref into the governed object store, the same
// discipline migration 00031's delivery_endpoint already applies to an
// address: the digest proves what arrived without this row itself becoming
// a place hostile content can be read back from.
//
// # Resolution never guesses
//
// A reply's own claimed thread (inbound_message.thread_id) is never trusted
// to say which outbound message it answers -- an inbound channel is
// adversarial by construction. [Store.Bind] is the only path a
// reply_binding row can leave UNRESOLVED: it looks up the recipient_message
// that the channel's own correlation token names, and only reaches BOUND
// when that message exists in this same tenant, belongs to the participant
// claimed to be replying, and has the exact thread id claimed by the inbound
// receipt (migration 00362). Any other outcome -- no match, a
// token that happens to belong to a different tenant (which simply never
// appears in a tenant-scoped lookup) or to a different participant's
// message -- reaches REJECTED with a reason, never BOUND. The schema itself
// backs this: reply_binding_bound_requires_recipient means no row can name a
// recipient_message unless its state is BOUND, and
// reply_binding_unresolved_has_no_recipient means an UNRESOLVED row never
// does either.
//
// [Store.Bind] returns an error only when the compare-and-swap or its
// arguments are themselves invalid (not found, wrong expected version,
// already resolved); a token that fails to resolve is not an error; it is a
// successful evaluation that reaches state REJECTED, and the returned
// [ReplyBinding] reports that explicitly.
//
// # Thread visibility
//
// [Store.ListThread] checks active migration 00031 thread_participant
// membership and only returns messages whose binding is BOUND to a
// recipient_message carrying the same canonical thread id (migration 00362).
// A forged thread claim or unresolved/rejected reply is invisible.
//
// # Executor
//
// Executor is the minimal database capability this store needs: exec and
// query, nothing more. A [dbport.Tx] and a [dbport.Conn] both satisfy it.
// Both tables are row-level-security protected, so the caller must have
// scoped its transaction with internal/data/tenancy.WithTenant before
// calling anything here; a store that opened its own connection could not
// guarantee that.
package inboundmsg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// The sentinels this package classifies failures with.
var (
	// ErrInvalid reports a row or argument that is not internally
	// consistent. It is returned before any statement runs.
	ErrInvalid = errors.New("inboundmsg: invalid row")

	// ErrNotFound reports an inbound message or reply binding that does not
	// exist for the given tenant.
	ErrNotFound = errors.New("inboundmsg: not found")

	// ErrVersionConflict reports a Bind whose expected version did not
	// match the binding's live version. Nothing was written.
	ErrVersionConflict = errors.New("inboundmsg: version conflict")

	// ErrAlreadyResolved reports a Bind attempted against a binding that has
	// already left UNRESOLVED. A binding resolves exactly once.
	ErrAlreadyResolved = errors.New("inboundmsg: binding already resolved")

	// ErrReplayConflict reports provider reuse of one message identity for
	// different immutable receipt bytes or correlation metadata.
	ErrReplayConflict = errors.New("inboundmsg: provider message replay conflicts with stored receipt")
)

// Reply-binding states reply_binding.state may hold.
const (
	Unresolved = "UNRESOLVED"
	Bound      = "BOUND"
	Rejected   = "REJECTED"
)

// Channels is the channel vocabulary inbound_message allows, matching
// migration 00031's delivery_endpoint_channel_allowed constraint.
var Channels = []string{"EMAIL", "SMS", "PUSH", "INBOX", "VOICE", "POSTAL", "WEBHOOK"}

// Executor is the minimal database capability the inbound-message store
// needs. See the package doc for why the caller, not this package, is
// responsible for tenant scoping.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

func isNoRows(err error) bool { return errors.Is(err, dbport.ErrNoRows) }

func invalid(field, detail string) error {
	return fmt.Errorf("%w: %s: %s", ErrInvalid, field, detail)
}

func oneOf(value string, allowed ...string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}

// InboundMessage is one append-only record of what a channel delivered.
type InboundMessage struct {
	TenantID             uuid.UUID
	InboundMessageID     uuid.UUID
	ThreadID             uuid.UUID
	Channel              string
	SenderEndpointDigest string
	ReceivedAt           time.Time
	ProviderMessageID    string
	ContentDigest        string
	ContentRef           string
	Classification       string
	CreatedAt            time.Time
}

func (m InboundMessage) validate() error {
	if m.TenantID == uuid.Nil || m.ThreadID == uuid.Nil {
		return invalid("thread_id", "tenant and thread id are required")
	}
	if !oneOf(m.Channel, Channels...) {
		return invalid("channel", "value is not a declared channel")
	}
	if m.SenderEndpointDigest == "" {
		return invalid("sender_endpoint_digest", "a digest is required, never the raw address")
	}
	if m.ReceivedAt.IsZero() {
		return invalid("received_at", "timestamp is unset")
	}
	if m.ProviderMessageID == "" {
		return invalid("provider_message_id", "the channel's own message id is required for dedupe")
	}
	if m.ContentDigest == "" {
		return invalid("content_digest", "a digest is required")
	}
	if m.ContentRef == "" {
		return invalid("content_ref", "content is stored as a governed reference, never the raw body")
	}
	if m.Classification == "" {
		return invalid("classification", "classification is required")
	}
	return nil
}

// ReplyBinding is the one compare-and-swap-fenced resolution attempt for one
// inbound message.
type ReplyBinding struct {
	TenantID           uuid.UUID
	ReplyBindingID     uuid.UUID
	InboundMessageID   uuid.UUID
	CorrelationToken   string
	RecipientMessageID *uuid.UUID
	State              string
	RejectionReason    string
	ResolvedAt         *time.Time
	Version            uint64
	CreatedAt          time.Time
}

// Store writes and resolves inbound_message and reply_binding.
type Store struct{}

const selectInboundMessageColumns = `tenant_id, inbound_message_id, thread_id, channel, sender_endpoint_digest,
			received_at, provider_message_id, content_digest, content_ref, classification, created_at`

func scanInboundMessage(row dbport.Row) (InboundMessage, error) {
	var m InboundMessage
	if err := row.Scan(&m.TenantID, &m.InboundMessageID, &m.ThreadID, &m.Channel, &m.SenderEndpointDigest,
		&m.ReceivedAt, &m.ProviderMessageID, &m.ContentDigest, &m.ContentRef, &m.Classification, &m.CreatedAt); err != nil {
		return InboundMessage{}, err
	}
	m.ReceivedAt = m.ReceivedAt.UTC()
	m.CreatedAt = m.CreatedAt.UTC()
	return m, nil
}

// Ingest inserts one inbound message and seeds its initial UNRESOLVED
// reply_binding row, keyed idempotently by (tenant, channel,
// provider_message_id): redelivery of the same provider message (a webhook
// retry, an IMAP re-sync) returns the row that already exists, with created
// = false, rather than erroring or creating a second copy. correlationToken
// is the channel's own reply-correlation token (for example an echoed
// reply-to header); it is stored on the binding so a later [Store.Bind] can
// resolve it.
func (s Store) Ingest(ctx context.Context, ex Executor, in InboundMessage, correlationToken string) (InboundMessage, bool, error) {
	if err := in.validate(); err != nil {
		return InboundMessage{}, false, err
	}
	if correlationToken == "" {
		return InboundMessage{}, false, invalid("correlation_token", "an inbound message always carries the channel's own correlation token")
	}
	if in.InboundMessageID == uuid.Nil {
		in.InboundMessageID = uuid.New()
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = in.ReceivedAt
	}

	row := ex.QueryRow(ctx, `
		INSERT INTO inbound_message (
			tenant_id, inbound_message_id, thread_id, channel,
			sender_endpoint_digest, received_at, provider_message_id,
			content_digest, content_ref, classification, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (tenant_id, channel, provider_message_id) DO NOTHING
		RETURNING inbound_message_id`,
		in.TenantID, in.InboundMessageID, in.ThreadID, in.Channel,
		in.SenderEndpointDigest, in.ReceivedAt.UTC(), in.ProviderMessageID,
		in.ContentDigest, in.ContentRef, in.Classification, in.CreatedAt.UTC())

	var stored uuid.UUID
	if err := row.Scan(&stored); err != nil {
		if !isNoRows(err) {
			return InboundMessage{}, false, fmt.Errorf("inboundmsg: ingest %s/%s: %w", in.Channel, in.ProviderMessageID, err)
		}
		existing, loadErr := s.loadByProvider(ctx, ex, in.TenantID, in.Channel, in.ProviderMessageID)
		if loadErr != nil {
			return InboundMessage{}, false, loadErr
		}
		if !sameInboundReceipt(existing, in) {
			return InboundMessage{}, false, fmt.Errorf("%w: %s/%s", ErrReplayConflict, in.Channel, in.ProviderMessageID)
		}
		binding, loadErr := s.LoadBinding(ctx, ex, in.TenantID, existing.InboundMessageID)
		if loadErr != nil {
			return InboundMessage{}, false, loadErr
		}
		if binding.CorrelationToken != correlationToken {
			return InboundMessage{}, false, fmt.Errorf("%w: correlation token changed for %s/%s", ErrReplayConflict, in.Channel, in.ProviderMessageID)
		}
		return existing, false, nil
	}
	in.InboundMessageID = stored

	if _, err := ex.Exec(ctx, `
		INSERT INTO reply_binding (tenant_id, reply_binding_id, inbound_message_id, correlation_token)
		VALUES ($1, $2, $3, $4)`,
		in.TenantID, uuid.New(), in.InboundMessageID, correlationToken); err != nil {
		return InboundMessage{}, false, fmt.Errorf("inboundmsg: seed binding for %s: %w", in.InboundMessageID, err)
	}
	return in, true, nil
}

func sameInboundReceipt(stored, candidate InboundMessage) bool {
	return stored.ThreadID == candidate.ThreadID &&
		stored.SenderEndpointDigest == candidate.SenderEndpointDigest &&
		stored.ReceivedAt.Equal(candidate.ReceivedAt) &&
		stored.ContentDigest == candidate.ContentDigest &&
		stored.ContentRef == candidate.ContentRef &&
		stored.Classification == candidate.Classification
}

// LoadMessage returns one inbound message by its id.
func (s Store) LoadMessage(ctx context.Context, ex Executor, tenantID, inboundMessageID uuid.UUID) (InboundMessage, error) {
	if tenantID == uuid.Nil || inboundMessageID == uuid.Nil {
		return InboundMessage{}, invalid("inbound_message_id", "tenant and message id are required")
	}
	m, err := scanInboundMessage(ex.QueryRow(ctx, `SELECT `+selectInboundMessageColumns+`
		FROM inbound_message WHERE tenant_id=$1 AND inbound_message_id=$2`, tenantID, inboundMessageID))
	if err != nil {
		if isNoRows(err) {
			return InboundMessage{}, fmt.Errorf("%w: inbound_message %s", ErrNotFound, inboundMessageID)
		}
		return InboundMessage{}, fmt.Errorf("inboundmsg: load message %s: %w", inboundMessageID, err)
	}
	return m, nil
}

func (s Store) loadByProvider(ctx context.Context, ex Executor, tenantID uuid.UUID, channel, providerMessageID string) (InboundMessage, error) {
	m, err := scanInboundMessage(ex.QueryRow(ctx, `SELECT `+selectInboundMessageColumns+`
		FROM inbound_message WHERE tenant_id=$1 AND channel=$2 AND provider_message_id=$3`,
		tenantID, channel, providerMessageID))
	if err != nil {
		if isNoRows(err) {
			return InboundMessage{}, fmt.Errorf("%w: inbound_message channel=%s provider=%s", ErrNotFound, channel, providerMessageID)
		}
		return InboundMessage{}, fmt.Errorf("inboundmsg: load by provider %s/%s: %w", channel, providerMessageID, err)
	}
	return m, nil
}

// LoadBinding returns one inbound message's reply_binding row.
func (s Store) LoadBinding(ctx context.Context, ex Executor, tenantID, inboundMessageID uuid.UUID) (ReplyBinding, error) {
	if tenantID == uuid.Nil || inboundMessageID == uuid.Nil {
		return ReplyBinding{}, invalid("inbound_message_id", "tenant and message id are required")
	}
	var (
		b          ReplyBinding
		recipient  *uuid.UUID
		resolvedAt *time.Time
		version    int64
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, reply_binding_id, inbound_message_id, correlation_token,
			recipient_message_id, state, rejection_reason, resolved_at, version, created_at
		FROM reply_binding WHERE tenant_id=$1 AND inbound_message_id=$2`, tenantID, inboundMessageID).
		Scan(&b.TenantID, &b.ReplyBindingID, &b.InboundMessageID, &b.CorrelationToken,
			&recipient, &b.State, &b.RejectionReason, &resolvedAt, &version, &b.CreatedAt)
	if err != nil {
		if isNoRows(err) {
			return ReplyBinding{}, fmt.Errorf("%w: reply_binding for inbound_message %s", ErrNotFound, inboundMessageID)
		}
		return ReplyBinding{}, fmt.Errorf("inboundmsg: load binding %s: %w", inboundMessageID, err)
	}
	b.RecipientMessageID = recipient
	b.ResolvedAt = resolvedAt
	b.Version = uint64(version)
	b.CreatedAt = b.CreatedAt.UTC()
	return b, nil
}

// Bind is the only path a reply_binding row can leave UNRESOLVED. It
// resolves correlationToken (read from the binding itself, not supplied by
// the caller, so a caller cannot bind to a token it did not actually
// ingest) against this tenant's own recipient_message rows:
//
//   - no match, or more than one match, refuses the bind: the binding moves
//     to REJECTED with a reason and never BOUND.
//   - a match whose recipient_ref does not equal claimedSenderRef refuses
//     the bind the same way -- this is what stops a reply token that names
//     another participant's message from resuming that participant's
//     workflow.
//   - a token that happens to equal another tenant's correlation_key value
//     never appears in the lookup at all, because the query is scoped to
//     tenantID; that case is indistinguishable from "no match" and reaches
//     REJECTED identically.
//   - exactly one match whose recipient_ref does equal claimedSenderRef
//     moves the binding to BOUND, recording that recipient_message_id.
//
// The compare-and-swap itself only ever succeeds once: the update's WHERE
// clause requires state = 'UNRESOLVED' and the caller's expectedVersion, so
// a second Bind against an already-resolved or concurrently-resolved
// binding reports [ErrAlreadyResolved] or [ErrVersionConflict] and writes
// nothing. Bind returns a non-nil error only for those structural failures;
// a token that fails to resolve is a successful evaluation, reported by the
// returned [ReplyBinding]'s State, not by an error.
func (s Store) Bind(ctx context.Context, ex Executor, tenantID, inboundMessageID uuid.UUID, claimedSenderRef string, expectedVersion uint64, at time.Time) (ReplyBinding, error) {
	if tenantID == uuid.Nil || inboundMessageID == uuid.Nil {
		return ReplyBinding{}, invalid("inbound_message_id", "tenant and inbound message id are required")
	}
	if claimedSenderRef == "" {
		return ReplyBinding{}, invalid("claimed_sender_ref", "a bind attempt always names who is claimed to be replying")
	}
	if expectedVersion == 0 {
		return ReplyBinding{}, invalid("expected_version", "a compare-and-swap needs the version it expects")
	}
	if at.IsZero() {
		return ReplyBinding{}, invalid("occurred_at", "timestamp is unset")
	}

	var (
		correlationToken string
		state            string
		version          int64
		claimedThreadID  uuid.UUID
	)
	err := ex.QueryRow(ctx, `
		SELECT rb.correlation_token, rb.state, rb.version, im.thread_id
		FROM reply_binding rb
		JOIN inbound_message im USING (tenant_id, inbound_message_id)
		WHERE rb.tenant_id=$1 AND rb.inbound_message_id=$2`,
		tenantID, inboundMessageID).Scan(&correlationToken, &state, &version, &claimedThreadID)
	if err != nil {
		if isNoRows(err) {
			return ReplyBinding{}, fmt.Errorf("%w: reply_binding for inbound_message %s", ErrNotFound, inboundMessageID)
		}
		return ReplyBinding{}, fmt.Errorf("inboundmsg: load binding %s: %w", inboundMessageID, err)
	}
	if state != Unresolved {
		return ReplyBinding{}, fmt.Errorf("%w: reply_binding %s is already %s", ErrAlreadyResolved, inboundMessageID, state)
	}
	if uint64(version) != expectedVersion {
		return ReplyBinding{}, fmt.Errorf("%w: reply_binding %s expected version %d, has %d", ErrVersionConflict, inboundMessageID, expectedVersion, version)
	}

	rows, err := ex.Query(ctx, `
		SELECT recipient_message_id, recipient_ref, conversation_thread_id
		FROM recipient_message
		WHERE tenant_id=$1 AND correlation_key=$2`,
		tenantID, correlationToken)
	if err != nil {
		return ReplyBinding{}, fmt.Errorf("inboundmsg: resolve correlation %s: %w", correlationToken, err)
	}
	var (
		matches              int
		recipientMessageID   uuid.UUID
		recipientRef         string
		conversationThreadID *uuid.UUID
	)
	for rows.Next() {
		matches++
		if scanErr := rows.Scan(&recipientMessageID, &recipientRef, &conversationThreadID); scanErr != nil {
			rows.Close()
			return ReplyBinding{}, fmt.Errorf("inboundmsg: scan correlation match: %w", scanErr)
		}
	}
	if scanErr := rows.Err(); scanErr != nil {
		rows.Close()
		return ReplyBinding{}, fmt.Errorf("inboundmsg: resolve correlation %s: %w", correlationToken, scanErr)
	}
	rows.Close()

	var (
		newState       string
		reason         string
		boundRecipient *uuid.UUID
	)
	switch {
	case matches == 0:
		newState = Rejected
		reason = fmt.Sprintf("correlation token %q does not resolve to any message in this tenant", correlationToken)
	case matches > 1:
		newState = Rejected
		reason = fmt.Sprintf("correlation token %q resolves to more than one message", correlationToken)
	case recipientRef != claimedSenderRef:
		newState = Rejected
		reason = fmt.Sprintf("correlation token names recipient %q, not the claimed sender %q", recipientRef, claimedSenderRef)
	case conversationThreadID == nil:
		newState = Rejected
		reason = "correlated recipient message has no canonical conversation thread"
	case *conversationThreadID != claimedThreadID:
		newState = Rejected
		reason = "claimed conversation thread does not match the correlated recipient message"
	default:
		newState = Bound
		id := recipientMessageID
		boundRecipient = &id
	}

	affected, err := ex.Exec(ctx, `
		UPDATE reply_binding
		SET state=$4, recipient_message_id=$5, rejection_reason=$6, resolved_at=$7, version=version+1
		WHERE tenant_id=$1 AND inbound_message_id=$2 AND state='UNRESOLVED' AND version=$3`,
		tenantID, inboundMessageID, int64(expectedVersion), newState, boundRecipient, reason, at.UTC())
	if err != nil {
		return ReplyBinding{}, fmt.Errorf("inboundmsg: resolve binding %s: %w", inboundMessageID, err)
	}
	if affected == 0 {
		return ReplyBinding{}, fmt.Errorf("%w: reply_binding %s expected version %d", ErrVersionConflict, inboundMessageID, expectedVersion)
	}

	resolvedAt := at.UTC()
	return ReplyBinding{
		TenantID:           tenantID,
		InboundMessageID:   inboundMessageID,
		CorrelationToken:   correlationToken,
		RecipientMessageID: boundRecipient,
		State:              newState,
		RejectionReason:    reason,
		ResolvedAt:         &resolvedAt,
		Version:            expectedVersion + 1,
	}, nil
}

// ListThread returns every inbound message belonging to one thread, oldest
// first, but only when participantRef is an active thread_participant of
// threadID. A caller who is not an active member of that thread observes an
// empty slice and no error: this is deliberately indistinguishable from a
// thread that exists but carries no inbound messages, or does not exist at
// all, matching internal/data/inbox's own non-leaking shape for a wrong
// subject.
func (s Store) ListThread(ctx context.Context, ex Executor, tenantID uuid.UUID, participantRef string, threadID uuid.UUID) ([]InboundMessage, error) {
	if tenantID == uuid.Nil || threadID == uuid.Nil {
		return nil, invalid("thread_id", "tenant and thread id are required")
	}
	if participantRef == "" {
		return nil, invalid("participant_ref", "a thread list always names the participant reading it")
	}

	var member bool
	if err := ex.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM thread_participant
			WHERE tenant_id=$1 AND thread_id=$2 AND principal_ref=$3 AND status='ACTIVE'
		)`, tenantID, threadID, participantRef).Scan(&member); err != nil {
		return nil, fmt.Errorf("inboundmsg: check thread membership %s: %w", threadID, err)
	}
	if !member {
		return nil, nil
	}

	rows, err := ex.Query(ctx, `SELECT `+selectInboundMessageColumns+`
		FROM inbound_message im
		WHERE im.tenant_id=$1 AND im.thread_id=$2
		  AND EXISTS (
			SELECT 1 FROM reply_binding rb
			WHERE rb.tenant_id=im.tenant_id AND rb.inbound_message_id=im.inbound_message_id
			  AND rb.state='BOUND'
		  AND rb.recipient_message_id IN (
			SELECT rm.recipient_message_id FROM recipient_message rm
			WHERE rm.tenant_id=im.tenant_id AND rm.conversation_thread_id=im.thread_id
		  )
		  )
		ORDER BY received_at, inbound_message_id`, tenantID, threadID)
	if err != nil {
		return nil, fmt.Errorf("inboundmsg: list thread %s: %w", threadID, err)
	}
	defer rows.Close()

	var out []InboundMessage
	for rows.Next() {
		m, scanErr := scanInboundMessage(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("inboundmsg: scan thread %s: %w", threadID, scanErr)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inboundmsg: list thread %s: %w", threadID, err)
	}
	return out, nil
}
