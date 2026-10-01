package application

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/contact"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ServedContactSurface exposes the contact endpoint and verification
// mechanics through the application boundary composed by hcmnext serve. The
// contact domain remains the owner of normalization, evidence and lifecycle;
// this surface only makes those typed operations reachable without retaining
// process-wide contact state.
type ServedContactSurface struct {
	ContractID          string
	Version             func() int
	NewEndpointRevision func(values.EntityRef, string, contact.EndpointType, string, string, int, string) (contact.ContactEndpointRevision, error)
	MarkVerified        func(contact.ContactEndpointRevision) (contact.ContactEndpointRevision, error)
	IssueChallenge      func(string, values.EntityRef, contact.ContactEndpointRevision, string, string, time.Time, time.Duration, int) (contact.ContactVerificationChallenge, string, error)
	IssueVerification   func(values.EntityRef, string, string, string, time.Time, time.Duration) (contact.VerificationChallenge, string, error)
	VerifyVerification  func(*contact.VerificationChallenge, values.EntityRef, string, string, string, time.Time) contact.ChallengeStatus
	NewMemoryStore      func() *contact.MemoryStore
	PutEndpointRevision func(*contact.MemoryStore, context.Context, values.TenantId, contact.ContactEndpointRevision, ...uint64) error
	PutChallenge        func(*contact.MemoryStore, context.Context, values.TenantId, contact.ContactVerificationChallenge, ...string) error
	CodeOf              func(error) contact.StoreErrorCode
}

// NewServedContactSurface returns contact capabilities reachable from the
// composed serving application. The memory store is the kernel-pure reference
// implementation of the Store port; production composition may supply the
// PostgreSQL adapter behind that same port.
func NewServedContactSurface() ServedContactSurface {
	return ServedContactSurface{
		ContractID:          contact.ServingContractID,
		Version:             contact.Version,
		NewEndpointRevision: contact.NewContactEndpointRevision,
		MarkVerified:        contact.ContactEndpointRevision.MarkVerified,
		IssueChallenge:      contact.IssueContactChallenge,
		IssueVerification:   contact.IssueChallenge,
		VerifyVerification:  contact.VerifyChallenge,
		NewMemoryStore:      contact.NewMemoryStore,
		PutEndpointRevision: (*contact.MemoryStore).PutEndpointRevision,
		PutChallenge:        (*contact.MemoryStore).PutChallenge,
		CodeOf:              contact.CodeOf,
	}
}

// Contact returns the contact surface exposed by a composed application. A
// nil application has no served capabilities.
func (a *App) Contact() ServedContactSurface {
	if a == nil {
		return ServedContactSurface{}
	}
	return NewServedContactSurface()
}
