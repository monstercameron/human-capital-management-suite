package application

import (
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

type AgentUXAnswerAttention struct {
	TenantID, AgentID, AgentVersion, Code, Owner, Location string
	Gate                                                   runstate.FailureGate
}

// AgentUXAnswerNeedsAttention is the operations projection: one row per cause
// and exact agent version. Operational detail stays out of Chat, and polling
// does not create repeated notifications or retain mutable process state.
func AgentUXAnswerNeedsAttention(runs []runstate.Run) []AgentUXAnswerAttention {
	byCause := map[string]AgentUXAnswerAttention{}
	for _, run := range runs {
		if run.State != runstate.StateFailed || run.TenantID == "" || run.AgentID == "" || run.AgentVersion == "" || run.FailureGate == "" {
			continue
		}
		row := AgentUXAnswerAttention{TenantID: run.TenantID, AgentID: run.AgentID, AgentVersion: run.AgentVersion, Code: run.TerminalCode, Gate: runstate.FailureGate(run.FailureGate), Owner: run.FailureOwner, Location: run.FailureLocation}
		key := row.TenantID + "\x00" + row.AgentID + "\x00" + row.AgentVersion + "\x00" + string(row.Gate) + "\x00" + row.Code
		prior, exists := byCause[key]
		if !exists || row.Location < prior.Location {
			byCause[key] = row
		}
	}
	keys := make([]string, 0, len(byCause))
	for key := range byCause {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]AgentUXAnswerAttention, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, byCause[key])
	}
	return rows
}
