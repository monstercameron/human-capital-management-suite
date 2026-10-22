package main

import (
	"encoding/binary"
	"hash/crc32"
)

// The preview gate only needs an image's declared size, which the first bytes
// of a PNG, JPEG or GIF carry. image.DecodeConfig answers the same question,
// but it can only be linked together with the three decoders (and their
// compression and colour tables), about 190 KB of the browser module that
// nothing in it ever runs. These readers take the size from the headers
// directly. They refuse what the gate cannot size: a wrong signature, a PNG
// header that fails its checksum or describes a colour layout PNG does not
// define, a JPEG frame of a kind the standard decoder does not take, a frame
// header of the wrong length, zero dimensions. Finer faults in a header (a JPEG
// sampling ratio the standard decoder would refuse) and a payload damaged after
// its header are left to the browser's own image decoder, which has always been
// the one that shows the image.

// chatImageHeader is what the preview gate reads from an image's first bytes.
type chatImageHeader struct {
	format string // "png", "jpeg" or "gif", the names image.DecodeConfig reports
	width  int
	height int
}

// chatImageHeaderOf reads the declared size and format of a PNG, JPEG or GIF.
func chatImageHeaderOf(data []byte) (chatImageHeader, bool) {
	var header chatImageHeader
	var ok bool
	switch {
	case len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		header, ok = chatPNGHeader(data)
	case len(data) >= 2 && data[0] == 0xff && data[1] == 0xd8:
		header, ok = chatJPEGHeader(data)
	case len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		header, ok = chatGIFHeader(data)
	}
	if !ok || header.width <= 0 || header.height <= 0 {
		return chatImageHeader{}, false
	}
	return header, true
}

// chatPNGHeader reads the IHDR chunk, which must come first, be 13 bytes, carry
// a valid checksum and describe a colour type and bit depth PNG defines.
func chatPNGHeader(data []byte) (chatImageHeader, bool) {
	const ihdrEnd = 8 + 8 + 13 + 4
	if len(data) < ihdrEnd || binary.BigEndian.Uint32(data[8:12]) != 13 || string(data[12:16]) != "IHDR" {
		return chatImageHeader{}, false
	}
	if crc32.ChecksumIEEE(data[12:29]) != binary.BigEndian.Uint32(data[29:33]) {
		return chatImageHeader{}, false
	}
	width, height := binary.BigEndian.Uint32(data[16:20]), binary.BigEndian.Uint32(data[20:24])
	depth, colorType, compression, filter, interlace := data[24], data[25], data[26], data[27], data[28]
	if width > 1<<31-1 || height > 1<<31-1 || compression != 0 || filter != 0 || interlace > 1 {
		return chatImageHeader{}, false
	}
	valid := false
	switch colorType {
	case 0: // greyscale
		valid = depth == 1 || depth == 2 || depth == 4 || depth == 8 || depth == 16
	case 2, 4, 6: // truecolour, greyscale with alpha, truecolour with alpha
		valid = depth == 8 || depth == 16
	case 3: // palette
		valid = depth == 1 || depth == 2 || depth == 4 || depth == 8
	}
	if !valid {
		return chatImageHeader{}, false
	}
	return chatImageHeader{format: "png", width: int(width), height: int(height)}, true
}

// chatJPEGHeader walks the segments before the frame header and reads the
// frame's size. Only the baseline, extended and progressive Huffman frames are
// taken, which is what the decoder this replaces supports.
func chatJPEGHeader(data []byte) (chatImageHeader, bool) {
	pos := 2
	for pos < len(data) {
		// The standard decoder, like libjpeg, skips stray bytes between segments
		// until it finds a marker.
		for pos < len(data) && data[pos] != 0xff {
			pos++
		}
		for pos < len(data) && data[pos] == 0xff { // fill bytes
			pos++
		}
		if pos >= len(data) {
			return chatImageHeader{}, false
		}
		marker := data[pos]
		pos++
		switch {
		case marker == 0x00:
			return chatImageHeader{}, false
		case marker == 0x01 || (marker >= 0xd0 && marker <= 0xd8): // no length
			continue
		case marker == 0xd9 || marker == 0xda: // end of image, start of scan: no frame came first
			return chatImageHeader{}, false
		}
		if pos+2 > len(data) {
			return chatImageHeader{}, false
		}
		length := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		if length < 2 || pos+length > len(data) {
			return chatImageHeader{}, false
		}
		if marker >= 0xc0 && marker <= 0xcf && marker != 0xc4 && marker != 0xc8 && marker != 0xcc {
			if marker > 0xc2 || length < 8 || data[pos+2] != 8 {
				return chatImageHeader{}, false
			}
			height := int(binary.BigEndian.Uint16(data[pos+3 : pos+5]))
			width := int(binary.BigEndian.Uint16(data[pos+5 : pos+7]))
			components := int(data[pos+7])
			if (components != 1 && components != 3 && components != 4) || length != 8+3*components {
				return chatImageHeader{}, false
			}
			return chatImageHeader{format: "jpeg", width: width, height: height}, true
		}
		pos += length
	}
	return chatImageHeader{}, false
}

// chatGIFHeader reads the logical screen descriptor, and checks that the global
// colour table it announces is there.
func chatGIFHeader(data []byte) (chatImageHeader, bool) {
	if len(data) < 13 {
		return chatImageHeader{}, false
	}
	if data[10]&0x80 != 0 && len(data) < 13+3*(1<<((data[10]&7)+1)) {
		return chatImageHeader{}, false
	}
	return chatImageHeader{
		format: "gif",
		width:  int(binary.LittleEndian.Uint16(data[6:8])),
		height: int(binary.LittleEndian.Uint16(data[8:10])),
	}, true
}
