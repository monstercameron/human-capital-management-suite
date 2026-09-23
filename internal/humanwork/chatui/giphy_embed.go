package chatui

import "strings"

// GiphyPostEmbed is transient display data resolved from a canonical GIPHY
// page link. MediaURL comes directly from a GIPHY API response and must never
// be persisted in the post body, draft state, or browser storage.
type GiphyPostEmbed struct {
	ID, PageURL, MediaURL, Alt string
}

// ResolveGiphyPostEmbeds matches canonical GIPHY page links in a post against
// validated API results. Unknown links stay ordinary links in the message.
// The API-provided animated preview URL is preserved byte-for-byte.
func ResolveGiphyPostEmbeds(body string, results []GiphyResult) []GiphyPostEmbed {
	byID := make(map[string]GiphyResult, len(results))
	for _, result := range results {
		if !ValidGiphyID(result.ID) || result.Rating != GiphyRating {
			continue
		}
		page, pageID, ok := CanonicalGiphyURL(result.URL)
		if !ok || pageID != result.ID || !validGiphyGIFURL(result.EmbedURL) {
			continue
		}
		result.URL = page
		byID[result.ID] = result
	}

	links := GiphyLinks(body)
	embeds := make([]GiphyPostEmbed, 0, len(links))
	for _, link := range links {
		result, ok := byID[link.ID]
		if !ok {
			continue
		}
		// Use the canonical URL found in the message, not a generated URL or
		// any other provider URL supplied by a caller.
		embeds = append(embeds, GiphyPostEmbed{
			ID: link.ID, PageURL: link.URL, MediaURL: result.EmbedURL,
			Alt: strings.TrimSpace(result.Alt),
		})
	}
	return embeds
}

func validGiphyGIFURL(raw string) bool {
	if !validGiphyMediaURL(raw) {
		return false
	}
	// The chat renderer uses an <img> element, so only accept the animated GIF
	// rendition returned by the API. Video and still renditions stay in picker
	// presentation data and are not used as a posted-message embed.
	return strings.HasSuffix(strings.ToLower(raw), ".gif") || strings.Contains(strings.ToLower(raw), ".gif?")
}
