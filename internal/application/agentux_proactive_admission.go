package application

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func (r *AgentAnnouncementRuntime) admissionRepository(tenant string) (*agentrunstore.AdmissionRepository, error) {
	return agentrunstore.NewAdmissionRepositoryWithSourceResolver(r.Agents, r.Work.tenantUUID(values.TenantId(tenant)), values.TenantId(tenant), announcementAdmissionSourceKeys{r})
}

type announcementAdmissionSourceKeys struct{ runtime *AgentAnnouncementRuntime }

func (s announcementAdmissionSourceKeys) ResolveSourceKey(ctx context.Context, request agentrun.Request) (string, error) {
	if request.Source.Kind != agentrun.SourceAnnouncement || request.Source.Ref == "" {
		return "", ErrAgentAnnouncementDenied
	}
	if strings.HasPrefix(request.Source.Ref, "now:"+request.Context.ID+":") || request.Source.Ref == "preview:"+request.Context.ID {
		return request.Source.Ref, nil
	}
	store, err := (announcementScheduleStore{s.runtime}).tenant(request.Source.TenantID)
	if err != nil {
		return "", err
	}
	delivery, found, err := store.GetDelivery(ctx, request.Source.TenantID, request.Source.Ref)
	if err != nil || !found || delivery.Request.Source.Kind != agentrun.SourceAnnouncement || delivery.Request.Context != request.Context || delivery.Request.InstallationID != request.InstallationID {
		return "", ErrAgentAnnouncementDenied
	}
	// The durable admission omits the raw source key. Recover it from the
	// occurrence owner before comparing the frozen request; the repository
	// then checks its persisted source-key digest.
	if request.Source.Key == "" {
		request.Source.Key = delivery.Key
	}
	want, wantErr := agentrun.AdmissionRequestDigest(delivery.Request)
	got, gotErr := agentrun.AdmissionRequestDigest(request)
	if wantErr != nil || gotErr != nil || want != got {
		return "", ErrAgentAnnouncementDenied
	}
	return delivery.Key, nil
}

// Other source kinds retain the canonical recovery used by the mention reader.
type announcementEvidenceSourceKeys struct{ runtime *AgentAnnouncementRuntime }

func (s announcementEvidenceSourceKeys) ResolveSourceKey(ctx context.Context, request agentrun.Request) (string, error) {
	if request.Source.Kind == agentrun.SourceAnnouncement {
		return (announcementAdmissionSourceKeys{s.runtime}).ResolveSourceKey(ctx, request)
	}
	source, err := (agentrun.CanonicalSourceConverter{}).ConvertSource(ctx, request)
	return source.Key, err
}
