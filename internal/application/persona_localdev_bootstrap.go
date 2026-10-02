package application

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/google/uuid"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

var ErrLocalDevPersonaChatBootstrap = errors.New("application: local persona chat bootstrap refused")

// LocalDevPersonaChatPolicyStore exposes only the chat-owned authorities that
// local demo setup administers. It cannot publish or install a persona.
type LocalDevPersonaChatPolicyStore interface {
	PutPublicAudiencePolicy(context.Context, string, string, int64, chatstore.PublicAudiencePolicy) (int64, error)
	CapturePublicAudienceSnapshot(context.Context, string, string) (chatstore.PublicAudienceSnapshot, error)
	PutPersonaChannelPolicy(context.Context, string, string, int64, chatstore.PersonaChannelPolicy) (int64, error)
	CapturePersonaChannelPolicy(context.Context, string, string, string) (chatstore.PersonaChannelPolicySnapshot, error)
}

// ProvisionLocalDevPersonaChatPolicy installs the explicit local room ceiling
// and, for public rooms, the complete active employee admission population.
// Room keys come from the reviewed demo room plan. Existing authority must
// match exactly; replay cannot overwrite an administrator's later changes.
func ProvisionLocalDevPersonaChatPolicy(ctx context.Context, store LocalDevPersonaChatPolicyStore, profile, tenant, conversation, roomKey string) (int, error) {
	if ctx == nil || store == nil || profile != ServeProfileLocalDev || strings.TrimSpace(conversation) == "" {
		return 0, ErrLocalDevPersonaChatBootstrap
	}
	pack, ok := demoworkforce.PackFor(tenant)
	if !ok || conversation != localDevPersonaDemoConversationID(tenant, roomKey) {
		return 0, ErrLocalDevPersonaChatBootstrap
	}
	ceiling, public, ok := localDevPersonaRoomCeiling(roomKey)
	if !ok {
		return 0, ErrLocalDevPersonaChatBootstrap
	}
	var population chatstore.PublicAudiencePolicy
	if public {
		employees, err := pack.Plan(pgstore.TenantID(tenant))
		if err != nil {
			return 0, fmt.Errorf("%w: workforce plan", ErrLocalDevPersonaChatBootstrap)
		}
		population.Classification = "INTERNAL"
		for _, employee := range employees {
			if strings.EqualFold(employee.Row.WorkerType, "employee") && strings.EqualFold(employee.Row.LifecycleStatus, "active") {
				population.Principals = append(population.Principals, chatstore.PublicAudiencePrincipal{HomeTenantID: tenant, SubjectID: employee.Row.WorkerKey})
			}
		}
		if len(population.Principals) == 0 {
			return 0, ErrLocalDevPersonaChatBootstrap
		}
		slices.SortFunc(population.Principals, func(a, b chatstore.PublicAudiencePrincipal) int { return strings.Compare(a.SubjectID, b.SubjectID) })
	}
	current, err := store.CapturePersonaChannelPolicy(ctx, tenant, conversation, "")
	if err != nil && !errors.Is(err, dbport.ErrNoRows) && !errors.Is(err, chatstore.ErrAudienceEligibilityUnavailable) {
		return 0, fmt.Errorf("%w: read room ceiling: %w", ErrLocalDevPersonaChatBootstrap, err)
	}
	if err == nil && !reflect.DeepEqual(current.Policy, ceiling) {
		return 0, fmt.Errorf("%w: existing room ceiling differs", ErrLocalDevPersonaChatBootstrap)
	}
	ceilingMissing := err != nil
	populationMissing := false
	if public {
		currentPopulation, popErr := store.CapturePublicAudienceSnapshot(ctx, tenant, conversation)
		if popErr != nil && !errors.Is(popErr, dbport.ErrNoRows) && !errors.Is(popErr, chatstore.ErrAudienceEligibilityUnavailable) {
			return 0, fmt.Errorf("%w: read public admission: %w", ErrLocalDevPersonaChatBootstrap, popErr)
		}
		if popErr == nil && (currentPopulation.Classification != population.Classification || !reflect.DeepEqual(currentPopulation.Eligible, population.Principals)) {
			return 0, fmt.Errorf("%w: existing public admission differs", ErrLocalDevPersonaChatBootstrap)
		}
		populationMissing = popErr != nil
	}
	created := 0
	if populationMissing {
		if _, err := store.PutPublicAudiencePolicy(ctx, tenant, conversation, 0, population); err != nil {
			return 0, fmt.Errorf("%w: provision public admission: %w", ErrLocalDevPersonaChatBootstrap, err)
		}
		created++
	}
	if ceilingMissing {
		if _, err := store.PutPersonaChannelPolicy(ctx, tenant, conversation, 0, ceiling); err != nil {
			return created, fmt.Errorf("%w: provision room ceiling: %w", ErrLocalDevPersonaChatBootstrap, err)
		}
		created++
	}
	return created, nil
}

func localDevPersonaDemoConversationID(tenant, roomKey string) string {
	namespace := uuid.NewSHA1(uuid.NameSpaceURL, []byte("hcmnext.chat.demo-seed"))
	return uuid.NewSHA1(namespace, []byte(tenant+"\x00"+roomKey)).String()
}

// ProvisionLocalDevPersonaDirectPolicy installs the reviewed one-to-one
// ceiling for the deterministic administrator/persona conversation. It is
// deliberately narrower than the room bootstrap: no arbitrary conversation
// or non-local profile can reach the writer.
func ProvisionLocalDevPersonaDirectPolicy(ctx context.Context, store LocalDevPersonaChatPolicyStore, profile, tenant, conversation, administrator string) (int, error) {
	expected, err := chatcore.DirectPairConversationID(tenant, []chatcore.MemberRef{{TenantID: tenant, SubjectID: administrator}, {TenantID: tenant, SubjectID: localAgentDemoAgentID}})
	if ctx == nil || store == nil || profile != ServeProfileLocalDev || tenant != localAgentDemoTenant || administrator != localAgentDemoAdmin || strings.TrimSpace(conversation) == "" || err != nil || conversation != expected {
		return 0, ErrLocalDevPersonaChatBootstrap
	}
	ceiling := chatstore.PersonaChannelPolicy{
		MaxTier:               "T3",
		AllowedDataClasses:    []string{"PUBLIC", "INTERNAL", "POLICY_DOCUMENT", "WORKFORCE", "SCHEDULE"},
		AllowedChannelClasses: []string{"ONE_TO_ONE"},
		PlacementClass:        "ONE_TO_ONE_DM",
		AlwaysPrivate:         true,
		// The agent's one capability is searching the documents officially
		// placed in the conversation it is asked in. Without this its own
		// direct conversation, where private answers are delivered and
		// follow-up questions are asked, could never answer one.
		ConversationSearchAllowed: true,
	}
	created := 0
	// A private conversation with no audience policy has no current authority:
	// the chat scope authorizer refuses every mention in it. The conversation
	// service does not create one for a direct conversation, so the local
	// preparation states it explicitly, once.
	if policies, ok := store.(interface {
		PutAudiencePolicy(context.Context, string, string, int64, chatstore.AudiencePolicy) (int64, error)
	}); ok {
		_, policyErr := policies.PutAudiencePolicy(ctx, tenant, conversation, 0, chatstore.AudiencePolicy{RoleMode: 1, Classification: "INTERNAL"})
		if policyErr == nil {
			created++
		} else if !errors.Is(policyErr, chatstore.ErrAudiencePolicyConflict) {
			return 0, fmt.Errorf("%w: provision direct-message audience policy: %w", ErrLocalDevPersonaChatBootstrap, policyErr)
		}
	}
	current, err := store.CapturePersonaChannelPolicy(ctx, tenant, conversation, "")
	if err == nil {
		if reflect.DeepEqual(current.Policy, ceiling) {
			return created, nil
		}
		// The only reviewed change to this ceiling is the search permission
		// above; anything else that differs was set by an administrator.
		earlier := ceiling
		earlier.ConversationSearchAllowed = false
		if !reflect.DeepEqual(current.Policy, earlier) {
			return created, fmt.Errorf("%w: existing direct-message ceiling differs", ErrLocalDevPersonaChatBootstrap)
		}
		if _, err := store.PutPersonaChannelPolicy(ctx, tenant, conversation, current.PolicyRevision, ceiling); err != nil {
			return created, fmt.Errorf("%w: update direct-message ceiling: %w", ErrLocalDevPersonaChatBootstrap, err)
		}
		return created + 1, nil
	}
	if !errors.Is(err, dbport.ErrNoRows) && !errors.Is(err, chatstore.ErrAudienceEligibilityUnavailable) {
		return 0, fmt.Errorf("%w: read direct-message ceiling: %w", ErrLocalDevPersonaChatBootstrap, err)
	}
	if _, err := store.PutPersonaChannelPolicy(ctx, tenant, conversation, 0, ceiling); err != nil {
		return created, fmt.Errorf("%w: provision direct-message ceiling: %w", ErrLocalDevPersonaChatBootstrap, err)
	}
	return created + 1, nil
}

func localDevPersonaRoomCeiling(room string) (chatstore.PersonaChannelPolicy, bool, bool) {
	policy := chatstore.PersonaChannelPolicy{MaxTier: "T0", AllowedDataClasses: []string{"PUBLIC", "INTERNAL", "POLICY_DOCUMENT"}, AllowedChannelClasses: []string{"PUBLIC"}, PlacementClass: "ANY_INTERNAL", ConversationSearchAllowed: true}
	switch room {
	case "company-chat", "general", "announcements", "people-ops", "comp-review", "engineering", "design", "sales", "benefits", "onboarding", "random":
		return policy, true, true
	case "leadership-private", "payroll-close", "incident-review":
		policy.MaxTier = "T1"
		policy.AllowedDataClasses = []string{"PUBLIC", "INTERNAL", "POLICY_DOCUMENT", "WORKFORCE", "COMPENSATION"}
		policy.AllowedChannelClasses = []string{"PRIVATE"}
		policy.PlacementClass = "MANAGER"
		policy.AlwaysPrivate = true
	case "group-comp-cycle":
		policy.MaxTier = "T1"
		policy.AllowedDataClasses = []string{"PUBLIC", "INTERNAL", "POLICY_DOCUMENT", "WORKFORCE", "COMPENSATION"}
		policy.AllowedChannelClasses = []string{"GROUP_DM"}
		policy.PlacementClass = "MANAGER"
		policy.AlwaysPrivate = true
	case "group-q4-hiring":
		policy.MaxTier = "T2"
		policy.AllowedDataClasses = []string{"PUBLIC", "INTERNAL", "POLICY_DOCUMENT", "WORKFORCE", "ONBOARDING"}
		policy.AllowedChannelClasses = []string{"GROUP_DM"}
		policy.PlacementClass = "ONBOARDING"
		policy.AlwaysPrivate = true
	case "dm-0", "dm-1", "dm-2", "dm-3", "dm-4", "dm-5", "dm-6", "dm-7":
		policy.MaxTier = "T3"
		policy.AllowedDataClasses = []string{"PUBLIC", "INTERNAL", "POLICY_DOCUMENT", "WORKFORCE", "SCHEDULE"}
		policy.AllowedChannelClasses = []string{"ONE_TO_ONE"}
		policy.PlacementClass = "ONE_TO_ONE_DM"
		policy.AlwaysPrivate = true
	default:
		return chatstore.PersonaChannelPolicy{}, false, false
	}
	return policy, false, true
}
