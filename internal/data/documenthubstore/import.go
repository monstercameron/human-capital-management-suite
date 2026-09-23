// Quarantined Markdown import for HUB-039: external bytes enter only
// through validation and link remapping, landing as candidate versions
// with provenance on the normal review and deploy path. Nothing imported
// is ever published directly: the version is a candidate until reviewed
// and deployed like any other.
package documenthubstore

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

// ErrImportUnsafe refuses import payloads that never passed quarantine.
var ErrImportUnsafe = errors.New("document import: payload is not admitted")

// ImportInput carries one Markdown import: the bytes, their quarantine
// evidence, the provenance source, and the remapping tables translating
// foreign document and artifact ids to hub ids.
type ImportInput struct {
	DocumentID, ActorID, Title, Locale, Classification, Markdown string
	DocMap, ArtifactMap                                          map[string]string
	QuarantineState, ScannerVersion, ImportedFrom                string
}

// RemapLinks rewrites canonical doc: and artifact: references through the
// remapping tables, preserving pins, blocks and scheme case. Unmapped
// references pass through byte-identical for the review path to judge.
func RemapLinks(markdown string, docMap, artifactMap map[string]string) string {
	return remapScheme(remapScheme(markdown, "doc", docMap), "artifact", artifactMap)
}

func remapScheme(md, scheme string, table map[string]string) string {
	if len(table) == 0 {
		return md
	}
	var out strings.Builder
	i := 0
	for i < len(md) {
		if md[i] == '(' && strings.HasPrefix(strings.ToLower(md[i+1:]), scheme+":") {
			base := i + 1 + len(scheme) + 1
			j := base
			for j < len(md) && md[j] != ')' && md[j] != '#' && md[j] != '@' && md[j] != ' ' && md[j] != '\t' && md[j] != '\n' {
				j++
			}
			if v, ok := table[md[base:j]]; ok {
				out.WriteString(md[i:base])
				out.WriteString(v)
				i = j
				continue
			}
		}
		out.WriteByte(md[i])
		i++
	}
	return out.String()
}

// ImportMarkdown validates, remaps and stores one import as a candidate
// version. Unscanned payloads and oversized bytes are refused before
// anything is stored; a new document is created when no target is named.
func (s *Store) ImportMarkdown(ctx context.Context, tenantID string, in ImportInput) (Version, error) {
	if in.QuarantineState != string(quarantine.Admitted) || strings.TrimSpace(in.ScannerVersion) == "" {
		return Version{}, ErrImportUnsafe
	}
	markdown := RemapLinks(in.Markdown, in.DocMap, in.ArtifactMap)
	if _, err := RenderMarkdown(markdown); err != nil {
		return Version{}, err
	}
	docID := in.DocumentID
	if docID == "" {
		id, err := s.CreateDocument(ctx, tenantID, in.ActorID, "PERSONAL")
		if err != nil {
			return Version{}, err
		}
		docID = id
	}
	note := "imported"
	if strings.TrimSpace(in.ImportedFrom) != "" {
		note = "imported from " + strings.TrimSpace(in.ImportedFrom)
	}
	v, err := s.SubmitCandidate(ctx, tenantID, Version{
		DocumentID: docID, CreatorID: in.ActorID, Title: in.Title, Locale: in.Locale,
		Classification: in.Classification, Markdown: markdown, ChangeNote: note,
	}, "")
	if err != nil {
		return Version{}, err
	}
	if err := s.StoreLinks(ctx, tenantID, docID, v.ID, ExtractLinks(markdown)); err != nil {
		return Version{}, err
	}
	return v, nil
}
