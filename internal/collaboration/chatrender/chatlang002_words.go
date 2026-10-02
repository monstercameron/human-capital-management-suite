package chatrender

import "strings"

// The word and character tables for the Latin-script languages offered first.
// They are written by hand and kept small: a word that is a common word of one
// language and not of the others weighs 2, a word the languages share weighs
// 1, and nothing else counts. They live in switch statements, so the package
// holds no mutable table.

type chatlangLatin int

const (
	chatlangLatinEN chatlangLatin = iota
	chatlangLatinDE
	chatlangLatinFR
	chatlangLatinES
	chatlangLatinPT
	chatlangLatinCount
)

func (l chatlangLatin) tag() string {
	switch l {
	case chatlangLatinEN:
		return "en"
	case chatlangLatinDE:
		return "de"
	case chatlangLatinFR:
		return "fr"
	case chatlangLatinES:
		return "es"
	}
	return "pt"
}

func chatlangWordWeight(lang chatlangLatin, w string) int {
	switch lang {
	case chatlangLatinEN:
		return chatlangEnglishWord(w)
	case chatlangLatinDE:
		return chatlangGermanWord(w)
	case chatlangLatinFR:
		return chatlangFrenchWord(w)
	case chatlangLatinES:
		return chatlangSpanishWord(w)
	}
	return chatlangPortugueseWord(w)
}

func chatlangEnglishWord(w string) int {
	switch w {
	case "the", "and", "you", "that", "this", "with", "have", "has", "for", "are", "were", "would", "could", "should", "not", "but", "from",
		"they", "their", "them", "what", "when", "where", "which", "who", "how", "why", "your", "our", "his", "her", "its", "we", "he",
		"she", "it", "been", "does", "did", "of", "to", "is", "be", "do", "if", "or", "at", "by", "yes", "my", "up", "one", "us",
		"thanks", "thank", "please", "hello", "meeting", "today", "tomorrow", "tonight", "message", "team", "work", "good", "morning",
		"afternoon", "evening", "just", "there", "here", "about", "after", "before", "any", "all", "some", "more", "out", "now", "get",
		"got", "make", "take", "need", "want", "know", "think", "see", "let", "send", "sent", "sure", "okay", "great", "really", "very",
		"much", "many", "well", "soon", "again", "can", "i'm", "i'll", "i've", "it's", "that's", "don't", "can't", "won't", "isn't",
		"doesn't", "didn't", "we're", "you're", "they're", "let's", "what's", "there's", "these", "those", "than", "then", "into",
		"over", "only", "other", "because", "while", "still":
		return 2
	case "being", "having", "going", "gonna", "wanna", "right", "back", "come", "came", "look", "looking", "find", "found", "give",
		"gave", "tell", "told", "say", "said", "ask", "asked", "call", "called", "help", "try", "tried", "keep", "kept", "put", "use",
		"used", "working", "check", "checked", "review", "report", "update", "updated", "schedule", "question", "questions", "problem",
		"issue", "issues", "fix", "fixed", "done", "finish", "finished", "ready", "free", "busy", "late", "early", "next", "last", "week",
		"weeks", "day", "days", "time", "times", "year", "years", "people", "thing", "things", "way", "new", "old", "same", "own", "most",
		"each", "every", "both", "few", "less", "might", "must", "shall", "may", "maybe", "already", "always", "never", "ever",
		"usually", "yesterday", "monday", "tuesday", "wednesday", "thursday", "friday", "hours", "minutes", "minute", "hour", "between",
		"during", "without", "within", "through", "above", "around", "though", "although", "however", "since", "until", "unless",
		"whether", "either", "anyone", "everyone", "someone", "nobody", "nothing", "something", "everything", "anything", "myself",
		"yourself", "mine", "yours", "ours", "theirs", "numbers":
		return 2
	case "a", "i", "me", "no", "so", "in", "on", "an", "as", "was", "will", "also", "am", "man", "he's", "she's", "hi", "hey":
		return 1
	}
	return 0
}

func chatlangGermanWord(w string) int {
	switch w {
	case "der", "den", "dem", "des", "und", "ist", "nicht", "ich", "du", "er", "sie", "wir", "ihr", "ein", "eine", "einen", "einem",
		"einer", "mit", "für", "auf", "aus", "bei", "nach", "von", "zu", "zum", "zur", "im", "auch", "aber", "oder", "wenn", "dass",
		"weil", "wie", "wer", "wo", "wann", "warum", "bitte", "danke", "hallo", "guten", "morgen", "tag", "abend", "heute", "gestern",
		"nachricht", "besprechung", "termin", "kann", "können", "muss", "müssen", "soll", "sollen", "wird", "werden", "wurde", "hat",
		"haben", "habe", "bin", "bist", "sind", "waren", "noch", "schon", "nur", "sehr", "mehr", "jetzt", "gerne", "ja", "nein", "doch",
		"kein", "keine", "mein", "meine", "dein", "unser", "unsere", "euch", "uns", "mir", "mich", "dir", "dich", "ihm", "ihn", "ihnen",
		"vielen", "dank", "super", "alles", "etwas", "nichts", "ganz", "dann", "denn", "hier", "dort", "wieder", "immer", "vielleicht",
		"über", "unter", "zwischen", "durch", "gegen", "ohne", "bis", "seit", "während", "diese", "dieser", "dieses", "jede", "jeder",
		"alle", "wäre", "hätte", "könnte", "würde", "gibt", "geht", "gehen", "machen", "mache", "schönen", "gruß", "grüße",
		"freundliche", "liebe", "lieber", "kollegen":
		return 2
	case "habt", "hast", "hatte", "hatten", "seid", "wollen", "willst", "möchte", "möchten", "mag", "darf", "dürfen", "lass", "lassen",
		"lasst", "sag", "sagen", "sagt", "sagte", "komm", "kommen", "kommt", "komme", "gehe", "gehst", "geh", "sehen", "sieht", "sehe",
		"weiß", "wissen", "wusste", "finde", "finden", "gerade", "bereits", "damit", "dafür", "dazu", "davon", "deshalb", "trotzdem",
		"außerdem", "allerdings", "eigentlich", "natürlich", "genau", "richtig", "falsch", "fertig", "bald", "später", "früher",
		"nachmittag", "vormittag", "woche", "wochen", "tage", "zeit", "mal", "einmal", "nochmal", "wirklich", "leider", "hoffentlich",
		"danach", "davor", "jemand", "niemand", "irgendwie", "welche", "welcher", "beide", "viele", "wenig", "manche", "andere",
		"neue", "neuen", "neuer", "alte", "erste", "ersten", "letzte", "nächste", "nächsten", "prüfen", "prüfe", "schicken", "schick",
		"schicke", "senden", "sende", "gesendet", "gesehen", "gemacht", "gesagt", "gewesen", "wegen", "statt", "ob", "sondern", "jedoch",
		"zahlen", "bericht", "frage", "fragen", "probleme", "problem", "arbeit", "arbeiten", "projekt", "datei", "kollege", "kollegin",
		"montag", "dienstag", "mittwoch", "donnerstag", "freitag", "stunde", "stunden", "minute", "minuten":
		return 2
	case "die", "das", "es", "was", "war", "will", "also", "am", "in", "an", "gut", "so", "man", "team":
		return 1
	}
	return 0
}

func chatlangFrenchWord(w string) int {
	switch w {
	case "le", "les", "des", "un", "une", "et", "est", "sont", "pas", "ne", "qui", "quoi", "dont", "où", "pour", "avec", "sans", "dans",
		"sur", "sous", "par", "chez", "nous", "vous", "ils", "elles", "je", "tu", "il", "elle", "ce", "cette", "ces", "mon", "ma", "mes",
		"ton", "ta", "tes", "son", "sa", "ses", "notre", "nos", "votre", "vos", "leur", "leurs", "mais", "donc", "car", "comme",
		"très", "aussi", "oui", "merci", "bonjour", "bonsoir", "salut", "svp", "plaît", "réunion", "équipe",
		"aujourd'hui", "demain", "hier", "matin", "soir", "avoir", "être", "fait", "faire", "peut", "peux", "peuvent", "veux", "voulez",
		"dois", "doit", "avons", "avez", "ont", "suis", "sommes", "êtes", "était", "sera", "serait", "déjà", "encore", "toujours",
		"jamais", "rien", "tout", "tous", "toute", "toutes", "beaucoup", "trop", "peu", "ici", "là", "voici", "voilà", "d'accord",
		"bonne", "journée", "soirée", "quand", "pourquoi", "comment", "parce", "cela", "ça", "c'est", "n'est", "j'ai", "qu'il", "s'il",
		"vers", "depuis", "pendant", "avant", "après", "alors", "puis", "ensuite", "besoin", "disponible", "envoyer",
		"envoyé", "semaine", "mois", "année", "rendez-vous", "cordialement", "au", "aux":
		return 2
	case "avais", "avait", "avaient", "aurai", "aura", "aurait", "fais", "faites", "font", "dit", "dis", "disent", "vois", "voit", "vu",
		"viens", "vient", "viennent", "venir", "aller", "vais", "vas", "vont", "allons", "allez", "pouvoir", "pouvons", "pouvez", "savoir",
		"sais", "sait", "savons", "savez", "veut", "veulent", "faut", "seront", "étais", "étaient", "été", "mettre", "mets", "prendre",
		"prends", "prend", "donne", "trouver", "trouve", "trouvé", "pense", "pensez", "crois", "demande", "demandé", "regarde",
		"regarder", "regardez", "voir", "vérifier", "vérifié", "envoie", "envoyez", "écris", "écrire", "écrit", "lire", "lu", "reçu",
		"attendre", "attends", "arrive", "arriver", "arrivé", "retard", "prochain", "prochaine", "dernier", "dernière", "nouveau",
		"nouvelle", "nouveaux", "même", "autre", "autres", "chaque", "plusieurs", "quelque", "quelques", "personne", "chose", "choses",
		"temps", "fois", "jour", "jours", "heure", "heures", "moment", "semaines", "lundi", "mardi", "mercredi", "jeudi", "vendredi",
		"maintenant", "bientôt", "tard", "tôt", "souvent", "parfois", "vraiment", "presque", "seulement", "environ", "également",
		"cependant", "pourtant", "malgré", "selon", "contre", "derrière", "devant", "moi", "toi", "lui", "eux", "excellent", "parfait",
		"génial", "volontiers", "plaisir", "désolé", "pardon", "bon", "bonnes", "calendrier", "propos", "chiffres", "rapport", "revue",
		"question", "questions", "problème", "problèmes", "travail", "travailler", "projet", "fichier", "collègues", "collègue",
		"déjeune", "déjeuner", "déployer", "déploiement":
		return 2
	case "la", "du", "de", "que", "message", "on", "en", "non", "es", "as", "ai", "si", "entre", "ou", "plus", "bien", "me", "te", "se":
		return 1
	}
	return 0
}

func chatlangFrenchElision(prefix string) bool {
	switch prefix {
	case "l'", "d'", "j'", "qu'", "n'", "c'", "s'", "m'", "t'", "jusqu'", "lorsqu'", "puisqu'":
		return true
	}
	return false
}

func chatlangSpanishWord(w string) int {
	switch w {
	case "el", "los", "las", "una", "unos", "unas", "y", "es", "estoy", "estás", "ha", "hemos", "han", "hay",
		"tengo", "tiene", "tienes", "tenemos", "tienen", "puedo", "puede", "podemos", "pueden", "quiero", "quiere", "voy",
		"hace", "hacer", "hago", "con", "sin", "pero", "cuando", "donde", "quien", "qué", "cómo", "cuándo", "dónde",
		"quién", "esto", "ese", "esa", "eso", "estos", "estas", "mi", "mis", "su", "sus", "nuestro", "nuestra", "yo",
		"tú", "él", "ella", "ellos", "ellas", "nosotros", "usted", "ustedes", "lo", "al", "del", "muy", "más", "ya", "también",
		"todavía", "siempre", "mucho", "muchas", "muchos", "gracias", "hola", "buenos",
		"buenas", "días", "tardes", "noches", "mensaje", "reunión", "equipo", "hoy", "mañana", "ayer", "ahora", "aquí", "allí", "sí",
		"bueno", "buena", "vale", "saludos", "perfecto", "entonces", "además", "así", "otra", "otro", "alguien",
		"pues", "aunque", "mientras", "después", "hasta", "necesito", "necesitamos", "disponible", "están", "eres", "soy", "fue":
		return 2
	case "viene", "vienen", "vengo", "vienes", "ven", "dime", "dame", "mira", "espera", "esperando", "vaya", "creo", "cree", "crees", "sé",
		"sabe", "sabes", "saber", "sabemos", "quieres", "quieren", "queremos", "puedes", "tengas", "tenga", "tengan", "tuve", "tuvo",
		"tenía", "pregunta", "preguntas", "alguna", "alguno", "algunos", "algunas", "vistazo", "momento", "momentos", "tal", "verdad",
		"bastante", "mejor", "peor", "poco", "poca", "pocos", "pocas", "tanto", "tanta", "tan", "cual", "cuales", "cuanto", "dijo",
		"dice", "dices", "decir", "digo", "hecho", "hizo", "hice", "fui", "fuiste", "fueron", "fuimos", "sería", "serán", "estaba",
		"estaban", "estado", "había", "haber", "podría", "podrían", "debo", "debe", "debes", "deben", "debemos", "debería",
		"deberíamos", "llegaré", "llegar", "llego", "llega", "empezar", "empiecen", "empiece", "empezamos", "terminar", "termina",
		"revisar", "revisión", "revisa", "revisarlas", "revisarlo", "revisado", "cifras", "números", "informe", "correo", "archivo", "tema",
		"temas", "problema", "problemas", "trabajo", "trabajar", "proyecto", "proyectos", "fecha", "hora", "horas", "minutos", "mes",
		"meses", "año", "años", "día", "lunes", "martes", "miércoles", "jueves", "viernes", "próxima", "próximo", "pasada", "pasado",
		"nueva", "nuevo", "nuevos", "mismo", "misma", "algún", "ningún", "ninguna", "ninguno", "cualquier", "aún", "apenas", "quizás",
		"quizá", "tampoco", "incluso", "luego", "ahí", "allá", "acá", "dentro", "fuera", "cerca", "lejos", "según", "hacia", "gusto",
		"encantado", "despliegue", "falló", "compañeros", "almuerzo", "seguimos", "avísame", "necesitas", "necesita", "ayuda", "ayudar",
		"idea", "gran", "vez", "veces", "gente", "cosa", "cosas":
		return 2
	case "la", "un", "de", "que", "en", "se", "me", "te", "le", "por", "para", "como", "tu", "no", "está", "ser", "era", "estar", "he",
		"has", "son", "va", "van", "este", "esta", "estamos", "somos", "será", "entre", "sobre", "desde", "porque", "nos", "algo", "cada",
		"nunca", "nada", "todo", "todos", "toda", "todas", "tarde", "claro", "favor", "antes", "durante", "enviar", "enviado", "semana",
		"bien", "vamos", "si":
		return 1
	}
	return 0
}

func chatlangPortugueseWord(w string) int {
	switch w {
	case "os", "um", "uma", "uns", "umas", "é", "são", "estou", "sou", "foi", "tem", "têm", "temos", "tenho", "tinha", "há", "pode",
		"posso", "podem", "quero", "quer", "vou", "vai", "vão", "faz", "fazer", "faço", "em", "nas", "dos", "ao",
		"aos", "pelo", "pela", "pelos", "pelas", "com", "sem", "até", "mas", "quando", "onde", "quem", "qual", "isto", "esse", "essa",
		"isso", "meu", "minha", "meus", "minhas", "seu", "sua", "seus", "suas", "nosso", "nossa", "eu", "ele", "ela", "eles", "elas",
		"nós", "você", "vocês", "lhe", "muito", "muita", "muitos", "já", "também", "ainda", "sempre", "tudo",
		"obrigado", "obrigada", "olá", "oi", "bom", "boa", "dia", "noite", "mensagem", "reunião",
		"equipe", "hoje", "amanhã", "ontem", "agora", "aqui", "ali", "sim", "não", "bem", "então", "além", "assim", "outra", "outro",
		"alguém", "ótimo", "perfeito", "tá", "pra", "pro", "né", "depois", "enquanto", "porém",
		"precisamos", "preciso", "disponível", "foram", "abraço", "vc":
		return 2
	case "tiver", "tiverem", "tenha", "tenham", "tive", "teve", "tinham", "avise", "avisar", "aviso", "vem", "vêm", "vir", "venha",
		"venho", "vejo", "vê", "ver", "viu", "vi", "vimos", "dizer", "diz", "disse", "fala", "falar", "falei", "fale", "sei", "sabe",
		"sabemos", "saber", "queremos", "querem", "puder", "pudermos", "podia", "poderia", "devo", "deve", "devemos", "deveria",
		"deveríamos", "precisa", "precisam", "tentar", "tentei", "tenta", "tentamos", "esperar", "espero", "espera", "chegar", "chego",
		"chega", "chegarei", "começar", "comecem", "começo", "começa", "terminar", "terminei", "termina", "revisar", "revisão", "revisa",
		"conferir", "confira", "conferi", "números", "relatório", "arquivo", "assunto", "problema", "problemas", "trabalho", "trabalhar",
		"trabalhamos", "projeto", "projetos", "data", "hora", "horas", "minutos", "mês", "meses", "ano", "anos", "segunda", "terça",
		"quarta", "quinta", "sexta", "próxima", "próximo", "passada", "passado", "nova", "novo", "novos", "mesmo", "mesma", "algum",
		"alguma", "alguns", "algumas", "nenhum", "nenhuma", "qualquer", "apenas", "talvez", "tampouco", "inclusive", "somente", "só",
		"aí", "lá", "fora", "perto", "longe", "segundo", "através", "ideia", "ideias", "dúvida", "dúvidas", "pergunta", "perguntas",
		"olhada", "vez", "vezes", "tempo", "coisa", "coisas", "gente", "pessoa", "pessoas", "bastante", "melhor", "pior", "pouco",
		"pouca", "poucos", "poucas", "tanto", "tanta", "tão", "ótima", "ótimas", "ótimos", "prazer", "feliz", "desculpe", "desculpa",
		"obrigadas", "obrigados", "valeu", "atrasado", "atrasada", "atraso", "implantação", "novamente", "falhou", "falhar", "funciona",
		"funcionar", "almoçar", "colegas", "ajuda", "ajudar", "dar":
		return 2
	case "o", "as", "e", "de", "que", "no", "nos", "se", "me", "te", "por", "para", "como", "tu", "está", "ser", "era", "entre",
		"sobre", "porque", "este", "esta", "favor", "claro", "desde", "estar", "do", "da", "na", "mais", "estamos", "somos", "será", "algo",
		"cada", "nunca", "nada", "todo", "todos", "toda", "todas", "tarde", "antes", "durante", "enviar", "enviado", "semana", "vamos":
		return 1
	}
	return 0
}

// chatlangCharacterEvidence adds the evidence that is not a whole word: the
// letters one language uses and the others do not, and a few endings. It is
// capped so a long message of loan words cannot decide on endings alone.
func chatlangCharacterEvidence(lang chatlangLatin, lower string) int {
	count := func(chars string) int {
		n := 0
		for _, r := range lower {
			if strings.ContainsRune(chars, r) {
				n++
			}
		}
		return n
	}
	points := 0
	grams := func(list ...string) {
		for _, gram := range list {
			points += min(1, strings.Count(lower, gram))
		}
	}
	switch lang {
	case chatlangLatinDE:
		points += 3*min(1, count("ß")) + min(2, count("äöü"))
		grams("sch", "ung", "keit", "heit", "lich", "cht")
	case chatlangLatinFR:
		points += 3*min(1, count("œ")) + min(2, count("èùûîïëâôê"))
		grams("eau", "ment", "ais", "ait", "eux", "oux")
	case chatlangLatinES:
		points += 3*min(1, count("ñ¿¡")) + min(1, count("áíóú"))
		grams("ción", "ando", "iendo", "dad", "mente")
	case chatlangLatinPT:
		points += 3*min(1, count("ãõ")) + min(1, count("êô"))
		grams("ção", "ões", "inho", "agem", "mente")
	case chatlangLatinEN:
		grams("ing", "ght", "ould", "th", "tion")
	}
	return min(points, 4)
}
