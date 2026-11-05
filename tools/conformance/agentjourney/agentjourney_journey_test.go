package agentjourney

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona/install"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/workflowbridge"
)

// TestTodo_AGENT_047 walks the first company-agent journey end to end: a
// design partner selects its inference provider, creates and publishes an
// agent, installs it, gets a cited DM answer, receives one scheduled
// digest, sees it participate autonomously, invokes it from a workflow,
// carries one exact approved HCM action, and explains every tool call,
// firing, approval, failure, and impact along the way.
func TestTodo_AGENT_047(t *testing.T) {
	world := NewWorld(t)
	world.DriveFull()

	t.Run("selected provider serves the journey", func(t *testing.T) {
		route := world.Route
		if len(route.Digest) != 64 || strings.Trim(route.Digest, "0123456789abcdef") != "" {
			t.Fatalf("route digest=%q, want the pinned route digest", route.Digest)
		}
		if len(world.RouteRecords()) != 1 {
			t.Fatalf("route records=%d, want the single recorded decision", len(world.RouteRecords()))
		}
		if route.Selected.ProfileID == "" || route.Selected.Identity.ProviderID == "" || route.Selected.Identity.ModelID == "" {
			t.Fatalf("route selected=%+v, want the pinned provider identity", route.Selected)
		}
		if route.Fallback {
			t.Fatalf("route fell back to %+v, want the pinned primary", route.Selected)
		}
		if len(route.Eligibility) == 0 {
			t.Fatal("route recorded no per-model eligibility evidence")
		}
		eligible := 0
		for _, e := range route.Eligibility {
			if e.Eligible {
				eligible++
			}
		}
		if eligible == 0 {
			t.Fatalf("eligibility=%+v, want at least one qualified model", route.Eligibility)
		}
	})

	t.Run("agent is created and published behind review and evaluation", func(t *testing.T) {
		publication := world.Publication
		if publication.TenantID != journeyTenant || publication.AgentID != journeyAgent {
			t.Fatalf("publication=%+v, want the journey agent pointer", publication)
		}
		if publication.ManifestVersion == 0 || publication.Generation == 0 {
			t.Fatalf("publication=%+v, want a versioned generation pointer", publication)
		}
		for name, digest := range map[string]string{"manifest": publication.ManifestDigest, "evaluation": publication.EvaluationDigest, "review": publication.ReviewDigest} {
			if !strings.HasPrefix(digest, "sha256:") {
				t.Fatalf("publication %s digest=%q, want a pinned sha256 digest", name, digest)
			}
		}
		if publication.ReviewerID == journeyOwner {
			t.Fatalf("reviewer=%q, want an independent reviewer", publication.ReviewerID)
		}
		if world.Evaluation == "" || world.Evaluation != publication.EvaluationDigest {
			t.Fatalf("evaluation=%q publication=%q, want the sealed passing run bound to the pointer", world.Evaluation, publication.EvaluationDigest)
		}
	})

	t.Run("persona installs into the DM", func(t *testing.T) {
		installation := world.Installation
		if installation.Status != install.InstallationActive {
			t.Fatalf("installation status=%q, want ACTIVE", installation.Status)
		}
		if installation.TenantID != journeyTenant || installation.PersonaID != journeyPersona || installation.ConversationID != journeyDM {
			t.Fatalf("installation=%+v, want the journey DM placement", installation)
		}
		if installation.Revision == 0 {
			t.Fatalf("installation=%+v, want a revisioned grant", installation)
		}
	})

	t.Run("DM mention is answered with a citation", func(t *testing.T) {
		invocation := world.Invocation
		if invocation.State != agentinvoke.InvocationStarted {
			t.Fatalf("invocation state=%q, want STARTED", invocation.State)
		}
		if invocation.Mode != agentinvoke.OnBehalfOf || invocation.InvokerID != journeyOwner {
			t.Fatalf("invocation=%+v, want the owner's on-behalf-of run", invocation)
		}
		if invocation.Grant.ID == "" || len(invocation.Grant.Skills) == 0 {
			t.Fatalf("invocation grant=%+v, want the narrowed mention grant", invocation.Grant)
		}
		if len(world.Journal.Runs) != 1 || world.Journal.Runs[0].InvocationID != invocation.ID {
			t.Fatalf("runs=%+v, want exactly the mention run", world.Journal.Runs)
		}
		body := world.AnswerBody()
		if !strings.Contains(body, journeyDocLabel) {
			t.Fatalf("answer body=%q, want the %q citation", body, journeyDocLabel)
		}
		if !strings.Contains(body, journeyDocID) {
			t.Fatalf("answer body=%q, want the %q source reference", body, journeyDocID)
		}
		if world.Delivery.AudienceRevision == 0 {
			t.Fatalf("delivery=%+v, want the pinned audience revision", world.Delivery)
		}
	})

	t.Run("one scheduled digest posts exactly once", func(t *testing.T) {
		if world.Digest.Receipt.SourceKey == "" || world.Digest.Receipt.RunRequestID == "" {
			t.Fatalf("digest outcome=%+v, want the recorded firing receipt", world.Digest)
		}
		if world.Digest.Duplicate {
			t.Fatalf("digest outcome=%+v, want the first firing, not a replay", world.Digest)
		}
		if got := world.DigestPostCount(); got != 1 {
			t.Fatalf("digest posts=%d, want exactly one scheduled digest", got)
		}
	})

	t.Run("agent participates autonomously without looping", func(t *testing.T) {
		if world.Reservation.SourceKey == "" || world.Reservation.InstallationID == "" {
			t.Fatalf("reservation=%+v, want the admitted trigger reservation", world.Reservation)
		}
		if world.Reservation.TenantID != journeyTenant || world.Reservation.ChannelID != journeyChannel {
			t.Fatalf("reservation=%+v, want the journey channel scope", world.Reservation)
		}
	})

	t.Run("workflow invocation derives a durable result", func(t *testing.T) {
		if world.Workflow.Outcome != workflowbridge.Succeeded {
			t.Fatalf("workflow outcome=%q, want SUCCEEDED", world.Workflow.Outcome)
		}
		if world.Workflow.RunID == "" || world.Workflow.OutputRef == "" || !strings.HasPrefix(world.Workflow.OutputDigest, "sha256:") {
			t.Fatalf("workflow result=%+v, want the pinned output evidence", world.Workflow)
		}
	})

	t.Run("one exact approved HCM action carries through", func(t *testing.T) {
		if len(world.Journal.Intents) != 1 {
			t.Fatalf("intents=%+v, want exactly one submitted HCM intent", world.Journal.Intents)
		}
		intent := world.Journal.Intents[0]
		if intent.Digest == "" || intent.Digest != world.SubmitReceipt.Digest {
			t.Fatalf("intent=%+v receipt=%+v, want the approved digest on the submission", intent, world.SubmitReceipt)
		}
		if len(intent.Material) == 0 {
			t.Fatalf("intent=%+v, want the exact material fields", intent)
		}
		if len(world.Journal.EffectKeys) != 1 {
			t.Fatalf("effect keys=%v, want the single owner effect", world.Journal.EffectKeys)
		}
		if world.Journal.Report == "" {
			t.Fatal("the journey produced no owner-visible outcome report")
		}
	})
}
