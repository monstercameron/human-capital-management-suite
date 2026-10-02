package application

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ChatlangEngineInfo says which translation engine the deployment composed,
// and whether it sends text outside the deployment.
type ChatlangEngineInfo struct {
	Name     string `json:"name"`
	Ready    bool   `json:"ready"`
	External bool   `json:"external"`
}

// ChatlangGovernance is what the administrator controls (CHATLANG-006) and the
// one answer to "may this message be translated here": the rendering policy,
// the worker's authority and the administration page all ask it. The original
// message is the only record; nothing here reads or stores message text.
type ChatlangGovernance struct {
	Store *chatstore.Store
	// Admins decides who administers a workspace (channel empty) or a channel:
	// the same rule message filters use.
	Admins chatfilter.Authority
	Now    func() time.Time

	engine atomic.Pointer[ChatlangEngineInfo]
	mu     sync.Mutex
	cache  map[string]chatlangCached
}

type chatlangCached struct {
	at        time.Time
	workspace chatlang.Workspace
	channel   chatlang.Channel
}

const chatlangCacheTTL = 2 * time.Second

func (g *ChatlangGovernance) now() time.Time {
	if g != nil && g.Now != nil {
		return g.Now().UTC()
	}
	return time.Now().UTC()
}

// BindEngine records the composed engine. Until one is bound translation is
// never offered, whatever an administrator has set.
func (g *ChatlangGovernance) BindEngine(info ChatlangEngineInfo) {
	if g != nil {
		g.engine.Store(&info)
	}
}

// Engine returns the bound engine, or the zero value when there is none.
func (g *ChatlangGovernance) Engine() ChatlangEngineInfo {
	if g == nil {
		return ChatlangEngineInfo{}
	}
	if info := g.engine.Load(); info != nil {
		return *info
	}
	return ChatlangEngineInfo{}
}

func (g *ChatlangGovernance) forget(tenant string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for key := range g.cache {
		if len(key) > len(tenant) && key[:len(tenant)+1] == tenant+"\x00" {
			delete(g.cache, key)
		}
	}
}

// settings reads the workspace and channel settings through a short cache: the
// rendering policy asks once per message read, and a setting that changed is
// followed within seconds on every replica and at once on the one that saved it.
func (g *ChatlangGovernance) settings(ctx context.Context, tenant, conversation string) (chatlang.Workspace, chatlang.Channel, error) {
	if g == nil || g.Store == nil {
		return chatlang.Workspace{}, chatlang.Channel{}, chatlang.ErrUnavailable
	}
	key := tenant + "\x00" + conversation
	now := g.now()
	g.mu.Lock()
	if hit, ok := g.cache[key]; ok && now.Sub(hit.at) < chatlangCacheTTL {
		g.mu.Unlock()
		return hit.workspace, hit.channel, nil
	}
	g.mu.Unlock()
	workspace, err := g.Store.ChatlangWorkspace(ctx, tenant)
	if err != nil {
		return workspace, chatlang.Channel{}, err
	}
	channel, err := g.Store.ChatlangChannel(ctx, tenant, conversation)
	if err != nil {
		return workspace, channel, err
	}
	g.mu.Lock()
	if g.cache == nil {
		g.cache = map[string]chatlangCached{}
	}
	g.cache[key] = chatlangCached{at: now, workspace: workspace, channel: channel}
	g.mu.Unlock()
	return workspace, channel, nil
}

// Allows reports whether the rendering policy may offer translation in this
// conversation: an engine is composed, the workspace has translation on, the
// channel has not turned it off, and an external engine is permitted where the
// engine is external. A failure to read settings answers no: translation is
// never offered on a guess.
func (g *ChatlangGovernance) Allows(ctx context.Context, tenant, conversation string) bool {
	engine := g.Engine()
	if !engine.Ready {
		return false
	}
	workspace, channel, err := g.settings(ctx, tenant, conversation)
	if err != nil || chatlang.ChannelOn(workspace, channel) != chatlang.Allowed {
		return false
	}
	return !engine.External || chatlang.ExternalOK(workspace, channel)
}

// Permit is the per-job policy decision for one target language: everything
// except the month's spend, which is read fresh by Budget.
func (g *ChatlangGovernance) Permit(ctx context.Context, tenant, conversation, target string) (chatlang.Reason, error) {
	engine := g.Engine()
	if !engine.Ready {
		return chatlang.ReasonWorkspaceOff, chatlang.ErrUnavailable
	}
	workspace, channel, err := g.settings(ctx, tenant, conversation)
	if err != nil {
		return "", err
	}
	return chatlang.Decide(workspace, channel, target, engine.External, 0), nil
}

// Budget returns what the workspace has spent this month and its limit. It is
// never cached: a job must not run on a stale budget.
func (g *ChatlangGovernance) Budget(ctx context.Context, tenant string) (spent, limit int64, err error) {
	if g == nil || g.Store == nil {
		return 0, 0, chatlang.ErrUnavailable
	}
	workspace, err := g.Store.ChatlangWorkspace(ctx, tenant)
	if err != nil {
		return 0, 0, err
	}
	spent, err = g.Store.ChatlangSpent(ctx, tenant, g.now())
	return spent, workspace.Budget(), err
}

// ChatlangView is what the administration page shows. Spend and budget are
// amounts; there is no message text in it.
type ChatlangView struct {
	Workspace          chatlang.Workspace       `json:"workspace"`
	Channel            *chatlang.Channel        `json:"channel,omitempty"`
	Effective          chatlang.Reason          `json:"effective"`
	Glossary           []chatstore.ChatlangTerm `json:"glossary"`
	Engine             ChatlangEngineInfo       `json:"engine"`
	SpentMicros        int64                    `json:"spent_micros"`
	BudgetMicros       int64                    `json:"budget_micros"`
	Paused             bool                     `json:"paused"`
	Supported          []string                 `json:"supported"`
	CanManageWorkspace bool                     `json:"can_manage_workspace"`
	CanManageChannel   bool                     `json:"can_manage_channel"`
}

type chatlangPerson struct{ tenant, subject string }

func chatlangPersonFrom(ctx context.Context) (chatlangPerson, error) {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman || !time.Now().Before(p.ExpiresAt()) {
		return chatlangPerson{}, chat.ErrPermissionDenied
	}
	return chatlangPerson{p.Tenant().String(), p.Subject()}, nil
}

func (g *ChatlangGovernance) authorize(ctx context.Context, a chatlangPerson, conversation string) bool {
	if g == nil || g.Admins == nil {
		return false
	}
	return g.Admins.AuthorizeFilters(ctx, chatfilter.Actor{Tenant: a.tenant, Subject: a.subject, Channel: conversation}, conversation) == nil
}

// View reads the settings for the caller. Workspace settings and the glossary
// are for workspace administrators; a channel manager sees their channel.
func (g *ChatlangGovernance) View(ctx context.Context, conversation string) (ChatlangView, error) {
	actor, err := chatlangPersonFrom(ctx)
	if err != nil {
		return ChatlangView{}, err
	}
	if g == nil || g.Store == nil {
		return ChatlangView{}, chatlang.ErrUnavailable
	}
	view := ChatlangView{Supported: chatlang.SupportedLanguages(), Engine: g.Engine(), Glossary: []chatstore.ChatlangTerm{}}
	view.CanManageWorkspace = g.authorize(ctx, actor, "")
	view.CanManageChannel = conversation != "" && g.authorize(ctx, actor, conversation)
	if !view.CanManageWorkspace && !view.CanManageChannel {
		return ChatlangView{}, chat.ErrPermissionDenied
	}
	if view.Workspace, err = g.Store.ChatlangWorkspace(ctx, actor.tenant); err != nil {
		return ChatlangView{}, err
	}
	if view.CanManageChannel {
		channel, err := g.Store.ChatlangChannel(ctx, actor.tenant, conversation)
		if err != nil {
			return ChatlangView{}, err
		}
		view.Channel = &channel
		view.Effective = chatlang.ChannelOn(view.Workspace, channel)
		if view.Effective == chatlang.Allowed && view.Engine.External && !chatlang.ExternalOK(view.Workspace, channel) {
			view.Effective = chatlang.ReasonExternalBarred
		}
	}
	if view.CanManageWorkspace {
		if _, view.Glossary, err = g.Store.ChatlangGlossary(ctx, actor.tenant); err != nil {
			return ChatlangView{}, err
		}
		if view.Glossary == nil {
			view.Glossary = []chatstore.ChatlangTerm{}
		}
	}
	view.BudgetMicros = view.Workspace.Budget()
	if view.SpentMicros, err = g.Store.ChatlangSpent(ctx, actor.tenant, g.now()); err != nil {
		return ChatlangView{}, err
	}
	view.Paused = view.Workspace.Enabled && view.SpentMicros >= view.BudgetMicros
	if !view.CanManageWorkspace {
		// A channel manager is told what their channel does, not the workspace's spend.
		view.Workspace, view.SpentMicros, view.BudgetMicros = chatlang.Workspace{Enabled: view.Workspace.Enabled}, 0, 0
	}
	return view, nil
}

// PutWorkspace changes the workspace's settings. Only a workspace administrator may.
func (g *ChatlangGovernance) PutWorkspace(ctx context.Context, w chatlang.Workspace) (chatlang.Workspace, error) {
	actor, err := chatlangPersonFrom(ctx)
	if err != nil {
		return chatlang.Workspace{}, err
	}
	if !g.authorize(ctx, actor, "") {
		return chatlang.Workspace{}, chat.ErrPermissionDenied
	}
	out, err := g.Store.PutChatlangWorkspace(ctx, actor.tenant, actor.subject, w)
	g.forget(actor.tenant)
	return out, err
}

// PutChannel changes one channel's setting: a workspace administrator or that
// channel's manager.
func (g *ChatlangGovernance) PutChannel(ctx context.Context, conversation string, c chatlang.Channel) error {
	actor, err := chatlangPersonFrom(ctx)
	if err != nil {
		return err
	}
	if conversation == "" || !g.authorize(ctx, actor, conversation) {
		return chat.ErrPermissionDenied
	}
	err = g.Store.PutChatlangChannel(ctx, actor.tenant, conversation, actor.subject, c)
	g.forget(actor.tenant)
	return err
}

// AddTerm adds a glossary term; only a workspace administrator may.
func (g *ChatlangGovernance) AddTerm(ctx context.Context, t chatlang.Term) (string, error) {
	actor, err := chatlangPersonFrom(ctx)
	if err != nil {
		return "", err
	}
	if !g.authorize(ctx, actor, "") {
		return "", chat.ErrPermissionDenied
	}
	id, err := g.Store.AddChatlangTerm(ctx, actor.tenant, actor.subject, t)
	g.forget(actor.tenant)
	return id, err
}

// RemoveTerm removes a glossary term; only a workspace administrator may.
func (g *ChatlangGovernance) RemoveTerm(ctx context.Context, id string) error {
	actor, err := chatlangPersonFrom(ctx)
	if err != nil {
		return err
	}
	if !g.authorize(ctx, actor, "") {
		return chat.ErrPermissionDenied
	}
	err = g.Store.RemoveChatlangTerm(ctx, actor.tenant, actor.subject, id)
	g.forget(actor.tenant)
	return err
}

// chatrenderOffersTranslation reports whether policy offers the translated
// kind for this message.
func chatrenderOffersTranslation(policy chatrender.Policy) bool {
	for _, kind := range policy.AllowedKinds {
		if kind == chatrender.Translate {
			return true
		}
	}
	return false
}
