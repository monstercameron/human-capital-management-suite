package application

import (
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona/limits"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
)

// personaConcurrencyRetry is when a person who already has the most runs going
// is told to ask again. The ceiling frees as soon as one run returns, which the
// hourly window the store reports does not say, so the card names a near time.
const personaConcurrencyRetry = time.Minute

// personaMentionLimit is the typed mention-limit refusal inside err, if there
// is one.
func personaMentionLimit(err error) (*PersonaMentionLimitError, bool) {
	var limit *PersonaMentionLimitError
	return limit, errors.As(err, &limit) && limit != nil
}

func isPersonaMentionLimit(err error) bool {
	_, ok := personaMentionLimit(err)
	return ok
}

// personaMentionLimitOutcome is what the stored outcome of a mention-limit
// refusal carries for the card: when the person can ask again, and how many
// times they asked in the hour when the hourly per-person ceiling refused them.
// The other ceilings (concurrent runs, the room's hourly total, the agent's
// daily spend) are not "you asked N times", so they carry the time alone and
// the card falls back to its general limit sentence.
func personaMentionLimitOutcome(limit *PersonaMentionLimitError, now time.Time) (retryAt time.Time, asked int) {
	wait := limit.RetryAfter
	if limit.Code == limits.DenialInvokerConcurrency && (wait <= 0 || wait > personaConcurrencyRetry) {
		wait = personaConcurrencyRetry
	}
	if wait < time.Second {
		wait = time.Second
	}
	retryAt = now.UTC().Add(wait)
	if limit.Code == limits.DenialInvokerRate && limit.Observed > 0 {
		asked = int(limit.Observed)
	}
	return retryAt, asked
}

// personaPostFailureOutcome is the stored outcome of a failed post: the code
// and retryability the classification names, plus a mention-limit refusal's
// retry time and count.
func personaPostFailureOutcome(post agentinvocationstore.PostFailure, cause error, now time.Time) agentinvocationstore.PostFailure {
	post.Code, post.Retryable = personaPostFailureClassification(cause)
	if limit, ok := personaMentionLimit(cause); ok && post.Code == agentinvocationstore.MentionLimitCode {
		post.RetryAt, post.MentionsAsked = personaMentionLimitOutcome(limit, now)
	}
	return post
}
