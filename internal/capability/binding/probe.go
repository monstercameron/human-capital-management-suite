package binding

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// ProbeResult is what one in-memory invocation of one published capability
// actually returned. BIND-001's RED clause names the failure this catches
// by name: "a map[string]any handler publishes or starts". A binding table
// that looks correct on paper while every handler answers with an untyped
// map has bound nothing, so the table is checked against a real call rather
// than against the registry's own description of itself.
type ProbeResult struct {
	Capability capability.Key
	// Invoked is false when the gateway refused before reaching a handler.
	Invoked bool
	// RefusalCode is the capability.GatewayError code for a refusal.
	RefusalCode string
	// HandlerError is a non-empty string when the handler itself failed.
	HandlerError string
	// ResultType is the Go type of the response, as %T renders it.
	ResultType string
	// Untyped is true when the response is a map[string]any (or an
	// equivalent untyped map) rather than a domain type: the exact shape
	// BIND-001 refuses.
	Untyped bool
}

// discardSink accepts invocation evidence and drops it. The probe is a type
// check, not an audit: it must not write to a real evidence store, and the
// gateway requires a sink, so this is the smallest honest one.
type discardSink struct{}

func (discardSink) RecordInvocation(context.Context, capability.InvocationEvidence) (string, error) {
	return "binding-probe", nil
}

func (sink discardSink) RecordInvocationTx(ctx context.Context, evt capability.InvocationEvidence) (string, error) {
	return sink.RecordInvocation(ctx, evt)
}

// ProbeResultTypes invokes every capability in reg through the governed
// gateway with a nil payload and an authorization that grants exactly the
// scope the capability publishes, and reports what came back.
//
// It is deliberately not pure — it calls handlers — and it is deliberately
// harmless: the gateway refuses every write-effect capability before
// reaching a handler (CAP-002), and the P1A table is entirely PURE and
// READ_ONLY, so nothing here can mutate state. A capability whose handler
// needs a real payload fails with a handler error, which the result records
// rather than hides.
func ProbeResultTypes(ctx context.Context, reg *capability.Registry) []ProbeResult {
	gateway := capability.NewGateway(reg, discardSink{})
	records := reg.List()

	out := make([]ProbeResult, 0, len(records))
	for _, rec := range records {
		def := rec.Definition
		result := ProbeResult{Capability: def.Key()}
		res, err := gateway.Invoke(ctx, capability.InvokeRequest{
			Capability: def.Key(),
			Payload:    nil,
			Authorization: capability.Authorization{
				Decision:   capability.Allow,
				Scopes:     []string{def.AuthZScopeRef},
				SubjectRef: "binding.probe",
			},
		})
		if err != nil {
			var gwErr *capability.GatewayError
			if errors.As(err, &gwErr) {
				result.RefusalCode = gwErr.Code
				if gwErr.Code == capability.CodeHandlerFailed {
					result.Invoked = true
					result.HandlerError = gwErr.Reason
				}
			} else {
				result.HandlerError = err.Error()
			}
			out = append(out, result)
			continue
		}
		result.Invoked = true
		result.ResultType = fmt.Sprintf("%T", res.Response)
		_, untyped := res.Response.(map[string]any)
		result.Untyped = untyped
		out = append(out, result)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Capability.String() < out[j].Capability.String() })
	return out
}

// UntypedResultCapabilities returns the capability keys whose probe came
// back as an untyped map, sorted.
func UntypedResultCapabilities(results []ProbeResult) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		if r.Untyped {
			out = append(out, r.Capability.String())
		}
	}
	sort.Strings(out)
	return out
}
