// Command wedge compiles, signs and persists real Gate A and Gate B wedge
// decision records (REV-002-01): it reads a partner-manifest/evidence YAML
// tree, calls the existing pure evaluators in tools/planning/wedge and
// tools/planning/gateevidence, writes a signed
// definitions/planning/gates/wedge-*.yaml decision record, and re-verifies
// the record signature and digest on load. The -verify mode re-verifies a
// previously written record without signing anything new.
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/wedge"
	"gopkg.in/yaml.v3"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "wedge:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("wedge", flag.ContinueOnError)
	flags.SetOutput(stderr)
	gate := flags.String("gate", "", "gate to decide: a or b")
	receiptPath := flags.String("receipt", "", "path to the evidence receipt YAML")
	partnerPath := flags.String("partner", "", "optional path to the partner manifest YAML")
	keyPath := flags.String("key", "", "path to the hex-encoded ed25519 signing key (32-byte seed or 64-byte private key)")
	signer := flags.String("signer", "", "signer identity recorded on the decision")
	signedAt := flags.String("signed-at", "", "signing time RFC3339 (default now, UTC)")
	nowFlag := flags.String("now", "", "evaluation time RFC3339 for gate B (default signing time)")
	out := flags.String("out", "", "output path for the signed decision record")
	verifyPath := flags.String("verify", "", "verify a previously written decision record instead of deciding")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *gate != "a" && *gate != "b" {
		return fmt.Errorf("-gate must be a or b")
	}
	if *verifyPath != "" {
		return verifyRecord(*gate, *verifyPath, stdout, stderr)
	}
	if *receiptPath == "" {
		return fmt.Errorf("-receipt is required")
	}
	if *keyPath == "" {
		return fmt.Errorf("-key is required")
	}
	if *signer == "" {
		return fmt.Errorf("-signer is required")
	}
	signedAtTime := time.Now().UTC()
	if *signedAt != "" {
		parsed, err := time.Parse(time.RFC3339, *signedAt)
		if err != nil {
			return fmt.Errorf("parsing -signed-at: %w", err)
		}
		signedAtTime = parsed.UTC()
	}
	evalTime := signedAtTime
	if *nowFlag != "" {
		parsed, err := time.Parse(time.RFC3339, *nowFlag)
		if err != nil {
			return fmt.Errorf("parsing -now: %w", err)
		}
		evalTime = parsed.UTC()
	}
	priv, err := loadKey(*keyPath)
	if err != nil {
		return err
	}
	outPath := *out
	if outPath == "" {
		outPath = defaultOut(*gate)
	}

	var partnerLine string
	if *partnerPath != "" {
		manifest, err := loadPartner(*partnerPath)
		if err != nil {
			return err
		}
		partnerLine = fmt.Sprintf("partner_manifest: %s digest=%s violations=0", manifest.ManifestID, manifest.Digest)
	}

	stamp := signedAtTime.Format(time.RFC3339)
	if *gate == "a" {
		return decideGateA(*receiptPath, *signer, stamp, priv, outPath, partnerLine, stdout, stderr)
	}
	return decideGateB(*receiptPath, *signer, stamp, evalTime, priv, outPath, partnerLine, stdout, stderr)
}

func defaultOut(gate string) string {
	return filepath.Join("definitions", "planning", "gates", "wedge-gate"+gate+"-decision.yaml")
}

// decodeYAML decodes YAML through JSON so keys follow the structs'
// documented json tags instead of yaml.v3's lowercased field names.
func decodeYAML(data []byte, target any) error {
	var anyForm any
	if err := yaml.Unmarshal(data, &anyForm); err != nil {
		return err
	}
	bridged, err := json.Marshal(anyForm)
	if err != nil {
		return err
	}
	return json.Unmarshal(bridged, target)
}

// encodeYAML renders a struct as YAML with its json-tag keys.
func encodeYAML(value any) ([]byte, error) {
	bridged, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var anyForm any
	if err := json.Unmarshal(bridged, &anyForm); err != nil {
		return nil, err
	}
	return yaml.Marshal(anyForm)
}

func loadKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading key file: %w", err)
	}
	keyBytes, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("decoding hex key: %w", err)
	}
	switch len(keyBytes) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(keyBytes), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(keyBytes), nil
	default:
		return nil, fmt.Errorf("key has %d bytes, want %d (seed) or %d (private key)", len(keyBytes), ed25519.SeedSize, ed25519.PrivateKeySize)
	}
}

func loadPartner(path string) (wedge.PartnerManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return wedge.PartnerManifest{}, fmt.Errorf("reading partner manifest: %w", err)
	}
	var manifest wedge.PartnerManifest
	if err := decodeYAML(raw, &manifest); err != nil {
		return wedge.PartnerManifest{}, fmt.Errorf("decoding partner manifest: %w", err)
	}
	if violations := wedge.ValidatePartnerManifest(manifest); len(violations) != 0 {
		var details []string
		for _, v := range violations {
			details = append(details, v.Error())
		}
		return wedge.PartnerManifest{}, fmt.Errorf("partner manifest invalid: %s", strings.Join(details, "; "))
	}
	return manifest, nil
}

func writeRecord(outPath string, record any) error {
	encoded, err := encodeYAML(record)
	if err != nil {
		return fmt.Errorf("encoding decision record: %w", err)
	}
	if dir := filepath.Dir(outPath); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}
	}
	if err := os.WriteFile(outPath, encoded, 0644); err != nil {
		return fmt.Errorf("writing decision record: %w", err)
	}
	return nil
}

func decideGateA(receiptPath, signer, stamp string, priv ed25519.PrivateKey, outPath, partnerLine string, stdout, stderr io.Writer) error {
	raw, err := os.ReadFile(receiptPath)
	if err != nil {
		return fmt.Errorf("reading receipt: %w", err)
	}
	var receipt gateevidence.P1AEvidenceReceipt
	if err := decodeYAML(raw, &receipt); err != nil {
		return fmt.Errorf("decoding receipt: %w", err)
	}
	decision := gateevidence.EvaluateGateADecision(receipt)
	signed, err := gateevidence.SignGateADecision(decision, signer, stamp, priv)
	if err != nil {
		return fmt.Errorf("signing gate A decision: %w", err)
	}
	if err := writeRecord(outPath, signed); err != nil {
		return err
	}
	reloaded, err := os.ReadFile(outPath)
	if err != nil {
		return fmt.Errorf("reloading decision record: %w", err)
	}
	var check gateevidence.GateADecisionRecord
	if err := decodeYAML(reloaded, &check); err != nil {
		return fmt.Errorf("decoding reloaded record: %w", err)
	}
	verified, err := gateevidence.VerifyGateADecision(check)
	if err != nil {
		fmt.Fprintln(stderr, "RELOAD SIGNATURE ERROR:", err)
		return err
	}
	if !verified {
		return fmt.Errorf("reloaded gate A record signature did not verify")
	}
	fmt.Fprintln(stdout, "gate: GATE_A")
	fmt.Fprintf(stdout, "decision: %s\n", signed.Decision)
	fmt.Fprintf(stdout, "evidence_digest: %s\n", signed.EvidenceDigest)
	fmt.Fprintf(stdout, "signature_verified_on_reload: %v\n", verified)
	if partnerLine != "" {
		fmt.Fprintln(stdout, partnerLine)
	}
	return nil
}

func decideGateB(receiptPath, signer, stamp string, evalTime time.Time, priv ed25519.PrivateKey, outPath, partnerLine string, stdout, stderr io.Writer) error {
	raw, err := os.ReadFile(receiptPath)
	if err != nil {
		return fmt.Errorf("reading receipt: %w", err)
	}
	var receipt gateevidence.GateBPilotReceipt
	if err := decodeYAML(raw, &receipt); err != nil {
		return fmt.Errorf("decoding receipt: %w", err)
	}
	decision := gateevidence.EvaluateGateBDecision(receipt, evalTime)
	signed, err := gateevidence.SignGateBDecision(decision, signer, stamp, priv)
	if err != nil {
		return fmt.Errorf("signing gate B decision: %w", err)
	}
	if err := writeRecord(outPath, signed); err != nil {
		return err
	}
	reloaded, err := os.ReadFile(outPath)
	if err != nil {
		return fmt.Errorf("reloading decision record: %w", err)
	}
	var check gateevidence.GateBDecisionRecord
	if err := decodeYAML(reloaded, &check); err != nil {
		return fmt.Errorf("decoding reloaded record: %w", err)
	}
	verified, err := gateevidence.VerifyGateBDecision(check)
	if err != nil {
		fmt.Fprintln(stderr, "RELOAD SIGNATURE ERROR:", err)
		return err
	}
	if !verified {
		return fmt.Errorf("reloaded gate B record signature did not verify")
	}
	fmt.Fprintln(stdout, "gate: GATE_B")
	fmt.Fprintf(stdout, "decision: %s\n", signed.Decision)
	fmt.Fprintf(stdout, "evidence_digest: %s\n", signed.EvidenceDigest)
	fmt.Fprintf(stdout, "signature_verified_on_reload: %v\n", verified)
	if partnerLine != "" {
		fmt.Fprintln(stdout, partnerLine)
	}
	return nil
}

func verifyRecord(gate, path string, stdout, stderr io.Writer) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading decision record: %w", err)
	}
	if gate == "a" {
		var record gateevidence.GateADecisionRecord
		if err := decodeYAML(raw, &record); err != nil {
			return fmt.Errorf("decoding gate A record: %w", err)
		}
		verified, err := gateevidence.VerifyGateADecision(record)
		fmt.Fprintf(stdout, "gate: GATE_A\ndecision: %s\nsignature_verified: %v\n", record.Decision, verified)
		if err != nil {
			fmt.Fprintln(stderr, "SIGNATURE ERROR:", err)
			return err
		}
		if !verified {
			return fmt.Errorf("signature did not verify")
		}
		return nil
	}
	var record gateevidence.GateBDecisionRecord
	if err := decodeYAML(raw, &record); err != nil {
		return fmt.Errorf("decoding gate B record: %w", err)
	}
	verified, err := gateevidence.VerifyGateBDecision(record)
	fmt.Fprintf(stdout, "gate: GATE_B\ndecision: %s\nsignature_verified: %v\n", record.Decision, verified)
	if err != nil {
		fmt.Fprintln(stderr, "SIGNATURE ERROR:", err)
		return err
	}
	if !verified {
		return fmt.Errorf("signature did not verify")
	}
	return nil
}
