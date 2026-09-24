package app

// These aliases are the application-facing ports consumed by transport
// adapters. Keeping the domain-backed representations behind this package
// lets listener code forward values without importing domain implementations.

import (
	"github.com/monstercameron/human-capital-management-suite/internal/domains/knowledge"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/partnerapp"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
	"time"
)

type KnowledgeSearchService = knowledge.SearchService
type KnowledgeSearchRequest = knowledge.SearchRequest

func KnowledgeAudienceForRoles(roles []string) string {
	return knowledge.AudienceForRoles(roles)
}

type PromotionRegion = promotion.Region
type PromotionCompensationGuardrail = promotion.CompensationGuardrail

var ErrPositionUnauthorized = position.ErrUnauthorized
var ErrPositionInvalidRevisionRef = position.ErrInvalidRevisionRef

type SIEMCredentialRing = subscription.CredentialRing

type MachineClientRegistry = partnerapp.ClientRegistry
type MachineClient = partnerapp.MachineClient
type MachineClientKey = partnerapp.MachineClientKey
type MachineClientUseRequest = partnerapp.ClientUseRequest

func AuthorizeMachineClientUse(c MachineClient, r MachineClientUseRequest) error {
	return partnerapp.AuthorizeClientUse(c, r)
}
func MachineClientGrantsWrite(scopes []string) bool { return partnerapp.GrantsWrite(scopes) }
func SelectMachineClientKey(keys []MachineClientKey, kid string, now time.Time) (MachineClientKey, error) {
	return partnerapp.SelectClientKey(keys, kid, now)
}
