package application

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// ChattoneContentPolicy is the production chatrewrite.Policy: a draft, and
// every rewrite of it, must pass the same workspace content filters a sent
// message passes, for the same writer in the same conversation (so the same
// exemptions apply). The check is read-only: a draft that is never sent leaves
// no filter hit on the record. A refusal is a plain "no" for the writer; a
// filter that cannot answer is an error, never a pass.
type ChattoneContentPolicy struct {
	Filters interface {
		Evaluate(context.Context, chatfilter.Input, bool) (chatfilter.Result, error)
	}
	Identity      chat.FilterIdentity
	Conversations interface {
		GetConversation(context.Context, chat.GetConversationRequest) (chat.Conversation, error)
	}
}

func (p ChattoneContentPolicy) Accept(ctx context.Context, id chatrewrite.Identity, text string) (bool, error) {
	if ctx == nil || isNilPersonaOutputPort(p.Filters) || isNilPersonaOutputPort(p.Identity) || isNilPersonaOutputPort(p.Conversations) || !id.Valid() {
		return false, chatrewrite.ErrUnavailable
	}
	principal := chat.Principal{TenantID: id.Tenant, SubjectID: id.Person}
	room, err := p.Conversations.GetConversation(ctx, chat.GetConversationRequest{Principal: principal, TenantID: id.Tenant, ConversationID: id.Conversation})
	if err != nil {
		return false, err
	}
	input, err := p.Identity.FilterInput(ctx, principal)
	if err != nil {
		return false, err
	}
	input.Tenant, input.Channel, input.Body = room.TenantID, room.ID, text
	input.Direct = room.Kind == chat.Direct || room.Kind == chat.Group
	result, err := p.Filters.Evaluate(ctx, input, false)
	if err != nil {
		return false, err
	}
	switch refusal := result.Refusal(); {
	case refusal == nil:
		return true, nil
	case errors.Is(refusal, chatfilter.ErrBlocked):
		return false, nil
	default:
		return false, refusal
	}
}

// chattoneSensitivePatterns are structural detectors for data that may not
// leave the deployment for a hosted model: government identifiers, payment
// card numbers, credentials and private keys. They are deliberately narrow
// (structure, not topic) so ordinary workplace talk is never refused.
func chattoneSensitiveInspector() (*dlp.Inspector, error) {
	return dlp.NewInspector(
		dlp.Detector{ID: "chattone-ssn", Class: dlp.ClassPII, Severity: dlp.SeverityHigh, Pattern: `\b\d{3}-\d{2}-\d{4}\b`},
		dlp.Detector{ID: "chattone-card", Class: dlp.ClassBank, Severity: dlp.SeverityHigh, Matcher: chattoneCardMatcher},
		dlp.Detector{ID: "chattone-private-key", Class: dlp.ClassSpecialCategory, Severity: dlp.SeverityHigh, Pattern: `-----BEGIN [A-Z ]*PRIVATE KEY-----`},
		dlp.Detector{ID: "chattone-cloud-key", Class: dlp.ClassSpecialCategory, Severity: dlp.SeverityHigh, Pattern: `\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`},
		dlp.Detector{ID: "chattone-api-token", Class: dlp.ClassSpecialCategory, Severity: dlp.SeverityHigh, Pattern: `\b(?:sk|pk|rk|ghp|gho|xox[abp])[-_][A-Za-z0-9_-]{16,}`},
		dlp.Detector{ID: "chattone-password", Class: dlp.ClassSpecialCategory, Severity: dlp.SeverityHigh, Pattern: `(?i)\b(?:password|passwd|passphrase|secret)\s*(?:is|[:=])\s*\S{4,}`},
	)
}

// chattoneCardMatcher reports 13-19 digit runs (single spaces or hyphens
// between groups allowed) that pass the Luhn check.
func chattoneCardMatcher(payload []byte) []dlp.Location {
	var out []dlp.Location
	n := len(payload)
	for i := 0; i < n; i++ {
		if payload[i] < '0' || payload[i] > '9' || (i > 0 && payload[i-1] >= '0' && payload[i-1] <= '9') {
			continue
		}
		digits := make([]byte, 0, 19)
		end := i
		for j := i; j < n && len(digits) < 20; j++ {
			c := payload[j]
			if c >= '0' && c <= '9' {
				digits = append(digits, c)
				end = j + 1
				continue
			}
			if (c == ' ' || c == '-') && j+1 < n && payload[j+1] >= '0' && payload[j+1] <= '9' {
				continue
			}
			break
		}
		if len(digits) >= 13 && len(digits) <= 19 && chattoneLuhn(digits) {
			out = append(out, dlp.Location{Start: i, End: end})
			i = end - 1
		}
	}
	return out
}

func chattoneLuhn(digits []byte) bool {
	sum, double := 0, false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// ChattoneOutboundVerifier is the production chatrewrite.Outbound. It checks
// the exact instruction and delimited data that would leave: shape, size and
// the sensitive-data detectors above. A hit is chatrewrite.ErrPolicy, so the
// writer is told the draft cannot be sent to the style model, not that the
// service is down.
type ChattoneOutboundVerifier struct{ inspector *dlp.Inspector }

const (
	chattoneMaxInstructionBytes = 4 << 10
	chattoneMaxDataBytes        = 64 << 10
)

func NewChattoneOutboundVerifier() (*ChattoneOutboundVerifier, error) {
	inspector, err := chattoneSensitiveInspector()
	if err != nil {
		return nil, err
	}
	return &ChattoneOutboundVerifier{inspector: inspector}, nil
}

func (v *ChattoneOutboundVerifier) Verify(ctx context.Context, p chatrewrite.Prompt) error {
	if v == nil || v.inspector == nil || ctx == nil {
		return chatrewrite.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !p.Identity.Valid() || p.TaskProfile != chatrewrite.TaskProfileID || strings.TrimSpace(p.Instruction) == "" ||
		len(p.Instruction) > chattoneMaxInstructionBytes || len(p.Data) > chattoneMaxDataBytes ||
		!utf8.ValidString(p.Instruction) || !utf8.ValidString(p.Data) ||
		!strings.HasPrefix(p.Data, "<untrusted_data>") || !strings.HasSuffix(p.Data, "</untrusted_data>") {
		return chatrewrite.ErrInvalid
	}
	for _, text := range []string{p.Instruction, p.Data} {
		found, err := v.inspector.Inspect([]byte(text))
		if err != nil {
			return chatrewrite.ErrUnavailable
		}
		if len(found.Findings) > 0 {
			return chatrewrite.ErrPolicy
		}
	}
	return nil
}

// ChattoneMeaningGuard is the production chatrewrite.Meaning. It is a
// deterministic, conservative guard, not a model: it answers "preserved" only
// when every check below holds, with confidence 1, and otherwise answers "not
// preserved" with confidence 0 so the service retries once and then refuses.
//
//   - negations keep their count (a "no" stays a "no", a "yes" is not made one);
//   - questions stay questions;
//   - apologies, promises and agreements the writer did not make are not added
//     (their count in the rewrite may not exceed the draft's);
//   - the rewrite keeps a share of the draft's content words, so it answers the
//     same message and not another one.
//
// Facts, names, numbers, links, mentions and code are already guaranteed by the
// service's placeholder restore. A model-backed question can be registered in
// its place without changing the service.
type ChattoneMeaningGuard struct{}

// Local reports that this check runs inside the process: nothing it is given
// leaves, so the service does not put it through the outbound verifier.
func (ChattoneMeaningGuard) Local() bool { return true }

func (ChattoneMeaningGuard) Check(ctx context.Context, q chatrewrite.MeaningQuestion) (chatrewrite.MeaningAnswer, error) {
	if ctx == nil {
		return chatrewrite.MeaningAnswer{}, chatrewrite.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return chatrewrite.MeaningAnswer{}, err
	}
	if q.Question != chatrewrite.PreservationQuestion || strings.TrimSpace(q.Original) == "" || strings.TrimSpace(q.Rewrite) == "" {
		return chatrewrite.MeaningAnswer{}, chatrewrite.ErrInvalid
	}
	if chattoneMeaningPreserved(q.Original, q.Rewrite) {
		return chatrewrite.MeaningAnswer{Preserved: true, Confidence: 1}, nil
	}
	return chatrewrite.MeaningAnswer{Preserved: false, Confidence: 0}, nil
}

func chattoneWords(s string) []string {
	s = strings.ReplaceAll(strings.ToLower(s), "’", "'")
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\'' })
}

// chattoneNegations are whole-word negations in the languages the product ships
// copy for. Contractions are matched by their "n't" suffix separately.
var chattoneNegations = map[string]bool{
	"no": true, "not": true, "never": true, "none": true, "nobody": true, "nothing": true, "cannot": true, "neither": true, "nor": true, "without": true,
	"nein": true, "nicht": true, "nie": true, "niemals": true, "kein": true, "keine": true, "keinen": true, "keiner": true, "nichts": true, "ohne": true,
	"non": true, "pas": true, "jamais": true, "rien": true, "sin": true, "nunca": true, "nada": true, "ningún": true, "ninguno": true,
}

// chattoneAdditions are words whose appearance makes a commitment, apology or
// agreement. The rewrite may carry no more of them than the draft. Courtesy
// (please, thanks) is tone, not content, and is not counted.
var chattoneAdditions = map[string]bool{
	"sorry": true, "apologize": true, "apologise": true, "apologies": true, "apology": true, "pardon": true,
	"promise": true, "promised": true, "guarantee": true, "guaranteed": true, "commit": true, "committed": true, "will": true, "shall": true,
	"agree": true, "agreed": true, "agreement": true,
	"entschuldigung": true, "entschuldigen": true, "versprochen": true, "versprechen": true, "verspreche": true,
	"désolé": true, "disculpa": true, "perdón": true, "prometo": true,
}

func chattoneNegationCount(words []string) int {
	n := 0
	for _, w := range words {
		if chattoneNegations[w] || strings.HasSuffix(w, "n't") {
			n++
		}
	}
	return n
}

func chattoneAdditionCount(words []string) int {
	n := 0
	for _, w := range words {
		if i := strings.IndexByte(w, '\''); i >= 0 {
			// "I'll" and "we'll" make a promise; "don't" is handled as a negation.
			if strings.HasSuffix(w, "'ll") {
				n++
			}
			continue
		}
		if chattoneAdditions[w] {
			n++
		}
	}
	return n
}

// chattoneContentWords are the words that carry the topic: at least four
// letters, not a negation or courtesy word.
func chattoneContentWords(words []string) map[string]bool {
	out := map[string]bool{}
	for _, w := range words {
		if utf8.RuneCountInString(w) < 4 || chattoneNegations[w] || chattoneAdditions[w] || strings.HasSuffix(w, "n't") || strings.Contains(w, "'") {
			continue
		}
		out[chattoneStem(w)] = true
	}
	return out
}

func chattoneStem(w string) string {
	for _, suffix := range []string{"ing", "ed", "es", "s"} {
		if len(w) > len(suffix)+3 && strings.HasSuffix(w, suffix) {
			return strings.TrimSuffix(w, suffix)
		}
	}
	return w
}

func chattoneMeaningPreserved(original, rewrite string) bool {
	ow, rw := chattoneWords(original), chattoneWords(rewrite)
	if chattoneNegationCount(ow) != chattoneNegationCount(rw) {
		return false
	}
	if (strings.Contains(original, "?") || strings.Contains(original, "？")) != (strings.Contains(rewrite, "?") || strings.Contains(rewrite, "？")) {
		return false
	}
	if chattoneAdditionCount(rw) > chattoneAdditionCount(ow) {
		return false
	}
	oc, rc := chattoneContentWords(ow), chattoneContentWords(rw)
	if len(oc) >= 3 {
		shared := 0
		for w := range oc {
			if rc[w] {
				shared++
			}
		}
		// A tone rewrite replaces insults and filler; it keeps the subject.
		if shared*100 < len(oc)*30 {
			return false
		}
	}
	return true
}

var (
	_ chatrewrite.Policy   = ChattoneContentPolicy{}
	_ chatrewrite.Outbound = (*ChattoneOutboundVerifier)(nil)
	_ chatrewrite.Meaning  = ChattoneMeaningGuard{}
)
