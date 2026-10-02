package chatmedia

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

const Chatattach001MaxBytes int64 = 20 << 20

// Chatattach001Scanner is a bounded structural admission check, not antivirus.
// External, when present, must also admit the original bytes. No content is
// executed, no network is used, and generic archives and macro documents fail.
type Chatattach001Scanner struct {
	External Scanner
	MaxBytes int64
}

func (s Chatattach001Scanner) Scan(ctx context.Context, id string, r io.Reader) (quarantine.Verdict, error) {
	limit := s.MaxBytes
	if limit <= 0 || limit > Chatattach001MaxBytes {
		limit = Chatattach001MaxBytes
	}
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return quarantine.Verdict{}, err
	}
	if err := ctx.Err(); err != nil {
		return quarantine.Verdict{}, err
	}
	if int64(len(b)) > limit {
		return quarantine.Verdict{Reason: "file too large"}, nil
	}
	if Chatattach001ContentType(b) == "" {
		return quarantine.Verdict{Reason: "file type not allowed"}, nil
	}
	if s.External != nil {
		return s.External.Scan(ctx, id, bytes.NewReader(b))
	}
	return quarantine.Verdict{Safe: true, Reason: "bounded structural file check only"}, nil
}

// Chatattach001ContentType returns only sniffed types; declarations and file
// extensions never establish safety. Office ZIP containers must contain one
// recognised document root, with no macros, executables, embedded objects,
// external relationships or path traversal. Legacy binary Office is refused.
func Chatattach001ContentType(b []byte) string {
	if len(b) == 0 || int64(len(b)) > Chatattach001MaxBytes {
		return ""
	}
	if bytes.HasPrefix(b, []byte("MZ")) || bytes.HasPrefix(b, []byte("\x7fELF")) || bytes.HasPrefix(b, []byte("Rar!")) || bytes.HasPrefix(b, []byte("7z\xbc\xaf\x27\x1c")) || bytes.HasPrefix(b, []byte("\x1f\x8b")) {
		return ""
	}
	if bytes.HasPrefix(b, []byte("PK\x03\x04")) {
		return chatattach001OfficeType(b)
	}
	t := strings.Split(http.DetectContentType(b), ";")[0]
	switch t {
	case "image/png", "image/jpeg", "image/gif", "image/bmp":
		return t
	case "application/pdf":
		lower := bytes.ToLower(b)
		for _, unsafe := range []string{"/javascript", "/js", "/launch", "/embeddedfile", "/openaction", "/aa", "/encrypt"} {
			if bytes.Contains(lower, []byte(unsafe)) {
				return ""
			}
		}
		return t
	case "text/plain":
		if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
			return ""
		}
		for _, c := range b {
			if c < 32 && c != '\n' && c != '\r' && c != '\t' {
				return ""
			}
		}
		lower := strings.ToLower(strings.TrimSpace(string(b)))
		for _, prefix := range []string{"#!", "<?php", "<script", "<!doctype", "<html", "<svg", "@echo", "powershell", "<?xml"} {
			if strings.HasPrefix(lower, prefix) {
				return ""
			}
		}
		return t
	}
	return ""
}

func chatattach001OfficeType(b []byte) string {
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil || len(z.File) > 2000 {
		return ""
	}
	var total uint64
	types, roots := false, 0
	result := ""
	for _, f := range z.File {
		n := strings.ToLower(f.Name)
		total += f.UncompressedSize64
		if f.UncompressedSize64 > uint64(Chatattach001MaxBytes)*4 || total > uint64(Chatattach001MaxBytes)*4 || strings.Contains(n, "\\") || strings.HasPrefix(n, "../") || strings.HasPrefix(n, "/") || path.Clean(n) != strings.TrimSuffix(n, "/") || strings.Contains(n, "vbaproject") || strings.Contains(n, "embeddings/") || strings.Contains(n, "activex/") {
			return ""
		}
		for _, ext := range []string{".exe", ".dll", ".com", ".bat", ".cmd", ".ps1", ".js", ".zip"} {
			if strings.HasSuffix(n, ext) {
				return ""
			}
		}
		switch n {
		case "[content_types].xml":
			types = true
		case "word/document.xml":
			roots++
			result = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		case "xl/workbook.xml":
			roots++
			result = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		case "ppt/presentation.xml":
			roots++
			result = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
		}
		if strings.HasSuffix(n, ".rels") || n == "[content_types].xml" {
			r, err := f.Open()
			if err != nil {
				return ""
			}
			v, err := io.ReadAll(io.LimitReader(r, 1<<20))
			_ = r.Close()
			if err != nil || len(v) == 1<<20 || bytes.Contains(bytes.ToLower(v), []byte("macroenabled")) || bytes.Contains(bytes.ToLower(v), []byte("external")) {
				return ""
			}
		}
	}
	if !types || roots != 1 {
		return ""
	}
	return result
}
