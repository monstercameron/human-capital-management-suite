package mapping

import (
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/adapters"
)

// Normalize validates and deep-copies the mapping IR without rewriting its
// null policy. An omitted policy historically behaves as OMIT for missing or
// failed rules, so changing the zero value here would change mapped bytes.
func Normalize(value IR) (IR, error) {
	out := value
	out.Rules = make([]Rule, len(value.Rules))
	for i, rule := range value.Rules {
		out.Rules[i] = rule
		if rule.Lookup != nil {
			out.Rules[i].Lookup = make(map[string]string, len(rule.Lookup))
			for key, mapped := range rule.Lookup {
				out.Rules[i].Lookup[key] = mapped
			}
		}
	}
	if err := out.Validate(); err != nil {
		return IR{}, err
	}
	return out, nil
}

// ExecuteShared lowers and runs each rule through the shared engine. Rules
// are isolated so site null policies retain their legacy row-level behavior
// when one transform fails.
func ExecuteShared(value IR, input map[string]string) (Result, error) {
	normalized, err := Normalize(value)
	if err != nil {
		return Result{}, err
	}
	for _, rule := range normalized.Rules {
		if rule.Op != OpConstant {
			if _, ok := input[rule.Source]; !ok && rule.Null == NullError {
				return Result{}, fmt.Errorf("%w: %s", ErrMissingSource, rule.Source)
			}
		}
	}
	_, err = adapters.LowerConnectivityRules(adapters.ConnectivityRules{
		Version: normalized.Version,
		Rules:   connectivityRules(normalized.Rules),
	})
	if err != nil {
		return Result{}, err
	}
	fields := make([]Field, 0, len(normalized.Rules))
	diags := make([]Diagnostic, 0)
	for _, rule := range normalized.Rules {
		if rule.Op != OpConstant {
			if _, ok := input[rule.Source]; !ok {
				diags = append(diags, Diagnostic{rule.Target, "source.missing", rule.Source})
				if rule.Null == NullError {
					return Result{Diagnostics: diags}, fmt.Errorf("%w: %s", ErrMissingSource, rule.Source)
				}
				if rule.Null == NullDelete {
					fields = append(fields, Field{Target: rule.Target, Deleted: true})
				}
				continue
			}
		}
		lowered, err := adapters.LowerConnectivityRules(adapters.ConnectivityRules{Version: normalized.Version, Rules: connectivityRules([]Rule{rule})})
		if err != nil {
			return Result{}, err
		}
		row := make(map[string]string, 1)
		if rule.Op != OpConstant {
			for _, binding := range lowered.Bindings {
				if binding.SourceKey != "" {
					row[binding.SourceKey] = input[rule.Source]
				}
			}
		}
		rows, runErr := lowered.Run([]map[string]string{row})
		if runErr != nil {
			detail := runErr.Error()
			diags = append(diags, Diagnostic{rule.Target, "transform.failed", detail})
			if rule.Null == NullError {
				return Result{Diagnostics: diags}, fmt.Errorf("%w: %s: %s", ErrTransform, rule.Target, detail)
			}
			if rule.Null == NullDelete {
				fields = append(fields, Field{Target: rule.Target, Deleted: true})
			}
			continue
		}
		texts, err := lowered.Texts(rows[0])
		if err != nil {
			return Result{}, err
		}
		text, ok := texts[rule.Target]
		if !ok {
			continue
		}
		if text == "" {
			if rule.Null == NullError {
				diags = append(diags, Diagnostic{rule.Target, "value.empty", "empty output"})
				return Result{Diagnostics: diags}, fmt.Errorf("%w: %s: empty output", ErrTransform, rule.Target)
			}
			if rule.Null == NullOmit {
				continue
			}
			if rule.Null == NullDelete {
				fields = append(fields, Field{Target: rule.Target, Deleted: true})
				continue
			}
		}
		fields = append(fields, Field{Target: rule.Target, Value: text})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Target < fields[j].Target })
	sort.Slice(diags, func(i, j int) bool {
		if diags[i].Target != diags[j].Target {
			return diags[i].Target < diags[j].Target
		}
		return diags[i].Code < diags[j].Code
	})
	return Result{Fields: fields, Diagnostics: diags, PayloadDigest: digest(normalized, fields)}, nil
}

func connectivityRules(rules []Rule) []adapters.ConnectivityRule {
	out := make([]adapters.ConnectivityRule, len(rules))
	for i, rule := range rules {
		lookup := make(map[string]string, len(rule.Lookup))
		for key, value := range rule.Lookup {
			lookup[key] = value
		}
		out[i] = adapters.ConnectivityRule{
			Source: rule.Source, Target: rule.Target, Op: adapters.ConnectivityOp(rule.Op),
			Argument: rule.Argument, MoneyMode: rule.MoneyMode, Lookup: lookup, Null: adapters.ConnectivityNullPolicy(rule.Null),
		}
	}
	return out
}
