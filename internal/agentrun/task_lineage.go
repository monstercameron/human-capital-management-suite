package agentrun

import "fmt"

// validateTaskLineage checks shape; the delegation owner proves ancestry and
// authority before creation. Lineage fields never select execution authority.
func validateTaskLineage(req CreateRequest) error {
	if req.ParentTaskID == "" && req.RootTaskID == "" && req.BudgetTaskID == "" && req.DelegationDepth == 0 {
		return nil
	}
	if req.ParentTaskID == "" || req.RootTaskID == "" || req.BudgetTaskID != req.RootTaskID || req.DelegationDepth == 0 || req.DelegationDepth > 8 || req.ID == req.ParentTaskID || req.ID == req.RootTaskID {
		return fmt.Errorf("%w: child task lineage must bind a distinct parent and root within the depth limit", ErrInvalid)
	}
	return nil
}
