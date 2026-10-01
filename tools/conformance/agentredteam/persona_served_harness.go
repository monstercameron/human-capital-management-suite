package agentredteam

import (
	"context"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdeliver"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
)

// personaServedHarness composes the exported invocation and delivery services
// at the same boundaries used by application/chat composition. Its ports are
// deliberately observable: a test passes only when the real service admits
// or refuses the adversarial request. There is no pretend HTTP or model
// server in this harness.
type personaServedHarness struct {
	invocations *agentinvoke.Service
	delivery    *agentdeliver.Service
	authority   *personaServedAuthority
	runs        *personaServedRuns
	public      *personaServedPublic
	private     *personaServedPrivate
}

type personaServedAuthority struct {
	mu     sync.Mutex
	byUser map[string]agentinvoke.Admission
}

func (a *personaServedAuthority) Resolve(_ context.Context, req agentinvoke.AdmissionRequest) (agentinvoke.Admission, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if admission, ok := a.byUser[req.InvokerID]; ok {
		return admission, nil
	}
	return agentinvoke.Admission{}, nil
}

type personaServedRuns struct {
	mu       sync.Mutex
	requests []agentinvoke.RunRequest
}

func (r *personaServedRuns) Start(_ context.Context, request agentinvoke.RunRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, request)
	return nil
}

type personaServedGrants struct{}

func (personaServedGrants) CreateOnBehalfOfGrant(_ context.Context, req agentinvoke.GrantRequest) (agentinvoke.DelegationGrant, error) {
	return agentinvoke.DelegationGrant{ID: "grant-" + req.InvocationID, UserID: req.UserID, TenantID: req.TenantID, Skills: req.Skills.Clone(), ExpiresAt: req.ExpiresAt}, nil
}

type personaServedPublic struct {
	mu       sync.Mutex
	posts    []agentdeliver.PublicPost
	changeOn bool
}

func (p *personaServedPublic) CommitPublic(_ context.Context, post agentdeliver.PublicPost) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.changeOn {
		p.changeOn = false
		return agentdeliver.ErrAudienceChanged
	}
	p.posts = append(p.posts, post)
	return nil
}

type personaServedPrivate struct {
	mu    sync.Mutex
	posts []agentdeliver.PrivatePost
}

func (p *personaServedPrivate) DeliverPrivate(_ context.Context, post agentdeliver.PrivatePost) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.posts = append(p.posts, post)
	return nil
}

type personaServedAudience struct {
	snapshot agentdeliver.AudienceSnapshot
}

func (a personaServedAudience) Snapshot(context.Context, agentdeliver.Conversation) (agentdeliver.AudienceSnapshot, error) {
	return a.snapshot, nil
}

type personaServedAuthorizer struct{ deny string }

func (a personaServedAuthorizer) Authorize(_ context.Context, req agentdeliver.ReauthorizationRequest) error {
	if req.Audience.SubjectID == a.deny {
		return agentdeliver.ErrInvalidRequest
	}
	return nil
}

func newPersonaServedHarness(tNow time.Time) (*personaServedHarness, error) {
	authority := &personaServedAuthority{byUser: map[string]agentinvoke.Admission{
		"manager":  {Persona: agentinvoke.Persona{ID: "comp", Version: "v1", PinnedSkills: agentinvoke.SkillScopes{"comp.read": {"worker:team-a"}}, Current: true}, Installation: agentinvoke.Installation{ID: "install-1", Current: true, SkillCeiling: agentinvoke.SkillScopes{"comp.read": {"worker:team-a"}}}, Channel: agentinvoke.ChannelPolicy{SkillCeiling: agentinvoke.SkillScopes{"comp.read": {"worker:team-a"}}}, Discoverable: agentinvoke.SkillScopes{"comp.read": {"worker:team-a"}}, HumanMember: true, AudienceMember: true, PersonaInstalled: true},
		"employee": {Persona: agentinvoke.Persona{ID: "comp", Version: "v1", PinnedSkills: agentinvoke.SkillScopes{"comp.read": {"worker:team-a"}}, Current: true}, Installation: agentinvoke.Installation{ID: "install-1", Current: true, SkillCeiling: agentinvoke.SkillScopes{"comp.read": {"worker:team-a"}}}, Channel: agentinvoke.ChannelPolicy{SkillCeiling: agentinvoke.SkillScopes{"comp.read": {"worker:team-a"}}}, Discoverable: agentinvoke.SkillScopes{"comp.read": {"worker:team-a"}}, HumanMember: true, AudienceMember: false, PersonaInstalled: true},
	}}
	runs := &personaServedRuns{}
	invocations, err := agentinvoke.NewService(agentinvoke.Config{Authority: authority, Grants: personaServedGrants{}, Runs: runs, Repository: agentinvoke.NewMemoryRepository(), Now: func() time.Time { return tNow }})
	if err != nil {
		return nil, err
	}
	public, private := &personaServedPublic{}, &personaServedPrivate{}
	delivery := &agentdeliver.Service{Audience: personaServedAudience{snapshot: agentdeliver.AudienceSnapshot{Revision: 7, CurrentMembers: []agentdeliver.AudienceMember{{TenantID: "tenant-a", SubjectID: "manager"}, {TenantID: "tenant-a", SubjectID: "guest", Guest: true}}}}, Authorize: personaServedAuthorizer{deny: "guest"}, Public: public, Private: private}
	return &personaServedHarness{invocations: invocations, delivery: delivery, authority: authority, runs: runs, public: public, private: private}, nil
}
