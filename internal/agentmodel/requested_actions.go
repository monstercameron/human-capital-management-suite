package agentmodel

import (
	"slices"
	"strings"
)

// RequestedAction is an untrusted typed description of the invoker's goal.
// It confers no capability, grant or permission to execute an action.
type RequestedAction string

const (
	ActionReadPolicy         RequestedAction = "read_policy"
	ActionChangeCompensation RequestedAction = "change_compensation"
	ActionSubmitPayroll      RequestedAction = "submit_payroll"
	ActionOther              RequestedAction = "other"
)

// RequestedActionPolicy is supplied by the current reviewed persona owner.
type RequestedActionPolicy struct {
	ProfileDigest string            `json:"profile_digest"`
	Allowed       []RequestedAction `json:"allowed"`
}

func ValidateRequestedActionPolicy(policy RequestedActionPolicy) error {
	if !strings.HasPrefix(policy.ProfileDigest, "sha256:") || !validDigest(strings.TrimPrefix(policy.ProfileDigest, "sha256:")) || len(policy.Allowed) == 0 {
		return ErrInvalidModelRequest
	}
	seen := map[RequestedAction]bool{}
	for _, action := range policy.Allowed {
		if !validRequestedAction(action) || seen[action] {
			return ErrInvalidModelRequest
		}
		seen[action] = true
	}
	return nil
}

// CheckRequestedActions validates the complete typed plan before checking the
// owner's allowlist. A malformed or missing plan cannot become a refusal proof.
func CheckRequestedActions(policy RequestedActionPolicy, actions []RequestedAction) (bool, error) {
	if ValidateRequestedActionPolicy(policy) != nil || len(actions) == 0 || len(actions) > 4 {
		return false, ErrInvalidModelResult
	}
	seen := map[RequestedAction]bool{}
	allowed := true
	for _, action := range actions {
		if !validRequestedAction(action) || seen[action] {
			return false, ErrInvalidModelResult
		}
		seen[action] = true
		allowed = allowed && slices.Contains(policy.Allowed, action)
	}
	return allowed, nil
}

func validRequestedAction(action RequestedAction) bool {
	return action == ActionReadPolicy || action == ActionChangeCompensation || action == ActionSubmitPayroll || action == ActionOther
}
