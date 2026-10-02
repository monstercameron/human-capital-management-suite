package chatrewrite

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Heat is how a message reads, from the typed question put to the decision
// port: calm, firm, heated or abusive. Only a heated message is reworded.
type Heat string

const (
	HeatCalm    Heat = "calm"
	HeatFirm    Heat = "firm"
	HeatHeated  Heat = "heated"
	HeatAbusive Heat = "abusive"
)

// HeatQuestion is the typed question the decision port answers.
const HeatQuestion = "How does this message read: calm, firm, heated or abusive? Firm is direct without hostility. Abusive is a threat, a slur or harassment."

// HeatDecision answers HeatQuestion for text the cheap screen could not clear.
// It is never asked about a message the screen finds calm.
type HeatDecision interface {
	Classify(ctx context.Context, id Identity, question, text string) (Heat, error)
}

// Word lists are matched whole-word and case-insensitively, in the languages
// the product ships. They only decide whether to ask further; the model and the
// workspace's own filters decide everything else.
var (
	heatedWords = []string{
		"idiot", "idiots", "stupid", "useless", "pathetic", "garbage", "trash", "incompetent", "clown", "moron", "morons", "ridiculous", "nonsense", "wtf", "damn", "crap", "sucks", "shut up",
		"wasting my time", "waste of time", "your fault", "what the hell", "sick of",
		"dummkopf", "blöd", "nutzlos", "erbärmlich", "lächerlich", "halt die klappe", "inútil", "estúpido", "patético", "ridículo", "cállate", "imbécile", "inutile", "ridicule", "tais-toi", "idiota",
		"غبي", "أحمق", "اخرس",
	}
	abusiveWords = []string{
		"kill you", "kill yourself", "hurt you", "i will find you", "i'll find you", "you will regret", "you'll regret", "i will make you pay", "i'll make you pay", "die in a fire", "hope you die", "go die",
		"ich bringe dich um", "te voy a matar", "je vais te tuer",
	}
)

// ScreenHeat is the cheap first look: no model, no network, no cost. A message
// with none of the marks is calm and is never reworded.
func ScreenHeat(text string) Heat {
	lower := " " + strings.ToLower(text) + " "
	for _, phrase := range abusiveWords {
		if strings.Contains(lower, phrase) {
			return HeatAbusive
		}
	}
	for _, word := range heatedWords {
		if containsWholeWord(lower, word) {
			return HeatHeated
		}
	}
	if shouting(text) || strings.Contains(text, "!!!") || strings.Contains(text, "?!?") || strings.Contains(text, "??") {
		return HeatHeated
	}
	return HeatCalm
}

func containsWholeWord(haystack, needle string) bool {
	for start := 0; ; {
		at := strings.Index(haystack[start:], needle)
		if at < 0 {
			return false
		}
		from, to := start+at, start+at+len(needle)
		before, _ := utf8.DecodeLastRuneInString(haystack[:from])
		after, _ := utf8.DecodeRuneInString(haystack[to:])
		if !unicode.IsLetter(before) && !unicode.IsLetter(after) {
			return true
		}
		start = from + 1
	}
}

// shouting is a run of at least two words in capitals, or most of the letters
// of a message of some length in capitals.
func shouting(text string) bool {
	upper, letters, run, longest := 0, 0, 0, 0
	for _, word := range strings.Fields(text) {
		w, up := 0, 0
		for _, r := range word {
			if unicode.IsLetter(r) {
				w++
				if unicode.IsUpper(r) {
					up++
				}
			}
		}
		letters += w
		upper += up
		if w >= 3 && up == w {
			run++
			if run > longest {
				longest = run
			}
		} else if w > 0 {
			run = 0
		}
	}
	return longest >= 2 || (letters >= 12 && upper*10 >= letters*7)
}

// RewordInstruction is the instruction the shared rewrite call carries for
// "Reword": tone only, in the writer's language.
const RewordInstruction = "Reword this message so it reads neutral or positive: remove insults, sarcasm, shouting and blame, and keep the writer's point, in the writer's language. A refusal, a deadline and a request stay exactly as firm as the writer made them. Recent messages give register only."

// RewordOutcome says what a reader should be given.
type RewordOutcome string

const (
	// RewordNotNeeded: the message is calm or firm; it is delivered as written.
	RewordNotNeeded RewordOutcome = "not-needed"
	// RewordDone: Text is the reworded rendering, all checks passed.
	RewordDone RewordOutcome = "reworded"
	// RewordFallback: rewording was needed but could not be done (the model was
	// unavailable, the budget was spent, or the checks failed). The message is
	// delivered as written with the workspace's ordinary filter applied, and
	// no failed rewrite is shown to anyone.
	RewordFallback RewordOutcome = "fallback"
	// RewordHardFilter: the message is abusive. Rewording is not a way to make
	// it acceptable: the workspace's hard filter outcome applies as it would
	// have without this feature.
	RewordHardFilter RewordOutcome = "hard-filter"
)

// RewordRequest is one message to consider.
type RewordRequest struct {
	Identity Identity
	Text     string
	// Context is the conversation's recent messages, for register only.
	Context []string
}

// RewordResult is the decision and, for RewordDone, the rewording.
type RewordResult struct {
	Outcome RewordOutcome
	Heat    Heat
	Text    string
	// Reason names why a fallback happened: "unavailable", "limit",
	// "preservation" or "policy".
	Reason string
}

// Reworder decides whether a message needs rewording and, if so, makes the one
// checked rewrite. Service carries the model, policy, outbound verifier, meaning
// check and ledger; its style registry is not used, so rewording is independent
// of the writing-style controls.
type Reworder struct {
	Service  *Service
	Decision HeatDecision
}

// Reword never returns a failed rewrite. Only a request that cannot be
// considered at all (no identity, no text, text too long) is an error.
func (r *Reworder) Reword(ctx context.Context, req RewordRequest) (RewordResult, error) {
	if ctx == nil || !req.Identity.Valid() || strings.TrimSpace(req.Text) == "" || len(req.Text) > MaxDraftBytes || !utf8.ValidString(req.Text) {
		return RewordResult{}, ErrInvalid
	}
	if r == nil || r.Service == nil {
		return RewordResult{Outcome: RewordFallback, Reason: "unavailable"}, nil
	}
	heat := ScreenHeat(req.Text)
	if heat == HeatCalm {
		return RewordResult{Outcome: RewordNotNeeded, Heat: HeatCalm}, nil
	}
	if r.Decision != nil && heat != HeatAbusive {
		decided, err := r.Decision.Classify(ctx, req.Identity, HeatQuestion, req.Text)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return RewordResult{}, ctxErr
			}
			return RewordResult{Outcome: RewordFallback, Heat: heat, Reason: "unavailable"}, nil
		}
		switch decided {
		case HeatCalm, HeatFirm, HeatHeated, HeatAbusive:
			heat = decided
		default:
			return RewordResult{Outcome: RewordFallback, Heat: heat, Reason: "unavailable"}, nil
		}
	}
	switch heat {
	case HeatAbusive:
		return RewordResult{Outcome: RewordHardFilter, Heat: heat}, nil
	case HeatCalm, HeatFirm:
		return RewordResult{Outcome: RewordNotNeeded, Heat: heat}, nil
	}
	text, err := r.Service.run(ctx, Request{Identity: req.Identity, Draft: req.Text, Context: req.Context}, RewordInstruction)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			return RewordResult{}, ctxErr
		}
		return RewordResult{Outcome: RewordFallback, Heat: heat, Reason: rewordReason(err)}, nil
	}
	return RewordResult{Outcome: RewordDone, Heat: heat, Text: text}, nil
}

func rewordReason(err error) string {
	switch {
	case errors.Is(err, ErrLimit):
		return "limit"
	case errors.Is(err, ErrPreservation):
		return "preservation"
	case errors.Is(err, ErrPolicy):
		return "policy"
	}
	return "unavailable"
}
