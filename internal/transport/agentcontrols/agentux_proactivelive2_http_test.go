package agentcontrols

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type proactiveLive2Refusal struct{}

func (proactiveLive2Refusal) Error() string { return "private raw model text" }
func (proactiveLive2Refusal) AnnouncementReason() string {
	return "The corrected reply contains source lines. Preview again."
}

func TestAgentUXProactiveLive_RefusalReason_HTTP(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeAnnouncementReply(recorder, AnnouncementReply{}, errors.Join(ErrUnavailable, proactiveLive2Refusal{}))
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "corrected reply contains source lines") || strings.Contains(recorder.Body.String(), "private raw model text") {
		t.Fatalf("unsafe or missing reason: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	writeAnnouncementReply(recorder, AnnouncementReply{}, errors.New("internal database credentials cause"))
	if strings.Contains(recorder.Body.String(), "credentials") || strings.Contains(recorder.Body.String(), "reason") {
		t.Fatalf("unapproved cause leaked: %s", recorder.Body.String())
	}
}
