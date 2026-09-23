package chatmedia

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/jpeg"
	"image/png"

	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
)

const (
	thumbnailMaxEdge = 640
	displayMaxEdge   = 2560
	maxImagePixels   = 16_000_000
	maxImageEdge     = 16_384
)

// imageRenditions runs only after the scanner admits the source. DecodeConfig
// prevents a small compressed upload from allocating an unbounded pixel map.
// Every static image must decode before it is admitted.
func imageRenditions(content []byte, mediaType MediaType) (map[string]Rendition, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return nil, ErrUnsupported
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxImageEdge || config.Height > maxImageEdge || int64(config.Width)*int64(config.Height) > maxImagePixels {
		return nil, fmt.Errorf("%w: image dimensions exceed the decode bound", ErrInvalid)
	}
	if !imageFormatMatches(format, mediaType) {
		return nil, ErrUnsupported
	}
	source, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return nil, ErrUnsupported
	}
	if mediaType == MediaJPEG {
		if orientation := jpegEXIFOrientation(content); orientation != 1 {
			source = orientedImage{source: source, orientation: orientation}
		}
	}
	bounds := source.Bounds()
	result := make(map[string]Rendition, 2)
	for _, variant := range []struct {
		name    string
		maxEdge int
		quality int
	}{{VariantThumbnail, thumbnailMaxEdge, 72}, {VariantDisplay, displayMaxEdge, 85}} {
		width, height := fitImage(bounds.Dx(), bounds.Dy(), variant.maxEdge)
		resized := image.NewNRGBA(image.Rect(0, 0, width, height))
		if mediaType == MediaJPEG {
			stddraw.Draw(resized, resized.Bounds(), image.NewUniform(color.White), image.Point{}, stddraw.Src)
			xdraw.ApproxBiLinear.Scale(resized, resized.Bounds(), source, source.Bounds(), xdraw.Over, nil)
		} else {
			xdraw.ApproxBiLinear.Scale(resized, resized.Bounds(), source, source.Bounds(), xdraw.Src, nil)
		}
		var encoded bytes.Buffer
		renditionType := MediaJPEG
		if resized.Opaque() {
			err = jpeg.Encode(&encoded, resized, &jpeg.Options{Quality: variant.quality})
			// A flat, already small PNG can compress far better as PNG than as
			// JPEG. Compare encodings only in that cheap case.
			if err == nil && mediaType == MediaPNG && len(content) <= 1<<20 {
				var alternate bytes.Buffer
				if pngErr := png.Encode(&alternate, resized); pngErr == nil && alternate.Len() < encoded.Len() {
					encoded = alternate
					renditionType = MediaPNG
				}
			}
		} else {
			renditionType = MediaPNG
			err = png.Encode(&encoded, resized)
		}
		if err != nil {
			return nil, err
		}
		result[variant.name] = Rendition{MediaType: renditionType, Content: encoded.Bytes(), Width: width, Height: height}
	}
	return result, nil
}

func imageFormatMatches(format string, mediaType MediaType) bool {
	switch mediaType {
	case MediaPNG:
		return format == "png"
	case MediaJPEG:
		return format == "jpeg"
	case MediaBMP:
		return format == "bmp"
	}
	return false
}

func fitImage(width, height, maxEdge int) (int, int) {
	if width <= maxEdge && height <= maxEdge {
		return width, height
	}
	if width >= height {
		return maxEdge, max(1, int((int64(height)*int64(maxEdge)+int64(width)/2)/int64(width)))
	}
	return max(1, int((int64(width)*int64(maxEdge)+int64(height)/2)/int64(height))), maxEdge
}

// orientedImage applies the eight TIFF orientation transforms through At, so
// phone photos are displayed upright without allocating another full source
// pixel map. The re-encoded rendition contains no EXIF metadata.
type orientedImage struct {
	source      image.Image
	orientation int
}

func (o orientedImage) ColorModel() color.Model { return o.source.ColorModel() }
func (o orientedImage) Opaque() bool {
	if source, ok := o.source.(interface{ Opaque() bool }); ok {
		return source.Opaque()
	}
	return false
}
func (o orientedImage) Bounds() image.Rectangle {
	b := o.source.Bounds()
	if o.orientation >= 5 {
		return image.Rect(0, 0, b.Dy(), b.Dx())
	}
	return image.Rect(0, 0, b.Dx(), b.Dy())
}
func (o orientedImage) At(x, y int) color.Color {
	b := o.source.Bounds()
	w, h := b.Dx(), b.Dy()
	switch o.orientation {
	case 2:
		x = w - 1 - x
	case 3:
		x, y = w-1-x, h-1-y
	case 4:
		y = h - 1 - y
	case 5:
		x, y = y, x
	case 6:
		x, y = y, h-1-x
	case 7:
		x, y = w-1-y, h-1-x
	case 8:
		x, y = w-1-y, x
	}
	return o.source.At(b.Min.X+x, b.Min.Y+y)
}
