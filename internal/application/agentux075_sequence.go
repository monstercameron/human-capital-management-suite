package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// AGENTUX-075: what a person sees after asking an agent a question. The agent
// reacts to the question at once, shows that it is working, then answers or says
// plainly why it could not. This file holds the server side of the first step
// and the failure that used to be silence.

// errPersonaModelNotConfigured is the failure of a server that has no model for
// its agents. It is a stated outcome of the question, never a missing run.
var errPersonaModelNotConfigured = errors.New("application: no model is configured on this server")

// personaQuestionReactor puts the agent's reaction on the question that asked
// it. The reaction is cosmetic: a failure to place it is logged and never stops
// the answer.
type personaQuestionReactor interface {
	ReactToQuestion(ctx context.Context, post chatcore.Post, asker chatcore.Principal, agentSubject string)
}

// personaReactionSwitch lets an agent's owner turn its reaction off. A reactor
// without one reacts for every agent.
type personaReactionSwitch interface {
	AgentReactionsEnabled(ctx context.Context, tenant, conversation, agentSubject string) bool
}

// personaOutcomeReactor replaces the agent's first reaction with the one that
// fits how the question ended (AGENTUX-052). first is the emoji it put when the
// question was asked.
type personaOutcomeReactor interface {
	ReactToOutcome(ctx context.Context, post chatcore.Post, asker chatcore.Principal, agentSubject, outcomeEmoji string)
}

// personaOutcomeEmojiChooser lets a model pick the outcome reaction from the
// closed set. Its choice is used only when it is in that set and the outcome was
// an answer; a failure always takes the table's emoji. Without one the table
// decides, so a server with no chooser makes no model call for a reaction.
type personaOutcomeEmojiChooser interface {
	ChooseOutcomeEmoji(ctx context.Context, question string, allowed []string) (string, bool)
}

// agentUX075OutcomeEmoji is the table: the reaction that fits how a run ended,
// from the failure's code and never from what was asked. err is nil for a run
// that answered. It returns "" when the first reaction should stay (an answer
// the person stopped is neither answered nor failed).
func agentUX075OutcomeEmoji(err error) string {
	if err == nil {
		return chatcore.AgentOutcomeAnswered()
	}
	code := ""
	var failure *PersonaRunFailure
	if errors.As(err, &failure) && failure != nil && failure.Code != "" {
		code = failure.Code
	} else {
		code, _ = personaPostFailureClassification(err)
	}
	switch chatcore.AgentAnswerFailureFor("en-US", "", code).Class {
	case "nothing", "permission":
		return chatcore.AgentOutcomeCouldNotAnswer()
	case "stopped":
		return ""
	}
	return chatcore.AgentOutcomeFailed()
}

// agentUX075QuestionEmoji chooses the reaction to a question from its words,
// without a model call. The table is small on purpose: one emoji per kind of
// question, "eyes" when nothing else fits.
func agentUX075QuestionEmoji(body string) string {
	text := strings.ToLower(strings.TrimSpace(body))
	switch {
	case text == "":
		return "👀"
	case agentUX075Mentions(text, "thank", "thx", "appreciate", "danke", "merci", "gracias", "شكرا", "شكراً"):
		if agentUX075Mentions(text, "great", "perfect", "awesome", "amazing", "love", "wonderful", "super") {
			return "🙌"
		}
		return "🙏"
	case agentUX075Mentions(text, "not working", "doesn't work", "does not work", "isn't working", "broken", "error", "problem", "issue", "wrong", "complain", "can't", "cannot", "unable", "failed", "fehler", "مشكلة", "خطأ"):
		if agentUX075Mentions(text, "complain", "unacceptable", "angry", "frustrat", "terrible") {
			return "🫡"
		}
		return "🛠️"
	case agentUX075Greeting(text):
		return "👋"
	case agentUX075Mentions(text, "list ", "list of", "show me all", "show all", "which ", "find ", "search", "look up", "lookup", "all the", "every ", "what are the", "suche", "liste", "ابحث", "قائمة"):
		return "🔎"
	}
	return "👀"
}

func agentUX075Mentions(text string, words ...string) bool {
	for _, word := range words {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

// agentUX075Greeting recognizes a message that is only a greeting: a long
// message that happens to begin with "hi" is a question.
func agentUX075Greeting(text string) bool {
	if utf8.RuneCountInString(text) > 40 {
		return false
	}
	for _, greeting := range []string{"hi", "hello", "hey", "good morning", "good afternoon", "good evening", "hallo", "guten tag", "hola", "bonjour", "مرحبا", "السلام عليكم", "أهلا"} {
		if text == greeting || strings.HasPrefix(text, greeting+" ") || strings.HasPrefix(text, greeting+",") || strings.HasPrefix(text, greeting+"!") {
			return true
		}
	}
	return false
}

// servedPersonaQuestionReactor stores the reaction through the chat service's
// agent-question port. Without the port nothing is stored.
type servedPersonaQuestionReactor struct {
	chat     chatcore.AgentQuestionReactionCommitter
	switches personaReactionSwitch
	// chooser, when set, may pick the outcome reaction of an answered question.
	chooser personaOutcomeEmojiChooser
}

// servedAgentQuestionReactions finds the chat service that can store an agent's
// reaction to a question: the served one, when the chain beneath it carries the
// reaction through to the core, else the core itself.
func servedAgentQuestionReactions(served *streamingChatService) chatcore.AgentQuestionReactionCommitter {
	if served == nil {
		return nil
	}
	// The routed service beneath the streaming one holds the route lease a write to
	// a routed conversation needs, so the reaction goes through the served service
	// (which forwards to it). Reaching the core service around it, as the first
	// version did, was refused for want of that lease (ErrNoRouteLease) in every
	// conversation of a routed server.
	if _, ok := served.ConversationService.(chatcore.AgentQuestionReactionCommitter); ok {
		return served
	}
	// A server composed without routing has no lease to hold: the core service
	// stores the reaction.
	if committer, ok := served.masker.(chatcore.AgentQuestionReactionCommitter); ok {
		return committer
	}
	return nil
}

func newServedPersonaQuestionReactor(chat chatcore.AgentQuestionReactionCommitter, switches personaReactionSwitch) personaQuestionReactor {
	if isNilPersonaOutputPort(chat) {
		return nil
	}
	return servedPersonaQuestionReactor{chat: chat, switches: switches}
}

func (r servedPersonaQuestionReactor) ReactToQuestion(ctx context.Context, post chatcore.Post, asker chatcore.Principal, agentSubject string) {
	if r.chat == nil || strings.TrimSpace(agentSubject) == "" {
		return
	}
	if r.switches != nil && !r.switches.AgentReactionsEnabled(ctx, post.TenantID, post.ConversationID, agentSubject) {
		return
	}
	_, err := r.chat.CommitAgentQuestionReaction(ctx, chatcore.AgentQuestionReaction{
		TenantID: post.TenantID, ConversationID: post.ConversationID, PostID: post.ID,
		AgentSubjectID: agentSubject, Asker: asker, Emoji: agentUX075QuestionEmoji(post.Body),
	})
	if err != nil {
		slog.WarnContext(ctx, "hcmnext.agent_question_reaction_failed", "post_id", post.ID, "conversation_id", post.ConversationID, "agent", agentSubject, "error", err.Error())
	}
}

// ReactToOutcome swaps the agent's first reaction for the one that fits the
// outcome. The model's choice, when a chooser is set, counts only for an
// answered question and only from the closed set.
func (r servedPersonaQuestionReactor) ReactToOutcome(ctx context.Context, post chatcore.Post, asker chatcore.Principal, agentSubject, outcomeEmoji string) {
	if r.chat == nil || strings.TrimSpace(agentSubject) == "" || outcomeEmoji == "" {
		return
	}
	if r.switches != nil && !r.switches.AgentReactionsEnabled(ctx, post.TenantID, post.ConversationID, agentSubject) {
		return
	}
	if r.chooser != nil && outcomeEmoji == chatcore.AgentOutcomeAnswered() {
		if chosen, ok := r.chooser.ChooseOutcomeEmoji(ctx, post.Body, chatcore.AgentQuestionReactionEmoji()); ok && chatcore.ValidAgentQuestionReactionEmoji(chosen) {
			outcomeEmoji = chosen
		}
	}
	_, err := r.chat.CommitAgentQuestionReaction(ctx, chatcore.AgentQuestionReaction{
		TenantID: post.TenantID, ConversationID: post.ConversationID, PostID: post.ID,
		AgentSubjectID: agentSubject, Asker: asker, Emoji: outcomeEmoji, Replaces: agentUX075QuestionEmoji(post.Body),
	})
	if err != nil {
		slog.WarnContext(ctx, "hcmnext.agent_outcome_reaction_failed", "post_id", post.ID, "conversation_id", post.ConversationID, "agent", agentSubject, "error", err.Error())
	}
}

// reactToOutcome replaces the first reaction once the run is over, for every
// agent the question was put to. err is the run's outcome: nil when it answered.
// The run's context may be spent by then, so the reaction has a short one of its
// own.
func (c *personaChatInvocation) reactToOutcome(ctx context.Context, principal *trust.Principal, post chatcore.Post, references []chatcore.Reference, err error) {
	if c == nil || isNilPersonaOutputPort(c.reactions) || principal == nil {
		return
	}
	outcome, ok := c.reactions.(personaOutcomeReactor)
	emoji := agentUX075OutcomeEmoji(err)
	if !ok || emoji == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	asker := chatcore.Principal{TenantID: principal.Tenant().String(), SubjectID: principal.Subject()}
	seen := map[string]bool{}
	for _, reference := range references {
		if reference.Kind != chatcore.AgentMention || seen[reference.ID] {
			continue
		}
		seen[reference.ID] = true
		outcome.ReactToOutcome(ctx, post, asker, reference.ID, emoji)
	}
}

// reactToQuestion adds the agent's reaction to the question for every agent the
// question was put to, once the references are known to name installed agents.
func (c *personaChatInvocation) reactToQuestion(ctx context.Context, principal *trust.Principal, post chatcore.Post, references []chatcore.Reference) {
	if c == nil || isNilPersonaOutputPort(c.reactions) || principal == nil {
		return
	}
	asker := chatcore.Principal{TenantID: principal.Tenant().String(), SubjectID: principal.Subject()}
	seen := map[string]bool{}
	for _, reference := range references {
		if reference.Kind != chatcore.AgentMention || seen[reference.ID] {
			continue
		}
		seen[reference.ID] = true
		c.reactions.ReactToQuestion(ctx, post, asker, reference.ID)
	}
}

// bindPersonaModelUnavailable installs that hand-off in the served chat service
// when no model runs agents here, so a question to an agent gets its stated
// failure instead of nothing.
func bindPersonaModelUnavailable(served *streamingChatService, personas *personaServeWiring, database composedAgentDatabase, logger personaInvocationLogger) error {
	if served == nil || personas == nil || database.store == nil {
		return errPersonaInvocationServeWiring
	}
	ports, err := composePersonaInvocationServedPorts(served, personas, logger)
	if err != nil {
		return err
	}
	failures, err := newPersonaDurableInvocationFailureSink(database.store, ports.failures)
	if err != nil {
		return err
	}
	wiring, err := newPersonaModelUnavailableInvocation(ports.chat, ports.directory, ports.references, failures, newServedPersonaQuestionReactor(servedAgentQuestionReactions(served), storedReactionSwitchFor(database, ports.references)))
	if err != nil {
		return err
	}
	return served.bindPersonaInvocation(wiring)
}

// newPersonaModelUnavailableInvocation is the chat hand-off of a server that has
// agents installed but no model to run them. A question to an agent there is
// stored as a failed question with a plain cause ("no model is configured"),
// where it used to be committed and never looked at again.
func newPersonaModelUnavailableInvocation(chat personaChatPostWriter, conversations personaDirectConversationReader, references personaReferenceResolver, failures personaInvocationFailureSink, reactions personaQuestionReactor) (*PersonaInvocationServeWiring, error) {
	if isNilPersonaOutputPort(chat) || isNilPersonaOutputPort(references) || isNilPersonaOutputPort(failures) {
		return nil, errPersonaInvocationServeWiring
	}
	invocation := &personaChatInvocation{chat: chat, refs: references, failures: failures, detached: true, quiet: &personaDirectQuiet{}, unavailable: errPersonaModelNotConfigured, reactions: reactions}
	if !isNilPersonaOutputPort(conversations) {
		invocation.conversations = conversations
	}
	return &PersonaInvocationServeWiring{Chat: invocation}, nil
}

// askedAgentReferences are the agents a post put its question to: the typed
// mentions it carries, or the one agent of the person's own direct conversation.
func (c *personaChatInvocation) askedAgentReferences(ctx context.Context, principal *trust.Principal, post chatcore.Post) []chatcore.Reference {
	if hasTypedAgentReference(post.References) {
		return post.References
	}
	if reference, ok := c.directAgentReference(ctx, principal, post); ok {
		return []chatcore.Reference{reference}
	}
	return nil
}

// personaReactionSettingReader reads an agent's owner's choice about reactions.
type personaReactionSettingReader interface {
	PersonaReactionsEnabled(ctx context.Context, tenant, personaID string) (bool, error)
}

// storedPersonaReactionSwitch is the owner's switch, read from the setting kept
// for the persona the agent is. An agent with no stored choice reacts, and a
// read that fails leaves the reaction on: it is cosmetic, and a lost setting must
// not become a silent agent.
type storedPersonaReactionSwitch struct {
	refs     personaReferenceResolver
	settings personaReactionSettingReader
}

func newStoredPersonaReactionSwitch(refs personaReferenceResolver, settings personaReactionSettingReader) personaReactionSwitch {
	if isNilPersonaOutputPort(refs) || isNilPersonaOutputPort(settings) {
		return nil
	}
	return storedPersonaReactionSwitch{refs: refs, settings: settings}
}

func (s storedPersonaReactionSwitch) AgentReactionsEnabled(ctx context.Context, tenant, conversation, agentSubject string) bool {
	mentions, err := s.refs.ResolvePersonaMentions(ctx, tenant, conversation, []chatcore.Reference{{Kind: chatcore.AgentMention, TenantID: tenant, ID: agentSubject, ConversationID: conversation}})
	if err != nil || len(mentions) != 1 || strings.TrimSpace(mentions[0].PersonaID) == "" {
		return true
	}
	enabled, err := s.settings.PersonaReactionsEnabled(ctx, tenant, mentions[0].PersonaID)
	if err != nil {
		slog.WarnContext(ctx, "hcmnext.agent_reaction_setting_unreadable", "error_type", fmt.Sprintf("%T", err))
		return true
	}
	return enabled
}

// storedReactionSwitchFor is the owner's switch read from the agent database, or
// none when that database is not composed (every agent then reacts).
func storedReactionSwitchFor(database composedAgentDatabase, refs personaReferenceResolver) personaReactionSwitch {
	if database.store == nil {
		return nil
	}
	settings, err := agentinvocationstore.NewWithTenantUUID(database.store, pgstore.TenantID)
	if err != nil {
		return nil
	}
	return newStoredPersonaReactionSwitch(refs, settings)
}
