package application

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
)

// Issue 3819: the refusal for a decision without a reason carries the field name
// so the page can show the sentence under the reason input.
func TestIssue3819_RefusalNamesTheReasonField(t *testing.T) {
	w := httptest.NewRecorder()
	chatgateHTTPError(w, chatgate.FieldError{Field: "reason", Cause: chatgate.ErrInvalid})
	var body struct {
		Error  string            `json:"error"`
		Fields map[string]string `json:"fields"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != 400 || body.Error != "invalid_argument" || body.Fields["reason"] != "invalid_argument" {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
}
