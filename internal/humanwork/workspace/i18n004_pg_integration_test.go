package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/i18ncatalogstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/i18n"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// TestTodo_I18N_004_Integration exercises the served publication endpoint and
// page against PostgreSQL. It proves that a catalog activation changes the
// response of the already-running handler, survives a failed activation, and
// is recovered by a fresh handler without rebuilding the binary.
func TestTodo_I18N_004_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 350); err != nil {
		t.Fatalf("apply migrations through 00350: %v", err)
	}
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', $4)`,
		tenantID, shellTenant, "Catalog test tenant", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	storeConn := db.NewConn(t)
	if _, err := storeConn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("set catalog store role: %v", err)
	}
	store := i18ncatalogstore.New(storeConn, func(tenant string) uuid.UUID {
		if tenant == shellTenant {
			return tenantID
		}
		return uuid.Nil
	})
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: shellSigningKey, Issuer: shellIssuer, Audience: shellAudience,
		Now: func() time.Time { return shellNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := verifier.Issue(trust.Claims{
		Issuer: shellIssuer, Audience: shellAudience, Subject: "catalog-publisher", SubjectKind: "human", Tenant: shellTenant,
		OrganizationScopeID: "org-north-america", Roles: []string{productui.RoleHCMAdmin}, Purposes: []string{shellPurpose},
		AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "catalog-publisher-session",
		IssuedAtUnix: shellNow.Add(-time.Minute).Unix(), ExpiresAtUnix: shellNow.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	newHandler := func() *Handler {
		h, _ := newShellHandler(t, false)
		h.catalogs = store
		// Database activation timestamps use wall time. Keep credential
		// validation pinned to shellNow while reading the active revision at
		// the actual current instant.
		h.now = func() time.Time { return time.Now().UTC() }
		return h
	}
	h := newHandler()
	activationTime := time.Now().UTC().Add(-time.Minute)
	makeRevision := func(id, previous, text string) i18n.CatalogRevision {
		revision := i18n.CatalogRevision{
			ID: id, PreviousRevision: previous, Locale: "en-US", Version: id,
			CreatedAt: activationTime, EffectiveFrom: activationTime,
			Translations: []i18n.Translation{{
				Key: "page.home.title", Text: text, MeaningID: "page.home.title", Source: "reviewed/home-title",
				Classification: "UI", EffectiveFrom: activationTime,
			}},
		}
		revision.CanonicalDigest = i18n.DigestRevision(revision)
		revision.Digest = revision.CanonicalDigest
		return revision
	}
	publish := func(handler *Handler, revision i18n.CatalogRevision) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(revision)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "http://cell.test"+PathCatalogPublication, strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	serve := func(handler *Handler) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathProductHome, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("GET served product page = %d: %s", res.Code, res.Body.String())
		}
		return res.Body.String()
	}

	first := makeRevision("pg-served-r1", "", "First PostgreSQL reviewed title")
	if res := publish(h, first); res.Code != http.StatusCreated {
		t.Fatalf("POST first catalog = %d: %s", res.Code, res.Body.String())
	}
	firstPage := serve(h)
	if !strings.Contains(firstPage, "First PostgreSQL reviewed title") || !strings.Contains(firstPage, `data-hcm-catalog-revision="pg-served-r1"`) {
		t.Fatalf("served page lacks first activated revision: %s", firstPage)
	}

	// Publish stores an immutable candidate before activation. A bad predecessor
	// makes activation fail, while the prior event remains the served revision.
	failed := makeRevision("pg-served-failed", "not-the-active-revision", "Must not be served")
	if res := publish(h, failed); res.Code != http.StatusConflict {
		t.Fatalf("POST invalid activation = %d, want 409: %s", res.Code, res.Body.String())
	}
	afterFailure := serve(h)
	if !strings.Contains(afterFailure, "First PostgreSQL reviewed title") || strings.Contains(afterFailure, "Must not be served") || !strings.Contains(afterFailure, `data-hcm-catalog-revision="pg-served-r1"`) {
		t.Fatalf("failed activation changed serving catalog: %s", afterFailure)
	}

	second := makeRevision("pg-served-r2", first.ID, "Second PostgreSQL reviewed title")
	if res := publish(h, second); res.Code != http.StatusCreated {
		t.Fatalf("POST second catalog = %d: %s", res.Code, res.Body.String())
	}
	secondPage := serve(h)
	if !strings.Contains(secondPage, "Second PostgreSQL reviewed title") || strings.Contains(secondPage, "First PostgreSQL reviewed title") || !strings.Contains(secondPage, `data-hcm-catalog-revision="pg-served-r2"`) {
		t.Fatalf("running handler did not serve the new revision: %s", secondPage)
	}

	// A new handler and store connection recover the active PostgreSQL record.
	recoveredConn := db.NewConn(t)
	if _, err := recoveredConn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("set recovered catalog store role: %v", err)
	}
	recoveredStore := i18ncatalogstore.New(recoveredConn, func(tenant string) uuid.UUID {
		if tenant == shellTenant {
			return tenantID
		}
		return uuid.Nil
	})
	recovered := newHandler()
	recovered.catalogs = recoveredStore
	recoveredPage := serve(recovered)
	if !strings.Contains(recoveredPage, "Second PostgreSQL reviewed title") || !strings.Contains(recoveredPage, `data-hcm-catalog-revision="pg-served-r2"`) {
		t.Fatalf("fresh handler did not recover active revision: %s", recoveredPage)
	}
}
