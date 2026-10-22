package chatgate

import (
	"fmt"
	"strings"
)

// CHATGATE-001: the decisions for channel gates, as data a test pins and a page
// can read. Each decision names what enforces it, so a change to the rule is a
// change to the record and to the enforcement together.

// Decision is one rule of the record. Enforced names the refusal or the check
// that holds it.
type Decision struct {
	Number   int
	Rule     string
	Enforced string
}

// Decisions are the eight decisions of the todo, in its order, and then the
// product decisions that fixed the numbers the consumers need.
func Decisions() []Decision {
	return []Decision{
		{1, "A gate is an additional requirement evaluated after the channel's existing eligibility policy, never instead of it.", "Service.CanJoin asks the authority for eligibility first; ChatgateJoinAdapter runs after the membership policy."},
		{2, "A gate is a versioned form with a semantic version. A major version requires members to answer again, a minor adds optional questions, a patch changes wording.", "RequiredBump; Service.Publish refuses a smaller version."},
		{3, "Admission modes are automatic, rule and review. Review is the default for private channels.", "Service.Define defaults the mode from Policy.Private."},
		{4, "Every field declares purpose, data class, who may see answers and retention. A gate may not ask for credentials, government identifiers or health, or what the tenant's policy names.", "Registry.ValidateDefinition refuses with ErrDenied."},
		{5, "Answers belong to the member, who can see, change and withdraw them. Withdrawing a required answer leaves the channel.", "Service.Submit, Service.Withdraw, Service.MySubmission."},
		{6, "Consumers read only the fields they declare, and every read is audited.", "Service.read, SegmentState application and Service.RunAgent each append a ReadAudit."},
		{7, "Answers passed to an agent are untrusted data, never instructions.", "Service.agentRead places them in a quarantined block; Service.RunAgent refuses any reply key the task did not ask for."},
		{8, "A gate never grants a role in the organization, never changes what a non-member can discover, and never hides a message from someone the channel's own policy lets read it unless the restricted-visibility rules are met.", "ErrReservedLabel; Service.Get; ErrRestricted."},
		{9, "A group is reported from a poll only with five or more voters (k = 5), and never so that one voter's choice can be subtracted out.", "MinGroup; BreakDown."},
		{10, "Restricted visibility is off by default, needs addressing, and exists only in private channels. The audience is the segment's members when the message is sent, frozen with it.", "Service.SetAudienceLevels; ResolveAudience; ErrRestricted."},
		{11, "A person may address content only to segments they are in; a manager may address any. Nobody learns who is in a segment by addressing it.", "ResolveAudience."},
		{12, "An agent reads answers only through its own grant, for the purposes the grant names, for questions that name the agent consumer. It never admits or declines anybody.", "Service.GrantAgent; Service.agentRead; no call changes a decision."},
		{13, "A person is limited to five submissions a minute, a channel to 120, and a person declined three times in a day waits for the next day.", "MaxSubmissionsPerPersonPerMinute, MaxSubmissionsPerChannelPerMinute, MaxDeclinesPerDay; ErrLimited."},
		{14, "Adding a gate never removes anyone. Current members are asked only if the administrator chooses \"ask\"; only an administrator's confirmed list is removed, after the date.", "Service.CanJoin (members are not locked out); Service.RemoveUnanswered."},
	}
}

// Nevers are the refusals of decision 8, each named so a test can assert it: the
// sentinel the service returns.
func Nevers() map[string]error {
	return map[string]error{
		"grant a role in the organization":             ErrReservedLabel,
		"change what a non-member can discover":        ErrDenied,
		"hide a message outside the restricted levels": ErrRestricted,
	}
}

// DecisionRecord is the record as text, in a fixed form. The golden file pins it.
func DecisionRecord() string {
	var b strings.Builder
	b.WriteString("Channel gates: decisions\n")
	for _, d := range Decisions() {
		fmt.Fprintf(&b, "%d. %s\n   Enforced by: %s\n", d.Number, d.Rule, d.Enforced)
	}
	return b.String()
}
