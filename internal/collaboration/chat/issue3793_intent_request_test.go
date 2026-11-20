package chat

import (
	"errors"
	"testing"
)

func issue3793Member(subject string) (bool, error) { return subject == "ana" || subject == "bo", nil }

func TestIssue3793_LeaveCommandIsAValidTypedForm(t *testing.T) {
	command := IntentRequestLeaveCommand()
	if err := command.Card("ana").Validate(); err != nil {
		t.Fatalf("the leave form must be a valid typed form: %v", err)
	}
	if command.IntentType == "" || command.SchemaID == "" || command.SchemaVersion == 0 || command.SubjectField != "employee" {
		t.Fatalf("the leave command must name the intent it becomes: %+v", command)
	}
	if _, ok := IntentRequestLookup(" LEAVE "); !ok {
		t.Fatal("a request command is found by name in any case")
	}
	if _, ok := IntentRequestLookup("fire"); ok {
		t.Fatal("only catalogued commands exist")
	}
}

func TestIssue3793_CheckCleansValuesAndNamesEveryRefusedField(t *testing.T) {
	command := IntentRequestLeaveCommand()
	clean, err := command.Check(map[string]string{"employee": "ana", "starts": "2026-12-01", "ends": "2026-12-05", "reason": "  Family trip ", "approver": ""}, issue3793Member)
	if err != nil {
		t.Fatalf("valid values were refused: %v", err)
	}
	if clean["reason"] != "Family trip" || clean["employee"] != "ana" {
		t.Fatalf("values were not cleaned: %v", clean)
	}
	if _, present := clean["approver"]; present {
		t.Fatalf("an empty optional field must be left out: %v", clean)
	}

	_, err = command.Check(map[string]string{"employee": "mallory", "starts": "2026-02-31", "reason": "", "x": "1"}, issue3793Member)
	var invalid *FormValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("want a validation error, got %v", err)
	}
	want := map[string]string{"employee": FormErrUnknownUser, "starts": FormErrBadDate, "ends": FormErrRequired, "reason": FormErrRequired, "x": FormErrUnknownField}
	for field, code := range want {
		if invalid.Fields[field] != code {
			t.Fatalf("field %s: want %s, got %q (all: %v)", field, code, invalid.Fields[field], invalid.Fields)
		}
	}
}

func TestIssue3793_LeaveCannotEndBeforeItStarts(t *testing.T) {
	_, err := IntentRequestLeaveCommand().Check(map[string]string{"employee": "ana", "starts": "2026-12-05", "ends": "2026-12-01", "reason": "x"}, issue3793Member)
	var invalid *FormValidationError
	if !errors.As(err, &invalid) || invalid.Fields["ends"] != IntentRequestErrBadRange {
		t.Fatalf("an end before the start must be refused on the end field, got %v", err)
	}
	if _, err := IntentRequestLeaveCommand().Check(map[string]string{"employee": "ana", "starts": "2026-12-05", "ends": "2026-12-05", "reason": "x"}, issue3793Member); err != nil {
		t.Fatalf("a one-day leave is allowed: %v", err)
	}
}

func TestIssue3793_PayloadCarriesOnlyTheTypedValues(t *testing.T) {
	payload := IntentRequestLeaveCommand().Payload(map[string]string{"employee": "ana", "starts": "2026-12-01", "ends": "2026-12-05", "reason": "x"})
	if payload["command"] != "leave" || payload["employee"] != "ana" || payload["reason"] != "x" {
		t.Fatalf("payload %v", payload)
	}
	for _, forbidden := range []string{"tenant", "initiator", "approved", "status"} {
		if _, present := payload[forbidden]; present {
			t.Fatalf("payload must not carry %q: %v", forbidden, payload)
		}
	}
}

func TestIssue3793_RegistryAddsRequestCommandsWithoutChangingDefaults(t *testing.T) {
	defaults := Chatcmd001Defaults()
	with := Chatcmd001WithIntentRequests(defaults)
	command, ok := with.Lookup("leave")
	if !ok || !command.Preview || command.Owner != IntentRequestOwner || command.Agents {
		t.Fatalf("leave must register as a previewed command that is not offered to agents: %+v ok=%v", command, ok)
	}
	if _, own := defaults.Lookup("leave"); own {
		t.Fatal("the product's own list must stay as it was")
	}
	if again := Chatcmd001WithIntentRequests(with); len(again.Commands()) != len(with.Commands()) {
		t.Fatal("registering twice must not add twice")
	}
	if !command.AllowedIn(Chatcmd001Place{Kind: Direct}) || command.AllowedIn(Chatcmd001Place{Kind: Direct, Agent: true}) {
		t.Fatal("leave works with people and not with an agent")
	}
}
