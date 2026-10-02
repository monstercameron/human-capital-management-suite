package application

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// TestChatlangWriteLocalDeployment derives the local development approval
// document for translation from the local persona deployment, once, on request:
//
//	HCMNEXT_WRITE_CHATLANG_DEPLOYMENT=1 HCMNEXT_CHATLANG_EVAL_SUITE_DIGEST=sha256:... \
//	  go test -run TestChatlangWriteLocalDeployment ./internal/application/
//
// The evaluation evidence is the deployer's to state: the suite digest comes from
// the environment and is required, never invented here. Without the first
// variable the test does nothing.
func TestChatlangWriteLocalDeployment(t *testing.T) {
	if os.Getenv("HCMNEXT_WRITE_CHATLANG_DEPLOYMENT") != "1" {
		t.Skip("set HCMNEXT_WRITE_CHATLANG_DEPLOYMENT=1 to write the local translation deployment")
	}
	suite := os.Getenv("HCMNEXT_CHATLANG_EVAL_SUITE_DIGEST")
	if suite == "" {
		t.Fatal("HCMNEXT_CHATLANG_EVAL_SUITE_DIGEST is required: the evaluation evidence is stated by the deployer")
	}
	root := filepath.Join("..", "..")
	base, err := LoadPersonaModelDeployment(filepath.Join(root, ".artifacts", "lanes", "agent-dev", "model-deployment.json"))
	if err != nil {
		t.Fatal(err)
	}
	dep, err := NewChatlangDeployment(base, agentmodel.ModelEvaluation{AgentVersionDigest: "hcm-chat-translation/v1", SuiteDigest: suite, Passed: true}, 20*time.Second, 20000)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(dep, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(localChatlangModelDeploymentPath)), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
