package values

import "golang.org/x/text/language"

// LanguageTag is the owned, immutable canonical spelling of a BCP-47 tag.
type LanguageTag string

func (tag LanguageTag) String() string { return string(tag) }

// ParseLanguageTag returns a canonical tag without exposing an x/text type.
func ParseLanguageTag(raw string) (LanguageTag, bool) {
	tag, err := language.Parse(raw)
	if err != nil {
		return "", false
	}
	return LanguageTag(tag.String()), true
}

// CanonicalLanguageTag keeps the replaceable BCP-47 parser behind the owned
// value boundary. Callers decide which canonical tags their product admits.
func CanonicalLanguageTag(raw string) (string, bool) {
	tag, ok := ParseLanguageTag(raw)
	return tag.String(), ok
}
