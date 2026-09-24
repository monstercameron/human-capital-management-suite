// Package formdraftstore is the tenant-scoped PostgreSQL repository for
// encrypted form drafts. Clear answer bytes exist only in memory; the database
// receives AES-GCM ciphertext and its SHA-256 digest.
package formdraftstore

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/forms/drafts"
)

// DB is the caller-owned transaction capability the repository needs.
type DB interface{ dbport.Beginner }

// Store implements drafts.Repository. The encryption key is supplied by the
// composition root and is never generated, persisted, or logged here. Every
// read or write uses a short transaction with tenant RLS enabled.
type Store struct {
	db   DB
	aead cipher.AEAD
	now  func() time.Time
	rng  io.Reader
}

// New constructs a durable repository. Key must be a secret AES key of 16,
// 24, or 32 bytes and must be backed up by the deployment's key custody path
// if persisted drafts need to survive key rotation or disaster recovery.
func New(db DB, key []byte, now func() time.Time) (*Store, error) {
	if db == nil || len(key) == 0 {
		return nil, fmt.Errorf("formdraftstore: database and encryption key are required")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("formdraftstore: encryption key: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("formdraftstore: configure encryption: %w", err)
	}
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, aead: aead, now: now, rng: rand.Reader}, nil
}

var _ drafts.Repository = (*Store)(nil)

func parseTenant(text string) (uuid.UUID, error) {
	id, err := uuid.Parse(text)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, drafts.ErrInvalidInput
	}
	return id, nil
}

func (s *Store) begin(ctx context.Context, tenant uuid.UUID) (dbport.Tx, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("formdraftstore: begin transaction: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

func (s *Store) check() error {
	if s == nil || s.db == nil || s.aead == nil || s.now == nil || s.rng == nil {
		return errors.New("formdraftstore: store is incomplete")
	}
	return nil
}

func finish(ctx context.Context, tx dbport.Tx, op string, err error) error {
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("formdraftstore: commit %s: %w", op, err)
	}
	return nil
}

type stored struct {
	principal, form, version string
	revision                 uint64
	expires                  time.Time
	digest, sealed           []byte
}

func load(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, id string, lock bool) (stored, error) {
	query := `SELECT principal_id, form_id, form_version, revision, expires_at, answer_digest, sealed_answers
		FROM form_draft WHERE tenant_id=$1 AND draft_id=$2`
	if lock {
		query += ` FOR UPDATE`
	}
	var rec stored
	var revision int64
	err := tx.QueryRow(ctx, query, tenant, id).Scan(&rec.principal, &rec.form, &rec.version, &revision, &rec.expires, &rec.digest, &rec.sealed)
	if errors.Is(err, dbport.ErrNoRows) {
		return stored{}, drafts.ErrNotFound
	}
	if err != nil {
		return stored{}, fmt.Errorf("formdraftstore: read draft: %w", err)
	}
	if revision < 1 {
		return stored{}, drafts.ErrDenied
	}
	rec.revision = uint64(revision)
	rec.expires = rec.expires.UTC()
	return rec, nil
}

func (s *Store) Save(req drafts.SaveRequest) (drafts.Draft, error) {
	if err := s.check(); err != nil {
		return drafts.Draft{}, err
	}
	if err := validateSave(req); err != nil {
		return drafts.Draft{}, err
	}
	tenant, err := parseTenant(req.TenantID)
	if err != nil {
		return drafts.Draft{}, err
	}
	now := s.now().UTC()
	if !req.ExpiresAt.After(now) {
		return drafts.Draft{}, drafts.ErrExpired
	}
	expires := req.ExpiresAt.UTC().Truncate(time.Microsecond)
	if !expires.After(now) {
		return drafts.Draft{}, drafts.ErrExpired
	}
	ctx := context.Background()
	tx, err := s.begin(ctx, tenant)
	if err != nil {
		return drafts.Draft{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	old, lookupErr := load(ctx, tx, tenant, req.ID, true)
	var next uint64 = 1
	if lookupErr == nil {
		switch {
		case old.principal != req.PrincipalID:
			return drafts.Draft{}, finish(ctx, tx, "save", drafts.ErrDenied)
		case old.form != req.FormID || old.version != req.FormVersion:
			return drafts.Draft{}, finish(ctx, tx, "save", drafts.ErrRebase)
		case old.revision != req.ExpectedRevision:
			return drafts.Draft{}, finish(ctx, tx, "save", drafts.ErrConflict)
		case old.revision >= uint64(^uint64(0)>>1):
			return drafts.Draft{}, finish(ctx, tx, "save", drafts.ErrConflict)
		default:
			next = old.revision + 1
		}
	} else if !errors.Is(lookupErr, drafts.ErrNotFound) {
		return drafts.Draft{}, finish(ctx, tx, "save", lookupErr)
	} else if req.ExpectedRevision != 0 {
		return drafts.Draft{}, finish(ctx, tx, "save", drafts.ErrConflict)
	}
	digest := sha256.Sum256(req.Answers)
	sealed, err := s.seal(req.Answers, aad(tenant, req.ID, req.PrincipalID, req.FormID, req.FormVersion, next, expires))
	if err != nil {
		return drafts.Draft{}, finish(ctx, tx, "save", err)
	}
	if lookupErr == nil {
		changed, updateErr := tx.Exec(ctx, `UPDATE form_draft SET revision=$3, expires_at=$4,
			answer_digest=$5, sealed_answers=$6 WHERE tenant_id=$1 AND draft_id=$2 AND revision=$7`,
			tenant, req.ID, int64(next), expires, digest[:], sealed, int64(req.ExpectedRevision))
		if updateErr != nil {
			return drafts.Draft{}, finish(ctx, tx, "save", fmt.Errorf("formdraftstore: update draft: %w", updateErr))
		}
		if changed != 1 {
			return drafts.Draft{}, finish(ctx, tx, "save", drafts.ErrConflict)
		}
	} else {
		inserted, insertErr := tx.Exec(ctx, `INSERT INTO form_draft
			(tenant_id, draft_id, principal_id, form_id, form_version, revision, expires_at, answer_digest, sealed_answers)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (tenant_id,draft_id) DO NOTHING`,
			tenant, req.ID, req.PrincipalID, req.FormID, req.FormVersion, int64(next), expires, digest[:], sealed)
		if insertErr != nil {
			return drafts.Draft{}, finish(ctx, tx, "save", fmt.Errorf("formdraftstore: insert draft: %w", insertErr))
		}
		if inserted != 1 {
			return drafts.Draft{}, finish(ctx, tx, "save", drafts.ErrConflict)
		}
	}
	if err := finish(ctx, tx, "save", nil); err != nil {
		return drafts.Draft{}, err
	}
	return drafts.Draft{ID: req.ID, TenantID: req.TenantID, PrincipalID: req.PrincipalID, FormID: req.FormID,
		FormVersion: req.FormVersion, Revision: next, ExpiresAt: expires, AnswerDigest: digest}, nil
}

func validateSave(req drafts.SaveRequest) error {
	if req.ID == "" || req.TenantID == "" || req.PrincipalID == "" || req.FormID == "" || req.FormVersion == "" || len(req.Answers) == 0 || req.ExpiresAt.IsZero() {
		return drafts.ErrInvalidInput
	}
	return nil
}

func (s *Store) Resume(req drafts.ResumeRequest) (drafts.ResumeResult, error) {
	if err := s.check(); err != nil {
		return drafts.ResumeResult{Outcome: drafts.Denied}, err
	}
	if req.ID == "" || req.TenantID == "" || req.PrincipalID == "" || req.FormID == "" || req.FormVersion == "" || req.Revision == 0 {
		return drafts.ResumeResult{Outcome: drafts.Denied}, drafts.ErrInvalidInput
	}
	tenant, err := parseTenant(req.TenantID)
	if err != nil {
		return drafts.ResumeResult{Outcome: drafts.Denied}, err
	}
	ctx := context.Background()
	tx, err := s.begin(ctx, tenant)
	if err != nil {
		return drafts.ResumeResult{Outcome: drafts.Denied}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rec, err := load(ctx, tx, tenant, req.ID, false)
	if err != nil {
		return drafts.ResumeResult{Outcome: drafts.Denied}, finish(ctx, tx, "resume", err)
	}
	d := draftRecord(req, rec)
	if rec.principal != req.PrincipalID || rec.form != req.FormID {
		return drafts.ResumeResult{Outcome: drafts.Denied}, finish(ctx, tx, "resume", drafts.ErrDenied)
	}
	if !rec.expires.After(s.now().UTC()) {
		return drafts.ResumeResult{Outcome: drafts.Expired, Draft: d}, finish(ctx, tx, "resume", drafts.ErrExpired)
	}
	if rec.version != req.FormVersion || rec.revision != req.Revision {
		return drafts.ResumeResult{Outcome: drafts.RebaseRequired, Draft: d}, finish(ctx, tx, "resume", drafts.ErrRebase)
	}
	plain, err := s.open(rec.sealed, aad(tenant, req.ID, rec.principal, rec.form, rec.version, rec.revision, rec.expires))
	if err != nil {
		return drafts.ResumeResult{Outcome: drafts.Denied}, finish(ctx, tx, "resume", drafts.ErrDenied)
	}
	digest := sha256.Sum256(plain)
	if len(rec.digest) != len(digest) || subtle.ConstantTimeCompare(rec.digest, digest[:]) != 1 {
		return drafts.ResumeResult{Outcome: drafts.Denied}, finish(ctx, tx, "resume", drafts.ErrDenied)
	}
	if err := finish(ctx, tx, "resume", nil); err != nil {
		return drafts.ResumeResult{Outcome: drafts.Denied}, err
	}
	d.AnswerDigest = digest
	return drafts.ResumeResult{Outcome: drafts.Current, Draft: d, Answers: plain}, nil
}

func (s *Store) Submit(req drafts.ResumeRequest, validate drafts.ValidateFunc) (drafts.SubmitResult, error) {
	if err := s.check(); err != nil {
		return drafts.SubmitResult{}, err
	}
	if validate == nil {
		return drafts.SubmitResult{}, drafts.ErrInvalidInput
	}
	r, err := s.Resume(req)
	if err != nil {
		return drafts.SubmitResult{Effects: drafts.EffectCounters{}}, err
	}
	if err := validate(r.Draft.FormVersion, r.Answers); err != nil {
		return drafts.SubmitResult{Effects: drafts.EffectCounters{}}, fmt.Errorf("%w: %v", drafts.ErrValidation, err)
	}
	tenant, err := parseTenant(req.TenantID)
	if err != nil {
		return drafts.SubmitResult{}, err
	}
	ctx := context.Background()
	tx, err := s.begin(ctx, tenant)
	if err != nil {
		return drafts.SubmitResult{Effects: drafts.EffectCounters{}}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := load(ctx, tx, tenant, req.ID, true)
	if err != nil {
		return drafts.SubmitResult{Effects: drafts.EffectCounters{}}, finish(ctx, tx, "submit", err)
	}
	if current.principal != req.PrincipalID || current.form != req.FormID {
		return drafts.SubmitResult{Effects: drafts.EffectCounters{}}, finish(ctx, tx, "submit", drafts.ErrDenied)
	}
	if !current.expires.After(s.now().UTC()) {
		return drafts.SubmitResult{Effects: drafts.EffectCounters{}}, finish(ctx, tx, "submit", drafts.ErrExpired)
	}
	if current.version != req.FormVersion || current.revision != req.Revision {
		return drafts.SubmitResult{Effects: drafts.EffectCounters{}}, finish(ctx, tx, "submit", drafts.ErrConflict)
	}
	if err := finish(ctx, tx, "submit", nil); err != nil {
		return drafts.SubmitResult{Effects: drafts.EffectCounters{}}, err
	}
	return drafts.SubmitResult{Submission: drafts.Submission{ID: uuid.NewString(), DraftID: req.ID, TenantID: req.TenantID,
		PrincipalID: req.PrincipalID, FormID: req.FormID, FormVersion: req.FormVersion,
		DraftRevision: req.Revision, Answers: append([]byte(nil), r.Answers...), SubmittedAt: s.now().UTC()},
		Effects: drafts.EffectCounters{}}, nil
}

func draftRecord(req drafts.ResumeRequest, rec stored) drafts.Draft {
	var digest [32]byte
	if len(rec.digest) == len(digest) {
		copy(digest[:], rec.digest)
	}
	return drafts.Draft{ID: req.ID, TenantID: req.TenantID, PrincipalID: rec.principal, FormID: rec.form,
		FormVersion: rec.version, Revision: rec.revision, ExpiresAt: rec.expires, AnswerDigest: digest}
}

func (s *Store) seal(plain, associated []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(s.rng, nonce); err != nil {
		return nil, fmt.Errorf("formdraftstore: create nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, plain, associated), nil
}

func (s *Store) open(sealed, associated []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(sealed) < n+s.aead.Overhead() {
		return nil, drafts.ErrDenied
	}
	plain, err := s.aead.Open(nil, sealed[:n], sealed[n:], associated)
	if err != nil {
		return nil, drafts.ErrDenied
	}
	return plain, nil
}

func aad(tenant uuid.UUID, id, principal, form, version string, revision uint64, expires time.Time) []byte {
	fields := [][]byte{tenant[:], []byte(id), []byte(principal), []byte(form), []byte(version)}
	var fixed [16]byte
	binary.BigEndian.PutUint64(fixed[:8], revision)
	binary.BigEndian.PutUint64(fixed[8:], uint64(expires.UTC().UnixMicro()))
	fields = append(fields, fixed[:])
	out := make([]byte, 0, 128)
	for _, field := range fields {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(field)))
		out = append(out, size[:]...)
		out = append(out, field...)
	}
	return out
}
