package application

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// chatlangLocalAgentVersionDigest is the agent-version string the translation
// task is qualified under; the translation derivation only requires it non-empty.
const chatlangLocalAgentVersionDigest = "hcm-chat-translation/v1"

// LocalChatTaskModelsCommand is the migrate subcommand that writes the three
// model-backed Chat approval documents. Serve hints name it.
const LocalChatTaskModelsCommand = "migrate chat-task-models -profile local-dev -suite-digest sha256:<64 hex digits>"

var localChatTaskSuiteDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// LocalChatTaskModelsConfig asks for the translation, writing-style and
// ambient-screen approval documents to be derived from the prepared local
// deployment. SuiteDigest is the deployer's own statement of the evaluation
// suite that passed: nothing here runs an evaluation or invents one.
type LocalChatTaskModelsConfig struct {
	Profile     string
	SuiteDigest string
}

// LocalChatTaskModelDocument is one document the command considered.
type LocalChatTaskModelDocument struct {
	Task    string
	Path    string
	Written bool // false when the file already held exactly this document
}

// ValidateLocalChatTaskModelsConfig refuses anything but the local-dev profile
// and a well-formed digest, before the command touches a file.
func ValidateLocalChatTaskModelsConfig(config LocalChatTaskModelsConfig) error {
	if config.Profile != ServeProfileLocalDev {
		return errors.New("chat task model documents can only be written with -profile local-dev")
	}
	if !localChatTaskSuiteDigest.MatchString(strings.TrimSpace(config.SuiteDigest)) {
		return errors.New("-suite-digest must be the sha256 digest (sha256: and 64 hex digits) of the evaluation suite you ran and are answerable for")
	}
	return nil
}

// WriteLocalChatTaskModels derives the translation, writing-style and
// ambient-screen approval documents from the base deployment the agent-demo
// preparation wrote, and stores each beside it, where a local-dev run looks
// first. It is idempotent: a document that already matches is left untouched.
// The evaluation is stated by the owner (the suite digest), is recorded as
// such, and no key or signing seed is read, written or returned.
func WriteLocalChatTaskModels(config LocalChatTaskModelsConfig) ([]LocalChatTaskModelDocument, error) {
	if err := ValidateLocalChatTaskModelsConfig(config); err != nil {
		return nil, err
	}
	suite := strings.TrimSpace(config.SuiteDigest)
	basePath := localAgentDevFile(localPersonaModelDeploymentPath)
	if _, err := os.Stat(basePath); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("the prepared model deployment %s does not exist: run migrate agent-demo -profile local-dev first", basePath)
	}
	base, err := LoadPersonaModelDeployment(basePath)
	if err != nil {
		return nil, fmt.Errorf("the prepared model deployment %s cannot be used: %w", basePath, err)
	}
	const latency, cost = 20 * time.Second, int64(20000)
	eval := func(version string) agentmodel.ModelEvaluation {
		return agentmodel.ModelEvaluation{AgentVersionDigest: version, SuiteDigest: suite, Passed: true}
	}
	steps := []struct {
		task, path string
		derive     func() (PersonaModelDeployment, error)
	}{
		{"chat translation", localChatlangModelDeploymentPath, func() (PersonaModelDeployment, error) {
			return NewChatlangDeployment(base, eval(chatlangLocalAgentVersionDigest), latency, cost)
		}},
		{"chat writing style", localChattoneModelDeploymentPath, func() (PersonaModelDeployment, error) {
			return NewChattoneDeployment(base, eval(ChattoneAgentVersionDigest()), latency, cost)
		}},
		{"ambient screen", localAgentUX079AmbientScreenDeploymentPath, func() (PersonaModelDeployment, error) {
			return NewAgentUX079AmbientScreenDeployment(base, eval(AgentUX079AmbientScreenAgentVersionDigest()), latency, cost)
		}},
	}
	// Derive all three before writing any, so a refusal leaves no half set.
	derived := make([]PersonaModelDeployment, len(steps))
	for i, step := range steps {
		if derived[i], err = step.derive(); err != nil {
			return nil, fmt.Errorf("%s deployment: %w", step.task, err)
		}
	}
	out := make([]LocalChatTaskModelDocument, 0, len(steps))
	for i, step := range steps {
		path := localAgentDevFile(step.path)
		written, err := writeLocalAgentDemoDeployment(path, derived[i])
		if err != nil {
			return out, fmt.Errorf("%s deployment cannot be written: %w", step.task, err)
		}
		out = append(out, LocalChatTaskModelDocument{Task: step.task, Path: path, Written: written})
	}
	return out, nil
}

// localChatTaskModelsHint is the sentence a serve log adds to a missing-document
// reason; only the local-dev profile has the command to run.
func localChatTaskModelsHint(local bool) string {
	if !local {
		return ""
	}
	return "; in local development run: " + LocalChatTaskModelsCommand
}
