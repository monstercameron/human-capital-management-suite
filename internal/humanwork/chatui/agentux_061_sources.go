package chatui

import (
	"regexp"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AGENTUX-061. A source line reads "Title · Section · v1.0.0". The version is
// the least important part of it, so it is drawn as secondary text after the
// title. A source is a one-line chip, so its icon is centred on that line.

var agentux061VersionSuffix = regexp.MustCompile(`^(.*\S)\s+·\s+(v?\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.+-]+)?)$`)

// agentux061SourceParts splits a source title into what the person reads first
// and the version that ends it; version is "" when the title carries none.
func agentux061SourceParts(title string) (name, version string) {
	if found := agentux061VersionSuffix.FindStringSubmatch(strings.TrimSpace(title)); found != nil {
		return found[1], found[2]
	}
	return strings.TrimSpace(title), ""
}

// agentux061SourceLabel is the text of one source: the title isolated for its
// own direction, then the version as muted text in the same line. link is true
// for the one inside an anchor, whose title takes its direction from the page.
func agentux061SourceLabel(title string, link bool) []ui.Node {
	name, version := agentux061SourceParts(title)
	props := html.Props{Text: name}
	if !link {
		props.Dir = "auto"
	}
	label := []ui.Node{html.Tag("bdi", props)}
	if version != "" {
		label = append(label, html.Span(html.Props{Class: "agent-reply-source-version", Text: " · " + version}))
	}
	return label
}

const agentux061SourceStyles = `.agent-reply-source-version{color:var(--hcm-color-text-muted);font-size:.8125em;white-space:nowrap}`
