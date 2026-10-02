package application

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chatremoveNameDirectory struct {
	Read func(context.Context, string, []string) (map[string]string, error)
}

func (d chatremoveNameDirectory) ModerationNames(ctx context.Context, p chat.Principal, tenant string, ids []string) (map[string]string, error) {
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman || verified.Tenant().String() != tenant || p.TenantID != tenant || p.SubjectID != verified.Subject() {
		return nil, chat.ErrPermissionDenied
	}
	if d.Read == nil {
		return map[string]string{}, nil
	}
	names, err := d.Read(ctx, tenant, ids)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, id := range ids {
		// The workforce directory falls back to its key. Keep that identifier
		// private and let the page use its localized author label instead.
		if name := strings.TrimSpace(names[id]); name != "" && name != id {
			out[id] = name
		}
	}
	return out, nil
}
