package sbom

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ValidateRootHash compares the CycloneDX root application's SHA-256 hash
// with an expected digest. It checks only this digest relationship: it does
// not verify provenance signatures, BOM completeness, or the binary's module
// graph. Callers must perform those checks separately.
func ValidateRootHash(doc *Document, expectedSHA256 string) error {
	if doc == nil {
		return errors.New("sbom: document is nil")
	}
	if !validSHA256(expectedSHA256) {
		return errors.New("sbom: expected digest must be 64 lowercase hexadecimal characters")
	}
	var found bool
	for _, hash := range doc.Metadata.Component.Hashes {
		if hash.Alg != HashAlgSHA256 {
			continue
		}
		if found {
			return errors.New("sbom: root component has multiple SHA-256 hashes")
		}
		found = true
		if hash.Content != expectedSHA256 {
			return fmt.Errorf("sbom: root component digest %q does not match expected digest %q", hash.Content, expectedSHA256)
		}
	}
	if !found {
		return errors.New("sbom: root component has no SHA-256 artifact subject")
	}
	return nil
}

// ValidateArtifactDigestBinding compares the bytes at artifactPath, the
// CycloneDX root hash, and expectedSHA256. It checks only digest equality:
// it does not verify that the expected digest came from signed provenance,
// validate BOM completeness, or compare the BOM modules with binary build
// metadata. It is not a release-admission check.
func ValidateArtifactDigestBinding(doc *Document, artifactPath, expectedSHA256 string) error {
	if !validSHA256(expectedSHA256) {
		return errors.New("sbom: expected digest must be 64 lowercase hexadecimal characters")
	}
	digest, err := artifactDigest(artifactPath)
	if err != nil {
		return err
	}
	if digest != expectedSHA256 {
		return fmt.Errorf("sbom: artifact digest %q does not match expected digest %q", digest, expectedSHA256)
	}
	return ValidateRootHash(doc, expectedSHA256)
}

func artifactDigest(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("sbom: artifact path is required")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("sbom: reading artifact %s: %w", path, err)
	}
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:]), nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
