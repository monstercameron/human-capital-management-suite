package simulate

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation"
	xformexec "github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/exec"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/rulepayload"
)

// PublishedTransforms executes a bounded XFORM program pinned by the compiled
// TRANSFORM node. A legacy transform without ProgramRef is refused here.
type PublishedTransforms struct{ Payloads PublishedRuleResolver }

func (t PublishedTransforms) Transform(ctx context.Context, req TransformRequest) (result TransformResult, retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.simulate.published_transform", req)
	defer func() { observe.Done(op, retErr) }()
	pin := req.Transform.Program
	if t.Payloads == nil || pin == nil || pin.Kind != workflow.RefTransform || pin.Digest == "" || pin.Status == workflow.ReferenceRetired {
		return TransformResult{}, refuse(CodeHandlerFailed, req.NodeID, "TRANSFORM has no exact published XFORM program pin")
	}
	payload, err := t.Payloads.Resolve(workflow.Reference{Kind: workflow.RefTransform, ID: pin.ID, Version: pin.Version}, pin.Digest)
	if err != nil {
		return TransformResult{}, wrap(CodeHandlerFailed, req.NodeID, err, "resolve published transform")
	}
	if payload.Kind != rulepayload.KindTransform || payload.Transform == nil {
		return TransformResult{}, refuse(CodeHandlerFailed, req.NodeID, "published transform reference does not contain XFORM IR")
	}
	programDigest, err := payload.Transform.Digest()
	if err != nil || programDigest != pin.Digest {
		return TransformResult{}, refuse(CodeHandlerFailed, req.NodeID, "published XFORM program digest is invalid")
	}
	input := make(xformexec.Record, len(req.Inputs))
	needed := make(map[string]bool, len(payload.Transform.Dependencies))
	for _, dependency := range payload.Transform.Dependencies {
		needed[dependency] = true
	}
	for name, value := range req.Inputs {
		key := "workflow." + name
		if !needed[key] {
			continue
		}
		typ, data, err := xformInput(value)
		if err != nil {
			return TransformResult{}, wrap(CodeHandlerFailed, req.NodeID, err, "map transform input %s", name)
		}
		input[key] = xformexec.Present(typ, data)
	}
	inputBytesRaw, err := json.Marshal(input)
	if err != nil {
		return TransformResult{}, wrap(CodeHandlerFailed, req.NodeID, err, "encode transform input")
	}
	inputBytes := uint64(len(inputBytesRaw))
	if req.Transform.Limits.MaxInputBytes > 0 && inputBytes > req.Transform.Limits.MaxInputBytes {
		return TransformResult{}, refuse(CodeHandlerFailed, req.NodeID, "transform input count exceeds the compiled limit")
	}
	rows, err := xformexec.Execute(*payload.Transform, []xformexec.Record{input}, xformexec.Limits{
		MaxRows: 1, MaxSteps: int(req.Transform.Limits.MaxSteps), MaxOutputBytes: int64(req.Transform.Limits.MaxOutputBytes),
	})
	if err != nil {
		return TransformResult{}, wrap(CodeHandlerFailed, req.NodeID, err, "execute published XFORM program")
	}
	outputs := make(Bag, len(req.Outputs))
	for _, field := range req.Outputs {
		key := "workflow." + field.Path
		value, ok := rows[0][key]
		if !ok || value.State != values.PresenceValue {
			return TransformResult{Outcome: workflow.OutcomeUnknown, Outputs: Bag{}, Detail: "published XFORM output " + field.Path + " is absent or unknown"}, nil
		}
		wantType, err := xformType(field.Type.Kind)
		if err != nil || value.Type != wantType {
			return TransformResult{}, refuse(CodeHandlerFailed, req.NodeID, "published XFORM output %s has type %s, incompatible with declared type %s", field.Path, value.Type, field.Type.Kind)
		}
		text, err := xformOutput(value)
		if err != nil {
			return TransformResult{}, wrap(CodeHandlerFailed, req.NodeID, err, "map transform output %s", field.Path)
		}
		outputs[field.Path] = Value{Type: field.Type, Text: text}
	}
	return TransformResult{Outcome: workflow.OutcomeSucceeded, Outputs: outputs, Detail: "published XFORM program " + programDigest}, nil
}

func xformType(kind workflow.Kind) (transformation.Type, error) {
	switch kind {
	case workflow.KindString, workflow.KindEnum:
		return transformation.TypeString, nil
	case workflow.KindBool:
		return transformation.TypeBool, nil
	case workflow.KindInteger:
		return transformation.TypeInt, nil
	case workflow.KindDecimal:
		return transformation.TypeDecimal, nil
	case workflow.KindLocalDate:
		return transformation.TypeDate, nil
	case workflow.KindInstant:
		return transformation.TypeTimestamp, nil
	default:
		return "", fmt.Errorf("workflow value type %s is unsupported by XFORM IR", kind)
	}
}

func xformInput(value Value) (transformation.Type, any, error) {
	switch value.Type.Kind {
	case workflow.KindString, workflow.KindEnum:
		return transformation.TypeString, value.Text, nil
	case workflow.KindBool:
		b, err := strconv.ParseBool(value.Text)
		return transformation.TypeBool, b, err
	case workflow.KindInteger:
		n, err := strconv.ParseInt(value.Text, 10, 64)
		return transformation.TypeInt, n, err
	case workflow.KindDecimal:
		return transformation.TypeDecimal, value.Text, nil
	case workflow.KindLocalDate:
		return transformation.TypeDate, value.Text, nil
	case workflow.KindInstant:
		return transformation.TypeTimestamp, value.Text, nil
	default:
		return "", nil, fmt.Errorf("workflow value type %s is unsupported by XFORM IR", value.Type.Kind)
	}
}

func xformOutput(value xformexec.Value) (string, error) {
	if value.State != values.PresenceValue {
		return "", fmt.Errorf("transform output state is %s", value.State)
	}
	switch value.Type {
	case transformation.TypeString, transformation.TypeDecimal, transformation.TypeDate, transformation.TypeTimestamp:
		text, ok := value.Data.(string)
		if !ok {
			return "", fmt.Errorf("XFORM output %s is not canonical text", value.Type)
		}
		return text, nil
	case transformation.TypeBool:
		b, ok := value.Data.(bool)
		if !ok {
			return "", fmt.Errorf("XFORM output %s is not boolean", value.Type)
		}
		return strconv.FormatBool(b), nil
	case transformation.TypeInt:
		n, ok := value.Data.(int64)
		if !ok {
			return "", fmt.Errorf("XFORM output %s is not int64", value.Type)
		}
		return strconv.FormatInt(n, 10), nil
	default:
		return "", fmt.Errorf("XFORM output type %s is unsupported", value.Type)
	}
}
