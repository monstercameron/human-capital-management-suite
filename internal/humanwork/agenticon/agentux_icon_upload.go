package agenticon

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"mime"
	"net/http"
	"strings"

	"golang.org/x/image/draw"
)

// Upload limits for an owner's own image: the stored proxy is always a small
// re-encoded square, so a hostile or huge source can never reach a surface.
const (
	UploadMaxBytes    = 2 << 20
	UploadMaxPixels   = 16_000_000
	UploadProxySize   = 160
	uploadJPEGQuality = 86
)

// ErrInvalidUpload reports an upload the icon store must refuse without
// touching the stored icon.
var ErrInvalidUpload = errors.New("agenticon: invalid upload")

// DecodeUpload validates a claimed PNG or JPEG and returns the normalized
// square JPEG proxy stored with the agent. The source bytes are never kept.
func DecodeUpload(content []byte, declaredType string) ([]byte, error) {
	if len(content) == 0 || len(content) > UploadMaxBytes {
		return nil, fmt.Errorf("%w: content size %d is outside 1..%d bytes", ErrInvalidUpload, len(content), UploadMaxBytes)
	}
	declared, _, err := mime.ParseMediaType(declaredType)
	if err != nil {
		return nil, fmt.Errorf("%w: media type %q is not allowed", ErrInvalidUpload, declaredType)
	}
	declared = strings.ToLower(declared)
	if declared != "image/png" && declared != "image/jpeg" {
		return nil, fmt.Errorf("%w: media type %q is not allowed", ErrInvalidUpload, declaredType)
	}
	sniffed, _, err := mime.ParseMediaType(http.DetectContentType(content))
	if err != nil || sniffed != declared {
		return nil, fmt.Errorf("%w: declared %s but content sniffed as %s", ErrInvalidUpload, declared, sniffed)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("%w: decode image metadata: %v", ErrInvalidUpload, err)
	}
	if format != "png" && format != "jpeg" {
		return nil, fmt.Errorf("%w: decoded format %q is not allowed", ErrInvalidUpload, format)
	}
	side := config.Width
	if config.Height < side {
		side = config.Height
	}
	if side < 32 || int64(config.Width)*int64(config.Height) > UploadMaxPixels {
		return nil, fmt.Errorf("%w: source dimensions %dx%d are outside the supported bounds", ErrInvalidUpload, config.Width, config.Height)
	}
	source, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("%w: decode pixels: %v", ErrInvalidUpload, err)
	}
	bounds := source.Bounds()
	offsetX := bounds.Min.X + (bounds.Dx()-side)/2
	offsetY := bounds.Min.Y + (bounds.Dy()-side)/2
	proxy := image.NewRGBA(image.Rect(0, 0, UploadProxySize, UploadProxySize))
	draw.ApproxBiLinear.Scale(proxy, proxy.Bounds(), source, image.Rect(offsetX, offsetY, offsetX+side, offsetY+side), draw.Over, nil)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, proxy, &jpeg.Options{Quality: uploadJPEGQuality}); err != nil {
		return nil, fmt.Errorf("agenticon: encode proxy: %w", err)
	}
	return encoded.Bytes(), nil
}
