package main

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math/rand"
	"testing"
)

// headerSamples are well-formed images of every kind the preview gate sizes.
func headerSamples(t *testing.T) map[string][]byte {
	t.Helper()
	encode := func(write func(*bytes.Buffer) error) []byte {
		var buffer bytes.Buffer
		if err := write(&buffer); err != nil {
			t.Fatal(err)
		}
		return buffer.Bytes()
	}
	rgba := image.NewNRGBA(image.Rect(0, 0, 37, 21))
	gray := image.NewGray(image.Rect(0, 0, 5, 300))
	gray16 := image.NewGray16(image.Rect(0, 0, 9, 9))
	rgba64 := image.NewRGBA64(image.Rect(0, 0, 3, 2))
	palette := image.NewPaletted(image.Rect(0, 0, 11, 7), color.Palette{color.Black, color.White, color.RGBA{R: 200, A: 255}})
	return map[string][]byte{
		"png rgba":    encode(func(b *bytes.Buffer) error { return png.Encode(b, rgba) }),
		"png gray":    encode(func(b *bytes.Buffer) error { return png.Encode(b, gray) }),
		"png gray16":  encode(func(b *bytes.Buffer) error { return png.Encode(b, gray16) }),
		"png rgba64":  encode(func(b *bytes.Buffer) error { return png.Encode(b, rgba64) }),
		"png palette": encode(func(b *bytes.Buffer) error { return png.Encode(b, palette) }),
		"jpeg color":  encode(func(b *bytes.Buffer) error { return jpeg.Encode(b, rgba, nil) }),
		"jpeg gray":   encode(func(b *bytes.Buffer) error { return jpeg.Encode(b, gray, &jpeg.Options{Quality: 40}) }),
		"gif":         encode(func(b *bytes.Buffer) error { return gif.Encode(b, palette, nil) }),
		"gif animated": encode(func(b *bytes.Buffer) error {
			return gif.EncodeAll(b, &gif.GIF{Image: []*image.Paletted{palette, palette}, Delay: []int{1, 1}})
		}),
	}
}

// For every well-formed image the header gives the size and format the standard
// decoder reports.
func TestChatImageHeaderMatchesTheStandardDecoder(t *testing.T) {
	for name, data := range headerSamples(t) {
		want, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: the sample does not decode: %v", name, err)
		}
		got, ok := chatImageHeaderOf(data)
		if !ok || got.width != want.Width || got.height != want.Height || got.format != format {
			t.Errorf("%s: header = %+v (%v), the standard decoder says %dx%d %s", name, got, ok, want.Width, want.Height, format)
		}
	}
}

// A progressive JPEG announces its frame with a different marker; the size is
// read the same way.
func TestChatImageHeaderReadsProgressiveJPEG(t *testing.T) {
	data := append([]byte(nil), headerSamples(t)["jpeg color"]...)
	index := bytes.Index(data, []byte{0xff, 0xc0})
	if index < 0 {
		t.Fatal("no baseline frame marker in the sample")
	}
	data[index+1] = 0xc2
	want, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("the progressive sample does not decode: %v", err)
	}
	if got, ok := chatImageHeaderOf(data); !ok || got.format != "jpeg" || got.width != want.Width || got.height != want.Height {
		t.Fatalf("progressive header = %+v (%v), want %dx%d", got, ok, want.Width, want.Height)
	}
	// Frame types the standard decoder does not take are not sized.
	for _, marker := range []byte{0xc3, 0xc5, 0xc9, 0xcf} {
		data[index+1] = marker
		if _, ok := chatImageHeaderOf(data); ok {
			t.Errorf("marker %#x was sized", marker)
		}
	}
}

// Damage to an image never makes the header accept what the standard decoder
// refuses (except where the standard decoder looks past the header: a palette
// PNG's palette chunk, a JPEG frame's sampling ratios), and never refuses what
// it accepts.
func TestChatImageHeaderAgreesOnDamagedImages(t *testing.T) {
	random := rand.New(rand.NewSource(14))
	for name, original := range headerSamples(t) {
		for round := 0; round < 700; round++ {
			data := append([]byte(nil), original...)
			switch random.Intn(3) {
			case 0: // flip bytes in the header region
				for flips := 1 + random.Intn(3); flips > 0; flips-- {
					data[random.Intn(min(len(data), 48))] ^= byte(1 + random.Intn(255))
				}
			case 1: // truncate
				data = data[:random.Intn(len(data))]
			default: // flip a byte anywhere
				data[random.Intn(len(data))] ^= byte(1 + random.Intn(255))
			}
			want, format, err := image.DecodeConfig(bytes.NewReader(data))
			got, ok := chatImageHeaderOf(data)
			switch {
			case err == nil && want.Width > 0 && want.Height > 0:
				if !ok || got.width != want.Width || got.height != want.Height || got.format != format {
					t.Fatalf("%s round %d: the standard decoder sizes it %dx%d %s, the header says %+v (%v)\n% x", name, round, want.Width, want.Height, format, got, ok, data[:min(len(data), 48)])
				}
			case ok && (got.format == "gif"):
				t.Fatalf("%s round %d: a GIF the standard decoder refuses (%v) was sized %+v\n% x", name, round, err, got, data[:min(len(data), 48)])
			case ok && got.format == "png" && data[25] != 3:
				t.Fatalf("%s round %d: a PNG the standard decoder refuses (%v) was sized %+v\n% x", name, round, err, got, data[:min(len(data), 48)])
			}
		}
	}
}

func TestChatImageHeaderRefusesWhatItCannotSize(t *testing.T) {
	samples := headerSamples(t)
	zeroGIF := append([]byte(nil), samples["gif"]...)
	zeroGIF[6], zeroGIF[7] = 0, 0
	for name, data := range map[string][]byte{
		"empty":            nil,
		"text":             []byte("not an image at all, but long enough to be read"),
		"png signature":    []byte("\x89PNG\r\n\x1a\n"),
		"jpeg signature":   {0xff, 0xd8},
		"jpeg no frame":    {0xff, 0xd8, 0xff, 0xd9},
		"gif no screen":    []byte("GIF89a"),
		"gif zero width":   zeroGIF,
		"bmp":              []byte("BM\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"),
		"gif with 6 bytes": []byte("GIF87a\x01\x00"),
	} {
		if got, ok := chatImageHeaderOf(data); ok {
			t.Errorf("%s was sized: %+v", name, got)
		}
	}
}
