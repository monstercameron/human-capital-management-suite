package chat

import (
	"strings"

	"github.com/google/uuid"
)

// DirectPairConversationID returns a stable UUID for a direct conversation
// between exactly two participants in a tenant. The participant identity
// includes each person's home tenant so a shared-tenant conversation cannot
// accidentally collapse two employees with the same local subject ID.
//
// The returned ID is an identity primitive for route admission and durable
// create-if-absent operations; callers must still authorize both participants
// and atomically reuse an existing conversation before exposing any history.
func DirectPairConversationID(tenantID string, participants []MemberRef) (string, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || len(participants) != 2 {
		return "", ErrInvalidArgument
	}

	identities := [2]string{}
	for i, participant := range participants {
		homeTenantID := strings.TrimSpace(participant.TenantID)
		subjectID := strings.TrimSpace(participant.SubjectID)
		if homeTenantID == "" || subjectID == "" {
			return "", ErrInvalidArgument
		}
		identities[i] = homeTenantID + "\x00" + subjectID
	}
	if identities[0] == identities[1] {
		return "", ErrInvalidArgument
	}
	if identities[1] < identities[0] {
		identities[0], identities[1] = identities[1], identities[0]
	}

	// A fixed application namespace keeps this UUID family separate from
	// unrelated UUIDv5 names while retaining an opaque, deterministic route ID.
	namespace := uuid.MustParse("1596e8d2-772c-5c80-a98c-04bc40126b6f")
	name := tenantID + "\x00" + identities[0] + "\x00" + identities[1]
	return uuid.NewSHA1(namespace, []byte(name)).String(), nil
}
