package chatrender

import "testing"

// chatlangHeldOut was written after the tables and is not used to fit them.
func chatlangHeldOut() map[string][]string {
	return map[string][]string{
		"en": {
			"I just pushed the fix, can someone approve it?",
			"We're running about ten minutes behind schedule.",
			"What time does the customer call start on Thursday?",
			"Could you share the slides after the presentation?",
			"Sorry for the delay, I was stuck in another meeting.",
			"The invoice was sent last week but nobody has replied yet.",
			"Let's sync on this tomorrow morning if that works.",
			"Please add Dana to the channel so she can see the thread.",
			"I'll check with legal and get back to you by noon.",
			"This looks fine to me, go ahead and merge it.",
		},
		"de": {
			"Ich habe den Fehler behoben, kann das bitte jemand prüfen?",
			"Wir sind ungefähr zehn Minuten hinter dem Zeitplan.",
			"Wann beginnt am Donnerstag das Gespräch mit dem Kunden?",
			"Könntest du die Folien nach dem Vortrag teilen?",
			"Entschuldigung für die Verzögerung, ich war in einem anderen Termin.",
			"Die Rechnung wurde letzte Woche verschickt, aber niemand hat geantwortet.",
			"Lass uns das morgen früh besprechen, wenn es dir passt.",
			"Bitte füge Dana zum Kanal hinzu, damit sie den Verlauf sehen kann.",
			"Ich frage bei der Rechtsabteilung nach und melde mich bis Mittag.",
			"Das sieht für mich gut aus, du kannst es zusammenführen.",
		},
		"fr": {
			"J'ai poussé la correction, quelqu'un peut la valider ?",
			"Nous avons environ dix minutes de retard sur le planning.",
			"À quelle heure commence l'appel client jeudi ?",
			"Pourrais-tu partager les diapositives après la présentation ?",
			"Désolé pour le retard, j'étais bloqué dans une autre réunion.",
			"La facture a été envoyée la semaine dernière mais personne n'a répondu.",
			"On en reparle demain matin si ça te va.",
			"Ajoute Dana au canal pour qu'elle puisse voir la discussion.",
			"Je vérifie avec le service juridique et je reviens vers vous avant midi.",
			"Ça me semble bon, tu peux fusionner.",
		},
		"es": {
			"Ya subí la corrección, ¿alguien puede aprobarla?",
			"Llevamos unos diez minutos de retraso respecto al calendario.",
			"¿A qué hora empieza la llamada con el cliente el jueves?",
			"¿Podrías compartir las diapositivas después de la presentación?",
			"Perdón por la demora, estaba atrapado en otra reunión.",
			"La factura se envió la semana pasada, pero nadie ha respondido todavía.",
			"Hablemos de esto mañana por la mañana si te parece.",
			"Añade a Dana al canal para que pueda ver el hilo.",
			"Consulto con el departamento legal y te respondo antes del mediodía.",
			"Me parece bien, puedes fusionarlo.",
		},
		"pt": {
			"Já enviei a correção, alguém pode aprovar?",
			"Estamos com cerca de dez minutos de atraso no cronograma.",
			"A que horas começa a ligação com o cliente na quinta-feira?",
			"Você poderia compartilhar os slides depois da apresentação?",
			"Desculpe pela demora, eu estava preso em outra reunião.",
			"A fatura foi enviada semana passada, mas ninguém respondeu ainda.",
			"Vamos conversar sobre isso amanhã de manhã, se der para você.",
			"Adicione a Dana ao canal para que ela possa ver a conversa.",
			"Vou falar com o jurídico e retorno antes do meio-dia.",
			"Para mim está bom, pode fazer o merge.",
		},
		"ar": {
			"لقد أرسلت الإصلاح، هل يستطيع أحد مراجعته؟",
			"نحن متأخرون حوالي عشر دقائق عن الجدول.",
			"متى تبدأ مكالمة العميل يوم الخميس؟",
			"هل يمكنك مشاركة الشرائح بعد العرض التقديمي؟",
		},
		"ja": {
			"修正をプッシュしました。誰か確認してもらえますか？",
			"スケジュールより十分ほど遅れています。",
			"木曜日の顧客との電話は何時に始まりますか？",
		},
		"hi": {
			"मैंने सुधार भेज दिया है, क्या कोई इसे जाँच सकता है?",
			"हम समय-सारणी से लगभग दस मिनट पीछे हैं।",
			"गुरुवार को ग्राहक के साथ कॉल कितने बजे शुरू होती है?",
		},
	}
}

func TestTodo_CHATLANG_002_HeldOut(t *testing.T) {
	total, right := 0, 0
	for want, samples := range chatlangHeldOut() {
		for _, text := range samples {
			total++
			if got := Detect(text); got.Language == want {
				right++
			} else {
				t.Logf("held-out miss: %q got %q (%.2f), want %q", text, got.Language, got.Confidence, want)
			}
		}
	}
	t.Logf("held-out accuracy %d of %d", right, total)
	if float64(right) < 0.9*float64(total) {
		t.Fatalf("held-out accuracy %d of %d is below 90 percent", right, total)
	}
}
