package extract

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

// StatesDir is the subdirectory of definitions/legal/packs that holds the
// fifty extracted state drafts. The two LEGAL-001 seed fixtures live beside
// it under seed/, because a hand-authored fixture and a mechanically
// extracted draft are different artifacts with different provenance.
const StatesDir = "states"

// GeneratedFile is one file the generator produced: its repository-relative
// path, its exact bytes, and the findings from extracting it.
type GeneratedFile struct {
	RelPath    string
	Contents   []byte
	Extraction StateExtraction
}

// Generate builds every state draft from the research corpus, the contract
// matrix and the generator's reviewed inputs under root, without writing
// anything. Callers that want the files on disk pass the result to
// [WriteAll]; the regeneration test compares it to what is already
// checked in.
func Generate(root string) ([]GeneratedFile, error) {
	matrix, err := LoadMatrix(root)
	if err != nil {
		return nil, err
	}
	reviewed, err := LoadReviewedOverrides(root)
	if err != nil {
		return nil, err
	}
	out := make([]GeneratedFile, 0, len(States))
	for _, state := range States {
		relPath := path.Join(legal.ResearchDir, state.File)
		file, err := ParseResearchFile(root, relPath)
		if err != nil {
			return nil, err
		}
		extraction, err := ExtractStateWithReviewed(matrix, file, state, reviewed)
		if err != nil {
			return nil, err
		}
		contents, err := legal.MarshalPackDefinition(extraction.Definition)
		if err != nil {
			return nil, fmt.Errorf("extract: rendering %s: %w", state.Code, err)
		}
		out = append(out, GeneratedFile{
			RelPath:    path.Join(legal.PackDefinitionDir, StatesDir, DefinitionFileName(state.Code)),
			Contents:   contents,
			Extraction: extraction,
		})
	}
	return out, nil
}

// DefinitionFileName is the file name for one state's draft.
func DefinitionFileName(code string) string {
	return "us-" + strings.ToLower(code) + ".json"
}

// WriteAll writes generated files under root, creating the directory as
// needed. It writes a file only when its bytes differ, so a regeneration that
// changes nothing leaves fifty untouched timestamps.
func WriteAll(root string, files []GeneratedFile) error {
	for _, f := range files {
		abs := filepath.Join(root, filepath.FromSlash(f.RelPath))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		if existing, err := os.ReadFile(abs); err == nil && string(existing) == string(f.Contents) {
			continue
		}
		if err := os.WriteFile(abs, f.Contents, 0o644); err != nil {
			return err
		}
	}
	return nil
}
