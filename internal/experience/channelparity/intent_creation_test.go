package channelparity

import (
	"errors"
	"testing"
)

func fixtureCreationInput() IntentCreationInput {
	return IntentCreationInput{IntentType: "leave-request", Fields: map[string]any{"reason": "family event", "endDate": "2026-10-03", "startDate": "2026-10-01"}}
}
func fixtureTrustedContext() TrustedContext {
	return TrustedContext{TenantID: "tenant-1", PrincipalID: "employee-7", Purpose: "self-service", Capability: CapabilityRef{ID: "intent.create", Version: "2.1"}}
}

func TestTodo_REV_018_02(t *testing.T) {
	input, context := fixtureCreationInput(), fixtureTrustedContext()
	var digest string
	var normalized NormalizedIntent
	for _, channel := range IntentCreationChannels() {
		out, err := CreateIntent(channel, input, context, nil)
		if err != nil {
			t.Fatalf("channel %s: %v", channel, err)
		}
		if !out.OK || out.Error != nil || out.NormalizedIntent == nil || out.Result == nil || out.Evidence == nil {
			t.Fatalf("channel %s failed: %+v", channel, out)
		}
		if digest == "" {
			digest = out.RequestDigest
			normalized = *out.NormalizedIntent
		} else if out.RequestDigest != digest || out.NormalizedIntent.IntentType != normalized.IntentType || out.NormalizedIntent.TrustedContext != normalized.TrustedContext {
			t.Fatalf("channel %s changed semantic identity: %+v", channel, out)
		}
		if !out.Evidence.ConfirmationRequired || !out.Evidence.SimulationAvailable || out.Adaptation.Simulation != "available" {
			t.Fatalf("channel %s skipped confirmation/simulation: %+v", channel, out)
		}
		if channel == CreateKiosk && (out.Adaptation.Handoff == nil || out.Adaptation.Handoff.AuthorityToken) {
			t.Fatalf("kiosk handoff delegated authority: %+v", out.Adaptation)
		}
	}
	if digest != "ac86d9b6970d0dcabd978e5d6115486525f8c1c37451ed1009e093d67fc9b233" {
		t.Fatalf("request digest=%s", digest)
	}
}

func TestTodo_REV_018_02_Conformance(t *testing.T) {
	input, context := fixtureCreationInput(), fixtureTrustedContext()
	expectedModes := []string{"pointer-keyboard", "touch", "guided-touch", "screen-reader"}
	for index, channel := range IntentCreationChannels() {
		out, err := CreateIntent(channel, input, context, nil)
		if err != nil || !out.OK {
			t.Fatalf("channel %s creation: %+v, %v", channel, out, err)
		}
		if out.Adaptation.InputMode != expectedModes[index] {
			t.Fatalf("channel %s input mode=%q", channel, out.Adaptation.InputMode)
		}
	}
	invalid := fixtureCreationInput()
	invalid.Fields["startDate"] = ""
	var want *IntentCreationError
	var digest string
	for _, channel := range IntentCreationChannels() {
		out, err := CreateIntent(channel, invalid, context, nil)
		if err != nil {
			t.Fatal(err)
		}
		if out.OK || out.Error == nil || out.Error.Code != "INVALID_REQUIRED_INPUT" {
			t.Fatalf("channel %s error=%+v", channel, out)
		}
		if want == nil {
			copyError := *out.Error
			copyError.Fields = append([]string(nil), out.Error.Fields...)
			want = &copyError
		} else if out.Error.Code != want.Code || out.Error.Message != want.Message || len(out.Error.Fields) != len(want.Fields) || out.Error.Fields[0] != want.Fields[0] {
			t.Fatalf("channel %s error diverged: %+v want %+v", channel, out.Error, want)
		}
		if digest == "" {
			digest = out.RequestDigest
		} else if out.RequestDigest != digest {
			t.Fatalf("channel %s error digest diverged", channel)
		}
	}
	if want.Code != "INVALID_REQUIRED_INPUT" || want.Fields[0] != "startDate" || want.Message != "required input is missing" {
		t.Fatalf("error contract=%+v", want)
	}
	badContext := context
	badContext.PrincipalID = ""
	out, err := CreateIntent(CreateDesktop, input, badContext, nil)
	if err != nil || out.Error == nil || out.Error.Code != "INVALID_CONTEXT" || len(out.Error.Fields) != 1 || out.Error.Fields[0] != "trustedContext" {
		t.Fatalf("invalid context=%+v err=%v", out, err)
	}
	assistance := &CreationAssistance{Kind: "representative", ActorID: "rep-9"}
	assisted, err := CreateIntent(CreateAccessibilityAssisted, input, context, assistance)
	if err != nil || assisted.Adaptation.Assistance == nil || assisted.Adaptation.Assistance.ActorID != "rep-9" || assisted.RequestDigest != digestForInput(t, input, context) {
		t.Fatalf("assisted identity changed: %+v err=%v", assisted, err)
	}
	if _, err := CreateIntent("telepathy", input, context, nil); !errors.Is(err, ErrInvalidCreationChannel) {
		t.Fatalf("unsupported channel err=%v", err)
	}
	input.Fields["nested"] = map[string]any{"value": "original"}
	copyResult, err := CreateIntent(CreateDesktop, input, context, nil)
	if err != nil {
		t.Fatal(err)
	}
	input.Fields["nested"].(map[string]any)["value"] = "mutated"
	if copyResult.NormalizedIntent.Fields["nested"].(map[string]any)["value"] != "original" {
		t.Fatal("normalized fields alias caller input")
	}
}

func TestTodo_REV_018_02_Golden(t *testing.T) {
	input, context := fixtureCreationInput(), fixtureTrustedContext()
	out, err := CreateIntent(CreateKiosk, input, context, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.RequestDigest != "ac86d9b6970d0dcabd978e5d6115486525f8c1c37451ed1009e093d67fc9b233" {
		t.Fatalf("fixture digest drifted: %s", out.RequestDigest)
	}
	if out.Adaptation.InputMode != "guided-touch" || out.Adaptation.Confirmation != "guided" || out.Adaptation.Simulation != "available" || out.Adaptation.Handoff == nil || *out.Adaptation.Handoff != (SafeHandoff{Kind: "safe-resume", Reference: "resume:opaque", AuthorityToken: false}) {
		t.Fatalf("kiosk adaptation=%+v", out.Adaptation)
	}
	for _, channel := range IntentCreationChannels() {
		adapted, err := CreateIntent(channel, input, context, nil)
		if err != nil || adapted.RequestDigest != out.RequestDigest {
			t.Fatalf("%s golden mismatch: %+v %v", channel, adapted, err)
		}
	}
}

func digestForInput(t *testing.T, input IntentCreationInput, context TrustedContext) string {
	t.Helper()
	out, err := CreateIntent(CreateDesktop, input, context, nil)
	if err != nil {
		t.Fatal(err)
	}
	return out.RequestDigest
}
