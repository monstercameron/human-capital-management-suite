package productui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pageledger"
)

type persistedPageRetirement struct {
	Retirement PageRetirement `json:"retirement"`
	Digest     string         `json:"integrity_digest"`
}

func retirementDigest(retirement PageRetirement) string {
	encoded, err := json.Marshal(retirement)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// MarshalPageRetirement stores the retirement with an integrity digest over
// the exact page, effective date, and review reason.
func MarshalPageRetirement(retirement PageRetirement) []byte {
	wrapper, err := json.Marshal(persistedPageRetirement{Retirement: retirement, Digest: retirementDigest(retirement)})
	if err != nil {
		return nil
	}
	return wrapper
}

// ParsePageRetirement validates the immutable persisted retirement payload.
func ParsePageRetirement(encoded []byte) (PageRetirement, error) {
	var stored persistedPageRetirement
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil {
		return PageRetirement{}, fmt.Errorf("productui: retirement bytes do not parse: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return PageRetirement{}, fmt.Errorf("productui: trailing retirement bytes")
	}
	if stored.Digest == "" || stored.Digest != retirementDigest(stored.Retirement) {
		return PageRetirement{}, fmt.Errorf("productui: retirement integrity digest mismatch for page %q", stored.Retirement.Page)
	}
	return stored.Retirement, nil
}

// RecordRetirementContext persists the page's immutable retirement evidence.
func (log *PageRevisionLog) RecordRetirementContext(ctx context.Context, retirement PageRetirement) error {
	if verdict := ValidatePageRetirement(retirement); !verdict.Compatible {
		return fmt.Errorf("productui: invalid retirement: %v", verdict.Reasons)
	}
	if err := VerifyRetirementTarget(log, retirement); err != nil {
		return err
	}
	if log.retirements == nil {
		log.retirements = make(map[PageID]PageRetirement)
	}
	if existing, ok := log.retirements[retirement.Page]; ok {
		if existing == retirement {
			return nil
		}
		return fmt.Errorf("productui: retirement conflict for page %q", retirement.Page)
	}
	if log.store != nil {
		body := MarshalPageRetirement(retirement)
		if err := log.store.PutRetirement(ctx, log.tenant, string(retirement.Page), retirementDigest(retirement), body); err != nil {
			return fmt.Errorf("productui: persist retirement: %w", err)
		}
	}
	log.retirements[retirement.Page] = retirement
	return nil
}

// Retirements returns a stable copy of the tenant's persisted retirements.
func (log *PageRevisionLog) Retirements() []PageRetirement {
	result := make([]PageRetirement, 0, len(log.retirements))
	for _, retirement := range log.retirements {
		result = append(result, retirement)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Page < result[j].Page })
	return result
}

func loadRetirements(ctx context.Context, log *PageRevisionLog, store pageledger.Store, tenant string) error {
	rows, err := store.LoadRetirements(ctx, tenant)
	if err != nil {
		return fmt.Errorf("productui: load page retirements: %w", err)
	}
	for _, row := range rows {
		retirement, err := ParsePageRetirement(row.Payload)
		if err != nil {
			return err
		}
		if row.Page != string(retirement.Page) || row.Digest != retirementDigest(retirement) {
			return fmt.Errorf("productui: invalid stored retirement key")
		}
		if err := log.RecordRetirementContext(ctx, retirement); err != nil {
			return err
		}
	}
	return nil
}
