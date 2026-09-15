package productui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestOrganizationVirtualTreeBoundsRenderedRows(t *testing.T) {
	rows := flattenOwnershipRows(benchmarkOwnershipTree(1000), nil)
	if len(rows) != 1000 {
		t.Fatalf("flattened %d rows, want 1000", len(rows))
	}
	markup, err := ui.RenderToString(ui.CreateElement(organizationVirtualTree, organizationVirtualTreeProps{label: "Reporting lines", rows: rows}))
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(markup, `role="treeitem"`); count == 0 || count > 12 {
		t.Fatalf("virtual window rendered %d rows, want 1..12", count)
	}
	for _, want := range []string{`data-virtualized="true"`, `aria-setsize="999"`, `aria-posinset="1"`, `worker-0`, `gwc-vlist-bottom`} {
		if !strings.Contains(markup, want) {
			t.Errorf("virtual window missing %q", want)
		}
	}
	if strings.Contains(markup, `worker-500`) {
		t.Fatal("offscreen worker was rendered")
	}
	markup, err = ui.RenderToString(ui.CreateElement(organizationVirtualTree, organizationVirtualTreeProps{
		label: "Reporting lines", rows: rows, scrollTop: 500 * organizationVirtualRowHeight,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `worker-500`) || strings.Contains(markup, `worker-0`) || !strings.Contains(markup, `gwc-vlist-top`) {
		t.Fatal("scrolled virtual window did not move to the requested workers")
	}
}

func TestOrganizationVirtualTreePreservesHierarchyAndAuthorization(t *testing.T) {
	nodes := benchmarkOwnershipTree(5)
	nodes[0].Reports[1].Reports = []OwnershipNodeProps{{ID: "grandchild", Name: "Visible child", Level: 3}}
	nodes[0].Reports = nodes[0].Reports[:3]
	rows := flattenOwnershipRows(nodes, nil)
	if len(rows) != 5 || rows[3].node.ID != "grandchild" || rows[3].node.Level != 3 || rows[3].position != 1 || rows[3].setSize != 1 {
		t.Fatalf("hierarchy was flattened incorrectly: row count %d", len(rows))
	}
	rows = flattenOwnershipRows(nodes, map[string]bool{"root": true})
	if len(rows) != 1 || rows[0].expanded {
		t.Fatalf("collapsed root exposed descendants: row count %d", len(rows))
	}
	// A replacement authorized projection must never inherit a worker from an
	// earlier window, even when the same viewport offset is retained.
	nodes[0].Reports = nodes[0].Reports[:1]
	rows = flattenOwnershipRows(nodes, nil)
	if len(rows) != 2 || rows[1].node.ID != "worker-0" {
		t.Fatalf("replacement projection disclosed stale rows: row count %d", len(rows))
	}
}

func TestOrganizationVirtualTreeCentersSelectedBeforeCurrent(t *testing.T) {
	rows := flattenOwnershipRows(benchmarkOwnershipTree(20), nil)
	rows[2].node.Current = true
	rows[15].node.Selected = true
	want := float64(15*organizationVirtualRowHeight - organizationVirtualViewportHeight/2)
	if got := initialOrganizationVirtualTop(rows); got != want {
		t.Fatalf("selected employee scroll top = %.0f, want %.0f", got, want)
	}
}

func benchmarkOwnershipTree(size int) []OwnershipNodeProps {
	view := testView(PageOrganization)
	root := OwnershipNodeProps{I18nProps: I18nProps{Locale: view.Locale}, ID: "root", Name: "Amina Rahman", Role: "Chief Executive Officer", Level: 1, ReportsLabel: "Direct reports"}
	root.Reports = make([]OwnershipNodeProps, size-1)
	for index := range root.Reports {
		root.Reports[index] = OwnershipNodeProps{
			I18nProps: I18nProps{Locale: view.Locale}, ID: fmt.Sprintf("worker-%d", index),
			Name: fmt.Sprintf("Worker %d", index), Role: "Care Coordinator", Team: "Care Coordination",
			Level: 2, Href: fmt.Sprintf("/workspace/app/person?person=worker-%d", index),
		}
	}
	return []OwnershipNodeProps{root}
}

func BenchmarkOrganizationOwnershipTreeLarge(b *testing.B) {
	props := OrganizationOwnershipTreeProps{Nodes: benchmarkOwnershipTree(1000), Label: "Reporting lines"}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ui.RenderToString(ui.CreateElement(OrganizationOwnershipTree, props)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOrganizationVirtualWindowLarge(b *testing.B) {
	rows := flattenOwnershipRows(benchmarkOwnershipTree(1000), nil)
	props := organizationVirtualTreeProps{label: "Reporting lines", rows: rows}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ui.RenderToString(ui.CreateElement(organizationVirtualTree, props)); err != nil {
			b.Fatal(err)
		}
	}
}
