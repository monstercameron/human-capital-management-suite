package app

import (
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// JourneyEventApprovalStartFailed is the stable projection kind for a failed
// approval-start attempt. Its payload remains a governed ledger Struct; this
// kind is only a presentation vocabulary and carries no unvalidated detail.
const JourneyEventApprovalStartFailed = "APPROVAL_START_FAILED"

// journeyFailureEvents reads only the owned failure schema and validates every
// field before it crosses into the journey presentation. Malformed or
// mismatched events are ignored so forged stream bytes cannot become a
// user-facing message.
func journeyFailureEvents(entries []TimelineEntry, intentID string) []workspace.JourneyEvent {
	out := make([]workspace.JourneyEvent, 0)
	for _, entry := range entries {
		if entry.SchemaRef != JourneyFailureSchemaRef || len(entry.Payload) == 0 {
			continue
		}
		failure, ok := decodeJourneyFailure(entry.Payload, intentID)
		if !ok {
			continue
		}
		out = append(out, workspace.JourneyEvent{
			At: entry.OccurredAt.UTC(), Actor: failure.actor,
			Kind:  JourneyEventApprovalStartFailed,
			Title: "Approval start failed", Detail: journeyFailureDetail(failure.reason),
			Ref: failure.revision,
		})
	}
	return out
}

type journeyFailureProjection struct {
	actor, reason, revision string
}

func decodeJourneyFailure(payload []byte, intentID string) (journeyFailureProjection, bool) {
	var value structpb.Struct
	if err := proto.Unmarshal(payload, &value); err != nil {
		return journeyFailureProjection{}, false
	}
	fields := value.GetFields()
	if len(fields) != 4 {
		return journeyFailureProjection{}, false
	}
	get := func(name string) (string, bool) {
		field, found := fields[name]
		if !found || field.GetKind() == nil {
			return "", false
		}
		text := strings.TrimSpace(field.GetStringValue())
		return text, text != ""
	}
	actor, actorOK := get("actor")
	storedIntent, intentOK := get("intent_id")
	reason, reasonOK := get("reason_ref")
	revisionField, revisionOK := fields["revision_id"]
	revision := ""
	if revisionOK && revisionField.GetKind() != nil {
		if _, isString := revisionField.GetKind().(*structpb.Value_StringValue); !isString {
			revisionOK = false
		}
		revision = strings.TrimSpace(revisionField.GetStringValue())
	}
	if !actorOK || !intentOK || storedIntent != intentID || !reasonOK || !revisionOK ||
		!journeyFailureReasonAllowed(reason) {
		return journeyFailureProjection{}, false
	}
	return journeyFailureProjection{actor: actor, reason: reason, revision: revision}, true
}

func journeyFailureDetail(reason string) string {
	switch reason {
	case journeyFailureDomainUnavailable:
		return "The approval service was unavailable. Try again."
	case journeyFailureStorageFailed:
		return "The approval could not be recorded. Try again."
	case journeyFailureStagePrecondition:
		return "This journey is no longer ready for approval. Review its current stage."
	default:
		return "The approval could not be started. Try again."
	}
}
