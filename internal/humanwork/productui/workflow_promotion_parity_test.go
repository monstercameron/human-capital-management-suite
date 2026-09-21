package productui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func TestPromotionDraftTopologyMatchesTheExecutableDefinition(t *testing.T) {
	definition := promotionexec.Definition()
	draft := WorkflowDraftView{
		DraftID: "draft-promotion", WorkflowID: definition.WorkflowID, Name: definition.Name,
		SemanticVersion: "1.1.1", Revision: 1, StartNodeID: definition.StartNodeID,
		DefinitionDigest: workflowversion.DefinitionDigest(definition), BaseDefinitionDigest: "sha256:older-publication",
		TemplateDefinitionDigest: workflowversion.DefinitionDigest(definition), MatchesTemplateDefinition: true, TemplateID: "hcmnext.templates.promotion", TemplateVersion: 1,
	}
	for _, node := range definition.Nodes {
		draft.Nodes = append(draft.Nodes, WorkflowDraftNode{ID: node.ID, StepType: string(node.Type)})
	}
	for _, edge := range definition.Edges {
		draft.Edges = append(draft.Edges, WorkflowDraftEdge{FromID: edge.From, ToID: edge.To, RouteKey: edge.RouteKey})
	}

	projection := workflowDraftGraphProjection(draft)
	if len(projection.Nodes) != len(definition.Nodes) || len(projection.Edges) != len(definition.Edges) {
		t.Fatalf("draft topology = %d nodes/%d edges, executable = %d/%d", len(projection.Nodes), len(projection.Edges), len(definition.Nodes), len(definition.Edges))
	}
	nodes := make(map[string]string, len(projection.Nodes))
	for _, node := range projection.Nodes {
		nodes[node.ID] = node.StepType
		if node.Start != (node.ID == definition.StartNodeID) {
			t.Fatalf("draft start marker for %q = %t", node.ID, node.Start)
		}
		if node.ID != definition.StartNodeID && node.Depth == 0 {
			t.Fatalf("reachable promotion node %q collapsed into the start stage", node.ID)
		}
	}
	for _, node := range definition.Nodes {
		if nodes[node.ID] != string(node.Type) {
			t.Fatalf("draft node %q type = %q, want %q", node.ID, nodes[node.ID], node.Type)
		}
	}
	edges := make(map[string]bool, len(projection.Edges))
	for _, edge := range projection.Edges {
		edges[edge.FromID+"\x00"+edge.RouteKey+"\x00"+edge.ToID] = true
	}
	for _, edge := range definition.Edges {
		if !edges[edge.From+"\x00"+edge.RouteKey+"\x00"+edge.To] {
			t.Fatalf("draft topology omitted executable route %s --%s--> %s", edge.From, edge.RouteKey, edge.To)
		}
	}

	markup, err := ui.RenderToString(workflowDraftWorkspace(I18nProps{Locale: ResolveProductLocale(DefaultProductLocale)}, draft, nil, "", nil, nil, nil, nil, nil, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-parity="exact"`,
		`Exact match to executable template`,
		fmt.Sprintf(`%d steps · %d routed outcomes`, len(definition.Nodes), len(definition.Edges)),
		`Await Payroll Confirmation`,
		`Observe Payroll`,
		`Await Access Confirmation`,
		`Observe Access`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("promotion draft workspace missing %q", want)
		}
	}
	for _, node := range definition.Nodes {
		if got := strings.Count(markup, `data-node-id="`+node.ID+`"`); got != 2 {
			t.Fatalf("promotion node %q rendered %d times, want graph and outline", node.ID, got)
		}
	}
}
