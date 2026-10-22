package application

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
)

// chatlangJobTranslates reports whether a rendering job asks for a translation.
// A job that only rewords is run by the same worker but is not governed by the
// translation switches, pair gate or monthly translation budget.
func chatlangJobTranslates(r chatrender.Rendering) bool {
	if chatrenderKindsInclude(r.Kinds, chatrender.Translate) {
		return true
	}
	// A job whose kinds are not recorded is treated as a translation: the
	// stricter rule is the safe one.
	return len(r.Kinds) == 0
}

func chatrenderKindsInclude(kinds []chatrender.Kind, want chatrender.Kind) bool {
	for _, kind := range kinds {
		if kind == want {
			return true
		}
	}
	return false
}

// chatlangExtraRegistrations are the rendering kinds the worker runs beside
// translation: rewording, when a reword model is composed.
func chatlangExtraRegistrations(in chatlangServeInput) []chatrender.Registration {
	if in.Reworder == nil || in.Chat.store == nil {
		return nil
	}
	producer := &ChattoneRewordProducer{Facts: in.Chat.store, Reworder: in.Reworder}
	return []chatrender.Registration{producer.Registration()}
}

// chatlangReworderOf is the reword service of a composed writing-style service,
// nil when none is composed.
func chatlangReworderOf(service *ChattoneService) *chatrewrite.Reworder {
	if service == nil {
		return nil
	}
	return service.Reworder
}

// recordPair adds one translation attempt to the monitoring of its language
// pair (CHATLANG-006): counts and a duration, never text. A message that
// needed no translation, a policy refusal and a missing engine are not the
// pair's quality and are not counted. A failure to record never fails the
// translation.
func (p *ChatlangProducer) recordPair(ctx context.Context, in, out chatrender.Rendering, err error, took time.Duration) {
	if p == nil || p.Store == nil {
		return
	}
	source, target := chatrender.Language(in.SourceLanguage), chatrender.Language(in.Language)
	if source == "" || source == target || !chatrender.Supported(target) {
		return
	}
	if err != nil && (source == "und" || errors.Is(err, chatlang.ErrNothingToTranslate) || errors.Is(err, chatrender.ErrDenied) || errors.Is(err, chatrender.ErrUnavailable)) {
		return
	}
	at := time.Now()
	if p.Now != nil {
		at = p.Now()
	}
	failed := err != nil
	_ = p.Store.RecordChatlangPairAttempt(ctx, in.Tenant, source, target, failed, !failed && out.Confidence < 0.8, took, at)
}

// EnvChatTranslationGate asks the local server to run the translation quality
// gate once at start-up ("run"): the labelled workplace cases are translated for
// every pair the cases cover, and a pair that passes only the lower mark is
// offered with "may be inaccurate" while one that fails is withheld. It calls the
// engine, so it is never on by default and never run by a test.
const EnvChatTranslationGate = "HCMNEXT_CHAT_TRANSLATION_GATE"

// chatlangGateStartup returns the start-up step that runs the quality gate and
// records its decisions with the governance. Only the languages the gate has
// cases for are judged; every other source language stays as it was (offered).
func chatlangGateStartup(governance *ChatlangGovernance, engine chatlang.Engine) func(context.Context) {
	return func(ctx context.Context) {
		sources := map[string]bool{}
		for _, c := range chatlang.GateCases() {
			sources[c.Source] = true
		}
		var pairs []chatlang.GatePair
		for _, source := range chatlang.SupportedLanguages() {
			for _, target := range chatlang.SupportedLanguages() {
				if sources[source] && source != target {
					pairs = append(pairs, chatlang.GatePair{Source: source, Target: target})
				}
			}
		}
		if results, err := chatlang.RunGate(ctx, engine, chatlang.Glossary{}, pairs); err == nil {
			governance.SetGate(results)
		}
	}
}
