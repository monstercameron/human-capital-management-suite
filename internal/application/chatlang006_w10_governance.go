package application

import (
	"context"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// chatlangMonitoringDays is the period the administration's monitoring covers.
const chatlangMonitoringDays = 30

// chatlangReportLimit is how many reports the administration lists.
const chatlangReportLimit = 50

// ChatlangGlossaryLanguage says, for one target language, how many glossary terms
// it has and who last edited them. The glossary version is the workspace's: every
// change to any language advances it.
type ChatlangGlossaryLanguage struct {
	Language   string `json:"language"`
	Terms      int    `json:"terms"`
	LastEditor string `json:"last_editor,omitempty"`
}

// chatlangAdminExtras adds to the administration view what only a workspace
// administrator reads: the monitoring of each language pair, the readers'
// reports of wrong translations, and the glossary's version and editor per
// language. None of it carries message text.
func (g *ChatlangGovernance) chatlangAdminExtras(ctx context.Context, tenant string, view *ChatlangView) error {
	since := g.now().AddDate(0, 0, -chatlangMonitoringDays)
	pairs, err := g.Store.ChatlangPairStats(ctx, tenant, since)
	if err != nil {
		return err
	}
	view.Pairs = pairs
	reports, err := g.Store.ChatlangReports(ctx, tenant, chatlangReportLimit)
	if err != nil {
		return err
	}
	for i := range reports {
		// Identifiers of the conversation and message stay on the server: a report
		// shows the pair and the reader's suggestion, and an administrator reads the
		// message itself only through the conversation they could already read.
		reports[i].Conversation, reports[i].Message = "", ""
	}
	view.Reports = reports
	editors, err := g.Store.ChatlangGlossaryEditors(ctx, tenant)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, term := range view.Glossary {
		if term.Language != "" {
			counts[term.Language]++
		}
	}
	view.GlossaryVersion = view.Workspace.GlossaryVersion
	for language := range counts {
		view.GlossaryLanguages = append(view.GlossaryLanguages, ChatlangGlossaryLanguage{Language: language, Terms: counts[language], LastEditor: editors[language]})
	}
	sort.Slice(view.GlossaryLanguages, func(i, j int) bool { return view.GlossaryLanguages[i].Language < view.GlossaryLanguages[j].Language })
	return nil
}

// ResolveReport closes a reader's report of a wrong translation as resolved or
// dismissed, with a note. Only a workspace administrator may.
func (g *ChatlangGovernance) ResolveReport(ctx context.Context, id, state, note string) error {
	actor, err := chatlangPersonFrom(ctx)
	if err != nil {
		return err
	}
	if g == nil || g.Store == nil {
		return chatlang.ErrUnavailable
	}
	if !g.authorize(ctx, actor, "") {
		return chat.ErrPermissionDenied
	}
	if err := g.Store.ResolveChatlangReport(ctx, actor.tenant, id, state, note, actor.subject); err != nil {
		if err == chatstore.ErrChatlangReport {
			return chatlang.ErrInvalid
		}
		return err
	}
	return nil
}

// chatlangReportSurface is the optional part of the administration surface that
// resolves reports.
type chatlangReportSurface interface {
	ResolveReport(ctx context.Context, id, state, note string) error
}

var _ chatlangReportSurface = (*ChatlangGovernance)(nil)
