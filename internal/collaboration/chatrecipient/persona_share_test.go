package chatrecipient

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdeliver"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

type personaShareSourceFake struct {
	result PrivatePersonaResult
	err    error
}

func (f *personaShareSourceFake) LoadPrivatePersonaResult(_ context.Context, _ chat.Principal, _ string) (PrivatePersonaResult, error) {
	return f.result, f.err
}

type personaShareCommitterFake struct {
	request      PersonaShareCommitRequest
	post         chat.Post
	err          error
	calls        int
	beforeCommit func()
}

func (f *personaShareCommitterFake) CommitPersonaShare(_ context.Context, request PersonaShareCommitRequest) (chat.Post, error) {
	f.calls++
	f.request = request
	if f.beforeCommit != nil {
		f.beforeCommit()
	}
	return chat.Post{ID: "shared-post", Body: request.Body}, f.err
}

func personaSharePrincipal() chat.Principal {
	return chat.Principal{TenantID: "home", SubjectID: "invoker"}
}

func personaShareResult() PrivatePersonaResult {
	return PrivatePersonaResult{
		EphemeralID: "ephemeral-result", TenantID: "host", OwnerHomeTenantID: "home", OwnerSubjectID: "invoker",
		ThreadID: "root-post", PersonaDisplayName: "Comp Analyst",
		Conversation: chat.Conversation{ID: "room", TenantID: "host", Kind: chat.PublicChannel, Revision: 7},
		OutputPolicy: agentdeliver.Conversation{TenantID: "host", ConversationID: "room", Kind: agentdeliver.PublicChannel, TenantOrigin: "https://hcm.example"},
		Items: []PrivatePersonaShareItem{
			{Output: agentdeliver.ResultItem{ID: "band-summary", Text: "The band midpoint is $90,000.", Materials: []agentdeliver.Material{{Kind: agentdeliver.MaterialSource, ID: "policy", DataClass: agentdeliver.DataClass(dlp.ClassCompensation)}, {Kind: agentdeliver.MaterialRecord, ID: "band-4", DataClass: agentdeliver.DataClass(dlp.ClassCompensation)}, {Kind: agentdeliver.MaterialField, ID: "band.midpoint", DataClass: agentdeliver.DataClass(dlp.ClassCompensation)}}}, Disclosures: []Disclosure{{SourceID: "policy", RecordID: "band-4", Field: "band.midpoint", DataClass: dlp.ClassCompensation}}},
			{Output: agentdeliver.ResultItem{ID: "employee-value", Text: "Ari's salary is $82,000.", Materials: []agentdeliver.Material{{Kind: agentdeliver.MaterialSource, ID: "payroll", DataClass: agentdeliver.DataClass(dlp.ClassCompensation)}, {Kind: agentdeliver.MaterialRecord, ID: "employee-ari", DataClass: agentdeliver.DataClass(dlp.ClassCompensation)}, {Kind: agentdeliver.MaterialField, ID: "compensation.base", DataClass: agentdeliver.DataClass(dlp.ClassCompensation)}}}, Disclosures: []Disclosure{{SourceID: "payroll", RecordID: "employee-ari", Field: "compensation.base", DataClass: dlp.ClassCompensation}}},
		},
	}
}

func personaShareFixture(t *testing.T) (*PersonaShareService, *personaShareSourceFake, *audienceFloorAuthority, *personaShareCommitterFake) {
	t.Helper()
	source := &personaShareSourceFake{result: personaShareResult()}
	audience := floorAuthority()
	committer := &personaShareCommitterFake{}
	now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	service := NewPersonaShareService(source, audience, committer, func() time.Time { return now })
	return service, source, audience, committer
}

func TestTodo_AGENTP_013(t *testing.T) {
	service, _, audience, committer := personaShareFixture(t)
	audience.deniedDisclosure = map[string]bool{disclosureKey(personaShareResult().Items[1].Disclosures[0]): true}
	preview, err := service.PreviewPersonaShare(context.Background(), PersonaSharePreviewRequest{Principal: personaSharePrincipal(), EphemeralResultID: "ephemeral-result"})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Items) != 2 || !preview.Items[0].Kept || preview.Items[0].DropReason != "" || preview.Items[1].Kept || preview.Items[1].DropReason != AudienceFloorUnauthorized {
		t.Fatalf("preview items = %+v", preview.Items)
	}
	if preview.AudienceRevision != 41 || preview.Token == "" || preview.ThreadID != "root-post" {
		t.Fatalf("preview binding = %+v", preview)
	}
	confirmed, err := service.ConfirmPersonaShare(context.Background(), PersonaShareConfirmRequest{Principal: personaSharePrincipal(), Token: preview.Token})
	if err != nil || !confirmed.Shared || confirmed.PostID != "shared-post" {
		t.Fatalf("confirm = %+v, %v", confirmed, err)
	}
	if committer.calls != 1 || committer.request.Principal.SubjectID != "invoker" || committer.request.ThreadID != "root-post" || committer.request.ExpectedAudienceRevision != 41 {
		t.Fatalf("commit request = %+v calls=%d", committer.request, committer.calls)
	}
	if !strings.Contains(committer.request.Body, "Shared from Comp Analyst") || !strings.Contains(committer.request.Body, "The band midpoint") || strings.Contains(committer.request.Body, "Ari's salary") {
		t.Fatalf("posted body did not preserve only passing items with attribution: %q", committer.request.Body)
	}
	replayed, err := service.ConfirmPersonaShare(context.Background(), PersonaShareConfirmRequest{Principal: personaSharePrincipal(), Token: preview.Token})
	if err != nil || !replayed.Shared || replayed.PostID != confirmed.PostID || committer.calls != 1 {
		t.Fatalf("idempotent confirm = %+v err=%v calls=%d", replayed, err, committer.calls)
	}
}

func TestTodo_AGENTP_013_Security(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*personaShareSourceFake)
		actor  chat.Principal
	}{
		{name: "result belongs to another invoker", mutate: func(source *personaShareSourceFake) { source.result.OwnerSubjectID = "other" }, actor: personaSharePrincipal()},
		{name: "result belongs to another home tenant", mutate: func(source *personaShareSourceFake) { source.result.OwnerHomeTenantID = "other-home" }, actor: personaSharePrincipal()},
		{name: "persona forces private", mutate: func(source *personaShareSourceFake) { source.result.AlwaysPrivate = true }, actor: personaSharePrincipal()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, source, _, committer := personaShareFixture(t)
			tc.mutate(source)
			preview, err := service.PreviewPersonaShare(context.Background(), PersonaSharePreviewRequest{Principal: tc.actor, EphemeralResultID: "ephemeral-result"})
			if !source.result.AlwaysPrivate {
				if !errors.Is(err, chat.ErrPermissionDenied) {
					t.Fatalf("foreign private result preview = %v, want permission denied", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("always-private preview: %v", err)
			}
			for _, item := range preview.Items {
				if item.Kept {
					t.Fatalf("unsafe item marked kept: %+v", item)
				}
			}
			if _, err := service.ConfirmPersonaShare(context.Background(), PersonaShareConfirmRequest{Principal: tc.actor, Token: preview.Token}); !errors.Is(err, ErrPersonaSharePreview) {
				t.Fatalf("confirm all-dropped private result = %v, want no-share refusal", err)
			}
			if committer.calls != 0 {
				t.Fatalf("unsafe source reached committer %d times", committer.calls)
			}
		})
	}
	service, _, _, committer := personaShareFixture(t)
	preview, err := service.PreviewPersonaShare(context.Background(), PersonaSharePreviewRequest{Principal: personaSharePrincipal(), EphemeralResultID: "ephemeral-result"})
	if err != nil {
		t.Fatal(err)
	}
	other := chat.Principal{TenantID: "home", SubjectID: "other"}
	if _, err := service.ConfirmPersonaShare(context.Background(), PersonaShareConfirmRequest{Principal: other, Token: preview.Token}); !errors.Is(err, ErrPersonaSharePreview) {
		t.Fatalf("other member confirmed invoker preview: %v", err)
	}
	if committer.calls != 0 {
		t.Fatal("unauthorized confirm reached committer")
	}
}

func TestTodo_AGENTP_013_Race(t *testing.T) {
	// Runs beside the package's other parallel tests so the race detector
	// sees this path against them.
	t.Parallel()
	service, _, audience, committer := personaShareFixture(t)
	preview, err := service.PreviewPersonaShare(context.Background(), PersonaSharePreviewRequest{Principal: personaSharePrincipal(), EphemeralResultID: "ephemeral-result"})
	if err != nil {
		t.Fatal(err)
	}
	audience.snapshot.Revision++
	audience.snapshot.EligibilityPopulation = append(audience.snapshot.EligibilityPopulation, AudiencePrincipal{TenantID: "guest-co", SubjectID: "new-guest", Guest: true, External: true})
	audience.denied = map[string]bool{"guest-co/new-guest/compensation.base": true}
	result, err := service.ConfirmPersonaShare(context.Background(), PersonaShareConfirmRequest{Principal: personaSharePrincipal(), Token: preview.Token})
	if err != nil || result.Shared || result.Preview == nil {
		t.Fatalf("changed audience confirm = %+v, %v", result, err)
	}
	if committer.calls != 0 {
		t.Fatal("stale preview posted before re-preview")
	}
	if result.Preview.AudienceRevision != 42 || !result.Preview.Items[0].Kept || result.Preview.Items[1].Kept {
		t.Fatalf("fresh guest was not reflected in the re-preview: %+v", result.Preview)
	}
	confirmed, err := service.ConfirmPersonaShare(context.Background(), PersonaShareConfirmRequest{Principal: personaSharePrincipal(), Token: result.Preview.Token})
	if err != nil || !confirmed.Shared || committer.calls != 1 || strings.Contains(committer.request.Body, "Ari's salary") {
		t.Fatalf("reconfirmed filtered preview result=%+v err=%v calls=%d body=%q", confirmed, err, committer.calls, committer.request.Body)
	}
}

func TestTodo_AGENTP_013_SourceChangeRequiresNewPreview(t *testing.T) {
	service, source, _, committer := personaShareFixture(t)
	preview, err := service.PreviewPersonaShare(context.Background(), PersonaSharePreviewRequest{Principal: personaSharePrincipal(), EphemeralResultID: "ephemeral-result"})
	if err != nil {
		t.Fatal(err)
	}
	source.result.Items[0].Output.Text = "The updated band midpoint is $95,000."
	confirmed, err := service.ConfirmPersonaShare(context.Background(), PersonaShareConfirmRequest{Principal: personaSharePrincipal(), Token: preview.Token})
	if err != nil || confirmed.Shared || confirmed.Preview == nil || committer.calls != 0 {
		t.Fatalf("changed source confirm=%+v err=%v calls=%d", confirmed, err, committer.calls)
	}
	if confirmed.Preview.Items[0].Text != "The updated band midpoint is $95,000." {
		t.Fatalf("re-preview retained stale source text: %+v", confirmed.Preview.Items[0])
	}
}

func TestTodo_AGENTP_013_ExpiredPreviewCannotCommit(t *testing.T) {
	service, _, _, committer := personaShareFixture(t)
	preview, err := service.PreviewPersonaShare(context.Background(), PersonaSharePreviewRequest{Principal: personaSharePrincipal(), EphemeralResultID: "ephemeral-result"})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return preview.ExpiresAt.Add(time.Second) }
	if _, err := service.ConfirmPersonaShare(context.Background(), PersonaShareConfirmRequest{Principal: personaSharePrincipal(), Token: preview.Token}); !errors.Is(err, ErrPersonaSharePreview) {
		t.Fatalf("expired preview confirm = %v", err)
	}
	if committer.calls != 0 {
		t.Fatal("expired preview reached committer")
	}
}

func TestTodo_AGENTP_013_CommitRaceRepreviews(t *testing.T) {
	service, _, audience, committer := personaShareFixture(t)
	committer.err = ErrPersonaShareAudienceChanged
	committer.beforeCommit = func() {
		audience.snapshot.Revision++
		audience.snapshot.EligibilityPopulation = append(audience.snapshot.EligibilityPopulation, AudiencePrincipal{TenantID: "guest-co", SubjectID: "joined-during-commit", Guest: true, External: true})
		audience.denied = map[string]bool{"guest-co/joined-during-commit/compensation.base": true}
	}
	preview, err := service.PreviewPersonaShare(context.Background(), PersonaSharePreviewRequest{Principal: personaSharePrincipal(), EphemeralResultID: "ephemeral-result"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ConfirmPersonaShare(context.Background(), PersonaShareConfirmRequest{Principal: personaSharePrincipal(), Token: preview.Token})
	if err != nil || result.Shared || result.Preview == nil || committer.calls != 1 {
		t.Fatalf("commit race result=%+v err=%v calls=%d", result, err, committer.calls)
	}
	if result.Preview.AudienceRevision != audience.snapshot.Revision || result.Preview.Token == preview.Token || result.Preview.Items[1].Kept {
		t.Fatalf("commit race did not replace the stale preview: old=%+v new=%+v", preview, result.Preview)
	}
}
