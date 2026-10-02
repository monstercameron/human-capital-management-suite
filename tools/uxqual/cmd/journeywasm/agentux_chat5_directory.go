package main

import (
	"sort"
	"strings"
)

// Only a changed answer-post set can change directory author attribution.
// Elapsed time and repeated stream snapshots do not invalidate agent membership.
func chat5InvocationPosts(invocations []personaChatInvocation) string {
	seen := map[string]bool{}
	var posts []string
	for _, invocation := range invocations {
		if invocation.PrivatePostID != "" && !seen[invocation.PrivatePostID] {
			seen[invocation.PrivatePostID] = true
			posts = append(posts, invocation.PrivatePostID)
		}
	}
	sort.Strings(posts)
	return strings.Join(posts, "\x00")
}
