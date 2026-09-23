package chatmedia

import (
	"bytes"
	"encoding/binary"
)

// jpegEXIFOrientation reads only IFD0's orientation tag. Malformed metadata
// cannot block an otherwise valid JPEG or trigger an unbounded traversal.
func jpegEXIFOrientation(jpeg []byte) int {
	if len(jpeg) < 4 || jpeg[0] != 0xff || jpeg[1] != 0xd8 {
		return 1
	}
	for pos := 2; pos+4 <= len(jpeg); {
		if jpeg[pos] != 0xff {
			return 1
		}
		marker := jpeg[pos+1]
		if marker == 0xda || marker == 0xd9 {
			return 1
		}
		length := int(binary.BigEndian.Uint16(jpeg[pos+2 : pos+4]))
		if length < 2 || pos+2+length > len(jpeg) {
			return 1
		}
		if marker == 0xe1 {
			segment := jpeg[pos+4 : pos+2+length]
			if bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
				return tiffOrientation(segment[6:])
			}
		}
		pos += 2 + length
	}
	return 1
}

func tiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return 1
	}
	offset := uint64(order.Uint32(tiff[4:8]))
	if offset+2 > uint64(len(tiff)) {
		return 1
	}
	count := uint64(order.Uint16(tiff[offset : offset+2]))
	start := offset + 2
	if count > (uint64(len(tiff))-start)/12 {
		return 1
	}
	for i := uint64(0); i < count; i++ {
		entry := tiff[start+i*12 : start+(i+1)*12]
		if order.Uint16(entry[:2]) != 0x0112 || order.Uint16(entry[2:4]) != 3 || order.Uint32(entry[4:8]) != 1 {
			continue
		}
		value := int(order.Uint16(entry[8:10]))
		if value >= 1 && value <= 8 {
			return value
		}
	}
	return 1
}
