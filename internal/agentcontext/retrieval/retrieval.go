package retrieval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Kind identifies the owning system that supplied a retrieval record.
type Kind string

const (
	// Chat identifies a record retrieved by the Chat owner.
	Chat Kind = "CHAT"
	// Document identifies a record retrieved by the document owner.
	Document Kind = "DOCUMENT"
)

// State records whether one source completed its portion of a retrieval.
type State string

const (
	// Complete means the owner returned a valid, currently authorized page.
	Complete State = "COMPLETE"
	// Partial means some records passed the late check and others did not.
	Partial State = "PARTIAL"
	// Failed means no content from this source is available to the caller.
	Failed State = "FAILED"
)

// FailureCode is a deliberately bounded source failure, not a business fact.
type FailureCode string

const (
	FailureUnavailable   FailureCode = "SOURCE_UNAVAILABLE"
	FailureInvalid       FailureCode = "INVALID_ENVELOPE"
	FailureAuthorization FailureCode = "CURRENT_AUTHORIZATION_UNAVAILABLE"
	FailureRevoked       FailureCode = "CURRENT_GRANT_REVOKED"
)

var (
	ErrInvalidRequest = errors.New("agent retrieval: invalid request")
	ErrBatchMismatch  = errors.New("agent retrieval: batch does not match current context")
)

// Access is supplied by server-side invocation admission. Callers must not
// construct it from chat text or model output. GrantIDs and RevocationTokens
// pin the exact current authorization observed for this request.
type Access struct {
	TenantID, LegalEntityID, PrincipalID, InvocationID string
	ConversationID, Audience, Purpose                  string
	AuthorizationVersion                               string
	GrantIDs, RevocationTokens                         []string
}

// Query is a bounded owner request. Conversation and thread scope are hints
// only; each owner still enforces its own visibility policy.
type Query struct {
	Text, ConversationID, ThreadID string
	Limit                          int
	Sources                        []Kind
}

// OwnerRequest binds a source query to the admitted actor and purpose.
type OwnerRequest struct {
	Access Access
	Query  Query
}

// Citation points to an exact source version and owner-issued target.
type Citation struct {
	SourceID, Version, Target string
}

// Envelope is the typed provenance attached by a source owner to content.
type Envelope struct {
	Kind                                Kind
	Owner, TenantID, LegalEntityID      string
	SourceID, Version, Classification   string
	Audience                            []string
	Purpose, ConversationID, ThreadID   string
	ObservedAt, RetrievedAt, FreshUntil time.Time
	Citation                            Citation
	RevocationToken                     string
	Deleted, OnHold                     bool
}

// Record is content returned by an owner together with its mandatory
// provenance. Content is withheld from callers until Finalize succeeds.
type Record struct {
	Envelope Envelope
	Content  string
}

// ChatOwner retrieves only posts the principal can currently read.
type ChatOwner interface {
	RetrieveChat(context.Context, OwnerRequest) ([]Record, error)
}

// DocumentOwner retrieves only deployed documents within current requester
// and installation grants.
type DocumentOwner interface {
	RetrieveDocuments(context.Context, OwnerRequest) ([]Record, error)
}

// CurrentGrantChecker rechecks the current owner grants for one exact source
// version. It is called both after retrieval and immediately before content is
// released for agent use or disclosure.
type CurrentGrantChecker interface {
	CheckCurrent(context.Context, Access, Envelope) error
}

// Config wires source-owner adapters, the current-grant checker, and a clock.
type Config struct {
	Chat      ChatOwner
	Documents DocumentOwner
	Grants    CurrentGrantChecker
	Now       func() time.Time
}

// Service retrieves and reauthorizes source records without joining source
// databases or treating a failed source as an empty result.
type Service struct {
	chat      ChatOwner
	documents DocumentOwner
	grants    CurrentGrantChecker
	now       func() time.Time
}

// New constructs a retrieval service. A missing source is reported as an
// explicit per-source failure; the grant checker and clock are mandatory.
func New(cfg Config) (*Service, error) {
	if cfg.Grants == nil || cfg.Now == nil {
		return nil, fmt.Errorf("%w: grant checker and clock are required", ErrInvalidRequest)
	}
	return &Service{chat: cfg.Chat, documents: cfg.Documents, grants: cfg.Grants, now: cfg.Now}, nil
}

// SourceStatus reports success or a typed partial failure without exposing a
// hidden record, citation, or source-specific error detail.
type SourceStatus struct {
	Kind  Kind
	State State
	Code  FailureCode
}

// Batch holds validated records privately until the service performs its
// final current-grant check. Only metadata status is readable beforehand.
type Batch struct {
	accessDigest string
	items        []Record
	statuses     []SourceStatus
}

// Statuses returns detached per-source outcomes. Failed sources are explicit
// and must not be interpreted as evidence that no matching fact exists.
func (b Batch) Statuses() []SourceStatus { return slices.Clone(b.statuses) }

// Count reports the number of records held for a later grant recheck without
// exposing their identities or content.
func (b Batch) Count() int { return len(b.items) }

// Disclosure contains records that passed the current-grant recheck. Its
// statuses preserve partial failures from retrieval and final authorization.
type Disclosure struct {
	items    []Record
	statuses []SourceStatus
}

// Items returns detached records that were authorized immediately before
// this disclosure was created.
func (d Disclosure) Items() []Record {
	out := cloneRecords(d.items)
	return out
}

// Statuses returns detached source outcomes, including partial failures.
func (d Disclosure) Statuses() []SourceStatus { return slices.Clone(d.statuses) }

// Retrieve asks each requested source owner independently, validates its
// envelope, and checks current grants before retaining any content. If one
// owner fails, successful results from the other owner remain available with
// an explicit failure status.
func (s *Service) Retrieve(ctx context.Context, access Access, query Query) (Batch, error) {
	if s == nil || s.grants == nil || s.now == nil {
		return Batch{}, fmt.Errorf("%w: service is incomplete", ErrInvalidRequest)
	}
	if err := validateRequest(access, query); err != nil {
		return Batch{}, err
	}
	query.Sources = normalizedSources(query.Sources)
	request := OwnerRequest{Access: cloneAccess(access), Query: cloneQuery(query)}
	batch := Batch{accessDigest: accessDigest(access)}
	for _, kind := range query.Sources {
		owner, err := s.owner(kind)
		if err != nil {
			batch.statuses = append(batch.statuses, SourceStatus{Kind: kind, State: Failed, Code: FailureUnavailable})
			continue
		}
		records, err := owner(ctx, request)
		if err != nil {
			batch.statuses = append(batch.statuses, SourceStatus{Kind: kind, State: Failed, Code: FailureUnavailable})
			continue
		}
		if err := validateRecords(records, kind, access, query, s.now().UTC()); err != nil {
			batch.statuses = append(batch.statuses, SourceStatus{Kind: kind, State: Failed, Code: FailureInvalid})
			continue
		}
		if err := s.checkAll(ctx, access, records); err != nil {
			batch.statuses = append(batch.statuses, SourceStatus{Kind: kind, State: Failed, Code: failureFor(err)})
			continue
		}
		batch.items = append(batch.items, cloneRecords(records)...)
		batch.statuses = append(batch.statuses, SourceStatus{Kind: kind, State: Complete})
	}
	return batch, nil
}

// Finalize performs the mandatory late grant check and returns content only
// for records still authorized now. This must be called immediately before
// agent use or disclosure; owner or grant failures stay explicit.
func (s *Service) Finalize(ctx context.Context, access Access, batch Batch) (Disclosure, error) {
	if s == nil || s.grants == nil || batch.accessDigest == "" || batch.accessDigest != accessDigest(access) {
		return Disclosure{}, ErrBatchMismatch
	}
	result := Disclosure{statuses: slices.Clone(batch.statuses)}
	bySource := make(map[Kind][]Record)
	for _, item := range batch.items {
		bySource[item.Envelope.Kind] = append(bySource[item.Envelope.Kind], item)
	}
	for _, status := range batch.statuses {
		if status.State != Complete {
			continue
		}
		var denied bool
		var allowed int
		var failure FailureCode
		for _, item := range bySource[status.Kind] {
			if err := s.grants.CheckCurrent(ctx, access, item.Envelope); err != nil {
				denied = true
				failure = failureFor(err)
				continue
			}
			result.items = append(result.items, cloneRecord(item))
			allowed++
		}
		if denied {
			state := Partial
			if allowed == 0 {
				state = Failed
			}
			result.statuses = replaceStatus(result.statuses, SourceStatus{Kind: status.Kind, State: state, Code: failure})
		}
	}
	return result, nil
}

func (s *Service) owner(kind Kind) (func(context.Context, OwnerRequest) ([]Record, error), error) {
	switch kind {
	case Chat:
		if s.chat == nil {
			return nil, ErrInvalidRequest
		}
		return s.chat.RetrieveChat, nil
	case Document:
		if s.documents == nil {
			return nil, ErrInvalidRequest
		}
		return s.documents.RetrieveDocuments, nil
	default:
		return nil, ErrInvalidRequest
	}
}

func (s *Service) checkAll(ctx context.Context, access Access, records []Record) error {
	for _, record := range records {
		if err := s.grants.CheckCurrent(ctx, access, record.Envelope); err != nil {
			return err
		}
	}
	return nil
}

func validateRequest(access Access, query Query) error {
	for name, value := range map[string]string{
		"tenant": access.TenantID, "principal": access.PrincipalID,
		"invocation": access.InvocationID, "conversation": access.ConversationID,
		"audience": access.Audience, "purpose": access.Purpose,
		"authorization version": access.AuthorizationVersion,
		"query":                 query.Text,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidRequest, name)
		}
	}
	if query.Limit < 1 || query.Limit > 100 || len(access.GrantIDs) == 0 || len(access.RevocationTokens) == 0 {
		return fmt.Errorf("%w: limit and current grant evidence are required", ErrInvalidRequest)
	}
	if query.ConversationID != access.ConversationID {
		return fmt.Errorf("%w: query conversation must match admitted conversation", ErrInvalidRequest)
	}
	if !validValues(access.GrantIDs) || !validValues(access.RevocationTokens) {
		return fmt.Errorf("%w: current grant evidence is incomplete", ErrInvalidRequest)
	}
	seen := map[Kind]bool{}
	for _, kind := range normalizedSources(query.Sources) {
		if (kind != Chat && kind != Document) || seen[kind] {
			return fmt.Errorf("%w: unsupported or duplicate source", ErrInvalidRequest)
		}
		seen[kind] = true
	}
	return nil
}

func validateRecords(records []Record, kind Kind, access Access, query Query, now time.Time) error {
	if len(records) > query.Limit {
		return ErrInvalidRequest
	}
	for _, record := range records {
		e := record.Envelope
		if strings.TrimSpace(record.Content) == "" || e.Kind != kind || strings.TrimSpace(e.Owner) == "" || e.TenantID != access.TenantID || strings.TrimSpace(e.SourceID) == "" || strings.TrimSpace(e.Version) == "" || strings.TrimSpace(e.Classification) == "" || !validValues(e.Audience) || !contains(e.Audience, access.Audience) || e.Purpose != access.Purpose || e.ObservedAt.IsZero() || e.RetrievedAt.IsZero() || e.FreshUntil.IsZero() || e.ObservedAt.After(e.RetrievedAt) || e.RetrievedAt.After(now) || !now.Before(e.FreshUntil) || e.Deleted || e.OnHold || strings.TrimSpace(e.RevocationToken) == "" || e.Citation.SourceID != e.SourceID || e.Citation.Version != e.Version || strings.TrimSpace(e.Citation.Target) == "" {
			return ErrInvalidRequest
		}
		if access.LegalEntityID != "" && e.LegalEntityID != access.LegalEntityID {
			return ErrInvalidRequest
		}
		if kind == Chat && (e.ConversationID != access.ConversationID || (query.ThreadID != "" && e.ThreadID != query.ThreadID)) {
			return ErrInvalidRequest
		}
	}
	return nil
}

func normalizedSources(in []Kind) []Kind {
	if len(in) == 0 {
		return []Kind{Chat, Document}
	}
	return slices.Clone(in)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func validValues(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

func failureFor(err error) FailureCode {
	if errors.Is(err, ErrGrantRevoked) {
		return FailureRevoked
	}
	return FailureAuthorization
}

// ErrGrantRevoked is returned by a CurrentGrantChecker when the pinned source
// grant or revocation token is no longer current.
var ErrGrantRevoked = errors.New("agent retrieval: current grant revoked")

func replaceStatus(statuses []SourceStatus, replacement SourceStatus) []SourceStatus {
	for i := range statuses {
		if statuses[i].Kind == replacement.Kind {
			statuses[i] = replacement
			return statuses
		}
	}
	return append(statuses, replacement)
}

func cloneAccess(a Access) Access {
	a.GrantIDs = slices.Clone(a.GrantIDs)
	a.RevocationTokens = slices.Clone(a.RevocationTokens)
	return a
}

func cloneQuery(q Query) Query { q.Sources = slices.Clone(q.Sources); return q }

func cloneRecords(records []Record) []Record {
	out := make([]Record, len(records))
	for i, record := range records {
		out[i] = cloneRecord(record)
	}
	return out
}

func cloneRecord(record Record) Record {
	record.Envelope.Audience = slices.Clone(record.Envelope.Audience)
	return record
}

func accessDigest(access Access) string {
	parts := []string{access.TenantID, access.LegalEntityID, access.PrincipalID, access.InvocationID, access.ConversationID, access.Audience, access.Purpose, access.AuthorizationVersion}
	parts = append(parts, access.GrantIDs...)
	parts = append(parts, access.RevocationTokens...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
