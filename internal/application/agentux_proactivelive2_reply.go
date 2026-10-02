package application

import (
	"context"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var ErrAgentAnnouncementChannelLocked = errors.New("announcement channel is locked")

type announcementRepairKey struct{}
type announcementReplyRefusal struct{ rule string }

func (e announcementReplyRefusal) Error() string {
	return "The agent's corrected reply still broke this rule: " + e.rule + ". Preview again to try a new reply."
}
func (e announcementReplyRefusal) AnnouncementReason() string { return e.Error() }
func (e announcementReplyRefusal) Unwrap() error              { return ErrPersonaRunOutputRejected }

// This deliberately checks explicit English month/day and ISO dates only.
// Relative, ambiguous and other-language dates remain evaluation concerns.
func announcementReplyRule(text, today, instruction string) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if normalized == "" {
		return "the announcement must contain text"
	}
	if len(normalized) > 16*1024 {
		return "the announcement must be at most 16 KB"
	}
	if personaReplyLinkPattern.MatchString(personaQualityCitationMarker.ReplaceAllString(normalized, "")) {
		return "use plain text without web addresses, square brackets or Source lines; the product adds sources"
	}
	for _, char := range normalized {
		if unicode.IsControl(char) && char != '\n' && char != '\t' {
			return "use text without control characters"
		}
	}
	lower := strings.ToLower(instruction)
	if !strings.Contains(lower, "upcoming") && !strings.Contains(lower, "coming up") && !strings.Contains(lower, "rest of") && !strings.Contains(lower, "remaining") && !strings.Contains(lower, "next") {
		return ""
	}
	day, err := time.Parse(time.DateOnly, today)
	if err != nil {
		return ""
	}
	for _, match := range regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`).FindAllString(normalized, -1) {
		date, err := time.Parse(time.DateOnly, match)
		if err == nil && date.Before(day) {
			return "upcoming items must have dates on or after " + today + "; leave past dates out"
		}
	}
	pattern := regexp.MustCompile(`(?i)\b(Jan(?:uary)?|Feb(?:ruary)?|Mar(?:ch)?|Apr(?:il)?|May|Jun(?:e)?|Jul(?:y)?|Aug(?:ust)?|Sep(?:tember)?|Oct(?:ober)?|Nov(?:ember)?|Dec(?:ember)?)\.?\s+(\d{1,2})(?:st|nd|rd|th)?(?:,?\s+(20\d{2}))?\b`)
	months := "jan feb mar apr may jun jul aug sep oct nov dec"
	for _, match := range pattern.FindAllStringSubmatch(normalized, -1) {
		month := time.Month(strings.Index(months, strings.ToLower(match[1][:3]))/4 + 1)
		dateDay, _ := strconv.Atoi(match[2])
		year := day.Year()
		if match[3] != "" {
			year, _ = strconv.Atoi(match[3])
		}
		date := time.Date(year, month, dateDay, 0, 0, 0, 0, time.UTC)
		if date.Day() == dateDay && date.Before(day) {
			return "upcoming items must have dates on or after " + today + "; leave past dates out"
		}
	}
	return ""
}

func announcementFailureReason(err error) string {
	var refusal announcementReplyRefusal
	switch {
	case errors.As(err, &refusal):
		return refusal.Error()
	case errors.Is(err, ErrAgentAnnouncementPreviewChanged):
		return announcementPreviewChangedReason
	case errors.Is(err, ErrAgentAnnouncementChannelLocked), errors.Is(err, chatrouting.ErrNotWritable), errors.Is(err, chatrouting.ErrStaleEpoch), errors.Is(err, chat.ErrPermissionDenied):
		return "The channel is locked or your posting access changed. Ask a channel manager to allow posting, then try again."
	case errors.Is(err, ErrAgentAnnouncementDenied):
		return "The agent is paused or its access changed. Resume the agent and check its channel and document access, then preview again."
	case errors.Is(err, ErrPersonaRunOutputRejected):
		return "The documents or posting authority changed. Preview again before posting."
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, ErrPersonaRunModelFailure), errors.Is(err, ErrAgentAnnouncementUnavailable):
		return "The service is busy or unavailable. Try again in a few minutes."
	default:
		return "Posting is temporarily unavailable. Try again in a few minutes; contact a workspace administrator if it continues."
	}
}
