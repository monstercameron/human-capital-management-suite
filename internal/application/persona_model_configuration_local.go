package application

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// LocalPersonaModelSigningMaterial is local development private material.
// Callers must store it below their ignored artifact root, never log it or use
// it as deployed production trust. Each key serves one independent purpose.
type LocalPersonaModelSigningMaterial struct {
	OutputSeed   string `json:"output_seed"`
	WorkloadSeed string `json:"workload_seed"`
	PricingSeed  string `json:"pricing_seed"`
	PolicySeed   string `json:"policy_seed,omitempty"`
}

// LoadOrCreateLocalPersonaModelSigningMaterial preserves local output recovery
// authority across restarts. Exclusive creation prevents replacing a running
// worker's keys, and a partial/corrupt document is refused rather than rotated.
func LoadOrCreateLocalPersonaModelSigningMaterial(path string) (LocalPersonaModelSigningMaterial, error) {
	var empty LocalPersonaModelSigningMaterial
	if path == "" {
		return empty, modelConfigurationError("local signing material path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return empty, modelConfigurationError("local signing directory cannot be created")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		// A concurrent starter may be finishing its exclusive first write.
		for attempt := 0; attempt < 20; attempt++ {
			material, readErr := readLocalPersonaSigningMaterial(path)
			if readErr == nil {
				if material.PolicySeed == "" {
					material.PolicySeed, readErr = loadOrCreateLocalPersonaPolicySeed(path+".policy-key", material)
					if readErr != nil {
						return empty, readErr
					}
				}
				return material, nil
			}
			if attempt == 19 {
				return empty, readErr
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if err != nil {
		return empty, modelConfigurationError("local signing material cannot be created")
	}
	defer f.Close()
	material := LocalPersonaModelSigningMaterial{}
	for _, target := range []*string{&material.OutputSeed, &material.WorkloadSeed, &material.PricingSeed, &material.PolicySeed} {
		seed := make([]byte, ed25519.SeedSize)
		if _, err := rand.Read(seed); err != nil {
			return empty, modelConfigurationError("local signing randomness unavailable")
		}
		*target = base64.StdEncoding.EncodeToString(seed)
	}
	if err := json.NewEncoder(f).Encode(material); err != nil {
		return empty, modelConfigurationError("local signing material cannot be written")
	}
	if err := f.Sync(); err != nil {
		return empty, modelConfigurationError("local signing material cannot be persisted")
	}
	return material, nil
}

func readLocalPersonaSigningMaterial(path string) (LocalPersonaModelSigningMaterial, error) {
	var material LocalPersonaModelSigningMaterial
	f, err := os.Open(path)
	if err != nil {
		return material, modelConfigurationError("local signing material cannot be opened")
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err := d.Decode(&material); err != nil {
		return material, modelConfigurationError("local signing material is malformed")
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return LocalPersonaModelSigningMaterial{}, modelConfigurationError("local signing material must contain one JSON document")
	}
	output, worker, err := personaModelSigningKeys(material.OutputSeed, material.WorkloadSeed)
	if err != nil {
		return LocalPersonaModelSigningMaterial{}, err
	}
	seed, err := decodeEd25519Seed(material.PricingSeed)
	if err != nil || len(seed) != ed25519.SeedSize || bytes.Equal(seed, output.Seed()) || bytes.Equal(seed, worker.Seed()) {
		return LocalPersonaModelSigningMaterial{}, modelConfigurationError("local pricing key must be dedicated")
	}
	if material.PolicySeed != "" {
		if err := validateLocalPersonaPolicySeed(material.PolicySeed, material); err != nil {
			return LocalPersonaModelSigningMaterial{}, err
		}
	}
	return material, nil
}

func validateLocalPersonaPolicySeed(encoded string, material LocalPersonaModelSigningMaterial) error {
	seed, err := decodeEd25519Seed(encoded)
	if err != nil {
		return modelConfigurationError("local policy signing seed is invalid")
	}
	for _, prior := range []string{material.OutputSeed, material.WorkloadSeed, material.PricingSeed} {
		decoded, err := decodeEd25519Seed(prior)
		if err != nil || bytes.Equal(seed, decoded) {
			return modelConfigurationError("local policy signing seed must be dedicated")
		}
	}
	return nil
}

// A separate exclusively created sidecar adds policy authority to a legacy
// three-key document without replacing its existing recovery or worker keys.
func loadOrCreateLocalPersonaPolicySeed(path string, material LocalPersonaModelSigningMaterial) (string, error) {
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return "", modelConfigurationError("local policy signing randomness unavailable")
	}
	encoded := base64.StdEncoding.EncodeToString(seed)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		for attempt := 0; attempt < 20; attempt++ {
			raw, readErr := os.ReadFile(path)
			if readErr == nil && validateLocalPersonaPolicySeed(string(raw), material) == nil {
				return string(raw), nil
			}
			if attempt == 19 {
				return "", modelConfigurationError("local policy signing material is malformed")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if err != nil {
		return "", modelConfigurationError("local policy signing material cannot be created")
	}
	defer f.Close()
	if _, err := f.WriteString(encoded); err != nil {
		return "", modelConfigurationError("local policy signing material cannot be written")
	}
	if err := f.Sync(); err != nil {
		return "", modelConfigurationError("local policy signing material cannot be persisted")
	}
	return encoded, nil
}

// SignLocalPersonaModelPricing records the local developer's explicit price
// schedule with persistent authority. It provides no rates, model approval,
// provider terms or evaluation claims.
func SignLocalPersonaModelPricing(cfg *PersonaModelDeployment, material LocalPersonaModelSigningMaterial) error {
	if cfg == nil {
		return modelConfigurationError("local pricing schedule is required")
	}
	seed, err := decodeEd25519Seed(material.PricingSeed)
	if err != nil {
		return modelConfigurationError("local pricing signing seed is invalid")
	}
	output, worker, err := personaModelSigningKeys(material.OutputSeed, material.WorkloadSeed)
	if err != nil {
		return err
	}
	if bytes.Equal(seed, output.Seed()) || bytes.Equal(seed, worker.Seed()) {
		return modelConfigurationError("local pricing signing seed must be dedicated")
	}
	private := ed25519.NewKeyFromSeed(seed)
	cfg.Pricing.Digest = agentmodel.PricingScheduleDigest(cfg.Pricing)
	cfg.Pricing.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(private, []byte(cfg.Pricing.Digest)))
	cfg.PricingPublicKey = base64.StdEncoding.EncodeToString(private.Public().(ed25519.PublicKey))
	if _, err := agentmodel.NewPricingSchedule(cfg.Pricing); err != nil {
		return modelConfigurationError("local pricing schedule is invalid")
	}
	return nil
}
