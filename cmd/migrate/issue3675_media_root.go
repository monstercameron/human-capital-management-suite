package main

import (
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
)

// chatSeedMediaRoot is where the chat seed writes the demo media bytes: the
// -chat-media-root flag, else the same answer serve gives from the same variables
// (the media root, else a child of the artifact root, else .artifacts/chat-media).
// Seed and serve therefore look in one place unless an operator points them apart.
func chatSeedMediaRoot(flag string) string {
	if flag != "" {
		return flag
	}
	return application.DefaultChatMediaArtifactRoot(os.Getenv(application.EnvChatMediaRoot), os.Getenv(application.EnvArtifactRoot))
}

// chatSeedMediaReport tells the operator where the media was written, so a serve
// started with another -chat-media-root can be pointed at it.
func chatSeedMediaReport(root string) string {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return "chat media written to " + root + "; start serve with -chat-media-root " + root + " (or HCMNEXT_CHAT_MEDIA_ROOT) so it finds these files\n"
}
