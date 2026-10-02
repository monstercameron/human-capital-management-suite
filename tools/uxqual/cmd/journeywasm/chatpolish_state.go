package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func chatPolishPinAction(key string) string {
	switch key {
	case "ArrowUp":
		return "north"
	case "ArrowDown":
		return "south"
	case "ArrowLeft":
		return "west"
	case "ArrowRight":
		return "east"
	}
	return ""
}

func chatPolishSearchOutcome(previous chatui.ChatSearchView, conversation chatui.Model, query string, response chatsearch.Response, recent []string, errorCode string) chatui.ChatSearchView {
	view := chatui.ChatSearchView{Query: query, Response: response, Recent: recent, Error: errorCode}
	if errorCode != "" && errorCode != "invalid" && errorCode != "meaning" {
		view.Response, view.Recent = previous.Response, previous.Recent
		view.Conversation = &conversation
	}
	return view
}
