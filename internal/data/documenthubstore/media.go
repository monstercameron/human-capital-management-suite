// Document media: images and PDFs uploaded into a personal document.
//
// The media type is sniffed from the bytes (http.DetectContentType plus a
// PDF magic check), never taken from the client. Images are capped at
// 10 MB and PDFs at 25 MB. The bytes are content-addressed by their
// SHA-256 and stored by a MediaBlobs adapter (the filesystem under the
// configured media root); the row that binds a blob to a document lives in
// document_media. Uploading needs the document's edit right (its owner
// holding read and propose); listing and reading need its read grant.
package documenthubstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"  // image.DecodeConfig for sniffed GIFs
	_ "image/jpeg" // image.DecodeConfig for sniffed JPEGs
	_ "image/png"  // image.DecodeConfig for sniffed PNGs
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Media size caps.
const (
	MaxMediaImageBytes = 10 << 20
	MaxMediaPDFBytes   = 25 << 20
	// maxMediaPixels refuses images whose header promises a decode far
	// larger than any document needs (a decompression bomb).
	maxMediaPixels = 40_000_000
)

// Media types this store admits.
const (
	MediaPNG  = "image/png"
	MediaJPEG = "image/jpeg"
	MediaGIF  = "image/gif"
	MediaWebP = "image/webp"
	MediaPDF  = "application/pdf"
)

var (
	// ErrMediaUnsupported is bytes that are not one of the admitted types.
	ErrMediaUnsupported = errors.New("document media: unsupported media type")
	// ErrMediaTooLarge is bytes over the cap for their sniffed type.
	ErrMediaTooLarge = errors.New("document media: too large")
	// ErrMediaCorrupt is stored bytes whose hash no longer matches.
	ErrMediaCorrupt = errors.New("document media: stored bytes do not match their hash")
)

// Media is one attachment row.
type Media struct {
	ID, DocumentID, SHA256, Filename, MediaType, UploadedBy string
	SizeBytes                                               int64
	Width, Height, Pages                                    int
	CreatedAt                                               time.Time
}

// IsImage reports whether the attachment renders inline.
func (m Media) IsImage() bool { return strings.HasPrefix(m.MediaType, "image/") }

// MediaFacts is what sniffing learned about some bytes.
type MediaFacts struct {
	MediaType            string
	Width, Height, Pages int
}

// InspectMedia sniffs the media type from the bytes and enforces the caps.
// A declared type is never consulted: the answer comes from the content.
func InspectMedia(content []byte) (MediaFacts, error) {
	if len(content) == 0 {
		return MediaFacts{}, ErrMediaUnsupported
	}
	if bytes.HasPrefix(content, []byte("%PDF-")) {
		if len(content) > MaxMediaPDFBytes {
			return MediaFacts{}, ErrMediaTooLarge
		}
		return MediaFacts{MediaType: MediaPDF, Pages: CountPDFPages(content)}, nil
	}
	sniffed := http.DetectContentType(content)
	facts := MediaFacts{MediaType: sniffed}
	switch sniffed {
	case MediaPNG, MediaJPEG, MediaGIF:
		cfg, format, err := image.DecodeConfig(bytes.NewReader(content))
		if err != nil || "image/"+format != sniffed {
			return MediaFacts{}, ErrMediaUnsupported
		}
		facts.Width, facts.Height = cfg.Width, cfg.Height
	case MediaWebP:
		w, h, ok := webpSize(content)
		if !ok {
			return MediaFacts{}, ErrMediaUnsupported
		}
		facts.Width, facts.Height = w, h
	default:
		return MediaFacts{}, ErrMediaUnsupported
	}
	if len(content) > MaxMediaImageBytes {
		return MediaFacts{}, ErrMediaTooLarge
	}
	if facts.Width <= 0 || facts.Height <= 0 || facts.Width*facts.Height > maxMediaPixels {
		return MediaFacts{}, ErrMediaUnsupported
	}
	return facts, nil
}

// webpSize reads the canvas size from a RIFF/WEBP header (VP8, VP8L or
// VP8X), without decoding the image.
func webpSize(b []byte) (int, int, bool) {
	if len(b) < 30 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return 0, 0, false
	}
	chunk, data := string(b[12:16]), b[20:]
	switch chunk {
	case "VP8 ":
		if len(data) < 10 || data[3] != 0x9d || data[4] != 0x01 || data[5] != 0x2a {
			return 0, 0, false
		}
		return int(binary.LittleEndian.Uint16(data[6:8]) & 0x3fff), int(binary.LittleEndian.Uint16(data[8:10]) & 0x3fff), true
	case "VP8L":
		if len(data) < 5 || data[0] != 0x2f {
			return 0, 0, false
		}
		bits := binary.LittleEndian.Uint32(data[1:5])
		return int(bits&0x3fff) + 1, int((bits>>14)&0x3fff) + 1, true
	case "VP8X":
		if len(data) < 10 {
			return 0, 0, false
		}
		w := int(data[4]) | int(data[5])<<8 | int(data[6])<<16
		h := int(data[7]) | int(data[8])<<8 | int(data[9])<<16
		return w + 1, h + 1, true
	}
	return 0, 0, false
}

var pdfPageObject = regexp.MustCompile(`/Type\s*/Page[^s]`)

// CountPDFPages is a cheap page count: the number of page objects written
// out in the file. Compressed object streams hide them, so 0 means unknown.
func CountPDFPages(content []byte) int {
	return len(pdfPageObject.FindAllIndex(content, -1))
}

// CleanMediaFilename keeps a display name safe to show and to send in a
// Content-Disposition: the base name only, no control characters, at most
// 120 characters, and a fallback when nothing is left.
func CleanMediaFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = name[strings.LastIndex(name, "/")+1:]
	if !utf8.ValidString(name) {
		name = strings.ToValidUTF8(name, "")
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '"' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if runes := []rune(name); len(runes) > 120 {
		name = string(runes[:120])
	}
	if name == "" || name == "." || name == ".." {
		return "attachment"
	}
	return name
}

// MediaBlobs stores content-addressed bytes per tenant.
type MediaBlobs interface {
	Put(ctx context.Context, tenantID, sha string, content []byte) error
	Get(ctx context.Context, tenantID, sha string) ([]byte, error)
}

// MediaFiles keeps blobs on disk: <root>/<tenant key>/<sha[0:2]>/<sha>.
// The tenant key is a hash of the tenant id, so no request text becomes a
// path component.
type MediaFiles struct{ root string }

// NewMediaFiles creates the media root if it does not exist.
func NewMediaFiles(root string) (*MediaFiles, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("document media: root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &MediaFiles{root: root}, nil
}

var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (f *MediaFiles) path(tenantID, sha string) (string, error) {
	if !shaPattern.MatchString(sha) || strings.TrimSpace(tenantID) == "" {
		return "", ErrDenied
	}
	key := sha256.Sum256([]byte(tenantID))
	return filepath.Join(f.root, hex.EncodeToString(key[:8]), sha[:2], sha), nil
}

// Put writes the blob once; an existing blob with the same hash is kept.
// The write goes to a temporary file first and is renamed into place, so a
// reader never sees a half-written blob.
func (f *MediaFiles) Put(ctx context.Context, tenantID, sha string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	target, err := f.path(tenantID, sha)
	if err != nil {
		return err
	}
	if _, err := os.Stat(target); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), sha+".*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(content)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return errors.Join(werr, cerr)
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		_ = os.Remove(tmp.Name())
		if _, statErr := os.Stat(target); statErr == nil {
			return nil
		}
		return err
	}
	return nil
}

// Get reads a blob.
func (f *MediaFiles) Get(ctx context.Context, tenantID, sha string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target, err := f.path(tenantID, sha)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(target)
}

const mediaColumns = `id,document_id,sha256,filename,media_type,size_bytes,width,height,page_count,uploaded_by,created_at`

func scanMedia(row interface{ Scan(...any) error }) (Media, error) {
	var m Media
	err := row.Scan(&m.ID, &m.DocumentID, &m.SHA256, &m.Filename, &m.MediaType, &m.SizeBytes, &m.Width, &m.Height, &m.Pages, &m.UploadedBy, &m.CreatedAt)
	return m, err
}

// mediaDocumentTx loads the document and checks the actor may act on its
// media: reading needs the read grant; editing is the owner's, holding
// read and propose (the same rule that lets them save a version).
func mediaDocumentTx(ctx context.Context, tx dbport.Tx, tenantID, actorID, docID string, edit bool) error {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(docID) == "" {
		return ErrDenied
	}
	var owner, home, lifecycle string
	if err := tx.QueryRow(ctx, `SELECT owner_id,home,lifecycle FROM document WHERE tenant_id=$1 AND id=$2`, tenantID, docID).Scan(&owner, &home, &lifecycle); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrDenied
		}
		return err
	}
	if home != "PERSONAL" || lifecycle == "DISPOSED" {
		return ErrDenied
	}
	if err := authorizeTx(ctx, tx, tenantID, docID, "person", actorID, ActionRead); err != nil {
		return err
	}
	if !edit {
		return nil
	}
	if owner != actorID {
		return ErrDenied
	}
	return authorizeTx(ctx, tx, tenantID, docID, "person", actorID, ActionPropose)
}

// AddMedia sniffs, caps and stores content as an attachment of a document
// the actor may edit. The same bytes uploaded to the same document again
// return the existing attachment.
func (s *Store) AddMedia(ctx context.Context, blobs MediaBlobs, tenantID, actorID, docID, filename string, content []byte) (Media, error) {
	if blobs == nil {
		return Media{}, errors.New("document media: no blob store")
	}
	facts, err := InspectMedia(content)
	if err != nil {
		return Media{}, err
	}
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])
	var out Media
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := mediaDocumentTx(ctx, tx, tenantID, actorID, docID, true); err != nil {
			return err
		}
		existing, err := scanMedia(tx.QueryRow(ctx, `SELECT `+mediaColumns+` FROM document_media WHERE tenant_id=$1 AND document_id=$2 AND sha256=$3`, tenantID, docID, sha))
		if err == nil {
			out = existing
			return nil
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if err := blobs.Put(ctx, tenantID, sha, content); err != nil {
			return err
		}
		out, err = scanMedia(tx.QueryRow(ctx, `INSERT INTO document_media(id,tenant_id,document_id,sha256,filename,media_type,size_bytes,width,height,page_count,uploaded_by)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+mediaColumns,
			"docm-"+uuid.NewString(), tenantID, docID, sha, CleanMediaFilename(filename), facts.MediaType, int64(len(content)),
			facts.Width, facts.Height, facts.Pages, actorID))
		return err
	})
	if err != nil {
		return Media{}, err
	}
	return out, nil
}

// ListMedia returns a readable document's attachments, oldest first.
func (s *Store) ListMedia(ctx context.Context, tenantID, actorID, docID string) ([]Media, error) {
	out := []Media{}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := mediaDocumentTx(ctx, tx, tenantID, actorID, docID, false); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+mediaColumns+` FROM document_media WHERE tenant_id=$1 AND document_id=$2 ORDER BY created_at, id`, tenantID, docID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			m, err := scanMedia(rows)
			if err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// OpenMedia returns one attachment and its bytes to a reader. Unknown ids
// fail as denied, and bytes whose hash no longer matches are refused.
func (s *Store) OpenMedia(ctx context.Context, blobs MediaBlobs, tenantID, actorID, docID, mediaID string) (Media, []byte, error) {
	if blobs == nil {
		return Media{}, nil, errors.New("document media: no blob store")
	}
	var m Media
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := mediaDocumentTx(ctx, tx, tenantID, actorID, docID, false); err != nil {
			return err
		}
		var err error
		m, err = scanMedia(tx.QueryRow(ctx, `SELECT `+mediaColumns+` FROM document_media WHERE tenant_id=$1 AND document_id=$2 AND id=$3`, tenantID, docID, mediaID))
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrDenied
		}
		return err
	})
	if err != nil {
		return Media{}, nil, err
	}
	content, err := blobs.Get(ctx, tenantID, m.SHA256)
	if err != nil {
		return Media{}, nil, err
	}
	sum := sha256.Sum256(content)
	if hex.EncodeToString(sum[:]) != m.SHA256 {
		return Media{}, nil, ErrMediaCorrupt
	}
	return m, content, nil
}
