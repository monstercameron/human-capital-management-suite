package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
)

// issue3830Base writes the prepared local deployment into a fresh local agent
// directory, pointed at the given fake provider, and returns the directory.
func issue3830Base(t *testing.T, providerURL string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(EnvLocalAgentDevDir, dir)
	profile := localOpenAIProfileFixture(t)
	profile.ToolSchemaDigest = LocalPersonaOpenAIToolSchemaDigest()
	profile.ProfileDigest = agentmodel.ModelProfileDigest(profile)
	base, err := NewLocalPersonaOpenAIModelDeployment("harborcare-demo", []agentmodel.ModelProfile{profile}, localOpenAIMaterialFixture())
	if err != nil {
		t.Fatal(err)
	}
	base.BaseURL = providerURL
	if _, err := writeLocalAgentDemoDeployment(filepath.Join(dir, "model-deployment.json"), base); err != nil {
		t.Fatal(err)
	}
	return dir
}

// issue3830Relay answers as the local model: the shared fake provider names
// "test-model", which the adapter rightly refuses for the local deployment.
func issue3830Relay(t *testing.T, provider *chattoneProvider) string {
	t.Helper()
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		req, _ := http.NewRequestWithContext(r.Context(), r.Method, provider.server.URL+r.URL.Path, bytes.NewReader(body))
		req.Header = r.Header.Clone()
		req.Header.Del("Accept-Encoding")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		out = bytes.ReplaceAll(out, []byte(`"model":"test-model"`), []byte(`"model":"`+LocalPersonaOpenAIModelVersion+`"`))
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(out)
	}))
	t.Cleanup(relay.Close)
	return relay.URL
}

func issue3830Good(t *testing.T) func(system, user string) string {
	replay := NewChattoneRewordReplay(chattoneRewordReplayFixture(t))
	return func(system, user string) string {
		if strings.HasSuffix(system, chatrewrite.RewordInstruction) {
			checked, err := replay.RewriteChecked(context.Background(), chatrewrite.Prompt{Data: user})
			if err != nil {
				return "no recorded answer"
			}
			return checked.Text
		}
		return chattoneGoodRewrite(system, user)
	}
}

func TestIssue3830_QualifyWritesMeasuredDeploymentAndRecord_Integration(t *testing.T) {
	provider := newChattoneProvider(t)
	provider.rewrite = issue3830Good(t)
	dir := issue3830Base(t, issue3830Relay(t, provider))
	cfg := LocalChattoneQualifyConfig{Profile: ServeProfileLocalDev, APIKey: "test-only-provider-key"}

	res, err := RunLocalChattoneQualification(context.Background(), cfg, time.Now)
	if err != nil {
		for _, c := range res.Qualification.Cases {
			if !c.Passed {
				t.Logf("case %s: %s", c.ID, c.Reason)
			}
		}
		if r := res.Qualification.Reword; r != nil {
			t.Logf("reword passed=%v", r.Passed)
		}
		t.Fatal(err)
	}
	if !res.Qualification.Passed || res.Qualification.Reword == nil || !res.Written || provider.calls() != 30+24 {
		t.Fatalf("result %+v calls=%d", res, provider.calls())
	}
	if filepath.Clean(filepath.Dir(res.Path)) != filepath.Clean(dir) || filepath.Base(res.Path) != "chat-writing-style-deployment.json" {
		t.Fatalf("deployment path %s", res.Path)
	}
	// The file is the one a local-dev serve reads, carries the run's own digest
	// (not an owner's statement), and is accepted by the service's own checks.
	dep, err := LoadPersonaModelDeployment(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	qualified, err := chattoneQualify(dep)
	if err != nil || qualified.profile.Evaluation.SuiteDigest != res.Qualification.SuiteDigest || !qualified.profile.Evaluation.Passed {
		t.Fatalf("document does not carry the measured evidence: %v", err)
	}
	// The evaluation record is kept beside it: digests and verdicts, no key.
	raw, err := os.ReadFile(res.RecordPath)
	if err != nil {
		t.Fatal(err)
	}
	var record ChattoneQualification
	if err := json.Unmarshal(raw, &record); err != nil || record.SuiteDigest != res.Qualification.SuiteDigest || len(record.Cases) != 30 {
		t.Fatalf("record: %v %+v", err, record.SuiteDigest)
	}
	if strings.Contains(string(raw), "test-only-provider-key") {
		t.Fatal("the record holds the provider key")
	}
	// A rerun against an unchanged model leaves the document alone.
	again, err := RunLocalChattoneQualification(context.Background(), cfg, func() time.Time { return res.Qualification.MeasuredAt })
	if err != nil || again.Written {
		t.Fatalf("rerun: %v written=%v", err, again.Written)
	}
}

func TestIssue3830_FailingModelWritesNoDeployment_Security(t *testing.T) {
	provider := newChattoneProvider(t)
	provider.rewrite = func(_, user string) string { return chattoneDraftOf(user) }
	dir := issue3830Base(t, issue3830Relay(t, provider))
	res, err := RunLocalChattoneQualification(context.Background(), LocalChattoneQualifyConfig{Profile: ServeProfileLocalDev, APIKey: "test-only-provider-key"}, time.Now)
	if !errors.Is(err, ErrChattoneNotQualified) || res.Qualification.Passed || res.Written {
		t.Fatalf("a failing model: %v %+v", err, res)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "chat-writing-style-deployment.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a document exists for a model that did not qualify: %v", statErr)
	}
	// The failed run's record is kept so the failing cases can be read.
	if _, statErr := os.Stat(res.RecordPath); statErr != nil {
		t.Fatalf("no record of the failed run: %v", statErr)
	}
}

func TestIssue3830_RefusesOutsideLocalDevAndWithoutKey_Security(t *testing.T) {
	provider := newChattoneProvider(t)
	provider.rewrite = chattoneGoodRewrite
	issue3830Base(t, issue3830Relay(t, provider))
	for name, cfg := range map[string]LocalChattoneQualifyConfig{
		"production profile": {Profile: "production", APIKey: "k"},
		"empty profile":      {APIKey: "k"},
		"no key":             {Profile: ServeProfileLocalDev},
		"blank key":          {Profile: ServeProfileLocalDev, APIKey: "  "},
	} {
		if _, err := RunLocalChattoneQualification(context.Background(), cfg, time.Now); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	if provider.calls() != 0 {
		t.Fatalf("a refused run called the provider %d times", provider.calls())
	}
}

func TestIssue3830_MissingBaseNamesTheStepAndNeverEchoesTheKey(t *testing.T) {
	t.Setenv(EnvLocalAgentDevDir, t.TempDir())
	_, err := RunLocalChattoneQualification(context.Background(), LocalChattoneQualifyConfig{Profile: ServeProfileLocalDev, APIKey: "secret-key-value"}, time.Now)
	if err == nil || !strings.Contains(err.Error(), "agent-demo") || strings.Contains(err.Error(), "secret-key-value") {
		t.Fatalf("error: %v", err)
	}
}
