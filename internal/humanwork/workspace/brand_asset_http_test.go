package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/brandasset"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// httpBrandAssets is a deterministic tenant-isolation double for exercising
// the real admitted HTTP routes; the PostgreSQL store remains covered by the
// separately coordinated integration lane.
type httpBrandAssets struct {
	mu       sync.Mutex
	byTenant map[string][]brandasset.Asset
}

func newHTTPBrandAssets() *httpBrandAssets {
	return &httpBrandAssets{byTenant: map[string][]brandasset.Asset{}}
}
func (s *httpBrandAssets) Save(_ context.Context, tenant string, expected int, actor string, asset brandasset.Asset) (brandasset.Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := s.byTenant[tenant]
	if len(rows) != expected {
		return brandasset.Asset{}, ErrBrandAssetVersionConflict
	}
	asset.TenantID, asset.Revision = tenant, expected+1
	s.byTenant[tenant] = append(rows, asset)
	return asset, nil
}
func (s *httpBrandAssets) Current(_ context.Context, tenant string) (brandasset.Asset, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := s.byTenant[tenant]
	if len(rows) == 0 {
		return brandasset.Asset{}, false, nil
	}
	return rows[len(rows)-1], true, nil
}
func (s *httpBrandAssets) Read(_ context.Context, tenant, digest string) (brandasset.Asset, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := s.byTenant[tenant]
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Digest == digest {
			if rows[i].Removed {
				return brandasset.Asset{}, false, nil
			}
			return rows[i], true, nil
		}
	}
	return brandasset.Asset{}, false, nil
}
func (s *httpBrandAssets) History(_ context.Context, tenant string, before int) ([]brandasset.Asset, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := s.byTenant[tenant]
	result := make([]brandasset.Asset, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		v := rows[i]
		v.Original = nil
		v.Proxy = nil
		active := true
		for j := len(rows) - 1; j >= 0; j-- {
			if rows[j].Digest == v.Digest {
				active = !rows[j].Removed
				break
			}
		}
		v.Available = active
		result = append(result, v)
	}
	if before > 0 {
		filtered := result[:0]
		for _, asset := range result {
			if asset.Revision < before {
				filtered = append(filtered, asset)
			}
		}
		result = filtered
	}
	more := len(result) > brandasset.HistoryPageSize
	if more {
		result = result[:brandasset.HistoryPageSize]
	}
	return result, more, nil
}
func (s *httpBrandAssets) Rollback(_ context.Context, tenant string, expected, target int, _ string) (brandasset.Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := s.byTenant[tenant]
	if len(rows) != expected || target < 1 || target >= expected {
		return brandasset.Asset{}, ErrBrandAssetVersionConflict
	}
	asset := rows[target-1]
	asset.Revision = expected + 1
	asset.Removed = false
	s.byTenant[tenant] = append(rows, asset)
	return asset, nil
}
func (s *httpBrandAssets) Remove(_ context.Context, tenant string, expected int, _ string) (brandasset.Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := s.byTenant[tenant]
	if len(rows) != expected || len(rows) == 0 {
		return brandasset.Asset{}, ErrBrandAssetVersionConflict
	}
	asset := rows[len(rows)-1]
	asset.Revision = expected + 1
	asset.Removed = true
	s.byTenant[tenant] = append(rows, asset)
	return asset, nil
}

func brandAssetHTTPToken(t *testing.T, tenant string) string {
	t.Helper()
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: shellSigningKey, Issuer: shellIssuer, Audience: shellAudience, Now: func() time.Time { return shellNow }})
	if err != nil {
		t.Fatal(err)
	}
	token, err := verifier.Issue(trust.Claims{Issuer: shellIssuer, Audience: shellAudience, Subject: shellSubject, SubjectKind: "human", Tenant: tenant, OrganizationScopeID: "org-north-america", Roles: []string{"comp_admin"}, Purposes: []string{shellPurpose}, AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "brand-assets", IssuedAtUnix: shellNow.Add(-time.Minute).Unix(), ExpiresAtUnix: shellNow.Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func brandAssetRequest(t *testing.T, h *Handler, tenant, method, path string, body *bytes.Buffer, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	var requestBody io.Reader
	if body != nil {
		requestBody = body
	}
	request := httptest.NewRequest(method, "http://cell.test"+path, requestBody)
	request.Header.Set("Authorization", "Bearer "+brandAssetHTTPToken(t, tenant))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	return response
}

func TestTodo_UXAUDIT_022_ServedUploadHistoryRemoveRollbackAndTenantIsolation(t *testing.T) {
	h, _ := newShellHandler(t, false)
	repository := newHTTPBrandAssets()
	h.brandAssets = repository
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile(brandAssetFormField, "tenant-logo.png")
	if err != nil {
		t.Fatal(err)
	}
	_, err = part.Write(brandPNG(t, 64, 48))
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	upload := brandAssetRequest(t, h, shellTenant, http.MethodPost, PathBrandAssets, body, writer.FormDataContentType())
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", upload.Code, upload.Body.String())
	}
	var uploaded brandAssetUploadResponse
	if err := json.Unmarshal(upload.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	if uploaded.Revision != 1 || uploaded.URL != PathBrandAssetPrefix+uploaded.Digest {
		t.Fatalf("unexpected upload result: %+v", uploaded)
	}
	read := brandAssetRequest(t, h, "another-tenant", http.MethodGet, uploaded.URL, nil, "")
	if read.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant read status=%d", read.Code)
	}
	read = brandAssetRequest(t, h, shellTenant, http.MethodGet, uploaded.URL, nil, "")
	if read.Code != http.StatusOK || read.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("same-tenant asset status=%d type=%s", read.Code, read.Header().Get("Content-Type"))
	}
	history := brandAssetRequest(t, h, shellTenant, http.MethodGet, PathBrandAssets, nil, "")
	if history.Code != http.StatusOK || !bytes.Contains(history.Body.Bytes(), []byte(`"revision":1`)) {
		t.Fatalf("history status=%d body=%s", history.Code, history.Body.String())
	}
	stale := brandAssetRequest(t, h, shellTenant, http.MethodPost, PathBrandAssetLifecycle, bytes.NewBufferString(`{"action":"remove","expected_revision":0}`), "application/json")
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale lifecycle status=%d body=%s", stale.Code, stale.Body.String())
	}
	removeBody := bytes.NewBufferString(`{"action":"remove","expected_revision":1}`)
	removed := brandAssetRequest(t, h, shellTenant, http.MethodPost, PathBrandAssetLifecycle, removeBody, "application/json")
	if removed.Code != http.StatusOK {
		t.Fatalf("remove status=%d body=%s", removed.Code, removed.Body.String())
	}
	read = brandAssetRequest(t, h, shellTenant, http.MethodGet, uploaded.URL, nil, "")
	if read.Code != http.StatusNotFound {
		t.Fatalf("removed asset read status=%d", read.Code)
	}
	rollbackBody := bytes.NewBufferString(`{"action":"rollback","revision":1,"expected_revision":2}`)
	restored := brandAssetRequest(t, h, shellTenant, http.MethodPost, PathBrandAssetLifecycle, rollbackBody, "application/json")
	if restored.Code != http.StatusOK {
		t.Fatalf("rollback status=%d body=%s", restored.Code, restored.Body.String())
	}
	read = brandAssetRequest(t, h, shellTenant, http.MethodGet, uploaded.URL, nil, "")
	if read.Code != http.StatusOK {
		t.Fatalf("restored asset read status=%d", read.Code)
	}
}

func TestTodo_UXAUDIT_022_ServedUploadRejectsSVGIncludingOnload(t *testing.T) {
	h, _ := newShellHandler(t, false)
	h.brandAssets = newHTTPBrandAssets()
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile(brandAssetFormField, "hostile.svg")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte(`<svg width="64" height="64" onload="alert(1)"/>`))
	_ = writer.Close()
	response := brandAssetRequest(t, h, shellTenant, http.MethodPost, PathBrandAssets, body, writer.FormDataContentType())
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("SVG upload status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTodo_UXAUDIT_022_ServedHistoryUsesStableBoundedCursor(t *testing.T) {
	h, _ := newShellHandler(t, false)
	assets := newHTTPBrandAssets()
	for revision := 1; revision <= brandasset.HistoryPageSize+1; revision++ {
		assets.byTenant[shellTenant] = append(assets.byTenant[shellTenant], brandasset.Asset{
			TenantID: shellTenant, Revision: revision, Name: "logo.png", MediaType: "image/png", Width: 64, Height: 48,
			Digest: strings.Repeat("a", 64), Available: true,
		})
	}
	h.brandAssets = assets
	first := brandAssetRequest(t, h, shellTenant, http.MethodGet, PathBrandAssets, nil, "")
	var firstPage brandAssetHistoryPage
	if err := json.Unmarshal(first.Body.Bytes(), &firstPage); err != nil || first.Code != http.StatusOK {
		t.Fatalf("first page status=%d body=%s err=%v", first.Code, first.Body.String(), err)
	}
	if len(firstPage.Assets) != brandasset.HistoryPageSize || firstPage.NextBefore != 2 {
		t.Fatalf("first cursor page len=%d next=%d", len(firstPage.Assets), firstPage.NextBefore)
	}
	second := brandAssetRequest(t, h, shellTenant, http.MethodGet, PathBrandAssets+"?before=2", nil, "")
	var secondPage brandAssetHistoryPage
	if err := json.Unmarshal(second.Body.Bytes(), &secondPage); err != nil || second.Code != http.StatusOK {
		t.Fatalf("second page status=%d body=%s err=%v", second.Code, second.Body.String(), err)
	}
	if len(secondPage.Assets) != 1 || secondPage.Assets[0].Revision != 1 || secondPage.NextBefore != 0 {
		t.Fatalf("second cursor page = %+v", secondPage)
	}
	bad := brandAssetRequest(t, h, shellTenant, http.MethodGet, PathBrandAssets+"?before=invalid", nil, "")
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor status=%d body=%s", bad.Code, bad.Body.String())
	}
}
