package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PersonaCatalogRuntimeStatusReader checks the current published version and
// its live runtime identity before offering a stopped placement for repair.
type PersonaCatalogRuntimeStatusReader interface {
	PersonaRuntimeReady(context.Context, values.TenantId, string, int64) bool
}

type PersonaCatalogRuntimeStatus struct {
	Store     *agentpersonastore.Store
	Principal PersonaRunAgentPrincipalResolver
}

func (r PersonaCatalogRuntimeStatus) PersonaRuntimeReady(ctx context.Context, tenant values.TenantId, persona string, version int64) bool {
	if r.Store == nil {
		return false
	}
	scoped, err := r.Store.Scoped(tenant)
	if err != nil {
		return false
	}
	state, err := scoped.Lifecycle(ctx, persona, version)
	if err != nil || state != agentpersonastore.StatePublished {
		return false
	}
	_, err = r.Principal.Resolve(ctx, tenant, persona, version)
	return err == nil
}

// ProjectPersonaCatalogInstallationRuntime contains no backend error text.
// The page translates the code and shows the plain sentence, never the code.
func ProjectPersonaCatalogInstallationRuntime(ctx context.Context, tenant values.TenantId, in PersonaCatalogInstallation, runtime PersonaCatalogRuntimeStatusReader) PersonaCatalogInstallation {
	in.SuspensionMessage = ""
	in.RestartAvailable = false
	if in.State != string(agentpersonastore.InstallationSuspended) {
		return in
	}
	in.SuspensionMessage = personaCatalogSuspensionMessage(in.SuspensionReason)
	in.RestartAvailable = runtime != nil && runtime.PersonaRuntimeReady(ctx, tenant, in.PersonaID, int64(in.PersonaVersion))
	return in
}

// The state-aware projection is separate from preview's active-placement read.
func (r personaAdminCatalogInstallations) ListPersonaCatalogInstallationStates(ctx context.Context, tenant values.TenantId) ([]PersonaCatalogInstallation, error) {
	entries, err := listPersonaAdminCatalog(ctx, r.store, tenant)
	if err != nil {
		return nil, err
	}
	var out []PersonaCatalogInstallation
	for _, entry := range entries {
		for _, in := range entry.Installations {
			channel, kind, ok := catalogPlacementKinds(in.ConversationClass)
			tier, valid := catalogPlacementTier(in.ChannelPolicy.MaxTier)
			if !ok || !valid || len(in.ChannelPolicy.AllowedDataClasses) == 0 {
				continue
			}
			out = append(out, PersonaCatalogInstallation{ID: in.ID, PersonaID: in.PersonaID, PersonaVersion: uint32(in.PersonaVersion), ConversationID: in.ConversationID, State: in.State, SuspensionReason: in.SuspensionReason, Active: in.State == string(agentpersonastore.InstallationActive), ChannelClass: string(channel), ConversationKind: string(kind), MaxTier: tier, AllowedDataClasses: append([]string(nil), in.ChannelPolicy.AllowedDataClasses...)})
		}
	}
	return out, nil
}

func personaCatalogVisibleInstallations(rows []PersonaCatalogInstallation) []PersonaCatalogInstallation {
	out := make([]PersonaCatalogInstallation, 0, len(rows))
	indices := make(map[string]int)
	for _, row := range rows {
		if !row.Active && row.State != string(agentpersonastore.InstallationSuspended) {
			continue
		}
		key := row.PersonaID + "\x00" + row.ConversationID
		if index, ok := indices[key]; ok {
			previous := out[index]
			if row.Active && !previous.Active || row.Active == previous.Active && (row.PersonaVersion > previous.PersonaVersion || row.PersonaVersion == previous.PersonaVersion && row.ID < previous.ID) {
				out[index] = row
			}
		} else {
			indices[key] = len(out)
			out = append(out, row)
		}
	}
	return out
}
