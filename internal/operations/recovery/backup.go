package recovery

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// BackupStatus is the result of checking an encrypted backup set.
type BackupStatus string

const (
	Verified BackupStatus = "VERIFIED"
	Rejected BackupStatus = "REJECTED"
)

var (
	ErrInvalidBackup      = errors.New("recovery: invalid backup set")
	ErrImmutable          = errors.New("recovery: immutable backup set cannot be replaced")
	ErrUnreadable         = errors.New("recovery: backup set is not readable")
	ErrWrongKey           = errors.New("recovery: encryption key does not match backup set")
	ErrInvalidSignature   = errors.New("recovery: backup manifest signature is invalid")
	ErrUnboundedTenant    = errors.New("recovery: backup tenant scope is unbounded")
	ErrRetentionViolation = errors.New("recovery: retention lock or expiry is invalid")
)

// CreateRequest describes one immutable backup set. Blocks are already
// bounded chunks supplied by the caller; the builder never stores plaintext.
type CreateRequest struct {
	SetID        string
	TenantID     string
	PolicyID     string
	SourcePlane  string
	Watermark    string
	KeyReference string
	KeyVersion   string
	RetainUntil  time.Time
	Blocks       [][]byte
	Now          time.Time
	NonceReader  io.Reader
}

// EncryptedBlock is the only block representation retained in BackupSet.
type EncryptedBlock struct {
	Index          int
	Nonce          []byte
	Ciphertext     []byte
	PlaintextHash  string
	CiphertextHash string
}

// Manifest is signed metadata for a backup set. Block hashes make the
// inventory tamper-evident even when verification samples only some blocks.
type Manifest struct {
	SetID            string
	TenantID         string
	PolicyID         string
	SourcePlane      string
	Watermark        string
	KeyReference     string
	KeyVersion       string
	RetainUntil      time.Time
	RetentionLocked  bool
	BlockCount       int
	ContentHash      string
	BlockHashes      []string
	CiphertextHashes []string
	Digest           string
	Signature        []byte
}

// BackupSet is an encrypted, signed, content-addressed backup inventory.
type BackupSet struct {
	Manifest Manifest
	Blocks   []EncryptedBlock
}

// VerifyOptions controls the readability check. A zero SampleCount selects a
// bounded deterministic sample of up to three blocks.
type VerifyOptions struct {
	Now         time.Time
	SampleCount int
	SampleSeed  []byte
}

// Check is one bounded verification result.
type Check struct {
	Name   string
	Passed bool
	Detail string
}

// Verification is the complete, payload-free result of checking a backup.
type Verification struct {
	Status         BackupStatus
	SetID          string
	SampledIndices []int
	Checks         []Check
	Failure        string
}

// Verified reports whether all requested backup checks passed.
func (v Verification) Verified() bool { return v.Status == Verified }

// Create encrypts and signs a backup set. AES-256-GCM provides authenticated
// encryption; Ed25519 signs the manifest digest. Neither plaintext nor the
// encryption key is retained in the returned set.
func Create(req CreateRequest, encryptionKey []byte, signingKey ed25519.PrivateKey) (BackupSet, error) {
	if err := validateCreateRequest(req, encryptionKey, signingKey); err != nil {
		return BackupSet{}, err
	}
	blockCipher, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return BackupSet{}, fmt.Errorf("%w: cipher: %v", ErrInvalidBackup, err)
	}
	gcm, err := cipher.NewGCM(blockCipher)
	if err != nil {
		return BackupSet{}, fmt.Errorf("%w: gcm: %v", ErrInvalidBackup, err)
	}
	rng := req.NonceReader
	if rng == nil {
		rng = rand.Reader
	}
	set := BackupSet{Manifest: Manifest{
		SetID:            req.SetID,
		TenantID:         req.TenantID,
		PolicyID:         req.PolicyID,
		SourcePlane:      req.SourcePlane,
		Watermark:        req.Watermark,
		KeyReference:     req.KeyReference,
		KeyVersion:       req.KeyVersion,
		RetainUntil:      req.RetainUntil.UTC(),
		RetentionLocked:  true,
		BlockCount:       len(req.Blocks),
		BlockHashes:      make([]string, 0, len(req.Blocks)),
		CiphertextHashes: make([]string, 0, len(req.Blocks)),
	}}
	set.Blocks = make([]EncryptedBlock, 0, len(req.Blocks))
	contentHasher := sha256.New()
	for index, plaintext := range req.Blocks {
		if len(plaintext) == 0 {
			return BackupSet{}, fmt.Errorf("%w: block %d is empty", ErrInvalidBackup, index)
		}
		plainHash := digest(plaintext)
		nonce := make([]byte, gcm.NonceSize())
		if _, err := io.ReadFull(rng, nonce); err != nil {
			return BackupSet{}, fmt.Errorf("%w: nonce for block %d: %v", ErrInvalidBackup, index, err)
		}
		ciphertext := gcm.Seal(nil, nonce, plaintext, associatedData(req.SetID, index, plainHash))
		cipherHash := digest(ciphertext)
		set.Manifest.BlockHashes = append(set.Manifest.BlockHashes, plainHash)
		set.Manifest.CiphertextHashes = append(set.Manifest.CiphertextHashes, cipherHash)
		_, _ = contentHasher.Write([]byte(plainHash))
		set.Blocks = append(set.Blocks, EncryptedBlock{
			Index:          index,
			Nonce:          append([]byte(nil), nonce...),
			Ciphertext:     append([]byte(nil), ciphertext...),
			PlaintextHash:  plainHash,
			CiphertextHash: cipherHash,
		})
	}
	set.Manifest.ContentHash = "sha256:" + hex.EncodeToString(contentHasher.Sum(nil))
	body := manifestBody(set.Manifest)
	set.Manifest.Digest = digest(body)
	set.Manifest.Signature = append([]byte(nil), ed25519.Sign(signingKey, []byte(set.Manifest.Digest))...)
	return cloneBackupSet(set), nil
}

// NewBackupSet is an explicit alias for Create for callers that prefer a
// constructor-shaped API.
func NewBackupSet(req CreateRequest, encryptionKey []byte, signingKey ed25519.PrivateKey) (BackupSet, error) {
	return Create(req, encryptionKey, signingKey)
}

// Verify checks inventory, signature, retention, authenticated decryption of
// a bounded sample, and manifest traversal. A corrupt unsampled block is still
// detected by its ciphertext hash in the inventory check.
func Verify(set BackupSet, publicKey ed25519.PublicKey, encryptionKey []byte, options ...VerifyOptions) Verification {
	var opts VerifyOptions
	if len(options) > 0 {
		opts = options[0]
	}
	result := Verification{Status: Rejected, SetID: set.Manifest.SetID}
	fail := func(name string, err error) Verification {
		result.Checks = append(result.Checks, Check{Name: name, Detail: err.Error()})
		result.Failure = err.Error()
		return result
	}
	if err := validateManifestShape(set); err != nil {
		return fail("manifest", err)
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return fail("signature", ErrInvalidSignature)
	}
	if len(encryptionKey) != 32 {
		return fail("decrypt", ErrWrongKey)
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	if !set.Manifest.RetentionLocked || !set.Manifest.RetainUntil.After(opts.Now) {
		return fail("retention", ErrRetentionViolation)
	}
	if digest(manifestBody(set.Manifest)) != set.Manifest.Digest {
		return fail("manifest-digest", fmt.Errorf("%w: manifest digest mismatch", ErrInvalidBackup))
	}
	if !ed25519.Verify(publicKey, []byte(set.Manifest.Digest), set.Manifest.Signature) {
		return fail("signature", ErrInvalidSignature)
	}
	result.Checks = append(result.Checks,
		Check{Name: "manifest-digest", Passed: true},
		Check{Name: "signature", Passed: true},
		Check{Name: "retention", Passed: true},
	)
	for index, block := range set.Blocks {
		if digest(block.Ciphertext) != block.CiphertextHash || block.CiphertextHash != set.Manifest.CiphertextHashes[index] || block.PlaintextHash != set.Manifest.BlockHashes[index] {
			return fail("inventory", fmt.Errorf("%w: block %d hash mismatch", ErrInvalidBackup, index))
		}
	}
	result.Checks = append(result.Checks, Check{Name: "inventory", Passed: true})
	contentHasher := sha256.New()
	for _, plainHash := range set.Manifest.BlockHashes {
		_, _ = contentHasher.Write([]byte(plainHash))
	}
	if got := "sha256:" + hex.EncodeToString(contentHasher.Sum(nil)); got != set.Manifest.ContentHash {
		return fail("content-inventory", fmt.Errorf("%w: content hash mismatch", ErrInvalidBackup))
	}
	result.Checks = append(result.Checks, Check{Name: "content-inventory", Passed: true})
	indices, err := sampleIndices(set.Manifest.BlockCount, opts.SampleCount, opts.SampleSeed)
	if err != nil {
		return fail("sample", fmt.Errorf("%w: sample selection: %v", ErrUnreadable, err))
	}
	result.SampledIndices = append([]int(nil), indices...)
	if err := decryptSamples(set, encryptionKey, indices); err != nil {
		return fail("decrypt-sample", err)
	}
	result.Checks = append(result.Checks, Check{Name: "decrypt-sample", Passed: true, Detail: fmt.Sprintf("blocks=%d", len(indices))})
	result.Status = Verified
	return result
}

// VerifyReadable is the error-oriented form of Verify.
func VerifyReadable(set BackupSet, publicKey ed25519.PublicKey, encryptionKey []byte, options ...VerifyOptions) (Verification, error) {
	result := Verify(set, publicKey, encryptionKey, options...)
	if !result.Verified() {
		return result, errors.New(result.Failure)
	}
	return result, nil
}

// Restore decrypts every block after verifying the set. It is intentionally a
// separate operation from Verify: a successful backup job is not a restore.
func Restore(set BackupSet, publicKey ed25519.PublicKey, encryptionKey []byte, options ...VerifyOptions) ([][]byte, Verification, error) {
	verification, err := VerifyReadable(set, publicKey, encryptionKey, options...)
	if err != nil {
		return nil, verification, err
	}
	blockCipher, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, verification, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	gcm, err := cipher.NewGCM(blockCipher)
	if err != nil {
		return nil, verification, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	plaintext := make([][]byte, len(set.Blocks))
	for i, block := range set.Blocks {
		value, err := gcm.Open(nil, block.Nonce, block.Ciphertext, associatedData(set.Manifest.SetID, block.Index, block.PlaintextHash))
		if err != nil || digest(value) != block.PlaintextHash {
			if err == nil {
				err = fmt.Errorf("plaintext digest mismatch")
			}
			return nil, verification, fmt.Errorf("%w: block %d: %v", ErrUnreadable, i, err)
		}
		plaintext[i] = value
	}
	return plaintext, verification, nil
}

// Repository is a process-local immutable repository useful for conformance
// tests and orchestration adapters. It copies on put and get, and it refuses a
// different digest for an existing set identity.
type Repository struct {
	mu   sync.RWMutex
	sets map[string]BackupSet
}

// NewRepository constructs an empty immutable backup repository.
func NewRepository() *Repository { return &Repository{sets: make(map[string]BackupSet)} }

// Put stores a set once. Repeating the exact same digest is idempotent.
func (r *Repository) Put(set BackupSet) error {
	if r == nil {
		return fmt.Errorf("%w: nil repository", ErrInvalidBackup)
	}
	if err := validateManifestShape(set); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.sets[set.Manifest.SetID]; ok {
		if existing.Manifest.Digest != set.Manifest.Digest {
			return ErrImmutable
		}
		return nil
	}
	r.sets[set.Manifest.SetID] = cloneBackupSet(set)
	return nil
}

// Get returns a defensive copy.
func (r *Repository) Get(setID string) (BackupSet, bool) {
	if r == nil {
		return BackupSet{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	set, ok := r.sets[setID]
	if !ok {
		return BackupSet{}, false
	}
	return cloneBackupSet(set), true
}

// Explain renders a bounded verification summary without key material or
// plaintext.
func (v Verification) Explain() string {
	return fmt.Sprintf("backup verification set=%s status=%s sampled=%d checks=%d", v.SetID, v.Status, len(v.SampledIndices), len(v.Checks))
}

// Explain is the package-level explanation symbol shared by recovery values.
func Explain(value any) string {
	switch v := value.(type) {
	case Matrix:
		return v.Explain()
	case Verification:
		return v.Explain()
	case BackupSet:
		return fmt.Sprintf("backup set=%s blocks=%d digest=%s", v.Manifest.SetID, v.Manifest.BlockCount, v.Manifest.Digest)
	default:
		return "recovery explanation unavailable"
	}
}

func validateCreateRequest(req CreateRequest, key []byte, signingKey ed25519.PrivateKey) error {
	if strings.TrimSpace(req.SetID) == "" || strings.TrimSpace(req.PolicyID) == "" || strings.TrimSpace(req.SourcePlane) == "" || strings.TrimSpace(req.Watermark) == "" {
		return fmt.Errorf("%w: set, policy, source plane and watermark are required", ErrInvalidBackup)
	}
	if !validTenant(req.TenantID) {
		return ErrUnboundedTenant
	}
	if strings.TrimSpace(req.KeyReference) == "" || strings.TrimSpace(req.KeyVersion) == "" {
		return fmt.Errorf("%w: key reference and version are required", ErrInvalidBackup)
	}
	if len(key) != 32 {
		return ErrWrongKey
	}
	if len(signingKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("%w: signing key is missing", ErrInvalidSignature)
	}
	if len(req.Blocks) == 0 {
		return fmt.Errorf("%w: at least one block is required", ErrInvalidBackup)
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !req.RetainUntil.After(now) {
		return ErrRetentionViolation
	}
	return nil
}

func validateManifestShape(set BackupSet) error {
	m := set.Manifest
	if strings.TrimSpace(m.SetID) == "" || !validTenant(m.TenantID) || strings.TrimSpace(m.PolicyID) == "" || strings.TrimSpace(m.SourcePlane) == "" || strings.TrimSpace(m.Watermark) == "" || strings.TrimSpace(m.KeyReference) == "" || strings.TrimSpace(m.KeyVersion) == "" {
		if !validTenant(m.TenantID) && strings.TrimSpace(m.TenantID) != "" {
			return ErrUnboundedTenant
		}
		return fmt.Errorf("%w: required manifest field is missing", ErrInvalidBackup)
	}
	if !m.RetentionLocked || m.RetainUntil.IsZero() || m.BlockCount < 1 || len(set.Blocks) != m.BlockCount || len(m.BlockHashes) != m.BlockCount || len(m.CiphertextHashes) != m.BlockCount || len(m.Signature) != ed25519.SignatureSize {
		return fmt.Errorf("%w: incomplete manifest or inventory", ErrInvalidBackup)
	}
	for index, block := range set.Blocks {
		if block.Index != index || len(block.Nonce) != 12 || len(block.Ciphertext) == 0 || block.PlaintextHash == "" || block.CiphertextHash == "" {
			return fmt.Errorf("%w: invalid block %d", ErrInvalidBackup, index)
		}
	}
	return nil
}

func validTenant(tenant string) bool {
	tenant = strings.TrimSpace(tenant)
	if tenant == "" || tenant == "*" || strings.EqualFold(tenant, "all") || strings.EqualFold(tenant, "global") {
		return false
	}
	return !strings.ContainsAny(tenant, ",;|\n\r\t")
}

type manifestCanonical struct {
	SetID            string   `json:"set_id"`
	TenantID         string   `json:"tenant_id"`
	PolicyID         string   `json:"policy_id"`
	SourcePlane      string   `json:"source_plane"`
	Watermark        string   `json:"watermark"`
	KeyReference     string   `json:"key_reference"`
	KeyVersion       string   `json:"key_version"`
	RetainUntil      int64    `json:"retain_until_unix_nano"`
	RetentionLocked  bool     `json:"retention_locked"`
	BlockCount       int      `json:"block_count"`
	ContentHash      string   `json:"content_hash"`
	BlockHashes      []string `json:"block_hashes"`
	CiphertextHashes []string `json:"ciphertext_hashes"`
}

func manifestBody(m Manifest) []byte {
	body, _ := json.Marshal(manifestCanonical{
		SetID: m.SetID, TenantID: m.TenantID, PolicyID: m.PolicyID, SourcePlane: m.SourcePlane,
		Watermark: m.Watermark, KeyReference: m.KeyReference, KeyVersion: m.KeyVersion,
		RetainUntil: m.RetainUntil.UTC().UnixNano(), RetentionLocked: m.RetentionLocked,
		BlockCount: m.BlockCount, ContentHash: m.ContentHash,
		BlockHashes: append([]string(nil), m.BlockHashes...), CiphertextHashes: append([]string(nil), m.CiphertextHashes...),
	})
	return body
}

func associatedData(setID string, index int, plaintextHash string) []byte {
	return []byte(fmt.Sprintf("hcm-next-backup:v1:%s:%d:%s", setID, index, plaintextHash))
}

func decryptSamples(set BackupSet, key []byte, indices []int) error {
	blockCipher, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWrongKey, err)
	}
	gcm, err := cipher.NewGCM(blockCipher)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWrongKey, err)
	}
	for _, index := range indices {
		block := set.Blocks[index]
		plaintext, err := gcm.Open(nil, block.Nonce, block.Ciphertext, associatedData(set.Manifest.SetID, block.Index, block.PlaintextHash))
		if err != nil || digest(plaintext) != block.PlaintextHash {
			return fmt.Errorf("%w: block %d", ErrWrongKey, index)
		}
	}
	return nil
}

func sampleIndices(count, requested int, seed []byte) ([]int, error) {
	if requested <= 0 || requested > count {
		requested = count
		if requested > 3 {
			requested = 3
		}
	}
	if len(seed) == 0 {
		seed = make([]byte, sha256.Size)
		if _, err := io.ReadFull(rand.Reader, seed); err != nil {
			return nil, err
		}
	}
	type candidate struct {
		index int
		score [32]byte
	}
	candidates := make([]candidate, 0, count)
	for index := 0; index < count; index++ {
		value := sha256.Sum256(append(append([]byte(nil), seed...), byte(index>>24), byte(index>>16), byte(index>>8), byte(index)))
		candidates = append(candidates, candidate{index: index, score: value})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if string(candidates[i].score[:]) == string(candidates[j].score[:]) {
			return candidates[i].index < candidates[j].index
		}
		return string(candidates[i].score[:]) < string(candidates[j].score[:])
	})
	indices := make([]int, requested)
	for i := range indices {
		indices[i] = candidates[i].index
	}
	sort.Ints(indices)
	return indices, nil
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func cloneBackupSet(set BackupSet) BackupSet {
	set.Manifest.BlockHashes = append([]string(nil), set.Manifest.BlockHashes...)
	set.Manifest.CiphertextHashes = append([]string(nil), set.Manifest.CiphertextHashes...)
	set.Manifest.Signature = append([]byte(nil), set.Manifest.Signature...)
	blocks := set.Blocks
	set.Blocks = make([]EncryptedBlock, len(blocks))
	for i, block := range blocks {
		set.Blocks[i] = EncryptedBlock{
			Index: block.Index, Nonce: append([]byte(nil), block.Nonce...), Ciphertext: append([]byte(nil), block.Ciphertext...),
			PlaintextHash: block.PlaintextHash, CiphertextHash: block.CiphertextHash,
		}
	}
	return set
}
