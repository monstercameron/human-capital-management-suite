package productui

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
)

// TestTodo_CHATUX_046_MarkersPinned pins the bundle-size optimization behind
// CHATUX-046: every type below carries the incomparable marker, so the
// compiler builds no equality function for it in the browser module. A type
// that regains comparability (marker removed, or a change that needs ==)
// fails here by name; remove its entry only together with the marker, and
// only when ==, a map key or a comparable constraint genuinely needs it.
func TestTodo_CHATUX_046_MarkersPinned(t *testing.T) {
	marked := map[string]any{
		"AccessibilityPreferences":       AccessibilityPreferences{},
		"AccessibilityOption":            AccessibilityOption{},
		"AgentScheduleDraft":             AgentScheduleDraft{},
		"AgentRolloutInstallation":       AgentRolloutInstallation{},
		"AgentPortableReviewDraft":       AgentPortableReviewDraft{},
		"AgentRolloutVersion":            AgentRolloutVersion{},
		"AgentPortableManifest":          AgentPortableManifest{},
		"AgentPortableDestination":       AgentPortableDestination{},
		"AgentSpendLimitsProps":          AgentSpendLimitsProps{},
		"AgentRequestDocumentSuggestion": AgentRequestDocumentSuggestion{},
		"AgentAnnouncementAttempt":       AgentAnnouncementAttempt{},
		"ApprovalDispositionProjection":  ApprovalDispositionProjection{},
		"BrandLogoProps":                 BrandLogoProps{},
		"BrandAssetOption":               BrandAssetOption{},
		"chatRetentionDraft":             chatRetentionDraft{},
		"personAvatarProps":              personAvatarProps{},
		"EmptyStateProps":                EmptyStateProps{},
		"SectionHeadingProps":            SectionHeadingProps{},
		"DocumentChatMessageReference":   DocumentChatMessageReference{},
		"DocsReferenceSuggestion":        DocsReferenceSuggestion{},
		"docsNoticeValue":                docsNoticeValue{},
		"DocsJourneyPreview":             DocsJourneyPreview{},
		"docsLibraryRoute":               docsLibraryRoute{},
		"DocumentAccessEntry":            DocumentAccessEntry{},
		"docsFolderNameFieldProps":       docsFolderNameFieldProps{},
		"DocumentAttachment":             DocumentAttachment{},
		"docsAttachmentImageProps":       docsAttachmentImageProps{},
		"docsAttachmentCardProps":        docsAttachmentCardProps{},
		"DocumentSummary":                DocumentSummary{},
		"DocumentComment":                DocumentComment{},
		"DocumentReviewProjection":       DocumentReviewProjection{},
		"DocumentSearchResult":           DocumentSearchResult{},
		"DocumentCommentCreateRequest":   DocumentCommentCreateRequest{},
		"DocumentSearchFilters":          DocumentSearchFilters{},
		"DocumentBacklink":               DocumentBacklink{},
		"DocumentVersionProjection":      DocumentVersionProjection{},
		"DocumentVersionSummary":         DocumentVersionSummary{},
		"DocumentEditRequest":            DocumentEditRequest{},
		"DocsProjectTaskPreview":         DocsProjectTaskPreview{},
		"FederationEntry":                FederationEntry{},
		"PageIdentity":                   PageIdentity{},
		"LocaleContext":                  LocaleContext{},
		"InsightsEvidenceProps":          InsightsEvidenceProps{},
		"ManagerCompensationRange":       ManagerCompensationRange{},
		"ManagerCompensationComponent":   ManagerCompensationComponent{},
		"Person":                         Person{},
		"personNormalizedIndex":          personNormalizedIndex{},
		"ViewerProfile":                  ViewerProfile{},
		"KnowledgeSearchResult":          KnowledgeSearchResult{},
		"RoleFeaturePermission":          RoleFeaturePermission{},
		"OrganizationSummaryProps":       OrganizationSummaryProps{},
		"PaySummaryProjection":           PaySummaryProjection{},
		"PayStatementRow":                PayStatementRow{},
		"PayValueProjection":             PayValueProjection{},
		"PeopleQuickActionProps":         PeopleQuickActionProps{},
		"PeopleSummaryProps":             PeopleSummaryProps{},
		"PersonHeroProps":                PersonHeroProps{},
		"PositionPickerOptionProps":      PositionPickerOptionProps{},
		"PositionOptionProjection":       PositionOptionProjection{},
		"PositionObjectProjection":       PositionObjectProjection{},
		"ProjectSummaryProjection":       ProjectSummaryProjection{},
		"projectListRow":                 projectListRow{},
		"PromotionRefusalViolation":      PromotionRefusalViolation{},
		"ProvenanceProjection":           ProvenanceProjection{},
		"StatusProjection":               StatusProjection{},
		"ViewerProfileProps":             ViewerProfileProps{},
		"TimeExceptionRow":               TimeExceptionRow{},
		"missingPunchCopy":               missingPunchCopy{},
		"MissingPunchReviewView":         MissingPunchReviewView{},
		"MissingPunchSessionView":        MissingPunchSessionView{},
		"MissingPunchReceipt":            MissingPunchReceipt{},
		"MissingPunchSubmission":         MissingPunchSubmission{},
		"MissingPunchDecision":           MissingPunchDecision{},
		"ClockProjection":                ClockProjection{},
		"ThemeToken":                     ThemeToken{},
		"UtilityDrawerItem":              UtilityDrawerItem{},
		"PolicyRuleProjection":           PolicyRuleProjection{},
		"ConfigurationSectionProjection": ConfigurationSectionProjection{},
		"workflowListRow":                workflowListRow{},
		"ValidationIssue":                ValidationIssue{},
		"PerformanceBudgetProjection":    PerformanceBudgetProjection{},
		"IntegrationOperationProjection": IntegrationOperationProjection{},
		"WorkflowHistoryRun":             WorkflowHistoryRun{},
		"ApprovalDispositionCardProps":   ApprovalDispositionCardProps{},
		"WorkerAction":                   WorkerAction{},
		"WorkerFact":                     WorkerFact{},
		"WorkflowDraftSemanticChange":    WorkflowDraftSemanticChange{},
		"WorkflowTemplateOverlayChange":  WorkflowTemplateOverlayChange{},
		"WorkflowTemplateOverlay":        WorkflowTemplateOverlay{},
		"WorkflowDraftCreateRequest":     WorkflowDraftCreateRequest{},
		"WorkflowInputBindingChange":     WorkflowInputBindingChange{},
		"WorkflowNotification":           WorkflowNotification{},
		"WorkflowPaletteItem":            WorkflowPaletteItem{},
	}
	for name, value := range marked {
		if reflect.TypeOf(value).Comparable() {
			t.Errorf("%s is comparable; its incomparable marker is gone and its equality function is back in the browser module", name)
		}
	}
}

// chatux046Rule is one qualified rule in a served stylesheet: the at-rule
// context it sits in (empty at the top level), its selector and its
// declarations, with the byte offset where the selector starts.
type chatux046Rule struct {
	context      string
	selector     string
	declarations string
	offset       int
}

// chatux046Part names one concatenated segment of a served sheet and whether
// this package owns its bytes. Duplicates are only removable from owned
// segments: chatui and projectui segments belong to sibling owners.
type chatux046Part struct {
	name  string
	text  string
	owned bool
}

// chatux046Normalize collapses every whitespace run to one space and drops a
// trailing semicolon, so textually identical rules compare equal. It does not
// case-fold and does not reorder declarations: only rules that are provably
// the same rule match.
func chatux046Normalize(text string) string {
	collapsed := strings.Join(strings.Fields(text), " ")
	return strings.TrimSuffix(collapsed, ";")
}

// chatux046ParseRules reads every qualified rule out of a stylesheet,
// tracking the at-rule context and the selector offset of each. It skips
// comments, respects quoted strings and nested blocks, and tolerates
// semicolon-terminated at-rules. It reports false when the sheet does not
// parse, naming the offset.
func chatux046ParseRules(sheet string, rules *[]chatux046Rule) (bool, string) {
	parser := &chatux046Parser{sheet: sheet, rules: rules}
	parser.block(nil)
	if parser.failed != "" {
		from := parser.pos - 80
		if from < 0 {
			from = 0
		}
		until := parser.pos + 80
		if until > len(sheet) {
			until = len(sheet)
		}
		return false, parser.failed + " at byte " + strconv.Itoa(parser.pos) + ": " + snippet(sheet[from:until])
	}
	return true, ""
}

type chatux046Parser struct {
	sheet  string
	pos    int
	rules  *[]chatux046Rule
	failed string
}

func (p *chatux046Parser) skipTrivia() {
	for p.pos < len(p.sheet) {
		switch {
		case p.sheet[p.pos] == '/' && p.pos+1 < len(p.sheet) && p.sheet[p.pos+1] == '*':
			end := strings.Index(p.sheet[p.pos+2:], "*/")
			if end < 0 {
				p.failed = "unterminated comment"
				p.pos = len(p.sheet)
				return
			}
			p.pos += end + 4
		case p.sheet[p.pos] == ' ' || p.sheet[p.pos] == '\t' || p.sheet[p.pos] == '\n' || p.sheet[p.pos] == '\r' || p.sheet[p.pos] == '\f':
			p.pos++
		default:
			return
		}
	}
}

// block parses `prelude { ... }` and `prelude;` items until the closing brace
// of the enclosing block or the end of the sheet. context holds the enclosing
// at-rule preludes.
func (p *chatux046Parser) block(context []string) {
	for p.failed == "" && p.pos < len(p.sheet) {
		p.skipTrivia()
		if p.pos >= len(p.sheet) || p.sheet[p.pos] == '}' {
			if p.pos < len(p.sheet) {
				p.pos++
			}
			return
		}
		start := p.pos
		prelude := p.prelude()
		if p.failed != "" {
			return
		}
		p.skipTrivia()
		if p.pos < len(p.sheet) && p.sheet[p.pos] == ';' {
			p.pos++
			continue
		}
		if p.pos < len(p.sheet) && p.sheet[p.pos] == '}' && len(context) > 0 && strings.TrimSpace(prelude) != "" {
			// A declaration without its trailing semicolon closing an
			// at-rule body (@property and @font-face are written this
			// way). It is not a qualified rule, so there is nothing to
			// record; the brace still closes this block.
			p.pos++
			return
		}
		if p.pos >= len(p.sheet) || p.sheet[p.pos] != '{' {
			p.failed = "expected a block"
			return
		}
		p.pos++
		if strings.HasPrefix(strings.TrimSpace(prelude), "@") {
			p.block(append(context, chatux046Normalize(prelude)))
			continue
		}
		declarations := p.declarations()
		if p.failed != "" {
			return
		}
		*p.rules = append(*p.rules, chatux046Rule{
			context:      strings.Join(context, " / "),
			selector:     chatux046Normalize(prelude),
			declarations: chatux046Normalize(declarations),
			offset:       start,
		})
	}
}

// prelude reads a selector or at-rule prelude up to the opening brace or
// semicolon, respecting quotes and escapes.
func (p *chatux046Parser) prelude() string {
	var out strings.Builder
	for p.pos < len(p.sheet) {
		char := p.sheet[p.pos]
		switch {
		case char == '{' || char == ';' || char == '}':
			return out.String()
		case char == '"' || char == '\'':
			out.WriteString(p.quoted())
		case char == '\\' && p.pos+1 < len(p.sheet):
			out.WriteByte(char)
			out.WriteByte(p.sheet[p.pos+1])
			p.pos += 2
		default:
			out.WriteByte(char)
			p.pos++
		}
	}
	return out.String()
}

// declarations reads a declaration block body (the opening brace is already
// consumed), respecting nested blocks, quotes and comments.
func (p *chatux046Parser) declarations() string {
	depth := 1
	start := p.pos
	for p.pos < len(p.sheet) && depth > 0 {
		char := p.sheet[p.pos]
		switch {
		case char == '/' && p.pos+1 < len(p.sheet) && p.sheet[p.pos+1] == '*':
			end := strings.Index(p.sheet[p.pos+2:], "*/")
			if end < 0 {
				p.failed = "unterminated comment in declarations"
				return ""
			}
			p.pos += end + 4
		case char == '"' || char == '\'':
			p.quoted()
		case char == '\\' && p.pos+1 < len(p.sheet):
			p.pos += 2
		case char == '{':
			depth++
			p.pos++
		case char == '}':
			depth--
			p.pos++
		default:
			p.pos++
		}
	}
	if depth > 0 {
		p.failed = "unterminated block"
		return ""
	}
	return p.sheet[start : p.pos-1]
}

func (p *chatux046Parser) quoted() string {
	quote := p.sheet[p.pos]
	start := p.pos
	p.pos++
	for p.pos < len(p.sheet) {
		char := p.sheet[p.pos]
		if char == '\\' && p.pos+1 < len(p.sheet) {
			p.pos += 2
			continue
		}
		p.pos++
		if char == quote {
			break
		}
	}
	return p.sheet[start:p.pos]
}

// chatux046OwnerOf attributes a rule offset to the part whose text holds it.
func chatux046OwnerOf(parts []chatux046Part, offset int) chatux046Part {
	at := 0
	for _, part := range parts {
		if offset >= at && offset < at+len(part.text) {
			return part
		}
		at += len(part.text)
	}
	return chatux046Part{name: "outside every part"}
}

// TestTodo_CHATUX_046_DuplicateRules proves no served stylesheet carries two
// copies of the same rule where the earlier copy sits in bytes this package
// owns. For identical selector and declarations only the last occurrence can
// ever win, so every earlier occurrence is dead weight on every page load;
// the fix is to delete the earlier owned copy, not to restyle anything.
// Duplicates whose dead copies all belong to sibling-owned segments are
// reported but do not fail: they are the segment owner's removal.
func TestTodo_CHATUX_046_DuplicateRules(t *testing.T) {
	platformStylesOnce.Do(initPlatformStyles)
	theme, _ := ResolveTheme(nil)
	chatParts := []chatux046Part{
		{name: "chatui.ScopedStylesheet", text: chatui.ScopedStylesheet(), owned: false},
		{name: "chatShellStylesheet", text: chatShellStylesheet(), owned: true},
		{name: "ChatRetentionStylesheet", text: ChatRetentionStylesheet(), owned: true},
		{name: "chatui.TranslationAdminPageStyles", text: chatui.TranslationAdminPageStyles, owned: false},
	}
	// platformStyles carries one sibling-owned span, projectui.Styles(),
	// carved out here by search rather than by mirroring the assembly order
	// in initPlatformStyles, so later parts can be reordered freely.
	projectStyles := projectui.Styles()
	if strings.Count(platformStyles, projectStyles) != 1 {
		t.Fatalf("projectui.Styles() does not occur exactly once in platformStyles; re-check the test's part list")
	}
	before, after, _ := strings.Cut(platformStyles, projectStyles)
	controlParts := []chatux046Part{
		{name: "theme", text: theme.CSS(), owned: true},
		{name: "platformHeadStyles", text: platformHeadStyles, owned: true},
		{name: "platformStyles before projectui", text: before, owned: true},
		{name: "projectui.Styles", text: projectStyles, owned: false},
		{name: "platformStyles after projectui", text: after, owned: true},
		{name: "preview", text: customAppearancePreviewStylesheet(theme), owned: true},
		{name: "platformDarkStyles", text: platformDarkStyles, owned: true},
	}
	for _, sheet := range []struct {
		name  string
		serve func() string
		parts []chatux046Part
	}{
		{"chat", ChatStylesheet, chatParts},
		{"control", ControlStylesheet, controlParts},
	} {
		t.Run(sheet.name, func(t *testing.T) {
			served := sheet.serve()
			var assembled strings.Builder
			for _, part := range sheet.parts {
				assembled.WriteString(part.text)
			}
			if assembled.String() != served {
				t.Fatalf("the %s sheet is not the concatenation of its named parts; re-check the test's part list", sheet.name)
			}
			var rules []chatux046Rule
			ok, failure := chatux046ParseRules(served, &rules)
			if !ok {
				t.Fatalf("the %s sheet does not parse: %s", sheet.name, failure)
			}
			if len(rules) == 0 {
				t.Fatalf("the %s sheet parsed to zero rules", sheet.name)
			}
			byRule := map[chatux046Rule][]chatux046Rule{}
			for _, rule := range rules {
				key := rule
				key.offset = 0
				byRule[key] = append(byRule[key], rule)
			}
			for key, occurrences := range byRule {
				if len(occurrences) < 2 {
					continue
				}
				dead := occurrences[:len(occurrences)-1]
				kept := occurrences[len(occurrences)-1]
				keptOwner := chatux046OwnerOf(sheet.parts, kept.offset)
				var owned, sibling []string
				for _, occurrence := range dead {
					owner := chatux046OwnerOf(sheet.parts, occurrence.offset)
					entry := owner.name + " byte " + strconv.Itoa(occurrence.offset)
					if owner.owned {
						owned = append(owned, entry)
					} else {
						sibling = append(sibling, entry)
					}
				}
				if len(owned) > 0 {
					t.Errorf("%s sheet: %d dead copies of %q (context %q) in owned bytes: %s; delete the earlier copies (the live copy stays at %s byte %s)",
						sheet.name, len(owned), snippet(key.selector), snippet(key.context), strings.Join(owned, ", "), keptOwner.name, strconv.Itoa(kept.offset))
				} else {
					t.Logf("%s sheet: %d dead copies of %q owned by a sibling segment: %s",
						sheet.name, len(sibling), snippet(key.selector), strings.Join(sibling, ", "))
				}
			}
		})
	}
}

func snippet(text string) string {
	if len(text) > 90 {
		return text[:90] + "..."
	}
	if text == "" {
		return "(top level)"
	}
	return text
}
