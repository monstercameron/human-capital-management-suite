package journeyclient

import (
	"encoding/json"
	"testing"
)

func TestTodo_AGENTUX_012_AgentTaskProjectionJSON(t *testing.T) {
	var projection Agents
	data := []byte(`{"enabled":true,"viewer_is_admin":true,"service":"available","agents":[{"id":"coach","name":"People Coach"}],"tasks":[{"id":"task-1","title":"Review policy","state":"completed","created_at":"2026-09-30T12:00:00Z","updated_at":"2026-09-30T12:05:00Z","result_preview":"Done","failure_reason":"","document_references":[{"document_id":"policy","label":"Policy"}]}]}`)
	if err := json.Unmarshal(data, &projection); err != nil {
		t.Fatal(err)
	}
	if !projection.Enabled || !projection.ViewerIsAdmin || len(projection.Agents) != 1 || projection.Agents[0].Name != "People Coach" || len(projection.Tasks) != 1 {
		t.Fatalf("projection = %+v", projection)
	}
	task := projection.Tasks[0]
	if task.CreatedAt != "2026-09-30T12:00:00Z" || task.UpdatedAt != "2026-09-30T12:05:00Z" || task.ResultPreview != "Done" || len(task.DocumentReferences) != 1 || task.DocumentReferences[0].DocumentID != "policy" {
		t.Fatalf("task = %+v", task)
	}
}
