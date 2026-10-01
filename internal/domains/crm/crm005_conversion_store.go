package crm

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ConversionRole is one role record created by a prospect conversion. The
// source attribution and consent are copied into every role so a recruiting
// reader never has to infer lineage from the prospect after conversion.
type ConversionRole struct {
	Ref         values.EntityRef
	Prospect    values.EntityRef
	Person      values.EntityRef
	Source      ProspectSourceAttribution
	Consent     ProspectConsent
	IdentityRef string
}

// ConversionRecord is the atomic recruiting result. Candidate, application
// and identity-link are separate role records in the domain model, but this
// value is exposed as one commit result so callers cannot observe a partial
// conversion.
type ConversionRecord struct {
	IdempotencyKey string
	Fence          ConversionCommitFence
	Candidate      ConversionRole
	Application    ConversionRole
	IdentityLink   ConversionRole
}

// ConversionStore is the kernel-pure reference recruiting writer. A deployed
// adapter can implement the same ports over durable candidate/application
// tables; this store provides the application composition with atomic,
// tenant-scoped and replay-safe semantics rather than a fixture writer.
type ConversionStore struct {
	mu           sync.Mutex
	byKey        map[string]storedConversion
	candidates   map[string]ConversionRole
	applications map[string]ConversionRole
	links        map[string]ConversionRole
}

type storedConversion struct {
	write  ConversionWrite
	result ConversionResult
	record ConversionRecord
}

var (
	_ ConversionAuthority = (*ConversionStore)(nil)
	_ ConversionWriter    = (*ConversionStore)(nil)
)

// NewConversionStore returns an empty tenant-scoped conversion writer.
func NewConversionStore() *ConversionStore {
	return &ConversionStore{
		byKey:        make(map[string]storedConversion),
		candidates:   make(map[string]ConversionRole),
		applications: make(map[string]ConversionRole),
		links:        make(map[string]ConversionRole),
	}
}

// AuthorizeProspectConversion is the in-process governed-intent authority.
// It deliberately revalidates the immutable proposal immediately before the
// write; authorization is never inferred from a caller's earlier preflight.
func (s *ConversionStore) AuthorizeProspectConversion(ctx context.Context, proposal CandidateConversionProposal) error {
	if err := conversionContextError(ctx); err != nil {
		return err
	}
	if s == nil {
		return fmt.Errorf("%w: conversion store is unavailable", ErrCRM005Rejected)
	}
	if err := validateConversionProposal(proposal); err != nil {
		return err
	}
	return nil
}

// CommitProspectConversion atomically creates all three recruiting roles.
// Exact replay returns the original result without another role write;
// reusing an idempotency key or role reference with different content is
// rejected before any map is changed.
func (s *ConversionStore) CommitProspectConversion(ctx context.Context, in ConversionWrite) (ConversionResult, error) {
	if err := conversionContextError(ctx); err != nil {
		return ConversionResult{}, err
	}
	if s == nil || in.Identity == nil {
		return ConversionResult{}, fmt.Errorf("%w: resolved identity is required", ErrCRM005Rejected)
	}
	if err := validateConversionProposal(in.Proposal); err != nil {
		return ConversionResult{}, err
	}
	if err := validateConversionIdentity(in.Proposal, *in.Identity); err != nil {
		return ConversionResult{}, err
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" || strings.TrimSpace(in.Fence.Token) == "" || !in.Fence.ProspectRevision.Equal(in.Proposal.Prospect.Revision) {
		return ConversionResult{}, fmt.Errorf("%w: invalid conversion fence or idempotency key", ErrCRM005Rejected)
	}

	record := ConversionRecord{
		IdempotencyKey: in.IdempotencyKey,
		Fence:          in.Fence,
		Candidate:      conversionRole(in.Proposal, *in.Identity, in.Proposal.ProposedCandidate),
		Application:    conversionRole(in.Proposal, *in.Identity, in.Proposal.ProposedApplication),
		IdentityLink:   conversionRole(in.Proposal, *in.Identity, in.Proposal.ProposedIdentityLink),
	}
	result := ConversionResult{Candidate: record.Candidate.Ref, Application: record.Application.Ref, IdentityLink: record.IdentityLink.Ref}
	key := conversionKey(in.Proposal.Prospect.ProspectID.Tenant, in.IdempotencyKey)

	s.mu.Lock()
	defer s.mu.Unlock()
	if previous, ok := s.byKey[key]; ok {
		if !reflect.DeepEqual(previous.write, in) {
			return ConversionResult{}, fmt.Errorf("%w: idempotency key was reused with different content", ErrCRM005Rejected)
		}
		result.Replayed = true
		return result, nil
	}
	if _, ok := s.candidates[record.Candidate.Ref.String()]; ok {
		return ConversionResult{}, fmt.Errorf("%w: candidate role already exists", ErrCRM005Rejected)
	}
	if _, ok := s.applications[record.Application.Ref.String()]; ok {
		return ConversionResult{}, fmt.Errorf("%w: application role already exists", ErrCRM005Rejected)
	}
	if _, ok := s.links[record.IdentityLink.Ref.String()]; ok {
		return ConversionResult{}, fmt.Errorf("%w: identity-link role already exists", ErrCRM005Rejected)
	}

	s.candidates[record.Candidate.Ref.String()] = record.Candidate
	s.applications[record.Application.Ref.String()] = record.Application
	s.links[record.IdentityLink.Ref.String()] = record.IdentityLink
	s.byKey[key] = storedConversion{write: in, result: result, record: record}
	return result, nil
}

// ConversionCounts reports the three authoritative role counts. It is a
// narrow inspection seam for recovery and security tests, not a write API.
func (s *ConversionStore) ConversionCounts() (candidate, application, identityLink int) {
	if s == nil {
		return 0, 0, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.candidates), len(s.applications), len(s.links)
}

// Conversion returns the immutable record associated with one tenant/key.
func (s *ConversionStore) Conversion(tenant values.TenantId, idempotencyKey string) (ConversionRecord, bool) {
	if s == nil {
		return ConversionRecord{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.byKey[conversionKey(tenant, idempotencyKey)]
	if !ok {
		return ConversionRecord{}, false
	}
	return entry.record, true
}

func conversionRole(proposal CandidateConversionProposal, identity CanonicalPersonBinding, ref values.EntityRef) ConversionRole {
	return ConversionRole{Ref: ref, Prospect: proposal.Prospect.ProspectID, Person: identity.Person, Source: proposal.Source, Consent: proposal.Consent, IdentityRef: identity.LinkRef}
}

func conversionKey(tenant values.TenantId, idempotencyKey string) string {
	return string(tenant) + "\x00" + idempotencyKey
}

func conversionContextError(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: nil context", ErrCRM005Rejected)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
