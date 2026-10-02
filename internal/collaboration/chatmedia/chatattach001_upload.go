package chatmedia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrChatattach001Size  = errors.New("chatmedia: attachment too large")
	ErrChatattach001Quota = errors.New("chatmedia: attachment quota reached")
)

type Chatattach001Record struct {
	Reference
	PrincipalID string
	CreatedAt   time.Time
	Linked      bool
}

// Chatattach001Uploads owns the single-node upload quota ledger. The ledger
// survives restart. Linked checks message retention; a failed check never
// releases quota or deletes bytes. Unsent uploads expire after one day.
type Chatattach001Uploads struct {
	Media                          *Service
	Root                           string
	Authorize                      Authorizer
	Linked                         func(context.Context, Reference) (bool, error)
	PersonBytes, ConversationBytes int64
	Now                            func() time.Time
	mu                             sync.Mutex
}

func (s *Chatattach001Uploads) Upload(ctx context.Context, r UploadRequest) (Reference, error) {
	if s == nil || s.Media == nil || s.Authorize == nil || s.Root == "" || r.TenantID == "" || r.ConversationID == "" || r.PrincipalID == "" || len(r.Content) == 0 {
		return Reference{}, ErrInvalid
	}
	if err := s.Authorize(ctx, AccessRequest{TenantID: r.TenantID, ConversationID: r.ConversationID, PrincipalID: r.PrincipalID}); err != nil {
		return Reference{}, ErrUnauthorized
	}
	if int64(len(r.Content)) > Chatattach001MaxBytes {
		return Reference{}, ErrChatattach001Size
	}
	switch strings.ToLower(filepath.Ext(r.Filename)) {
	case ".exe", ".dll", ".com", ".bat", ".cmd", ".ps1", ".js", ".vbs", ".msi", ".sh", ".zip", ".tar", ".gz", ".rar", ".7z", ".docm", ".xlsm", ".pptm":
		return Reference{}, ErrUnsupported
	}
	t := Chatattach001ContentType(r.Content)
	declared := strings.Split(strings.ToLower(r.DeclaredType), ";")[0]
	if t == "text/plain" && (declared == "text/csv" || declared == "text/markdown") {
		declared = t
	}
	if t == "" || (declared != "" && declared != "application/octet-stream" && declared != t) {
		return Reference{}, ErrUnsupported
	}
	id := scopedArtifactID(r.TenantID, r.ConversationID, r.Content)
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.records(ctx, r.TenantID)
	if err != nil {
		return Reference{}, err
	}
	var person, conversation int64
	for _, a := range records {
		if a.ArtifactID == id {
			// A retry is free but must still pass the current post permission.
			return s.Media.Chatattach001Describe(ctx, r.TenantID, r.ConversationID, id)
		}
		if a.PrincipalID == r.PrincipalID {
			person += a.Size
		}
		if a.ConversationID == r.ConversationID {
			conversation += a.Size
		}
	}
	pmax, cmax := s.PersonBytes, s.ConversationBytes
	if pmax <= 0 {
		pmax = 200 << 20
	}
	if cmax <= 0 {
		cmax = 1 << 30
	}
	if person+int64(len(r.Content)) > pmax || conversation+int64(len(r.Content)) > cmax {
		return Reference{}, ErrChatattach001Quota
	}
	r.DeclaredType = t
	var ref Reference
	if mt, ok := normalizeType(t); ok && sniff(r.Content) == mt {
		ref, err = s.Media.Upload(ctx, r)
	} else {
		ref, err = s.Media.chatattach001UploadDocument(ctx, r, t)
	}
	if err != nil {
		cleanupErr := s.removeArtifact(Reference{ArtifactID: id, TenantID: r.TenantID, ConversationID: r.ConversationID}, false)
		return Reference{}, errors.Join(err, cleanupErr)
	}
	a := Chatattach001Record{Reference: ref, PrincipalID: r.PrincipalID, CreatedAt: s.now()}
	encoded, err := json.Marshal(a)
	if err != nil {
		return Reference{}, err
	}
	if err := os.WriteFile(filepath.Join(s.Root, id+".attachment"), encoded, 0600); err != nil {
		_ = s.remove(ref)
		return Reference{}, err
	}
	return ref, nil
}

func (s *Chatattach001Uploads) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Chatattach001Uploads) records(ctx context.Context, tenant string) ([]Chatattach001Record, error) {
	files, err := filepath.Glob(filepath.Join(s.Root, "*.attachment"))
	if err != nil {
		return nil, err
	}
	var out []Chatattach001Record
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var a Chatattach001Record
		if err := json.Unmarshal(b, &a); err != nil {
			return nil, err
		}
		if a.TenantID != tenant {
			continue
		}
		if filepath.Base(file) != a.ArtifactID+".attachment" {
			return nil, ErrInvalid
		}
		linked := a.Linked
		if s.Linked != nil {
			linked, err = s.Linked(ctx, a.Reference)
			if err != nil {
				return nil, err
			}
		}
		if !linked && (a.Linked || s.now().Sub(a.CreatedAt) >= 24*time.Hour) {
			if err := s.remove(a.Reference); err != nil {
				return nil, err
			}
			continue
		}
		if linked && !a.Linked {
			a.Linked = true
			b, err = json.Marshal(a)
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(file, b, 0600); err != nil {
				return nil, err
			}
		}
		out = append(out, a)
	}
	return out, nil
}

// Retain synchronises the tenant's ledger with message retention on each
// media operation. Files linked to a retained tombstone remain for record holds.
func (s *Chatattach001Uploads) Retain(ctx context.Context, tenant string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.records(ctx, tenant)
	return err
}

func (s *Chatattach001Uploads) remove(r Reference) error {
	return s.removeArtifact(r, true)
}

func (s *Chatattach001Uploads) removeArtifact(r Reference, allowAdmitted bool) error {
	f, ok := s.Media.store.(*FilesystemStore)
	if !ok || f.root != s.Root || filepath.Base(r.ArtifactID) != r.ArtifactID || r.ArtifactID == "." {
		return ErrInvalid
	}
	// Verify tenant identity even for rejected content. Failed scans must not
	// accumulate unmetered quarantine bytes or delete a previously admitted file.
	_, metadata := f.paths(r.ArtifactID)
	b, err := os.ReadFile(metadata)
	if !allowAdmitted && os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var artifact Artifact
	if err := json.Unmarshal(b, &artifact); err != nil {
		return err
	}
	if artifact.TenantID != r.TenantID || artifact.ConversationID != r.ConversationID || artifact.ArtifactID != r.ArtifactID {
		return ErrUnauthorized
	}
	if artifact.State == StateAdmitted && !allowAdmitted {
		return nil
	}
	for _, suffix := range []string{".json", ".bytes", ".thumbnail", ".display", ".attachment"} {
		if err := os.Remove(filepath.Join(f.root, r.ArtifactID+suffix)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (s *Service) chatattach001UploadDocument(ctx context.Context, r UploadRequest, t string) (Reference, error) {
	if s.scanner == nil {
		return Reference{}, ErrScannerUnavailable
	}
	id := scopedArtifactID(r.TenantID, r.ConversationID, r.Content)
	if s.authorize == nil || s.authorize(ctx, AccessRequest{TenantID: r.TenantID, ConversationID: r.ConversationID, PrincipalID: r.PrincipalID, ArtifactID: id}) != nil {
		return Reference{}, ErrUnauthorized
	}
	ref := Reference{ArtifactID: id, TenantID: r.TenantID, ConversationID: r.ConversationID, MediaType: MediaType(t), Size: int64(len(r.Content)), State: StateQuarantined}
	if err := s.store.Quarantine(ctx, Artifact{Reference: ref, Content: r.Content}); err != nil {
		return Reference{}, err
	}
	v, err := s.scanner.Scan(ctx, id, bytes.NewReader(r.Content))
	if err != nil {
		_ = s.store.SetVerdict(ctx, id, r.TenantID, StateRejected, "scanner unavailable", "")
		return Reference{}, ErrScannerUnavailable
	}
	if !v.Safe {
		_ = s.store.SetVerdict(ctx, id, r.TenantID, StateRejected, v.Reason, "")
		return Reference{}, ErrQuarantined
	}
	if err := s.store.SetVerdict(ctx, id, r.TenantID, StateAdmitted, "", "structural-file-check"); err != nil {
		return Reference{}, err
	}
	ref.State = StateAdmitted
	return ref, nil
}

// Chatattach001Describe keeps the tenant-first store contract explicit. The
// older Describe method reverses these arguments and cannot validate a post.
func (s *Service) Chatattach001Describe(ctx context.Context, tenant, conversation, id string) (Reference, error) {
	if s == nil || s.store == nil || tenant == "" || conversation == "" || id == "" {
		return Reference{}, ErrInvalid
	}
	a, err := s.store.Get(ctx, tenant, id)
	if err != nil {
		return Reference{}, err
	}
	if a.TenantID != tenant || a.ConversationID != conversation {
		return Reference{}, ErrUnauthorized
	}
	if a.State != StateAdmitted {
		return Reference{}, ErrQuarantined
	}
	return a.Reference, nil
}
