package chatui

import (
	"sync"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// CHATBUG-014: a message's Markdown was parsed again every time the timeline
// was drawn. Measured on the review machine, parsing was 77 ms of each draw of
// a 37-message conversation, and a load draws the timeline eight or more times.
// A message body parses to the same tree every time, and drawing only reads the
// tree, so the browser client parses each distinct body once.
//
// The cache lives in the browser client only (chatperf_markdown_js.go). The
// server draws no timelines, and a process that serves every tenant has no
// business keeping message text in a package-level table.

// chatperfMarkdownTree is one parsed message body. source is the byte slice the
// tree was parsed from: the tree's text segments are offsets into it.
type chatperfMarkdownTree struct {
	root   ast.Node
	source []byte
}

// chatperfMarkdownCache keeps the parsed trees of the bodies drawn most
// recently, at most limit of them.
type chatperfMarkdownCache struct {
	mu     sync.Mutex
	limit  int
	trees  map[string]chatperfMarkdownTree
	parsed int
}

func newChatperfMarkdownCache(limit int) *chatperfMarkdownCache {
	return &chatperfMarkdownCache{limit: limit, trees: make(map[string]chatperfMarkdownTree)}
}

// tree returns the parsed tree of body and the bytes it was parsed from.
func (c *chatperfMarkdownCache) tree(body string) (ast.Node, []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if kept, ok := c.trees[body]; ok {
		return kept.root, kept.source
	}
	if len(c.trees) >= c.limit {
		// Bodies come and go with the conversations that are opened; starting
		// again is cheaper than tracking which were drawn last, and the bodies
		// on screen are parsed again on the next draw.
		c.trees = make(map[string]chatperfMarkdownTree)
	}
	parsed := chatperfParseMarkdown(body)
	c.trees[body] = parsed
	c.parsed++
	return parsed.root, parsed.source
}

func chatperfParseMarkdown(body string) chatperfMarkdownTree {
	source := []byte(body)
	return chatperfMarkdownTree{root: markdownParser.Parse(text.NewReader(source)), source: source}
}
