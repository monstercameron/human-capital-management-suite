package chat

import (
	"strings"
	"time"
)

// Issue 3793: a person asks for a governed change from the composer. A typed
// request command (/leave) is one entry of a small catalog that the page and
// the server both read: its name, the typed fields it asks for, and the
// BusinessIntent it becomes. The page draws the form from the fields and the
// server checks every value again, so the two cannot drift apart. A request is
// only ever a proposal for review: nothing here approves or carries out the
// change.

// IntentRequestOwner is who registers the request commands: the platform, not
// Chat's own list.
const IntentRequestOwner = "requests"

// Request commands.
const (
	IntentRequestLeave = "leave"
)

// IntentRequestErrBadRange is the field code for an end date before the start.
const IntentRequestErrBadRange = "bad_range"

// IntentRequestCommand is one typed request command. Fields are the same typed
// fields a form card uses; labels are the page's copy, not part of the
// definition, so a label is looked up by command and field id in each language.
type IntentRequestCommand struct {
	Name string
	// Description is one line for the menu, in English; the page has its own copy.
	Description string
	// IntentType, SchemaID, SchemaVersion and ProtoFullName name the
	// BusinessIntent the request becomes.
	IntentType    string
	SchemaID      string
	SchemaVersion uint32
	ProtoFullName string
	// Fields are what the person fills in.
	Fields []FormField
	// SubjectField is the person field the request is about, and SubjectKind and
	// SubjectDomain how the intent kernel names that subject.
	SubjectField  string
	SubjectKind   string
	SubjectDomain string
}

// IntentRequestCommands lists the request commands in the order the menu shows
// them.
func IntentRequestCommands() []IntentRequestCommand {
	return []IntentRequestCommand{IntentRequestLeaveCommand()}
}

// IntentRequestLookup finds a request command by name.
func IntentRequestLookup(name string) (IntentRequestCommand, bool) {
	for _, command := range IntentRequestCommands() {
		if command.Name == strings.ToLower(strings.TrimSpace(name)) {
			return command, true
		}
	}
	return IntentRequestCommand{}, false
}

// IntentRequestLeaveCommand is /leave: leave for one person over a range of
// days, with an optional approver to name and a reason. Which approver decides
// is the governed workflow's own rule; the one named here is only a note for
// the reviewer.
func IntentRequestLeaveCommand() IntentRequestCommand {
	return IntentRequestCommand{
		Name:          IntentRequestLeave,
		Description:   "Ask for leave in this conversation",
		IntentType:    "hcmnext.workforce.leave_request",
		SchemaID:      "hcmnext.workforce.v1.RequestLeaveRequest",
		SchemaVersion: 1,
		ProtoFullName: "hcmnext.workforce.v1.RequestLeaveRequest",
		SubjectField:  "employee",
		SubjectKind:   "EMPLOYMENT",
		SubjectDomain: "PEOPLE",
		Fields: []FormField{
			{ID: "employee", Type: FormFieldPerson, Required: true},
			{ID: "starts", Type: FormFieldDate, Required: true},
			{ID: "ends", Type: FormFieldDate, Required: true},
			{ID: "approver", Type: FormFieldPerson},
			{ID: "reason", Type: FormFieldText, Required: true, MaxLength: 500},
		},
	}
}

// Card is the command's fields as a form card, which carries the checks every
// typed field shares. The addressee is the person asking.
func (c IntentRequestCommand) Card(asker string) FormCard {
	fields := append([]FormField(nil), c.Fields...)
	for i := range fields {
		// The page words each label; the id stands in so the card is a valid form.
		fields[i].Label = fields[i].ID
	}
	return FormCard{Title: c.Name, Addressee: asker, Fields: fields}
}

// Check validates a request's values: every field by its own type, then the
// rules between fields (the leave ends on or after the day it starts). It
// returns the cleaned values, or a *FormValidationError naming every refused
// field. isPerson says whether a subject may be chosen (a current member of the
// conversation).
func (c IntentRequestCommand) Check(values map[string]string, isPerson func(string) (bool, error)) (map[string]string, error) {
	clean, err := c.Card("asker").CheckAnswers(values, isPerson)
	if err != nil {
		return nil, err
	}
	if c.Name == IntentRequestLeave {
		starts, _ := time.Parse("2006-01-02", clean["starts"])
		ends, _ := time.Parse("2006-01-02", clean["ends"])
		if !starts.IsZero() && !ends.IsZero() && ends.Before(starts) {
			return nil, &FormValidationError{Fields: map[string]string{"ends": IntentRequestErrBadRange}}
		}
	}
	return clean, nil
}

// Payload is the typed request the intent receives: the person's own values,
// keyed by field id, and nothing the caller can use to claim authority (no
// tenant, initiator or approval).
func (c IntentRequestCommand) Payload(clean map[string]string) map[string]any {
	payload := map[string]any{"command": c.Name}
	for _, f := range c.Fields {
		if value, ok := clean[f.ID]; ok {
			payload[f.ID] = value
		}
	}
	return payload
}

// Chatcmd001WithIntentRequests returns the registry with the typed request
// commands added through the versioned registration interface. Like every
// command they work only in an open conversation between people (not one with
// an agent) and show what will be sent before it is. A registry that already
// holds a name keeps its own command.
func Chatcmd001WithIntentRequests(registry Chatcmd001Registry) Chatcmd001Registry {
	for _, command := range IntentRequestCommands() {
		if _, taken := registry.Lookup(command.Name); taken {
			continue
		}
		added, err := registry.Register(Chatcmd001Version, Chatcmd001Command{
			Name: command.Name, Description: command.Description,
			Usage: "/" + command.Name, Example: "/" + command.Name, Preview: true, Owner: IntentRequestOwner,
		})
		if err == nil {
			registry = added
		}
	}
	return registry
}
