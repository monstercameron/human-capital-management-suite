package agentdelegationstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
)

func encodeGrantLineage(grant agentdelegation.Grant) (any, any, error) {
	if err := agentdelegation.ValidateGrant(grant); err != nil {
		return nil, nil, err
	}
	if grant.ParentGrantID == "" {
		return nil, nil, nil
	}
	actor, err := json.Marshal(grant.ParentActor)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: encode parent actor", agentdelegation.ErrInvalidGrant)
	}
	return grant.ParentGrantID, string(actor), nil
}

func decodeGrantLineage(parentID *string, raw []byte, grant *agentdelegation.Grant) error {
	if grant == nil {
		return agentdelegation.ErrInvalidGrant
	}
	if parentID == nil && len(raw) == 0 {
		return nil
	}
	if parentID == nil || *parentID == "" || len(raw) == 0 {
		return agentdelegation.ErrInvalidGrant
	}
	var actor agentdelegation.ActorClaim
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&actor); err != nil {
		return agentdelegation.ErrInvalidGrant
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return agentdelegation.ErrInvalidGrant
	}
	grant.ParentGrantID, grant.ParentActor = *parentID, &actor
	return agentdelegation.ValidateGrant(*grant)
}
func nullableAdmissionID(id string) any {
	if id == "" {
		return nil
	}
	return id
}
