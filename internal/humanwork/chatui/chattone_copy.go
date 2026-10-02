package chatui

const chattoneCopyTable = "\nstyle\u0000Style\u0000Stil\u0000الأسلوب\u0000" +
	"\nprofessional\u0000Professional\u0000Professionell\u0000مهني\u0000" +
	"\nfriendly\u0000Friendly\u0000Freundlich\u0000ودود\u0000" +
	"\nconcise\u0000Concise\u0000Prägnant\u0000موجز\u0000" +
	"\nprofessional_help\u0000Rewrite with neutral, courteous wording.\u0000Neutral und höflich umformulieren.\u0000أعد الصياغة بلغة محايدة ومهذبة.\u0000" +
	"\nfriendly_help\u0000Rewrite with warm, positive wording.\u0000Warm und positiv umformulieren.\u0000أعد الصياغة بلغة ودودة وإيجابية.\u0000" +
	"\nconcise_help\u0000Rewrite shorter and directly, keeping the same content.\u0000Kürzer und direkt umformulieren, mit gleichem Inhalt.\u0000أعد الصياغة باختصار ووضوح مع الحفاظ على المحتوى.\u0000" +
	"\nsuggested\u0000Suggested\u0000Empfohlen\u0000مقترح\u0000" +
	"\nnotice\u0000Your draft is sent to the model only when you choose a style. Review and edit the preview before sending.\u0000Ihr Entwurf wird erst an das Modell gesendet, wenn Sie einen Stil wählen. Prüfen und bearbeiten Sie die Vorschau vor dem Senden.\u0000تُرسل مسودتك إلى النموذج فقط عند اختيار أسلوب. راجع المعاينة وعدّلها قبل الإرسال.\u0000" +
	"\nworking\u0000Rewriting… Keep typing; your changes will be kept.\u0000Wird umformuliert… Sie können weiterschreiben; Änderungen bleiben erhalten.\u0000جارٍ إعادة الصياغة… يمكنك متابعة الكتابة؛ ستبقى تغييراتك محفوظة.\u0000" +
	"\nunavailable\u0000Writing styles are unavailable. Keep writing or try again later.\u0000Schreibstile sind nicht verfügbar. Schreiben Sie weiter oder versuchen Sie es später erneut.\u0000أساليب الكتابة غير متاحة. تابع الكتابة أو حاول لاحقًا.\u0000" +
	"\npreservation\u0000The rewrite could not preserve your meaning. Keep your draft or try another style.\u0000Die Bedeutung konnte nicht erhalten werden. Behalten Sie Ihren Entwurf oder versuchen Sie einen anderen Stil.\u0000تعذّر الحفاظ على المعنى. احتفظ بمسودتك أو جرّب أسلوبًا آخر.\u0000" +
	"\npolicy\u0000The rewrite did not pass the workspace's content checks. Keep writing or revise your draft.\u0000Die Umformulierung besteht die Inhaltsprüfung nicht. Schreiben Sie weiter oder ändern Sie Ihren Entwurf.\u0000لم تجتز الصياغة فحوص محتوى مساحة العمل. تابع الكتابة أو عدّل مسودتك.\u0000" +
	"\nlimit\u0000You have reached today's writing-style limit. Keep writing; try again tomorrow.\u0000Das heutige Limit für Schreibstile ist erreicht. Schreiben Sie weiter; versuchen Sie es morgen erneut.\u0000بلغت الحد اليومي لأساليب الكتابة. تابع الكتابة وحاول غدًا.\u0000" +
	"\ninvalid\u0000Write a few words, then choose a style.\u0000Schreiben Sie einige Wörter und wählen Sie dann einen Stil.\u0000اكتب بضع كلمات ثم اختر أسلوبًا.\u0000" +
	"\ndenied\u0000Writing styles are unavailable in this conversation. Keep writing.\u0000Schreibstile sind in diesem Gespräch nicht verfügbar. Schreiben Sie weiter.\u0000أساليب الكتابة غير متاحة في هذه المحادثة. تابع الكتابة.\u0000" +
	"\ndisabled\u0000Writing-style controls are turned off for this workspace.\u0000Schreibstil-Steuerelemente sind für diesen Arbeitsbereich ausgeschaltet.\u0000عناصر أسلوب الكتابة معطّلة لمساحة العمل هذه.\u0000" +
	"\nundo\u0000Undo rewrite\u0000Umformulierung rückgängig machen\u0000تراجع عن إعادة الصياغة\u0000" +
	"\nchanges\u0000See changes\u0000Änderungen ansehen\u0000عرض التغييرات\u0000" +
	"\nbefore\u0000As typed\u0000Wie eingegeben\u0000كما كُتبت\u0000" +
	"\nafter\u0000Rewritten preview\u0000Umformulierte Vorschau\u0000معاينة الصياغة\u0000" +
	"\nedited\u0000Your draft changed while rewriting. Choose a style again to rewrite the latest text.\u0000Ihr Entwurf wurde während der Umformulierung geändert. Wählen Sie erneut einen Stil für den aktuellen Text.\u0000تغيّرت المسودة أثناء إعادة الصياغة. اختر أسلوبًا مرة أخرى للنص الحالي.\u0000" +
	"\nincident\u0000Incident conversations benefit from short, direct messages.\u0000In Vorfallgesprächen helfen kurze, direkte Nachrichten.\u0000تستفيد محادثات الحوادث من الرسائل القصيرة والمباشرة.\u0000" +
	"\nformal\u0000This audience benefits from neutral, courteous wording.\u0000Für diese Zielgruppe ist neutrale, höfliche Sprache hilfreich.\u0000تناسب هذا الجمهور لغة محايدة ومهذبة.\u0000" +
	"\nshort\u0000Recent messages in this conversation are short and direct.\u0000Die letzten Nachrichten in diesem Gespräch sind kurz und direkt.\u0000الرسائل الأخيرة في هذه المحادثة قصيرة ومباشرة.\u0000" +
	"\nclose\u0000A small group of colleagues benefits from warm wording.\u0000In einer kleinen Gruppe ist ein freundlicher Ton hilfreich.\u0000تستفيد مجموعة صغيرة من الزملاء من أسلوب ودود.\u0000" +
	"\nneutral\u0000Neutral, courteous wording suits this conversation.\u0000Neutrale, höfliche Sprache passt zu diesem Gespräch.\u0000تناسب هذه المحادثة لغة محايدة ومهذبة.\u0000" +
	"\naudience\u0000This style fits the conversation's audience and recent register.\u0000Dieser Stil passt zur Zielgruppe und zum bisherigen Ton des Gesprächs.\u0000يناسب هذا الأسلوب جمهور المحادثة ونبرتها الأخيرة.\u0000"

func ChattoneText(locale, key string) string {
	return integrate1Copy(chattoneCopyTable, locale, key)
}
