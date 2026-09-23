package main

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func chatTestPreviewPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	return out.Bytes()
}

func chatTestPreviewGIF(t *testing.T, frames int) []byte {
	t.Helper()
	images := make([]*image.Paletted, frames)
	for i := range images {
		images[i] = image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White})
	}
	var out bytes.Buffer
	if err := gif.EncodeAll(&out, &gif.GIF{Image: images, Delay: make([]int, frames)}); err != nil {
		t.Fatalf("encode GIF: %v", err)
	}
	return out.Bytes()
}

func chatTestPreviewJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := jpeg.Encode(&out, image.NewRGBA(image.Rect(0, 0, width, height)), nil); err != nil {
		t.Fatalf("encode JPEG: %v", err)
	}
	return out.Bytes()
}

func TestChatPreviewDimensionsAcceptsValidPNGAndJPEG(t *testing.T) {
	for _, test := range []struct {
		name, contentType string
		data              []byte
		want              int64
	}{
		{"png", "image/png", chatTestPreviewPNG(t, 3, 2), 24},
		{"jpeg", "image/jpeg", chatTestPreviewJPEG(t, 4, 3), 48},
		{"png-parameter", "image/png; name=preview.png", chatTestPreviewPNG(t, 2, 2), 16},
		{"animated-gif", "image/gif", chatTestPreviewGIF(t, 3), 48},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := chatPreviewDimensions(test.data, test.contentType)
			if !ok || got != test.want {
				t.Fatalf("decoded charge = %d, ok=%v; want %d", got, ok, test.want)
			}
		})
	}
}

func TestChatPreviewDimensionsRejectsUnsupportedMismatchAndTruncation(t *testing.T) {
	pngData := chatTestPreviewPNG(t, 3, 2)
	jpegData := chatTestPreviewJPEG(t, 4, 3)
	for _, test := range []struct {
		name, contentType string
		data              []byte
	}{
		{"empty", "image/png", nil},
		{"unsupported", "image/gif", pngData},
		{"gif-mismatch", "image/png", chatTestPreviewGIF(t, 2)},
		{"gif-truncated", "image/gif", chatTestPreviewGIF(t, 2)[:20]},
		{"mismatch", "image/jpeg", pngData},
		{"malformed", "image/png", []byte("not an image")},
		{"truncated-header", "image/png", pngData[:12]},
		{"truncated-jpeg-header", "image/jpeg", jpegData[:8]},
	} {
		t.Run(test.name, func(t *testing.T) {
			if charge, ok := chatPreviewDimensions(test.data, test.contentType); ok || charge != 0 {
				t.Fatalf("unsafe image charged %d, ok=%v", charge, ok)
			}
		})
	}
}

func TestChatPreviewDimensionsGIFCanvasTimesFramesBudget(t *testing.T) {
	manyFrames := chatTestPreviewGIF(t, 9)
	binary.LittleEndian.PutUint16(manyFrames[6:8], 1024)
	binary.LittleEndian.PutUint16(manyFrames[8:10], 1024)
	if charge, ok := chatPreviewDimensions(manyFrames, "image/gif"); ok || charge != 0 {
		t.Fatalf("nine 4 MiB frames charged %d, ok=%v", charge, ok)
	}
	gifData := chatTestPreviewGIF(t, 3)
	// GIF canvas dimensions are little endian. The image blocks remain tiny,
	// making compressed size an intentionally misleading memory signal.
	binary.LittleEndian.PutUint16(gifData[6:8], 2048)
	binary.LittleEndian.PutUint16(gifData[8:10], 2048)
	if charge, ok := chatPreviewDimensions(gifData, "image/gif"); ok || charge != 0 {
		t.Fatalf("three 16 MiB frames charged %d, ok=%v", charge, ok)
	}
	boundary := chatTestPreviewGIF(t, 2)
	binary.LittleEndian.PutUint16(boundary[6:8], 2048)
	binary.LittleEndian.PutUint16(boundary[8:10], 2048)
	if charge, ok := chatPreviewDimensions(boundary, "image/gif"); !ok || charge != chatPreviewMaxDecodedBytes {
		t.Fatalf("two 16 MiB frames charged %d, ok=%v", charge, ok)
	}
	for _, damaged := range [][]byte{boundary[:len(boundary)-1], boundary[:len(boundary)-3], append(append([]byte(nil), boundary...), 0)} {
		if charge, ok := chatPreviewDimensions(damaged, "image/gif"); ok || charge != 0 {
			t.Fatalf("truncated or trailing GIF charged %d, ok=%v", charge, ok)
		}
	}
}

func TestChatPreviewDimensionsRejectsCompressedSmallButOversizedPixels(t *testing.T) {
	data := append([]byte(nil), chatTestPreviewPNG(t, 1, 1)...)
	// The IHDR carries dimensions before the compressed pixel payload.
	// Recompute its CRC to make this a valid header, while leaving a tiny
	// compressed body that would be dangerous to trust by byte length alone.
	binary.BigEndian.PutUint32(data[16:20], 5000)
	binary.BigEndian.PutUint32(data[20:24], 5000)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if charge, ok := chatPreviewDimensions(data, "image/png"); ok || charge != 0 {
		t.Fatalf("25-million-pixel header charged %d, ok=%v", charge, ok)
	}
	binary.BigEndian.PutUint32(data[16:20], 4096)
	binary.BigEndian.PutUint32(data[20:24], 2048)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if charge, ok := chatPreviewDimensions(data, "image/png"); !ok || charge != chatPreviewMaxDecodedBytes {
		t.Fatalf("valid boundary metadata charged %d, ok=%v", charge, ok)
	}
	if charge, ok := chatPreviewDimensions(make([]byte, chatMediaPreviewMaxBytes+1), "image/png"); ok || charge != 0 {
		t.Fatalf("oversized compressed body charged %d, ok=%v", charge, ok)
	}
}
