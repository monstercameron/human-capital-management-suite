//go:build js && wasm

package main

import (
	"reflect"
	"syscall/js"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func agentP018SeedPreviewBrowser(cfg journeyclient.Config, snapshot productui.PersonaAdminSnapshot) {
	personaAdminBrowser.Lock()
	personaAdminBrowser.tenant, personaAdminBrowser.subject, personaAdminBrowser.bearer = cfg.Tenant, cfg.Subject, cfg.Bearer
	personaAdminBrowser.cfg = cfg
	held := snapshot
	personaAdminBrowser.snapshot = &held
	personaAdminBrowser.Unlock()
}

func agentP018RestorePreviewBrowser(tenant, subject, bearer string, cfg journeyclient.Config, snapshot *productui.PersonaAdminSnapshot, selected, previewSubject, previewConversation string, validation []string, unavailable bool, seq uint64) {
	personaAdminBrowser.Lock()
	personaAdminBrowser.tenant, personaAdminBrowser.subject, personaAdminBrowser.bearer = tenant, subject, bearer
	personaAdminBrowser.cfg = cfg
	personaAdminBrowser.snapshot = snapshot
	personaAdminBrowser.selected, personaAdminBrowser.previewSubject, personaAdminBrowser.previewConversation = selected, previewSubject, previewConversation
	personaAdminBrowser.previewValidation = validation
	personaAdminBrowser.previewUnavailable = unavailable
	personaAdminBrowser.previewSeq = seq
	personaAdminBrowser.revalidate = nil
	personaAdminBrowser.Unlock()
}

func agentP018SavePreviewBrowser() (tenant, subject, bearer string, cfg journeyclient.Config, snapshot *productui.PersonaAdminSnapshot, selected, previewSubject, previewConversation string, validation []string, unavailable bool, seq uint64) {
	personaAdminBrowser.Lock()
	defer personaAdminBrowser.Unlock()
	return personaAdminBrowser.tenant, personaAdminBrowser.subject, personaAdminBrowser.bearer, personaAdminBrowser.cfg, personaAdminBrowser.snapshot,
		personaAdminBrowser.selected, personaAdminBrowser.previewSubject, personaAdminBrowser.previewConversation,
		personaAdminBrowser.previewValidation, personaAdminBrowser.previewUnavailable, personaAdminBrowser.previewSeq
}

// TestTodo_AGENTP_018_FailedPreviewClearsStaleResult proves the RED fix for
// "an old preview visible after a failed request": when the access check is
// refused, the held projection is cleared, the failure flag is set so the
// page says the check failed instead of showing the old answer, and the page
// re-renders.
func TestTodo_AGENTP_018_FailedPreviewClearsStaleResult(t *testing.T) {
	global := js.Global()
	priorFetch := global.Get("fetch")
	defer global.Set("fetch", priorFetch)
	object := global.Get("Object")
	catchable := object.New()
	catchAll := js.FuncOf(func(js.Value, []js.Value) any { return catchable })
	defer catchAll.Release()
	catchable.Set("catch", catchAll)
	thenable := object.New()
	then := js.FuncOf(func(_ js.Value, args []js.Value) any {
		response := object.New()
		response.Set("ok", false)
		args[0].Invoke(response)
		return catchable
	})
	defer then.Release()
	thenable.Set("then", then)
	fetch := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].String() != "/workspace/persona-admin/?conversation_id=leadership&persona_id=policy-helper&subject_id=walt" {
			t.Errorf("preview request lost its targets: %s", args[0].String())
		}
		return thenable
	})
	defer fetch.Release()
	global.Set("fetch", fetch)

	cfg := journeyclient.Config{Bearer: "signed-session", Tenant: "tenant-a", Subject: "admin-a"}
	prior := agentP018SeededBrowser(cfg)
	defer agentP018RestoreBrowser(prior)
	revalidated := 0
	personaAdminBrowser.Lock()
	// personaAdminCheckPreview records the chosen targets before the read.
	personaAdminBrowser.selected, personaAdminBrowser.previewSubject, personaAdminBrowser.previewConversation = "policy-helper", "walt", "leadership"
	personaAdminBrowser.revalidate = func() { revalidated++ }
	personaAdminBrowser.Unlock()

	refreshPersonaAdminPreview(cfg, "policy-helper", "walt", "leadership")

	personaAdminBrowser.Lock()
	defer personaAdminBrowser.Unlock()
	if personaAdminBrowser.snapshot == nil {
		t.Fatal("failed preview dropped the held catalog")
	}
	if !reflect.DeepEqual(personaAdminBrowser.snapshot.Preview, productui.PersonaAdminPreview{}) {
		t.Fatalf("failed preview kept a stale access answer: %+v", personaAdminBrowser.snapshot.Preview)
	}
	if !personaAdminBrowser.previewUnavailable {
		t.Fatal("failed preview did not flag the check as failed")
	}
	if revalidated != 1 {
		t.Fatalf("revalidate calls = %d, want 1", revalidated)
	}
	if personaAdminBrowser.selected != "policy-helper" || personaAdminBrowser.previewSubject != "walt" || personaAdminBrowser.previewConversation != "leadership" {
		t.Fatalf("failed preview lost the chosen targets: %q %q %q", personaAdminBrowser.selected, personaAdminBrowser.previewSubject, personaAdminBrowser.previewConversation)
	}
}

type agentP018BrowserPrior struct {
	tenant, subject, bearer               string
	cfg                                   journeyclient.Config
	snapshot                              *productui.PersonaAdminSnapshot
	selected, previewSubject, previewConv string
	validation                            []string
	unavailable                           bool
	seq                                   uint64
}

func agentP018SeededBrowser(cfg journeyclient.Config) agentP018BrowserPrior {
	tenant, subject, bearer, oldCfg, snapshot, selected, previewSubject, previewConv, validation, unavailable, seq := agentP018SavePreviewBrowser()
	agentP018SeedPreviewBrowser(cfg, productui.PersonaAdminSnapshot{
		Available: true,
		Personas:  []productui.PersonaAdminPersona{{ID: "policy-helper", Name: "Policy Helper"}},
		Preview: productui.PersonaAdminPreview{
			Subject: "Walt Brennan", Conversation: "Leadership",
			EffectiveSkills: []productui.PersonaAdminSkill{{ID: "persona.chat_reply", Tier: "T2"}},
		},
	})
	return agentP018BrowserPrior{tenant, subject, bearer, oldCfg, snapshot, selected, previewSubject, previewConv, validation, unavailable, seq}
}

func agentP018RestoreBrowser(prior agentP018BrowserPrior) {
	agentP018RestorePreviewBrowser(prior.tenant, prior.subject, prior.bearer, prior.cfg, prior.snapshot, prior.selected, prior.previewSubject, prior.previewConv, prior.validation, prior.unavailable, prior.seq)
}

// TestTodo_AGENTP_018_IncompleteCheckClearsStalePreviewFailure proves a new
// access check that names no complete selection clears both the old answer
// and the old failure: the page must show what to choose next, not the
// previous request's "could not be checked".
func TestTodo_AGENTP_018_IncompleteCheckClearsStalePreviewFailure(t *testing.T) {
	global := js.Global()
	priorDocument := global.Get("document")
	defer global.Set("document", priorDocument)
	object := global.Get("Object")
	document := object.New()
	get := js.FuncOf(func(js.Value, []js.Value) any { return js.Null() })
	defer get.Release()
	document.Set("getElementById", get)
	global.Set("document", document)

	cfg := journeyclient.Config{Bearer: "signed-session", Tenant: "tenant-a", Subject: "admin-a"}
	prior := agentP018SeededBrowser(cfg)
	defer agentP018RestoreBrowser(prior)
	revalidated := 0
	personaAdminBrowser.Lock()
	personaAdminBrowser.previewUnavailable = true
	if personaAdminBrowser.snapshot != nil {
		withServerFailure := *personaAdminBrowser.snapshot
		withServerFailure.PreviewState = productui.PersonaAdminRegionState{Unavailable: true, Omitted: 2}
		personaAdminBrowser.snapshot = &withServerFailure
	}
	personaAdminBrowser.revalidate = func() { revalidated++ }
	personaAdminBrowser.Unlock()

	personaAdminCheckPreview()

	personaAdminBrowser.Lock()
	defer personaAdminBrowser.Unlock()
	if personaAdminBrowser.snapshot == nil {
		t.Fatal("incomplete check dropped the held catalog")
	}
	if !reflect.DeepEqual(personaAdminBrowser.snapshot.Preview, productui.PersonaAdminPreview{}) {
		t.Fatalf("incomplete check kept a stale access answer: %+v", personaAdminBrowser.snapshot.Preview)
	}
	if personaAdminBrowser.previewUnavailable {
		t.Fatal("incomplete check kept the previous request's failure visible")
	}
	if personaAdminBrowser.snapshot.PreviewState.Unavailable {
		t.Fatal("incomplete check kept the previous request's server-side failure visible")
	}
	if !reflect.DeepEqual(personaAdminBrowser.previewValidation, []string{"persona", "subject", "conversation"}) {
		t.Fatalf("validation fields = %v", personaAdminBrowser.previewValidation)
	}
	if revalidated != 1 {
		t.Fatalf("revalidate calls = %d, want 1", revalidated)
	}
}

// TestTodo_AGENTP_018_SessionChangeClearsPreviewState proves a signed-in
// identity change never carries preview targets, answers or command feedback
// from the previous session into the next one.
func TestTodo_AGENTP_018_SessionChangeClearsPreviewState(t *testing.T) {
	personaAdminBrowser.Lock()
	priorTenant, priorSubject, priorBearer, priorCfg := personaAdminBrowser.tenant, personaAdminBrowser.subject, personaAdminBrowser.bearer, personaAdminBrowser.cfg
	priorSnapshot := personaAdminBrowser.snapshot
	priorSelected, priorPreviewSubject, priorPreviewConv := personaAdminBrowser.selected, personaAdminBrowser.previewSubject, personaAdminBrowser.previewConversation
	priorValidation, priorUnavailable, priorSeq := personaAdminBrowser.previewValidation, personaAdminBrowser.previewUnavailable, personaAdminBrowser.previewSeq
	priorHistory, priorStatus, priorPersona, priorAction := personaAdminBrowser.historyRequested, personaAdminBrowser.commandStatus, personaAdminBrowser.commandPersona, personaAdminBrowser.commandAction
	priorResults := personaAdminBrowser.evaluationResults
	personaAdminBrowser.Unlock()
	defer func() {
		personaAdminBrowser.Lock()
		personaAdminBrowser.tenant, personaAdminBrowser.subject, personaAdminBrowser.bearer = priorTenant, priorSubject, priorBearer
		personaAdminBrowser.cfg = priorCfg
		personaAdminBrowser.snapshot = priorSnapshot
		personaAdminBrowser.selected, personaAdminBrowser.previewSubject, personaAdminBrowser.previewConversation = priorSelected, priorPreviewSubject, priorPreviewConv
		personaAdminBrowser.previewValidation = priorValidation
		personaAdminBrowser.previewUnavailable = priorUnavailable
		personaAdminBrowser.previewSeq = priorSeq
		personaAdminBrowser.historyRequested = priorHistory
		personaAdminBrowser.commandStatus, personaAdminBrowser.commandPersona, personaAdminBrowser.commandAction = priorStatus, priorPersona, priorAction
		personaAdminBrowser.evaluationResults = priorResults
		personaAdminBrowser.revalidate = nil
		personaAdminBrowser.Unlock()
	}()

	first := journeyclient.Config{Bearer: "bearer-one", Tenant: "tenant-a", Subject: "admin-a"}
	configurePersonaAdminBrowser(first, nil)
	personaAdminBrowser.Lock()
	personaAdminBrowser.selected, personaAdminBrowser.previewSubject, personaAdminBrowser.previewConversation = "policy-helper", "walt", "leadership"
	personaAdminBrowser.previewValidation = []string{"subject"}
	personaAdminBrowser.previewUnavailable = true
	personaAdminBrowser.commandStatus, personaAdminBrowser.commandPersona, personaAdminBrowser.commandAction = "success", "policy-helper", "PUBLISH"
	seq := personaAdminBrowser.previewSeq
	personaAdminBrowser.Unlock()

	configurePersonaAdminBrowser(journeyclient.Config{Bearer: "bearer-two", Tenant: "tenant-a", Subject: "admin-b"}, nil)

	personaAdminBrowser.Lock()
	defer personaAdminBrowser.Unlock()
	if personaAdminBrowser.selected != "" || personaAdminBrowser.previewSubject != "" || personaAdminBrowser.previewConversation != "" {
		t.Fatalf("session change kept preview targets: %q %q %q", personaAdminBrowser.selected, personaAdminBrowser.previewSubject, personaAdminBrowser.previewConversation)
	}
	if personaAdminBrowser.previewValidation != nil || personaAdminBrowser.previewUnavailable {
		t.Fatalf("session change kept preview failure state: %v %v", personaAdminBrowser.previewValidation, personaAdminBrowser.previewUnavailable)
	}
	if personaAdminBrowser.commandStatus != "" || personaAdminBrowser.commandPersona != "" || personaAdminBrowser.commandAction != "" {
		t.Fatal("session change kept command feedback")
	}
	if personaAdminBrowser.previewSeq != seq+1 {
		t.Fatalf("session change did not invalidate in-flight previews: %d -> %d", seq, personaAdminBrowser.previewSeq)
	}
	if personaAdminBrowser.snapshot != nil {
		t.Fatalf("session change kept the previous session's projection: %+v", personaAdminBrowser.snapshot)
	}
}
