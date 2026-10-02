package chatrender

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// chatlangLabelled is the labelled sample: messages as people write them at
// work, short and long, informal and formal, with names and numbers in them.
// The tables in chatlang002_words.go were not fitted to a held-out half of
// these; every sample must be right.
func chatlangLabelled() map[string][]string {
	return map[string][]string{
		"en": {
			"Can you send me the report before the meeting?",
			"Thanks, that works for me.",
			"I'll be a few minutes late, please start without me.",
			"Did you see the message from Maria about the schedule?",
			"We need to move the review to tomorrow afternoon.",
			"Good morning team, here is the plan for today.",
			"That's a great idea, let's try it next week.",
			"Please let me know if you have any questions.",
			"The deploy failed again and I don't know why.",
			"Happy to help, just tell me what you need.",
			"Thank you very much for your help yesterday!",
			"Are we still on for lunch with the new hires?",
			"I think the numbers look good, but we should check them again.",
			"Hey, could you take a look at this when you get a chance?",
		},
		"de": {
			"Kannst du mir den Bericht vor dem Meeting schicken?",
			"Danke, das passt für mich.",
			"Ich komme ein paar Minuten später, bitte fangt ohne mich an.",
			"Hast du die Nachricht von Maria wegen des Termins gesehen?",
			"Wir müssen die Besprechung auf morgen Nachmittag verschieben.",
			"Guten Morgen Team, hier ist der Plan für heute.",
			"Das ist eine tolle Idee, lass uns das nächste Woche ausprobieren.",
			"Bitte sagt mir Bescheid, wenn ihr Fragen habt.",
			"Der Build ist schon wieder fehlgeschlagen und ich weiß nicht warum.",
			"Gerne, sag mir einfach, was du brauchst.",
			"Vielen Dank für deine Hilfe gestern!",
			"Gehen wir noch zusammen mit den neuen Kollegen essen?",
			"Ich finde die Zahlen gut, aber wir sollten sie noch einmal prüfen.",
			"Hallo, kannst du dir das bitte kurz ansehen?",
		},
		"fr": {
			"Peux-tu m'envoyer le rapport avant la réunion ?",
			"Merci, ça me convient très bien.",
			"Je serai en retard de quelques minutes, commencez sans moi.",
			"As-tu vu le message de Maria à propos du calendrier ?",
			"Nous devons déplacer la revue à demain après-midi.",
			"Bonjour à tous, voici le programme pour aujourd'hui.",
			"C'est une excellente idée, essayons la semaine prochaine.",
			"N'hésitez pas à me dire si vous avez des questions.",
			"Le déploiement a encore échoué et je ne sais pas pourquoi.",
			"Avec plaisir, dis-moi simplement ce dont tu as besoin.",
			"Merci beaucoup pour ton aide hier !",
			"On déjeune toujours avec les nouveaux collègues ?",
			"Je trouve que les chiffres sont bons, mais il faut les vérifier encore.",
			"Salut, tu peux regarder ça quand tu as un moment ?",
		},
		"es": {
			"¿Puedes enviarme el informe antes de la reunión?",
			"Gracias, me viene bien.",
			"Llegaré unos minutos tarde, empiecen sin mí.",
			"¿Viste el mensaje de María sobre el calendario?",
			"Tenemos que mover la revisión a mañana por la tarde.",
			"Buenos días equipo, este es el plan para hoy.",
			"Es una gran idea, vamos a probarlo la semana que viene.",
			"Avísame si tienes alguna pregunta.",
			"El despliegue falló otra vez y no sé por qué.",
			"Con gusto, solo dime qué necesitas.",
			"Muchas gracias por tu ayuda de ayer.",
			"¿Seguimos con el almuerzo con los nuevos compañeros?",
			"Creo que las cifras se ven bien, pero deberíamos revisarlas otra vez.",
			"Hola, ¿puedes echarle un vistazo cuando tengas un momento?",
		},
		"pt": {
			"Você pode me enviar o relatório antes da reunião?",
			"Obrigado, isso funciona para mim.",
			"Vou chegar alguns minutos atrasado, comecem sem mim.",
			"Você viu a mensagem da Maria sobre o cronograma?",
			"Precisamos mudar a revisão para amanhã à tarde.",
			"Bom dia equipe, aqui está o plano de hoje.",
			"É uma ótima ideia, vamos tentar na semana que vem.",
			"Me avise se você tiver alguma dúvida.",
			"A implantação falhou de novo e eu não sei por quê.",
			"Com prazer, é só me dizer do que você precisa.",
			"Muito obrigada pela sua ajuda ontem!",
			"Ainda vamos almoçar com os novos colegas?",
			"Acho que os números estão bons, mas deveríamos conferir de novo.",
			"Oi, você pode dar uma olhada nisso quando tiver um tempo?",
		},
		"ar": {
			"هل يمكنك إرسال التقرير قبل الاجتماع؟",
			"شكرًا لك، هذا مناسب لي.",
			"سأتأخر بضع دقائق، ابدأوا بدوني من فضلكم.",
			"هل رأيت رسالة مريم عن الجدول الزمني؟",
			"يجب أن ننقل المراجعة إلى بعد ظهر الغد.",
			"صباح الخير يا فريق، هذه خطة اليوم.",
			"فكرة رائعة، لنجربها الأسبوع القادم.",
			"أخبروني إذا كانت لديكم أي أسئلة.",
		},
		"ja": {
			"会議の前にレポートを送ってもらえますか？",
			"ありがとうございます、それで大丈夫です。",
			"少し遅れますので、先に始めてください。",
			"マリアさんからのメッセージを見ましたか？",
			"明日の午後にレビューを移動する必要があります。",
			"皆さん、おはようございます。今日の予定です。",
		},
		"hi": {
			"क्या आप बैठक से पहले रिपोर्ट भेज सकते हैं?",
			"धन्यवाद, यह मेरे लिए ठीक है।",
			"मुझे कुछ मिनट देर होगी, कृपया मेरे बिना शुरू करें।",
			"क्या आपने मारिया का संदेश देखा?",
			"हमें समीक्षा कल दोपहर तक ले जानी होगी।",
			"सभी को सुप्रभात, यह आज की योजना है।",
		},
	}
}

func TestTodo_CHATLANG_002_Accuracy(t *testing.T) {
	total, right := 0, 0
	for want, samples := range chatlangLabelled() {
		for _, text := range samples {
			total++
			got := Detect(text)
			if got.Language != want {
				t.Errorf("%q: got %q (%.2f), want %q", text, got.Language, got.Confidence, want)
				continue
			}
			if got.Confidence <= 0 || got.Confidence > 1 {
				t.Errorf("%q: confidence %v outside (0, 1]", text, got.Confidence)
			}
			right++
		}
	}
	if total < 70 {
		t.Fatalf("the labelled sample shrank to %d messages", total)
	}
	if right != total {
		t.Fatalf("detection accuracy %d of %d", right, total)
	}
}

// TestTodo_CHATLANG_002_NoLanguage: very short or ambiguous messages are "no
// language", so they are never translated.
func TestTodo_CHATLANG_002_NoLanguage(t *testing.T) {
	for _, text := range []string{
		"", " ", "ok", "OK", "k", "lol", "👍", "👍🔥🎉", "ok 👍", "Alexander", "Maria García", "Jean-Pierre Dupont", "BMW X5", "SKU-12345",
		"Hi John", "thanks", "danke", "gracias", "12:30", "3,14", "2026-10-01", "+1 (555) 010-9999", "a b c d", "x := 12",
		"@alice", "@alice @bob", "#general", "https://example.com/a/b?c=d", "www.example.com", "alice@example.com", "doc:policy-2026",
		"```go\nfunc main() {\n\tfmt.Println(\"hello world\")\n}\n```", "`git status`", "func main() { return }", "if (a == b) { c = d; }",
		"SELECT * FROM users WHERE id = 1;", "foo_bar_baz.go", "src/main/java/App.java", "C:\\Users\\alice\\file.txt",
		// Not offered: Chinese (Han without kana), Persian, Russian, Korean.
		"你好，请问会议几点开始", "سلام، حال شما چطور است؟ من خوبم", "Привет, как дела у команды сегодня", "안녕하세요 여러분 오늘 회의가 있습니다",
		// A name or a word of an unlisted language, however long.
		"Wolfeschlegelsteinhausenbergerdorff", "Siobhan O'Sullivan-Whitfield",
	} {
		if got := Detect(text); got.Language != "und" {
			t.Errorf("%q: got %+v, want no language", text, got)
		}
	}
}

func TestTodo_CHATLANG_002_NoiseDoesNotChangeTheLanguage(t *testing.T) {
	noise := []string{" 👍", " @alice", " #general", " https://example.com/x", " `code`", " doc:policy-2026", " 12:30", " 🎉🎉"}
	for want, samples := range chatlangLabelled() {
		for _, text := range samples {
			for _, n := range noise {
				if got := Detect(text + n); got.Language != want {
					t.Errorf("%q + %q: got %q, want %q", text, n, got.Language, want)
				}
				if got := Detect(n[1:] + " " + text); got.Language != want {
					t.Errorf("%q before %q: got %q, want %q", n[1:], text, got.Language, want)
				}
			}
		}
	}
}

func TestTodo_CHATLANG_002_MixedMessages(t *testing.T) {
	samples := chatlangLabelled()
	for _, c := range [][2]string{{"en", "de"}, {"de", "fr"}, {"es", "pt"}, {"fr", "en"}, {"en", "ar"}, {"ja", "en"}} {
		text := samples[c[0]][0] + " " + samples[c[1]][1]
		got := Detect(text)
		if got.Language != "mul" || len(got.Spans) != 2 || got.Spans[0].Language != c[0] || got.Spans[1].Language != c[1] {
			t.Errorf("%s + %s: %+v", c[0], c[1], got)
			continue
		}
		for _, s := range got.Spans {
			if s.Start < 0 || s.End > len(text) || s.Start >= s.End || !utf8.ValidString(text[s.Start:s.End]) {
				t.Errorf("span %+v does not slice %q", s, text)
			}
		}
		if got.Spans[0].End > got.Spans[1].Start {
			t.Errorf("spans overlap: %+v", got.Spans)
		}
	}
	// One language with a name or a loan word in it is not a mixed message.
	if got := Detect("Bitte schick das an Maria, danke für die Hilfe mit dem Meeting"); got.Language != "de" || len(got.Spans) != 0 {
		t.Errorf("a German message with a loan word: %+v", got)
	}
}

// TestTodo_CHATLANG_002_Detector_Property: detection never panics, is a pure
// function of the text, and its spans are ordered, valid slices of the text.
func TestTodo_CHATLANG_002_Detector_Property(t *testing.T) {
	r := rand.New(rand.NewSource(20261001))
	var all []string
	for _, samples := range chatlangLabelled() {
		all = append(all, samples...)
	}
	alphabet := []rune("abcdefghijklmnopqrstuvwxyz ÄÖÜäöüßéèêàçñãõ¿¡.,!?;\n@#/:`'-0123456789👍مرحباこんにちはनमस्ते\u0301\u200d")
	for i := 0; i < 4000; i++ {
		var text string
		switch i % 3 {
		case 0:
			var b strings.Builder
			for n := r.Intn(300); n > 0; n-- {
				b.WriteRune(alphabet[r.Intn(len(alphabet))])
			}
			text = b.String()
		case 1:
			a, b := all[r.Intn(len(all))], all[r.Intn(len(all))]
			text = a + " " + b
			if cut := r.Intn(len(text) + 1); r.Intn(2) == 0 {
				text = text[:cut] // may split a rune: invalid UTF-8 must be safe
			}
		default:
			text = strings.Repeat(all[r.Intn(len(all))]+" ", 1+r.Intn(200))
		}
		a, b := Detect(text), Detect(text)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("detection is not deterministic for %q", text)
		}
		if a.Confidence < 0 || a.Confidence > 1 {
			t.Fatalf("confidence %v for %q", a.Confidence, text)
		}
		if a.Language == "und" && a.Confidence != 0 {
			t.Fatalf("no language with confidence %v", a.Confidence)
		}
		if (a.Language == "mul") != (len(a.Spans) > 0) {
			t.Fatalf("spans and mixed disagree: %+v", a)
		}
		prev := 0
		for _, s := range a.Spans {
			if s.Start < prev || s.Start >= s.End || s.End > len(text) || s.Language == "und" || s.Language == "mul" {
				t.Fatalf("span %+v in %d bytes", s, len(text))
			}
			prev = s.End
		}
	}
	// An edit re-detects: the same slot with another language's text changes.
	a, b := Detect(chatlangLabelled()["en"][0]), Detect(chatlangLabelled()["de"][0])
	if a.Language == b.Language {
		t.Fatal("an edit to another language kept the old detection")
	}
}

func TestTodo_CHATLANG_002_Detector_Performance(t *testing.T) {
	var all []string
	for _, samples := range chatlangLabelled() {
		all = append(all, samples...)
	}
	long := strings.Repeat("Bitte lesen wir die Nachricht heute mit dem Team. Please read this. ", 400)
	start := time.Now()
	const rounds = 2000
	for i := 0; i < rounds; i++ {
		Detect(all[i%len(all)])
	}
	if each := time.Since(start) / rounds; each >= 2*time.Millisecond {
		t.Fatalf("a chat message takes %v to detect, the budget is 2 ms", each)
	}
	start = time.Now()
	for i := 0; i < 100; i++ {
		Detect(long)
	}
	if each := time.Since(start) / 100; each >= 2*time.Millisecond {
		t.Fatalf("a long message takes %v to detect, the budget is 2 ms", each)
	}
}
