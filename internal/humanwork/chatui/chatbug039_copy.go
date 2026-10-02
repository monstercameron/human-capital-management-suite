package chatui

import "strings"

// chatbug039Text resolves reviewed feature copy before its English fallback.
// Catalog missing-key markers and the key itself are never visible copy.
func chatbug039Text(key, localized, english string) string {
	for _, text := range []string{localized, english} {
		if strings.TrimSpace(text) != "" && text != key && !strings.ContainsAny(text, "⟦⟧") {
			return text
		}
	}
	return ""
}

func chatbug039LocaleIndex(locale string) int {
	switch locale = strings.ToLower(strings.TrimSpace(locale)); {
	case strings.HasPrefix(locale, "de"):
		return 1
	case strings.HasPrefix(locale, "ar"):
		return 2
	default:
		return 0
	}
}

// chatbug039CatalogText resolves the shell's core Chat catalog through the same
// missing-key guard as feature copy. Core translations live in that catalog.
func chatbug039CatalogText(m Model, key, english string) string {
	if m.Text != nil {
		return chatbug039Text(key, m.Text(key), english)
	}
	return chatbug039Text(key, "", english)
}

// chatbug039CompactText reads the three columns of compact feature tables.
func chatbug039CompactText(table, locale, key string) string {
	if key == "" || strings.ContainsAny(key, "\n\x00") {
		return ""
	}
	_, row, ok := strings.Cut(table, "\n"+key+"\x00")
	if !ok {
		return ""
	}
	values := strings.SplitN(row, "\x00", 4)
	if len(values) < 3 {
		return ""
	}
	return chatbug039Text(key, values[chatbug039LocaleIndex(locale)], values[0])
}

// chatbug039SavedCopy applies shared resolution to the Saved panel copy record.
func chatbug039SavedCopy(copy, english SavedCopy) SavedCopy {
	copy.Save = chatbug039Text("", copy.Save, english.Save)
	copy.Saved = chatbug039Text("", copy.Saved, english.Saved)
	copy.Todo = chatbug039Text("", copy.Todo, english.Todo)
	copy.Done = chatbug039Text("", copy.Done, english.Done)
	copy.All = chatbug039Text("", copy.All, english.All)
	copy.Remove = chatbug039Text("", copy.Remove, english.Remove)
	copy.Note = chatbug039Text("", copy.Note, english.Note)
	copy.Remind = chatbug039Text("", copy.Remind, english.Remind)
	copy.Reopen = chatbug039Text("", copy.Reopen, english.Reopen)
	copy.SaveNote = chatbug039Text("", copy.SaveNote, english.SaveNote)
	copy.SaveDue = chatbug039Text("", copy.SaveDue, english.SaveDue)
	copy.ClearDue = chatbug039Text("", copy.ClearDue, english.ClearDue)
	copy.Title = chatbug039Text("", copy.Title, english.Title)
	copy.EmptyTodo = chatbug039Text("", copy.EmptyTodo, english.EmptyTodo)
	copy.EmptyDone = chatbug039Text("", copy.EmptyDone, english.EmptyDone)
	copy.EmptyAll = chatbug039Text("", copy.EmptyAll, english.EmptyAll)
	copy.Loading = chatbug039Text("", copy.Loading, english.Loading)
	copy.Failed = chatbug039Text("", copy.Failed, english.Failed)
	copy.Retry = chatbug039Text("", copy.Retry, english.Retry)
	copy.Limit = chatbug039Text("", copy.Limit, english.Limit)
	copy.NoAccess = chatbug039Text("", copy.NoAccess, english.NoAccess)
	copy.Removed = chatbug039Text("", copy.Removed, english.Removed)
	copy.Deleted = chatbug039Text("", copy.Deleted, english.Deleted)
	copy.AuthorUnavailable = chatbug039Text("", copy.AuthorUnavailable, english.AuthorUnavailable)
	copy.Search = chatbug039Text("", copy.Search, english.Search)
	copy.More = chatbug039Text("", copy.More, english.More)
	copy.Close = chatbug039Text("", copy.Close, english.Close)
	copy.Updated = chatbug039Text("", copy.Updated, english.Updated)
	copy.Invalid = chatbug039Text("", copy.Invalid, english.Invalid)
	copy.Private = chatbug039Text("", copy.Private, english.Private)
	copy.Unsave = chatbug039Text("", copy.Unsave, english.Unsave)
	return copy
}
