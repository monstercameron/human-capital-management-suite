package agentstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var (
	// ErrInstructionContentNotFound means no trusted content is bound to the exact manifest identity.
	ErrInstructionContentNotFound = errors.New("agentstore: manifest instruction content not found")
	// ErrInvalidInstructionContent means content is empty, oversized, or invalid UTF-8.
	ErrInvalidInstructionContent = errors.New("agentstore: invalid instruction content")
)

const maxInstructionContentBytes = 64 << 10

// ManifestInstructionReader resolves executable text from an immutable agent
// manifest version using its tenant, definition, version, and declared digest.
type ManifestInstructionReader interface {
	ManifestInstructions(context.Context, uuid.UUID, string, uint64, string) (string, error)
}

// SaveInstructionContent stores the exact executable bytes under their
// tenant-scoped SHA-256 digest and returns that digest for the manifest.
func (s *Store) SaveInstructionContent(ctx context.Context, tenantID uuid.UUID, content string) (string, error) {
	if tenantID == uuid.Nil || len(content) == 0 || len(content) > maxInstructionContentBytes || !utf8.ValidString(content) || strings.TrimSpace(content) == "" {
		return "", ErrInvalidInstructionContent
	}
	digest := instructionDigest(content)
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `INSERT INTO agent_instruction_content (tenant_id,digest,content)
			VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, tenantID, digest, content)
		if err != nil {
			return fmt.Errorf("agentstore: save instruction content: %w", err)
		}
		if n == 1 {
			return nil
		}
		var stored string
		if err := tx.QueryRow(ctx, `SELECT content FROM agent_instruction_content WHERE tenant_id=$1 AND digest=$2`, tenantID, digest).Scan(&stored); err != nil {
			return fmt.Errorf("agentstore: verify retained instruction content: %w", err)
		}
		if stored != content {
			return fmt.Errorf("agentstore: digest collision for instruction content")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return digest, nil
}

// ManifestInstructions returns the exact bytes referenced by one immutable
// manifest version. The row, tenant, version and digest must all agree.
func (s *Store) ManifestInstructions(ctx context.Context, tenantID uuid.UUID, definitionID string, version uint64, digest string) (string, error) {
	if tenantID == uuid.Nil || strings.TrimSpace(definitionID) == "" || version == 0 || version > math.MaxInt64 || !validInstructionDigest(digest) {
		return "", fmt.Errorf("%w: invalid tenant, version, definition or digest", ErrInvalidConfig)
	}
	var content string
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		err := tx.QueryRow(ctx, `SELECT c.content
			FROM agent_definition_version v
			JOIN agent_instruction_content c
				ON c.tenant_id=v.tenant_id AND c.digest=v.manifest->>'instructions_digest'
			WHERE v.tenant_id=$1 AND v.definition_id=$2 AND v.version=$3
				AND v.manifest->>'instructions_digest'=$4 AND c.digest=$4`,
			tenantID, definitionID, int64(version), digest).Scan(&content)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrInstructionContentNotFound
		}
		if err != nil {
			return fmt.Errorf("agentstore: read manifest instruction content: %w", err)
		}
		if instructionDigest(content) != digest {
			return ErrInstructionContentNotFound
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return content, nil
}

func instructionDigest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validInstructionDigest(digest string) bool {
	if len(digest) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(digest, "sha256:") {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(digest, "sha256:"))
	return err == nil && len(decoded) == sha256.Size && digest == strings.ToLower(digest)
}

var _ ManifestInstructionReader = (*Store)(nil)
