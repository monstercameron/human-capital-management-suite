// Package continuity is the forms-owned adapter for alternate-channel
// continuity records.
package continuity

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/formcontinuity"

type (
	Channel           = formcontinuity.Channel
	Outcome           = formcontinuity.Outcome
	Identity          = formcontinuity.Identity
	Authority         = formcontinuity.Authority
	Attribution       = formcontinuity.Attribution
	Privacy           = formcontinuity.Privacy
	Validation        = formcontinuity.Validation
	ContinuityRequest = formcontinuity.ContinuityRequest
	Request           = formcontinuity.Request
	ContinuityRecord  = formcontinuity.ContinuityRecord
	Record            = formcontinuity.Record
)

const (
	ChannelWeb        = formcontinuity.ChannelWeb
	ChannelRTL        = formcontinuity.ChannelRTL
	ChannelAccessible = formcontinuity.ChannelAccessible
	ChannelPhone      = formcontinuity.ChannelPhone
	ChannelInPerson   = formcontinuity.ChannelInPerson
	ChannelPostal     = formcontinuity.ChannelPostal
	OutcomeSafe       = formcontinuity.OutcomeSafe
	OutcomeBlocked    = formcontinuity.OutcomeBlocked
	OutcomeExpired    = formcontinuity.OutcomeExpired
	OutcomeRevalidate = formcontinuity.OutcomeRevalidate
)

var (
	Establish = formcontinuity.Establish
	Route     = formcontinuity.Route
)
