package agentdocref

import (
	"errors"
	"fmt"
	"strings"
)

const (
	tokenPrefix = "{{doc:"
	tokenSuffix = "}}"
)

var ErrUnknownInstructionDocumentToken = errors.New("unknown agent document instruction token")

type InstructionDocumentTitle struct {
	DocumentID string
	Title      string
	Readable   bool
}

func Token(documentID string) string {
	return tokenPrefix + documentID + tokenSuffix
}

func InstructionDocumentTokens(text string) []string {
	tokens := make([]string, 0)
	seen := map[string]struct{}{}
	for offset := 0; offset < len(text); {
		start := strings.Index(text[offset:], tokenPrefix)
		if start < 0 {
			break
		}
		start += offset
		end := strings.Index(text[start+len(tokenPrefix):], tokenSuffix)
		if end < 0 {
			break
		}
		end += start + len(tokenPrefix)
		documentID := text[start+len(tokenPrefix) : end]
		if validDocumentID(documentID) {
			if _, ok := seen[documentID]; !ok {
				tokens = append(tokens, documentID)
				seen[documentID] = struct{}{}
			}
		}
		offset = end + len(tokenSuffix)
	}
	return tokens
}

func ValidateInstructionDocumentTokens(text string, refs []Reference) error {
	known := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		known[ref.DocumentID] = struct{}{}
	}
	for _, documentID := range InstructionDocumentTokens(text) {
		if _, ok := known[documentID]; !ok {
			return fmt.Errorf("%w: %s", ErrUnknownInstructionDocumentToken, Token(documentID))
		}
	}
	return nil
}

func RenderInstructionDocumentTokens(text string, refs []Reference, titles []InstructionDocumentTitle, unreadable string) string {
	if text == "" {
		return ""
	}
	if unreadable == "" {
		unreadable = "a document you cannot read"
	}
	referenced := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		referenced[ref.DocumentID] = struct{}{}
	}
	titleByDocument := make(map[string]InstructionDocumentTitle, len(titles))
	for _, title := range titles {
		if _, ok := referenced[title.DocumentID]; ok {
			titleByDocument[title.DocumentID] = title
		}
	}
	var out strings.Builder
	for offset := 0; offset < len(text); {
		start := strings.Index(text[offset:], tokenPrefix)
		if start < 0 {
			out.WriteString(text[offset:])
			break
		}
		start += offset
		end := strings.Index(text[start+len(tokenPrefix):], tokenSuffix)
		if end < 0 {
			out.WriteString(text[offset:])
			break
		}
		end += start + len(tokenPrefix)
		documentID := text[start+len(tokenPrefix) : end]
		out.WriteString(text[offset:start])
		if !validDocumentID(documentID) {
			out.WriteString(text[start : end+len(tokenSuffix)])
		} else if title, ok := titleByDocument[documentID]; ok && title.Readable && strings.TrimSpace(title.Title) != "" {
			out.WriteByte('"')
			out.WriteString(title.Title)
			out.WriteByte('"')
		} else {
			out.WriteString(unreadable)
		}
		offset = end + len(tokenSuffix)
	}
	return out.String()
}
