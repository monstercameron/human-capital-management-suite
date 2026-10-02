package main

import "testing"

func TestAgentUXChat5_IdleDirectoryRequests(t *testing.T) {
	posts := ""
	requests := 1
	for second := 0; second < 60; second++ {
		snapshot := []personaChatInvocation{{InvocationID: "run", PostID: "question", Status: "working", CurrentStep: second}}
		next := chat5InvocationPosts(snapshot)
		if next != posts {
			requests++
			posts = next
		}
	}
	if requests != 1 {
		t.Fatalf("idle view made %d directory requests", requests)
	}
	answer := []personaChatInvocation{{PrivatePostID: "answer"}, {PrivatePostID: "answer"}}
	if got := chat5InvocationPosts(answer); got != "answer" {
		t.Fatal(got)
	}
	if chat5InvocationPosts([]personaChatInvocation{{PrivatePostID: "b"}, {PrivatePostID: "a"}}) != chat5InvocationPosts([]personaChatInvocation{{PrivatePostID: "a"}, {PrivatePostID: "b"}}) {
		t.Fatal("snapshot ordering triggered an unnecessary read")
	}
}
