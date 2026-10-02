package chatui

// Chatattach001Composer is the selected conversation's upload projection.
// Its owner retains attachments until the server acknowledges the post.
type Chatattach001Composer struct {
	Files   []Chatattach001Draft
	Choose  func()
	Send    func(string, []ChatReference)
	Remove  func(string)
	Error   string
	Sending bool
	// Thread is the same projection for the reply box of the open thread; nil
	// when no thread is open. A reply's files are the reply's own: they are
	// uploaded to the conversation and sent with the reply, under its parent.
	Thread *Chatattach001Composer
}

// Sendable reports whether the files alone make a message: there is at least
// one and every one is uploaded. Text is then optional.
func (s *Chatattach001Composer) Sendable() bool {
	return s != nil && len(s.Files) > 0 && s.Ready()
}

// thread is the open thread's projection, nil when there is none. It is safe
// on a nil composer, like Ready.
func (s *Chatattach001Composer) thread() *Chatattach001Composer {
	if s == nil {
		return nil
	}
	return s.Thread
}

type Chatattach001Draft struct {
	Key string
	Attachment
	Uploading bool
	Progress  int
	// Failed is an upload that did not get through (the connection dropped, the
	// service did not answer). The file itself was not refused, so it stays
	// under the draft with Retry instead of having to be chosen again.
	Failed bool
}

// Ready reports whether the message can be sent: nothing is still uploading
// and nothing failed to upload. A failed file is retried or removed first, so
// a message never goes out without a file the person can see under it.
func (s *Chatattach001Composer) Ready() bool {
	if s == nil {
		return true
	}
	if s.Sending {
		return false
	}
	for _, f := range s.Files {
		if f.Uploading || f.Failed {
			return false
		}
	}
	return true
}

func Chatattach001Text(locale, key string) string {
	// "note" is under "Attach a file" in the Add menu: it says the limits
	// before a file is chosen, not after one is refused.
	words := map[string][3]string{
		"attach":      {"Attach a file", "Datei anhängen", "أرفق ملفًا"},
		"note":        {"Images, PDF, text or Office files, up to 20 MB each and ten per message. You can also paste or drop a file.", "Bilder, PDF-, Text- oder Office-Dateien, je bis 20 MB, höchstens zehn pro Nachricht. Einfügen oder Ablegen geht auch.", "صور أو ملفات PDF أو نص أو Office، حتى 20 ميغابايت لكل ملف وعشرة ملفات لكل رسالة. يمكنك أيضًا لصق ملف أو إسقاطه."},
		"remove":      {"Remove file", "Datei entfernen", "أزل الملف"},
		"uploading":   {"Uploading", "Wird hochgeladen", "جارٍ الرفع"},
		"size":        {"This file is too large. Choose a file under 20 MB.", "Diese Datei ist zu groß. Wählen Sie eine Datei unter 20 MB.", "هذا الملف كبير جدًا. اختر ملفًا أصغر من 20 ميغابايت."},
		"empty":       {"This file is empty. Choose a file with content.", "Diese Datei ist leer. Wählen Sie eine Datei mit Inhalt.", "هذا الملف فارغ. اختر ملفًا يحتوي على محتوى."},
		"type":        {"This file type is not allowed. Choose an image, PDF, text or Office document.", "Dieser Dateityp ist nicht erlaubt. Wählen Sie ein Bild, PDF, Text- oder Office-Dokument.", "نوع الملف غير مسموح. اختر صورة أو ملف PDF أو نصًا أو مستند Office."},
		"limit":       {"You can attach up to ten files. Remove a file to add another.", "Sie können bis zu zehn Dateien anhängen. Entfernen Sie eine, um eine weitere hinzuzufügen.", "يمكنك إرفاق عشرة ملفات كحد أقصى. أزل ملفًا لإضافة آخر."},
		"quota":       {"The attachment storage limit is reached. Ask your workspace administrator for help.", "Das Speicherlimit für Anhänge ist erreicht. Wenden Sie sich an die Arbeitsbereichsadministration.", "تم بلوغ حد تخزين المرفقات. اطلب المساعدة من مسؤول مساحة العمل."},
		"failed":      {"The file could not be uploaded. Choose it again to retry.", "Die Datei konnte nicht hochgeladen werden. Wählen Sie sie erneut aus.", "تعذر رفع الملف. اختره مجددًا للمحاولة."},
		"not_sent":    {"Not uploaded", "Nicht hochgeladen", "لم يُرفع"},
		"retry":       {"Retry", "Erneut versuchen", "أعد المحاولة"},
		"download":    {"Download", "Herunterladen", "نزّل"},
		"files":       {"Attached files", "Angehängte Dateien", "الملفات المرفقة"},
		"unavailable": {"Preview unavailable", "Vorschau nicht verfügbar", "المعاينة غير متاحة"},
	}
	i := 0
	if len(locale) >= 2 && locale[:2] == "de" {
		i = 1
	}
	if len(locale) >= 2 && locale[:2] == "ar" {
		i = 2
	}
	return chatbug039Text(key, words[key][i], words[key][0])
}
