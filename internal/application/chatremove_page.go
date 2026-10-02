package application

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const ChatModerationPagePath = "/api/chat/moderation/page"

type ChatModerationContext interface {
	ListPosts(context.Context, chat.ListPostsRequest) (chat.ListPostsResponse, error)
	ListMemberships(context.Context, chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error)
}

type ChatModerationDirectory interface {
	ModerationNames(context.Context, chat.Principal, string, []string) (map[string]string, error)
}

type ChatModerationPageHTTP struct {
	HTTP      ChatModerationHTTP
	Context   ChatModerationContext
	Directory ChatModerationDirectory
}

func chatremoveLocale(r *http.Request) string {
	locale := r.URL.Query().Get("locale")
	if locale == "" {
		locale = r.Header.Get("Accept-Language")
	}
	if strings.HasPrefix(locale, "de") {
		return "de-DE"
	}
	if strings.HasPrefix(locale, "ar") {
		return "ar"
	}
	return "en-US"
}

func (h ChatModerationPageHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(405)
		return
	}
	principal, ok := trust.FromContext(r.Context())
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || !principal.ExpiresAt().After(time.Now()) {
		http.Error(w, "authentication required", 401)
		return
	}
	locale := chatremoveLocale(r)
	p := chat.Principal{TenantID: principal.Tenant().String(), SubjectID: principal.Subject()}
	if h.HTTP.Service == nil || h.HTTP.Service.Store == nil {
		h.render(w, chatui.ModerationPage(chatui.ModerationPageModel{Locale: locale, State: chatui.StateError}), 503)
		return
	}
	query := r.URL.Query()
	action := query.Get("action")
	if action != "" {
		cid, id := query.Get("conversation"), query.Get("post")
		if action != "report" && action != "remove" && action != "restore" && action != "message" {
			http.Error(w, "invalid action", 400)
			return
		}
		if h.HTTP.Permissions == nil || cid == "" || id == "" {
			http.Error(w, "unavailable", 503)
			return
		}
		var err error
		if action == "report" {
			err = h.HTTP.Permissions.CanReportMessage(r.Context(), p, p.TenantID, cid, id)
		} else {
			permission := chat.PermissionRemoveMessages
			if action == "restore" {
				permission = chat.PermissionReviewRemovedMessages
			}
			err = h.HTTP.Permissions.CanModerate(r.Context(), p, p.TenantID, cid, permission)
			if err != nil && action == "message" {
				// Writing to the author is open to anyone who can decide an item.
				err = h.HTTP.Permissions.CanModerate(r.Context(), p, p.TenantID, cid, chat.PermissionReviewRemovedMessages)
			}
		}
		if err != nil {
			http.Error(w, "permission denied", 403)
			return
		}
		// The dialog names the author and shows the message as this person may see
		// it; nothing else of the conversation is loaded.
		post, e := h.HTTP.Service.Target(r.Context(), p, p.TenantID, cid, id)
		if e != nil {
			status := 503
			if errors.Is(e, chat.ErrNotFound) || errors.Is(e, chat.ErrPermissionDenied) {
				status = 404
			}
			http.Error(w, "unavailable", status)
			return
		}
		target := chatui.ModerationTargetView{AuthorID: post.AuthorID, HomeTenantID: post.AuthorHomeTenantID, Body: post.Body}
		if h.Directory != nil {
			names, e := h.Directory.ModerationNames(r.Context(), p, p.TenantID, []string{post.AuthorID})
			if e != nil {
				h.render(w, chatui.ModerationPage(chatui.ModerationPageModel{Locale: locale, State: chatui.StateError}), 503)
				return
			}
			target.AuthorName = names[post.AuthorID]
		}
		h.render(w, chatui.ModerationDialog(chatui.ModerationDialogModel{Model: chatui.Model{Locale: locale, SelectedID: cid}, Selection: chat.RemovalSelection{ConversationID: cid, PostIDs: []string{id}}, Target: target, Action: action, Report: action == "report", CaseID: query.Get("case")}), 200)
		return
	}
	// A person who holds no moderation permission sees their notices alone; the
	// queue they could not open would only ever be empty.
	summary, summaryErr := h.HTTP.Service.Summary(r.Context(), p, p.TenantID)
	noQueue := summaryErr == nil && !summary.Moderator
	tab := "open"
	if query.Get("tab") == "resolved" {
		tab = "resolved"
	}
	zone := time.UTC
	if minutes, e := strconv.Atoi(query.Get("tz")); e == nil && minutes > -14*60 && minutes < 15*60 {
		zone = time.FixedZone("", minutes*60)
	}
	var items []chat.ModerationItem
	var err error
	if !noQueue {
		queueQuery := query.Get("query")
		if tab == "resolved" {
			queueQuery = strings.TrimSpace("state:closed " + queueQuery)
		}
		items, err = h.HTTP.Service.Queue(r.Context(), p, p.TenantID, queueQuery)
		if err != nil {
			h.render(w, chatui.ModerationPage(chatui.ModerationPageModel{Locale: locale, State: chatui.StateError}), 503)
			return
		}
	}
	host := query.Get("tenant")
	if host == "" {
		host = p.TenantID
	}
	notices, err := h.HTTP.Service.Store.ModerationNotices(r.Context(), p, host)
	if err != nil {
		h.render(w, chatui.ModerationPage(chatui.ModerationPageModel{Locale: locale, State: chatui.StateError}), 503)
		return
	}
	names := map[string]string{}
	if h.Directory != nil {
		ids := []string{}
		for _, item := range items {
			ids = append(ids, item.AuthorID)
			if item.DecidedBy != "" {
				ids = append(ids, item.DecidedBy)
			}
			if item.ReporterID != "" {
				ids = append(ids, item.ReporterID)
			}
		}
		names, err = h.Directory.ModerationNames(r.Context(), p, p.TenantID, ids)
		if err != nil {
			h.render(w, chatui.ModerationPage(chatui.ModerationPageModel{Locale: locale, State: chatui.StateError}), 503)
			return
		}
	}
	if len(notices) > 20 {
		notices = notices[:20]
	}
	h.render(w, chatui.ModerationPage(chatui.ModerationPageModel{Locale: locale, State: chatui.StateReady, Items: items, Notices: notices, Names: names, Query: query.Get("query"), NoQueue: noQueue, Tab: tab, OpenCount: summary.Open, TimeZone: zone}), 200)
}

func (ChatModerationPageHTTP) render(w http.ResponseWriter, node ui.Node, status int) {
	markup, err := ui.RenderToString(node)
	if err != nil {
		http.Error(w, "unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(markup))
}
