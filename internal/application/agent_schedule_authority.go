package application

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
)

// AgentScheduleManagementScope is independently issued trust data. It grants
// exact installation, pinned target and source choices to a human manager;
// neither a UI draft nor an agent run can create this authority.
type AgentScheduleManagementScope struct {
	SchemaVersion   uint32                   `json:"schema_version"`
	TenantID        string                   `json:"tenant_id"`
	ScheduleID      string                   `json:"schedule_id"`
	Actions         []string                 `json:"actions"`
	Schedule        scheduled.Schedule       `json:"schedule"`
	CronExpressions []string                 `json:"cron_expressions"`
	MisfirePolicies []schedule.MisfirePolicy `json:"misfire_policies"`
	OverlapPolicies []schedule.OverlapPolicy `json:"overlap_policies"`
	DSTPolicies     []string                 `json:"dst_policies"`
}

func (s *AgentScheduleService) managementScopes(ctx context.Context, actor scheduled.Actor) ([]AgentScheduleManagementScope, error) {
	tenantID := s.cfg.TenantUUID(values.TenantId(actor.TenantID))
	tx, err := s.cfg.CoreDB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	now := s.cfg.Now().UTC()
	rows, err := tx.Query(ctx, `SELECT b.scope,p.revocation_epoch,a.version,a.content_digest FROM principal p JOIN authority_binding b ON b.tenant_id=p.tenant_id AND b.principal_id=p.principal_id JOIN authority_source a ON a.tenant_id=b.tenant_id AND a.authority_source_id=b.authority_source_id WHERE p.tenant_id=$1 AND p.subject=$2 AND p.kind='USER' AND p.lifecycle='ACTIVE' AND (p.expires_at IS NULL OR p.expires_at>$3) AND b.valid_from<=$3 AND (b.valid_to IS NULL OR b.valid_to>$3) AND a.kind='POLICY_BUNDLE' AND a.valid_interval @> $3::timestamptz`, tenantID, actor.UserID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var scopes []AgentScheduleManagementScope
	for rows.Next() {
		var raw []byte
		var epoch, version int64
		var digest string
		if err := rows.Scan(&raw, &epoch, &version, &digest); err != nil {
			return nil, err
		}
		var scope AgentScheduleManagementScope
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&scope) != nil || decoder.Decode(new(any)) != io.EOF || scope.SchemaVersion != 1 || scope.TenantID != actor.TenantID || scope.ScheduleID == "" || epoch < 1 || version < 1 || !personaRunAuthorityDigest("sha256:"+digest) || scheduled.ValidateSchedule(scope.Schedule) != nil || scope.Schedule.Trigger.Definition.TenantID != actor.TenantID || scope.Schedule.Trigger.Definition.ID != scope.ScheduleID {
			continue
		}
		scopes = append(scopes, scope)
	}
	return scopes, rows.Err()
}
func (s *AgentScheduleService) managementScope(ctx context.Context, actor scheduled.Actor, id string) (AgentScheduleManagementScope, error) {
	scopes, err := s.managementScopes(ctx, actor)
	if err != nil {
		return AgentScheduleManagementScope{}, err
	}
	var selected AgentScheduleManagementScope
	found := false
	for _, scope := range scopes {
		if scope.ScheduleID != id {
			continue
		}
		if found {
			return AgentScheduleManagementScope{}, agentcontrols.ErrDenied
		}
		selected = scope
		found = true
	}
	if !found {
		return AgentScheduleManagementScope{}, agentcontrols.ErrDenied
	}
	return selected, nil
}
func agentScheduleScopeMatches(scope AgentScheduleManagementScope, s scheduled.Schedule) bool {
	granted := scope.Schedule
	d, g := s.Trigger.Definition, granted.Trigger.Definition
	if d.AgentRun == nil || g.AgentRun == nil {
		return false
	}
	sourceAllowed := d.Source.Kind == schedule.SourceCron && slices.Contains(scope.CronExpressions, d.Source.Cron.Expression) || d.Source.Kind == schedule.SourceCalendar && d.Source.Calendar == g.Source.Calendar && reflect.DeepEqual(s.Calendar, granted.Calendar)
	return sourceAllowed && s.OwnerID == granted.OwnerID && s.InstallationID == granted.InstallationID && s.AgentPrincipalID == granted.AgentPrincipalID && s.LegalEntity == granted.LegalEntity && s.Context == granted.Context && s.Zone == granted.Zone && s.RunTimeout <= granted.RunTimeout && s.RunTimeout > 0 && d.AgentRun.Agent == g.AgentRun.Agent && d.AgentRun.SponsorID == g.AgentRun.SponsorID && d.AgentRun.Purpose == g.AgentRun.Purpose && d.AgentRun.Destination == g.AgentRun.Destination && d.AgentRun.Budget.MaxCostMicros <= g.AgentRun.Budget.MaxCostMicros && d.AgentRun.Budget.MaxInputTokens <= g.AgentRun.Budget.MaxInputTokens && d.AgentRun.Budget.MaxOutputTokens <= g.AgentRun.Budget.MaxOutputTokens && slices.Contains(scope.MisfirePolicies, s.Misfire.Policy) && slices.Contains(scope.OverlapPolicies, d.Overlap) && slices.Contains(scope.DSTPolicies, s.DST) && s.Misfire.MaxCatchUp <= granted.Misfire.MaxCatchUp && s.Misfire.Grace == granted.Misfire.Grace && d.InputTemplateDigest == g.InputTemplateDigest && d.Storm == g.Storm && d.StormPolicy == g.StormPolicy
}

type agentScheduleAuthority struct {
	service  *AgentScheduleService
	bindings interface {
		CheckCurrent(context.Context, agentrun.Request) error
	}
}

func (a agentScheduleAuthority) Authorize(ctx context.Context, actor scheduled.Actor, action scheduled.Action, s scheduled.Schedule) ([]schedule.AuthorizedTarget, error) {
	scope, err := a.service.managementScope(ctx, actor, s.Trigger.Definition.ID)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(scope.Actions, string(action)) || !agentScheduleScopeMatches(scope, s) {
		return nil, scheduled.ErrAuthority
	}
	if action != scheduled.ActionPause && action != scheduled.ActionSkip && action != scheduled.ActionRetire {
		if err := a.CheckCurrent(ctx, s); err != nil {
			return nil, err
		}
	}
	return []schedule.AuthorizedTarget{{AgentRun: s.Trigger.Definition.AgentRun}}, nil
}
func (a agentScheduleAuthority) CheckCurrent(ctx context.Context, s scheduled.Schedule) error {
	target := s.Trigger.Definition.AgentRun
	if target == nil {
		return scheduled.ErrAuthority
	}
	r := agentrun.Request{Source: agentrun.SourceIdentity{TenantID: s.Trigger.Definition.TenantID, Kind: agentrun.SourceSchedule, Key: "publication", Ref: s.Trigger.Definition.ID}, LegalEntity: s.LegalEntity, Agent: agentrun.VersionRef{AgentID: target.Agent.ID, Version: target.Agent.Version, Digest: target.Agent.Digest}, InstallationID: s.InstallationID, Principal: agentrun.PrincipalChain{Mode: agentrun.ModeSponsored, AgentPrincipalID: s.AgentPrincipalID, SponsorID: target.SponsorID}, Purpose: target.Purpose, Audience: agentrun.AudienceScope{ID: target.Destination.AudienceID, SnapshotID: target.Destination.AudienceSnapshotID, Digest: target.Destination.AudienceDigest}, Context: s.Context, Deadline: a.service.cfg.Now().Add(s.RunTimeout), Budget: agentrun.Budget{MaxCostMicros: target.Budget.MaxCostMicros, MaxInputTokens: target.Budget.MaxInputTokens, MaxOutputTokens: target.Budget.MaxOutputTokens}}
	return a.bindings.CheckCurrent(ctx, r)
}
func (s *AgentScheduleService) resolveDraft(ctx context.Context, actor scheduled.Actor, input productui.AgentScheduleDraft) (scheduled.Schedule, error) {
	scope, err := s.managementScope(ctx, actor, input.ID)
	if err != nil {
		return scheduled.Schedule{}, err
	}
	if !slices.Contains(scope.Actions, "DRAFT") && !slices.Contains(scope.Actions, "PREVIEW") {
		return scheduled.Schedule{}, agentcontrols.ErrDenied
	}
	record := scope.Schedule
	def := record.Trigger.Definition
	target := *def.AgentRun
	def.AgentRun = &target
	if input.Installation != record.InstallationID || input.Version != target.Agent.Version || input.Zone != record.Zone.ID || input.Destination != target.Destination.AudienceID {
		return scheduled.Schedule{}, agentcontrols.ErrInvalid
	}
	def.Version = agentSchedulePublicationVersion(input.ExpectedRevision)
	if input.Calendar != "" {
		if def.Source.Kind != schedule.SourceCalendar || input.Calendar != record.Calendar.Calendar.String() || input.Recurrence != "" {
			return scheduled.Schedule{}, agentcontrols.ErrInvalid
		}
	} else {
		def.Source = schedule.TriggerSource{Kind: schedule.SourceCron, Cron: schedule.CronSource{Expression: input.Recurrence}}
	}
	def.Overlap = schedule.OverlapPolicy(input.Overlap)
	def.OverlapPolicy = ""
	var budget schedule.AgentRunBudget
	decoder := json.NewDecoder(bytes.NewBufferString(input.Budget))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&budget) != nil || decoder.Decode(new(any)) != io.EOF {
		return scheduled.Schedule{}, agentcontrols.ErrInvalid
	}
	target.Budget = budget
	record.Misfire.Policy = schedule.MisfirePolicy(input.Misfire)
	record.DST = input.DST
	record.State = scheduled.StateDraft
	record.Revision = input.ExpectedRevision
	record.Trigger.Definition = def
	if !agentScheduleScopeMatches(scope, record) {
		return scheduled.Schedule{}, agentcontrols.ErrDenied
	}
	publication, err := schedule.Publish(schedule.NewRegistry(), def, []schedule.AuthorizedTarget{{AgentRun: &target}})
	if err != nil {
		return scheduled.Schedule{}, agentScheduleError(err)
	}
	record.Trigger = publication
	return record, nil
}

var _ scheduled.OwnerAuthority = agentScheduleAuthority{}
