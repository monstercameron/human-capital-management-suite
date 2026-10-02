package chatui

import (
	"strconv"
	"strings"
	"time"
)

// The Chat preferences list time zones by what a person recognises, a city and
// its offset ("Denver (UTC−6)"), while the stored value stays the IANA ID
// ("America/Denver"). The offset comes from the zone data compiled into the
// page, so nothing is downloaded; a zone the data does not know is listed by
// its city alone.

const (
	keyS24ZoneDevice = "chat.s24.timezone_device"
	keyS24ZoneUTC    = "chat.s24.timezone_utc"
)

var s24ZoneCopy = map[string]map[string]string{
	"en-US": {keyS24ZoneDevice: "{zone} (this device)", keyS24ZoneUTC: "UTC"},
	"de-DE": {keyS24ZoneDevice: "{zone} (dieses Gerät)", keyS24ZoneUTC: "UTC"},
	"ar":    {keyS24ZoneDevice: "{zone} (هذا الجهاز)", keyS24ZoneUTC: "التوقيت العالمي"},
}

// s24ZoneCities names the cities the zone list and the common devices use, in
// German and Arabic. A city missing here is shown under its English name, which
// is what the IANA ID spells.
var s24ZoneCities = map[string][2]string{
	"America/Los_Angeles": {"Los Angeles", "لوس أنجلوس"},
	"America/Denver":      {"Denver", "دنفر"},
	"America/Chicago":     {"Chicago", "شيكاغو"},
	"America/New_York":    {"New York", "نيويورك"},
	"America/Sao_Paulo":   {"São Paulo", "ساو باولو"},
	"America/Toronto":     {"Toronto", "تورونتو"},
	"America/Vancouver":   {"Vancouver", "فانكوفر"},
	"America/Mexico_City": {"Mexiko-Stadt", "مكسيكو سيتي"},
	"Europe/London":       {"London", "لندن"},
	"Europe/Berlin":       {"Berlin", "برلين"},
	"Europe/Paris":        {"Paris", "باريس"},
	"Europe/Madrid":       {"Madrid", "مدريد"},
	"Europe/Rome":         {"Rom", "روما"},
	"Europe/Amsterdam":    {"Amsterdam", "أمستردام"},
	"Europe/Vienna":       {"Wien", "فيينا"},
	"Europe/Zurich":       {"Zürich", "زيورخ"},
	"Europe/Moscow":       {"Moskau", "موسكو"},
	"Europe/Istanbul":     {"Istanbul", "إسطنبول"},
	"Africa/Johannesburg": {"Johannesburg", "جوهانسبرغ"},
	"Africa/Cairo":        {"Kairo", "القاهرة"},
	"Asia/Dubai":          {"Dubai", "دبي"},
	"Asia/Riyadh":         {"Riad", "الرياض"},
	"Asia/Kolkata":        {"Kalkutta", "كولكاتا"},
	"Asia/Singapore":      {"Singapur", "سنغافورة"},
	"Asia/Hong_Kong":      {"Hongkong", "هونغ كونغ"},
	"Asia/Shanghai":       {"Shanghai", "شنغهاي"},
	"Asia/Seoul":          {"Seoul", "سيول"},
	"Asia/Tokyo":          {"Tokio", "طوكيو"},
	"Australia/Sydney":    {"Sydney", "سيدني"},
}

// s24ZoneText resolves a string of the zone copy for a locale.
func s24ZoneText(locale, key string) string {
	return chatbug039Text(key, s24ZoneCopy[chatEmojiLocale(locale)][key], s24ZoneCopy["en-US"][key])
}

// s24ZoneCity is the city a zone ID names, in the reader's language.
func s24ZoneCity(locale, zone string) string {
	if zone == "UTC" || zone == "Etc/UTC" {
		return s24ZoneText(locale, keyS24ZoneUTC)
	}
	english := zone
	if i := strings.LastIndex(zone, "/"); i >= 0 {
		english = zone[i+1:]
	}
	english = strings.ReplaceAll(english, "_", " ")
	if names, ok := s24ZoneCities[zone]; ok {
		switch chatEmojiLocale(locale) {
		case "de-DE":
			return names[0]
		case "ar":
			return names[1]
		}
	}
	return english
}

// s24ZoneOffset writes an offset the way a clock reads it: "UTC−6",
// "UTC+5:30", "UTC+0". The minus is a true minus sign. Arabic uses its own
// digits and isolates the token so the digits do not reorder against the text
// around them.
func s24ZoneOffset(locale string, seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign, seconds = "−", -seconds
	}
	text := "UTC" + sign + strconv.Itoa(seconds/3600)
	if minutes := seconds % 3600 / 60; minutes != 0 {
		text += ":" + s24TwoDigits(minutes)
	}
	if chatEmojiLocale(locale) == "ar" {
		return "⁦" + arabicDigits(text) + "⁩"
	}
	return text
}

func s24TwoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// s24ZoneLabel is a zone's readable name for the preferences list and summary:
// "Denver (UTC−6)". now picks the offset in force (daylight saving moves it).
func s24ZoneLabel(locale, zone string, now time.Time) string {
	city := s24ZoneCity(locale, zone)
	if zone == "UTC" || zone == "Etc/UTC" {
		return city
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return city
	}
	_, offset := now.In(loc).Zone()
	return city + " (" + s24ZoneOffset(locale, offset) + ")"
}
