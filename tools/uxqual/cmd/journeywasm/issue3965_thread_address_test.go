//go:build !(js && wasm)

package main

import "testing"

func TestIssue3965_ThreadAddressRoundTrips(t *testing.T) {
	state := chatNavigationState{ConversationID: "room", ShowThread: true, ThreadParentID: "post/1 a&b"}
	href := chatHrefWithThread("/workspace/app/chat#channel=general", state)
	want := "/workspace/app/chat#channel=general&openthread=post%2F1+a%26b"
	if href != want {
		t.Fatalf("href = %q, want %q", href, want)
	}
	if got := chatAddressOpenThread("#" + href[len("/workspace/app/chat#"):]); got != "post/1 a&b" {
		t.Fatalf("parsed thread = %q", got)
	}
}

func TestIssue3965_ChannelAddressStillNamesTheChannel(t *testing.T) {
	channel, tab := chatChannelAddress("#channel=general&openthread=p1&tab=docs")
	if channel != "#channel=general" || tab != "docs" {
		t.Fatalf("channel %q tab %q; the thread part must not change which room opens", channel, tab)
	}
}

func TestIssue3965_NoThreadNoSuffix(t *testing.T) {
	closed := chatNavigationState{ConversationID: "room"}
	if got := chatHrefWithThread("/workspace/app/chat#channel=general", closed); got != "/workspace/app/chat#channel=general" {
		t.Fatalf("closed thread changed the address: %q", got)
	}
	open := chatNavigationState{ConversationID: "room", ShowThread: true, ThreadParentID: "p1"}
	if got := chatHrefWithThread("/workspace/app/chat#saved", open); got != "/workspace/app/chat#saved" {
		t.Fatalf("a page address got a thread: %q", got)
	}
	if got := chatHrefWithThread("/workspace/app/chat#channel=general&openthread=p1", open); got != "/workspace/app/chat#channel=general&openthread=p1" {
		t.Fatalf("thread added twice: %q", got)
	}
}

func TestIssue3965_ParseRejectsMissingOrOversizedThread(t *testing.T) {
	for _, hash := range []string{"#channel=general", "#channel=general&openthread=", "#other=x&openthread=p", "#channel=g&openthread=" + string(make([]byte, 300))} {
		if got := chatAddressOpenThread(hash); got != "" {
			t.Fatalf("%q parsed thread %q", hash, got)
		}
	}
	if got := chatAddressOpenThread("#channel=g&thread=1&message=m&at=3"); got != "" {
		t.Fatalf("the search address's thread=1 flag was read as a root: %q", got)
	}
}
