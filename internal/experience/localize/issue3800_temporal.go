package localize

import (
	"strconv"
	"strings"
	"time"
)

// Issue 3800: locale-correct clock, day and relative-time text. The caller
// chooses the zone by passing a time already in it (t.In(zone)); these
// functions never read the machine's zone, so one explicit source decides
// every stamp a shared view shows. Patterns follow CLDR for en, de, fr, es,
// ja and ar; any other tag falls back to English.

type temporalFamily int

const (
	famEN temporalFamily = iota
	famDE
	famFR
	famES
	famJA
	famAR
)

func temporalFamilyOf(locale string) temporalFamily {
	l := strings.ToLower(strings.ReplaceAll(locale, "_", "-"))
	switch {
	case strings.HasPrefix(l, "de"):
		return famDE
	case strings.HasPrefix(l, "fr"):
		return famFR
	case strings.HasPrefix(l, "es"):
		return famES
	case strings.HasPrefix(l, "ja"):
		return famJA
	case strings.HasPrefix(l, "ar"):
		return famAR
	}
	return famEN
}

var (
	enWeekdays = [...]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	enMonthsL  = [...]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	enMonthsS  = [...]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	deWeekdays = [...]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"}
	deMonthsL  = [...]string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"}
	deMonthsS  = [...]string{"Jan.", "Feb.", "März", "Apr.", "Mai", "Juni", "Juli", "Aug.", "Sept.", "Okt.", "Nov.", "Dez."}
	frWeekdays = [...]string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}
	frMonthsL  = [...]string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}
	frMonthsS  = [...]string{"janv.", "févr.", "mars", "avr.", "mai", "juin", "juil.", "août", "sept.", "oct.", "nov.", "déc."}
	esWeekdays = [...]string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"}
	esMonthsL  = [...]string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"}
	esMonthsS  = [...]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sept", "oct", "nov", "dic"}
	jaWeekdays = [...]string{"日曜日", "月曜日", "火曜日", "水曜日", "木曜日", "金曜日", "土曜日"}
	arWeekdays = [...]string{"الأحد", "الاثنين", "الثلاثاء", "الأربعاء", "الخميس", "الجمعة", "السبت"}
	arMonths   = [...]string{"يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو", "يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر", "ديسمبر"}
)

// FormatClock writes the time of day in t's own zone: "3:04 PM" (en),
// "15:04" (de, fr, es, ja) and Arabic-Indic digits for ar.
func FormatClock(locale string, t time.Time) string {
	switch temporalFamilyOf(locale) {
	case famEN:
		return t.Format("3:04 PM")
	case famAR:
		return arabicDigits(t.Format("15:04"))
	}
	return t.Format("15:04")
}

// FormatDay writes a day label. Without withYear it carries the weekday
// ("Tuesday, September 22"); with it the weekday is dropped and the year added.
func FormatDay(locale string, t time.Time, withYear bool) string {
	d, m, y, wd := strconv.Itoa(t.Day()), int(t.Month())-1, strconv.Itoa(t.Year()), int(t.Weekday())
	switch temporalFamilyOf(locale) {
	case famDE:
		if withYear {
			return d + ". " + deMonthsL[m] + " " + y
		}
		return deWeekdays[wd] + ", " + d + ". " + deMonthsL[m]
	case famFR:
		if withYear {
			return d + " " + frMonthsL[m] + " " + y
		}
		return frWeekdays[wd] + " " + d + " " + frMonthsL[m]
	case famES:
		if withYear {
			return d + " de " + esMonthsL[m] + " de " + y
		}
		return esWeekdays[wd] + ", " + d + " de " + esMonthsL[m]
	case famJA:
		if withYear {
			return y + "年" + strconv.Itoa(m+1) + "月" + d + "日"
		}
		return strconv.Itoa(m+1) + "月" + d + "日" + jaWeekdays[wd]
	case famAR:
		if withYear {
			return arabicDigits(d) + " " + arMonths[m] + " " + arabicDigits(y)
		}
		return arWeekdays[wd] + "، " + arabicDigits(d) + " " + arMonths[m]
	}
	if withYear {
		return enMonthsL[m] + " " + d + ", " + y
	}
	return enWeekdays[wd] + ", " + enMonthsL[m] + " " + d
}

// FormatShortDay writes a compact month-day label: "Sep 22", "22. Sept.".
func FormatShortDay(locale string, t time.Time) string {
	d, m := strconv.Itoa(t.Day()), int(t.Month())-1
	switch temporalFamilyOf(locale) {
	case famDE:
		return d + ". " + deMonthsS[m]
	case famFR:
		return d + " " + frMonthsS[m]
	case famES:
		return d + " " + esMonthsS[m]
	case famJA:
		return strconv.Itoa(m+1) + "月" + d + "日"
	case famAR:
		return arabicDigits(d) + " " + arMonths[m]
	}
	return enMonthsS[m] + " " + d
}

// FormatTime writes the clock with the zone abbreviation appended only when
// the stamp's zone differs from the viewer's zone, so a shared view never
// shows a bare time that reads as the viewer's own.
func FormatTime(locale string, t time.Time, viewer *time.Location) string {
	clock := FormatClock(locale, t)
	if viewer == nil || t.Location().String() == viewer.String() {
		return clock
	}
	_, viewerOffset := t.In(viewer).Zone()
	name, offset := t.Zone()
	if offset == viewerOffset {
		return clock
	}
	return clock + " " + name
}

// FormatRelative writes how far t is from now ("2 hours ago", "yesterday",
// "in 5 minutes"). ok is false when the distance is a week or more, where
// the caller should show a date instead.
func FormatRelative(locale string, t, now time.Time) (string, bool) {
	delta := now.Sub(t)
	past := delta >= 0
	if !past {
		delta = -delta
	}
	f := temporalFamilyOf(locale)
	if delta < 45*time.Second {
		return relNow(f), true
	}
	var unit, n int // unit: 0 minute, 1 hour, 2 day
	switch {
	case delta < time.Hour:
		unit, n = 0, int(delta/time.Minute)
		if n < 1 {
			n = 1
		}
	case delta < 24*time.Hour:
		unit, n = 1, int(delta/time.Hour)
	case delta < 7*24*time.Hour:
		// Calendar days in t's zone, so "yesterday" means the previous date.
		ty, tm, td := t.Date()
		ny, nm, nd := now.In(t.Location()).Date()
		n = int(time.Date(ny, nm, nd, 0, 0, 0, 0, time.UTC).Sub(time.Date(ty, tm, td, 0, 0, 0, 0, time.UTC)) / (24 * time.Hour))
		if n < 0 {
			n = -n
		}
		unit = 2
		if n == 0 {
			unit, n = 1, int(delta/time.Hour)
		}
	default:
		return "", false
	}
	return relPhrase(f, unit, n, past), true
}

func relNow(f temporalFamily) string {
	return map[temporalFamily]string{famEN: "now", famDE: "jetzt", famFR: "maintenant", famES: "ahora", famJA: "今", famAR: "الآن"}[f]
}

func relWord(n int, forms [2]string) string {
	if n == 1 {
		return forms[0]
	}
	return forms[1]
}

func relPhrase(f temporalFamily, unit, n int, past bool) string {
	s := strconv.Itoa(n)
	one := n == 1
	switch f {
	case famDE:
		if unit == 2 && one {
			return pick(past, "gestern", "morgen")
		}
		w := relWord(n, [][2]string{{"Minute", "Minuten"}, {"Stunde", "Stunden"}, {"Tag", "Tagen"}}[unit])
		return pick(past, "vor "+s+" "+w, "in "+s+" "+w)
	case famFR:
		if unit == 2 && one {
			return pick(past, "hier", "demain")
		}
		w := relWord(n, [][2]string{{"minute", "minutes"}, {"heure", "heures"}, {"jour", "jours"}}[unit])
		return pick(past, "il y a "+s+" "+w, "dans "+s+" "+w)
	case famES:
		if unit == 2 && one {
			return pick(past, "ayer", "mañana")
		}
		w := relWord(n, [][2]string{{"minuto", "minutos"}, {"hora", "horas"}, {"día", "días"}}[unit])
		return pick(past, "hace "+s+" "+w, "dentro de "+s+" "+w)
	case famJA:
		if unit == 2 && one {
			return pick(past, "昨日", "明日")
		}
		u := []string{"分", "時間", "日"}[unit]
		return pick(past, s+u+"前", s+u+"後")
	case famAR:
		if unit == 2 && one {
			return pick(past, "أمس", "غدًا")
		}
		return pick(past, "قبل "+arCount(unit, n), "خلال "+arCount(unit, n))
	}
	if unit == 2 && one {
		return pick(past, "yesterday", "tomorrow")
	}
	w := relWord(n, [][2]string{{"minute", "minutes"}, {"hour", "hours"}, {"day", "days"}}[unit])
	return pick(past, s+" "+w+" ago", "in "+s+" "+w)
}

func arCount(unit, n int) string {
	forms := [][4]string{
		{"دقيقة", "دقيقتين", "دقائق", "دقيقة"},
		{"ساعة", "ساعتين", "ساعات", "ساعة"},
		{"يوم", "يومين", "أيام", "يومًا"},
	}[unit]
	switch {
	case n == 1:
		return forms[0]
	case n == 2:
		return forms[1]
	case n >= 3 && n <= 10:
		return arabicDigits(strconv.Itoa(n)) + " " + forms[2]
	}
	return arabicDigits(strconv.Itoa(n)) + " " + forms[3]
}

func pick(past bool, a, b string) string {
	if past {
		return a
	}
	return b
}
