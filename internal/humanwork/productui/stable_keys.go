package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// keyUnlessKeyed gives node a reconciliation key for its position in a
// sibling list, unless it already carries one. A caller's own key wins: the
// live journey content is keyed by its address on purpose, so that a
// different journey mounts a fresh subtree.
func keyUnlessKeyed(node ui.Node, key string) ui.Node {
	if node == nil {
		return nil
	}
	if node.Key != "" {
		return node
	}
	if existing, ok := node.Props["key"]; ok && existing != nil && existing != "" {
		return node
	}
	return html.WithKey(node, key)
}
