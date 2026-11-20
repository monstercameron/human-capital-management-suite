package application

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

// TestIssue3920_QuietSchedule_Integration_Security drives /api/chat/quiet-schedule
// through the served handler assembly over a really composed chat runtime: a
// person saves a weekly schedule and a pause, reads them back with the next
// pause or resume, optional delivery follows it while a governed HCM notice does
// not, a stale revision is refused, and nobody else sees or changes it.
func TestIssue3920_QuietSchedule_Integration_Security(t *testing.T) {
	ctx := context.Background()
	chatDB, coreDB := pgtest.NewEmpty(t), pgtest.NewEmpty(t)
	migrations, err := fs.Sub(chatstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, chatDB.SQL, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	databaseURL := func(raw, schema string) string {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	core, err := pgxadapter.NewPool(ctx, databaseURL(coreDB.URL, coreDB.Schema), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	deployment, err := composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: databaseURL(chatDB.URL, chatDB.Schema), ChatCursorKey: "issue-3920-quiet-cursor-key"}, time.Now, chatAdminFacts{role: "employee"}, core, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer deployment.close()
	at := time.Now().UTC()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: "alice", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "test-session-alice", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "test-verified-alice"})
	if err != nil {
		t.Fatal(err)
	}
	aliceCtx := trust.WithPrincipal(ctx, principal)
	alice := chatcore.Principal{TenantID: "host", SubjectID: "alice"}
	room, err := deployment.service.CreateConversation(aliceCtx, chatcore.CreateConversationRequest{Principal: alice, TenantID: "host", Kind: chatcore.PublicChannel, Name: "quiet-room", IdempotencyKey: "issue-3920-room"})
	if err != nil {
		t.Fatal(err)
	}

	call := func(method, tenant, subject, body string) (int, ChatQuietScheduleReply, string) {
		t.Helper()
		admission, bearer := integrate1Admission(t, tenant, subject, time.Now)
		handler := overlayIntegrate1Chat(http.NotFoundHandler(), deployment, VoiceService{}, admission)
		r := httptest.NewRequest(method, ChatQuietSchedulePath, strings.NewReader(body))
		r.Header.Set("Authorization", bearer)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		var reply ChatQuietScheduleReply
		if w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
				t.Fatal(err)
			}
		}
		return w.Code, reply, w.Body.String()
	}
	days := func(mode string) string {
		parts := make([]string, 7)
		for i := range parts {
			parts[i] = `{"mode":"` + mode + `"}`
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	// A person who never chose has no weekly schedule and the daily interval is as it was.
	code, reply, body := call(http.MethodGet, "host", "alice", "")
	if code != http.StatusOK || reply.WeekActive || reply.Revision != 1 || reply.Pause.Kind != "" || reply.StartMinute != 1320 {
		t.Fatalf("default: %d %s", code, body)
	}
	// Every day is quiet all day: optional delivery stops, with the explicit next resume absent.
	if code, reply, body = call(http.MethodPost, "host", "alice", `{"expected_revision":1,"week_active":true,"days":`+days("all")+`}`); code != http.StatusOK || !reply.WeekActive || reply.Revision != 2 {
		t.Fatalf("save: %d %s", code, body)
	}
	// The daily switch is the master switch: it is still off, so nothing is quiet yet.
	if reply.Enabled || reply.QuietNow {
		t.Fatalf("a weekly schedule must not switch quiet hours on by itself: %s", body)
	}
	svc := deployment.extensions.Recipients
	if ok, err := svc.OptionalDelivery(aliceCtx, alice, "host", room.ID, chatrecipient.OptionalMessage, time.Now()); err != nil || !ok {
		t.Fatalf("delivery with quiet hours off = %v %v", ok, err)
	}
	// A pause is separate from the schedule and ends on its own.
	until := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)
	if code, reply, body = call(http.MethodPost, "host", "alice", `{"expected_revision":2,"week_active":true,"days":`+days("all")+`,"pause":{"kind":"pause","until":"`+until+`"}}`); code != http.StatusOK || !reply.QuietNow || reply.Pause.Kind != "pause" || reply.Next == nil || reply.Next.Quiet {
		t.Fatalf("pause: %d %s", code, body)
	}
	if ok, err := svc.OptionalDelivery(aliceCtx, alice, "host", room.ID, chatrecipient.OptionalMessage, time.Now()); err != nil || ok {
		t.Fatalf("an optional message was delivered during a pause: %v %v", ok, err)
	}
	if ok, err := svc.OptionalDelivery(aliceCtx, alice, "host", room.ID, chatrecipient.GovernedHCM, time.Now()); err != nil || !ok {
		t.Fatalf("a governed HCM notice was held by a pause: %v %v", ok, err)
	}
	// A stale revision is refused and changes nothing.
	if code, _, body = call(http.MethodPost, "host", "alice", `{"expected_revision":2,"week_active":false,"days":`+days("none")+`}`); code != http.StatusConflict || !strings.Contains(body, `"conflict"`) {
		t.Fatalf("stale: %d %s", code, body)
	}
	// An ambiguous week is refused with the day and the reason.
	bad := `[{"mode":"none"},{"mode":"interval","start":1320,"end":480},{"mode":"interval","start":420,"end":540},{"mode":"none"},{"mode":"none"},{"mode":"none"},{"mode":"none"}]`
	if code, _, body = call(http.MethodPost, "host", "alice", `{"expected_revision":3,"week_active":true,"days":`+bad+`}`); code != http.StatusBadRequest || !strings.Contains(body, `"reason":"overlap"`) || !strings.Contains(body, `"day":2`) {
		t.Fatalf("overlap: %d %s", code, body)
	}
	// Someone else, and another company, see their own (empty) schedule.
	if code, reply, body = call(http.MethodGet, "host", "bob", ""); code != http.StatusOK || reply.WeekActive || reply.Pause.Kind != "" {
		t.Fatalf("bob reads alice's schedule: %d %s", code, body)
	}
	if code, reply, body = call(http.MethodGet, "other", "alice", ""); code != http.StatusOK || reply.WeekActive || reply.Pause.Kind != "" {
		t.Fatalf("another company reads it: %d %s", code, body)
	}
}
