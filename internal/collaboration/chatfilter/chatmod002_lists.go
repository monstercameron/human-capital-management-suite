package chatfilter

// BuiltinListVersion is the version of the product-owned word lists. A shipped
// version is never edited: a change to any term is a new version.
//
//	1.0.0  three trivial terms per language (superseded)
//	1.1.0  data-driven lists, 25 to 60 profanity terms, 10 to 25 slur terms and
//	       10 to 25 harassment phrases per language
const BuiltinListVersion = "1.1.0"

type builtinList struct {
	Language, Category string
	Terms              []string
}

// builtinLists is product-owned seed data, curated for this product on
// 2026-10-01. No third-party list or licence is imported.
//
// The slurs category holds derogatory insults and dehumanising or ableist
// terms. It deliberately does NOT spell out identity-targeted racial, ethnic,
// religious, sexual-orientation or national-origin slurs in source: the product
// owner supplies a vetted list for those through a custom word-list filter,
// which is an ordinary versioned definition of the same kind.
//
// Terms are written in ordinary spelling; each is folded exactly as message
// text is (case, accents, compatibility forms, look-alike letters, letter
// substitutions), so one spelling covers its variants. Every entry was reviewed
// so that it is not also a common innocent word of its language; the words that
// merely CONTAIN a term are covered by whole-word matching and by
// knownFalsePositives.
func builtinLists() []builtinList {
	return []builtinList{
		{"en", "profanity", []string{
			"damn", "damnit", "dammit", "goddamn", "goddamnit", "goddammit", "crap", "crappy",
			"shit", "shits", "shitty", "shithead", "shitheads", "shitface", "bullshit", "horseshit", "dipshit",
			"fuck", "fucks", "fucked", "fucker", "fuckers", "fucking", "fuckin", "motherfucker", "motherfuckers", "motherfucking", "clusterfuck",
			"piss", "bollocks", "bugger", "arse", "arsehole", "arseholes", "ass", "asses", "asshole", "assholes", "asshat", "jackass", "dumbass",
			"cunt", "cunts", "twat", "twats", "wank", "wanker", "wankers", "dickhead", "dickheads", "douchebag", "douchebags",
			"bitch", "bitches", "bastard", "bastards", "cocksucker",
		}},
		{"en", "slurs", []string{
			"idiot", "idiots", "moron", "morons", "imbecile", "imbeciles", "retard", "retards", "retarded", "spastic", "spaz", "cretin", "cretins",
			"scumbag", "scumbags", "subhuman", "subhumans", "whore", "whores", "slut", "sluts", "skank",
		}},
		{"en", "harassment", []string{
			"shut up", "shut your mouth", "shut your face", "shut the fuck up", "piss off", "buzz off", "go to hell", "fuck off", "fuck you", "screw you",
			"go fuck yourself", "go screw yourself", "kill yourself", "go kill yourself", "kys", "go die", "i hope you die", "i'll kill you", "i will kill you",
			"nobody likes you", "nobody wants you", "everyone hates you",
		}},
		{"de", "profanity", []string{
			"scheiße", "scheiß", "scheißdreck", "scheißegal", "scheißhaufen", "scheißkopf", "beschissen", "beschissene", "kacke", "kacken", "kackbratze",
			"verdammt", "verdammte", "verdammter", "verdammtes", "verdammten", "verflucht", "verfluchte", "verfluchter", "verfluchtes",
			"verfickt", "verfickte", "verficktes", "ficken", "fick", "fickt", "ficker",
			"arsch", "arschloch", "arschlöcher", "arschgeige", "arschkriecher", "wichser", "wichsen", "hurensohn", "hurensöhne", "fotze", "fotzen", "votze", "pisser", "schwanzlutscher",
		}},
		{"de", "slurs", []string{
			"idiot", "idioten", "vollidiot", "vollidioten", "trottel", "volltrottel", "dummkopf", "blödmann", "schwachkopf", "spast", "spasti", "mongo", "missgeburt",
			"hure", "huren", "schlampe", "nutte", "mistkerl", "scheißkerl", "dreckskerl", "drecksau", "drecksack", "abschaum", "untermensch", "untermenschen",
		}},
		{"de", "harassment", []string{
			"halt die klappe", "halt die fresse", "halt die schnauze", "halt das maul", "halt's maul", "verpiss dich", "verpisst euch", "geh sterben", "geh zum teufel", "fahr zur hölle",
			"fick dich", "leck mich am arsch", "ich bring dich um", "ich töte dich", "ich mach dich fertig", "erschieß dich", "stirb doch", "niemand mag dich", "keiner mag dich", "du bist wertlos",
		}},
		{"ar", "profanity", []string{
			"اللعنة", "اللعين", "لعين", "لعينة", "ملعون", "ملعونة", "تبا", "خرا", "خراء", "طيز", "منيوك", "منيوكة", "كسمك", "كس امك", "كس اختك", "عرص", "متناك", "متناكة",
			"ينعل", "ينعل ابوك", "يلعن ابوك", "يلعن امك", "تبا لك", "اللعنة عليك", "لعنك الله",
		}},
		{"ar", "slurs", []string{
			"أحمق", "حمقاء", "غبي", "غبية", "أبله", "معتوه", "متخلف", "عبيط", "أهبل", "حقير", "حقيرة", "وغد", "سافل", "منحط", "حثالة", "وضيع",
			"عاهرة", "عاهر", "شرموطة", "شرموط", "قحبة", "ابن الكلب", "ابن الحرام", "ابن العاهرة",
		}},
		{"ar", "harassment", []string{
			"اخرس", "اخرسي", "اغرب عن وجهي", "ابتعد عن وجهي", "اذهب إلى الجحيم", "اذهب للجحيم", "روح في داهية", "انقلع", "انقلعي", "سأقتلك", "سوف اقتلك", "سأذبحك",
			"اذهب وانتحر", "اقتل نفسك", "اقتلي نفسك", "لا احد يحبك", "لا احد يريدك", "العالم افضل بدونك", "اتمنى ان تموت", "اتمنى موتك",
		}},
	}
}

// knownFalsePositives lists innocent words of a language that contain, or look
// like, a listed term. Whole-word matching already keeps them clean; the list
// is the product's tested record of the traps (the Scunthorpe problem) and a
// guard that stops a product list entry from ever being one of these words.
func knownFalsePositives(lang string) []string {
	switch lang {
	case "en":
		return []string{
			"Scunthorpe", "Penistone", "Lightwater", "Essex", "Sussex", "Middlesex", "Hancock", "Hitchcock", "Peacock", "Cockburn", "Cockney", "cocktail", "cockpit",
			"assess", "assessment", "assistant", "assassin", "assume", "assist", "association", "class", "classic", "classy", "bass", "pass", "passage", "passenger",
			"grass", "mass", "glass", "compass", "embassy", "ambassador", "harass", "harassment", "bassoon", "massage", "message", "analysis", "analyst", "canal",
			"document", "button", "buttress", "therapist", "shitake", "shiitake", "retardant", "arsenal", "arsenic", "Wankel", "Dickens", "Dickinson", "documentary",
			"scrap", "scrape", "Twatt",
		}
	case "de":
		return []string{
			"Klassik", "klassisch", "Klasse", "Schifffahrt", "Kasse", "Hass", "Masse", "Spaß", "Spass", "Assistent", "Assistentin", "Glasscheibe", "Scheibe", "Scheide",
			"Pissoir", "Idiotie", "Mongolei", "Mongolen", "spastisch", "Spastik", "Passagier", "Passage", "Wichtel", "Kaffeekasse", "Verdammnis", "Schlamm", "Schlampig", "Abschied",
		}
	case "ar":
		return []string{
			"أحماض", "مخلص", "خرافة", "خرائط", "خراب", "خراج", "تباهى", "تبادل", "تباين", "كتبا", "تحقير", "أسافل", "الأخرس", "عرصة", "عرصات", "حقيقة",
			"طيزنا", "وغدان", "لعينان", "سافلة", "منحطات", "الحثالة", "متخلفون", "ملعب", "كسوة", "كسب", "كسر",
		}
	}
	return nil
}
