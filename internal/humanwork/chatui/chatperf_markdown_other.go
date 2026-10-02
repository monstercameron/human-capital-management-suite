//go:build !(js && wasm)

package chatui

import "github.com/yuin/goldmark/ast"

// chatperfMarkdownTreeOf parses a message body. Outside the browser client
// nothing is kept: see chatperf_markdown.go.
func chatperfMarkdownTreeOf(body string) (ast.Node, []byte) {
	parsed := chatperfParseMarkdown(body)
	return parsed.root, parsed.source
}
