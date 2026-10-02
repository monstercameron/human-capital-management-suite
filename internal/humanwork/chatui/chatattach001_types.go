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
}

type Chatattach001Draft struct {
	Key string
	Attachment
	Uploading bool
	Progress  int
}

func (s *Chatattach001Composer) Ready() bool {
	if s == nil {
		return true
	}
	if s.Sending {
		return false
	}
	for _, f := range s.Files {
		if f.Uploading {
			return false
		}
	}
	return true
}

func Chatattach001Text(locale, key string) string {
	words := map[string][3]string{
		"attach":      {"Attach a file", "Datei anhängen", "أرفق ملفًا"},
		"note":        {"Choose a file or photo, or paste or drop it here.", "Datei oder Foto auswählen, hier einfügen oder ablegen.", "اختر ملفًا أو صورة، أو الصقه أو أسقطه هنا."},
		"remove":      {"Remove file", "Datei entfernen", "أزل الملف"},
		"uploading":   {"Uploading", "Wird hochgeladen", "جارٍ الرفع"},
		"size":        {"This file is too large. Choose a file under 20 MB.", "Diese Datei ist zu groß. Wählen Sie eine Datei unter 20 MB.", "هذا الملف كبير جدًا. اختر ملفًا أصغر من 20 ميغابايت."},
		"empty":       {"This file is empty. Choose a file with content.", "Diese Datei ist leer. Wählen Sie eine Datei mit Inhalt.", "هذا الملف فارغ. اختر ملفًا يحتوي على محتوى."},
		"type":        {"This file type is not allowed. Choose an image, PDF, text or Office document.", "Dieser Dateityp ist nicht erlaubt. Wählen Sie ein Bild, PDF, Text- oder Office-Dokument.", "نوع الملف غير مسموح. اختر صورة أو ملف PDF أو نصًا أو مستند Office."},
		"limit":       {"You can attach up to ten files. Remove a file to add another.", "Sie können bis zu zehn Dateien anhängen. Entfernen Sie eine, um eine weitere hinzuzufügen.", "يمكنك إرفاق عشرة ملفات كحد أقصى. أزل ملفًا لإضافة آخر."},
		"quota":       {"The attachment storage limit is reached. Ask your workspace administrator for help.", "Das Speicherlimit für Anhänge ist erreicht. Wenden Sie sich an die Arbeitsbereichsadministration.", "تم بلوغ حد تخزين المرفقات. اطلب المساعدة من مسؤول مساحة العمل."},
		"failed":      {"The file could not be uploaded. Choose it again to retry.", "Die Datei konnte nicht hochgeladen werden. Wählen Sie sie erneut aus.", "تعذر رفع الملف. اختره مجددًا للمحاولة."},
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
