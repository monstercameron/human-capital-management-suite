package agentrunstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

type taskLineage struct {
	ParentTaskID    string `json:"parent_task_id"`
	RootTaskID      string `json:"root_task_id"`
	BudgetTaskID    string `json:"budget_task_id"`
	DelegationDepth uint8  `json:"delegation_depth"`
}

func encodeTaskLineage(task agentrun.AgentTask) ([]byte, error) {
	lineage := taskLineage{task.ParentTaskID, task.RootTaskID, task.BudgetTaskID, task.DelegationDepth}
	if lineage == (taskLineage{}) {
		return nil, nil
	}
	if err := validateTaskLineage(task.ID, lineage); err != nil {
		return nil, err
	}
	return json.Marshal(lineage)
}

func decodeTaskLineage(raw []byte, task *agentrun.AgentTask) error {
	if task == nil {
		return fmt.Errorf("%w: missing lineage task", agentrun.ErrInvalid)
	}
	if len(raw) == 0 {
		return nil
	}
	var lineage taskLineage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&lineage); err != nil {
		return fmt.Errorf("%w: malformed task lineage", agentrun.ErrInvalid)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("%w: trailing task lineage", agentrun.ErrInvalid)
	}
	if err := validateTaskLineage(task.ID, lineage); err != nil {
		return err
	}
	task.ParentTaskID, task.RootTaskID, task.BudgetTaskID, task.DelegationDepth = lineage.ParentTaskID, lineage.RootTaskID, lineage.BudgetTaskID, lineage.DelegationDepth
	return nil
}

func validateTaskLineage(taskID string, lineage taskLineage) error {
	for _, id := range []string{lineage.ParentTaskID, lineage.RootTaskID, lineage.BudgetTaskID} {
		if id == "" || strings.TrimSpace(id) != id || strings.ContainsAny(id, "\r\n\x00") {
			return fmt.Errorf("%w: lineage identity is required", agentrun.ErrInvalid)
		}
	}
	if lineage.ParentTaskID == taskID || lineage.RootTaskID == taskID || lineage.BudgetTaskID != lineage.RootTaskID || lineage.DelegationDepth < 1 || lineage.DelegationDepth > 16 || (lineage.DelegationDepth == 1 && lineage.ParentTaskID != lineage.RootTaskID) {
		return fmt.Errorf("%w: invalid specialist task lineage", agentrun.ErrInvalid)
	}
	return nil
}
