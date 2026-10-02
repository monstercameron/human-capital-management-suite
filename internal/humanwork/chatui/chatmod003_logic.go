package chatui

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// The editor's own vocabulary for what a filter checks. Two of its choices are
// one stored kind: both "sensitive" and "links" are detectors.
const (
	ModKindWords      = "words"
	ModKindPattern    = "pattern"
	ModKindSensitive  = "sensitive"
	ModKindLinks      = "links"
	ModKindAttachment = "attachment"
)

// ModKinds is the order of the editor's "What it checks" choices.
var ModKinds = []string{ModKindWords, ModKindPattern, ModKindSensitive, ModKindLinks, ModKindAttachment}

// ModActions is the order of the editor's "What happens" choices; the built-in
// lists offer the first three.
var ModActions = []string{"block", "mask", "flag", "notify"}

// ModDetectors is the order of the sensitive-data choices.
var ModDetectors = []string{"card", "national-id", "access-key", "secret"}

// ModKindChoice is the editor choice a stored definition corresponds to.
func ModKindChoice(d chatfilter.Definition) string {
	switch d.Kind {
	case "pattern":
		return ModKindPattern
	case "attachment":
		return ModKindAttachment
	case "detector":
		if len(d.Match) > 0 && d.Match[0] == "external-link" {
			return ModKindLinks
		}
		return ModKindSensitive
	}
	return ModKindWords
}

// ModIsCustom reports whether a definition was written by an administrator
// rather than shipped with the product.
func ModIsCustom(d chatfilter.Definition) bool {
	return !d.Product && !strings.HasPrefix(d.ID, "builtin-")
}

// ModNewRuleID makes the id of a new custom filter: the name as plain lower
// case letters and digits, then a short random suffix, so two channels'
// "Project" filters never collide. An edit keeps the id it has. entropy is
// crypto/rand when nil.
func ModNewRuleID(name string, entropy io.Reader) (string, error) {
	if entropy == nil {
		entropy = rand.Reader
	}
	var slug strings.Builder
	dash := true
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			slug.WriteRune(r)
			dash = false
		case r == ' ' || r == '-' || r == '_':
			if !dash {
				slug.WriteByte('-')
				dash = true
			}
		}
		if slug.Len() >= 32 {
			break
		}
	}
	base := strings.Trim(slug.String(), "-")
	if base == "" {
		base = "filter"
	}
	raw := make([]byte, 3)
	if _, err := io.ReadFull(entropy, raw); err != nil {
		return "", err
	}
	id := base + "-" + hex.EncodeToString(raw)
	if strings.HasPrefix(id, "builtin-") {
		id = "custom-" + id
	}
	return id, nil
}

// ModNextVersion is the version an edit saves: the patch number after the one
// the loaded definition has. A version that does not read as x.y.z starts the
// next line at 1.0.1.
func ModNextVersion(version string) string {
	parts := strings.Split(strings.TrimSpace(version), ".")
	if len(parts) != 3 {
		return "1.0.1"
	}
	patch, err := strconv.ParseUint(parts[2], 10, 32)
	if err != nil {
		return "1.0.1"
	}
	next := parts[0] + "." + parts[1] + "." + strconv.FormatUint(patch+1, 10)
	if !chatfilter.ValidVersion(next) {
		return "1.0.1"
	}
	return next
}

// ModState is what one filter does where the administrator is looking: in a
// channel it is that channel's own row when there is one, else the workspace
// row, else off; on the workspace page it is the workspace row.
type ModState struct {
	On     bool
	Action string
	// DryRun is true while the filter only records.
	DryRun bool
	// HasOverride is true when this channel has a row of its own for the filter.
	HasOverride bool
	OverrideOn  bool
	// WorkspaceOn and WorkspaceAction are the workspace row's values; both are
	// the "off" defaults when the workspace has no row.
	WorkspaceOn     bool
	WorkspaceAction string
	// Differs is true when this channel's own row is not the workspace setting,
	// which is when "Use the workspace setting" has something to undo.
	Differs bool
}

// modRows finds the workspace row and this channel's row of one filter. A
// channel row only counts for a built-in list or a filter written for exactly
// one channel, the same rule the server evaluates by.
func modRows(def chatfilter.Definition, rows []chatfilter.Enablement, channel string) (workspace, own *chatfilter.Enablement) {
	for i := range rows {
		row := &rows[i]
		if row.RuleID != def.ID {
			continue
		}
		if row.Channel == "" {
			workspace = row
		} else if channel != "" && row.Channel == channel && (def.Product || len(def.Channels) == 1) {
			own = row
		}
	}
	return workspace, own
}

func modRowAction(def chatfilter.Definition, row *chatfilter.Enablement) string {
	if row != nil && row.Action != "" && def.Product {
		return row.Action
	}
	return def.Action
}

// ModResolve answers what a filter does for the viewer. channel is "" on the
// workspace page.
func ModResolve(def chatfilter.Definition, rows []chatfilter.Enablement, channel string, now time.Time) ModState {
	// A filter written for exactly one channel keeps its row at that channel,
	// wherever it is looked at from.
	if !def.Product && len(def.Channels) == 1 {
		channel = def.Channels[0]
	}
	workspace, own := modRows(def, rows, channel)
	state := ModState{Action: modRowAction(def, workspace), WorkspaceAction: modRowAction(def, workspace)}
	if workspace != nil {
		state.WorkspaceOn = workspace.Enabled
	}
	winner := workspace
	if own != nil {
		winner = own
		state.HasOverride = true
		state.OverrideOn = own.Enabled
		state.Action = modRowAction(def, own)
		state.Differs = own.Enabled != state.WorkspaceOn || (own.Enabled && state.Action != state.WorkspaceAction)
	}
	if winner != nil {
		state.On = winner.Enabled
		state.DryRun = winner.Enabled && winner.DryRunUntil.After(now)
	}
	return state
}

// ModStatusKey names the copy line that says where a built-in list stands.
func ModStatusKey(state ModState, workspaceMode bool) string {
	switch {
	case state.DryRun:
		return "s_dry"
	case workspaceMode || !state.HasOverride || !state.Differs:
		if state.On {
			return "s_ws_on"
		}
		return "s_off"
	case state.On && !state.WorkspaceOn:
		return "s_chan_only"
	case state.On:
		return "s_chan_own"
	case state.WorkspaceOn:
		return "s_chan_off"
	}
	return "s_off"
}

// ModSwitch is one write the panel asks for: set the filter's row at Channel
// ("" is the workspace row) to On with Action.
type ModSwitch struct {
	RuleID, Channel string
	On              bool
	Action          string
}

// modWriteChannel is the level a filter's row lives at: a built-in list's row
// is the workspace's or this channel's; a custom filter written for exactly one
// channel keeps its row at that channel and any other custom filter at the
// workspace.
func modWriteChannel(def chatfilter.Definition, channel string, workspaceMode bool) string {
	if !def.Product {
		if len(def.Channels) == 1 {
			return def.Channels[0]
		}
		return ""
	}
	if workspaceMode {
		return ""
	}
	return channel
}

func modWriteAction(def chatfilter.Definition, action string) string {
	if !def.Product {
		return ""
	}
	return action
}

// ModToggleRequest is the write a flip of the filter's switch makes.
func ModToggleRequest(def chatfilter.Definition, rows []chatfilter.Enablement, channel string, workspaceMode bool, now time.Time) ModSwitch {
	if workspaceMode {
		channel = ""
	}
	state := ModResolve(def, rows, channel, now)
	return ModSwitch{RuleID: def.ID, Channel: modWriteChannel(def, channel, workspaceMode), On: !state.On, Action: modWriteAction(def, state.Action)}
}

// ModActionRequest is the write a change of "What happens" makes. It keeps the
// list on.
func ModActionRequest(def chatfilter.Definition, channel string, workspaceMode bool, action string) ModSwitch {
	if workspaceMode {
		channel = ""
	}
	return ModSwitch{RuleID: def.ID, Channel: modWriteChannel(def, channel, workspaceMode), On: true, Action: modWriteAction(def, action)}
}

// ModResetRequest is the write "Use the workspace setting" makes. The server
// has no way to delete a channel's row, so the channel's row is set equal to
// the workspace's.
func ModResetRequest(def chatfilter.Definition, rows []chatfilter.Enablement, channel string, now time.Time) ModSwitch {
	state := ModResolve(def, rows, channel, now)
	return ModSwitch{RuleID: def.ID, Channel: channel, On: state.WorkspaceOn, Action: modWriteAction(def, state.WorkspaceAction)}
}

// ModDefinitionByID finds a definition by its id.
func ModDefinitionByID(defs []chatfilter.Definition, id string) (chatfilter.Definition, bool) {
	for _, d := range defs {
		if d.ID == id {
			return d, true
		}
	}
	return chatfilter.Definition{}, false
}

// ModLists is what the panel shows, already divided into its sections.
type ModLists struct {
	// Languages is the built-in languages in the order they are shown; Builtin
	// holds each one's lists in the order profanity, slurs, harassment.
	Languages []string
	Builtin   map[string][]chatfilter.Definition
	// Own is the custom filters the viewer can change here: this channel's own
	// in a channel, every custom filter on the workspace page.
	Own []chatfilter.Definition
	// Admin is the workspace-wide custom filters that apply in this channel.
	Admin []chatfilter.Definition
}

var modListOrder = []string{"profanity", "slurs", "harassment"}

func modListRank(name string) int {
	for i, n := range modListOrder {
		if n == name {
			return i
		}
	}
	return len(modListOrder)
}

// ModGroup divides the definitions the server delivered into the panel's
// sections. locale puts the viewer's own language first.
func ModGroup(defs []chatfilter.Definition, channel string, workspaceMode bool, locale string) ModLists {
	return ModGroupFor(defs, channel, workspaceMode, locale, true)
}

// ModGroupFor is ModGroup for a viewer who is, or is not, a workspace
// administrator. A filter an administrator wrote for this one channel is the
// channel manager's to read and not to change (the server refuses the change),
// so for a manager it is listed with the workspace's filters.
func ModGroupFor(defs []chatfilter.Definition, channel string, workspaceMode bool, locale string, administrator bool) ModLists {
	out := ModLists{Builtin: map[string][]chatfilter.Definition{}}
	for _, d := range defs {
		switch {
		case d.Product || strings.HasPrefix(d.ID, "builtin-"):
			out.Builtin[d.Language] = append(out.Builtin[d.Language], d)
		case workspaceMode:
			out.Own = append(out.Own, d)
		case len(d.Channels) == 1 && d.Channels[0] == channel && !administrator && d.Authority != chatfilter.AuthorityChannel:
			out.Admin = append(out.Admin, d)
		case len(d.Channels) == 1 && d.Channels[0] == channel:
			out.Own = append(out.Own, d)
		case len(d.Channels) == 0 || modContains(d.Channels, channel):
			out.Admin = append(out.Admin, d)
		}
	}
	for language := range out.Builtin {
		lists := out.Builtin[language]
		sort.SliceStable(lists, func(i, j int) bool {
			if ri, rj := modListRank(lists[i].Name), modListRank(lists[j].Name); ri != rj {
				return ri < rj
			}
			return lists[i].ID < lists[j].ID
		})
	}
	first := strings.ToLower(strings.Split(locale, "-")[0])
	order := []string{"en", "de", "ar"}
	var languages []string
	if _, ok := out.Builtin[first]; ok {
		languages = append(languages, first)
	}
	for _, language := range order {
		if _, ok := out.Builtin[language]; ok && language != first {
			languages = append(languages, language)
		}
	}
	var rest []string
	for language := range out.Builtin {
		if !modContains(languages, language) {
			rest = append(rest, language)
		}
	}
	sort.Strings(rest)
	out.Languages = append(languages, rest...)
	byName := func(list []chatfilter.Definition) {
		sort.SliceStable(list, func(i, j int) bool {
			if a, b := strings.ToLower(list[i].Name), strings.ToLower(list[j].Name); a != b {
				return a < b
			}
			return list[i].ID < list[j].ID
		})
	}
	byName(out.Own)
	byName(out.Admin)
	return out
}

func modContains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// ModForm is what the editor's fields hold when Save or Try is pressed.
type ModForm struct {
	// ID and Version are the loaded definition's when an edit is saved; an edit
	// keeps the id and saves the next patch version, and keeps its scope.
	Edit          bool
	ID, Version   string
	KeepChannels  []string
	Name          string
	Kind          string
	Match         string
	Detector      string
	Domains       string
	Action        string
	Target        string
	Scope         string // "channel", "workspace" or "chosen"
	Channel       string // the open channel, for the "channel" scope
	Chosen        []string
	Roles, Agents string
	Hard          bool
	// Admin is true for a workspace administrator; only one may make a filter
	// that also applies in direct messages.
	Admin bool
	// ForTry builds a definition for the Try box: no name or place is needed,
	// and the definition applies wherever the sample is tried.
	ForTry bool
}

// ModFieldError names the field that is wrong and the copy line that says why.
type ModFieldError struct{ Field, Key string }

func (e *ModFieldError) Error() string { return e.Field + ": " + e.Key }

func modLines(value string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(value, "\r", ""), "\n") {
		if term := strings.TrimSpace(line); term != "" {
			out = append(out, term)
		}
	}
	return out
}

// modDomain reduces what an administrator typed for an allowed site ("https://
// Docs.Example.com/page") to the host name the detector compares against.
func modDomain(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if i := strings.Index(value, "://"); i >= 0 {
		value = value[i+3:]
	}
	if i := strings.IndexAny(value, "/?#"); i >= 0 {
		value = value[:i]
	}
	if i := strings.LastIndex(value, "@"); i >= 0 {
		value = value[i+1:]
	}
	if i := strings.LastIndex(value, ":"); i >= 0 && !strings.Contains(value[i:], "]") {
		value = value[:i]
	}
	return strings.TrimSpace(value)
}

// ModDefinitionFromForm builds the definition the form describes, or names the
// field that is wrong. entropy is only read for a new filter's id.
func ModDefinitionFromForm(f ModForm, entropy io.Reader) (chatfilter.Definition, *ModFieldError) {
	d := chatfilter.Definition{Name: strings.TrimSpace(f.Name), Action: f.Action, Target: strings.TrimSpace(f.Target), ExemptRoles: modLines(f.Roles), ExemptAgents: modLines(f.Agents)}
	if d.Name == "" && f.ForTry {
		d.Name = "Try"
	}
	if d.Name == "" {
		return d, &ModFieldError{"name", "v_name"}
	}
	if len(d.Name) > 100 {
		return d, &ModFieldError{"name", "v_name_long"}
	}
	field := "match"
	switch f.Kind {
	case ModKindWords:
		d.Kind, d.Match = "words", modLines(f.Match)
		if len(d.Match) == 0 {
			return d, &ModFieldError{field, "v_words"}
		}
	case ModKindPattern:
		d.Kind, d.Match = "pattern", modLines(f.Match)
		if len(d.Match) == 0 {
			return d, &ModFieldError{field, "v_pattern"}
		}
	case ModKindAttachment:
		d.Kind = "attachment"
		for _, line := range modLines(f.Match) {
			d.Match = append(d.Match, strings.ToLower(line))
		}
		if len(d.Match) == 0 {
			return d, &ModFieldError{field, "v_attachment"}
		}
	case ModKindSensitive:
		field = "detector"
		d.Kind = "detector"
		if !modContains(ModDetectors, f.Detector) {
			return d, &ModFieldError{field, "v_sensitive"}
		}
		d.Match = []string{f.Detector}
	case ModKindLinks:
		field = "domains"
		d.Kind = "detector"
		d.Match = []string{"external-link"}
		for _, line := range modLines(f.Domains) {
			if host := modDomain(line); host != "" {
				d.Match = append(d.Match, host)
			}
		}
	default:
		return d, &ModFieldError{"kind", "v_invalid"}
	}
	if len(d.Match) > 128 {
		return d, &ModFieldError{field, "v_too_many"}
	}
	for _, term := range d.Match {
		if len(term) > 512 {
			return d, &ModFieldError{field, "v_line_long"}
		}
	}
	if d.Action == "notify" && d.Target == "" {
		return d, &ModFieldError{"target", "v_target"}
	}
	if d.Action != "notify" {
		d.Target = ""
	}
	switch {
	case f.ForTry:
	case f.Edit:
		d.Channels = append([]string(nil), f.KeepChannels...)
	case f.Scope == "channel":
		d.Channels = []string{f.Channel}
	case f.Scope == "chosen":
		d.Channels = append([]string(nil), f.Chosen...)
		if len(d.Channels) == 0 {
			return d, &ModFieldError{"scope", "v_channels"}
		}
	}
	d.Hard = f.Hard && (f.Admin || f.Edit)
	if f.Edit {
		d.ID, d.Version = f.ID, ModNextVersion(f.Version)
	} else {
		id, err := ModNewRuleID(d.Name, entropy)
		if err != nil {
			return d, &ModFieldError{"name", "v_invalid"}
		}
		d.ID, d.Version = id, "1.0.0"
	}
	if _, err := chatfilter.NewRegistry().Compile([]chatfilter.Definition{d}); err != nil {
		key := "v_invalid"
		switch f.Kind {
		case ModKindPattern:
			key = "v_pattern_bad"
		case ModKindWords:
			key = "v_words_bad"
		}
		return d, &ModFieldError{field, key}
	}
	return d, nil
}

// ModFormValues is a stored definition laid out as the editor's fields.
type ModFormValues struct {
	Name, Kind, Match, Detector, Domains, Action, Target, Roles, Agents string
	Hard                                                                bool
	Channels                                                            []string
	// DryRun is whether the filter being edited is only recording. The editor
	// opens with "Record only" as it stands, so saving a change to a filter on
	// trial does not start enforcing it unasked.
	DryRun bool
}

// ModFormValuesOf lays a stored definition out for editing.
func ModFormValuesOf(d chatfilter.Definition) ModFormValues {
	v := ModFormValues{Name: d.Name, Kind: ModKindChoice(d), Action: d.Action, Target: d.Target, Roles: strings.Join(d.ExemptRoles, "\n"), Agents: strings.Join(d.ExemptAgents, "\n"), Hard: d.Hard, Channels: d.Channels}
	switch v.Kind {
	case ModKindSensitive:
		if len(d.Match) > 0 {
			v.Detector = d.Match[0]
		}
	case ModKindLinks:
		if len(d.Match) > 1 {
			v.Domains = strings.Join(d.Match[1:], "\n")
		}
	default:
		v.Match = strings.Join(d.Match, "\n")
	}
	return v
}

// ModErrorKey turns what the filter service answered into the copy line that
// says it in plain words. No code, id or internal text reaches the page.
func ModErrorKey(code string) string {
	switch code {
	case "permission_denied", "unauthenticated", "request_denied":
		return "er_perm"
	case "filters_unavailable":
		return "er_unavail"
	case "version_conflict":
		return "er_conflict"
	case "unknown_target":
		return "er_target"
	case "invalid_filter", "invalid_request":
		return "er_invalid"
	}
	return "er_network"
}

// ModOutcome is what a tried message would meet.
type ModOutcome struct {
	// Kind is block, mask, flag, notify or none.
	Kind string
	// Term is what matched, Rule the name of the filter that matched it, Target
	// who a notify filter tells, Masked what readers would see.
	Term, Rule, Target, Masked string
}

// ModOutcomeOf reads the service's answer to a tried message. sample is the text
// that was tried, from which the matched term is cut by the hit's byte span.
func ModOutcomeOf(result chatfilter.Result, sample string) ModOutcome {
	if result.Action == "" || len(result.Hits) == 0 {
		return ModOutcome{Kind: "none"}
	}
	out := ModOutcome{Kind: result.Action, Masked: result.Masked}
	for _, hit := range result.Hits {
		if hit.Action != result.Action {
			continue
		}
		out.Rule, out.Target = hit.RuleName, hit.Target
		s := hit.Span
		if s.Start >= 0 && s.End > s.Start && s.End <= len(sample) && utf8.ValidString(sample[s.Start:s.End]) {
			out.Term = modTruncate(sample[s.Start:s.End], 40)
		}
		break
	}
	return out
}

func modTruncate(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit]) + "…"
}

// ModExcerpt shortens a long masked message to the part around the first
// removed word, so the sentence stays one line of reading.
func ModExcerpt(masked string) string {
	const token = "[removed word]"
	runes := []rune(masked)
	if len(runes) <= 120 {
		return masked
	}
	at := strings.Index(masked, token)
	centre := 0
	if at >= 0 {
		centre = utf8.RuneCountInString(masked[:at])
	}
	start, end := centre-50, centre+len([]rune(token))+50
	prefix, suffix := "…", "…"
	if start <= 0 {
		start, prefix = 0, ""
	}
	if end >= len(runes) {
		end, suffix = len(runes), ""
	}
	return prefix + string(runes[start:end]) + suffix
}
