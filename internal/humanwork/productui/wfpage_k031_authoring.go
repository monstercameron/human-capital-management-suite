package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/documents"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

// WorkflowPageRuleLibraryEntry is a company-owned validation rule that can
// be selected by an author without copying executable logic into a page.
type WorkflowPageRuleLibraryEntry struct {
	ID   string
	Rule pagedef.WorkflowPageRule
}

type WorkflowPageRuleEvaluation struct {
	Rule          pagedef.WorkflowPageRule
	Passed        bool
	Message       string
	CoveredFields []string
}

// EditWorkflowPageRule adds or replaces one typed rule, validates all of its
// paths against the compiled input surface, and evaluates it against sample
// values before returning the new page definition.
func EditWorkflowPageRule(page pagedef.WorkflowPageDefinition, plan *workflow.CompiledWorkflow, library []WorkflowPageRuleLibraryEntry, libraryID string, rule pagedef.WorkflowPageRule, sample map[string]string) (pagedef.WorkflowPageDefinition, WorkflowPageRuleEvaluation, error) {
	if strings.TrimSpace(libraryID) != "" {
		found := false
		for _, entry := range library {
			if entry.ID == libraryID {
				rule = entry.Rule
				found = true
				break
			}
		}
		if !found {
			return pagedef.WorkflowPageDefinition{}, WorkflowPageRuleEvaluation{}, fmt.Errorf("productui: rule library entry %q was not found", libraryID)
		}
	}
	if strings.TrimSpace(rule.ID) == "" || strings.TrimSpace(rule.Message) == "" {
		return pagedef.WorkflowPageDefinition{}, WorkflowPageRuleEvaluation{}, fmt.Errorf("productui: rule id and user message are required")
	}
	updated := cloneWorkflowPageDefinition(page)
	updated.Rules = append(updated.Rules, rule)
	violations := updated.ValidateAgainst(plan)
	for _, violation := range violations {
		if strings.HasPrefix(violation.Path, fmt.Sprintf("rules[%d]", len(updated.Rules)-1)) {
			return pagedef.WorkflowPageDefinition{}, WorkflowPageRuleEvaluation{}, fmt.Errorf("productui: rule refused at %s: %s", violation.Path, violation.Reason)
		}
	}
	return updated, WorkflowPageRuleEvaluation{Rule: rule, Passed: evaluateWorkflowPageCondition(rule.When, sample), Message: rule.Message, CoveredFields: conditionFields(rule.When)}, nil
}

// EvaluateWorkflowPageRule is the small live-evaluation projection used by
// the editor and preview. It reads only the declared sample map.
func EvaluateWorkflowPageRule(rule pagedef.WorkflowPageRule, sample map[string]string) WorkflowPageRuleEvaluation {
	return WorkflowPageRuleEvaluation{Rule: rule, Passed: evaluateWorkflowPageCondition(rule.When, sample), Message: rule.Message, CoveredFields: conditionFields(rule.When)}
}

// WorkflowPageLocalizedMarkdown is the localized copy edited by the shared
// Documents Markdown editor. Every release locale is required before a page
// can be published.
type WorkflowPageLocalizedMarkdown struct {
	ENUS string
	DEDE string
	AR   string
}

type WorkflowPageContentEdit struct {
	ID        string
	SectionID string
	Kind      string
	TextKey   string
	Copy      WorkflowPageLocalizedMarkdown
}

// AddWorkflowPageContentBlock is the typed content-block editor. The page
// schema receives only a text key; localized Markdown stays in the governed
// copy projection and cannot become HTML in the page definition.
func AddWorkflowPageContentBlock(page *pagedef.WorkflowPageDefinition, edit WorkflowPageContentEdit) error {
	if page == nil || strings.TrimSpace(edit.ID) == "" || strings.TrimSpace(edit.SectionID) == "" || strings.TrimSpace(edit.TextKey) == "" {
		return fmt.Errorf("productui: content block id, section, and text key are required")
	}
	if edit.Kind != WorkflowPageNote && edit.Kind != WorkflowPageGuide && edit.Kind != WorkflowPageChecklist {
		return fmt.Errorf("productui: unsupported workflow page content kind %q", edit.Kind)
	}
	for _, section := range page.Sections {
		if section.ID == edit.SectionID {
			if err := ValidateWorkflowPageLocalizedMarkdown(edit.Copy); err != nil {
				return err
			}
			page.ContentBlocks = append(page.ContentBlocks, pagedef.WorkflowPageContentBlock{ID: edit.ID, SectionID: edit.SectionID, Kind: edit.Kind, TextKey: edit.TextKey})
			return nil
		}
	}
	return fmt.Errorf("productui: content block section %q was not found", edit.SectionID)
}

func EditWorkflowPageMarkdown(copy *WorkflowPageLocalizedMarkdown, locale, markdown string) error {
	if copy == nil || strings.TrimSpace(markdown) == "" {
		return fmt.Errorf("productui: localized Markdown cannot be blank")
	}
	if strings.Contains(strings.ToLower(markdown), "javascript:") {
		return fmt.Errorf("productui: unsafe link in localized Markdown")
	}
	switch strings.ToLower(strings.TrimSpace(locale)) {
	case "en-us":
		copy.ENUS = markdown
	case "de-de":
		copy.DEDE = markdown
	case "ar":
		copy.AR = markdown
	default:
		return fmt.Errorf("productui: unsupported Markdown locale %q", locale)
	}
	return nil
}

func ValidateWorkflowPageLocalizedMarkdown(copy WorkflowPageLocalizedMarkdown) error {
	for _, localized := range []struct {
		locale   string
		markdown string
	}{{"en-US", copy.ENUS}, {"de-DE", copy.DEDE}, {"ar", copy.AR}} {
		locale, markdown := localized.locale, localized.markdown
		if strings.TrimSpace(markdown) == "" {
			return fmt.Errorf("productui: missing %s localized Markdown", locale)
		}
		if strings.Contains(strings.ToLower(markdown), "javascript:") {
			return fmt.Errorf("productui: unsafe link in %s localized Markdown", locale)
		}
	}
	return nil
}

// PickWorkflowPageSOP delegates version selection and authorization to the
// Documents link picker. The page stores a document reference, never a URL.
func PickWorkflowPageSOP(slot documents.WorkflowDocumentLinkSlot, catalog []documents.WorkflowDocument) (documents.WorkflowDocumentLink, error) {
	return documents.ResolveWorkflowDocumentLink(slot, catalog)
}

type WorkflowPageSupportBinding struct {
	ChannelID string
	Label     string
	Href      string
	Eligible  bool
}

func ValidateWorkflowPageSupportBinding(binding WorkflowPageSupportBinding) error {
	if strings.TrimSpace(binding.ChannelID) == "" || strings.TrimSpace(binding.Label) == "" {
		return fmt.Errorf("productui: support channel id and label are required")
	}
	if binding.Eligible && strings.TrimSpace(binding.Href) == "" {
		return fmt.Errorf("productui: eligible support channel requires a governed href")
	}
	if strings.ContainsAny(binding.Href, "\r\n<>") {
		return fmt.Errorf("productui: support channel href contains markup")
	}
	return nil
}

func evaluateWorkflowPageCondition(condition pagedef.WorkflowCondition, sample map[string]string) bool {
	left, present := sample[condition.Path]
	right := condition.Value
	if condition.Path2 != "" {
		right = sample[condition.Path2]
	}
	switch condition.Op {
	case pagedef.RuleEquals:
		return present && left == right
	case pagedef.RuleNotEquals:
		return present && left != right
	case pagedef.RuleExists:
		return present
	case pagedef.RuleNotExists:
		return !present
	case pagedef.RuleIn:
		if !present {
			return false
		}
		for _, candidate := range strings.Split(condition.Value, ",") {
			if strings.TrimSpace(candidate) == left {
				return true
			}
		}
	}
	return false
}

func conditionFields(condition pagedef.WorkflowCondition) []string {
	fields := []string{condition.Path}
	if condition.Path2 != "" {
		fields = append(fields, condition.Path2)
	}
	return fields
}

func cloneWorkflowPageDefinition(page pagedef.WorkflowPageDefinition) pagedef.WorkflowPageDefinition {
	page.Sections = append([]pagedef.WorkflowPageSection(nil), page.Sections...)
	page.Steps = append([]pagedef.WorkflowPageStep(nil), page.Steps...)
	page.Widgets = append([]pagedef.WorkflowPageWidget(nil), page.Widgets...)
	page.Rules = append([]pagedef.WorkflowPageRule(nil), page.Rules...)
	page.ContentBlocks = append([]pagedef.WorkflowPageContentBlock(nil), page.ContentBlocks...)
	page.LinkSlots = append([]pagedef.WorkflowPageLinkSlot(nil), page.LinkSlots...)
	page.Visibility = append([]pagedef.WorkflowPageVisibility(nil), page.Visibility...)
	page.BrandTokens = append([]string(nil), page.BrandTokens...)
	return page
}
