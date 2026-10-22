package agentcost

import "fmt"

// The largest daily ceilings a limit may carry. They are far above any real
// budget, and they keep the gate's percentage arithmetic (a count times 100, a
// ceiling times 80) inside int64: a ceiling near the int64 maximum used to wrap
// negative and make the first run look like 80 percent of the day.
const (
	MaxRunsPerDay        int64 = 1_000_000_000
	MaxSpendMicrosPerDay int64 = 1_000_000_000_000_000 // one billion dollars
)

// OwnerChecker is an Owners that can say it could not tell. A store that fails
// to answer must not look like "not the owner": the owner would be told they may
// not change their own agent, and their runs would drop out of their report.
type OwnerChecker interface {
	CheckOwner(tenant, agentID, actor string) (bool, error)
}

// isOwner asks owners whether actor owns the agent. A failure to ask is
// ErrUnavailable, never a denial.
func isOwner(owners Owners, tenant, agentID, actor string) (bool, error) {
	if checker, ok := owners.(OwnerChecker); ok {
		owned, err := checker.CheckOwner(tenant, agentID, actor)
		if err != nil {
			return false, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return owned, nil
	}
	return owners.IsOwner(tenant, agentID, actor), nil
}
