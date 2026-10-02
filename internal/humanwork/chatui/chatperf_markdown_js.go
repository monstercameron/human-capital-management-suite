//go:build js && wasm

package chatui

import "github.com/yuin/goldmark/ast"

// chatperfMarkdownBodies is how many distinct message bodies keep their parsed
// tree: several conversations' worth of messages.
const chatperfMarkdownBodies = 1024

var chatperfMarkdown = newChatperfMarkdownCache(chatperfMarkdownBodies)

// chatperfMarkdownTreeOf returns the parsed tree of a message body, parsing a
// body the first time it is drawn (chatperf_markdown.go).
func chatperfMarkdownTreeOf(body string) (ast.Node, []byte) {
	return chatperfMarkdown.tree(body)
}
