package workspace

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func uxauditK2UploadBody(t *testing.T, name string, body []byte) (*bytes.Buffer, string) {
	t.Helper()
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	part, err := writer.CreateFormFile(brandAssetFormField, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &form, writer.FormDataContentType()
}

// TestTodo_UXAUDIT_022_Integration proves that the served picker crosses the
// upload, responsive-proxy and tenant-scoped read boundary with the same
// content-derived identity.
func TestTodo_UXAUDIT_022_Integration(t *testing.T) {
	h, _ := newShellHandler(t, false)
	repository := newHTTPBrandAssets()
	h.brandAssets = repository
	form, contentType := uxauditK2UploadBody(t, "tenant-logo.png", brandPNG(t, 640, 240))
	upload := brandAssetRequest(t, h, shellTenant, http.MethodPost, PathBrandAssets, form, contentType)
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", upload.Code, upload.Body.String())
	}
	var result brandAssetUploadResponse
	if err := json.Unmarshal(upload.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Digest == "" || result.URL != PathBrandAssetPrefix+result.Digest || result.Revision != 1 {
		t.Fatalf("upload result=%+v", result)
	}

	proxy := brandAssetRequest(t, h, shellTenant, http.MethodGet, result.URL+"?variant=proxy", nil, "")
	if proxy.Code != http.StatusOK || proxy.Header().Get("Content-Type") != "image/jpeg" || proxy.Body.Len() == 0 {
		t.Fatalf("proxy response status=%d type=%q bytes=%d", proxy.Code, proxy.Header().Get("Content-Type"), proxy.Body.Len())
	}
	if proxy.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("proxy response is missing nosniff protection")
	}

	history := brandAssetRequest(t, h, shellTenant, http.MethodGet, PathBrandAssets, nil, "")
	if history.Code != http.StatusOK || strings.Contains(history.Body.String(), `"original"`) || strings.Contains(history.Body.String(), `"proxy"`) {
		t.Fatalf("history exposed stored bytes: status=%d body=%s", history.Code, history.Body.String())
	}
	otherTenant := brandAssetRequest(t, h, "another-tenant", http.MethodGet, result.URL, nil, "")
	if otherTenant.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant asset read status=%d body=%s", otherTenant.Code, otherTenant.Body.String())
	}
}

// TestTodo_UXAUDIT_022_Security proves the asset boundary rejects forged path
// and digest inputs before any bytes are served.
func TestTodo_UXAUDIT_022_Security(t *testing.T) {
	if _, err := ValidateBrandAsset(BrandAssetUpload{TenantID: shellTenant, Name: "../logo.png", Bytes: brandPNG(t, 64, 64)}); err != ErrBrandAssetUnsafe {
		t.Fatalf("path traversal validation error=%v", err)
	}
	h, _ := newShellHandler(t, false)
	h.brandAssets = newHTTPBrandAssets()
	request := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathBrandAssetPrefix+strings.Repeat("f", 63)+"g", nil)
	request.Header.Set("Authorization", "Bearer "+brandAssetHTTPToken(t, shellTenant))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("forged digest status=%d body=%s", response.Code, response.Body.String())
	}

	unauthenticated := httptest.NewRecorder()
	h.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "http://cell.test"+PathBrandAssets, nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated asset history status=%d", unauthenticated.Code)
	}
}
