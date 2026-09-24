package productui

import (
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func docsCommentThread(locale string, comments []DocumentComment, unavailable bool) ui.Node {
	items := make([]ui.Node, 0, len(comments))
	for _, comment := range comments {
		author := strings.TrimSpace(comment.Author)
		if author == "" {
			author = strings.TrimSpace(comment.AuthorID)
		}
		meta := author
		if created := docsCommentTime(comment.CreatedAt); created != "" {
			if meta != "" {
				meta += " · "
			}
			meta += created
		}
		items = append(items, html.Li(html.Props{Class: "docs-comment", Data: map[string]string{"comment-id": comment.ID, "version-id": comment.VersionID}},
			html.P(html.Props{Class: "docs-comment-meta"}, ui.Text(meta)),
			html.P(html.Props{Class: "docs-comment-body"}, ui.Text(comment.Body))))
	}
	if len(items) == 0 {
		message := "comments_empty"
		if unavailable {
			message = "comments_unavailable"
		}
		items = append(items, html.Li(html.Props{Class: "docs-comment-empty", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(locale, message))))
	}
	return html.Section(html.Props{Class: "docs-comments", Raw: map[string]any{"aria-labelledby": "docs-comments-heading"}},
		html.H3(html.Props{ID: "docs-comments-heading"}, ui.Text(docsText(locale, "comments_heading"))),
		html.Ol(html.Props{Class: "docs-comment-list"}, items...))
}

type docsEditFormProps struct {
	Locale, DocumentID, BaseVersionID, Title, Markdown string
	Open                                               ui.State[bool]
	Save                                               func(DocumentEditRequest, func(error))
}

func docsEditForm(props docsEditFormProps) ui.Node {
	title := ui.UseState(props.Title)
	body := ui.UseState(props.Markdown)
	busy := ui.UseState(false)
	failed := ui.UseState(false)
	conflict := ui.UseState(false)
	saved := ui.UseState(false)
	titleProps := html.Props{ID: "docs-edit-title", Name: "title", Required: true, MaxLength: 200, Value: title.Get()}
	titleProps.OnInput = ui.UseEvent(func(event ui.InputEvent) { title.Set(event.GetValue()) })
	bodyProps := html.Props{ID: "docs-edit-markdown", Name: "markdown", Required: true, Value: body.Get(), Raw: map[string]any{"aria-describedby": "docs-edit-help"}}
	bodyProps.OnInput = ui.UseEvent(func(event ui.InputEvent) { body.Set(event.GetValue()) })
	if !props.Open.Get() {
		return nil
	}
	submit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		if strings.TrimSpace(title.Get()) == "" || strings.TrimSpace(body.Get()) == "" || props.Save == nil {
			return
		}
		busy.Set(true)
		failed.Set(false)
		conflict.Set(false)
		saved.Set(false)
		props.Save(DocumentEditRequest{DocumentID: props.DocumentID, BaseVersionID: props.BaseVersionID, Title: title.Get(), Markdown: body.Get()}, func(err error) {
			busy.Set(false)
			if err != nil {
				failed.Set(true)
				conflict.Set(isDocumentVersionConflict(err))
				return
			}
			saved.Set(true)
		})
	})
	cancel := ui.UseEvent(func(ui.MouseEvent) {
		title.Set(props.Title)
		body.Set(props.Markdown)
		failed.Set(false)
		conflict.Set(false)
		saved.Set(false)
		props.Open.Set(false)
	})
	status := ui.Node(nil)
	if failed.Get() {
		message := "edit_failed"
		if conflict.Get() {
			message = "edit_conflict"
		}
		status = html.P(html.Props{ID: "docs-edit-status", Class: "docs-notice", Raw: map[string]any{"role": "alert"}}, ui.Text(docsText(props.Locale, message)))
	} else if busy.Get() {
		status = html.P(html.Props{ID: "docs-edit-status", Class: "muted", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(props.Locale, "edit_busy")))
	} else if saved.Get() {
		status = html.P(html.Props{ID: "docs-edit-status", Class: "docs-notice", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(props.Locale, "edit_saved")))
	}
	return html.Section(html.Props{Key: "docs-edit-" + props.BaseVersionID, Class: "docs-edit", Raw: map[string]any{"aria-labelledby": "docs-edit-heading"}},
		html.H3(html.Props{ID: "docs-edit-heading"}, ui.Text(docsText(props.Locale, "edit_heading"))),
		html.P(html.Props{ID: "docs-edit-help", Class: "muted"}, ui.Text(docsText(props.Locale, "edit_help"))),
		html.Form(html.Props{Class: "docs-edit-form", OnSubmit: submit},
			html.Label(html.Props{For: titleProps.ID}, ui.Text(docsText(props.Locale, "title"))), html.Input(titleProps),
			html.Label(html.Props{For: bodyProps.ID}, ui.Text(docsText(props.Locale, "markdown"))), html.Textarea(bodyProps), status,
			html.Div(html.Props{Class: "docs-edit-actions"}, html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: busy.Get() || saved.Get()}, ui.Text(docsText(props.Locale, "edit_save"))), html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: busy.Get(), OnClick: cancel}, ui.Text(docsText(props.Locale, "edit_cancel"))))),
	)
}

func isDocumentVersionConflict(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "document.stale_version") || strings.Contains(message, "stale_version")
}

func docsCommentTime(value string) string {
	if value == "" {
		return ""
	}
	created, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	return created.UTC().Format("2006-01-02 15:04 UTC")
}

type docsCommentFormProps struct {
	Locale, DocumentID, VersionID string
	Add                           func(DocumentCommentCreateRequest, func(error))
}

func docsCommentForm(props docsCommentFormProps) ui.Node {
	state := ui.UseState(DocumentCommentCreateRequest{DocumentID: props.DocumentID, VersionID: props.VersionID})
	busy := ui.UseState(false)
	failed := ui.UseState(false)
	body := html.Props{ID: "docs-comment-body", Name: "body", Required: true, MaxLength: 4000, Value: state.Get().Body, Raw: map[string]any{"aria-describedby": "docs-comment-help"}}
	body.OnInput = ui.UseEvent(func(event ui.InputEvent) {
		next := state.Get()
		next.Body = event.GetValue()
		state.Set(next)
	})
	status := ui.Node(nil)
	if failed.Get() {
		status = html.P(html.Props{ID: "docs-comment-status", Class: "docs-notice", Raw: map[string]any{"role": "alert"}}, ui.Text(docsText(props.Locale, "comment_failed")))
	} else if busy.Get() {
		status = html.P(html.Props{ID: "docs-comment-status", Class: "muted", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(props.Locale, "comment_busy")))
	}
	submit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		value := state.Get()
		if strings.TrimSpace(value.Body) == "" || props.Add == nil {
			return
		}
		busy.Set(true)
		failed.Set(false)
		props.Add(value, func(err error) {
			busy.Set(false)
			if err != nil {
				failed.Set(true)
				return
			}
			state.Set(DocumentCommentCreateRequest{DocumentID: props.DocumentID, VersionID: props.VersionID})
		})
	})
	return html.Section(html.Props{Class: "docs-comment-compose", Raw: map[string]any{"aria-labelledby": "docs-comment-compose-heading"}},
		html.H3(html.Props{ID: "docs-comment-compose-heading"}, ui.Text(docsText(props.Locale, "comment_action"))),
		html.P(html.Props{ID: "docs-comment-help", Class: "muted"}, ui.Text(docsText(props.Locale, "comment_help"))),
		html.Form(html.Props{Class: "docs-comment-form", OnSubmit: submit},
			html.Label(html.Props{For: body.ID}, ui.Text(docsText(props.Locale, "comment_body"))), html.Textarea(body), status,
			html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: busy.Get()}, ui.Text(docsText(props.Locale, "comment_action")))),
	)
}

// docsSearchProvenance turns a raw match reason ("keyword", "semantic",
// "title", "fuzzy") into a locale-appropriate label. An unrecognized reason
// is shown verbatim rather than suppressed, so a reader always sees why a
// result appeared.
func docsSearchProvenance(locale, why string) string {
	switch strings.ToLower(strings.TrimSpace(why)) {
	case "semantic", "meaning":
		return docsText(locale, "provenance_semantic")
	case "keyword", "text":
		return docsText(locale, "provenance_keyword")
	case "title":
		return docsText(locale, "provenance_title")
	case "fuzzy":
		return docsText(locale, "provenance_fuzzy")
	default:
		return why
	}
}

func docsSearch(view View) ui.Node {
	locale, search := view.Locale.Resolved, view.DocumentSearch
	searchQuery := search.Query
	filters := search.Filters
	queryProps := html.Props{ID: "docs-search-query", Name: "docs_q", Type: "search", Value: search.Query, Raw: map[string]any{"aria-describedby": "docs-search-help"}}
	queryProps.OnInput = ui.UseEvent(func(event ui.InputEvent) { searchQuery = event.GetValue() })
	query := html.Input(queryProps)
	mode := html.Select(html.Props{ID: "docs-search-mode", Name: "mode"},
		html.Option(html.Props{Value: "keyword", Selected: search.Mode != "semantic"}, ui.Text(docsText(locale, "keyword"))),
		html.Option(html.Props{Value: "semantic", Selected: search.Mode == "semantic", Disabled: !search.SemanticAvailable}, ui.Text(docsText(locale, "semantic"))))
	teamProps := html.Props{ID: "docs-search-team", Name: "team", Value: filters.Team}
	teamProps.OnInput = ui.UseEvent(func(event ui.InputEvent) { filters.Team = event.GetValue() })
	channelProps := html.Props{ID: "docs-search-channel", Name: "channel", Value: filters.Channel}
	channelProps.OnInput = ui.UseEvent(func(event ui.InputEvent) { filters.Channel = event.GetValue() })
	ownerProps := html.Props{ID: "docs-search-owner", Name: "owner", Value: filters.Owner}
	ownerProps.OnInput = ui.UseEvent(func(event ui.InputEvent) { filters.Owner = event.GetValue() })
	statusProps := html.Props{ID: "docs-search-status", Name: "status", Value: filters.Status}
	statusProps.OnInput = ui.UseEvent(func(event ui.InputEvent) { filters.Status = event.GetValue() })
	dateFromProps := html.Props{ID: "docs-search-date-from", Name: "date_from", Type: "date", Value: filters.DateFrom}
	dateFromProps.OnInput = ui.UseEvent(func(event ui.InputEvent) { filters.DateFrom = event.GetValue() })
	dateToProps := html.Props{ID: "docs-search-date-to", Name: "date_to", Type: "date", Value: filters.DateTo}
	dateToProps.OnInput = ui.UseEvent(func(event ui.InputEvent) { filters.DateTo = event.GetValue() })
	filterFields := html.Tag("fieldset", html.Props{Class: "docs-search-filters"},
		html.Tag("legend", html.Props{}, ui.Text(docsText(locale, "search_filters"))),
		html.Label(html.Props{For: "docs-search-team"}, ui.Text(docsText(locale, "filter_team"))), html.Input(teamProps),
		html.Label(html.Props{For: "docs-search-channel"}, ui.Text(docsText(locale, "filter_channel"))), html.Input(channelProps),
		html.Label(html.Props{For: "docs-search-owner"}, ui.Text(docsText(locale, "filter_owner"))), html.Input(ownerProps),
		html.Label(html.Props{For: "docs-search-status"}, ui.Text(docsText(locale, "filter_status"))), html.Input(statusProps),
		html.Label(html.Props{For: "docs-search-date-from"}, ui.Text(docsText(locale, "filter_date_from"))), html.Input(dateFromProps),
		html.Label(html.Props{For: "docs-search-date-to"}, ui.Text(docsText(locale, "filter_date_to"))), html.Input(dateToProps),
	)
	children := []ui.Node{
		html.Label(html.Props{For: "docs-search-query"}, ui.Text(docsText(locale, "search"))),
		html.Div(html.Props{Class: "docs-search-row"}, query, mode, html.Button(html.Props{Class: "button primary", Type: "submit"}, ui.Text(docsText(locale, "search_action")))),
		html.P(html.Props{ID: "docs-search-help", Class: "muted"}, ui.Text(docsText(locale, "search_help"))),
		filterFields,
	}
	if search.FallbackUsed {
		children = append(children, html.P(html.Props{Class: "docs-notice", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(locale, "keyword_fallback"))))
	}
	if search.Ready && len(search.Results) == 0 {
		children = append(children, html.P(html.Props{Class: "docs-empty", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(locale, "search_no_results"))))
	}
	for _, result := range search.Results {
		title := ui.Node(ui.Text(result.Title))
		if strings.TrimSpace(result.DocumentID) != "" {
			title = appLink(view, html.Props{}, docsDocumentHref(result.DocumentID), ui.Text(result.Title))
		}
		children = append(children, html.Article(html.Props{Class: "docs-search-result"},
			html.H3(html.Props{}, title),
			html.Span(html.Props{Class: "docs-search-provenance"}, ui.Text(docsSearchProvenance(locale, result.Why))),
			docsField(locale, "owner", result.Owner), docsField(locale, "scope", result.Scope),
			docsField(locale, "version", result.VersionID), docsField(locale, "why", result.Why),
			// Snippet is authorized server-supplied plain text rendered through
			// ui.Text, so it can never execute even when it contains markup
			// characters -- the same guarantee the reader body relies on.
			html.P(html.Props{Class: "docs-snippet"}, ui.Text(result.Snippet))))
	}
	formChildren := append([]ui.Node{html.H2(html.Props{ID: "docs-search-heading"}, ui.Text(docsText(locale, "search_heading")))}, children...)
	formProps := html.Props{Class: "docs-search", Action: "/workspace/app/docs", Method: "get", Raw: map[string]any{"role": "search", "aria-labelledby": "docs-search-heading"}}
	if view.Navigate != nil {
		formProps.OnSubmit = ui.UseEvent(func(event ui.FormEvent) {
			event.PreventDefault()
			values := url.Values{}
			if q := strings.TrimSpace(searchQuery); q != "" {
				values.Set("docs_q", q)
			}
			if v := strings.TrimSpace(filters.Team); v != "" {
				values.Set("team", v)
			}
			if v := strings.TrimSpace(filters.Channel); v != "" {
				values.Set("channel", v)
			}
			if v := strings.TrimSpace(filters.Owner); v != "" {
				values.Set("owner", v)
			}
			if v := strings.TrimSpace(filters.Status); v != "" {
				values.Set("status", v)
			}
			if v := strings.TrimSpace(filters.DateFrom); v != "" {
				values.Set("date_from", v)
			}
			if v := strings.TrimSpace(filters.DateTo); v != "" {
				values.Set("date_to", v)
			}
			href := "/workspace/app/docs"
			if encoded := values.Encode(); encoded != "" {
				href += "?" + encoded
			}
			view.Navigate(href)
		})
	}
	return html.Form(formProps, formChildren...)
}

func docsReviewControls(locale string, reviews []DocumentReviewProjection) ui.Node {
	items := make([]ui.Node, 0, len(reviews))
	for _, review := range reviews {
		if strings.TrimSpace(review.DocumentID) == "" {
			continue
		}
		actions := make([]ui.Node, 0, 2)
		// CanReview and CanDeploy are independent, per-render authority
		// checks: a reviewer without current deploy authority sees no deploy
		// form even if they already reviewed this exact version, and deploy
		// authority revoked after an earlier review removes the deploy form
		// the next time this authorized projection is rendered.
		if review.CanReview {
			actions = append(actions, docsActionForm(review.ReviewAction, docsText(locale, "review"), review))
		}
		if review.CanDeploy {
			actions = append(actions, docsActionForm(review.DeployAction, docsText(locale, "deploy"), review))
		}
		if !review.CanReview && !review.CanDeploy {
			continue
		}
		facts := []ui.Node{
			docsField(locale, "review_state", review.ReviewState),
			docsField(locale, "version", review.VersionID),
			docsField(locale, "review_hash", review.VersionHash),
			docsField(locale, "review_scope", review.Scope),
		}
		var diff ui.Node
		if strings.TrimSpace(review.Diff) != "" {
			diff = html.Div(html.Props{Class: "docs-review-diff-wrap"},
				html.Strong(html.Props{}, ui.Text(docsText(locale, "review_diff"))),
				html.Tag("pre", html.Props{Class: "docs-review-diff"}, ui.Text(review.Diff)))
		}
		items = append(items, html.Li(html.Props{Class: "docs-review-item"}, append(append([]ui.Node{html.H3(html.Props{}, ui.Text(review.Title))}, facts...), diff, html.Div(html.Props{Class: "docs-actions"}, actions...))...))
	}
	if len(items) == 0 {
		return nil
	}
	return html.Section(html.Props{Class: "docs-review", Raw: map[string]any{"aria-labelledby": "docs-review-heading"}}, html.H2(html.Props{ID: "docs-review-heading"}, ui.Text(docsText(locale, "review_heading"))), html.Ul(html.Props{}, items...))
}

type docsCreateFormProps struct {
	Locale string
	Create func(DocumentCreateRequest, func(error))
}

func docsCreateForm(props docsCreateFormProps) ui.Node {
	state := ui.UseState(DocumentCreateRequest{})
	busy := ui.UseState(false)
	failed := ui.UseState(false)
	open := ui.UseState(false)
	restoreFocus := ui.UseState(false)
	titleRef := ui.UseDOMRef()
	triggerRef := ui.UseDOMRef()
	ui.UseAutoFocus(titleRef, open.Get())
	ui.UseEffect(func() func() {
		if restoreFocus.Get() {
			triggerRef.Focus()
		}
		return nil
	}, restoreFocus.Get())
	edit := func(update func(*DocumentCreateRequest)) {
		next := state.Get()
		update(&next)
		state.Set(next)
	}
	// The fields are uncontrolled: a Value prop re-applied on every render
	// drops keystrokes typed while a render is in flight.
	title := html.Props{ID: "docs-create-title", Name: "title", Required: true, MaxLength: 200}
	title.OnInput = ui.UseEvent(func(event ui.InputEvent) { edit(func(next *DocumentCreateRequest) { next.Title = event.GetValue() }) })
	body := html.Props{ID: "docs-create-markdown", Name: "markdown", Required: true, Raw: map[string]any{"aria-describedby": "docs-create-help"}}
	body.OnInput = ui.UseEvent(func(event ui.InputEvent) {
		edit(func(next *DocumentCreateRequest) { next.Markdown = event.GetValue() })
	})
	submit := func(event ui.FormEvent) {
		event.PreventDefault()
		value := state.Get()
		if strings.TrimSpace(value.Title) == "" || strings.TrimSpace(value.Markdown) == "" || props.Create == nil {
			return
		}
		busy.Set(true)
		failed.Set(false)
		props.Create(value, func(err error) {
			busy.Set(false)
			if err != nil {
				failed.Set(true)
				return
			}
			state.Set(DocumentCreateRequest{})
			open.Set(false)
			restoreFocus.Set(true)
		})
	}
	status := ui.Node(nil)
	if failed.Get() {
		status = html.P(html.Props{ID: "docs-create-status", Class: "docs-notice", Raw: map[string]any{"role": "alert"}}, ui.Text(docsText(props.Locale, "create_failed")))
	} else if busy.Get() {
		status = html.P(html.Props{ID: "docs-create-status", Class: "muted", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(props.Locale, "create_busy")))
	}
	confirming := ui.UseState(false)
	focus := useDocsFocus()
	isOpen := open.Get()
	ui.UseLayoutEffect(func() func() {
		if isOpen {
			draft := state.Get()
			setDocsFieldValue("docs-create-title", draft.Title)
			setDocsFieldValue("docs-create-markdown", draft.Markdown)
		}
		return nil
	}, isOpen)
	useDocsModal(isOpen, "docs-create-dialog", "#docs-create-title", "#docs-browse-query")
	discard := func() {
		state.Set(DocumentCreateRequest{})
		failed.Set(false)
		confirming.Set(false)
		open.Set(false)
		restoreFocus.Set(true)
	}
	// Closing never drops a draft silently: with text in either field the
	// dialog asks first, and the question starts on "Keep editing". Asked
	// again (Escape, the scrim) while the question shows, it keeps editing.
	requestClose := func() {
		if busy.Get() {
			return
		}
		if confirming.Get() {
			confirming.Set(false)
			focus(false, "#docs-create-title")
			return
		}
		draft := state.Get()
		if strings.TrimSpace(draft.Title) != "" || strings.TrimSpace(draft.Markdown) != "" {
			confirming.Set(true)
			focus(false, "#docs-create-keep")
			return
		}
		discard()
	}
	latestClose := ui.UseRef(requestClose)
	latestClose.Set(requestClose)
	// Escape closes the dialog wherever focus is, not only inside it.
	ui.UseEffectOf(func() func() {
		if !isOpen {
			return nil
		}
		return docsListenEscape(func() { latestClose.Get()() })
	}, isOpen)
	openClick := ui.UseEvent(func(ui.MouseEvent) {
		if open.Get() {
			requestClose()
			return
		}
		restoreFocus.Set(false)
		open.Set(true)
	})
	cancelClick := ui.UseEvent(func(ui.MouseEvent) { requestClose() })
	discardClick := ui.UseEvent(func(ui.MouseEvent) { discard() })
	keepClick := ui.UseEvent(func(ui.MouseEvent) {
		confirming.Set(false)
		focus(false, "#docs-create-title")
	})
	submitEvent := ui.UseEvent(submit)
	trigger := html.Div(html.Props{Class: "docs-create-trigger"},
		html.Button(html.WithProps(html.Props{Class: "button primary", Type: "button", Raw: map[string]any{"aria-expanded": map[bool]string{true: "true", false: "false"}[open.Get()], "aria-controls": "docs-create-panel"}, OnClick: openClick}, html.Ref(triggerRef)), ui.Text(docsText(props.Locale, "create_open"))),
	)
	if !open.Get() {
		return trigger
	}
	actions := html.Div(html.Props{Class: "docs-create-actions"},
		html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: busy.Get(), OnClick: cancelClick}, ui.Text(docsText(props.Locale, "create_cancel"))),
		html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: busy.Get()}, ui.Text(docsText(props.Locale, "create_action"))))
	if confirming.Get() {
		actions = html.Div(html.Props{Class: "docs-create-discard", Role: "group", Aria: map[string]string{"labelledby": "docs-create-discard-text"}},
			html.P(html.Props{ID: "docs-create-discard-text", Role: "alert"}, ui.Text(docsText(props.Locale, "create_discard_confirm"))),
			html.Div(html.Props{Class: "docs-create-discard-actions"},
				html.Button(html.Props{ID: "docs-create-keep", Class: "button secondary", Type: "button", OnClick: keepClick}, ui.Text(docsText(props.Locale, "create_keep"))),
				html.Button(html.Props{Class: "button secondary docs-danger", Type: "button", OnClick: discardClick}, ui.Text(docsText(props.Locale, "create_discard"))),
			))
	}
	return html.Div(html.Props{Class: "docs-create-stack"}, trigger,
		html.Div(html.Props{ID: "docs-create-panel", Class: "docs-dialog-layer"},
			html.Div(html.Props{Class: "docs-dialog-scrim", OnClick: cancelClick, Raw: map[string]any{"aria-hidden": "true"}}),
			html.Section(html.Props{ID: "docs-create-dialog", Class: "docs-dialog docs-dialog-wide", Role: "dialog", TabIndex: -1, Aria: map[string]string{"modal": "true", "labelledby": "docs-create-heading"}},
				html.Header(html.Props{Class: "docs-dialog-head"},
					html.H2(html.Props{ID: "docs-create-heading"}, ui.Text(docsText(props.Locale, "create_heading"))),
					html.Button(html.Props{Class: "docs-dialog-close", Type: "button", Disabled: busy.Get(), OnClick: cancelClick, Aria: map[string]string{"label": docsText(props.Locale, "create_cancel")}}, productIcon("close", "docs-button-icon")),
				),
				html.Form(html.Props{Class: "docs-dialog-body docs-create-form", OnSubmit: submitEvent},
					html.P(html.Props{ID: "docs-create-help", Class: "docs-dialog-lede"}, ui.Text(docsText(props.Locale, "create_help"))),
					html.Label(html.Props{For: title.ID}, ui.Text(docsText(props.Locale, "title"))), html.Input(html.WithProps(title, html.Ref(titleRef))),
					html.Label(html.Props{For: body.ID}, ui.Text(docsText(props.Locale, "markdown"))), html.Textarea(body),
					status,
					actions),
			),
		))
}

type docsShareFormProps struct {
	Locale     string
	DocumentID string
	Origin     string
	People     []Person
	Share      func(DocumentShareRequest, func(error))
}

func docsShareForm(props docsShareFormProps) ui.Node {
	state := ui.UseState(DocumentShareRequest{DocumentID: props.DocumentID})
	busy := ui.UseState(false)
	failed := ui.UseState(false)
	succeeded := ui.UseState(false)
	edit := func(update func(*DocumentShareRequest)) {
		next := state.Get()
		update(&next)
		state.Set(next)
	}
	recipient := html.Props{ID: "docs-share-recipient", Name: "recipient_id", Required: true, MaxLength: 200, Value: state.Get().RecipientID, Raw: map[string]any{"list": "docs-share-people", "autocomplete": "off", "aria-describedby": "docs-share-help"}}
	recipient.OnInput = ui.UseEvent(func(event ui.InputEvent) {
		edit(func(next *DocumentShareRequest) { next.RecipientID = event.GetValue() })
	})
	peopleOptions := make([]ui.Node, 0, len(props.People))
	for _, person := range props.People {
		if person.LifecycleStatus != "" && !strings.EqualFold(person.LifecycleStatus, "active") {
			continue
		}
		id := strings.TrimSpace(person.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(person.PreferredName)
		if name == "" {
			name = strings.TrimSpace(person.Name)
		}
		if name == "" {
			name = id
		}
		peopleOptions = append(peopleOptions, html.Option(html.Props{Value: id, Raw: map[string]any{"label": name}}, ui.Text(name)))
	}
	status := ui.Node(nil)
	if failed.Get() {
		status = html.P(html.Props{ID: "docs-share-status", Class: "docs-notice", Raw: map[string]any{"role": "alert"}}, ui.Text(docsText(props.Locale, "share_failed")))
	} else if succeeded.Get() {
		status = html.P(html.Props{ID: "docs-share-status", Class: "docs-notice", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(props.Locale, "share_success")))
	} else if busy.Get() {
		status = html.P(html.Props{ID: "docs-share-status", Class: "muted", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(props.Locale, "share_busy")))
	}
	submit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		value := state.Get()
		if strings.TrimSpace(value.RecipientID) == "" || props.Share == nil {
			return
		}
		busy.Set(true)
		failed.Set(false)
		succeeded.Set(false)
		props.Share(value, func(err error) {
			busy.Set(false)
			if err != nil {
				failed.Set(true)
				return
			}
			succeeded.Set(true)
			state.Set(DocumentShareRequest{DocumentID: props.DocumentID})
		})
	})
	link := []ui.Node{}
	if href := docsShareableHref(props.Origin, props.DocumentID); href != "" {
		link = append(link,
			html.Label(html.Props{For: "docs-share-url"}, ui.Text(docsText(props.Locale, "share_url"))),
			html.Input(html.Props{ID: "docs-share-url", Class: "docs-share-url", ReadOnly: true, Value: href, Raw: map[string]any{"aria-label": docsText(props.Locale, "share_url")}}),
		)
	}
	children := []ui.Node{
		html.H3(html.Props{ID: "docs-share-heading"}, ui.Text(docsText(props.Locale, "share_heading"))),
		html.P(html.Props{ID: "docs-share-help", Class: "muted"}, ui.Text(docsText(props.Locale, "share_help"))),
	}
	children = append(children, link...)
	children = append(children, html.Form(html.Props{Class: "docs-share-form", OnSubmit: submit},
		html.Label(html.Props{For: recipient.ID}, ui.Text(docsText(props.Locale, "share_recipient"))), html.Input(recipient),
		html.Tag("datalist", html.Props{ID: "docs-share-people"}, peopleOptions...),
		status,
		html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: busy.Get()}, ui.Text(docsText(props.Locale, "share_action"))),
	))
	return html.Section(html.Props{Class: "docs-share", Raw: map[string]any{"aria-labelledby": "docs-share-heading"}}, children...)
}

// docsActionForm posts the exact document, version and content hash the
// projection just displayed. Review and deploy forms for one review item
// always read VersionHash from the same DocumentReviewProjection value, so
// this UI layer has no code path that could show a reviewer one hash and
// submit a different one for deploy.
func docsActionForm(action, label string, review DocumentReviewProjection) ui.Node {
	children := []ui.Node{
		html.Input(html.Props{Name: "document_id", Type: "hidden", Value: review.DocumentID}),
		html.Input(html.Props{Name: "version_id", Type: "hidden", Value: review.VersionID}),
	}
	if strings.TrimSpace(review.VersionHash) != "" {
		children = append(children, html.Input(html.Props{Name: "version_hash", Type: "hidden", Value: review.VersionHash}))
	}
	children = append(children, html.Button(html.Props{Class: "button primary", Type: "submit"}, ui.Text(label)))
	return html.Form(html.Props{Action: action, Method: "post", Class: "docs-action-form"}, children...)
}
