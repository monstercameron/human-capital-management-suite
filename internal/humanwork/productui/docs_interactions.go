package productui

import (
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

func docsSearch(view View) ui.Node {
	locale, search := view.Locale.Resolved, view.DocumentSearch
	searchQuery := search.Query
	queryProps := html.Props{ID: "docs-search-query", Name: "docs_q", Type: "search", Value: search.Query, Raw: map[string]any{"aria-describedby": "docs-search-help"}}
	queryProps.OnInput = ui.UseEvent(func(event ui.InputEvent) { searchQuery = event.GetValue() })
	query := html.Input(queryProps)
	mode := html.Select(html.Props{ID: "docs-search-mode", Name: "mode"},
		html.Option(html.Props{Value: "keyword", Selected: search.Mode != "semantic"}, ui.Text(docsText(locale, "keyword"))),
		html.Option(html.Props{Value: "semantic", Selected: search.Mode == "semantic", Disabled: !search.SemanticAvailable}, ui.Text(docsText(locale, "semantic"))))
	children := []ui.Node{
		html.Label(html.Props{For: "docs-search-query"}, ui.Text(docsText(locale, "search"))),
		html.Div(html.Props{Class: "docs-search-row"}, query, mode, html.Button(html.Props{Class: "button primary", Type: "submit"}, ui.Text(docsText(locale, "search_action")))),
		html.P(html.Props{ID: "docs-search-help", Class: "muted"}, ui.Text(docsText(locale, "search_help"))),
	}
	if search.FallbackUsed {
		children = append(children, html.P(html.Props{Class: "docs-notice", Raw: map[string]any{"role": "status"}}, ui.Text(docsText(locale, "keyword_fallback"))))
	}
	for _, result := range search.Results {
		title := ui.Node(ui.Text(result.Title))
		if strings.TrimSpace(result.DocumentID) != "" {
			title = appLink(view, html.Props{}, docsDocumentHref(result.DocumentID), ui.Text(result.Title))
		}
		children = append(children, html.Article(html.Props{Class: "docs-search-result"},
			html.H3(html.Props{}, title),
			docsField(locale, "owner", result.Owner), docsField(locale, "scope", result.Scope),
			docsField(locale, "version", result.VersionID), docsField(locale, "why", result.Why),
			html.P(html.Props{Class: "docs-snippet"}, ui.Text(result.Snippet))))
	}
	formChildren := append([]ui.Node{html.H2(html.Props{ID: "docs-search-heading"}, ui.Text(docsText(locale, "search_heading")))}, children...)
	formProps := html.Props{Class: "docs-search", Action: "/workspace/app/docs", Method: "get", Raw: map[string]any{"role": "search", "aria-labelledby": "docs-search-heading"}}
	if view.Navigate != nil {
		formProps.OnSubmit = ui.UseEvent(func(event ui.FormEvent) {
			event.PreventDefault()
			view.Navigate(docsBrowseHref(strings.TrimSpace(searchQuery), "all", ""))
		})
	}
	return html.Form(formProps, formChildren...)
}

func docsReviewControls(locale string, reviews []DocumentReviewProjection) ui.Node {
	items := make([]ui.Node, 0, len(reviews))
	for _, review := range reviews {
		if strings.TrimSpace(review.DocumentID) == "" || (!review.CanReview && !review.CanDeploy) {
			continue
		}
		actions := make([]ui.Node, 0, 2)
		if review.CanReview {
			actions = append(actions, docsActionForm(review.ReviewAction, docsText(locale, "review"), review))
		}
		if review.CanDeploy {
			actions = append(actions, docsActionForm(review.DeployAction, docsText(locale, "deploy"), review))
		}
		items = append(items, html.Li(html.Props{Class: "docs-review-item"}, html.H3(html.Props{}, ui.Text(review.Title)), docsField(locale, "review_state", review.ReviewState), docsField(locale, "version", review.VersionID), html.Div(html.Props{Class: "docs-actions"}, actions...)))
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
	title := html.Props{ID: "docs-create-title", Name: "title", Required: true, MaxLength: 200, Value: state.Get().Title}
	title.OnInput = ui.UseEvent(func(event ui.InputEvent) { edit(func(next *DocumentCreateRequest) { next.Title = event.GetValue() }) })
	body := html.Props{ID: "docs-create-markdown", Name: "markdown", Required: true, Value: state.Get().Markdown, Raw: map[string]any{"aria-describedby": "docs-create-help"}}
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
	openClick := ui.UseEvent(func(ui.MouseEvent) {
		if open.Get() {
			state.Set(DocumentCreateRequest{})
			failed.Set(false)
			open.Set(false)
			restoreFocus.Set(true)
			return
		}
		restoreFocus.Set(false)
		open.Set(true)
	})
	cancelClick := ui.UseEvent(func(ui.MouseEvent) {
		state.Set(DocumentCreateRequest{})
		failed.Set(false)
		open.Set(false)
		restoreFocus.Set(true)
	})
	submitEvent := ui.UseEvent(submit)
	trigger := html.Div(html.Props{Class: "docs-create-trigger"},
		html.Button(html.WithProps(html.Props{Class: "button primary", Type: "button", Raw: map[string]any{"aria-expanded": map[bool]string{true: "true", false: "false"}[open.Get()], "aria-controls": "docs-create-panel"}, OnClick: openClick}, html.Ref(triggerRef)), ui.Text(docsText(props.Locale, "create_open"))),
	)
	if !open.Get() {
		return trigger
	}
	return html.Div(html.Props{Class: "docs-create-stack"}, trigger, html.Section(html.Props{ID: "docs-create-panel", Class: "docs-create", Raw: map[string]any{"aria-labelledby": "docs-create-heading"}},
		html.Div(html.Props{Class: "docs-create-header"},
			html.Div(html.Props{}, html.H2(html.Props{ID: "docs-create-heading"}, ui.Text(docsText(props.Locale, "create_heading"))), html.P(html.Props{ID: "docs-create-help", Class: "muted"}, ui.Text(docsText(props.Locale, "create_help")))),
			html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: busy.Get(), OnClick: cancelClick}, ui.Text(docsText(props.Locale, "create_cancel"))),
		),
		html.Form(html.Props{Class: "docs-create-form", OnSubmit: submitEvent},
			html.Label(html.Props{For: title.ID}, ui.Text(docsText(props.Locale, "title"))), html.Input(html.WithProps(title, html.Ref(titleRef))),
			html.Label(html.Props{For: body.ID}, ui.Text(docsText(props.Locale, "markdown"))), html.Textarea(body),
			status,
			html.Div(html.Props{Class: "docs-create-actions"}, html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: busy.Get()}, ui.Text(docsText(props.Locale, "create_action"))))),
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

func docsActionForm(action, label string, review DocumentReviewProjection) ui.Node {
	return html.Form(html.Props{Action: action, Method: "post", Class: "docs-action-form"},
		html.Input(html.Props{Name: "document_id", Type: "hidden", Value: review.DocumentID}),
		html.Input(html.Props{Name: "version_id", Type: "hidden", Value: review.VersionID}),
		html.Button(html.Props{Class: "button primary", Type: "submit"}, ui.Text(label)))
}
