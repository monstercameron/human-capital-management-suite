package chatui

import "strings"

func ModerationText(locale, key string) string { return chatremoveText(locale, key) }

const chatremoveCopyTable = "\nmoderation\u0000Moderation\u0000Moderation\u0000الإشراف\u0000" +
	"\nreport\u0000Report message\u0000Nachricht melden\u0000الإبلاغ عن رسالة\u0000" +
	"\nremove\u0000Remove message\u0000Nachricht entfernen\u0000إزالة الرسالة\u0000" +
	"\nremove_everyone\u0000Remove for everyone\u0000Für alle entfernen\u0000إزالة للجميع\u0000" +
	"\nremoved\u0000Removed by an administrator\u0000Von einem Administrator entfernt\u0000أزالها مسؤول\u0000" +
	"\ndeleted\u0000Deleted by the author\u0000Vom Verfasser gelöscht\u0000حذفها الكاتب\u0000" +
	"\nrestore\u0000Restore\u0000Wiederherstellen\u0000استعادة\u0000" +
	"\nrestored\u0000Message restored\u0000Nachricht wiederhergestellt\u0000تمت استعادة الرسالة\u0000" +
	"\nsaved_removed\u0000This message was removed\u0000Diese Nachricht wurde entfernt\u0000أُزيلت هذه الرسالة\u0000" +
	"\noutcome_remove\u0000A moderator removed the message.\u0000Ein Moderator hat die Nachricht entfernt.\u0000أزال مشرف الرسالة.\u0000" +
	"\noutcome_dismiss\u0000A moderator dismissed the report.\u0000Ein Moderator hat die Meldung geschlossen.\u0000رفض مشرف البلاغ.\u0000" +
	"\noutcome_restore\u0000A moderator restored the message.\u0000Ein Moderator hat die Nachricht wiederhergestellt.\u0000استعاد مشرف الرسالة.\u0000" +
	"\noutcome_message_author\u0000A moderator contacted the author.\u0000Ein Moderator hat den Verfasser kontaktiert.\u0000راسل مشرف الكاتب.\u0000" +
	"\ndismiss\u0000Dismiss\u0000Abweisen\u0000رفض البلاغ\u0000" +
	"\nmessage_author\u0000Message the author\u0000Verfasser kontaktieren\u0000مراسلة الكاتب\u0000" +
	"\nappeal\u0000Ask for a review\u0000Überprüfung anfordern\u0000طلب مراجعة\u0000" +
	"\nappeal_sent\u0000Review requested\u0000Überprüfung angefordert\u0000طُلبت المراجعة\u0000" +
	"\nreason\u0000Reason\u0000Grund\u0000السبب\u0000" +
	"\nnote\u0000Add details (optional)\u0000Details hinzufügen (optional)\u0000إضافة تفاصيل (اختياري)\u0000" +
	"\ndecision_reason\u0000Explain your decision or write to the author\u0000Entscheidung erläutern oder dem Verfasser schreiben\u0000اشرح قرارك أو اكتب للكاتب\u0000" +
	"\nharassment\u0000Harassment or bullying\u0000Belästigung oder Mobbing\u0000مضايقة أو تنمر\u0000" +
	"\nsensitive_information\u0000Private or confidential information\u0000Private oder vertrauliche Informationen\u0000معلومات خاصة أو سرية\u0000" +
	"\nspam\u0000Spam or advertising\u0000Spam oder Werbung\u0000رسائل مزعجة أو إعلانات\u0000" +
	"\npolicy_violation\u0000Breaks the workplace rules\u0000Verstößt gegen die Arbeitsplatzregeln\u0000تخالف قواعد العمل\u0000" +
	"\npreview\u0000Count messages\u0000Nachrichten zählen\u0000عد الرسائل\u0000" +
	"\nconfirm\u0000Confirm removal\u0000Entfernung bestätigen\u0000تأكيد الإزالة\u0000" +
	"\nconfirm_restore\u0000Confirm restore\u0000Wiederherstellung bestätigen\u0000تأكيد الاستعادة\u0000" +
	"\ncount\u0000{n} messages selected\u0000{n} Nachrichten ausgewählt\u0000تم اختيار {n} رسائل\u0000" +
	"\nopen_count\u0000{n} open items you can review\u0000{n} offene Einträge zur Überprüfung\u0000{n} عناصر مفتوحة يمكنك مراجعتها\u0000" +
	"\nretain\u0000The record and its holds stay intact. Restore is available for thirty days. The author receives the reason privately.\u0000Der Datensatz und seine Aufbewahrungssperren bleiben erhalten. Wiederherstellung ist dreißig Tage lang möglich. Der Verfasser erhält den Grund privat.\u0000يبقى السجل وأوامر حفظه كما هي. تتاح الاستعادة لمدة ثلاثين يومًا. يتلقى الكاتب السبب بصورة خاصة.\u0000" +
	"\nreport_private\u0000Only moderators see your identity and note. You will receive the outcome.\u0000Nur Moderatoren sehen Ihre Identität und Notiz. Sie erhalten das Ergebnis.\u0000يرى المشرفون فقط هويتك وملاحظتك. ستتلقى نتيجة المراجعة.\u0000" +
	"\nempty\u0000No open items you can review. Report a message from its menu if you need help.\u0000Keine offenen Einträge zur Überprüfung. Melden Sie bei Bedarf eine Nachricht über ihr Menü.\u0000لا توجد عناصر مفتوحة يمكنك مراجعتها. أبلغ عن رسالة من قائمتها إذا احتجت إلى مساعدة.\u0000" +
	"\nloading\u0000Loading moderation items. Please wait.\u0000Moderationseinträge werden geladen. Bitte warten.\u0000جارٍ تحميل عناصر الإشراف. يرجى الانتظار.\u0000" +
	"\nerror\u0000Moderation is unavailable. Try again.\u0000Moderation ist nicht verfügbar. Erneut versuchen.\u0000الإشراف غير متاح. حاول مجددًا.\u0000" +
	"\nconflict\u0000The messages or your access changed. Count messages again before confirming.\u0000Nachrichten oder Zugriffsrechte haben sich geändert. Zählen Sie die Nachrichten vor der Bestätigung erneut.\u0000تغيرت الرسائل أو صلاحياتك. عد الرسائل مجددًا قبل التأكيد.\u0000" +
	"\nretry\u0000Try again\u0000Erneut versuchen\u0000حاول مجددًا\u0000" +
	"\nsearch\u0000Search queue and history\u0000Warteschlange und Verlauf durchsuchen\u0000البحث في قائمة المراجعة والسجل\u0000" +
	"\nsearch_action\u0000Search\u0000Suchen\u0000بحث\u0000" +
	"\nauthor\u0000Author\u0000Verfasser\u0000الكاتب\u0000" +
	"\nreporter\u0000Reported by\u0000Gemeldet von\u0000أبلغ عنها\u0000" +
	"\ncontext\u0000Message in context\u0000Nachricht im Kontext\u0000الرسالة في سياقها\u0000" +
	"\nnotice\u0000Your message was removed\u0000Ihre Nachricht wurde entfernt\u0000أُزيلت رسالتك\u0000" +
	"\noutcome\u0000Review outcome\u0000Ergebnis der Überprüfung\u0000نتيجة المراجعة\u0000" +
	"\ncancel\u0000Cancel\u0000Abbrechen\u0000إلغاء\u0000" +
	"\nselected\u0000Selected messages\u0000Ausgewählte Nachrichten\u0000الرسائل المختارة\u0000" +
	"\nrange\u0000One person's messages in a time range\u0000Nachrichten einer Person in einem Zeitraum\u0000رسائل شخص خلال فترة زمنية\u0000" +
	"\nfrom\u0000From\u0000Von\u0000من\u0000" +
	"\nuntil\u0000Until (exclusive)\u0000Bis (ausschließlich)\u0000حتى (غير مشمول)\u0000" +
	"\nsubmit\u0000Send report\u0000Meldung senden\u0000إرسال البلاغ\u0000" +
	"\nsent\u0000Your report was sent. You will receive the outcome.\u0000Ihre Meldung wurde gesendet. Sie erhalten das Ergebnis.\u0000أُرسل بلاغك. ستتلقى النتيجة.\u0000" +
	"\nclosed\u0000Closed\u0000Geschlossen\u0000مغلق\u0000" +
	"\nopen\u0000Open\u0000Offen\u0000مفتوح\u0000" +
	"\nfilter\u0000Filter hit\u0000Filtertreffer\u0000تطابق مع مرشح\u0000" +
	"\nremoval\u0000Removal\u0000Entfernung\u0000إزالة\u0000" +
	"\nappeal_item\u0000Appeal\u0000Überprüfungsantrag\u0000طلب مراجعة\u0000" +
	"\nread_reason\u0000Record a reason to review the removed message\u0000Grund für die Einsicht in die entfernte Nachricht angeben\u0000سجّل سببًا لمراجعة الرسالة المُزالة\u0000" +
	"\nreview\u0000Review removed message\u0000Entfernte Nachricht überprüfen\u0000مراجعة الرسالة المُزالة\u0000" +
	"\nremove_title\u0000Remove this message?\u0000Diese Nachricht entfernen?\u0000إزالة هذه الرسالة؟\u0000" +
	"\nremove_help\u0000Everyone in this channel will see “Removed by an administrator” instead of the text. {name} is told why and can ask for a review. You can restore it for thirty days. The message itself is kept.\u0000Alle in diesem Kanal sehen statt des Textes „Von einem Administrator entfernt“. {name} erfährt den Grund und kann eine Überprüfung anfordern. Sie können die Nachricht dreißig Tage lang wiederherstellen. Die Nachricht selbst bleibt gespeichert.\u0000سيرى الجميع في هذه القناة «أزالها مسؤول» بدل النص. سيُبلَّغ {name} بالسبب ويمكنه طلب مراجعة. يمكنك استعادتها خلال ثلاثين يومًا. تبقى الرسالة نفسها محفوظة.\u0000" +
	"\nmessage_from\u0000Message from {name}\u0000Nachricht von {name}\u0000رسالة من {name}\u0000" +
	"\nwhy_removed\u0000Why is it being removed?\u0000Warum wird sie entfernt?\u0000لماذا تُزال؟\u0000" +
	"\nwhy_reported\u0000What is wrong with it?\u0000Was ist daran falsch?\u0000ما المشكلة فيها؟\u0000" +
	"\nnote_for_author\u0000Add a note for {name} (optional)\u0000Notiz für {name} hinzufügen (optional)\u0000أضف ملاحظة لـ {name} (اختياري)\u0000" +
	"\nseveral\u0000Remove several messages instead\u0000Stattdessen mehrere Nachrichten entfernen\u0000إزالة عدة رسائل بدلاً من ذلك\u0000" +
	"\nseveral_help\u0000Removes every message from {name} in this channel between the times you choose. You see the count before anything is removed.\u0000Entfernt alle Nachrichten von {name} in diesem Kanal zwischen den gewählten Zeitpunkten. Die Anzahl sehen Sie, bevor etwas entfernt wird.\u0000تُزال كل رسائل {name} في هذه القناة بين الوقتين اللذين تختارهما. ترى العدد قبل إزالة أي شيء.\u0000" +
	"\nrestore_title\u0000Restore this message?\u0000Diese Nachricht wiederherstellen?\u0000استعادة هذه الرسالة؟\u0000" +
	"\nrestore_help\u0000The message comes back exactly as it was, with its reactions and files. {name} is told.\u0000Die Nachricht kommt genau so zurück, wie sie war, mit Reaktionen und Dateien. {name} wird benachrichtigt.\u0000تعود الرسالة تمامًا كما كانت، مع تفاعلاتها وملفاتها. يُبلَّغ {name}.\u0000" +
	"\nno_action\u0000No action was needed.\u0000Es war keine Maßnahme nötig.\u0000لم يلزم أي إجراء.\u0000" +
	"\nnote_required\u0000Write a note for the author first.\u0000Schreiben Sie zuerst eine Notiz für den Verfasser.\u0000اكتب ملاحظة للكاتب أولاً.\u0000" +
	"\nnotices_title\u0000Notices about your messages\u0000Hinweise zu Ihren Nachrichten\u0000إشعارات عن رسائلك\u0000" +
	"\nnotices_empty\u0000Nothing to show. When a moderator removes one of your messages, or answers a report you sent, it appears here.\u0000Nichts anzuzeigen. Wenn ein Moderator eine Ihrer Nachrichten entfernt oder eine Meldung von Ihnen beantwortet, erscheint es hier.\u0000لا شيء للعرض. عندما يزيل مشرف إحدى رسائلك أو يرد على بلاغ أرسلته، يظهر ذلك هنا.\u0000" +
	"\nsidebar_open\u0000{n} open items\u0000{n} offene Einträge\u0000{n} عناصر مفتوحة\u0000" +
	"\nsidebar_notices\u0000{n} new notices\u0000{n} neue Hinweise\u0000{n} إشعارات جديدة\u0000" +
	"\nrule\u0000Rule that matched\u0000Zutreffende Regel\u0000القاعدة المطابقة\u0000" +
	"\nfrom_moderator\u0000A moderator wrote to you\u0000Ein Moderator hat Ihnen geschrieben\u0000كتب لك مشرف\u0000" +
	"\nremoval_reason\u0000Reason for removing\u0000Grund für die Entfernung\u0000سبب الإزالة\u0000" +
	"\nown_reason\u0000Reason: {reason}\u0000Grund: {reason}\u0000السبب: {reason}\u0000" +
	"\nkind_report\u0000Report\u0000Meldung\u0000بلاغ\u0000" +
	"\nsomeone\u0000Someone\u0000Jemand\u0000شخص\u0000" +
	"\nno_message\u0000No message could be matched to this item. It can be dismissed.\u0000Diesem Eintrag konnte keine Nachricht zugeordnet werden. Er kann geschlossen werden.\u0000تعذر ربط رسالة بهذا العنصر. يمكن إغلاقه.\u0000" +
	"\nfailed\u0000That did not work. Nothing was changed. Try again.\u0000Das hat nicht geklappt. Es wurde nichts geändert. Versuchen Sie es erneut.\u0000لم تنجح العملية. لم يتغير شيء. حاول مجددًا.\u0000" +
	"\nforbidden\u0000You no longer have permission to do that.\u0000Dafür haben Sie keine Berechtigung mehr.\u0000لم تعد لديك صلاحية لذلك.\u0000" +
	"\nclose\u0000Close\u0000Schließen\u0000إغلاق\u0000" +
	"\nclose_moderation\u0000Close moderation\u0000Moderation schließen\u0000إغلاق الإشراف\u0000" +
	"\ntab_open\u0000Open\u0000Offen\u0000مفتوحة\u0000" +
	"\ntab_resolved\u0000Resolved\u0000Erledigt\u0000تمت معالجتها\u0000" +
	"\nsearch_placeholder\u0000Search reports and history\u0000Meldungen und Verlauf durchsuchen\u0000ابحث في البلاغات والسجل\u0000" +
	"\nnothing\u0000Nothing to review.\u0000Nichts zu überprüfen.\u0000لا شيء للمراجعة.\u0000" +
	"\nnothing_hint\u0000Reports and flagged messages will appear here.\u0000Meldungen und markierte Nachrichten erscheinen hier.\u0000ستظهر هنا البلاغات والرسائل المعلَّمة.\u0000" +
	"\nnothing_resolved\u0000Nothing has been resolved yet.\u0000Noch nichts wurde erledigt.\u0000لم تتم معالجة أي شيء بعد.\u0000" +
	"\nnothing_resolved_hint\u0000Decisions you and other moderators make are kept here.\u0000Entscheidungen von Ihnen und anderen Moderatoren bleiben hier erhalten.\u0000تبقى هنا القرارات التي تتخذها أنت وبقية المشرفين.\u0000" +
	"\nshow_context\u0000Show the conversation around it\u0000Unterhaltung drumherum anzeigen\u0000عرض المحادثة المحيطة بها\u0000" +
	"\nno_match\u0000No items match your search.\u0000Keine Einträge passen zur Suche.\u0000لا توجد عناصر تطابق بحثك.\u0000" +
	"\nreported_line\u0000Reported by {name} · {when}\u0000Gemeldet von {name} · {when}\u0000أبلغ عنها {name} · {when}\u0000" +
	"\nappeal_line\u0000{name} asked for a review · {when}\u0000{name} hat eine Überprüfung angefordert · {when}\u0000طلب {name} مراجعة · {when}\u0000" +
	"\nflag_line\u0000Flagged by a filter · {when}\u0000Von einem Filter markiert · {when}\u0000علّمها مرشح · {when}\u0000" +
	"\nremoval_line\u0000Removed by {name} · {when}\u0000Entfernt von {name} · {when}\u0000أزالها {name} · {when}\u0000" +
	"\ndecided_remove\u0000Removed by {name} · {when}\u0000Entfernt von {name} · {when}\u0000أزالها {name} · {when}\u0000" +
	"\ndecided_dismiss\u0000Dismissed by {name} · {when}\u0000Abgewiesen von {name} · {when}\u0000رفضها {name} · {when}\u0000" +
	"\ndecided_restore\u0000Restored by {name} · {when}\u0000Wiederhergestellt von {name} · {when}\u0000استعادها {name} · {when}\u0000" +
	"\ndecided_message_author\u0000{name} wrote to the author · {when}\u0000{name} hat dem Verfasser geschrieben · {when}\u0000كتب {name} إلى الكاتب · {when}\u0000" +
	"\nmessage_title\u0000Message {name}\u0000{name} schreiben\u0000مراسلة {name}\u0000" +
	"\nmessage_label\u0000What do you want to tell {name}?\u0000Was möchten Sie {name} mitteilen?\u0000ماذا تريد أن تخبر {name}؟\u0000" +
	"\nmessage_help\u0000Only {name} sees this. It is recorded with your decision.\u0000Nur {name} sieht das. Es wird mit Ihrer Entscheidung festgehalten.\u0000لا يراها إلا {name}. تُسجَّل مع قرارك.\u0000" +
	"\nsend_message\u0000Send message\u0000Nachricht senden\u0000إرسال الرسالة\u0000" +
	"\nremoved_badge\u0000Removed\u0000Entfernt\u0000مُزالة\u0000" +
	"\nremove_short\u0000Remove\u0000Entfernen\u0000إزالة\u0000" +
	"\nchoose\u0000Choose which messages to remove\u0000Auswählen, welche Nachrichten entfernt werden\u0000اختيار الرسائل المراد إزالتها\u0000" +
	"\nchoose_help\u0000Tick the messages to remove from this part of the conversation. You see the count before anything is removed.\u0000Markieren Sie die Nachrichten aus diesem Teil der Unterhaltung, die entfernt werden sollen. Die Anzahl sehen Sie, bevor etwas entfernt wird.\u0000حدّد الرسائل المراد إزالتها من هذا الجزء من المحادثة. ترى العدد قبل إزالة أي شيء.\u0000" +
	"\nnone_selected\u0000Tick at least one message first.\u0000Markieren Sie zuerst mindestens eine Nachricht.\u0000حدّد رسالة واحدة على الأقل أولاً.\u0000" +
	"\nrestored_notice\u0000An administrator restored this message.\u0000Ein Administrator hat diese Nachricht wiederhergestellt.\u0000استعاد مسؤول هذه الرسالة.\u0000" +
	"\nfilter_notice\u0000A filter matched a message\u0000Ein Filter hat bei einer Nachricht angeschlagen\u0000طابق مرشحٌ رسالة\u0000" +
	"\nfilter_notice_rule\u0000Filter: {name}\u0000Filter: {name}\u0000المرشح: {name}\u0000" +
	"\nfilter_notice_open\u0000Open the conversation it matched in\u0000Unterhaltung mit dem Treffer öffnen\u0000فتح المحادثة التي حدثت فيها المطابقة\u0000" +
	"\ntab_permissions\u0000Permissions\u0000Berechtigungen\u0000الصلاحيات\u0000" +
	"\nperm_intro\u0000Who may do what in moderation. An answer for a role applies to everyone who has that role. Press a switch to change it.\u0000Wer in der Moderation was tun darf. Eine Angabe für eine Rolle gilt für alle mit dieser Rolle. Drücken Sie einen Schalter, um sie zu ändern.\u0000من يحق له فعل ماذا في الإشراف. ما يُحدَّد لدور يسري على كل من يحمل ذلك الدور. اضغط مفتاحاً لتغييره.\u0000" +
	"\nperm_scope_ws\u0000In the whole workspace\u0000Im ganzen Arbeitsbereich\u0000في مساحة العمل بأكملها\u0000" +
	"\nperm_scope_ch\u0000Only in #{name}\u0000Nur in #{name}\u0000في #{name} فقط\u0000" +
	"\nperm_choose\u0000Set them for one channel instead\u0000Stattdessen für einen Kanal festlegen\u0000تحديدها لقناة واحدة بدلاً من ذلك\u0000" +
	"\nperm_back_ws\u0000Back to the whole workspace\u0000Zurück zum ganzen Arbeitsbereich\u0000العودة إلى مساحة العمل بأكملها\u0000" +
	"\nperm_role\u0000Role\u0000Rolle\u0000الدور\u0000" +
	"\nperm_report\u0000Report messages\u0000Nachrichten melden\u0000الإبلاغ عن الرسائل\u0000" +
	"\nperm_remove\u0000Remove messages\u0000Nachrichten entfernen\u0000إزالة الرسائل\u0000" +
	"\nperm_review\u0000Read and restore removed messages\u0000Entfernte Nachrichten lesen und wiederherstellen\u0000قراءة الرسائل المُزالة واستعادتها\u0000" +
	"\nperm_filters\u0000Manage filters\u0000Filter verwalten\u0000إدارة المرشحات\u0000" +
	"\nrole_WORKSPACE_ADMIN\u0000Workspace administrators\u0000Arbeitsbereichsadministratoren\u0000مسؤولو مساحة العمل\u0000" +
	"\nrole_MANAGER\u0000Channel managers, in their channel\u0000Kanalverwaltung, im eigenen Kanal\u0000مديرو القنوات، في قنواتهم\u0000" +
	"\nrole_MEMBER\u0000Channel members, in their channel\u0000Kanalmitglieder, im eigenen Kanal\u0000أعضاء القنوات، في قنواتهم\u0000" +
	"\nperm_yes\u0000Allowed\u0000Erlaubt\u0000مسموح\u0000" +
	"\nperm_no\u0000Not allowed\u0000Nicht erlaubt\u0000غير مسموح\u0000" +
	"\nperm_cell\u0000{permission}: {role}\u0000{permission}: {role}\u0000{permission}: {role}\u0000" +
	"\nperm_src_default\u0000Default\u0000Standard\u0000الإعداد الافتراضي\u0000" +
	"\nperm_src_workspace\u0000Set for the workspace\u0000Für den Arbeitsbereich festgelegt\u0000محدَّد لمساحة العمل\u0000" +
	"\nperm_src_own\u0000Set for this channel\u0000Für diesen Kanal festgelegt\u0000محدَّد لهذه القناة\u0000" +
	"\nperm_note\u0000A person holds a permission when any of their roles does. A channel's own answer replaces the workspace's for that channel. Removing or reading in a private channel still takes being a member of it.\u0000Eine Person hat eine Berechtigung, wenn eine ihrer Rollen sie hat. Die Angabe eines Kanals ersetzt dort die des Arbeitsbereichs. In einem privaten Kanal entfernen oder lesen kann weiterhin nur, wer dort Mitglied ist.\u0000يحمل الشخص الصلاحية إذا حملها أحد أدواره. ما يُحدَّد لقناة يحل محل إعداد مساحة العمل في تلك القناة. الإزالة أو القراءة في قناة خاصة تتطلب العضوية فيها.\u0000" +
	"\nperm_other\u0000Another role\u0000Eine andere Rolle\u0000دور آخر\u0000" +
	"\nperm_other_hint\u0000Type a role's name exactly as your workspace uses it. Its row appears; nothing is saved until you press a switch in it.\u0000Geben Sie den Namen einer Rolle genau so ein, wie Ihr Arbeitsbereich ihn verwendet. Die Zeile erscheint; gespeichert wird erst, wenn Sie darin einen Schalter drücken.\u0000اكتب اسم الدور تماماً كما تستخدمه مساحة عملك. يظهر صفّه، ولا يُحفظ شيء حتى تضغط مفتاحاً فيه.\u0000" +
	"\nperm_other_add\u0000Show this role\u0000Diese Rolle anzeigen\u0000عرض هذا الدور\u0000"

func chatremoveText(locale, key string) string {
	return integrate1Copy(chatremoveCopyTable, locale, key)
}

func chatremoveReason(locale, reason string) string {
	parts := strings.SplitN(reason, " — ", 2)
	translated := chatremoveText(locale, parts[0])
	if translated == "" {
		return reason
	}
	if len(parts) == 2 {
		translated += " — " + parts[1]
	}
	return translated
}
