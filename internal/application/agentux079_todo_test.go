package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

// TestTodo_AGENTUX_079 covers the ready-made agents' unfinished preparation
// (AGENTUX-079): Support Desk's project and grant, the SUPPORT_EMAIL source
// kind, Task Catcher's offers planner, Birthday Buddy's schedule, and the four
// agents' listing with their skills in plain words.
func TestTodo_AGENTUX_079(t *testing.T) {
	s15Run(t,
		s15Proof{"a support email run is admitted", TestAgentUX079_SupportEmailAdmitted},
		s15Proof{"the Customer support project and grant are prepared", TestAgentUX079_SupportProjectPrepared},
		s15Proof{"no project database is a plain reason", TestAgentUX079_SupportProjectUnconfigured},
		s15Proof{"served targets resolve the prepared project", TestAgentUX079_SupportTargetsResolve},
		s15Proof{"a screen-model deployment qualifies the offers planner", TestAgentUX079_AmbientScreenDeploymentQualifies},
		s15Proof{"the planner screens through the governed gateway", TestAgentUX079_AmbientScreenPlannerDispatches},
		s15Proof{"no screen model is an absent planner with a reason", TestAgentUX079_AmbientScreenAbsent},
		s15Proof{"the served composition wires the planner or states why", TestAgentUX079_AmbientScreenComposition},
		s15Proof{"the birthday schedule is filed paused without a route", TestAgentUX079_BirthdaySchedulePausedWithoutRoute},
		s15Proof{"the birthday schedule resumes with a route", TestAgentUX079_BirthdayScheduleResumedWithRoute},
		s15Proof{"settled schedules are left alone", TestAgentUX079_BirthdayScheduleLeavesOthersAlone},
		s15Proof{"interrupted schedules converge", TestAgentUX079_BirthdayScheduleConverges},
		s15Proof{"the schedule binds the current facts or refuses", TestAgentUX079_BirthdayScheduleBindings},
		s15Proof{"the store and the application name the same birthday persona", TestAgentUX079_BirthdayPersonaIdentity},
		s15Proof{"the served registry publishes the demo skills", TestAgentUX079_DemoSkillsPublished},
		s15Proof{"every demo skill states its work in plain words", TestAgentUX079_DemoSkillDescriptions},
		s15Proof{"discovery purposes derive from the registry", TestAgentUX079_SkillDiscoveryPurposes},
		s15Proof{"the reader discovers pinned skills across purposes", TestAgentUX079_ReaderDiscoversAcrossPurposes},
		s15Proof{"the reader lists valid unplaced personas", TestAgentUX079_ReaderListsUnplaced},
		s15Proof{"the roster lists every kind while Ask answers", TestAgentUX079_CatalogListPage},
	)
}

// TestAgentUX079_SupportEmailAdmitted: the support executor's source kind
// passes the admission layer instead of being refused as unknown.
func TestAgentUX079_SupportEmailAdmitted(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	request := agentrun.Request{
		Source:         agentrun.SourceIdentity{TenantID: localAgentDemoTenant, Kind: agentrun.SourceKind(AgentUXDemoSupportSourceKind), Key: "support-email-key-1", Ref: "support-email:1"},
		Persona:        &agentrun.PersonaRef{ID: AgentUXDemoSupportPersonaID, Version: "1", Digest: testPersonaDigest},
		LegalEntity:    "ironridge-legal-entity",
		Agent:          agentrun.VersionRef{AgentID: "support-desk", Version: "1", Digest: testPersonaDigest},
		InstallationID: "support-desk-installation",
		Principal:      agentrun.PrincipalChain{Mode: agentrun.ModeSponsored, AgentPrincipalID: "agent:support-desk", SponsorID: "service:support-desk"},
		Purpose:        "customer-support",
		Audience:       agentrun.AudienceScope{ID: "incident-review", SnapshotID: "1", Digest: testPersonaDigest},
		Context:        agentrun.ContextScope{ID: "support-email:1", SnapshotID: "1", Digest: testPersonaDigest},
		Deadline:       now.Add(time.Hour),
		Budget:         agentrun.Budget{MaxCostMicros: 1000, MaxInputTokens: 1000, MaxOutputTokens: 1000},
		CauseID:        "support-email:1",
	}
	service, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: personaChatAdmissionAuthorityFake{}, Store: agentrun.NewMemoryAdmissionStore(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	record, created, err := service.Admit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !created || record.Decision != agentrun.DecisionAccepted {
		t.Fatalf("SUPPORT_EMAIL admission = created=%t decision=%s refusal=%s, want an accepted record", created, record.Decision, record.RefusalCode)
	}
}
