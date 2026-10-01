package configuration

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

var (
	ErrValidationRule    = errors.New("configuration: invalid validation rule")
	ErrValidationTenant  = errors.New("configuration: tenant validation rule not found")
	ErrValidationVersion = errors.New("configuration: validation rule version not found")
)

type ValidationParameter struct {
	Key  string
	Type workflow.ValueType
}

type ValidationRule struct {
	TenantID   string
	Name       string
	Version    int
	Rule       workflow.PageRule
	Parameters []ValidationParameter
	Digest     string
}

type ValidationPageUse struct {
	PageID      string
	RuleName    string
	RuleVersion int
}

type ValidationImpact struct {
	Rule     ValidationRule
	PageUses []ValidationPageUse
}

type ValidationLibrary struct {
	mu    sync.RWMutex
	rules map[string]map[string][]ValidationRule
	uses  map[string]map[string]map[string]ValidationPageUse
}

func NewValidationLibrary() *ValidationLibrary {
	return &ValidationLibrary{rules: make(map[string]map[string][]ValidationRule), uses: make(map[string]map[string]map[string]ValidationPageUse)}
}

// Publish creates a new immutable version. Existing page bindings retain the
// old version until explicitly rebound and republished.
func (l *ValidationLibrary) Publish(tenantID, name string, rule workflow.PageRule, parameters []ValidationParameter) (ValidationRule, error) {
	if l == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(name) == "" {
		return ValidationRule{}, ErrValidationRule
	}
	parameterTypes := make(map[string]workflow.ValueType, len(parameters))
	for _, parameter := range parameters {
		if strings.TrimSpace(parameter.Key) == "" || parameter.Type.Validate() != nil {
			return ValidationRule{}, ErrValidationRule
		}
		if _, duplicate := parameterTypes[parameter.Key]; duplicate {
			return ValidationRule{}, ErrValidationRule
		}
		parameterTypes[parameter.Key] = parameter.Type
	}
	if rule.Parameters == nil {
		rule.Parameters = make(map[string]workflow.ValueType)
	}
	for key, typ := range rule.Parameters {
		if declared, ok := parameterTypes[key]; !ok || declared.String() != typ.String() {
			return ValidationRule{}, fmt.Errorf("%w: parameter %s is not typed by the library", ErrValidationRule, key)
		}
	}
	for key, typ := range parameterTypes {
		if _, declared := rule.Parameters[key]; !declared {
			rule.Parameters[key] = typ
		}
	}
	if _, err := rule.Compile(); err != nil {
		return ValidationRule{}, fmt.Errorf("%w: rule: %v", ErrValidationRule, err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.rules == nil {
		l.rules = make(map[string]map[string][]ValidationRule)
		l.uses = make(map[string]map[string]map[string]ValidationPageUse)
	}
	if l.rules[tenantID] == nil {
		l.rules[tenantID] = make(map[string][]ValidationRule)
	}
	version := len(l.rules[tenantID][name]) + 1
	copyRule := cloneValidationRule(rule, tenantID, name, version, parameters)
	l.rules[tenantID][name] = append(l.rules[tenantID][name], copyRule)
	return cloneValidationRuleValue(copyRule), nil
}

func (l *ValidationLibrary) Get(tenantID, name string, version int) (ValidationRule, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	versions := l.rules[tenantID][name]
	if len(versions) == 0 {
		return ValidationRule{}, ErrValidationTenant
	}
	if version <= 0 || version > len(versions) {
		return ValidationRule{}, ErrValidationVersion
	}
	return cloneValidationRuleValue(versions[version-1]), nil
}

func (l *ValidationLibrary) BindPage(tenantID, pageID, name string, version int) error {
	if l == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(pageID) == "" {
		return ErrValidationRule
	}
	rule, err := l.Get(tenantID, name, version)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.uses[tenantID] == nil {
		l.uses[tenantID] = make(map[string]map[string]ValidationPageUse)
	}
	if l.uses[tenantID][name] == nil {
		l.uses[tenantID][name] = make(map[string]ValidationPageUse)
	}
	l.uses[tenantID][name][pageID] = ValidationPageUse{PageID: pageID, RuleName: rule.Name, RuleVersion: rule.Version}
	return nil
}

func (l *ValidationLibrary) PageUses(tenantID, name string) []ValidationPageUse {
	l.mu.RLock()
	defer l.mu.RUnlock()
	byPage := l.uses[tenantID][name]
	out := make([]ValidationPageUse, 0, len(byPage))
	for _, use := range byPage {
		out = append(out, use)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PageID < out[j].PageID })
	return out
}

func (l *ValidationLibrary) ImpactBeforeChange(tenantID, name string, nextVersion int) (ValidationImpact, error) {
	rule, err := l.Get(tenantID, name, nextVersion)
	if err != nil {
		return ValidationImpact{}, err
	}
	return ValidationImpact{Rule: rule, PageUses: l.PageUses(tenantID, name)}, nil
}

func cloneValidationRule(rule workflow.PageRule, tenantID, name string, version int, parameters []ValidationParameter) ValidationRule {
	rule.Parameters = make(map[string]workflow.ValueType, len(parameters))
	copied := make([]ValidationParameter, len(parameters))
	copy(copied, parameters)
	for _, parameter := range parameters {
		rule.Parameters[parameter.Key] = parameter.Type
	}
	return ValidationRule{TenantID: tenantID, Name: name, Version: version, Rule: rule, Parameters: copied, Digest: rule.Digest()}
}

func cloneValidationRuleValue(rule ValidationRule) ValidationRule {
	if compiled, err := rule.Rule.Compile(); err == nil {
		rule.Rule = compiled.Definition()
	}
	rule.Parameters = append([]ValidationParameter(nil), rule.Parameters...)
	return rule
}
