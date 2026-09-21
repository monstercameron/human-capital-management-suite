package productui

import "strings"

// The employment facts the object page shows beside a worker's placement
// arrive as server vocabulary tokens -- REGULAR, PART_TIME, HYBRID,
// ANNUAL_SALARY -- because a locale change must not be a data migration. This
// file is the one place those tokens become the words a reader sees.
//
// Translation is deliberately not a plain locale.Text call on a derived key.
// LocaleContext.Text renders an unresolved key as "⟦key⟧", so a token this
// build has no word for would reach the page as visible debug output. Resolve
// is used instead and an unrecognized token falls back to the token itself:
// showing "SABBATICAL" is wrong-looking, but it is the truth the service sent
// and it is legible, which "⟦person.time_type.sabbatical⟧" is not.

// employmentVocabulary maps each token to the catalog key naming it.
//
// It is one flat map rather than one per field because the tokens are
// disjoint across the four vocabularies, and a single lookup keeps the
// accessors symmetric.
var employmentVocabulary = map[string]string{
	"REGULAR":    "person.employment_type.regular",
	"FIXED_TERM": "person.employment_type.fixed_term",

	"FULL_TIME": "person.time_type.full_time",
	"PART_TIME": "person.time_type.part_time",

	"ON_SITE": "person.work_arrangement.on_site",
	"HYBRID":  "person.work_arrangement.hybrid",
	"REMOTE":  "person.work_arrangement.remote",

	"ANNUAL_SALARY": "person.pay_frequency.annual",
	"HOURLY_RATE":   "person.pay_frequency.hourly",
}

// employmentTerm renders one vocabulary token in the reader's language.
//
// An empty token stays empty: "nobody asserted this" is a state the section
// engine already renders as unreported, and substituting a word here would
// take that answer away from it.
func employmentTerm(locale LocaleContext, token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	key, known := employmentVocabulary[token]
	if !known {
		return token
	}
	result, err := locale.Resolve(key)
	if err != nil {
		return token
	}
	return result.Text
}
