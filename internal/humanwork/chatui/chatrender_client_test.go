package chatui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

type chatrenderRoundTrip func(*http.Request) (*http.Response, error)

func (f chatrenderRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
func testRenderingSettingsClient(t *testing.T) {
	calls := 0
	pref := chatrender.DefaultPreference("de")
	payload, _ := json.Marshal(pref)
	client := ReadingSettingsClient{Origin: "https://product.example", Bearer: "fixture-session", Locale: "de-DE", HTTP: &http.Client{Transport: chatrenderRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fixture-session" || r.URL.Host != "product.example" || r.URL.Query().Get("conversation") != "room & one" {
			t.Fatal("wrong session or endpoint")
		}
		if r.Header.Get("Accept-Language") != "de-DE" {
			t.Fatal("interface locale missing")
		}
		if r.URL.Path == "/api/chat/renderings/v1/languages" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"de":2}`)), Header: make(http.Header)}, nil
		}
		if r.URL.Path != "/api/chat/renderings/v1/settings" {
			t.Fatal("wrong endpoint")
		}
		if r.Method == http.MethodPut {
			if r.Header.Get("Content-Type") != "application/json" {
				t.Fatal("missing JSON type")
			}
			var body struct {
				Settings chatrender.Preference `json:"settings"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Settings.ReadingLanguage != "de" {
				t.Fatal(body, err)
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(payload))), Header: make(http.Header)}, nil
	})}}
	got, err := client.Load(context.Background(), "room & one")
	if err != nil || got.ReadingLanguage != "de" {
		t.Fatal(got, err)
	}
	if err = client.Save(context.Background(), "room & one", pref); err != nil || calls != 2 {
		t.Fatal(calls, err)
	}
	counts, err := client.LoadLanguages(context.Background(), "room & one")
	if err != nil || counts["de"] != 2 || calls != 3 {
		t.Fatal(counts, calls, err)
	}
	if counts, err = client.LoadLanguages(context.Background(), ""); err != nil || len(counts) != 0 || calls != 3 {
		t.Fatal(counts, calls, err)
	}
	bad := client
	bad.Bearer = ""
	if _, err = bad.Load(context.Background(), ""); err != chatrender.ErrUnavailable {
		t.Fatal(err)
	}
	bad = client
	bad.Origin = "https://user:secret@other.example"
	if _, err = bad.Load(context.Background(), ""); err != chatrender.ErrUnavailable {
		t.Fatal(err)
	}
	client.HTTP.Transport = chatrenderRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unavailable")), Header: make(http.Header)}, nil
	})
	if _, err = client.Load(context.Background(), ""); err != chatrender.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err = client.LoadLanguages(context.Background(), "room & one"); err != chatrender.ErrUnavailable {
		t.Fatal(err)
	}
	if err = client.Save(context.Background(), "", pref); err != chatrender.ErrUnavailable {
		t.Fatal(err)
	}
	pref.ReadingLanguage = "xx"
	if err = client.Save(context.Background(), "", pref); err != chatrender.ErrInvalid {
		t.Fatal(err)
	}
}
