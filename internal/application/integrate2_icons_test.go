package application

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestIntegrate2ServedPersonasSurviveInvalidIcon(t *testing.T) {
	store, scoped, _, now, db := agentIconApplicationFixture(t)
	base, ctx, _, _, _ := personaSurfaceFixture(t)
	principal, _ := trust.FromContext(ctx)
	profiles, err := base.Personas.ListAvailable(ctx, principal)
	if err != nil || len(profiles) == 0 {
		t.Fatal(profiles, err)
	}
	profile := profiles[0].Profile
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	row := agentpersonastore.PersonaVersion{TenantID: "tenant-a", PersonaID: profile.PersonaID, Version: int64(profile.Version), AgentVersion: "agent-v1", Handle: profile.Handle, DisplayName: profile.DisplayName, Profile: raw, ContentDigest: profiles[0].Digest, CreatedAt: now}
	owner := agentpersonastore.PersonaOwner{TenantID: "tenant-a", PersonaID: profile.PersonaID, Role: agentpersonastore.BusinessOwner, PrincipalID: profile.Owner}
	steward := agentpersonastore.PersonaOwner{TenantID: "tenant-a", PersonaID: profile.PersonaID, Role: agentpersonastore.TechnicalSteward, PrincipalID: profile.Steward}
	if err = scoped.CreateDraftWithIcon(ctx, row, owner, steward, "user-a", now); err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `UPDATE persona_icons SET icon='{}'::jsonb WHERE persona_id=$1`, profile.PersonaID)
	if _, err = scoped.GetIcon(ctx, profile.PersonaID); err == nil {
		t.Fatal("invalid icon fixture not rejected")
	}
	admission, bearer := integrate1Admission(t, "tenant-a", "user-a", base.Now)
	handler := OverlayPersonaChatSurface(http.NotFoundHandler(), base, admission, PersonaChatBrowserOptions{Icons: store})
	req := httptest.NewRequest("GET", personachat.Path+"?conversation_id=channel-a", nil)
	req.Header.Set("Authorization", bearer)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), profile.DisplayName) || !strings.Contains(w.Body.String(), `"icon"`) || !strings.Contains(w.Body.String(), `"icon_revision"`) {
		t.Fatalf("optional icon disabled personas: %d %s", w.Code, w.Body)
	}
}

func TestIntegrate2IconCollisionBackfill(t *testing.T) {
	_, scoped, ctx, now, db := agentIconApplicationFixture(t)
	agentIconCreateApplicationDraft(t, scoped, ctx, now, "assistant")
	agentIconCreateApplicationDraft(t, scoped, ctx, now, "policy-helper")
	first, err := scoped.GetIcon(ctx, "assistant")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(first.Value)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `UPDATE persona_icons SET icon=$1::jsonb,initial_icon=$1::jsonb WHERE persona_id='policy-helper'`, string(encoded))
	count, err := scoped.BackfillIcons(ctx, "operator", now)
	if err != nil || count != 1 {
		t.Fatal("repair count", count, err)
	}
	second, err := scoped.GetIcon(ctx, "policy-helper")
	if err != nil {
		t.Fatal(err)
	}
	repaired, err := scoped.GetIcon(ctx, "assistant")
	if err != nil {
		t.Fatal(err)
	}
	if repaired.Value == second.Value || (repaired.Value.Glyph == second.Value.Glyph && repaired.Value.Shape == second.Value.Shape && repaired.Value.Foreground == second.Value.Foreground && repaired.Value.Background == second.Value.Background) {
		t.Fatal("visible collision survived", repaired, second)
	}
	if count, err = scoped.BackfillIcons(ctx, "operator", now); err != nil || count != 0 {
		t.Fatal("backfill not idempotent", count, err)
	}
}
