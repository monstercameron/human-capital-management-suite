package chatmedia

import (
	"encoding/binary"
	"time"
)

// MaxVoiceMessageDuration bounds recorded audio before it is scanned or
// admitted. The upload byte limit remains an independent bound.
const MaxVoiceMessageDuration = 10 * time.Minute

// inspectAudioDuration validates the complete container and returns its
// playable duration. It is deliberately independent of the filename and
// declared MIME type; Upload compares the inspected container with the type.
func inspectAudioDuration(data []byte, kind MediaType) (time.Duration, bool) {
	var duration time.Duration
	switch kind {
	case MediaWAV:
		duration, _ = inspectWAVDuration(data)
	case MediaMP3:
		duration, _ = inspectMP3Duration(data)
	default:
		return 0, false
	}
	return duration, duration > 0 && duration <= MaxVoiceMessageDuration
}

func inspectWAVDuration(data []byte) (time.Duration, bool) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" || int(binary.LittleEndian.Uint32(data[4:8])) != len(data)-8 {
		return 0, false
	}
	var sampleRate, byteRate, blockAlign uint32
	var format, channels, bits uint16
	var dataBytes uint32
	haveFormat, haveData := false, false
	pos := 12
	for pos+8 <= len(data) {
		name := string(data[pos : pos+4])
		size := binary.LittleEndian.Uint32(data[pos+4 : pos+8])
		pos += 8
		if uint64(size) > uint64(len(data)-pos) {
			return 0, false
		}
		chunk := data[pos : pos+int(size)]
		switch name {
		case "fmt ":
			if haveFormat || len(chunk) < 16 {
				return 0, false
			}
			format = binary.LittleEndian.Uint16(chunk[0:2])
			channels = binary.LittleEndian.Uint16(chunk[2:4])
			sampleRate = binary.LittleEndian.Uint32(chunk[4:8])
			byteRate = binary.LittleEndian.Uint32(chunk[8:12])
			blockAlign = uint32(binary.LittleEndian.Uint16(chunk[12:14]))
			bits = binary.LittleEndian.Uint16(chunk[14:16])
			haveFormat = true
		case "data":
			if haveData {
				return 0, false
			}
			dataBytes, haveData = size, true
		}
		pos += int(size)
		if size&1 != 0 {
			if pos >= len(data) {
				return 0, false
			}
			pos++
		}
	}
	if pos != len(data) || !haveFormat || !haveData || len(data)%2 != 0 || dataBytes == 0 || channels == 0 || channels > 8 || sampleRate == 0 || sampleRate > 384000 {
		return 0, false
	}
	bytesPerSample := uint32((bits + 7) / 8)
	if (format != 1 && format != 3) || (bits != 8 && bits != 16 && bits != 24 && bits != 32) || (format == 3 && bits != 32) || blockAlign != uint32(channels)*bytesPerSample || byteRate != sampleRate*blockAlign || dataBytes%blockAlign != 0 {
		return 0, false
	}
	return time.Duration(uint64(dataBytes) * uint64(time.Second) / uint64(byteRate)), true
}

func inspectMP3Duration(data []byte) (time.Duration, bool) {
	pos := 0
	if len(data) >= 10 && string(data[:3]) == "ID3" {
		// ID3 sizes use four synchsafe bytes. The high bit is reserved.
		for _, b := range data[6:10] {
			if b&0x80 != 0 {
				return 0, false
			}
		}
		size := int(data[6])<<21 | int(data[7])<<14 | int(data[8])<<7 | int(data[9])
		pos = 10 + size
		if data[5]&0x10 != 0 {
			pos += 10
		}
		if pos > len(data) {
			return 0, false
		}
	}
	var samples uint64
	var sampleRate uint32
	frames := 0
	for pos+4 <= len(data) {
		// ID3v1 is the only accepted trailer. Everything else must be MPEG audio.
		if len(data)-pos == 128 && string(data[pos:pos+3]) == "TAG" {
			pos = len(data)
			break
		}
		header := binary.BigEndian.Uint32(data[pos : pos+4])
		if header>>21 != 0x7ff || (header>>17)&3 != 1 {
			return 0, false
		}
		version := (header >> 19) & 3
		if version == 1 {
			return 0, false
		}
		bitrateIndex := (header >> 12) & 15
		rateIndex := (header >> 10) & 3
		if bitrateIndex == 0 || bitrateIndex == 15 || rateIndex == 3 {
			return 0, false
		}
		v1Bitrates := [...]uint32{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
		v2Bitrates := [...]uint32{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
		rateTable := [...]uint32{44100, 48000, 32000}
		rate := rateTable[rateIndex]
		if version == 2 {
			rate /= 2
		} else if version == 0 {
			rate /= 4
		}
		bitrates := v1Bitrates
		samplesPerFrame := uint64(1152)
		coefficient := uint64(144)
		if version != 3 {
			bitrates = v2Bitrates
			samplesPerFrame = 576
			coefficient = 72
		}
		frameLen := int(coefficient*uint64(bitrates[bitrateIndex])*1000/uint64(rate)) + int((header>>9)&1)
		if frameLen < 4 || frameLen > len(data)-pos {
			return 0, false
		}
		if sampleRate != 0 && sampleRate != rate {
			return 0, false
		}
		sampleRate = rate
		samples += samplesPerFrame
		frames++
		pos += frameLen
	}
	if pos != len(data) || frames == 0 || sampleRate == 0 {
		return 0, false
	}
	return time.Duration(samples * uint64(time.Second) / uint64(sampleRate)), true
}
