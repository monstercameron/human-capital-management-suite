package providerreceipt_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerreceipt"
)

func TestTodo_INTAPI_014(t *testing.T) {
	h := providerreceipt.NewInboundHandler(payrollVerifier(t, nil), func() time.Time { return wall.Add(time.Second) })
	req := httptest.NewRequest("POST", providerreceipt.InboundPath("connection-1"), strings.NewReader(payrollBody))
	req.Header = okHeader(payrollBody)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != 202 {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	var response providerreceipt.InboundResponse
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Accepted || response.EventID != "evt-1" || response.PayloadDigest == "" {
		t.Fatalf("response = %+v", response)
	}
}

func TestTodo_INTAPI_014_Integration(t *testing.T) {
	h := providerreceipt.NewInboundHandler(payrollVerifier(t, nil), func() time.Time { return wall })
	req := httptest.NewRequest("POST", "/v1/inbound/connection-1", strings.NewReader(payrollBody))
	req.Header = okHeader(payrollBody)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != 202 {
		t.Fatalf("first callback status = %d", res.Code)
	}
	duplicate := httptest.NewRecorder()
	replay := httptest.NewRequest("POST", "/v1/inbound/connection-1", strings.NewReader(payrollBody))
	replay.Header = okHeader(payrollBody)
	h.ServeHTTP(duplicate, replay)
	if duplicate.Code != 202 {
		t.Fatalf("identical replay status = %d", duplicate.Code)
	}
}

func TestTodo_INTAPI_014_Security(t *testing.T) {
	h := providerreceipt.NewInboundHandler(payrollVerifier(t, nil), func() time.Time { return wall })
	unauthenticated := httptest.NewRecorder()
	h.ServeHTTP(unauthenticated, httptest.NewRequest("GET", "/v1/inbound/connection-1", nil))
	if unauthenticated.Code != 404 {
		t.Fatalf("GET status = %d", unauthenticated.Code)
	}
	bad := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/inbound/connection-1", strings.NewReader(payrollBody))
	req.Header = okHeader(payrollBody)
	req.Header.Set(providerreceipt.HeaderSignature, "forged")
	h.ServeHTTP(bad, req)
	if bad.Code != 400 {
		t.Fatalf("forged callback status = %d", bad.Code)
	}
}
