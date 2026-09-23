// Package payloadvault implements the WF-REV-015 crypto core: personal
// fields flagged by WF-REV-014 (tools/policy/storeprivacy) move out of
// PERMANENT rows into vault references encrypted under per-subject keys,
// so destroying a subject's key crypto-shreds every copy at once and the
// ledger keeps only a minimal tombstone.
//
// The package is kernel-pure: no database, no clock, no network, no new
// dependencies (stdlib AES-256-GCM only, following
// internal/cryptoagility). Times arrive as parameters, exactly like
// cryptoagility's migration receipts. State lives on the Vault value that
// owns it; there is no package-level registry.
//
// What this package does NOT do (remainder, owned by later work): persist
// keys or references, wire references into ledger/customstore writes,
// record FIELD_LEVEL rows in the storage-disposition registry, or purge
// operational backups. It proves the crypto property those layers rely on:
// ciphertext without its subject key reveals nothing, before or after the
// backup-expiry date.
package payloadvault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

const (
	// SchemaVersion changes only when the reference shape changes.
	SchemaVersion = 1
	// AlgorithmAES256GCMV1 is the only write algorithm. Reads accept the
	// same identifier; rotation introduces a new identifier, never a
	// silent parameter change.
	AlgorithmAES256GCMV1 = "aes-256-gcm-v1"
	// KeySize is the AES-256 key size in bytes.
	KeySize = 32
	// ReferenceKind marks the inline string form stored in a PERMANENT row
	// where the personal payload used to sit.
	ReferenceKind = "payload-vault-ref"
)

var (
	// ErrInvalidRef is matched when a vault reference is malformed.
	ErrInvalidRef = errors.New("payloadvault: invalid vault reference")
	// ErrInvalidKey is matched when a subject key is malformed.
	ErrInvalidKey = errors.New("payloadvault: invalid subject key")
	// ErrKeyUnknown is matched when no live or destroyed key names the
	// reference's key id.
	ErrKeyUnknown = errors.New("payloadvault: unknown subject key")
	// ErrKeyDestroyed is matched when the reference's subject key was
	// destroyed. Callers must leave the original and reversal facts as
	// minimal tombstones and must not retry with another key.
	ErrKeyDestroyed = errors.New("payloadvault: subject key destroyed")
	// ErrDecryptFailed is matched when authentication fails under a known
	// live key (wrong key, tampered nonce or ciphertext).
	ErrDecryptFailed = errors.New("payloadvault: vault payload unrecoverable")
)

// SubjectKey is one per-subject data key. Material never leaves this value
// except inside Seal/Open calls; it is never serialized with a VaultRef.
type SubjectKey struct {
	Tenant   string
	Subject  string
	KeyID    string
	Material [KeySize]byte
}

// GenerateSubjectKey mints a fresh random per-subject key. Randomness is the
// only impurity; callers needing determinism (golden tests) build
// SubjectKey through NewSubjectKey with fixed material.
func GenerateSubjectKey(tenant, subject, keyID string) (SubjectKey, error) {
	var material [KeySize]byte
	if _, err := rand.Read(material[:]); err != nil {
		return SubjectKey{}, fmt.Errorf("payloadvault: generate subject key: %w", err)
	}
	return NewSubjectKey(tenant, subject, keyID, material[:])
}

// NewSubjectKey builds a subject key from explicit material. It exists so
// tests and reviewed key-custody flows control key bytes; production uses
// GenerateSubjectKey.
func NewSubjectKey(tenant, subject, keyID string, material []byte) (SubjectKey, error) {
	if tenant == "" || subject == "" || keyID == "" {
		return SubjectKey{}, fmt.Errorf("%w: tenant, subject and key id are required", ErrInvalidKey)
	}
	if len(material) != KeySize {
		return SubjectKey{}, fmt.Errorf("%w: want %d key bytes, got %d", ErrInvalidKey, KeySize, len(material))
	}
	var fixed [KeySize]byte
	copy(fixed[:], material)
	return SubjectKey{Tenant: tenant, Subject: subject, KeyID: keyID, Material: fixed}, nil
}

// scope names the subject whose erasure destroys every key under it.
func (k SubjectKey) scope() string { return k.Tenant + "\x00" + k.Subject }

// id names one key generation inside a scope.
func (k SubjectKey) id() string { return k.scope() + "\x00" + k.KeyID }

// VaultRef is what a PERMANENT row stores where the personal payload sat:
// an opaque, authenticated ciphertext plus audit metadata. It carries no
// plaintext and no key material.
type VaultRef struct {
	Version      int       `json:"v"`
	Kind         string    `json:"kind"`
	Algorithm    string    `json:"alg"`
	Tenant       string    `json:"tenant"`
	Subject      string    `json:"subject"`
	KeyID        string    `json:"kid"`
	Table        string    `json:"table"`
	Field        string    `json:"field"`
	Nonce        []byte    `json:"nonce"`
	Ciphertext   []byte    `json:"ct"`
	PlainBytes   int       `json:"plain_bytes"`
	CipherDigest string    `json:"ct_digest"`
	BackupExpiry time.Time `json:"backup_expiry"`
	SealedAt     time.Time `json:"sealed_at"`
}

// Validate reports whether the reference is structurally sound and names
// the supported algorithm. It does not attempt decryption.
func (r VaultRef) Validate() error {
	if r.Version != SchemaVersion {
		return fmt.Errorf("%w: version %d", ErrInvalidRef, r.Version)
	}
	if r.Kind != ReferenceKind {
		return fmt.Errorf("%w: kind %q", ErrInvalidRef, r.Kind)
	}
	if r.Algorithm != AlgorithmAES256GCMV1 {
		return fmt.Errorf("%w: algorithm %q", ErrInvalidRef, r.Algorithm)
	}
	if r.Tenant == "" || r.Subject == "" || r.KeyID == "" {
		return fmt.Errorf("%w: tenant, subject and key id are required", ErrInvalidRef)
	}
	if r.Table == "" || r.Field == "" {
		return fmt.Errorf("%w: table and field are required", ErrInvalidRef)
	}
	if len(r.Nonce) == 0 || len(r.Ciphertext) == 0 {
		return fmt.Errorf("%w: nonce and ciphertext are required", ErrInvalidRef)
	}
	if r.PlainBytes < 0 {
		return fmt.Errorf("%w: negative plain size", ErrInvalidRef)
	}
	if r.CipherDigest == "" {
		return fmt.Errorf("%w: ciphertext digest is required", ErrInvalidRef)
	}
	if r.SealedAt.IsZero() || r.BackupExpiry.IsZero() {
		return fmt.Errorf("%w: sealed-at and backup-expiry are required", ErrInvalidRef)
	}
	if !r.BackupExpiry.After(r.SealedAt) {
		return fmt.Errorf("%w: backup-expiry must be after sealed-at", ErrInvalidRef)
	}
	return nil
}

// Canonical returns the deterministic byte encoding pinned by the golden
// test. Map iteration is excluded by construction: the struct marshals in
// field order.
func (r VaultRef) Canonical() []byte {
	raw, err := json.Marshal(r)
	if err != nil {
		return nil
	}
	return raw
}

// Digest is a stable identity for the reference shape and ciphertext.
func (r VaultRef) Digest() string {
	sum := sha256.Sum256(r.Canonical())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// PayloadRecord is the tombstone's per-field memory: where the destroyed
// payload lived and how to recognize its ciphertext copies, without the
// payload itself.
type PayloadRecord struct {
	Table        string `json:"table"`
	Field        string `json:"field"`
	PlainBytes   int    `json:"plain_bytes"`
	CipherDigest string `json:"ct_digest"`
}

// Tombstone is the minimally necessary fact the ledger keeps after key
// destruction: whose data, which keys, which fields, when, and the date
// after which deleted data cannot reappear from backups. It carries no
// plaintext, no ciphertext and no key material.
type Tombstone struct {
	Tenant       string          `json:"tenant"`
	Subject      string          `json:"subject"`
	KeyIDs       []string        `json:"key_ids"`
	Payloads     []PayloadRecord `json:"payloads"`
	DestroyedAt  time.Time       `json:"destroyed_at"`
	BackupExpiry time.Time       `json:"backup_expiry"`
}

// Validate reports whether the tombstone is structurally complete.
func (t Tombstone) Validate() error {
	if t.Tenant == "" || t.Subject == "" {
		return errors.New("payloadvault: tombstone requires tenant and subject")
	}
	if len(t.KeyIDs) == 0 {
		return errors.New("payloadvault: tombstone requires at least one key id")
	}
	if t.DestroyedAt.IsZero() || t.BackupExpiry.IsZero() {
		return errors.New("payloadvault: tombstone requires destroyed-at and backup-expiry")
	}
	return nil
}

// BackupClearAt returns the date after which the destroyed payload cannot
// reappear: restores must reapply this tombstone before service, per the
// secure-deletion contract.
func (t Tombstone) BackupClearAt() time.Time { return t.BackupExpiry }

// Canonical returns the deterministic byte encoding of the tombstone.
func (t Tombstone) Canonical() []byte {
	raw, err := json.Marshal(t)
	if err != nil {
		return nil
	}
	return raw
}

// Vault holds the live per-subject keys for one execution scope. It is the
// only state in this package; callers own it and pass it where sealing or
// opening is needed.
type Vault struct {
	live      map[string]*SubjectKey
	destroyed map[string]bool
	order     []string
}

// New returns an empty vault.
func New() *Vault {
	return &Vault{live: map[string]*SubjectKey{}, destroyed: map[string]bool{}}
}

// AddKey registers a subject key. Re-adding the same key id is rejected so
// a rotation can never silently shadow the key a reference names.
func (v *Vault) AddKey(key SubjectKey) error {
	if v == nil {
		return errors.New("payloadvault: vault is required")
	}
	if key.Tenant == "" || key.Subject == "" || key.KeyID == "" {
		return fmt.Errorf("%w: tenant, subject and key id are required", ErrInvalidKey)
	}
	var zero [KeySize]byte
	if subtle.ConstantTimeCompare(key.Material[:], zero[:]) == 1 {
		return fmt.Errorf("%w: key material is zero", ErrInvalidKey)
	}
	id := key.id()
	if _, ok := v.live[id]; ok {
		return fmt.Errorf("%w: key %s already registered", ErrInvalidKey, key.KeyID)
	}
	if v.destroyed[id] {
		return fmt.Errorf("%w: key %s was destroyed", ErrKeyDestroyed, key.KeyID)
	}
	held := key
	v.live[id] = &held
	v.order = append(v.order, id)
	return nil
}

// currentKey returns the most recently added live key for a scope.
func (v *Vault) currentKey(tenant, subject string) (SubjectKey, error) {
	prefix := tenant + "\x00" + subject + "\x00"
	for i := len(v.order) - 1; i >= 0; i-- {
		id := v.order[i]
		if len(id) <= len(prefix) || id[:len(prefix)] != prefix {
			continue
		}
		if key, ok := v.live[id]; ok && key != nil {
			return *key, nil
		}
	}
	return SubjectKey{}, fmt.Errorf("%w: no live key for subject", ErrKeyUnknown)
}

// Seal encrypts one personal field value under the subject's current key
// and returns the reference the PERMANENT row stores. at and backupExpiry
// are caller-supplied: the vault takes no clock. backupExpiry must be after
// at and names the date after which backups cannot reintroduce the payload.
func (v *Vault) Seal(tenant, subject, table, field string, plaintext []byte, at, backupExpiry time.Time) (VaultRef, error) {
	nonce := make([]byte, nonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return VaultRef{}, fmt.Errorf("payloadvault: generate nonce: %w", err)
	}
	return v.SealWithNonce(tenant, subject, table, field, plaintext, nonce, at, backupExpiry)
}

// SealWithNonce is Seal with caller-supplied nonce bytes. Production uses
// Seal; tests use this for byte-pinned golden vectors.
func (v *Vault) SealWithNonce(tenant, subject, table, field string, plaintext, nonce []byte, at, backupExpiry time.Time) (VaultRef, error) {
	if v == nil {
		return VaultRef{}, errors.New("payloadvault: vault is required")
	}
	if tenant == "" || subject == "" || table == "" || field == "" {
		return VaultRef{}, fmt.Errorf("%w: tenant, subject, table and field are required", ErrInvalidRef)
	}
	if len(plaintext) == 0 {
		return VaultRef{}, fmt.Errorf("%w: plaintext is empty", ErrInvalidRef)
	}
	if at.IsZero() || backupExpiry.IsZero() {
		return VaultRef{}, fmt.Errorf("%w: sealed-at and backup-expiry are required", ErrInvalidRef)
	}
	if !backupExpiry.After(at) {
		return VaultRef{}, fmt.Errorf("%w: backup-expiry must be after sealed-at", ErrInvalidRef)
	}
	key, err := v.currentKey(tenant, subject)
	if err != nil {
		return VaultRef{}, err
	}
	if key.Tenant != tenant || key.Subject != subject {
		return VaultRef{}, ErrKeyUnknown
	}
	gcm, err := newGCM(key.Material[:])
	if err != nil {
		return VaultRef{}, err
	}
	if len(nonce) != gcm.NonceSize() {
		return VaultRef{}, fmt.Errorf("%w: want %d nonce bytes, got %d", ErrInvalidRef, gcm.NonceSize(), len(nonce))
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	sum := sha256.Sum256(ciphertext)
	ref := VaultRef{
		Version:      SchemaVersion,
		Kind:         ReferenceKind,
		Algorithm:    AlgorithmAES256GCMV1,
		Tenant:       tenant,
		Subject:      subject,
		KeyID:        key.KeyID,
		Table:        table,
		Field:        field,
		Nonce:        append([]byte(nil), nonce...),
		Ciphertext:   ciphertext,
		PlainBytes:   len(plaintext),
		CipherDigest: "sha256:" + hex.EncodeToString(sum[:]),
		BackupExpiry: backupExpiry.UTC(),
		SealedAt:     at.UTC(),
	}
	if err := ref.Validate(); err != nil {
		return VaultRef{}, err
	}
	return ref, nil
}

// Open authenticates and decrypts a reference under the matching live key.
// A destroyed subject key yields ErrKeyDestroyed even when a ciphertext
// copy (ledger row, projection, backup) is intact: the copies are useless
// without the key, which is the crypto-shredding property.
func (v *Vault) Open(ref VaultRef) ([]byte, error) {
	if v == nil {
		return nil, errors.New("payloadvault: vault is required")
	}
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	id := ref.Tenant + "\x00" + ref.Subject + "\x00" + ref.KeyID
	key, ok := v.live[id]
	if !ok {
		if v.destroyed[id] {
			return nil, fmt.Errorf("%w: subject %s key %s", ErrKeyDestroyed, ref.Subject, ref.KeyID)
		}
		return nil, fmt.Errorf("%w: subject %s key %s", ErrKeyUnknown, ref.Subject, ref.KeyID)
	}
	gcm, err := newGCM(key.Material[:])
	if err != nil {
		return nil, err
	}
	if len(ref.Nonce) != gcm.NonceSize() {
		return nil, ErrInvalidRef
	}
	plain, err := gcm.Open(nil, ref.Nonce, ref.Ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecryptFailed, err)
	}
	return plain, nil
}

// DestroySubjectKey crypto-shreds every payload sealed under the subject's
// keys: it zeroes the key material, drops the keys, and returns the minimal
// tombstone for the ledger. refs name the sealed payloads the tombstone
// binds to by ciphertext digest; refs outside the subject are rejected so
// one subject's erasure can never tombstone another's facts.
func (v *Vault) DestroySubjectKey(tenant, subject string, refs []VaultRef, at time.Time) (Tombstone, error) {
	if v == nil {
		return Tombstone{}, errors.New("payloadvault: vault is required")
	}
	if tenant == "" || subject == "" {
		return Tombstone{}, errors.New("payloadvault: tenant and subject are required")
	}
	if at.IsZero() {
		return Tombstone{}, errors.New("payloadvault: destroyed-at is required")
	}
	prefix := tenant + "\x00" + subject + "\x00"
	var keyIDs []string
	for id := range v.live {
		if len(id) > len(prefix) && id[:len(prefix)] == prefix {
			keyIDs = append(keyIDs, id[len(prefix):])
		}
	}
	if len(keyIDs) == 0 {
		return Tombstone{}, fmt.Errorf("%w: no live key for subject", ErrKeyUnknown)
	}
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return Tombstone{}, err
		}
		if ref.Tenant != tenant || ref.Subject != subject {
			return Tombstone{}, fmt.Errorf("%w: reference belongs to %s/%s", ErrInvalidRef, ref.Tenant, ref.Subject)
		}
	}
	sort.Strings(keyIDs)
	expiry := time.Time{}
	payloads := make([]PayloadRecord, 0, len(refs))
	for _, ref := range refs {
		if expiry.IsZero() || ref.BackupExpiry.Before(expiry) {
			expiry = ref.BackupExpiry
		}
		payloads = append(payloads, PayloadRecord{
			Table:        ref.Table,
			Field:        ref.Field,
			PlainBytes:   ref.PlainBytes,
			CipherDigest: ref.CipherDigest,
		})
	}
	sort.Slice(payloads, func(i, j int) bool {
		if payloads[i].Table != payloads[j].Table {
			return payloads[i].Table < payloads[j].Table
		}
		if payloads[i].Field != payloads[j].Field {
			return payloads[i].Field < payloads[j].Field
		}
		return payloads[i].CipherDigest < payloads[j].CipherDigest
	})
	for id, key := range v.live {
		if len(id) > len(prefix) && id[:len(prefix)] == prefix && key != nil {
			for i := range key.Material {
				key.Material[i] = 0
			}
			delete(v.live, id)
			v.destroyed[id] = true
		}
	}
	tomb := Tombstone{
		Tenant:       tenant,
		Subject:      subject,
		KeyIDs:       keyIDs,
		Payloads:     payloads,
		DestroyedAt:  at.UTC(),
		BackupExpiry: expiry.UTC(),
	}
	if expiry.IsZero() {
		// No sealed payloads were named: no copies exist anywhere, so
		// the backup-clear date is the destruction instant itself.
		expiry = at
	}
	tomb.BackupExpiry = expiry.UTC()
	if err := tomb.Validate(); err != nil {
		return Tombstone{}, err
	}
	return tomb, nil
}

func nonceSize() int {
	var zeroKey [KeySize]byte
	gcm, err := newGCM(zeroKey[:])
	if err != nil {
		return 12
	}
	return gcm.NonceSize()
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("payloadvault: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("payloadvault: gcm: %w", err)
	}
	return gcm, nil
}
