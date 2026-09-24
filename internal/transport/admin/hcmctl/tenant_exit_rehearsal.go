package hcmctl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	exit "github.com/monstercameron/human-capital-management-suite/internal/governance/exit"
)

const maxExitRehearsalInput = 4 << 20

func parseTenant(g globalFlags, args []string) (parsedCommand, error) {
	if len(args) == 0 || args[0] != "exit-rehearsal" {
		return parsedCommand{}, fmt.Errorf("hcmctl: tenant requires the exit-rehearsal action")
	}
	sub := newSubFlagSet("tenant exit-rehearsal")
	tenant := sub.String("tenant", "", "tenant id in the rehearsal evidence")
	input := sub.String("file", "", "JSON file containing the complete rehearsal evidence")
	if err := sub.Parse(args[1:]); err != nil {
		return parsedCommand{}, err
	}
	if strings.TrimSpace(*tenant) == "" || strings.TrimSpace(*input) == "" {
		return parsedCommand{}, fmt.Errorf("hcmctl: tenant exit-rehearsal requires -tenant and -file")
	}
	return parsedCommand{global: g, runLocal: func() (string, error) {
		return runTenantExitRehearsal(*tenant, *input)
	}}, nil
}

func runTenantExitRehearsal(tenant, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("hcmctl: open rehearsal evidence: %w", err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxExitRehearsalInput+1))
	if err != nil {
		return "", fmt.Errorf("hcmctl: read rehearsal evidence: %w", err)
	}
	if len(data) > maxExitRehearsalInput {
		return "", fmt.Errorf("hcmctl: rehearsal evidence exceeds %d bytes", maxExitRehearsalInput)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var request exit.RehearsalRequest
	if err := decoder.Decode(&request); err != nil {
		return "", fmt.Errorf("hcmctl: decode rehearsal evidence: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return "", fmt.Errorf("hcmctl: rehearsal evidence must contain one JSON object")
		}
		return "", fmt.Errorf("hcmctl: decode trailing rehearsal evidence: %w", err)
	}
	if request.Tenant != tenant {
		return "", fmt.Errorf("hcmctl: requested tenant does not match rehearsal evidence")
	}
	receipt, err := exit.Rehearse(request)
	if err != nil {
		return "", fmt.Errorf("hcmctl: rehearse tenant exit: %w", err)
	}
	raw, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return "", fmt.Errorf("hcmctl: render rehearsal receipt: %w", err)
	}
	return receipt.Explain() + "\nverdict: " + receipt.Status + "\n" + string(raw) + "\n", nil
}
