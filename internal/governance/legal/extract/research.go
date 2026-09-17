package extract

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Topic names one of the twelve sections every research file uses, per the
// template in planning/research/state-employment-law/README.md. Sections are
// identified by their heading text rather than by their number, because
// alabama.md writes them unnumbered while the other forty-nine number them.
type Topic string

// Topics, in file order.
const (
	TopicSummary        Topic = "summary"
	TopicRelationship   Topic = "employment relationship"
	TopicWages          Topic = "wages"
	TopicTransparency   Topic = "pay transparency"
	TopicLeave          Topic = "leave"
	TopicRecords        Topic = "records"
	TopicPrivacy        Topic = "privacy"
	TopicHiring         Topic = "hiring"
	TopicSeparation     Topic = "separation"
	TopicClassification Topic = "classification"
	TopicImplications   Topic = "implications"
	TopicSources        Topic = "sources"
	TopicUnknown        Topic = ""
)

var topicPrefixes = []struct {
	prefix string
	topic  Topic
}{
	{"summary", TopicSummary},
	{"employment relationship", TopicRelationship},
	{"wages", TopicWages},
	{"pay transparency", TopicTransparency},
	{"leave and time", TopicLeave},
	{"leave", TopicLeave},
	{"records and access", TopicRecords},
	{"records", TopicRecords},
	{"privacy", TopicPrivacy},
	{"hiring", TopicHiring},
	{"separation", TopicSeparation},
	{"classification", TopicClassification},
	{"implications", TopicImplications},
	{"sources", TopicSources},
}

// Item is one extracted bullet or numbered point, with the topic section and
// the bold sub-heading it appeared under. Group matters: an item under an
// "Open questions" heading is evidence that something is uncertain, never
// evidence that a duty exists.
type Item struct {
	Topic Topic
	Group string
	Text  string
	// numbered marks an item drawn from a numbered-list line, as opposed to
	// a bullet or paragraph. Only numbered Implications heads own detail
	// bullets (see ParseResearch).
	numbered bool
	// detailOf is the index of the numbered head this bullet elaborates, or
	// -1. Detail bullets stay searchable on their own; the head additionally
	// absorbs their text so rule parameters survive extraction.
	detailOf int
}

// ResearchFile is one parsed state file.
type ResearchFile struct {
	// Path is the repository-relative path every citation points back to.
	Path string
	// Items are every bullet and numbered point outside the Sources section,
	// in document order.
	Items []Item
	// OpenQuestions are the items that sat under an open-questions heading.
	// They are never evidence of a duty; they are why a marker says VERIFY.
	OpenQuestions []Item
}

var (
	headingRE     = regexp.MustCompile(`^##\s+(?:\d+\.\s*)?(.*)$`)
	boldHeaderRE  = regexp.MustCompile(`^\*\*(.+?)\*\*:?\s*$`)
	bulletRE      = regexp.MustCompile(`^\s*[-*]\s+(.*)$`)
	numberedRE    = regexp.MustCompile(`^\s*\d+\.\s+(.*)$`)
	boldMarkupRE  = regexp.MustCompile(`\*\*|__|` + "`")
	whitespaceRE  = regexp.MustCompile(`\s+`)
	openQuestions = regexp.MustCompile(`(?i)open question|verification item|open-question|to verify`)
)

// ParseResearchFile reads and parses one state research file.
func ParseResearchFile(root, relPath string) (*ResearchFile, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relPath)))
	if err != nil {
		return nil, fmt.Errorf("extract: reading %s: %w", relPath, err)
	}
	return ParseResearch(relPath, string(data)), nil
}

// ParseResearch parses a research file's markdown into topic-tagged items.
//
// In the Implications section a numbered head owns the bullets that
// elaborate it: "9. Iowa WARN (§ 84C) - Layoff Scenario: ..." states the
// rule, and the indented bullets beneath it carry the 25-employee threshold
// and the 30-day period. The head absorbs each immediately-adjacent bullet's
// text (the bullets stay searchable on their own) so extraction sees the
// rule statement whole. A blank line, a new group, or a new heading breaks
// ownership: the "previously unverified" bullets that follow a resolved
// head are freestanding evidence, never its detail.
func ParseResearch(relPath, markdown string) *ResearchFile {
	out := &ResearchFile{Path: relPath}
	topic := TopicUnknown
	group := ""
	var pending []string
	pendingNumbered := false
	pendingDetailOf := -1
	// adjacent is false once a blank line, heading, or group header passes
	// since the last content line, which is what breaks head ownership.
	adjacent := false
	flush := func() {
		if len(pending) == 0 {
			pendingNumbered = false
			pendingDetailOf = -1
			return
		}
		text := normalizeText(strings.Join(pending, " "))
		pending = pending[:0]
		numbered := pendingNumbered
		detailOf := pendingDetailOf
		pendingNumbered = false
		pendingDetailOf = -1
		if text == "" {
			return
		}
		item := Item{Topic: topic, Group: group, Text: text, numbered: numbered, detailOf: detailOf}
		if detailOf >= 0 && detailOf < len(out.Items) {
			out.Items[detailOf].Text += " " + text
		}
		if openQuestions.MatchString(group) {
			out.OpenQuestions = append(out.OpenQuestions, item)
			return
		}
		out.Items = append(out.Items, item)
	}
	// detailTarget returns the head index an Implications bullet elaborates,
	// or -1 when the bullet is freestanding.
	detailTarget := func() int {
		if topic != TopicImplications || !adjacent || len(out.Items) == 0 {
			return -1
		}
		prev := out.Items[len(out.Items)-1]
		if prev.Topic != TopicImplications || prev.Group != group {
			return -1
		}
		if prev.numbered {
			return len(out.Items) - 1
		}
		return prev.detailOf
	}

	for _, raw := range strings.Split(markdown, "\n") {
		line := strings.TrimRight(raw, "\r")
		if m := headingRE.FindStringSubmatch(line); m != nil {
			flush()
			topic = classifyTopic(m[1])
			group = ""
			adjacent = false
			continue
		}
		if topic == TopicSources || topic == TopicUnknown {
			continue
		}
		if m := boldHeaderRE.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			flush()
			group = normalizeText(m[1])
			adjacent = false
			continue
		}
		if m := bulletRE.FindStringSubmatch(line); m != nil {
			flush()
			pendingDetailOf = detailTarget()
			pending = append(pending, m[1])
			adjacent = true
			continue
		}
		if m := numberedRE.FindStringSubmatch(line); m != nil {
			flush()
			pendingNumbered = true
			pending = append(pending, m[1])
			adjacent = true
			continue
		}
		if strings.TrimSpace(line) == "" {
			flush()
			adjacent = false
			continue
		}
		// A plain paragraph is an item too. Several files state a rule as a
		// bold-led paragraph ("**Pay frequency:** § 204 requires wages at
		// least semimonthly...") rather than as a bullet, and skipping those
		// lines would drop the best-cited evidence in the corpus.
		pending = append(pending, strings.TrimSpace(line))
		adjacent = true
	}
	flush()
	return out
}

func classifyTopic(heading string) Topic {
	lowered := strings.ToLower(strings.TrimSpace(heading))
	for _, p := range topicPrefixes {
		if strings.HasPrefix(lowered, p.prefix) {
			return p.topic
		}
	}
	return TopicUnknown
}

// normalizeText strips markdown emphasis and collapses whitespace, so that a
// reflowed research file produces the same extracted text and therefore the
// same definition file.
func normalizeText(s string) string {
	s = boldMarkupRE.ReplaceAllString(s, "")
	s = whitespaceRE.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// ItemsForSearch returns the items to scan for one kind, in priority order:
// the Implications section first (the contract names section 11 as the
// extraction input), then the Summary, then the topic section that owns the
// kind. The topic fallback exists because section 11 states what the platform
// must do and frequently omits the underlying rule; without it, thirty states
// with a minimum wage would produce no WAGE_FLOOR obligation at all.
// The last pass is the whole file in document order, because the corpus does
// not keep to its own template: Delaware states its retaliation rule under
// Hiring and Connecticut states one under Leave. Searching the remaining
// sections finds the rule where the file actually put it; it never reaches
// outside the file, and the Sources section and the open-questions items are
// excluded before any pass runs.
func (f *ResearchFile) ItemsForSearch(owning Topic) []Item {
	seen := map[int]bool{}
	var out []Item
	appendTopic := func(match func(Item) bool) {
		for i, item := range f.Items {
			if seen[i] || !match(item) {
				continue
			}
			seen[i] = true
			out = append(out, item)
		}
	}
	for _, t := range []Topic{TopicImplications, TopicSummary, owning} {
		if t == TopicUnknown {
			continue
		}
		topic := t
		appendTopic(func(item Item) bool { return item.Topic == topic })
	}
	appendTopic(func(Item) bool { return true })
	return out
}
