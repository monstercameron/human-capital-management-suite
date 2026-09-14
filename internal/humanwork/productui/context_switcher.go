package productui

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ContextSwitcherState is presentation state for the server-resolved context
// catalog. It is not evidence that a context exchange succeeded.
type ContextSwitcherState string

const (
	ContextSwitcherReady     ContextSwitcherState = "ready"
	ContextSwitcherLoading   ContextSwitcherState = "loading"
	ContextSwitcherSwitching ContextSwitcherState = "switching"
	ContextSwitcherError     ContextSwitcherState = "error"
	ContextSwitcherEmpty     ContextSwitcherState = "empty"
)

const (
	maxContextOptions = 32
	maxContextText    = 160
)

// AuthorityContext is the exact, display-safe current context returned by the
// authoritative session projection.
type AuthorityContext struct {
	TenantID          string
	TenantName        string
	ActingContextID   string
	ActingContextName string
	Delegator         string
	ExpiresAt         string
	Delegated         bool
	Elevated          bool
	// State is the server-resolved classification of why the acting-authority
	// banner would matter (UXAUDIT-007). It is a separate signal from
	// Delegated/Elevated, not a replacement for them: those two remain the
	// source for the banner's detail text (see currentContextDetail), while
	// State is the source [ActingAuthorityBanner] consults when it carries an
	// explicit, non-empty value. An empty State defers to Delegated/Elevated
	// (see authorityNoticeworthy) rather than being treated as "unknown" on
	// its own, because those two booleans are themselves already a complete,
	// known answer about the one authority a session projection currently
	// resolves. A non-empty State the [AuthorityState.noticeworthy] switch
	// does not recognize is not the same as "no state was ever set" and
	// discloses precisely because it does not match the one safe value.
	State AuthorityState
}

// AuthorityState is the closed set of reasons a session's acting authority
// would need a persistent, shell-wide notice. It is resolved by the
// authoritative session projection -- never inferred by a page -- and is
// deliberately exhaustive: [AuthorityState.noticeworthy] names every safe
// (quiet) value explicitly and discloses for everything else, including a
// value this type does not recognize at all, so a future authority mode the
// presentation layer has not been taught yet fails toward disclosure, not
// toward silently impersonating the viewer's own authority.
type AuthorityState string

const (
	// AuthorityStateSelf is the one value [AuthorityState.noticeworthy]
	// treats as quiet: acting under the viewer's own, undelegated,
	// unelevated authority.
	AuthorityStateSelf AuthorityState = "self"
	// AuthorityStateDelegated is another principal's authority, exercised on
	// their behalf.
	AuthorityStateDelegated AuthorityState = "delegated"
	// AuthorityStateViewAs is a policy simulation under a viewed identity;
	// no authority is actually assumed (see PolicySimulation), but the
	// acting-authority notice still applies while it is in force.
	AuthorityStateViewAs AuthorityState = "view_as"
	// AuthorityStateElevated is the viewer's own identity acting with
	// broadened, temporarily granted capability.
	AuthorityStateElevated AuthorityState = "elevated"
	// AuthorityStateBreakGlass is emergency access exercised under an active
	// break-glass grant.
	AuthorityStateBreakGlass AuthorityState = "break_glass"
)

// noticeworthy reports whether s calls for the shell's persistent
// acting-authority banner. AuthorityStateSelf is the only value that stays
// quiet; every other named state discloses, and -- with no permissive
// default -- so does anything this switch does not recognize at all,
// including the empty AuthorityState. Hiding the banner under real
// delegated, view-as, elevated, or break-glass authority would misrepresent
// who the viewer is acting as, which is the more dangerous error; showing it
// once too often for an otherwise-ordinary session is only noise.
func (s AuthorityState) noticeworthy() bool {
	switch s {
	case AuthorityStateSelf:
		return false
	case AuthorityStateDelegated, AuthorityStateViewAs, AuthorityStateElevated, AuthorityStateBreakGlass:
		return true
	default:
		return true
	}
}

// AuthorityContextOption is one complete tenant/acting pair admitted by the
// server. Keeping the pair together prevents the browser from combining a
// tenant from one answer with authority from another.
type AuthorityContextOption struct {
	TenantID                 string
	TenantName               string
	TenantDescription        string
	ActingContextID          string
	ActingContextName        string
	ActingContextDescription string
	Delegator                string
	ExpiresAt                string
	Delegated                bool
	Elevated                 bool
}

// ContextSelection is the bounded input to the injected authoritative seam.
// It carries no credential and this package never persists it.
type ContextSelection struct {
	TenantID        string
	ActingContextID string
}

// ContextSwitchResult is the bounded, display-safe receipt for one fully
// resolved authoritative exchange. ProjectionRef is an opaque correlation
// handle, never authority: the injected adapter owns the staged View and must
// resolve this handle only inside its private transaction.
type ContextSwitchResult struct {
	Selection      ContextSelection
	Current        AuthorityContext
	Options        []AuthorityContextOption
	Page           PageID
	TenantLabel    string
	PrincipalLabel string
	ProjectionRef  string
}

// ContextExchange is injected by the authenticated transport adapter. This
// package deliberately does not invent an RPC or accept URL/browser state as
// an authority source.
type ContextExchange func(context.Context, ContextSelection) (ContextSwitchResult, error)

// ContextCommit resolves ProjectionRef against the adapter's private staged
// View, then atomically invalidates every old tenant/principal/authority-scoped
// projection and browser-history hint and adopts that View. Neither the staged
// View nor any page-wide record crosses the component-props boundary. The
// adapter owns rollback if its transaction cannot complete.
type ContextCommit func(ContextSwitchResult) error

// ContextRollback restores the pre-commit browser/runtime snapshot. It must be
// idempotent and is invoked when Commit errors or panics.
type ContextRollback func()

// ContextSwitcherProps contains only a display-safe server projection and the
// explicit adapter seams required to replace it. The callbacks exchange only
// a narrow receipt; their adapter owns the staged page projection out of band.
type ContextSwitcherProps struct {
	I18nProps
	Current    AuthorityContext
	Options    []AuthorityContextOption
	State      ContextSwitcherState
	Exchange   ContextExchange
	Commit     ContextCommit
	Rollback   ContextRollback
	Controller *ContextSwitchController
}

// ContextSwitchController cancels and fences overlapping authoritative
// exchanges. Commit executes while the generation lock is held, so an older
// response cannot race a newer Begin between its final check and adoption.
type ContextSwitchController struct {
	mu         sync.Mutex
	generation uint64
	cancel     context.CancelFunc
}

func (c *ContextSwitchController) begin(parent context.Context) (context.Context, uint64) {
	if parent == nil {
		parent = context.Background()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.generation++
	requestContext, cancel := context.WithCancel(parent)
	c.cancel = cancel
	return requestContext, c.generation
}

// Cancel invalidates the active exchange and propagates cancellation to a
// cooperative adapter.
func (c *ContextSwitchController) Cancel() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.generation++
	c.mu.Unlock()
}

func (c *ContextSwitchController) current(generation uint64) bool {
	if c == nil || generation == 0 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation == generation
}

func (c *ContextSwitchController) finish(generation uint64) {
	if c == nil || generation == 0 {
		return
	}
	c.mu.Lock()
	if c.generation == generation && c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.mu.Unlock()
}

func (c *ContextSwitchController) commit(generation uint64, result ContextSwitchResult, commit ContextCommit, rollback ContextRollback) (err error) {
	if c == nil || generation == 0 || commit == nil || rollback == nil {
		return ErrContextExchangeUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation != generation {
		return ErrContextResponseStale
	}
	defer func() {
		if recover() != nil {
			safeContextRollback(rollback)
			err = ErrContextCommitFailed
		}
	}()
	if commit(result) != nil {
		safeContextRollback(rollback)
		return ErrContextCommitFailed
	}
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	return nil
}

func safeContextRollback(rollback ContextRollback) {
	defer func() { _ = recover() }()
	rollback()
}

var (
	ErrContextExchangeUnavailable = errors.New("productui: authoritative context exchange unavailable")
	ErrContextSelectionInvalid    = errors.New("productui: context selection is not authorized")
	ErrContextResponseStale       = errors.New("productui: context response is stale")
	ErrContextResponseInvalid     = errors.New("productui: context response is invalid")
	ErrContextExchangeFailed      = errors.New("productui: context exchange failed")
	ErrContextCommitFailed        = errors.New("productui: context replacement could not be adopted")
)

// SwitchAuthorityContext validates an exact server-projected pair, performs an
// authoritative exchange, rejects malformed or stale answers, and only then
// asks the adapter to atomically clear old scoped state and adopt the fully
// resolved replacement staged privately by the adapter. Adapter errors are
// intentionally not wrapped because server messages and identifiers are not
// safe presentation text.
func SwitchAuthorityContext(props ContextSwitcherProps, selection ContextSelection) error {
	return SwitchAuthorityContextWithContext(context.Background(), props, selection)
}

// SwitchAuthorityContextWithContext is the cancellation-aware form used by a
// runtime adapter and by overlap tests.
func SwitchAuthorityContextWithContext(parent context.Context, props ContextSwitcherProps, selection ContextSelection) (err error) {
	normalized := normalizeContextSwitcherProps(props)
	if normalizeContextSelection(selection) != selection || !contextSelectionAllowed(normalized, selection) {
		return ErrContextSelectionInvalid
	}
	if sameContextSelection(selection, normalized.Current) {
		return nil
	}
	if props.Exchange == nil || props.Commit == nil || props.Rollback == nil || props.Controller == nil {
		return ErrContextExchangeUnavailable
	}
	requestContext, generation := props.Controller.begin(parent)
	defer props.Controller.finish(generation)
	defer func() {
		if recover() != nil {
			err = ErrContextExchangeFailed
		}
	}()
	result, exchangeErr := props.Exchange(requestContext, selection)
	if !props.Controller.current(generation) {
		return ErrContextResponseStale
	}
	if exchangeErr != nil {
		return ErrContextExchangeFailed
	}
	// Detach the receipt's bounded catalog from storage retained by the
	// transport adapter before validating or handing it to Commit.
	result.Options = append([]AuthorityContextOption(nil), result.Options...)
	if !validContextSwitchResult(result, selection) {
		return ErrContextResponseInvalid
	}
	return props.Controller.commit(generation, result, props.Commit, props.Rollback)
}

// ContextSwitcher renders a native details/button baseline. Exact pairs are
// grouped visually by tenant, but every button closes over one inseparable
// server-projected selection and no opaque identifier is written to the DOM.
func ContextSwitcher(props ContextSwitcherProps) ui.Node {
	interaction := ui.UseState(ContextSwitcherReady)
	summaryRef := ui.UseDOMRef()
	props = normalizeContextSwitcherProps(props)
	if !contextSwitcherVisible(props) {
		return html.Fragment()
	}
	phase := props.State
	if interaction.Get() != ContextSwitcherReady {
		phase = interaction.Get()
	}
	busy := phase == ContextSwitcherLoading || phase == ContextSwitcherSwitching
	locale := props.Locale.normalized()
	label := locale.Text("context_switcher.label")
	current := currentContextLabel(props)

	children := []ui.Node{html.Strong(html.Props{Class: "context-switcher-title"}, ui.Text(label))}
	if validAuthorityContext(props.Current) {
		children = append(children, html.P(html.Props{Class: "context-switcher-current-detail"}, ui.Text(currentContextDetail(locale, props))))
	}
	for _, group := range groupedContextOptions(props.Options) {
		buttons := make([]ui.Node, 0, len(group.Options))
		for _, option := range group.Options {
			option := option
			selection := ContextSelection{TenantID: option.TenantID, ActingContextID: option.ActingContextID}
			isCurrent := sameContextSelection(selection, props.Current)
			disabled := busy || props.Exchange == nil || props.Commit == nil || props.Rollback == nil || props.Controller == nil
			button := ui.CreateElement(contextSwitcherOption, contextSwitcherOptionProps{
				Locale: locale, Option: option, Selection: selection, Current: isCurrent, Disabled: disabled,
				Switcher: props, Interaction: interaction, SummaryRef: summaryRef,
			})
			buttons = append(buttons, html.WithKey(button, option.TenantID+"\x00"+option.ActingContextID))
		}
		children = append(children,
			html.Div(html.Props{Class: "context-switcher-tenant"},
				html.P(html.Props{Class: "context-switcher-section-title"}, ui.Text(group.Name)),
				html.Ul(html.Props{Class: "context-switcher-options"}, buttons...),
			),
		)
	}
	if len(props.Options) == 0 {
		children = append(children, html.P(html.Props{Class: "context-switcher-empty"}, ui.Text(locale.Text("context_switcher.empty"))))
	}
	children = append(children, html.P(html.Props{
		Class: "context-switcher-status",
		Raw:   map[string]any{"role": "status", "aria-live": "polite", "aria-atomic": "true"},
	}, ui.Text(contextSwitcherStatus(locale, phase, len(props.Options)))))
	panelRaw := map[string]any{}
	if busy {
		panelRaw["aria-busy"] = true
	}
	trigger := html.Span(html.Props{Class: "context-switcher-trigger", Raw: map[string]any{"title": label}},
		html.Span(html.Props{Class: "context-switcher-current"}, ui.Text(current)),
		productIcon("expand", "context-switcher-chevron"),
	)
	return html.Details(html.Props{Class: "context-switcher", Dir: string(locale.Direction), Data: map[string]string{
		"hcm-context-switcher": "true", "hcm-transient-popover": "context", "hcm-popover-grace-ms": transientPopoverGraceMilliseconds,
	}},
		html.Summary(html.PropsOf(
			html.Class("context-switcher-summary"),
			html.Aria("label", label),
			html.Dataset(map[string]string{"hcm-context-trigger": "true"}),
			html.Ref(summaryRef),
		), trigger),
		ui.CreateElement(PopoverSurface, PopoverSurfaceProps{Class: "context-switcher-panel", Raw: panelRaw, Children: children}),
	)
}

type contextSwitcherOptionProps struct {
	Locale      LocaleContext
	Option      AuthorityContextOption
	Selection   ContextSelection
	Current     bool
	Disabled    bool
	Switcher    ContextSwitcherProps
	Interaction ui.State[ContextSwitcherState]
	SummaryRef  ui.DOMRef
}

func contextSwitcherOption(props contextSwitcherOptionProps) ui.Node {
	onClick := ui.UseEvent(func(ui.MouseEvent) {
		if props.Current || props.Disabled {
			return
		}
		props.Interaction.Set(ContextSwitcherSwitching)
		go func() {
			switchErr := SwitchAuthorityContext(props.Switcher, props.Selection)
			if errors.Is(switchErr, ErrContextResponseStale) {
				return
			}
			if switchErr != nil {
				props.Interaction.Set(ContextSwitcherError)
				return
			}
			props.Interaction.Set(ContextSwitcherReady)
			props.SummaryRef.Focus()
		}()
	})
	return contextOptionButton(props.Locale, props.Option, props.Current, props.Disabled, onClick)
}

func contextOptionButton(locale LocaleContext, option AuthorityContextOption, current, disabled bool, onClick ui.Handler) ui.Node {
	label := contextOptionLabel(option)
	detail := contextOptionDetail(locale, option)
	ariaLabel := locale.Text("context_switcher.switch_to", map[string]string{"tenant": option.TenantName, "acting": label})
	if current {
		ariaLabel = locale.Text("context_switcher.current_context", map[string]string{"tenant": option.TenantName, "acting": label})
	}
	buttonProps := html.Props{
		Class: "context-switcher-option", Type: "button", Disabled: disabled || current,
		Aria: map[string]string{"label": ariaLabel}, OnClick: onClick,
	}
	if current {
		buttonProps.Class += " current"
		buttonProps.Aria["current"] = "true"
	}
	content := []ui.Node{html.Strong(html.Props{}, ui.Text(label))}
	if detail != "" {
		content = append(content, html.Small(html.Props{}, ui.Text(detail)))
	}
	if current {
		content = append(content, html.Span(html.Props{Class: "context-switcher-current-mark"}, ui.Text(locale.Text("context_switcher.current"))))
	}
	return html.Li(html.Props{}, html.Button(buttonProps, content...))
}

type contextOptionGroup struct {
	TenantID string
	Name     string
	Options  []AuthorityContextOption
}

func groupedContextOptions(options []AuthorityContextOption) []contextOptionGroup {
	groups := make([]contextOptionGroup, 0, len(options))
	indexes := make(map[string]int, len(options))
	for _, option := range options {
		index, ok := indexes[option.TenantID]
		if !ok {
			index = len(groups)
			indexes[option.TenantID] = index
			groups = append(groups, contextOptionGroup{TenantID: option.TenantID, Name: option.TenantName})
		}
		groups[index].Options = append(groups[index].Options, option)
	}
	return groups
}

func contextSwitcherVisible(props ContextSwitcherProps) bool {
	return len(props.Options) > 0 || props.State == ContextSwitcherLoading || props.State == ContextSwitcherError || props.State == ContextSwitcherEmpty
}

func currentContextLabel(props ContextSwitcherProps) string {
	if !validAuthorityContext(props.Current) {
		return props.Locale.Text("context_switcher.current_unknown")
	}
	if acting := authorityContextName(props.Current); acting != "" {
		return strings.Join([]string{props.Current.TenantName, acting}, " · ")
	}
	return props.Current.TenantName
}

func currentContextDetail(locale LocaleContext, props ContextSwitcherProps) string {
	detail := currentContextLabel(props)
	if props.Current.Delegated && props.Current.Delegator != "" {
		detail += " · " + locale.Text("context_switcher.delegated_by", map[string]string{"name": props.Current.Delegator})
	}
	if props.Current.ExpiresAt != "" {
		detail += " · " + locale.Text("context_switcher.expires", map[string]string{"date": props.Current.ExpiresAt})
	}
	if props.Current.Elevated {
		detail += " · " + locale.Text("context_switcher.elevated")
	}
	return detail
}

func contextOptionLabel(option AuthorityContextOption) string {
	if option.ActingContextName != "" {
		return option.ActingContextName
	}
	return option.TenantName
}

func contextOptionDetail(locale LocaleContext, option AuthorityContextOption) string {
	detail := option.ActingContextDescription
	if option.Delegated && option.Delegator != "" {
		detail = appendContextDetail(detail, locale.Text("context_switcher.delegated_by", map[string]string{"name": option.Delegator}))
	}
	if option.ExpiresAt != "" {
		detail = appendContextDetail(detail, locale.Text("context_switcher.expires", map[string]string{"date": option.ExpiresAt}))
	}
	if option.Elevated {
		detail = appendContextDetail(detail, locale.Text("context_switcher.elevated"))
	}
	return detail
}

func appendContextDetail(current, next string) string {
	if current == "" {
		return next
	}
	return current + " · " + next
}

func contextSwitcherStatus(locale LocaleContext, state ContextSwitcherState, optionCount int) string {
	switch state {
	case ContextSwitcherLoading:
		return locale.Text("context_switcher.loading")
	case ContextSwitcherSwitching:
		return locale.Text("context_switcher.switching")
	case ContextSwitcherError:
		return locale.Text("context_switcher.failed")
	case ContextSwitcherEmpty:
		return locale.Text("context_switcher.empty")
	}
	if optionCount == 0 {
		return locale.Text("context_switcher.empty")
	}
	if optionCount == 1 {
		return locale.Text("context_switcher.single")
	}
	return locale.Text("context_switcher.ready")
}

func normalizeContextSwitcherProps(props ContextSwitcherProps) ContextSwitcherProps {
	props.Locale = props.Locale.normalized()
	props.Options = normalizeContextOptions(props.Options)
	props.Current = resolvedAuthorityContext(normalizeAuthorityContext(props.Current), props.Options)
	if props.State == "" {
		props.State = ContextSwitcherReady
	}
	return props
}

func normalizeContextOptions(options []AuthorityContextOption) []AuthorityContextOption {
	result := make([]AuthorityContextOption, 0, minContextOptions(len(options)))
	seen := make(map[ContextSelection]struct{}, maxContextOptions)
	for _, option := range options {
		option = normalizeContextOption(option)
		selection := ContextSelection{TenantID: option.TenantID, ActingContextID: option.ActingContextID}
		if !validContextOption(option) {
			continue
		}
		if _, ok := seen[selection]; ok {
			continue
		}
		seen[selection] = struct{}{}
		result = append(result, option)
		if len(result) >= maxContextOptions {
			break
		}
	}
	return result
}

func normalizeContextOption(option AuthorityContextOption) AuthorityContextOption {
	option.TenantID = normalizeContextIdentifier(option.TenantID)
	option.TenantName = boundedContextText(option.TenantName)
	option.TenantDescription = boundedContextText(option.TenantDescription)
	option.ActingContextID = normalizeContextIdentifier(option.ActingContextID)
	option.ActingContextName = boundedContextText(option.ActingContextName)
	option.ActingContextDescription = boundedContextText(option.ActingContextDescription)
	option.Delegator = boundedContextText(option.Delegator)
	option.ExpiresAt = boundedContextText(option.ExpiresAt)
	return option
}

func normalizeAuthorityContext(current AuthorityContext) AuthorityContext {
	current.TenantID = normalizeContextIdentifier(current.TenantID)
	current.TenantName = boundedContextText(current.TenantName)
	current.ActingContextID = normalizeContextIdentifier(current.ActingContextID)
	current.ActingContextName = boundedContextText(current.ActingContextName)
	current.Delegator = boundedContextText(current.Delegator)
	current.ExpiresAt = boundedContextText(current.ExpiresAt)
	return current
}

func normalizeContextSelection(selection ContextSelection) ContextSelection {
	return ContextSelection{TenantID: normalizeContextIdentifier(selection.TenantID), ActingContextID: normalizeContextIdentifier(selection.ActingContextID)}
}

func contextSelectionAllowed(props ContextSwitcherProps, selection ContextSelection) bool {
	if selection.TenantID == "" {
		return false
	}
	for _, option := range props.Options {
		if selection == (ContextSelection{TenantID: option.TenantID, ActingContextID: option.ActingContextID}) {
			return true
		}
	}
	return false
}

func validContextSwitchResult(result ContextSwitchResult, selection ContextSelection) bool {
	if result.Selection != selection || !knownPage(result.Page) ||
		boundedContextText(result.TenantLabel) != result.TenantLabel || result.TenantLabel == "" ||
		boundedContextText(result.PrincipalLabel) != result.PrincipalLabel || result.PrincipalLabel == "" ||
		normalizeContextIdentifier(result.ProjectionRef) != result.ProjectionRef || result.ProjectionRef == "" {
		return false
	}
	return strictContextCatalog(result.Current, result.Options) &&
		sameContextSelection(selection, result.Current) &&
		result.TenantLabel == result.Current.TenantName
}

func strictContextCatalog(current AuthorityContext, options []AuthorityContextOption) bool {
	if len(options) == 0 || len(options) > maxContextOptions || normalizeAuthorityContext(current) != current || !validAuthorityContext(current) {
		return false
	}
	seen := make(map[ContextSelection]struct{}, len(options))
	currentCount := 0
	for _, option := range options {
		if normalizeContextOption(option) != option || !validContextOption(option) {
			return false
		}
		selection := ContextSelection{TenantID: option.TenantID, ActingContextID: option.ActingContextID}
		if _, duplicate := seen[selection]; duplicate {
			return false
		}
		seen[selection] = struct{}{}
		if sameContextSelection(selection, current) {
			if current != authorityContextFromOption(option) {
				return false
			}
			currentCount++
		}
	}
	return currentCount == 1
}

func validContextOption(option AuthorityContextOption) bool {
	return option.TenantID != "" && option.TenantName != "" &&
		(option.ActingContextID == "" || option.ActingContextName != "") &&
		(!option.Delegated || option.Delegator != "" && option.ExpiresAt != "")
}

func validAuthorityContext(current AuthorityContext) bool {
	return current.TenantID != "" && current.TenantName != "" &&
		(current.ActingContextID == "" || authorityContextName(current) != "") &&
		(!current.Delegated || current.Delegator != "" && current.ExpiresAt != "")
}

func authorityContextName(current AuthorityContext) string {
	return current.ActingContextName
}

func resolvedAuthorityContext(current AuthorityContext, options []AuthorityContextOption) AuthorityContext {
	if !validAuthorityContext(current) {
		return AuthorityContext{}
	}
	for _, option := range options {
		if current.TenantID == option.TenantID && current.ActingContextID == option.ActingContextID {
			return authorityContextFromOption(option)
		}
	}
	return AuthorityContext{}
}

func authorityContextFromOption(option AuthorityContextOption) AuthorityContext {
	return AuthorityContext{
		TenantID: option.TenantID, TenantName: option.TenantName,
		ActingContextID: option.ActingContextID, ActingContextName: option.ActingContextName,
		Delegator: option.Delegator, ExpiresAt: option.ExpiresAt,
		Delegated: option.Delegated, Elevated: option.Elevated,
	}
}

func sameContextSelection(selection ContextSelection, current AuthorityContext) bool {
	return selection.TenantID == current.TenantID && selection.ActingContextID == current.ActingContextID
}

func normalizeContextIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxContextText {
		return ""
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f || r == '<' || r == '>' || r == '"' || r == '\'' || r == '/' || r == '\\' || r == ' ' || r == '\t' || r == '\n' {
			return ""
		}
	}
	return value
}

func boundedContextText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxContextText {
		return ""
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}
	return value
}

func minContextOptions(length int) int {
	if length > maxContextOptions {
		return maxContextOptions
	}
	if length < 0 {
		return 0
	}
	return length
}
