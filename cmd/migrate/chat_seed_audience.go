package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
)

//go:embed testdata/chat_seed_audience_v1.json
var chatSeedAudienceFixture embed.FS

type chatSeedAudienceFixtureV1 struct {
	SchemaVersion int                          `json:"schema_version"`
	Tenants       []chatSeedTenantAudienceFact `json:"tenants"`
	Rooms         []chatSeedRoomAudienceFact   `json:"rooms"`
}

type chatSeedTenantAudienceFact struct {
	TenantID  string `json:"tenant_id"`
	Residency string `json:"residency"`
}

type chatSeedRoomAudienceFact struct {
	RoomKey        string `json:"room_key"`
	Classification string `json:"classification"`
	RoleMode       int    `json:"role_mode"`
}

// chatSeedAudiencePolicy returns only facts declared by the checked-in v1
// demo fixture. The generated membership must also resolve to an active local
// employee in that tenant's workforce pack; unknown and external members fail
// closed instead of inheriting an internal classification.
func chatSeedAudiencePolicy(tenant string, room roomSpec, members []chatcore.Membership, employees []demoworkforce.Employee) (chatstore.AudiencePolicy, bool, error) {
	if room.Kind == roomPublic {
		return chatstore.AudiencePolicy{}, false, nil
	}
	facts, err := loadChatSeedAudienceFixture()
	if err != nil {
		return chatstore.AudiencePolicy{}, false, err
	}
	residency := ""
	for _, fact := range facts.Tenants {
		if fact.TenantID == tenant {
			residency = strings.TrimSpace(fact.Residency)
			break
		}
	}
	if residency == "" {
		return chatstore.AudiencePolicy{}, false, fmt.Errorf("chat seed audience: no residency fact for tenant %q", tenant)
	}
	classByRoom := make(map[string]chatSeedRoomAudienceFact, len(facts.Rooms))
	for _, fact := range facts.Rooms {
		if _, duplicate := classByRoom[fact.RoomKey]; duplicate || strings.TrimSpace(fact.RoomKey) == "" || strings.TrimSpace(fact.Classification) == "" || (fact.RoleMode != 1 && fact.RoleMode != 2) {
			return chatstore.AudiencePolicy{}, false, errors.New("chat seed audience: invalid or duplicate room fact")
		}
		classByRoom[fact.RoomKey] = fact
	}
	fact, ok := classByRoom[room.Key]
	if !ok {
		return chatstore.AudiencePolicy{}, false, fmt.Errorf("chat seed audience: no classification fact for room %q", room.Key)
	}
	localEmployees := make(map[string]struct{}, len(employees))
	for _, employee := range employees {
		if strings.EqualFold(strings.TrimSpace(employee.Row.WorkerType), "employee") && strings.EqualFold(strings.TrimSpace(employee.Row.LifecycleStatus), "active") {
			localEmployees[employee.Row.WorkerKey] = struct{}{}
		}
	}
	if len(members) == 0 {
		return chatstore.AudiencePolicy{}, false, fmt.Errorf("chat seed audience: room %q has no classified members", room.Key)
	}
	for _, member := range members {
		if member.TenantID != tenant || member.HomeTenantID != tenant {
			return chatstore.AudiencePolicy{}, false, fmt.Errorf("chat seed audience: room %q has external or cross-tenant member %q", room.Key, member.SubjectID)
		}
		if _, classified := localEmployees[member.SubjectID]; !classified {
			return chatstore.AudiencePolicy{}, false, fmt.Errorf("chat seed audience: room %q has unknown or unclassified member %q", room.Key, member.SubjectID)
		}
	}
	return chatstore.AudiencePolicy{RoleMode: fact.RoleMode, Classification: fact.Classification, Residency: residency}, true, nil
}

func loadChatSeedAudienceFixture() (chatSeedAudienceFixtureV1, error) {
	file, err := chatSeedAudienceFixture.Open("testdata/chat_seed_audience_v1.json")
	if err != nil {
		return chatSeedAudienceFixtureV1{}, fmt.Errorf("open chat seed audience fixture: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var facts chatSeedAudienceFixtureV1
	if err = decoder.Decode(&facts); err != nil {
		return chatSeedAudienceFixtureV1{}, fmt.Errorf("decode chat seed audience fixture: %w", err)
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return chatSeedAudienceFixtureV1{}, errors.New("chat seed audience fixture has trailing JSON")
		}
		return chatSeedAudienceFixtureV1{}, fmt.Errorf("decode trailing chat seed audience fixture data: %w", err)
	}
	if facts.SchemaVersion != 1 || len(facts.Tenants) == 0 || len(facts.Rooms) == 0 {
		return chatSeedAudienceFixtureV1{}, errors.New("chat seed audience fixture is not a complete v1 fixture")
	}
	seenTenants := make(map[string]struct{}, len(facts.Tenants))
	for _, tenant := range facts.Tenants {
		if strings.TrimSpace(tenant.TenantID) == "" || strings.TrimSpace(tenant.Residency) == "" {
			return chatSeedAudienceFixtureV1{}, errors.New("chat seed audience fixture has an incomplete tenant fact")
		}
		if _, duplicate := seenTenants[tenant.TenantID]; duplicate {
			return chatSeedAudienceFixtureV1{}, fmt.Errorf("chat seed audience fixture repeats tenant %q", tenant.TenantID)
		}
		seenTenants[tenant.TenantID] = struct{}{}
	}
	return facts, nil
}

// chatSeedMemberships returns the exact local active-employee memberships used
// both by initial creation and the non-destructive legacy-policy repair.
func chatSeedMemberships(tenant, conversation string, room roomSpec, people []string) (string, []chatcore.Membership, error) {
	if len(room.Members) == 0 {
		return "", nil, fmt.Errorf("room %q has no members", room.Key)
	}
	ownerIndex := room.Members[0]
	if ownerIndex < 0 || ownerIndex >= len(people) {
		return "", nil, fmt.Errorf("room %q owner index %d is outside the workforce", room.Key, ownerIndex)
	}
	owner := people[ownerIndex]
	members := make([]chatcore.Membership, 0, len(room.Members))
	seen := make(map[string]struct{}, len(room.Members))
	for _, index := range room.Members {
		if index < 0 || index >= len(people) {
			return "", nil, fmt.Errorf("room %q member index %d is outside the workforce", room.Key, index)
		}
		subject := people[index]
		if _, duplicate := seen[subject]; duplicate {
			return "", nil, fmt.Errorf("room %q repeats member %q", room.Key, subject)
		}
		seen[subject] = struct{}{}
		role := chatcore.Member
		if subject == owner {
			role = chatcore.Manager
		}
		members = append(members, chatcore.Membership{ConversationID: conversation, TenantID: tenant, HomeTenantID: tenant, SubjectID: subject, Role: role, HistoryVisibility: chatcore.FullHistory, Revision: 1})
	}
	return owner, members, nil
}

// repairChatSeedAudiencePolicies adds policies to the exact deterministic
// seed rooms from the pre-policy seed. It never removes or rewrites a room,
// membership, post, or an existing policy. Every room and membership is
// validated before the first policy write, and existing policies must digest
// to the complete v1 fixture before the repair proceeds.
func repairChatSeedAudiencePolicies(ctx context.Context, store *chatstore.Store, adapter *chatstore.Adapter, tenant string, rooms []roomSpec, people []string, employees []demoworkforce.Employee) (int, error) {
	type pendingPolicy struct {
		conversation string
		policy       chatstore.AudiencePolicy
	}
	pending := make([]pendingPolicy, 0)
	for _, room := range rooms {
		conversationID := chatSeedConversationID(tenant, room.Key)
		exists, err := store.ConversationExists(ctx, tenant, conversationID)
		if err != nil {
			return 0, fmt.Errorf("check existing chat seed room %s: %w", room.Key, err)
		}
		if !exists {
			return 0, nil
		}
		owner, expectedMembers, err := chatSeedMemberships(tenant, conversationID, room, people)
		if err != nil {
			return 0, err
		}
		conversation, err := adapter.GetConversation(ctx, tenant, conversationID)
		if err != nil {
			return 0, fmt.Errorf("read existing chat seed room %s: %w", room.Key, err)
		}
		if conversation.Kind != kindOf(room.Kind) || conversation.OwnerID != owner || conversation.Archived || int(conversation.MemberCount) != len(expectedMembers) {
			return 0, fmt.Errorf("chat seed audience repair refused mismatched room %q", room.Key)
		}
		actual, err := adapter.ListMemberships(ctx, tenant, conversationID, chatcore.Page{PageSize: 200})
		if err != nil {
			return 0, fmt.Errorf("read existing chat seed members %s: %w", room.Key, err)
		}
		if actual.NextCursor != "" || !chatSeedMembershipSetMatches(tenant, expectedMembers, actual.Memberships) {
			return 0, fmt.Errorf("chat seed audience repair refused changed membership for room %q", room.Key)
		}
		policy, requiresPolicy, err := chatSeedAudiencePolicy(tenant, room, expectedMembers, employees)
		if err != nil {
			return 0, err
		}
		if !requiresPolicy {
			continue
		}
		snapshot, snapshotErr := store.CaptureAudienceSnapshot(ctx, tenant, conversationID)
		if snapshotErr == nil {
			if snapshot.Digest != chatSeedAudienceDigest(tenant, conversationID, kindOf(room.Kind), policy, snapshot) {
				return 0, fmt.Errorf("chat seed audience repair refused non-fixture policy for room %q", room.Key)
			}
			continue
		}
		if !errors.Is(snapshotErr, chatstore.ErrAudienceEligibilityUnavailable) {
			return 0, fmt.Errorf("read existing chat seed audience %s: %w", room.Key, snapshotErr)
		}
		pending = append(pending, pendingPolicy{conversation: conversationID, policy: policy})
	}
	if len(pending) == 0 {
		return 0, nil
	}
	for _, item := range pending {
		if _, err := store.PutAudiencePolicy(ctx, tenant, item.conversation, 0, item.policy); err != nil {
			if !errors.Is(err, chatstore.ErrAudiencePolicyConflict) {
				return 0, fmt.Errorf("repair audience policy for %s: %w", item.conversation, err)
			}
		}
		snapshot, err := store.CaptureAudienceSnapshot(ctx, tenant, item.conversation)
		if err != nil {
			return 0, fmt.Errorf("verify repaired audience policy for %s: %w", item.conversation, err)
		}
		var roomKindValue chatcore.ConversationKind
		for _, room := range rooms {
			if chatSeedConversationID(tenant, room.Key) == item.conversation {
				roomKindValue = kindOf(room.Kind)
				break
			}
		}
		if roomKindValue == "" || snapshot.Digest != chatSeedAudienceDigest(tenant, item.conversation, roomKindValue, item.policy, snapshot) {
			return 0, fmt.Errorf("repaired audience policy for %s does not match its versioned seed facts", item.conversation)
		}
	}
	return len(pending), nil
}

func chatSeedMembershipSetMatches(tenant string, expected, actual []chatcore.Membership) bool {
	if len(expected) != len(actual) {
		return false
	}
	want := make(map[string]chatcore.Membership, len(expected))
	for _, member := range expected {
		want[member.HomeTenantID+"\x00"+member.SubjectID] = member
	}
	for _, member := range actual {
		if member.TenantID != tenant || member.HomeTenantID != tenant || member.LeftAt != nil {
			return false
		}
		key := member.HomeTenantID + "\x00" + member.SubjectID
		wantMember, ok := want[key]
		if !ok || member.Role != wantMember.Role || member.HistoryVisibility != wantMember.HistoryVisibility {
			return false
		}
		delete(want, key)
	}
	return len(want) == 0
}

type chatSeedAudienceCanonicalPolicy struct {
	Revision               int64
	RequiredRoles          []string
	RoleMode               int
	RequiredQualifications []string
	AllowedPrincipals      []string
	AllowedTenants         []string
	Classification         string
	Residency              string
}

type chatSeedAudienceCanonical struct {
	TenantID             string                          `json:"tenant_id"`
	ConversationID       string                          `json:"conversation_id"`
	Kind                 string                          `json:"kind"`
	Classification       string                          `json:"classification"`
	ConversationRevision int64                           `json:"conversation_revision"`
	Policy               chatSeedAudienceCanonicalPolicy `json:"policy"`
	Members              []chatstore.AudienceMember      `json:"members"`
}

func chatSeedAudienceDigest(tenant, conversation string, kind chatcore.ConversationKind, policy chatstore.AudiencePolicy, snapshot chatstore.AudienceSnapshot) string {
	canonicalPolicy := chatSeedAudienceCanonicalPolicy{
		Revision: snapshot.PolicyRevision, RequiredRoles: append([]string{}, policy.RequiredRoles...), RoleMode: policy.RoleMode,
		RequiredQualifications: append([]string{}, policy.RequiredQualifications...), AllowedPrincipals: append([]string{}, policy.AllowedPrincipals...),
		AllowedTenants: append([]string{}, policy.AllowedTenants...), Classification: policy.Classification, Residency: policy.Residency,
	}
	sort.Strings(canonicalPolicy.RequiredRoles)
	sort.Strings(canonicalPolicy.RequiredQualifications)
	sort.Strings(canonicalPolicy.AllowedPrincipals)
	sort.Strings(canonicalPolicy.AllowedTenants)
	raw, err := json.Marshal(chatSeedAudienceCanonical{
		TenantID: tenant, ConversationID: conversation, Kind: string(kind), Classification: policy.Classification,
		ConversationRevision: snapshot.ConversationRevision, Policy: canonicalPolicy, Members: snapshot.Members,
	})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}
