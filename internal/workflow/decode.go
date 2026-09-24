package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type planDecoder func([]byte) (*CompiledWorkflow, error)

// decoderTable is deliberately closed: a runtime can only interpret schemas
// it has an explicit decoder for. The map belongs to the lookup call so no
// mutable package-level registry is exposed.
func decoderTable() map[uint32]planDecoder {
	return map[uint32]planDecoder{
		1:                      decodePlanV1,
		CurrentIRSchemaVersion: decodePlanV2,
	}
}

// DecodeCanonicalPlan rehydrates a compiled plan from the deterministic JSON
// bytes a version record stores (version.CanonicalPlanBytes). The digest is
// recomputed from the decoded content so the returned plan verifies and pins
// exactly like the plan it was rendered from. It is the only way a value this
// package did not compile in-process gets a digest.
func DecodeCanonicalPlan(data []byte) (*CompiledWorkflow, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("workflow: decode canonical plan: no bytes supplied")
	}
	version, _, err := readIRSchemaVersion(data)
	if err != nil {
		return nil, fmt.Errorf("workflow: decode canonical plan: %w", err)
	}
	decoder, supported := decoderTable()[version]
	if !supported {
		return nil, fmt.Errorf("workflow: decode canonical plan: unsupported IR schema version %d", version)
	}
	return decoder(data)
}

func decodePlanV1(data []byte) (*CompiledWorkflow, error) {
	version, present, err := readIRSchemaVersion(data)
	if err != nil {
		return nil, fmt.Errorf("workflow: decode canonical plan v1: %w", err)
	}
	if present && version != 1 {
		return nil, fmt.Errorf("workflow: decode canonical plan v1: version field is %d", version)
	}
	plan, err := decodePlanData(data)
	if err != nil {
		return nil, fmt.Errorf("workflow: decode canonical plan v1: %w", err)
	}
	plan.IRSchemaVersion = 0 // normalize old and explicit v1 wire forms
	return finishDecodedPlan(&plan, 1)
}

func decodePlanV2(data []byte) (*CompiledWorkflow, error) {
	version, present, err := readIRSchemaVersion(data)
	if err != nil {
		return nil, fmt.Errorf("workflow: decode canonical plan v2: %w", err)
	}
	if !present || version != CurrentIRSchemaVersion {
		return nil, fmt.Errorf("workflow: decode canonical plan v2: missing or invalid schema version")
	}
	plan, err := decodePlanData(data)
	if err != nil {
		return nil, fmt.Errorf("workflow: decode canonical plan v2: %w", err)
	}
	return finishDecodedPlan(&plan, CurrentIRSchemaVersion)
}

func readIRSchemaVersion(data []byte) (uint32, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	open, err := decoder.Token()
	if err != nil {
		return 0, false, err
	}
	if open != json.Delim('{') {
		return 0, false, fmt.Errorf("compiled plan must be a JSON object")
	}
	var raw json.RawMessage
	present := false
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return 0, false, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return 0, false, fmt.Errorf("compiled plan contains a non-string object key")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return 0, false, err
		}
		if !strings.EqualFold(key, "ir_schema_version") {
			continue
		}
		if key != "ir_schema_version" {
			return 0, true, fmt.Errorf("IR schema field has non-canonical spelling %q", key)
		}
		if present {
			return 0, true, fmt.Errorf("duplicate IR schema version field")
		}
		raw, present = value, true
	}
	close, err := decoder.Token()
	if err != nil {
		return 0, false, err
	}
	if close != json.Delim('}') {
		return 0, false, fmt.Errorf("compiled plan has an invalid object terminator")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return 0, false, fmt.Errorf("trailing JSON value")
		}
		return 0, false, err
	}
	if !present {
		return 1, false, nil // frozen v1 plans predate the explicit field
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, true, fmt.Errorf("IR schema version is null")
	}
	var version uint32
	if err := json.Unmarshal(raw, &version); err != nil {
		return 0, true, fmt.Errorf("IR schema version is invalid: %w", err)
	}
	return version, true, nil
}

func decodePlanData(data []byte) (CompiledWorkflow, error) {
	var plan CompiledWorkflow
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return CompiledWorkflow{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return CompiledWorkflow{}, fmt.Errorf("trailing JSON value")
		}
		return CompiledWorkflow{}, err
	}
	return plan, nil
}

func finishDecodedPlan(plan *CompiledWorkflow, version uint32) (*CompiledWorkflow, error) {
	if plan.WorkflowID == "" {
		return nil, fmt.Errorf("decoded plan names no workflow")
	}
	if plan.SchemaVersion() != version {
		return nil, fmt.Errorf("decoded plan schema version %d differs from selected decoder %d", plan.SchemaVersion(), version)
	}
	plan.digest = computePlanDigest(plan)
	return plan, nil
}
