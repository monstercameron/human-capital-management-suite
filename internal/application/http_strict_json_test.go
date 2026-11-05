package application

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatrev_StrictJSONBody(t *testing.T) {
	decode := func(body string, limit int64) error {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		var into struct {
			Name string `json:"name"`
		}
		return decodeStrictJSONBody(httptest.NewRecorder(), request, limit, &into)
	}
	if err := decode(`{"name":"a"}`, 1024); err != nil {
		t.Fatalf("valid body refused: %v", err)
	}
	for name, body := range map[string]string{
		"unknown field": `{"name":"a","extra":1}`,
		"trailing data": `{"name":"a"} {"name":"b"}`,
		"trailing word": `{"name":"a"} garbage`,
		"malformed":     `{"name":`,
		"empty":         ``,
		"wrong type":    `{"name":42}`,
	} {
		if err := decode(body, 1024); !errors.Is(err, errHTTPStrictJSONInvalid) {
			t.Fatalf("%s: err = %v, want invalid", name, err)
		}
	}
	if err := decode(`{"name":"a"}`, 4); !errors.Is(err, errHTTPStrictJSONInvalid) {
		t.Fatalf("over-limit body: err = %v, want invalid", err)
	}
	if err := decodeStrictJSONBody(nil, nil, 0, nil); !errors.Is(err, errHTTPStrictJSONInvalid) {
		t.Fatalf("nil call: err = %v, want invalid", err)
	}
}
