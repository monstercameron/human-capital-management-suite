package chatui

import (
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3932. Private labels on saved messages. A label is the owner's own name
// for a group of saved messages (open enrollment, a shift handover, a hiring
// cycle). It is drawn only in the Saved panel, which only its owner can open.

// SavedLabelOption is one label in use and how many saved messages carry it.
type SavedLabelOption struct {
	Name  string
	Count int
}

// SavedLabelsView is the label part of the Saved panel's state.
type SavedLabelsView struct {
	// Options are every label in use across the whole list, with its count.
	Options []SavedLabelOption
	// Unlabelled counts the saved messages that have no label.
	UnlabelledCount int
	// Filter is the label the list is narrowed to; Only is true for the
	// Unlabelled view. Neither set shows every saved message.
	Filter         string
	OnlyUnlabelled bool
	// EditingItem is the key of the saved message whose label editor is open.
	EditingItem string
	// Manage is "rename" or "remove" while the filtered label's manager is open.
	Manage string
}

// SavedLabelOptions counts labels over rows, case-insensitively, keeping the
// first spelling, in name order.
func SavedLabelOptions(rows [][]string) (options []SavedLabelOption, unlabelled int) {
	index := map[string]int{}
	for _, labels := range rows {
		if len(labels) == 0 {
			unlabelled++
			continue
		}
		seen := map[string]bool{}
		for _, label := range labels {
			key := strings.ToLower(label)
			if seen[key] {
				continue
			}
			seen[key] = true
			if at, ok := index[key]; ok {
				options[at].Count++
				continue
			}
			index[key] = len(options)
			options = append(options, SavedLabelOption{Name: label, Count: 1})
		}
	}
	sort.SliceStable(options, func(i, j int) bool { return strings.ToLower(options[i].Name) < strings.ToLower(options[j].Name) })
	return options, unlabelled
}

// SavedRowHasLabel says whether labels contain name, ignoring letter case.
func SavedRowHasLabel(labels []string, name string) bool {
	for _, label := range labels {
		if strings.EqualFold(label, name) {
			return true
		}
	}
	return false
}

var chatsave3932Table = map[string][3]string{
	"lbl_group":         {"Filter by label", "Nach Label filtern", "التصفية حسب التصنيف"},
	"lbl_all":           {"All labels", "Alle Labels", "كل التصنيفات"},
	"lbl_none":          {"Unlabelled", "Ohne Label", "بلا تصنيف"},
	"lbl_add":           {"Add label", "Label hinzufügen", "إضافة تصنيف"},
	"lbl_field":         {"Add a label", "Label hinzufügen", "أضف تصنيفًا"},
	"lbl_drop":          {"Remove label {label} from this message", "Label {label} von dieser Nachricht entfernen", "إزالة التصنيف {label} من هذه الرسالة"},
	"lbl_list":          {"Labels for this saved message", "Labels dieser gespeicherten Nachricht", "تصنيفات هذه الرسالة المحفوظة"},
	"lbl_private":       {"Only you can see your labels. They are not posted or shared.", "Nur Sie sehen Ihre Labels. Sie werden weder veröffentlicht noch geteilt.", "أنت وحدك ترى تصنيفاتك. لا تُنشر ولا تُشارك."},
	"lbl_hint":          {"Separate labels with commas. Enter to save, Esc to cancel", "Labels mit Kommas trennen. Enter zum Speichern, Esc zum Abbrechen", "افصل بين التصنيفات بفواصل. Enter للحفظ وEsc للإلغاء"},
	"lbl_yours":         {"Your labels", "Ihre Labels", "تصنيفاتك"},
	"lbl_use":           {"Add label {label}", "Label {label} hinzufügen", "إضافة التصنيف {label}"},
	"lbl_manage":        {"Manage label {label}", "Label {label} verwalten", "إدارة التصنيف {label}"},
	"lbl_rename":        {"Rename label", "Label umbenennen", "إعادة تسمية التصنيف"},
	"lbl_rename_field":  {"New name for label {label}", "Neuer Name für das Label {label}", "اسم جديد للتصنيف {label}"},
	"lbl_remove":        {"Remove label", "Label entfernen", "إزالة التصنيف"},
	"lbl_remove_ask":    {"Remove the label {label}? Your saved messages stay saved and keep their other labels.", "Label {label} entfernen? Ihre gespeicherten Nachrichten bleiben gespeichert und behalten ihre anderen Labels.", "إزالة التصنيف {label}؟ تبقى رسائلك المحفوظة محفوظة وتحتفظ بتصنيفاتها الأخرى."},
	"lbl_empty":         {"No saved messages have this label.", "Keine gespeicherte Nachricht hat dieses Label.", "لا توجد رسائل محفوظة بهذا التصنيف."},
	"lbl_empty_none":    {"Every saved message here has a label.", "Jede gespeicherte Nachricht hat hier ein Label.", "لكل رسالة محفوظة هنا تصنيف."},
	"lbl_limit":         {"A message can have up to 10 labels, and you can use up to 50 different labels.", "Eine Nachricht kann bis zu 10 Labels haben, und Sie können bis zu 50 verschiedene Labels verwenden.", "يمكن أن تحمل الرسالة حتى 10 تصنيفات، ويمكنك استخدام حتى 50 تصنيفًا مختلفًا."},
	"lbl_empty_tab":     {"No messages on this tab match your label choice.", "Keine Nachricht auf dieser Registerkarte passt zu Ihrer Label-Auswahl.", "لا توجد رسائل في هذا التبويب تطابق اختيارك للتصنيف."},
	"lbl_invalid":       {"A label can have up to 40 characters. Shorten it and try again.", "Ein Label darf höchstens 40 Zeichen haben. Kürzen Sie es und versuchen Sie es erneut.", "يمكن أن يحتوي التصنيف على 40 حرفًا كحد أقصى. اختصره وحاول مرة أخرى."},
	"lbl_filtered_note": {"Showing saved messages with the label {label}.", "Gespeicherte Nachrichten mit dem Label {label}.", "عرض الرسائل المحفوظة بالتصنيف {label}."},
}

func init() {
	for key, value := range chatsave3932Table {
		chatsave002Table[key] = value
	}
	chatsave002Paths["tag"] = "M3.5 4.5h7.6l9 9-7.6 7.6-9-9V4.5zM7.5 8.5h.01"
}

func chatsave3932Data(row SavedMessageRow, extra map[string]string) map[string]string {
	data := map[string]string{"saved-host": row.TenantID, "saved-conversation": row.ConversationID, "saved-post": row.PostID}
	for k, v := range extra {
		data[k] = v
	}
	return data
}

// chatsave3932Filter is the label row under the tabs: every label in use, an
// "All labels" and an "Unlabelled" choice, and, once a label is chosen, the
// controls that rename or remove it.
func chatsave3932Filter(view SavedMessagesView) ui.Node {
	lv := view.Labels
	loc := view.Locale
	if len(lv.Options) == 0 && lv.Filter == "" && !lv.OnlyUnlabelled {
		return nil
	}
	chip := func(id, text string, count int, pressed bool, data map[string]string) ui.Node {
		children := []ui.Node{html.Span(html.Props{Dir: "auto", Text: text})}
		if count >= 0 {
			children = append(children, html.Span(html.Props{Class: "chatsave-seg-count", Text: chatsave002Num(loc, count)}))
		}
		return html.Button(html.Props{ID: id, Class: "chatsave-labelchip", Type: "button", Data: data, Aria: map[string]string{"pressed": boolString(pressed)}}, children...)
	}
	chips := []ui.Node{chip("chatsave-labelf-all", chatsave002Text(loc, "lbl_all"), -1, lv.Filter == "" && !lv.OnlyUnlabelled, map[string]string{"saved-action": "label-all"})}
	for i, option := range lv.Options {
		chips = append(chips, chip("chatsave-labelf-"+chatsave002Num("en", i), option.Name, option.Count, strings.EqualFold(lv.Filter, option.Name), map[string]string{"saved-action": "label-filter", "saved-label": option.Name}))
	}
	if lv.UnlabelledCount > 0 || lv.OnlyUnlabelled {
		chips = append(chips, chip("chatsave-labelf-none", chatsave002Text(loc, "lbl_none"), lv.UnlabelledCount, lv.OnlyUnlabelled, map[string]string{"saved-action": "label-unlabelled"}))
	}
	nodes := []ui.Node{html.Div(html.Props{Class: "chatsave-labelchips", Role: "group", Aria: map[string]string{"label": chatsave002Text(loc, "lbl_group")}}, chips...)}
	if lv.Filter != "" {
		nodes = append(nodes, chatsave3932Manage(view))
	}
	return html.Div(html.Props{Class: "chatsave-labels", Data: map[string]string{"saved-labels": "true"}}, nodes...)
}

func chatsave3932Manage(view SavedMessagesView) ui.Node {
	lv := view.Labels
	loc := view.Locale
	name := lv.Filter
	group := html.Props{Class: "chatsave-labelmanage", Role: "group", Aria: map[string]string{"label": chatsave002Text(loc, "lbl_manage", "label", name)}}
	switch lv.Manage {
	case "rename":
		return html.Form(html.Props{Class: "chatsave-labelmanage chatsave-labelform", Data: map[string]string{"saved-form": "label-rename", "saved-label": name}},
			html.Label(html.Props{Class: "sr-only", For: "chatsave-label-rename-field", Text: chatsave002Text(loc, "lbl_rename_field", "label", name)}),
			html.Input(html.Props{ID: "chatsave-label-rename-field", Class: "chatsave-note-input", Name: "label", Type: "text", MaxLength: 40, AutoComplete: "off", Required: true, Data: map[string]string{"chat-value": name}, Aria: map[string]string{"describedby": "chatsave-status chatsave-label-private"}}),
			html.P(html.Props{ID: "chatsave-label-private", Class: "chatsave-note-private", Text: chatsave002Text(loc, "lbl_private")}),
			html.Div(html.Props{Class: "chatsave-note-buttons"},
				html.Button(html.Props{Class: "button small", Type: "submit", Text: chatsave002Text(loc, "note_save")}),
				html.Button(html.Props{ID: "chatsave-label-manage-cancel", Class: "button secondary small", Type: "button", Text: chatsave002Text(loc, "note_cancel"), Data: map[string]string{"saved-action": "label-manage-cancel"}})))
	case "remove":
		return html.Div(html.Props{Class: "chatsave-labelmanage chatsave-labelconfirm", Role: "group", Aria: map[string]string{"labelledby": "chatsave-label-ask"}},
			html.P(html.Props{ID: "chatsave-label-ask", Class: "chatsave-note-private", Dir: "auto", Text: chatsave002Text(loc, "lbl_remove_ask", "label", name)}),
			html.Div(html.Props{Class: "chatsave-note-buttons"},
				html.Button(html.Props{ID: "chatsave-label-remove-confirm", Class: "button small", Type: "button", Text: chatsave002Text(loc, "lbl_remove"), Data: map[string]string{"saved-action": "label-remove-confirm", "saved-label": name}}),
				html.Button(html.Props{ID: "chatsave-label-manage-cancel", Class: "button secondary small", Type: "button", Text: chatsave002Text(loc, "note_cancel"), Data: map[string]string{"saved-action": "label-manage-cancel"}})))
	}
	return html.Div(group,
		html.Button(html.Props{ID: "chatsave-label-rename", Class: "chatsave-labelaction", Type: "button", Text: chatsave002Text(loc, "lbl_rename"), Data: map[string]string{"saved-action": "label-rename"}}),
		html.Button(html.Props{ID: "chatsave-label-remove", Class: "chatsave-labelaction", Type: "button", Text: chatsave002Text(loc, "lbl_remove"), Data: map[string]string{"saved-action": "label-remove"}}))
}

// chatsave3932EmptyText is what an empty label view says, or "" when no label
// view is chosen. On the To do and Done tabs the label may still be on messages
// of the other tab, so it says so and offers Show All instead of claiming that
// no saved message has the label.
func chatsave3932EmptyText(view SavedMessagesView) (string, ui.Node) {
	if !view.Labels.OnlyUnlabelled && view.Labels.Filter == "" {
		return "", nil
	}
	if view.Tab == "todo" || view.Tab == "done" || view.Tab == "" {
		link := html.Button(html.Props{Class: "chatsave-empty-link", Type: "button", Text: chatbug091Text(view.Locale, "show_all"), Data: map[string]string{"saved-tab": "all"}})
		return chatsave002Text(view.Locale, "lbl_empty_tab"), link
	}
	if view.Labels.OnlyUnlabelled {
		return chatsave002Text(view.Locale, "lbl_empty_none"), nil
	}
	return chatsave002Text(view.Locale, "lbl_empty"), nil
}

// chatsave3932Item is the label line of one saved message: its labels, each
// removable, and a button that opens the editor. Done messages keep it; a
// message the person can no longer open shows the labels without the editor.
func chatsave3932Item(view SavedMessagesView, row SavedMessageRow, key string, editable bool) ui.Node {
	loc := view.Locale
	if len(row.Labels) == 0 && !editable {
		return nil
	}
	chips := []ui.Node{}
	for _, label := range row.Labels {
		chips = append(chips, html.Span(html.Props{Class: "chatsave-tag"},
			html.Span(html.Props{Class: "chatsave-tag-name", Dir: "auto", Text: label}),
			html.Button(html.Props{Class: "chatsave-tag-x", Type: "button", Title: chatsave002Text(loc, "lbl_drop", "label", label), Data: chatsave3932Data(row, map[string]string{"saved-action": "label-drop", "saved-label": label}), Aria: map[string]string{"label": chatsave002Text(loc, "lbl_drop", "label", label)}}, icon("close"))))
	}
	if editable && view.Labels.EditingItem != key {
		add := chatsave002Text(loc, "lbl_add")
		chips = append(chips, html.Button(html.Props{ID: "chatsave-labeladd-" + row.PostID, Class: "chatsave-tag-add", Type: "button", Data: chatsave3932Data(row, map[string]string{"saved-action": "label-edit"}), Aria: map[string]string{"label": add}}, chatsave002Icon("tag", false), html.Span(html.Props{Text: add})))
	}
	nodes := []ui.Node{}
	if len(chips) > 0 {
		nodes = append(nodes, html.Div(html.Props{Class: "chatsave-tags", Role: "group", Aria: map[string]string{"label": chatsave002Text(loc, "lbl_list")}}, chips...))
	}
	if editable && view.Labels.EditingItem == key {
		nodes = append(nodes, chatsave3932Editor(view, row))
	}
	return html.Div(html.Props{Class: "chatsave-labelline"}, nodes...)
}

func chatsave3932Editor(view SavedMessagesView, row SavedMessageRow) ui.Node {
	loc := view.Locale
	fieldID := "chatsave-labels-field-" + row.PostID
	privateID := "chatsave-labels-private-" + row.PostID
	form := []ui.Node{
		html.Label(html.Props{Class: "sr-only", For: fieldID, Text: chatsave002Text(loc, "lbl_field")}),
		html.Input(html.Props{ID: fieldID, Class: "chatsave-note-input", Name: "label", Type: "text", MaxLength: 200, Placeholder: chatsave002Text(loc, "lbl_field"), AutoComplete: "off", Aria: map[string]string{"describedby": "chatsave-status " + privateID}}),
		html.P(html.Props{ID: privateID, Class: "chatsave-note-private", Text: chatsave002Text(loc, "lbl_private")}),
	}
	var suggestions []ui.Node
	for _, option := range view.Labels.Options {
		if SavedRowHasLabel(row.Labels, option.Name) {
			continue
		}
		suggestions = append(suggestions, html.Button(html.Props{Class: "chatsave-tag-suggest", Type: "button", Data: chatsave3932Data(row, map[string]string{"saved-action": "label-add", "saved-label": option.Name}), Aria: map[string]string{"label": chatsave002Text(loc, "lbl_use", "label", option.Name)}}, html.Span(html.Props{Dir: "auto", Text: option.Name})))
	}
	if len(suggestions) > 0 {
		form = append(form, html.Div(html.Props{Class: "chatsave-tags", Role: "group", Aria: map[string]string{"label": chatsave002Text(loc, "lbl_yours")}}, suggestions...))
	}
	form = append(form,
		html.Div(html.Props{Class: "chatsave-note-buttons"},
			html.Button(html.Props{Class: "button small", Type: "submit", Text: chatsave002Text(loc, "note_save")}),
			html.Button(html.Props{ID: "chatsave-labels-cancel-" + row.PostID, Class: "button secondary small", Type: "button", Text: chatsave002Text(loc, "note_cancel"), Data: chatsave3932Data(row, map[string]string{"saved-action": "label-cancel"})})),
		html.P(html.Props{Class: "chatsave-note-hint", Text: chatsave002Text(loc, "lbl_hint")}))
	return html.Form(html.Props{Class: "chatsave-note-form chatsave-labels-form", Data: chatsave3932Data(row, map[string]string{"saved-form": "labels"})}, form...)
}
