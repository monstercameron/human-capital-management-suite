//go:build !js

package main

import (
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
)

func installChatSoundUnlock() {}

func playChatSoundIfAllowed(string, *chatv1.Post, time.Time) {}
