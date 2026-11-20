package chatui

import (
	"hash/fnv"
	"strconv"
)

// issue3941SnippetID names one result's excerpt so its open button can point at
// it with aria-describedby. It is derived from the result's own target, so two
// results from the same author in the same conversation get different ids.
func issue3941SnippetID(target, kind string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(kind))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(target))
	return "chatsearch-snippet-" + strconv.FormatUint(h.Sum64(), 36)
}
