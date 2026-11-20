package application

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// ChatSearchGatePort is what Chat search needs of the channel gates (issue
// 3816): which gates the person may know of, the questions of one gate as
// that person may see them, and the person's own answers. Each read decides
// access itself; search adds no rule of its own.
type ChatSearchGatePort interface {
	GateSummaries(ctx context.Context, tenant, person string) ([]chatstore.GateSummary, error)
	SearchGateQuestions(ctx context.Context, a chatgate.Actor, scope chatgate.Scope, query string) ([]chatgate.Field, error)
	SearchOwnAnswers(ctx context.Context, r chatgate.ReadRequest, query string) (map[string]json.RawMessage, error)
}

// chatSearchGates joins the gate repository's list of gates with the gate
// service's reads.
type chatSearchGates struct {
	Summaries ChatgateSummaries
	Service   *chatgate.Service
}

func (g chatSearchGates) GateSummaries(ctx context.Context, tenant, person string) ([]chatstore.GateSummary, error) {
	return g.Summaries.GateSummaries(ctx, tenant, person)
}

func (g chatSearchGates) SearchGateQuestions(ctx context.Context, a chatgate.Actor, scope chatgate.Scope, query string) ([]chatgate.Field, error) {
	return g.Service.SearchGateQuestions(ctx, a, scope, query)
}

func (g chatSearchGates) SearchOwnAnswers(ctx context.Context, r chatgate.ReadRequest, query string) (map[string]json.RawMessage, error) {
	return g.Service.SearchOwnAnswers(ctx, r, query)
}

// RegisterChatSearchGates adds the gate question and own-answer sources.
//
// A question is found by the people the gate itself shows it to (members and
// applicants), through the same discovery check the gate form uses. An answer is
// found only by the person who wrote it: the service refuses any other reader,
// and a row is built only from the caller's own submission, so no reviewer or
// member finds somebody else's answer by searching.
func issue3816Sources(port ChatSearchGatePort) (chatsearch.SourceFuncs, chatsearch.SourceFuncs) {
	questions := func(ctx context.Context, q chatsearch.Request) ([]chatsearch.Row, error) {
		rows := []chatsearch.Row{}
		if q.Actor.TenantID != q.Actor.HomeTenantID {
			return rows, nil
		}
		summaries, err := issue3816Summaries(ctx, port, q)
		if err != nil {
			return nil, err
		}
		actor := chatgate.Actor{Tenant: q.Actor.TenantID, Person: q.Actor.PersonID}
		for _, summary := range summaries {
			fields, err := port.SearchGateQuestions(ctx, actor, chatgate.Scope{Tenant: q.Actor.TenantID, Conversation: summary.Conversation}, "")
			if errors.Is(err, chatgate.ErrDenied) || errors.Is(err, chatgate.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, field := range fields {
				row := chatsearch.Row{Kind: chatsearch.GateQuestion, ID: summary.Conversation + ":" + field.ID, TenantID: q.Actor.TenantID, Text: strings.TrimSpace(field.Label + "\n" + field.Help), Target: chatsearch.Target{ConversationID: summary.Conversation, ItemID: field.ID}}
				if chatsearch.Match(row, q) {
					rows = append(rows, row)
				}
			}
		}
		return chatsearch.WindowRows(rows, q), nil
	}
	collect := func(ctx context.Context, q chatsearch.Request) ([]chatsearch.Row, error) {
		rows := []chatsearch.Row{}
		if q.Actor.TenantID != q.Actor.HomeTenantID {
			return rows, nil
		}
		summaries, err := issue3816Summaries(ctx, port, q)
		if err != nil {
			return nil, err
		}
		actor := chatgate.Actor{Tenant: q.Actor.TenantID, Person: q.Actor.PersonID}
		for _, summary := range summaries {
			// Only a member or somebody with a request of their own has answers.
			if !summary.Member && summary.Request == "" {
				continue
			}
			scope := chatgate.Scope{Tenant: q.Actor.TenantID, Conversation: summary.Conversation}
			values, err := port.SearchOwnAnswers(ctx, chatgate.ReadRequest{Actor: actor, Scope: scope, Person: q.Actor.PersonID, Purpose: "Search my answers"}, "")
			if errors.Is(err, chatgate.ErrDenied) || errors.Is(err, chatgate.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			labels := map[string]string{}
			if fields, err := port.SearchGateQuestions(ctx, actor, scope, ""); err == nil {
				for _, field := range fields {
					labels[field.ID] = field.Label
				}
			}
			for id, value := range values {
				text := issue3816AnswerText(value)
				if text == "" {
					continue
				}
				row := chatsearch.Row{Kind: chatsearch.GateAnswer, ID: summary.Conversation + ":" + id, TenantID: q.Actor.TenantID, Text: text, AuthorID: q.Actor.PersonID, OwnerID: q.Actor.PersonID, Private: true, Target: chatsearch.Target{ConversationID: summary.Conversation, ItemID: id}}
				if !chatsearch.Match(row, q) {
					continue
				}
				// The question is shown with the answer so the row reads on its own.
				if label := labels[id]; label != "" {
					row.Text = label + ": " + text
				}
				rows = append(rows, row)
			}
		}
		return chatsearch.WindowRows(rows, q), nil
	}
	answers := func(ctx context.Context, q chatsearch.Request) ([]chatsearch.Row, error) {
		// Own answers are matched on the answer itself: the question's wording is
		// found through the question's own row.
		if strings.TrimSpace(strings.ReplaceAll(q.Query, "*", "")) == "" {
			return []chatsearch.Row{}, nil
		}
		return collect(ctx, q)
	}
	open := func(search func(context.Context, chatsearch.Request) ([]chatsearch.Row, error)) func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) {
		return func(ctx context.Context, a chatsearch.Actor, row chatsearch.Row) (bool, error) {
			rows, err := search(ctx, chatsearch.Request{Actor: a, Query: "*", Filters: chatsearch.Filters{Conversation: row.Target.ConversationID}, At: time.Now().UTC(), OpenIDs: []string{row.ID}})
			for _, current := range rows {
				if current.ID == row.ID && current.Text == row.Text && current.Target == row.Target {
					return true, err
				}
			}
			return false, err
		}
	}
	// An answer is opened again only by its owner, from the owner's own answers.
	return chatsearch.SourceFuncs{SearchRows: questions, OpenRow: open(questions)}, chatsearch.SourceFuncs{SearchRows: answers, OpenRow: func(ctx context.Context, a chatsearch.Actor, row chatsearch.Row) (bool, error) {
		if row.OwnerID != a.PersonID {
			return false, nil
		}
		return open(collect)(ctx, a, row)
	}}
}

func RegisterChatSearchGates(registry *chatsearch.Registry, port ChatSearchGatePort) error {
	if registry == nil || isNilPersonaOutputPort(port) {
		return chatsearch.ErrInvalid
	}
	questions, answers := issue3816Sources(port)
	if err := registry.RegisterSource(chatsearch.GateQuestion, questions); err != nil {
		return err
	}
	return registry.RegisterSource(chatsearch.GateAnswer, answers)
}

// issue3816Summaries lists the gates the person may know of, once per search,
// narrowed to the one conversation a filter names.
func issue3816Summaries(ctx context.Context, port ChatSearchGatePort, q chatsearch.Request) ([]chatstore.GateSummary, error) {
	value, err := chatsearch.Memo(ctx, "gates\x00"+q.Actor.TenantID+"\x00"+q.Actor.PersonID, func() (any, error) {
		return port.GateSummaries(ctx, q.Actor.TenantID, q.Actor.PersonID)
	})
	if err != nil {
		return nil, err
	}
	all, _ := value.([]chatstore.GateSummary)
	if q.Filters.Conversation == "" {
		return all, nil
	}
	out := []chatstore.GateSummary{}
	for _, summary := range all {
		if summary.Conversation == q.Filters.Conversation {
			out = append(out, summary)
		}
	}
	return out, nil
}

// issue3816AnswerText is an answer as words: text as written, a list joined, a
// yes or no, a date or number as it was typed.
func issue3816AnswerText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return strings.Join(list, ", ")
	}
	var flag bool
	if json.Unmarshal(raw, &flag) == nil {
		return strconv.FormatBool(flag)
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}
