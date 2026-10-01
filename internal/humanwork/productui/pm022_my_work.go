package productui

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type MyWorkTaskKind string

const (
	MyWorkProjectTask MyWorkTaskKind = "PROJECT_TASK"
	MyWorkHumanWork   MyWorkTaskKind = "HUMAN_WORK"
)

type CompletionAuthority string

const (
	CompletionAuthorityProject CompletionAuthority = "PROJECT"
	CompletionAuthorityHuman   CompletionAuthority = "HUMAN_WORK"
)

type CombinedWorkSource struct {
	ID, Title, Summary, Href, Freshness, SafeNextAction string
	Kind                                                MyWorkTaskKind
	CompletionAuthority                                 CompletionAuthority
}

type CombinedWorkItem struct {
	CombinedWorkSource
	ActionLabel string
}

var combinedWorkHrefID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]{0,127}$`)

// CombineMyWork is a read-only merge of already authorized projections. It
// keeps project tasks and HCM obligations visibly distinct and retains the
// owning completion authority for every row. It does not infer completion or
// actions from a status string.
func CombineMyWork(sources []CombinedWorkSource) []CombinedWorkItem {
	out := make([]CombinedWorkItem, 0, len(sources))
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if !combinedWorkSourceValid(source) {
			continue
		}
		key := string(source.Kind) + "\x00" + source.ID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, CombinedWorkItem{CombinedWorkSource: source, ActionLabel: combinedWorkAction(source)})
	}
	return out
}

// HumanWorkSources adapts the existing authorized HCM WorkItem rows into the
// combined read model. It carries only server-granted next actions and keeps
// completion with Human Work; no project action is inferred from status.
func HumanWorkSources(items []WorkItem) []CombinedWorkSource {
	out := make([]CombinedWorkSource, 0, len(items))
	for _, item := range items {
		if item.ID == "" || item.Href == "" || item.Freshness == "" {
			continue
		}
		out = append(out, CombinedWorkSource{
			ID: item.ID, Title: item.Title, Summary: item.Summary, Href: item.Href,
			Freshness: item.Freshness, SafeNextAction: WorkNextAction(item),
			Kind: MyWorkHumanWork, CompletionAuthority: CompletionAuthorityHuman,
		})
	}
	return out
}

func combinedWorkSourceValid(source CombinedWorkSource) bool {
	if source.Kind == MyWorkProjectTask && source.CompletionAuthority == CompletionAuthorityProject {
		return combinedWorkHref(source.Href, "/workspace/app/project")
	}
	if source.Kind == MyWorkHumanWork && source.CompletionAuthority == CompletionAuthorityHuman {
		return combinedWorkHref(source.Href, "/workspace/app/work")
	}
	return false
}

func combinedWorkAction(source CombinedWorkSource) string {
	if source.Kind == MyWorkProjectTask {
		return "OPEN_PROJECT_TASK"
	}
	if source.SafeNextAction != "" {
		return source.SafeNextAction
	}
	return "OPEN_HUMAN_WORK"
}

func combinedWorkHref(href, expectedPath string) bool {
	if href == "" || strings.ContainsAny(href, "\r\n") {
		return false
	}
	u, err := url.Parse(href)
	if err != nil || u.IsAbs() || u.Host != "" || u.Path != expectedPath {
		return false
	}
	selector := u.Query().Get("selected")
	if selector == "" {
		selector = u.Query().Get("task")
	}
	return combinedWorkHrefID.MatchString(strings.TrimSpace(selector))
}

// CombinedMyWorkList is a semantic component-level projection used by the
// My Work browser harness. It keeps each row's kind and action explicit.
func CombinedMyWorkList(items []CombinedWorkItem) ui.Node {
	rows := make([]ui.Node, 0, len(items))
	for _, item := range items {
		label := string(item.Kind) + ": " + item.Title
		rows = append(rows, html.Li(html.Props{Data: map[string]string{"work-kind": string(item.Kind), "completion-authority": string(item.CompletionAuthority)}}, html.A(html.Props{Href: item.Href, Aria: map[string]string{"label": label}}, html.Span(html.Props{Text: item.Title}), html.Small(html.Props{Text: item.ActionLabel}))))
	}
	return html.Ul(html.Props{Role: "list", Aria: map[string]string{"label": "Combined My Work"}}, rows...)
}
