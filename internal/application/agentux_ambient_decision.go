package application

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrAgentUXAmbientDenied = errors.New("application: ambient action denied")
var ErrAgentUXAmbientInvalid = errors.New("application: ambient input invalid")

type AgentUXAmbientMember struct {
	ID, Name, Zone, HomeTenant string
}

type AgentUXAmbientMessage struct {
	Tenant, Conversation, ID, Author, Body, Parent, Zone string
	Revision                                             uint64
	Mentions                                             []string
}

// AgentUXAmbientProposal is data, never authority. Title must be drawn from
// the source message; recipient and delivery scope are independently decided.
type AgentUXAmbientProposal struct {
	Kind, Title, Owner, Date, Clock, Event string
	Explicit                               bool
}

type AgentUXAmbientAudience struct{ Scope, Person, Reason string }

func AgentUXAmbientAudienceFor(m AgentUXAmbientMessage, p AgentUXAmbientProposal, members []AgentUXAmbientMember) AgentUXAmbientAudience {
	private := AgentUXAmbientAudience{"PRIVATE", m.Author, "uncertain"}
	text := strings.ToLower(m.Body)
	if agentUXAmbientContains(text, "remind me", "erinnere mich", "ذكرني", "ذكّرني") {
		private.Reason = "explicit_private"
		return private
	}
	if agentUXAmbientContains(text, "remind us", "remind the channel", "erinnere uns", "ذكرنا", "ذكّرنا") {
		return AgentUXAmbientAudience{"PUBLIC", "", "explicit_channel"}
	}
	if agentUXAmbientContains(text, "i'll ", "i will ", "i need to ", "i must ", "ich werde ", "ich muss ", "سأ", "عليّ ", "علي ") {
		private.Reason = "self_commitment"
		return private
	}
	// A model cannot invent an assignee. Only canonical mentions of current
	// members may select another person, and multiple mentions remain private.
	var addressed []string
	for _, id := range m.Mentions {
		if id == m.Author {
			continue
		}
		for _, member := range members {
			if member.ID == id && (member.HomeTenant == "" || member.HomeTenant == m.Tenant) && !slices.Contains(addressed, id) {
				addressed = append(addressed, id)
			}
		}
	}
	if len(addressed) == 1 {
		return AgentUXAmbientAudience{"PRIVATE", addressed[0], "addressed_member"}
	}
	// A named owner is resolved only against the trusted current member list
	// and only when that name occurs in the source's explicit owner clause.
	var named []string
	for _, member := range members {
		name := strings.ToLower(strings.TrimSpace(member.Name))
		if name == "" || member.HomeTenant != "" && member.HomeTenant != m.Tenant {
			continue
		}
		for _, clause := range []string{"owner ", "owner: ", "zuständig: ", "المسؤول: "} {
			if !strings.Contains(text, clause+name) {
				continue
			}
			remainder := strings.SplitN(text, clause+name, 2)[1]
			if remainder == "" || strings.ContainsAny(remainder[:1], ",.; \n") {
				if !slices.Contains(named, member.ID) {
					named = append(named, member.ID)
				}
			}
		}
	}
	if len(addressed) == 0 && len(named) == 1 {
		return AgentUXAmbientAudience{"PRIVATE", named[0], "addressed_member"}
	}
	if len(named) > 1 {
		return private
	}
	if agentUXAmbientContains(text, "owner ", "owner: ", "zuständig: ", "المسؤول: ") {
		return private
	}
	if len(addressed) > 1 || p.Owner != "" && p.Owner != "GROUP" && p.Owner != "UNOWNED" {
		return private
	}
	if p.Owner == "GROUP" || p.Owner == "UNOWNED" || agentUXAmbientContains(text, "we need ", "can someone ", "timesheets are due", "all-hands", "wir müssen", "kann jemand", "نحتاج", "على الجميع") {
		return AgentUXAmbientAudience{"PUBLIC", "", "channel_task"}
	}
	return private
}

func agentUXAmbientContains(text string, cues ...string) bool {
	for _, cue := range cues {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return false
}

func agentUXAmbientUnsafe(text string) bool {
	return agentUXAmbientContains(strings.ToLower(text), "password", "passwort", "كلمة المرور", "ignore previous", "ignore your", "ignore all", "system:", "reveal secret", "api key", "send credentials", "disregard instructions", "تجاهل التعليمات")
}

// AgentUXAmbientScreen is an allocation-bounded preflight, not a model. It is
// run after metadata authorization and before constructing a model request.
func AgentUXAmbientScreen(body, agent string) bool {
	if len(body) < 8 || len(body) > 4000 || !utf8.ValidString(body) || agentUXAmbientUnsafe(body) {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(body))
	if strings.HasPrefix(text, ">") || strings.HasPrefix(text, "\"") || agentUXAmbientContains(text, "sarcasm", "yeah right", "as if", "said \"", "sagte \"", "قال \"", "i already ", "i sent ", "i finished ", "i did ", "ich habe ", "لقد ") {
		return false
	}
	if agent == "task-catcher" {
		if agentUXAmbientContains(text, "can you explain", "could you explain", "kannst du erklären", "هل يمكنك شرح") {
			return false
		}
		return agentUXAmbientContains(text, "i'll ", "i will ", "i need to ", "i must ", "we need to ", "can someone ", "can you ", "could you ", "please review", "@task catcher add:", "ich werde ", "ich muss ", "wir müssen ", "kann jemand ", "kannst du ", "سأ", "نحتاج إلى", "هل يمكنك")
	}
	if agent != "reminder" {
		return false
	}
	if agentUXAmbientContains(text, "remind me", "remind us", "remind the channel", "erinnere mich", "erinnere uns", "ذكرني", "ذكّرني", "ذكرنا", "ذكّرنا") {
		return true
	}
	return agentUXAmbientContains(text, " due ", "deadline", "all-hands moved", "meeting at", "call the ", "fällig", "frist", "treffen um", "موعد", "بحلول") &&
		agentUXAmbientContains(text, "monday", "tuesday", "wednesday", "thursday", "friday", "tomorrow", "today", "montag", "dienstag", "mittwoch", "donnerstag", "freitag", "morgen", "غد", "الجمعة", "الخميس", "202", " at ", " um ", " الساعة")
}

type AgentUXAmbientTime struct {
	At   time.Time
	Fire []time.Time
	Ask  string
	Lead string
}

// AgentUXAmbientUnderstandTime interprets civil time in the author's zone.
// Gaps and folds ask for clarification; no conversion silently picks a side.
func AgentUXAmbientUnderstandTime(p AgentUXAmbientProposal, zone string, now time.Time) AgentUXAmbientTime {
	result := AgentUXAmbientTime{Lead: "deadline_two_hours_and_morning"}
	if strings.TrimSpace(zone) == "" || zone == "Local" {
		result.Ask = "choose_zone"
		return result
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		result.Ask = "choose_zone"
		return result
	}
	local := now.In(loc)
	date := strings.ToLower(strings.TrimSpace(p.Date))
	day := local
	switch date {
	case "today", "heute", "اليوم":
	case "tomorrow", "morgen", "غدا", "غداً":
		day = local.AddDate(0, 0, 1)
	default:
		weekdays := map[string]time.Weekday{"monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday, "thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday, "sunday": time.Sunday, "montag": time.Monday, "dienstag": time.Tuesday, "mittwoch": time.Wednesday, "donnerstag": time.Thursday, "freitag": time.Friday, "الجمعة": time.Friday, "الخميس": time.Thursday}
		if weekday, ok := weekdays[date]; ok {
			delta := (int(weekday) - int(local.Weekday()) + 7) % 7
			day = local.AddDate(0, 0, delta)
		} else {
			day, err = time.ParseInLocation("2006-01-02", date, loc)
			if err != nil {
				result.Ask = "choose_date"
				return result
			}
		}
	}
	clock, err := time.Parse("15:04", p.Clock)
	if err != nil {
		result.Ask = "choose_time"
		return result
	}
	candidate := time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, loc)
	matches := agentUXAmbientCivilMatches(candidate, day, clock, loc)
	if len(matches) != 1 {
		result.Ask = "ambiguous_time"
		return result
	}
	result.At = matches[0].UTC()
	if !result.At.After(now) {
		result.Ask = "past_time"
		return result
	}
	if p.Explicit {
		result.Lead = "at_requested_time"
		result.Fire = []time.Time{result.At}
		return result
	}
	if p.Event == "MEETING" {
		result.Lead = "meeting_fifteen_minutes"
		result.Fire = []time.Time{result.At.Add(-15 * time.Minute)}
	} else {
		morning := time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, loc).UTC()
		result.Fire = []time.Time{morning, result.At.Add(-2 * time.Hour)}
	}
	var fire []time.Time
	for _, at := range result.Fire {
		if at.After(now) && at.Before(result.At) && !slices.ContainsFunc(fire, func(t time.Time) bool { return t.Equal(at) }) {
			fire = append(fire, at)
		}
	}
	if len(fire) == 0 {
		result.Ask = "choose_lead_time"
	}
	slices.SortFunc(fire, func(a, b time.Time) int { return a.Compare(b) })
	result.Fire = fire
	return result
}

func agentUXAmbientCivilMatches(candidate, day, clock time.Time, loc *time.Location) []time.Time {
	var matches []time.Time
	for delta := -180; delta <= 180; delta++ {
		at := candidate.Add(time.Duration(delta) * time.Minute).In(loc)
		if at.Year() == day.Year() && at.Month() == day.Month() && at.Day() == day.Day() && at.Hour() == clock.Hour() && at.Minute() == clock.Minute() {
			matches = append(matches, at)
		}
	}
	return matches
}

// AgentUXAmbientFixtureProposal is the deterministic no-provider extractor
// used by preparation and acceptance fixtures. Production supplies the same
// structured output through AgentUXAmbientModel.
func AgentUXAmbientFixtureProposal(m AgentUXAmbientMessage, agent string) AgentUXAmbientProposal {
	if !AgentUXAmbientScreen(m.Body, agent) {
		return AgentUXAmbientProposal{}
	}
	p := AgentUXAmbientProposal{Kind: "TASK", Title: m.Body, Owner: "UNOWNED"}
	text := strings.ToLower(m.Body)
	if agentUXAmbientContains(text, "i'll ", "i will ", "i need to ", "ich werde ", "ich muss ", "سأ") {
		p.Owner = m.Author
	}
	if agentUXAmbientContains(text, "can you ", "could you ", "kannst du ", "هل يمكنك") {
		p.Owner = "ADDRESSED"
	}
	if agent == "reminder" {
		p.Kind = "REMINDER"
	}
	p.Explicit = agentUXAmbientContains(text, "remind me", "remind us", "remind the channel", "erinnere mich", "erinnere uns", "ذكرني", "ذكّرني", "ذكرنا", "ذكّرنا")
	if agentUXAmbientContains(text, "meeting", "all-hands", "treffen", "اجتماع") {
		p.Event = "MEETING"
	}
	for _, date := range []string{"tomorrow", "today", "monday", "tuesday", "wednesday", "thursday", "friday", "morgen", "heute", "montag", "dienstag", "mittwoch", "donnerstag", "freitag", "غداً", "غدا", "الخميس", "الجمعة"} {
		if strings.Contains(text, date) {
			p.Date = date
			break
		}
	}
	iso := regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`).FindString(text)
	if iso != "" {
		p.Date = iso
	}
	if p.Date == "" {
		if due := strings.Index(text, "due "); due >= 0 {
			p.Date = strings.TrimSpace(text[due+4:])
		}
	}
	if agent == "task-catcher" {
		title := m.Body
		for _, cue := range []string{"i'll ", "i will ", "i need to ", "i must ", "we need to ", "can someone ", "@task catcher add: "} {
			if strings.HasPrefix(strings.ToLower(title), cue) {
				title = title[len(cue):]
				break
			}
		}
		if index := strings.Index(strings.ToLower(title), " by "); index > 0 {
			title = title[:index]
		}
		p.Title = strings.TrimSpace(strings.TrimSuffix(title, "?"))
	}
	clock := regexp.MustCompile(`\b(\d{1,2})(?::(\d{2}))?\s*(am|pm)\b`).FindStringSubmatch(text)
	if len(clock) > 0 {
		parsed, err := time.Parse("3:04pm", clock[1]+":"+agentUXAmbientMinute(clock[2])+clock[3])
		if err == nil {
			p.Clock = parsed.Format("15:04")
		}
	} else {
		p.Clock = regexp.MustCompile(`\b\d{2}:\d{2}\b`).FindString(text)
	}
	return p
}

func agentUXAmbientMinute(value string) string {
	if value == "" {
		return "00"
	}
	return value
}
