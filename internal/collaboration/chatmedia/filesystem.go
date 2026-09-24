package chatmedia

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

// FilesystemStore is a small durable local adapter. The metadata file is the
// authority for state; content is always written below root and is returned
// only after metadata says ADMITTED. Production object providers can implement
// Store without changing the service.
type FilesystemStore struct{ root string }

func NewFilesystemStore(root string) (*FilesystemStore, error) {
	if root == "" {
		return nil, ErrInvalid
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	return &FilesystemStore{root: root}, nil
}
func (f *FilesystemStore) paths(id string) (string, string) {
	return filepath.Join(f.root, id+".bytes"), filepath.Join(f.root, id+".json")
}
func (f *FilesystemStore) Quarantine(ctx context.Context, a Artifact) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.ArtifactID == "" || filepath.Base(a.ArtifactID) != a.ArtifactID {
		return ErrInvalid
	}
	bp, mp := f.paths(a.ArtifactID)
	if existing, err := os.ReadFile(mp); err == nil {
		var stored Artifact
		if err := json.Unmarshal(existing, &stored); err != nil {
			return err
		}
		if stored.TenantID != a.TenantID || stored.ConversationID != a.ConversationID {
			return ErrUnauthorized
		}
		if stored.MediaType != a.MediaType || stored.Size != a.Size || stored.Transcript != a.Transcript || stored.AltText != a.AltText {
			return ErrInvalid
		}
		content, err := os.ReadFile(bp)
		if err != nil {
			return err
		}
		if !bytes.Equal(content, a.Content) {
			return ErrInvalid
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.WriteFile(bp, a.Content, 0600); err != nil {
		return err
	}
	// Original bytes live in .bytes; metadata must not duplicate a 64 MiB
	// upload as base64 in the JSON file.
	a.Content = nil
	b, _ := json.Marshal(a)
	if err := os.WriteFile(mp, b, 0600); err != nil {
		_ = os.Remove(bp)
		return err
	}
	return nil
}
func (f *FilesystemStore) SetRenditions(ctx context.Context, id, tenant string, renditions map[string]Rendition) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if filepath.Base(id) != id {
		return ErrInvalid
	}
	_, mp := f.paths(id)
	b, err := os.ReadFile(mp)
	if err != nil {
		return err
	}
	var a Artifact
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	if a.TenantID != tenant {
		return ErrUnauthorized
	}
	if a.State != StateQuarantined && a.State != StateAdmitted {
		return ErrInvalid
	}
	if a.State == StateAdmitted && len(a.Renditions) != 0 {
		return nil
	}
	a.Renditions = make(map[string]Rendition, len(renditions))
	for variant, rendition := range renditions {
		if variant != VariantThumbnail && variant != VariantDisplay {
			return ErrInvalid
		}
		if err := os.WriteFile(filepath.Join(f.root, id+"."+variant), rendition.Content, 0600); err != nil {
			return err
		}
		rendition.Content = nil
		a.Renditions[variant] = rendition
	}
	b, err = json.Marshal(a)
	if err != nil {
		return err
	}
	return os.WriteFile(mp, b, 0600)
}
func (f *FilesystemStore) SetVerdict(ctx context.Context, id, tenant string, state ArtifactState, reason, scanner string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, mp := f.paths(id)
	b, err := os.ReadFile(mp)
	if err != nil {
		return err
	}
	var a Artifact
	if err = json.Unmarshal(b, &a); err != nil {
		return err
	}
	if a.TenantID != tenant {
		return ErrUnauthorized
	}
	if a.State == StateAdmitted {
		return nil
	}
	a.State = state
	a.Reason = reason
	a.ScannerID = scanner
	b, err = json.Marshal(a)
	if err != nil {
		return err
	}
	return os.WriteFile(mp, b, 0600)
}
func (f *FilesystemStore) Get(ctx context.Context, tenant, id string) (Artifact, error) {
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}
	if filepath.Base(id) != id {
		return Artifact{}, ErrInvalid
	}
	_, mp := f.paths(id)
	b, err := os.ReadFile(mp)
	if err != nil {
		return Artifact{}, err
	}
	var a Artifact
	if err = json.Unmarshal(b, &a); err != nil {
		return Artifact{}, err
	}
	if a.TenantID != tenant {
		return Artifact{}, ErrUnauthorized
	}
	if a.State != StateAdmitted {
		return Artifact{}, ErrQuarantined
	}
	bp, _ := f.paths(id)
	a.Content, err = os.ReadFile(bp)
	if err != nil {
		return Artifact{}, err
	}
	return a, nil
}

func (f *FilesystemStore) GetRendition(ctx context.Context, tenant, id, variant string) (Artifact, error) {
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}
	if filepath.Base(id) != id || variant != VariantThumbnail && variant != VariantDisplay {
		return Artifact{}, ErrInvalid
	}
	_, mp := f.paths(id)
	b, err := os.ReadFile(mp)
	if err != nil {
		return Artifact{}, err
	}
	var a Artifact
	if err := json.Unmarshal(b, &a); err != nil {
		return Artifact{}, err
	}
	if a.TenantID != tenant {
		return Artifact{}, ErrUnauthorized
	}
	if a.State != StateAdmitted {
		return Artifact{}, ErrQuarantined
	}
	if a.MediaType == MediaGIF {
		bp, _ := f.paths(id)
		a.Content, err = os.ReadFile(bp)
		return a, err
	}
	r, ok := a.Renditions[variant]
	if !ok {
		return Artifact{}, ErrUnsupported
	}
	a.Content, err = os.ReadFile(filepath.Join(f.root, id+"."+variant))
	if err != nil {
		return Artifact{}, err
	}
	a.MediaType, a.Size = r.MediaType, int64(len(a.Content))
	return a, nil
}

var _ Store = (*FilesystemStore)(nil)
