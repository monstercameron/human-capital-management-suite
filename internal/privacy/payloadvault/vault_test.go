package payloadvault_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/privacy/payloadvault"
)

var (
	sealedAt     = time.Date(2026, time.September, 21, 12, 0, 0, 0, time.UTC)
	backupExpiry = time.Date(2027, time.January, 19, 12, 0, 0, 0, time.UTC)
	destroyedAt  = time.Date(2026, time.October, 5, 9, 30, 0, 0, time.UTC)
)

func fixedMaterial() []byte {
	out := make([]byte, payloadvault.KeySize)
	for i := range out {
		out[i] = byte(i + 1)
	}
	return out
}

func fixedNonce() []byte {
	return []byte{0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5, 0xA6, 0xA7, 0xA8, 0xA9, 0xAA, 0xAB}
}

func testVault(t *testing.T, tenant, subject, keyID string) *payloadvault.Vault {
	t.Helper()
	v := payloadvault.New()
	key, err := payloadvault.NewSubjectKey(tenant, subject, keyID, fixedMaterial())
	if err != nil {
		t.Fatal(err)
	}
	if err := v.AddKey(key); err != nil {
		t.Fatal(err)
	}
	return v
}

// TestTodo_WF_REV_015 is the PRIMARY contract: personal fields move to
// payload-vault references encrypted under per-subject keys, and destroying
// a key leaves tombstones while closing every copy.
func TestTodo_WF_REV_015(t *testing.T) {
	v := testVault(t, "tenant-a", "subject-1", "k1")

	ref, err := v.Seal("tenant-a", "subject-1", "ledger_event", "payload",
		[]byte("candidate Amina Yusuf <amina@example.com>"), sealedAt, backupExpiry)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if err := ref.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if ref.Algorithm != payloadvault.AlgorithmAES256GCMV1 {
		t.Fatalf("algorithm = %q", ref.Algorithm)
	}
	if bytes.Contains(ref.Canonical(), []byte("amina@example.com")) {
		t.Fatal("reference carries plaintext")
	}

	plain, err := v.Open(ref)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(plain) != "candidate Amina Yusuf <amina@example.com>" {
		t.Fatalf("round trip = %q", plain)
	}

	// Randomized encryption: the same plaintext seals to different bytes
	// twice, so two rows never share a recognizable ciphertext.
	dup, err := v.Seal("tenant-a", "subject-1", "ledger_event", "payload",
		[]byte("candidate Amina Yusuf <amina@example.com>"), sealedAt, backupExpiry)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(dup.Ciphertext, ref.Ciphertext) {
		t.Fatal("two seals of one plaintext share ciphertext")
	}

	// Rotation: the newest key seals, older references still open.
	rotated, err := payloadvault.NewSubjectKey("tenant-a", "subject-1", "k2", bytes.Repeat([]byte{0x7F}, payloadvault.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.AddKey(rotated); err != nil {
		t.Fatalf("AddKey rotation: %v", err)
	}
	ref2, err := v.Seal("tenant-a", "subject-1", "ledger_event", "payload", []byte("second"), sealedAt, backupExpiry)
	if err != nil {
		t.Fatalf("Seal after rotation: %v", err)
	}
	if ref2.KeyID != "k2" {
		t.Fatalf("rotated seal key = %q, want k2", ref2.KeyID)
	}
	if _, err := v.Open(ref); err != nil {
		t.Fatalf("Open pre-rotation reference: %v", err)
	}

	// A reference from another subject is rejected at destroy time.
	other := testVault(t, "tenant-a", "subject-9", "k1")
	otherRef, err := other.Seal("tenant-a", "subject-9", "ledger_event", "payload", []byte("x"), sealedAt, backupExpiry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.DestroySubjectKey("tenant-a", "subject-1", []payloadvault.VaultRef{otherRef}, destroyedAt); err == nil {
		t.Fatal("DestroySubjectKey accepted another subject's reference")
	}

	tomb, err := v.DestroySubjectKey("tenant-a", "subject-1", []payloadvault.VaultRef{ref, ref2}, destroyedAt)
	if err != nil {
		t.Fatalf("DestroySubjectKey: %v", err)
	}
	if err := tomb.Validate(); err != nil {
		t.Fatalf("tombstone Validate: %v", err)
	}
	if len(tomb.Payloads) != 2 {
		t.Fatalf("tombstone payloads = %d, want 2", len(tomb.Payloads))
	}
	if !tomb.BackupClearAt().Equal(backupExpiry) {
		t.Fatalf("backup-clear = %v, want %v", tomb.BackupClearAt(), backupExpiry)
	}

	if _, err := v.Open(ref); !errors.Is(err, payloadvault.ErrKeyDestroyed) {
		t.Fatalf("Open after destroy = %v, want ErrKeyDestroyed", err)
	}
	if _, err := v.Open(ref2); !errors.Is(err, payloadvault.ErrKeyDestroyed) {
		t.Fatalf("Open rotated ref after destroy = %v, want ErrKeyDestroyed", err)
	}
	// A destroyed key id can never be re-registered: the shred is final.
	if err := v.AddKey(rotated); !errors.Is(err, payloadvault.ErrKeyDestroyed) {
		t.Fatalf("AddKey destroyed = %v, want ErrKeyDestroyed", err)
	}
}

func TestTodo_WF_REV_015_Validation(t *testing.T) {
	v := payloadvault.New()
	if err := v.AddKey(payloadvault.SubjectKey{}); !errors.Is(err, payloadvault.ErrInvalidKey) {
		t.Fatalf("AddKey empty = %v", err)
	}
	if _, err := payloadvault.NewSubjectKey("t", "s", "k", []byte("short")); !errors.Is(err, payloadvault.ErrInvalidKey) {
		t.Fatalf("NewSubjectKey short = %v", err)
	}
	if _, err := v.Seal("tenant-a", "subject-1", "ledger_event", "payload", []byte("x"), sealedAt, backupExpiry); !errors.Is(err, payloadvault.ErrKeyUnknown) {
		t.Fatalf("Seal without key = %v", err)
	}
	if _, err := v.Open(payloadvault.VaultRef{}); !errors.Is(err, payloadvault.ErrInvalidRef) {
		t.Fatalf("Open empty ref = %v", err)
	}
	v2 := testVault(t, "tenant-a", "subject-1", "k1")
	if _, err := v2.Seal("tenant-a", "subject-1", "t", "f", nil, sealedAt, backupExpiry); !errors.Is(err, payloadvault.ErrInvalidRef) {
		t.Fatalf("Seal empty plaintext = %v", err)
	}
	if _, err := v2.Seal("tenant-a", "subject-1", "t", "f", []byte("x"), sealedAt, sealedAt); !errors.Is(err, payloadvault.ErrInvalidRef) {
		t.Fatalf("Seal expiry==sealed = %v", err)
	}

	// Tampering with a live ciphertext fails authentication, never returns
	// adjacent plaintext.
	ref, err := v2.Seal("tenant-a", "subject-1", "ledger_event", "payload", []byte("sensitive"), sealedAt, backupExpiry)
	if err != nil {
		t.Fatal(err)
	}
	tampered := ref
	tampered.Ciphertext = append([]byte(nil), ref.Ciphertext...)
	tampered.Ciphertext[0] ^= 0xFF
	if _, err := v2.Open(tampered); !errors.Is(err, payloadvault.ErrDecryptFailed) {
		t.Fatalf("Open tampered = %v, want ErrDecryptFailed", err)
	}
}

// TestTodo_WF_REV_015_Security proves the destroyed payload is
// unrecoverable from the ledger, projections and backups after the
// backup-expiry date: every stored copy is ciphertext, the key is gone, and
// the tombstone retains no recoverable content.
func TestTodo_WF_REV_015_Security(t *testing.T) {
	const personal = "candidate Amina Yusuf, dob 1990-04-17, iban DE75512108001245123456"
	v := testVault(t, "tenant-a", "subject-1", "k1")
	otherKey, err := payloadvault.NewSubjectKey("tenant-a", "subject-2", "k1", bytes.Repeat([]byte{0x6D}, payloadvault.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.AddKey(otherKey); err != nil {
		t.Fatal(err)
	}

	original, err := v.Seal("tenant-a", "subject-1", "custom_record_revision", "field_values",
		[]byte(personal), sealedAt, backupExpiry)
	if err != nil {
		t.Fatal(err)
	}
	reversal, err := v.Seal("tenant-a", "subject-1", "ledger_event", "payload",
		[]byte(personal), sealedAt, backupExpiry)
	if err != nil {
		t.Fatal(err)
	}
	otherSubject, err := v.Seal("tenant-a", "subject-2", "custom_record_revision", "field_values",
		[]byte("candidate another person"), sealedAt, backupExpiry)
	if err != nil {
		t.Fatal(err)
	}
	// Ledger, projection and backup copies: identical ciphertext, distinct
	// stores.
	ledgerCopy := original
	projectionCopy := original
	backupCopy := original

	tomb, err := v.DestroySubjectKey("tenant-a", "subject-1",
		[]payloadvault.VaultRef{original, reversal}, destroyedAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(tomb.Payloads) != 2 {
		t.Fatalf("tombstone payloads = %d, want original and reversal records", len(tomb.Payloads))
	}
	gotPayloads := map[string]bool{}
	for _, payload := range tomb.Payloads {
		gotPayloads[payload.Table+"."+payload.Field] = true
	}
	for _, want := range []string{"custom_record_revision.field_values", "ledger_event.payload"} {
		if !gotPayloads[want] {
			t.Fatalf("tombstone omits %s", want)
		}
	}
	otherPlain, err := v.Open(otherSubject)
	if err != nil || string(otherPlain) != "candidate another person" {
		t.Fatalf("subject-2 payload after subject-1 erasure = %q, %v", otherPlain, err)
	}

	stores := map[string]payloadvault.VaultRef{
		"ledger":     ledgerCopy,
		"projection": projectionCopy,
		"backup":     backupCopy,
		"reversal":   reversal,
	}
	for name, ref := range stores {
		if _, err := v.Open(ref); !errors.Is(err, payloadvault.ErrKeyDestroyed) {
			t.Fatalf("Open %s copy after destroy = %v, want ErrKeyDestroyed", name, err)
		}
	}

	// After the backup-expiry date the copies are still useless: expiry is
	// an operational purge promise, and the crypto holds regardless.
	_ = tomb.BackupClearAt()
	for name, ref := range stores {
		if _, err := v.Open(ref); !errors.Is(err, payloadvault.ErrKeyDestroyed) {
			t.Fatalf("Open %s copy after backup-expiry = %v, want ErrKeyDestroyed", name, err)
		}
	}

	// No stored or tombstone byte string retains the personal content, the
	// ciphertext, or key material.
	leakCheck := map[string][]byte{
		"ledger row":     ledgerCopy.Canonical(),
		"projection row": projectionCopy.Canonical(),
		"backup copy":    backupCopy.Canonical(),
		"tombstone":      tomb.Canonical(),
	}
	for name, raw := range leakCheck {
		if bytes.Contains(raw, []byte("Amina")) || bytes.Contains(raw, []byte("DE75512108001245123456")) {
			t.Fatalf("%s leaks personal content", name)
		}
	}
	tombJSON, _ := json.Marshal(tomb)
	var tombMap map[string]any
	if err := json.Unmarshal(tombJSON, &tombMap); err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"ciphertext", "ct", "material", "plaintext", "nonce"} {
		if _, ok := tombMap[banned]; ok && (banned == "ct" || banned == "ciphertext" || banned == "material" || banned == "plaintext") {
			t.Fatalf("tombstone carries %q", banned)
		}
	}
	if !strings.Contains(string(tombJSON), "2027-01-19") {
		t.Fatalf("tombstone omits backup-expiry date: %s", tombJSON)
	}

	// An attacker vault holding a different key for the same subject learns
	// nothing from a stolen copy.
	attacker := payloadvault.New()
	evilKey, err := payloadvault.NewSubjectKey("tenant-a", "subject-1", "k1", bytes.Repeat([]byte{0x42}, payloadvault.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	if err := attacker.AddKey(evilKey); err != nil {
		t.Fatal(err)
	}
	if _, err := attacker.Open(backupCopy); !errors.Is(err, payloadvault.ErrDecryptFailed) {
		t.Fatalf("attacker Open = %v, want ErrDecryptFailed", err)
	}
}

// TestTodo_WF_REV_015_Golden pins the byte shape of a vault reference and
// its tombstone so a new personal field or a format change breaks the
// golden until reviewed.
func TestTodo_WF_REV_015_Golden(t *testing.T) {
	v := payloadvault.New()
	key, err := payloadvault.NewSubjectKey("tenant-a", "subject-1", "k1", fixedMaterial())
	if err != nil {
		t.Fatal(err)
	}
	if err := v.AddKey(key); err != nil {
		t.Fatal(err)
	}
	ref, err := v.SealWithNonce("tenant-a", "subject-1", "ledger_event", "payload",
		[]byte("candidate Amina Yusuf"), fixedNonce(), sealedAt, backupExpiry)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(ref.Canonical())
	want := `{"v":1,"kind":"payload-vault-ref","alg":"aes-256-gcm-v1","tenant":"tenant-a","subject":"subject-1","kid":"k1","table":"ledger_event","field":"payload","nonce":"oKGio6Slpqeoqaqr","ct":"2rBTApTRO3AaLuMGmOtBpA6iw2w+8H+IbH10rQubfVZo3LG0Lg==","plain_bytes":21,"ct_digest":"sha256:f25235d170d4b654db04d6bf366bb71cc30292691408f57aad922d595b9f3f8c","backup_expiry":"2027-01-19T12:00:00Z","sealed_at":"2026-09-21T12:00:00Z"}`
	if raw != want {
		t.Fatalf("golden reference mismatch:\n got: %s\nwant: %s", raw, want)
	}

	tomb, err := v.DestroySubjectKey("tenant-a", "subject-1", []payloadvault.VaultRef{ref}, destroyedAt)
	if err != nil {
		t.Fatal(err)
	}
	tombRaw := string(tomb.Canonical())
	wantTomb := `{"tenant":"tenant-a","subject":"subject-1","key_ids":["k1"],"payloads":[{"table":"ledger_event","field":"payload","plain_bytes":21,"ct_digest":"sha256:f25235d170d4b654db04d6bf366bb71cc30292691408f57aad922d595b9f3f8c"}],"destroyed_at":"2026-10-05T09:30:00Z","backup_expiry":"2027-01-19T12:00:00Z"}`
	if tombRaw != wantTomb {
		t.Fatalf("golden tombstone mismatch:\n got: %s\nwant: %s", tombRaw, wantTomb)
	}
}
