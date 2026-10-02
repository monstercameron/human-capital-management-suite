package chatui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATMAP-006: the workspace's location settings sit on the Chat settings page
// beside Translation, drawn with the same section styles. Each switch and limit
// saves as soon as it changes, with "Saved" beside it; the one button on the page
// is "Save country", for the country list's own form.

// ChatmapCountry is one country row: whether sharing is on there and why.
type ChatmapCountry struct {
	Country string
	Enabled bool
	Basis   string
}

// ChatmapAdminData is what the server answers for the workspace settings.
type ChatmapAdminData struct {
	Policy struct {
		SharingEnabled, LiveEnabled, ExactAllowed bool
		MaxLiveSeconds, MaxRetentionSeconds       int
		CanAdminister                             bool
	}
	Jurisdictions []ChatmapCountry
}

// ChatmapAdminModel is everything the settings need to draw. A nil handler
// draws its control disabled.
type ChatmapAdminModel struct {
	Locale                 string
	Data                   ChatmapAdminData
	Loaded                 bool
	Loading, Failed, Saved bool
	// SavedField names the control whose change was saved last.
	SavedField string
	Denied     bool
	SignedOut  bool
	// Invalid is true when the country form was submitted without a country or
	// without its basis.
	Invalid bool
	// SavePolicy saves the switches and limits; SaveCountry saves the country
	// form; Retry reads the settings again after a failure.
	SavePolicy, SaveCountry, Retry ui.Handler
}

func (m ChatmapAdminModel) failureKey() string {
	if m.SignedOut {
		return "admin_signed_out"
	}
	return "admin_error"
}

// ChatmapAdminText returns one string of the settings page in the person's
// language.
func ChatmapAdminText(locale, key string) string {
	values, ok := chatmapAdminCopy[key]
	if !ok {
		return ""
	}
	index := 0
	if strings.HasPrefix(locale, "de") {
		index = 1
	}
	if strings.HasPrefix(locale, "ar") {
		index = 2
	}
	return chatbug039Text(key, values[index], values[0])
}

var chatmapAdminCopy = map[string][3]string{
	"admin_title":           {"Location sharing", "Standortfreigabe", "مشاركة الموقع"},
	"admin_intro":           {"These settings apply to the whole workspace. Each one saves as soon as you change it. A shared location is never used for time or pay.", "Diese Einstellungen gelten für den gesamten Arbeitsbereich. Jede wird gespeichert, sobald Sie sie ändern. Ein geteilter Standort wird nie für Zeit oder Entgelt verwendet.", "تنطبق هذه الإعدادات على مساحة العمل كاملة. يُحفظ كل إعداد بمجرد تغييره. لا يُستخدم الموقع المشارك أبداً لحساب الوقت أو الأجر."},
	"admin_loading":         {"Loading location settings. Please wait.", "Standorteinstellungen werden geladen. Bitte warten.", "جار تحميل إعدادات الموقع. يرجى الانتظار."},
	"admin_error":           {"Location settings could not load or save. Try again.", "Standorteinstellungen konnten nicht geladen oder gespeichert werden. Versuchen Sie es erneut.", "تعذر تحميل إعدادات الموقع أو حفظها. حاول مجدداً."},
	"admin_signed_out":      {"You were signed out, so location settings could not load or save. Reload the page to sign in again.", "Sie wurden abgemeldet, daher konnten die Standorteinstellungen nicht geladen oder gespeichert werden. Laden Sie die Seite neu und melden Sie sich erneut an.", "تم تسجيل خروجك لذا تعذر تحميل إعدادات الموقع أو حفظها. أعد تحميل الصفحة لتسجيل الدخول مجددًا."},
	"admin_retry":           {"Try again", "Erneut versuchen", "حاول مجدداً"},
	"admin_denied":          {"Only a workspace administrator can change location sharing for the workspace.", "Nur Arbeitsbereichsadministratoren können die Standortfreigabe für den Arbeitsbereich ändern.", "يمكن فقط لمسؤول مساحة العمل تغيير مشاركة الموقع لمساحة العمل."},
	"admin_saved":           {"Saved", "Gespeichert", "تم الحفظ"},
	"admin_sharing":         {"Allow people to share a location", "Das Teilen eines Standorts erlauben", "السماح للأشخاص بمشاركة موقع"},
	"admin_sharing_hint":    {"When off, nobody can share a location in any channel. Locations already shared end at their time.", "Wenn ausgeschaltet, kann niemand in einem Kanal einen Standort teilen. Bereits geteilte Standorte enden zur festgelegten Zeit.", "عند إيقافها لا يستطيع أحد مشاركة موقع في أي قناة. تنتهي المواقع المشاركة بالفعل في وقتها."},
	"admin_live":            {"Allow live sharing", "Live-Teilen erlauben", "السماح بالمشاركة المباشرة"},
	"admin_live_hint":       {"Live sharing follows a person's position while their page is open, for a time they choose.", "Beim Live-Teilen wird die Position einer Person verfolgt, solange ihre Seite geöffnet ist, für eine von ihr gewählte Zeit.", "تتبع المشاركة المباشرة موقع الشخص ما دامت صفحته مفتوحة، للمدة التي يختارها."},
	"admin_exact":           {"Allow exact positions", "Genaue Positionen erlauben", "السماح بالمواقع الدقيقة"},
	"admin_exact_hint":      {"When off, people can share only an approximate position.", "Wenn ausgeschaltet, können Personen nur eine ungefähre Position teilen.", "عند إيقافها يمكن للأشخاص مشاركة موقع تقريبي فقط."},
	"admin_max_live":        {"Longest live share", "Längste Live-Freigabe", "أطول مشاركة مباشرة"},
	"admin_retention":       {"Longest time a location is kept", "Längste Aufbewahrung eines Standorts", "أطول مدة لحفظ الموقع"},
	"admin_retention_hint":  {"You can shorten this and never lengthen it: nothing is kept longer than 24 hours.", "Sie können dies verkürzen, aber nie verlängern: Nichts wird länger als 24 Stunden aufbewahrt.", "يمكنك تقصير هذه المدة ولا يمكن إطالتها: لا يُحفظ شيء أكثر من 24 ساعة."},
	"admin_d15m":            {"15 minutes", "15 Minuten", "15 دقيقة"},
	"admin_d1h":             {"1 hour", "1 Stunde", "ساعة واحدة"},
	"admin_d8h":             {"8 hours", "8 Stunden", "8 ساعات"},
	"admin_d24h":            {"24 hours", "24 Stunden", "24 ساعة"},
	"admin_minutes":         {"%d minutes", "%d Minuten", "%d دقيقة"},
	"admin_countries":       {"By country", "Nach Land", "حسب البلد"},
	"admin_countries_hint":  {"Switch location sharing off where an agreement with employee representatives is needed and not yet made. A person's country comes from their work location in the people directory. Saving a country that is already listed replaces its row.", "Schalten Sie die Standortfreigabe dort aus, wo eine Vereinbarung mit der Arbeitnehmervertretung nötig und noch nicht getroffen ist. Das Land einer Person ergibt sich aus ihrem Arbeitsort im Personenverzeichnis. Wird ein bereits aufgeführtes Land gespeichert, ersetzt es dessen Zeile.", "أوقف مشاركة الموقع حيث يلزم اتفاق مع ممثلي الموظفين ولم يُبرم بعد. يُستمد بلد الشخص من موقع عمله في دليل الأشخاص. يؤدي حفظ بلد مدرج بالفعل إلى استبدال سطره."},
	"admin_countries_empty": {"No country is set apart. Location sharing follows the settings above everywhere.", "Kein Land ist gesondert festgelegt. Die Standortfreigabe folgt überall den Einstellungen oben.", "لا يوجد بلد مخصص. تتبع مشاركة الموقع الإعدادات أعلاه في كل مكان."},
	"admin_country":         {"Country code or name", "Ländercode oder Name", "رمز البلد أو اسمه"},
	"admin_country_hint":    {"For example DE, US or France. Use * for everywhere.", "Zum Beispiel DE, US oder Frankreich. * steht für überall.", "مثل DE أو US أو فرنسا. استخدم * لكل مكان."},
	"admin_country_state":   {"Location sharing there", "Standortfreigabe dort", "مشاركة الموقع هناك"},
	"admin_on":              {"On", "An", "مفعّلة"},
	"admin_off":             {"Off", "Aus", "متوقفة"},
	"admin_basis":           {"Basis for this choice", "Grundlage für diese Entscheidung", "الأساس لهذا القرار"},
	"admin_basis_hint":      {"Recorded with the change, for example the agreement or the date it is expected.", "Wird mit der Änderung festgehalten, zum Beispiel die Vereinbarung oder das erwartete Datum.", "يُسجَّل مع التغيير، مثل الاتفاق أو التاريخ المتوقع."},
	"admin_save_country":    {"Save country", "Land speichern", "حفظ البلد"},
	"admin_country_invalid": {"Enter a country and the basis for the choice.", "Geben Sie ein Land und die Grundlage der Entscheidung ein.", "أدخل بلداً وأساس القرار."},
	"admin_everywhere":      {"Everywhere else", "Überall sonst", "في كل مكان آخر"},
	"admin_row":             {"%s: location sharing %s. Basis: %s", "%s: Standortfreigabe %s. Grundlage: %s", "%s: مشاركة الموقع %s. الأساس: %s"},
}

// chatmapAdminDuration names a length of time the way the limits' options do.
func chatmapAdminDuration(locale string, seconds int) string {
	switch seconds {
	case 900:
		return ChatmapAdminText(locale, "admin_d15m")
	case 3600:
		return ChatmapAdminText(locale, "admin_d1h")
	case 28800:
		return ChatmapAdminText(locale, "admin_d8h")
	case 86400:
		return ChatmapAdminText(locale, "admin_d24h")
	}
	return fmt.Sprintf(ChatmapAdminText(locale, "admin_minutes"), max(1, seconds/60))
}

func chatmapAdminSaved(m ChatmapAdminModel, field string) ui.Node {
	if !m.Saved || m.Loading || m.SavedField != field {
		return nil
	}
	return html.Span(html.Props{Class: "chatlangadmin-saved", Role: "status", Aria: map[string]string{"live": "polite"}}, icon("check"), html.Span(html.Props{Text: ChatmapAdminText(m.Locale, "admin_saved")}))
}

// ChatmapWorkspaceSettings draws the workspace's location settings for the Chat
// settings page. It is the shell (loading, refusal, failure with Try again) until
// the settings have been read, and nothing at all is guessed meanwhile.
func ChatmapWorkspaceSettings(m ChatmapAdminModel) ui.Node {
	t := func(key string) string { return ChatmapAdminText(m.Locale, key) }
	dir := direction(m.Locale)
	retry := func() ui.Node {
		return html.Button(html.Props{Type: "button", Class: "button secondary", Text: t("admin_retry"), OnClick: m.Retry, Disabled: m.Loading || m.Retry.Value() == nil})
	}
	switch {
	case !m.Loaded && !m.Failed:
		return html.Section(html.Props{Class: "chatlangadmin chatlangadmin-page chatmapadmin", Dir: dir, Role: "status", Aria: map[string]string{"busy": "true"}}, html.H2(html.Props{Text: t("admin_title")}), html.P(html.Props{Text: t("admin_loading")}))
	case m.Denied:
		// A person who is not a workspace administrator is told so once and shown
		// nothing else: the page already says who can change what.
		return html.Section(html.Props{Class: "chatlangadmin chatlangadmin-page chatmapadmin", Dir: dir, Role: "alert"}, html.H2(html.Props{Text: t("admin_title")}), html.P(html.Props{Text: t("admin_denied")}))
	case m.Failed && !m.Loaded:
		return html.Section(html.Props{Class: "chatlangadmin chatlangadmin-page chatmapadmin", Dir: dir, Role: "alert"}, html.H2(html.Props{Text: t("admin_title")}), html.P(html.Props{Text: t(m.failureKey())}), retry())
	}
	if !m.Data.Policy.CanAdminister {
		return html.Section(html.Props{Class: "chatlangadmin chatlangadmin-page chatmapadmin", Dir: dir, Role: "alert"}, html.H2(html.Props{Text: t("admin_title")}), html.P(html.Props{Text: t("admin_denied")}))
	}
	p := m.Data.Policy
	problem := ""
	switch {
	case m.Loading:
		problem = t("admin_loading")
	case m.Invalid:
		problem = t("admin_country_invalid")
	case m.Failed:
		problem = t(m.failureKey())
	}
	check := func(id, name, label, hint string, checked bool) ui.Node {
		return html.Div(html.Props{Class: "chatlangadmin-setting"},
			html.Label(html.Props{Class: "chatlangadmin-check", For: id},
				html.Input(html.Props{ID: id, Class: "switch", Role: "switch", Name: name, Type: "checkbox", Checked: checked, Disabled: m.Loading || m.SavePolicy.Value() == nil, Aria: map[string]string{"describedby": id + "-hint"}}),
				html.Span(html.Props{Text: label}), chatmapAdminSaved(m, name)),
			html.P(html.Props{ID: id + "-hint", Class: "field-hint", Text: hint}))
	}
	choose := func(id, name, label, hint string, current int, values []int) ui.Node {
		options := []ui.Node{}
		listed := false
		for _, v := range values {
			listed = listed || v == current
		}
		if !listed {
			options = append(options, html.Option(html.Props{Value: strconv.Itoa(current), Selected: true, Text: chatmapAdminDuration(m.Locale, current)}))
		}
		for _, v := range values {
			options = append(options, html.Option(html.Props{Value: strconv.Itoa(v), Selected: v == current, Text: chatmapAdminDuration(m.Locale, v)}))
		}
		setting := []ui.Node{
			html.Label(html.Props{For: id}, ui.Text(label), chatmapAdminSaved(m, name)),
			html.Select(html.Props{ID: id, Name: name, Disabled: m.Loading || m.SavePolicy.Value() == nil}, options...),
		}
		if hint != "" {
			setting[1] = html.Select(html.Props{ID: id, Name: name, Disabled: m.Loading || m.SavePolicy.Value() == nil, Aria: map[string]string{"describedby": id + "-hint"}}, options...)
			setting = append(setting, html.P(html.Props{ID: id + "-hint", Class: "field-hint", Text: hint}))
		}
		return html.Div(html.Props{Class: "chatlangadmin-setting"}, setting...)
	}
	form := html.Form(html.Props{Class: "chatlangadmin-form", Data: map[string]string{"chatmapadmin": "policy"}, OnChange: m.SavePolicy, OnSubmit: m.SavePolicy, Aria: map[string]string{"labelledby": "chatmapadmin-title"}},
		check("chatmapadmin-sharing", "sharing", t("admin_sharing"), t("admin_sharing_hint"), p.SharingEnabled),
		check("chatmapadmin-live", "live", t("admin_live"), t("admin_live_hint"), p.LiveEnabled),
		check("chatmapadmin-exact", "exact", t("admin_exact"), t("admin_exact_hint"), p.ExactAllowed),
		choose("chatmapadmin-maxlive", "maxlive", t("admin_max_live"), "", p.MaxLiveSeconds, []int{900, 3600, 28800, 86400}),
		choose("chatmapadmin-retention", "retention", t("admin_retention"), t("admin_retention_hint"), p.MaxRetentionSeconds, []int{900, 3600, 28800, 86400}))
	rows := []ui.Node{}
	for _, c := range m.Data.Jurisdictions {
		name, state := c.Country, t("admin_off")
		if c.Country == "*" {
			name = t("admin_everywhere")
		}
		if c.Enabled {
			state = t("admin_on")
		}
		rows = append(rows, html.Li(html.Props{Class: "chatlangadmin-term"}, html.Span(html.Props{Class: "chatlangadmin-term-text", Dir: "auto", Text: fmt.Sprintf(t("admin_row"), name, state, c.Basis)})))
	}
	countries := []ui.Node{html.H3(html.Props{ID: "chatmapadmin-countries-title", Text: t("admin_countries")}), html.P(html.Props{Class: "field-hint", Text: t("admin_countries_hint")})}
	if len(rows) == 0 {
		countries = append(countries, html.P(html.Props{Text: t("admin_countries_empty")}))
	} else {
		countries = append(countries, html.Ul(html.Props{Class: "chatlangadmin-terms", Aria: map[string]string{"label": t("admin_countries")}}, rows...))
	}
	countries = append(countries, html.Form(html.Props{Class: "chatlangadmin-form", Data: map[string]string{"chatmapadmin": "country"}, OnSubmit: m.SaveCountry, Aria: map[string]string{"labelledby": "chatmapadmin-countries-title"}},
		html.Label(html.Props{For: "chatmapadmin-country", Text: t("admin_country")}),
		html.Input(html.Props{ID: "chatmapadmin-country", Name: "country", Type: "text", MaxLength: 40, Required: true, Disabled: m.Loading, Dir: "auto", Aria: map[string]string{"describedby": "chatmapadmin-country-hint"}}),
		html.P(html.Props{ID: "chatmapadmin-country-hint", Class: "field-hint", Text: t("admin_country_hint")}),
		html.Label(html.Props{For: "chatmapadmin-country-state", Text: t("admin_country_state")}),
		html.Select(html.Props{ID: "chatmapadmin-country-state", Name: "state", Disabled: m.Loading}, html.Option(html.Props{Value: "off", Selected: true, Text: t("admin_off")}), html.Option(html.Props{Value: "on", Text: t("admin_on")})),
		html.Label(html.Props{For: "chatmapadmin-basis", Text: t("admin_basis")}),
		html.Input(html.Props{ID: "chatmapadmin-basis", Name: "basis", Type: "text", MaxLength: 500, Required: true, Disabled: m.Loading, Dir: "auto", Aria: map[string]string{"describedby": "chatmapadmin-basis-hint"}}),
		html.P(html.Props{ID: "chatmapadmin-basis-hint", Class: "field-hint", Text: t("admin_basis_hint")}),
		html.Div(html.Props{Class: "chatlangadmin-actions"}, html.Button(html.Props{Type: "submit", Class: "button", Text: t("admin_save_country"), Disabled: m.Loading || m.SaveCountry.Value() == nil}), chatmapAdminSaved(m, "country"))))
	sections := []ui.Node{
		html.H2(html.Props{ID: "chatmapadmin-title", Text: t("admin_title")}),
		html.P(html.Props{Class: "field-hint", Text: t("admin_intro")}),
		html.P(html.Props{ID: "chatmapadmin-status", Role: "status", Aria: map[string]string{"live": "polite"}, Text: problem}),
	}
	if m.Failed {
		sections = append(sections, retry())
	}
	sections = append(sections, form, html.Section(html.Props{Class: "chatlangadmin-glossary"}, countries...))
	return html.Section(html.Props{Class: "chatlangadmin chatlangadmin-page chatmapadmin", Dir: dir}, sections...)
}
