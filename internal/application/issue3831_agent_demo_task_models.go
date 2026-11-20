package application

import (
	"errors"
	"os"
	"strings"
)

// validateLocalAgentDemoChatTaskDigest accepts an empty digest (the task
// documents are then not derived) or a well-formed owner-stated one.
func validateLocalAgentDemoChatTaskDigest(digest string) error {
	digest = strings.TrimSpace(digest)
	if digest == "" || localChatTaskSuiteDigest.MatchString(digest) {
		return nil
	}
	return errors.New("-suite-digest must be the sha256 digest (sha256: and 64 hex digits) of the evaluation suite you ran and are answerable for")
}

// prepareChatTaskModels derives the translation, writing-style and
// ambient-screen documents beside the base deployment when the owner stated an
// evaluation suite digest, using the idempotent writer of the chat-task-models
// command. Without a digest nothing is written (no evidence is invented) and
// the note names the flag to add.
func (s *LocalAgentDemoSummary) prepareChatTaskModels(config LocalAgentDemoConfig) error {
	digest := strings.TrimSpace(config.ChatTaskSuiteDigest)
	if digest == "" {
		s.ChatTaskModelsNote = "not derived: add -suite-digest sha256:<64 hex digits> naming the evaluation suite you ran, or run " + LocalChatTaskModelsCommand
		return nil
	}
	// The base deployment is what the three are derived from. When this run did
	// not leave one (nothing was published here), the finished preparation is not
	// failed at its last step: the receipt says what is missing instead.
	if base := localAgentDevFile(localPersonaModelDeploymentPath); !issue3831FileExists(base) {
		s.ChatTaskModelsNote = "not derived: the prepared model deployment " + base + " does not exist, so there is nothing to derive them from"
		return nil
	}
	docs, err := WriteLocalChatTaskModels(LocalChatTaskModelsConfig{Profile: config.Profile, SuiteDigest: digest})
	s.ChatTaskModels = docs
	return err
}

func (s LocalAgentDemoSummary) chatTaskModelsWritten() bool {
	for _, doc := range s.ChatTaskModels {
		if doc.Written {
			return true
		}
	}
	return false
}

func issue3831FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
