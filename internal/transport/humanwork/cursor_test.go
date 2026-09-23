package humanwork

import (
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	rotationActiveKey   = []byte("work-queue-active-key-0000000000")
	rotationPreviousKey = []byte("work-queue-previous-key-00000000")
	rotationDevKey      = []byte("development-hmac-key-000000000000")
)

func rotationPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(transporttest.Tenant), Subject: transporttest.Subject, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session-rotation", IssuedAt: workNow.Add(-time.Minute), ExpiresAt: workNow.Add(time.Hour),
		CredentialDigest: "credential-digest",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

func rotationItems() []workitem.WorkItem {
	return []workitem.WorkItem{{WorkItemID: workTenantUUID(), ItemVersion: 1}}
}

// TestQueueCursorRotatesWithoutBreakingInflightPages is INTAPI-006's RED
// for the shared-signing-key defect: page cursors were HMAC'd with the
// development credential key, so rotating either meant rotating both, and
// there was no retired-key acceptance at all. A dedicated key mints;
// the retired key still verifies until in-flight pages drain; a cursor
// from any other key (including the dev HMAC key) never verifies.
func TestQueueCursorRotatesWithoutBreakingInflightPages(t *testing.T) {
	p := rotationPrincipal(t)
	items := rotationItems()
	now := workNow

	mint := func(key []byte) string {
		token, err := encodeQueueCursor(queueCursor{
			Principal: p.Subject(), Tenant: "acme-corp",
			Snapshot: queueDigest(items), Index: 1, Version: cursorVersion,
			ExpiresAt: now.Add(cursorTTL).Unix(), Nonce: "nonce-1",
		}, key)
		if err != nil {
			t.Fatalf("encodeQueueCursor: %v", err)
		}
		return token
	}
	decode := func(token string, active, previous []byte) (int, error) {
		return decodeQueueCursor(&commonv1.PageRequest{Cursor: token}, active, previous, p, items, now)
	}

	preRotation := mint(rotationPreviousKey)

	// The retired key verifies on a rotated server.
	if got, err := decode(preRotation, rotationActiveKey, rotationPreviousKey); err != nil || got != 1 {
		t.Fatalf("previous-key cursor after rotation = %d, %v; want index 1 verified", got, err)
	}
	// Without the retired key it does not: rotation is acceptance, not
	// absence of verification.
	if _, err := decode(preRotation, rotationActiveKey, nil); err == nil {
		t.Fatal("previous-key cursor verified without the retired key")
	}
	// New cursors mint under the active key and verify under it alone.
	postRotation := mint(rotationActiveKey)
	if got, err := decode(postRotation, rotationActiveKey, nil); err != nil || got != 1 {
		t.Fatalf("active-key cursor = %d, %v; want index 1 verified", got, err)
	}
	if _, err := decode(postRotation, rotationPreviousKey, nil); err == nil {
		t.Fatal("active-key cursor verified under the retired key alone")
	}
	// A cursor signed with the development HMAC key is foreign to the
	// dedicated page-cursor key, before and after rotation.
	devSigned := mint(rotationDevKey)
	if _, err := decode(devSigned, rotationActiveKey, rotationPreviousKey); err == nil {
		t.Fatal("dev-key cursor verified against the dedicated page-cursor keys")
	}
}
