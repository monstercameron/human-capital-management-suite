package main

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime"
)

// One preview may use at most half the 64 MiB media cache after decoding.
// RGBA is the conservative charge even when the encoded image has fewer
// channels; the browser may expand it before display.
const chatPreviewMaxDecodedBytes int64 = 32 << 20

// chatPreviewDimensions validates image metadata before browser decode and
// returns its estimated RGBA allocation. It never trusts a declared type or
// a compressed byte count as evidence of a small decoded image. A damaged
// pixel payload is left to the browser's existing image-error fallback.
func chatPreviewDimensions(data []byte, contentType string) (decodedBytes int64, ok bool) {
	if len(data) == 0 || len(data) > chatMediaPreviewMaxBytes {
		return 0, false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || (mediaType != "image/png" && mediaType != "image/jpeg" && mediaType != "image/gif") {
		return 0, false
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 ||
		(mediaType == "image/png" && format != "png") ||
		(mediaType == "image/jpeg" && format != "jpeg") ||
		(mediaType == "image/gif" && format != "gif") {
		return 0, false
	}
	// Divide before multiplying so even forged, extreme dimensions cannot
	// overflow the calculation used by the cache and preview gate.
	const maxPixels = chatPreviewMaxDecodedBytes / 4
	width, height := int64(config.Width), int64(config.Height)
	if width > maxPixels/height {
		return 0, false
	}
	charge := width * height * 4
	if mediaType == "image/gif" {
		frames, valid := chatPreviewGIFFrames(data, chatPreviewMaxDecodedBytes/charge)
		if !valid {
			return 0, false
		}
		return charge * frames, true
	}
	return charge, true
}

// chatPreviewGIFFrames walks GIF blocks without inflating pixel data. The
// frame limit is checked as blocks arrive, so a tiny animated GIF cannot
// impose unbounded decoded memory on the browser preview.
func chatPreviewGIFFrames(data []byte, maxFrames int64) (int64, bool) {
	if len(data) < 13 || (string(data[:6]) != "GIF87a" && string(data[:6]) != "GIF89a") {
		return 0, false
	}
	pos := 13
	if data[10]&0x80 != 0 && !chatGIFSkip(&pos, data, 3*(1<<((data[10]&7)+1))) {
		return 0, false
	}
	var frames int64
	for pos < len(data) {
		switch data[pos] {
		case 0x3b: // trailer
			return frames, frames > 0 && pos == len(data)-1
		case 0x21: // extension label, then length-prefixed data blocks
			if !chatGIFSkip(&pos, data, 2) || !chatGIFSkipSubBlocks(&pos, data) {
				return 0, false
			}
		case 0x2c: // image descriptor, optional local color table, LZW data
			if !chatGIFSkip(&pos, data, 10) {
				return 0, false
			}
			packed := data[pos-1]
			if packed&0x80 != 0 && !chatGIFSkip(&pos, data, 3*(1<<((packed&7)+1))) {
				return 0, false
			}
			if !chatGIFSkip(&pos, data, 1) || !chatGIFSkipSubBlocks(&pos, data) {
				return 0, false
			}
			frames++
			if frames > maxFrames {
				return 0, false
			}
		default:
			return 0, false
		}
	}
	return 0, false
}

func chatGIFSkip(pos *int, data []byte, length int) bool {
	if length < 0 || length > len(data)-*pos {
		return false
	}
	*pos += length
	return true
}

func chatGIFSkipSubBlocks(pos *int, data []byte) bool {
	for *pos < len(data) {
		length := int(data[*pos])
		*pos++
		if length == 0 {
			return true
		}
		if !chatGIFSkip(pos, data, length) {
			return false
		}
	}
	return false
}
