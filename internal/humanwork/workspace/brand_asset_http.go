package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/brandasset"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const (
	PathBrandAssets         = "/workspace/brand-assets"
	PathBrandAssetLifecycle = "/workspace/brand-assets/lifecycle"
	PathBrandAssetPrefix    = "/workspace/brand-assets/"
	brandAssetFormField     = "asset"
)

type brandAssetUploadResponse struct {
	URL      string `json:"url"`
	Digest   string `json:"digest"`
	Revision int    `json:"revision"`
	Name     string `json:"name"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type brandAssetHistoryResponse struct {
	Revision     int    `json:"revision"`
	HeadRevision int    `json:"head_revision"`
	Digest       string `json:"digest"`
	Name         string `json:"name"`
	URL          string `json:"url,omitempty"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Removed      bool   `json:"removed"`
	CanRollback  bool   `json:"can_rollback"`
	CanRemove    bool   `json:"can_remove"`
}

type brandAssetHistoryPage struct {
	Assets     []brandAssetHistoryResponse `json:"assets"`
	NextBefore int                         `json:"next_before,omitempty"`
}

func (h *Handler) listBrandAssets(w http.ResponseWriter, r *http.Request) {
	admitted, ok := h.admit(w, r)
	if !ok {
		return
	}
	principal, _ := trust.FromContext(admitted.Context())
	if principal == nil || h.brandAssets == nil {
		http.Error(w, "asset library unavailable", http.StatusServiceUnavailable)
		return
	}
	if !h.brandAssetMayView(w, admitted) {
		return
	}
	before := 0
	if raw := r.URL.Query().Get("before"); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 1 {
			http.Error(w, "invalid history cursor", http.StatusBadRequest)
			return
		}
		before = parsed
	}
	history, hasMore, err := h.brandAssets.History(r.Context(), string(principal.Tenant()), before)
	if err != nil {
		http.Error(w, "asset history unavailable", http.StatusInternalServerError)
		return
	}
	response := brandAssetHistoryPage{Assets: make([]brandAssetHistoryResponse, 0, len(history))}
	current, hasCurrent, currentErr := h.brandAssets.Current(r.Context(), string(principal.Tenant()))
	if currentErr != nil {
		http.Error(w, "asset history unavailable", http.StatusInternalServerError)
		return
	}
	for _, asset := range history {
		item := brandAssetHistoryResponse{Revision: asset.Revision, HeadRevision: current.Revision, Digest: asset.Digest, Name: asset.Name, Width: asset.Width, Height: asset.Height, Removed: asset.Removed, CanRollback: hasCurrent && !asset.Removed && asset.Revision < current.Revision, CanRemove: hasCurrent && !current.Removed && !asset.Removed && asset.Revision == current.Revision}
		if !asset.Removed && asset.Available && len(asset.Digest) == 64 {
			item.URL = PathBrandAssetPrefix + asset.Digest
		}
		response.Assets = append(response.Assets, item)
	}
	if hasMore && len(history) > 0 {
		response.NextBefore = history[len(history)-1].Revision
	}
	writeBrandAssetJSON(w, http.StatusOK, response)
}

func (h *Handler) uploadBrandAsset(w http.ResponseWriter, r *http.Request) {
	admitted, ok := h.admit(w, r)
	if !ok {
		return
	}
	if !h.brandAssetMayUpdate(w, admitted) {
		return
	}
	principal, _ := trust.FromContext(admitted.Context())
	if h.brandAssets == nil {
		http.Error(w, "brand asset library unavailable", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, BrandAssetMaxBytes+(64<<10))
	if err := r.ParseMultipartForm(BrandAssetMaxBytes + (64 << 10)); err != nil {
		http.Error(w, "upload must be one image smaller than 2 MB", http.StatusRequestEntityTooLarge)
		return
	}
	file, header, err := r.FormFile(brandAssetFormField)
	if err != nil {
		http.Error(w, "choose one image to upload", http.StatusBadRequest)
		return
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, BrandAssetMaxBytes+1))
	if err != nil || len(body) > BrandAssetMaxBytes {
		http.Error(w, "upload must be smaller than 2 MB", http.StatusRequestEntityTooLarge)
		return
	}
	select {
	case h.brandAssetUploadSlots <- struct{}{}:
		defer func() { <-h.brandAssetUploadSlots }()
	case <-r.Context().Done():
		return
	}
	tenant := string(principal.Tenant())
	asset, err := ValidateBrandAsset(BrandAssetUpload{TenantID: tenant, Name: header.Filename, Bytes: body})
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
		return
	}
	proxy, proxyType, err := brandAssetProxy(body, asset.MediaType)
	if err != nil {
		http.Error(w, "image could not be prepared safely", http.StatusUnsupportedMediaType)
		return
	}
	expected := 0
	current, exists, err := h.brandAssets.Current(r.Context(), tenant)
	if err != nil {
		http.Error(w, "asset library could not be read", http.StatusInternalServerError)
		return
	}
	if exists {
		expected = current.Revision
	}
	if supplied := r.Header.Get("X-HCM-Asset-Revision"); supplied != "" {
		parsed, parseErr := strconv.Atoi(supplied)
		if parseErr != nil || parsed < 0 || parsed != expected {
			http.Error(w, "asset library changed; reload and try again", http.StatusConflict)
			return
		}
		expected = parsed
	}
	stored, err := h.brandAssets.Save(r.Context(), tenant, expected, principal.Subject(), brandasset.Asset{
		Name: asset.Name, MediaType: asset.MediaType, Width: asset.Width, Height: asset.Height,
		Digest: asset.Digest, Original: body, Proxy: proxy, ProxyType: proxyType,
	})
	if errors.Is(err, ErrBrandAssetVersionConflict) {
		http.Error(w, "asset library changed; reload and try again", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "asset could not be saved", http.StatusInternalServerError)
		return
	}
	writeBrandAssetJSON(w, http.StatusCreated, brandAssetUploadResponse{URL: PathBrandAssetPrefix + stored.Digest, Digest: stored.Digest, Revision: stored.Revision, Name: stored.Name, Width: stored.Width, Height: stored.Height})
}

func (h *Handler) changeBrandAsset(w http.ResponseWriter, r *http.Request) {
	admitted, ok := h.admit(w, r)
	if !ok {
		return
	}
	if !h.brandAssetMayUpdate(w, admitted) {
		return
	}
	principal, _ := trust.FromContext(admitted.Context())
	if h.brandAssets == nil {
		http.Error(w, "brand asset library unavailable", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var request struct {
		Action   string `json:"action"`
		Expected *int   `json:"expected_revision"`
		Revision int    `json:"revision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid asset action", http.StatusBadRequest)
		return
	}
	tenant := string(principal.Tenant())
	if request.Expected == nil || *request.Expected < 0 {
		http.Error(w, "expected asset revision is required", http.StatusBadRequest)
		return
	}
	expected := *request.Expected
	var asset brandasset.Asset
	var err error
	switch request.Action {
	case "remove":
		asset, err = h.brandAssets.Remove(r.Context(), tenant, expected, principal.Subject())
	case "rollback":
		asset, err = h.brandAssets.Rollback(r.Context(), tenant, expected, request.Revision, principal.Subject())
	default:
		http.Error(w, "unknown asset action", http.StatusBadRequest)
		return
	}
	if errors.Is(err, ErrBrandAssetVersionConflict) {
		http.Error(w, "asset library changed; reload and try again", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "asset action could not be applied", http.StatusInternalServerError)
		return
	}
	url := ""
	if !asset.Removed {
		url = PathBrandAssetPrefix + asset.Digest
	}
	writeBrandAssetJSON(w, http.StatusOK, brandAssetUploadResponse{URL: url, Digest: asset.Digest, Revision: asset.Revision, Name: asset.Name, Width: asset.Width, Height: asset.Height})
}

func (h *Handler) serveBrandAsset(w http.ResponseWriter, r *http.Request) {
	admitted, ok := h.admit(w, r)
	if !ok {
		return
	}
	principal, _ := trust.FromContext(admitted.Context())
	if h.brandAssets == nil {
		http.NotFound(w, r)
		return
	}
	digest := r.PathValue("digest")
	if len(digest) != 64 || strings.Trim(digest, "0123456789abcdef") != "" {
		http.NotFound(w, r)
		return
	}
	asset, found, err := h.brandAssets.Read(r.Context(), string(principal.Tenant()), digest)
	if err != nil || !found {
		http.NotFound(w, r)
		return
	}
	body, contentType := asset.Original, asset.MediaType
	if r.URL.Query().Get("variant") == "proxy" {
		body, contentType = asset.Proxy, asset.ProxyType
	} else if r.URL.Query().Get("variant") != "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *Handler) brandAssetMayUpdate(w http.ResponseWriter, admitted *http.Request) bool {
	principal, _ := trust.FromContext(admitted.Context())
	if principal == nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return false
	}
	access, err := h.resolveProductAccess(admitted.Context(), principal)
	if err != nil {
		http.Error(w, "authorization unavailable", http.StatusServiceUnavailable)
		return false
	}
	if len(access.permissions) == 0 && !access.configured {
		return true
	}
	if access.can(productui.PageAppearance, roleaccess.ActionUpdate) {
		return true
	}
	http.Error(w, "appearance update is not authorized", http.StatusForbidden)
	return false
}

func (h *Handler) brandAssetMayView(w http.ResponseWriter, admitted *http.Request) bool {
	principal, _ := trust.FromContext(admitted.Context())
	if principal == nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return false
	}
	access, err := h.resolveProductAccess(admitted.Context(), principal)
	if err != nil {
		http.Error(w, "authorization unavailable", http.StatusServiceUnavailable)
		return false
	}
	if len(access.permissions) == 0 && !access.configured {
		return true
	}
	if access.can(productui.PageAppearance, roleaccess.ActionView) {
		return true
	}
	http.Error(w, "appearance view is not authorized", http.StatusForbidden)
	return false
}

func writeBrandAssetJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func brandAssetProxy(body []byte, mediaType string) ([]byte, string, error) {
	if mediaType != "image/png" && mediaType != "image/jpeg" && mediaType != "image/webp" {
		return nil, "", ErrBrandAssetInvalidType
	}
	decoded, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	source := decoded.Bounds()
	width, height := source.Dx(), source.Dy()
	if width > 512 || height > 512 {
		ratio := min(512.0/float64(width), 512.0/float64(height))
		width, height = max(1, int(float64(width)*ratio)), max(1, int(float64(height)*ratio))
	}
	proxy := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			rgba := color.NRGBAModel.Convert(decoded.At(source.Min.X+x*source.Dx()/width, source.Min.Y+y*source.Dy()/height)).(color.NRGBA)
			proxy.Set(x, y, rgba)
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, proxy, &jpeg.Options{Quality: 85}); err != nil {
		return nil, "", err
	}
	return encoded.Bytes(), "image/jpeg", nil
}
