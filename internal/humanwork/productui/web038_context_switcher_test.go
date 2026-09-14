package productui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

func web038Fixture() ContextSwitcherProps {
	return ContextSwitcherProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Current: AuthorityContext{
			TenantID: "tenant-a", TenantName: "HarborCare", ActingContextID: "self-a", ActingContextName: "Your own authority",
		},
		Options: []AuthorityContextOption{
			{TenantID: "tenant-a", TenantName: "HarborCare", ActingContextID: "self-a", ActingContextName: "Your own authority"},
			{TenantID: "tenant-a", TenantName: "HarborCare", ActingContextID: "delegate-a", ActingContextName: "Covering HR", Delegated: true, Elevated: true, Delegator: "Maya Chen", ExpiresAt: "2026-09-18"},
			{TenantID: "tenant-b", TenantName: "Northwind", ActingContextID: "self-b", ActingContextName: "Northwind employee"},
		},
		State:      ContextSwitcherReady,
		Controller: &ContextSwitchController{},
	}
}

func web038ResolvedView(selection ContextSelection) View {
	props := web038Fixture()
	props.Exchange = nil
	props.Commit = nil
	props.Rollback = nil
	props.Controller = nil
	for _, option := range props.Options {
		if selection != (ContextSelection{TenantID: option.TenantID, ActingContextID: option.ActingContextID}) {
			continue
		}
		props.Current = AuthorityContext{
			TenantID: option.TenantID, TenantName: option.TenantName,
			ActingContextID: option.ActingContextID, ActingContextName: option.ActingContextName,
			Delegator: option.Delegator, ExpiresAt: option.ExpiresAt, Delegated: option.Delegated, Elevated: option.Elevated,
		}
	}
	view := NewView(PageHome, props.Current.TenantName, "resolved-principal", "resolved-purpose")
	view.ContextSwitcher = props
	return view
}

func web038Result(selection ContextSelection) ContextSwitchResult {
	view := web038ResolvedView(selection)
	return ContextSwitchResult{
		Selection: selection, Current: view.ContextSwitcher.Current,
		Options: append([]AuthorityContextOption(nil), view.ContextSwitcher.Options...),
		Page:    view.Page, TenantLabel: view.Tenant, PrincipalLabel: view.Principal,
		ProjectionRef: "projection-" + selection.TenantID + "-" + selection.ActingContextID,
	}
}

// TestTodo_WEB_038 proves that the component renders bounded exact pairs and
// cannot claim a change before an authoritative resolved replacement commits.
func TestTodo_WEB_038(t *testing.T) {
	props := web038Fixture()
	props.Current.TenantName = "Hidden current alias"
	props.Current.ActingContextName = "Hidden acting alias"
	props.Options = append(props.Options,
		AuthorityContextOption{TenantID: "tenant-b", TenantName: "Duplicate", ActingContextID: "self-b", ActingContextName: "Duplicate"},
		AuthorityContextOption{TenantID: "tenant/<foreign>", TenantName: "Foreign", ActingContextID: "foreign", ActingContextName: "Foreign"},
		AuthorityContextOption{TenantID: "tenant-c", TenantName: "", ActingContextID: "self-c", ActingContextName: "Malformed"},
	)
	markup, err := ui.RenderToString(ui.CreateElement(ContextSwitcher, props))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	if findClass(root, "context-switcher") == nil || !strings.Contains(markup, `data-hcm-popover-surface="true"`) {
		t.Fatal("context switcher did not render through the shared popover surface")
	}
	for _, want := range []string{
		`<details`, `<summary`, `<button`, `type="button"`, `aria-live="polite"`, `aria-atomic="true"`,
		"HarborCare", "Northwind", "Your own authority", "Covering HR", "delegated by Maya Chen", "expires 2026-09-18", "Elevated access",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("switcher missing %q: %s", want, markup)
		}
	}
	for _, unwanted := range []string{"Foreign", "Malformed", "Duplicate", "Hidden current alias", "Hidden acting alias", "tenant-a", "tenant-b", "self-a", "self-b", `role="listbox"`, `role="option"`, `aria-selected`} {
		if strings.Contains(markup, unwanted) {
			t.Errorf("switcher leaked an identifier/malformed option or emitted invalid mixed semantics %q", unwanted)
		}
	}
	if got := strings.Count(markup, `<button`); got != 3 {
		t.Fatalf("authorized pair buttons = %d, want three", got)
	}
	if got := strings.Count(markup, `aria-current="true"`); got != 1 {
		t.Fatalf("current pair count = %d, want one", got)
	}

	target := ContextSelection{TenantID: "tenant-b", ActingContextID: "self-b"}
	if err := SwitchAuthorityContext(props, target); !errors.Is(err, ErrContextExchangeUnavailable) {
		t.Fatalf("selection without authoritative seams = %v, want fail closed", err)
	}
	var calls []string
	var staged View
	props.Exchange = func(_ context.Context, selection ContextSelection) (ContextSwitchResult, error) {
		calls = append(calls, "exchange")
		staged = web038ResolvedView(selection)
		return web038Result(selection), nil
	}
	props.Commit = func(result ContextSwitchResult) error {
		calls = append(calls, "commit")
		if result.ProjectionRef == "" || staged.ContextSwitcher.Current.TenantName != "Northwind" {
			t.Fatalf("commit did not resolve adapter-owned staged view: receipt=%+v staged=%+v", result, staged.ContextSwitcher.Current)
		}
		return nil
	}
	props.Rollback = func() { t.Fatal("successful commit rolled back") }
	if err := SwitchAuthorityContext(props, target); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "exchange,commit" {
		t.Fatalf("context switch calls = %v, want exchange then atomic commit", calls)
	}
	if err := SwitchAuthorityContext(props, ContextSelection{TenantID: "tenant-b", ActingContextID: "delegate-a"}); !errors.Is(err, ErrContextSelectionInvalid) {
		t.Fatalf("unauthorized cross-tenant pair = %v, want refusal", err)
	}
	if err := SwitchAuthorityContext(props, ContextSelection{TenantID: " tenant-b", ActingContextID: "self-b"}); !errors.Is(err, ErrContextSelectionInvalid) {
		t.Fatalf("malformed client selection = %v, want refusal", err)
	}

	calls = nil
	current := ContextSelection{TenantID: "tenant-a", ActingContextID: "self-a"}
	if err := SwitchAuthorityContext(props, current); err != nil || len(calls) != 0 {
		t.Fatalf("same-context no-op = %v calls=%v", err, calls)
	}
}

// TestTodo_WEB_038_Golden pins option grouping, native semantics, hidden opaque
// identifiers and accessibility state as deterministic bytes.
func TestTodo_WEB_038_Golden(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(ContextSwitcher, web038Fixture()))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(markup))
	got := hex.EncodeToString(digest[:])
	const want = "509deb0fe2f068810974a0412bdc7faafd4fb3aace61f2bcec51c1c93c655d35"
	if got != want {
		t.Fatalf("context switcher golden digest = %s, want %s", got, want)
	}
}

// TestTodo_WEB_038_Conformance proves localization/RTL, duplicate-free
// multi-instance markup, strict answer validation and failure rollback.
func TestTodo_WEB_038_Conformance(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			props := web038Fixture()
			props.Locale = ResolveProductLocale(locale)
			server, err := ui.RenderToString(ui.CreateElement(ContextSwitcher, props))
			if err != nil {
				t.Fatal(err)
			}
			mounted, err := ui.RenderToString(ui.CreateElement(ContextSwitcher, props))
			if err != nil {
				t.Fatal(err)
			}
			if server != mounted {
				t.Fatal("SSR and WASM component trees differ")
			}
			if locale == "ar" && !strings.Contains(server, `dir="rtl"`) {
				t.Fatal("Arabic switcher did not preserve RTL direction")
			}
			view := testView(PageHome)
			view.Locale = ResolveProductLocale(locale)
			props.I18nProps = I18nProps{Locale: view.Locale}
			view.ContextSwitcher = props
			doc, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			start, end := strings.Index(doc, "<body>"), strings.LastIndex(doc, "</body>")
			body, renderErr := ui.RenderToString(Build(view))
			if start < 0 || end <= start || renderErr != nil || doc[start+len("<body>"):end] != body {
				t.Fatal("shell SSR and component body differ with context switcher")
			}
		})
	}

	t.Run("multiple instances need no collision-prone ids", func(t *testing.T) {
		markup, err := ui.RenderToString(ui.CreateElement(func(props ContextSwitcherProps) ui.Node {
			return ui.Fragment(ui.CreateElement(ContextSwitcher, props), ui.CreateElement(ContextSwitcher, props))
		}, web038Fixture()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(markup, ` id="`) || strings.Contains(markup, `aria-controls=`) {
			t.Fatalf("multi-instance component emitted collision-prone ids: %s", markup)
		}
	})

	t.Run("state semantics and responsive themes", func(t *testing.T) {
		props := web038Fixture()
		props.State = ContextSwitcherLoading
		markup, err := ui.RenderToString(ui.CreateElement(ContextSwitcher, props))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, "Loading authorized contexts") || !strings.Contains(markup, `aria-busy`) || strings.Count(markup, "disabled") != len(props.Options) {
			t.Fatalf("loading state is not busy and disabled: %s", markup)
		}
		styles := Stylesheet()
		for _, want := range []string{"@media (max-width:430px)", "inset-block-start:102px;", "@media (prefers-reduced-motion:reduce)", "@media (forced-colors:active)", "var(--surface)", "inset-inline-start"} {
			if !strings.Contains(styles, want) {
				t.Errorf("context switcher style gate missing %q", want)
			}
		}
	})

	t.Run("foreign malformed duplicate and partial answers fail before commit", func(t *testing.T) {
		target := ContextSelection{TenantID: "tenant-b", ActingContextID: "self-b"}
		cases := map[string]func(*ContextSwitchResult){
			"foreign echo": func(result *ContextSwitchResult) { result.Selection.TenantID = "tenant-z" },
			"wrong current": func(result *ContextSwitchResult) {
				result.Current.ActingContextID = "delegate-a"
			},
			"missing page identity": func(result *ContextSwitchResult) { result.Page = "" },
			"duplicate pair": func(result *ContextSwitchResult) {
				result.Options = append(result.Options, result.Options[0])
			},
			"oversized catalog": func(result *ContextSwitchResult) {
				base := result.Options[0]
				for len(result.Options) <= maxContextOptions {
					base.ActingContextID += "x"
					base.ActingContextName += "x"
					result.Options = append(result.Options, base)
				}
			},
			"malformed identifier":     func(result *ContextSwitchResult) { result.Options[0].TenantID = "tenant/a" },
			"malformed projection ref": func(result *ContextSwitchResult) { result.ProjectionRef = " projection/ref" },
			"mismatched tenant label":  func(result *ContextSwitchResult) { result.TenantLabel = "Foreign tenant" },
		}
		for name, mutate := range cases {
			t.Run(name, func(t *testing.T) {
				props := web038Fixture()
				committed := false
				props.Exchange = func(context.Context, ContextSelection) (ContextSwitchResult, error) {
					result := web038Result(target)
					result.Options = append([]AuthorityContextOption(nil), result.Options...)
					mutate(&result)
					return result, nil
				}
				props.Commit = func(ContextSwitchResult) error { committed = true; return nil }
				props.Rollback = func() { t.Fatal("invalid answer reached rollback") }
				if err := SwitchAuthorityContext(props, target); !errors.Is(err, ErrContextResponseInvalid) || committed {
					t.Fatalf("invalid answer = %v committed=%v", err, committed)
				}
			})
		}
	})

	t.Run("failures are sanitized and transactional commit rolls back", func(t *testing.T) {
		target := ContextSelection{TenantID: "tenant-b", ActingContextID: "self-b"}
		props := web038Fixture()
		props.Exchange = func(context.Context, ContextSelection) (ContextSwitchResult, error) {
			return ContextSwitchResult{}, errors.New("rpc denied tenant-b bearer-secret")
		}
		props.Commit = func(ContextSwitchResult) error { t.Fatal("failed exchange reached commit"); return nil }
		props.Rollback = func() { t.Fatal("failed exchange reached rollback") }
		err := SwitchAuthorityContext(props, target)
		if !errors.Is(err, ErrContextExchangeFailed) || strings.Contains(err.Error(), "tenant-b") || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe exchange error = %q", err)
		}

		props.Controller = &ContextSwitchController{}
		var staged View
		props.Exchange = func(context.Context, ContextSelection) (ContextSwitchResult, error) {
			staged = web038ResolvedView(target)
			return web038Result(target), nil
		}
		adopted := props.Current
		before := adopted
		rolledBack := false
		props.Commit = func(result ContextSwitchResult) error {
			if result.ProjectionRef == "" {
				t.Fatal("commit received an empty staged-projection receipt")
			}
			adopted = staged.ContextSwitcher.Current
			return errors.New("history reset failed with tenant-b")
		}
		props.Rollback = func() { adopted = before; rolledBack = true }
		err = SwitchAuthorityContext(props, target)
		if !errors.Is(err, ErrContextCommitFailed) || adopted != props.Current || !rolledBack || strings.Contains(err.Error(), "tenant-b") {
			t.Fatalf("partial commit = %v adopted=%+v rollback=%v", err, adopted, rolledBack)
		}
	})
}

func TestTodo_WEB_038_OverlapCancellationAndPanicSafety(t *testing.T) {
	props := web038Fixture()
	first := ContextSelection{TenantID: "tenant-a", ActingContextID: "delegate-a"}
	second := ContextSelection{TenantID: "tenant-b", ActingContextID: "self-b"}
	started := make(chan struct{})
	release := make(chan struct{})
	cancelled := make(chan struct{})
	props.Exchange = func(ctx context.Context, selection ContextSelection) (ContextSwitchResult, error) {
		if selection == first {
			close(started)
			<-ctx.Done()
			close(cancelled)
			<-release // deliberately ignore cancellation and answer late
		}
		return web038Result(selection), nil
	}
	var mu sync.Mutex
	var committed []ContextSelection
	props.Commit = func(result ContextSwitchResult) error {
		mu.Lock()
		defer mu.Unlock()
		committed = append(committed, result.Selection)
		return nil
	}
	props.Rollback = func() { t.Fatal("successful overlap commit rolled back") }
	firstDone := make(chan error, 1)
	go func() { firstDone <- SwitchAuthorityContext(props, first) }()
	<-started
	if err := SwitchAuthorityContext(props, second); err != nil {
		t.Fatal(err)
	}
	<-cancelled
	close(release)
	if err := <-firstDone; !errors.Is(err, ErrContextResponseStale) {
		t.Fatalf("late first answer = %v, want stale", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(committed) != 1 || committed[0] != second {
		t.Fatalf("overlap commits = %+v, want only second", committed)
	}

	panicProps := web038Fixture()
	panicProps.Exchange = func(context.Context, ContextSelection) (ContextSwitchResult, error) { panic("server identifier") }
	panicProps.Commit = func(ContextSwitchResult) error { return nil }
	panicProps.Rollback = func() { t.Fatal("exchange panic reached rollback") }
	if err := SwitchAuthorityContext(panicProps, second); !errors.Is(err, ErrContextExchangeFailed) || strings.Contains(err.Error(), "identifier") {
		t.Fatalf("exchange panic escaped/surfaced: %v", err)
	}
	panicProps.Controller = &ContextSwitchController{}
	panicProps.Exchange = func(context.Context, ContextSelection) (ContextSwitchResult, error) { return web038Result(second), nil }
	panicRollback := false
	panicProps.Commit = func(ContextSwitchResult) error { panic("principal secret") }
	panicProps.Rollback = func() { panicRollback = true }
	if err := SwitchAuthorityContext(panicProps, second); !errors.Is(err, ErrContextCommitFailed) || strings.Contains(err.Error(), "secret") || !panicRollback {
		t.Fatalf("commit panic escaped/surfaced or skipped rollback: %v rollback=%v", err, panicRollback)
	}
}

func BenchmarkStable(b *testing.B) {
	b.Run("ContextSwitcher", func(b *testing.B) {
		props := web038Fixture()
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if _, err := ui.RenderToString(ui.CreateElement(ContextSwitcher, props)); err != nil {
				b.Fatal(err)
			}
		}
	})
}
