package chatmedia

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"time"
)

// Voice messages are what a browser's MediaRecorder produces: Opus in a WebM
// or an Ogg container. The server decodes the container itself and takes the
// duration from the stream; the client's claim of a duration is never read.

const (
	MediaWebM MediaType = "audio/webm"
	MediaOgg  MediaType = "audio/ogg"
)

// MaxVoiceDuration is the longest voice message, the same two minutes the
// composer enforces.
const MaxVoiceDuration = 2 * time.Minute

var errVoiceContainer = errors.New("chatmedia: not an audio-only Opus stream")

// VoiceFacts is what the stream itself says.
type VoiceFacts struct {
	MediaType MediaType
	Duration  time.Duration
}

// InspectVoice validates the whole container and returns its type and playable
// duration. A file that carries video, another codec, more than one stream or
// trailing junk is refused.
func InspectVoice(data []byte) (VoiceFacts, error) {
	switch {
	case bytes.HasPrefix(data, []byte("OggS")):
		d, err := inspectOggOpus(data)
		return VoiceFacts{MediaType: MediaOgg, Duration: d}, err
	case bytes.HasPrefix(data, []byte{0x1a, 0x45, 0xdf, 0xa3}):
		d, err := inspectWebMOpus(data)
		return VoiceFacts{MediaType: MediaWebM, Duration: d}, err
	}
	return VoiceFacts{}, errVoiceContainer
}

var oggTable = func() (t [256]uint32) {
	for i := range t {
		r := uint32(i) << 24
		for range 8 {
			if r&0x80000000 != 0 {
				r = r<<1 ^ 0x04c11db7
			} else {
				r <<= 1
			}
		}
		t[i] = r
	}
	return
}()

func oggChecksum(page []byte) uint32 {
	var crc uint32
	for i, b := range page {
		if i >= 22 && i < 26 {
			b = 0
		}
		crc = crc<<8 ^ oggTable[byte(crc>>24)^b]
	}
	return crc
}

func inspectOggOpus(data []byte) (time.Duration, error) {
	pos, pages := 0, 0
	var serial uint32
	var preSkip uint16
	var last int64 = -1
	for pos < len(data) {
		if len(data)-pos < 27 || string(data[pos:pos+4]) != "OggS" || data[pos+4] != 0 {
			return 0, errVoiceContainer
		}
		segments := int(data[pos+26])
		if len(data)-pos < 27+segments {
			return 0, errVoiceContainer
		}
		size := 0
		for _, n := range data[pos+27 : pos+27+segments] {
			size += int(n)
		}
		end := pos + 27 + segments + size
		if end > len(data) {
			return 0, errVoiceContainer
		}
		page := data[pos:end]
		if binary.LittleEndian.Uint32(page[22:26]) != oggChecksum(page) {
			return 0, errVoiceContainer
		}
		stream := binary.LittleEndian.Uint32(page[14:18])
		body := page[27+segments:]
		if pages == 0 {
			if page[5]&0x02 == 0 || len(body) < 19 || string(body[:8]) != "OpusHead" || body[8] != 1 || body[9] == 0 || body[9] > 2 || body[18] != 0 {
				return 0, errVoiceContainer
			}
			serial, preSkip = stream, binary.LittleEndian.Uint16(body[10:12])
		} else if stream != serial {
			return 0, errVoiceContainer
		}
		if g := int64(binary.LittleEndian.Uint64(page[6:14])); g >= 0 {
			last = g
		}
		pages++
		pos = end
	}
	if pages < 3 || last <= int64(preSkip) {
		return 0, errVoiceContainer
	}
	return time.Duration(last-int64(preSkip)) * time.Second / 48000, nil
}

// EBML element ids used by a WebM voice recording.
const (
	ebmlHeader     = 0x1a45dfa3
	ebmlSegment    = 0x18538067
	ebmlInfo       = 0x1549a966
	ebmlTracks     = 0x1654ae6b
	ebmlTrack      = 0xae
	ebmlTrackType  = 0x83
	ebmlCodecID    = 0x86
	ebmlCluster    = 0x1f43b675
	ebmlTimecode   = 0xe7
	ebmlSimple     = 0xa3
	ebmlGroup      = 0xa0
	ebmlBlock      = 0xa1
	ebmlBlockDur   = 0x9b
	ebmlTimeScale  = 0x2ad7b1
	ebmlDocType    = 0x4282
	ebmlUnknownLen = -1
)

func ebmlID(b []byte) (id uint32, n int) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0
	}
	n = 1
	for mask := byte(0x80); b[0]&mask == 0; mask >>= 1 {
		n++
	}
	if n > 4 || len(b) < n {
		return 0, 0
	}
	for _, c := range b[:n] {
		id = id<<8 | uint32(c)
	}
	return id, n
}

func ebmlSize(b []byte) (size int64, n int) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0
	}
	n = 1
	mask := byte(0x80)
	for ; b[0]&mask == 0; mask >>= 1 {
		n++
	}
	if len(b) < n {
		return 0, 0
	}
	v := uint64(b[0] & (mask - 1))
	allOnes := v == uint64(mask-1)
	for _, c := range b[1:n] {
		v = v<<8 | uint64(c)
		allOnes = allOnes && c == 0xff
	}
	if allOnes {
		return ebmlUnknownLen, n
	}
	if v > math.MaxInt32 {
		return 0, 0
	}
	return int64(v), n
}

type ebmlElement struct {
	id           uint32
	body         []byte
	headerLength int
}

// ebmlNext reads one element from b. An unknown-size master (a live
// recording's Segment and Cluster) extends to the next element that cannot be
// its child, or to the end of b.
func ebmlNext(b []byte, closes func(uint32) bool) (ebmlElement, bool) {
	id, n := ebmlID(b)
	if n == 0 {
		return ebmlElement{}, false
	}
	size, m := ebmlSize(b[n:])
	if m == 0 {
		return ebmlElement{}, false
	}
	start := n + m
	if size == ebmlUnknownLen {
		end := len(b)
		for scan := start; scan < len(b); {
			child, ok := ebmlNext(b[scan:], nil)
			if !ok {
				return ebmlElement{}, false
			}
			if closes != nil && closes(child.id) {
				end = scan
				break
			}
			scan += child.headerLength + len(child.body)
		}
		return ebmlElement{id: id, body: b[start:end], headerLength: start}, true
	}
	if int64(len(b)-start) < size {
		return ebmlElement{}, false
	}
	return ebmlElement{id: id, body: b[start : start+int(size)], headerLength: start}, true
}

func ebmlChildren(b []byte, closes func(uint32) bool, visit func(ebmlElement) bool) bool {
	for len(b) > 0 {
		e, ok := ebmlNext(b, closes)
		if !ok {
			return false
		}
		if !visit(e) {
			return false
		}
		b = b[e.headerLength+len(e.body):]
	}
	return true
}

func ebmlUint(b []byte) uint64 {
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

func inspectWebMOpus(data []byte) (time.Duration, error) {
	scale := uint64(1_000_000)
	tracks, audioOpus := 0, false
	var endNS float64
	segmentClose := func(id uint32) bool { return id == ebmlSegment || id == ebmlHeader }
	clusterClose := func(id uint32) bool {
		return id == ebmlCluster || id == ebmlTracks || id == ebmlInfo || id == 0x1c53bb6b || id == 0x1254c367 || id == 0x114d9b74 || id == 0x1941a469 || id == 0x1043a770
	}
	sawHeader, sawSegment, bad := false, false, false
	top := ebmlChildren(data, segmentClose, func(e ebmlElement) bool {
		switch e.id {
		case ebmlHeader:
			sawHeader = true
			return ebmlChildren(e.body, nil, func(c ebmlElement) bool {
				if c.id == ebmlDocType && string(c.body) != "webm" {
					bad = true
				}
				return true
			})
		case ebmlSegment:
			sawSegment = true
			return ebmlChildren(e.body, clusterClose, func(s ebmlElement) bool {
				switch s.id {
				case ebmlInfo:
					return ebmlChildren(s.body, nil, func(c ebmlElement) bool {
						if c.id == ebmlTimeScale {
							scale = ebmlUint(c.body)
						}
						return true
					})
				case ebmlTracks:
					return ebmlChildren(s.body, nil, func(t ebmlElement) bool {
						if t.id != ebmlTrack {
							return true
						}
						tracks++
						var kind uint64
						codec := ""
						ok := ebmlChildren(t.body, nil, func(c ebmlElement) bool {
							switch c.id {
							case ebmlTrackType:
								kind = ebmlUint(c.body)
							case ebmlCodecID:
								codec = string(c.body)
							}
							return true
						})
						audioOpus = ok && kind == 2 && codec == "A_OPUS"
						return ok
					})
				case ebmlCluster:
					return webmCluster(s.body, scale, &endNS)
				}
				return true
			})
		}
		return true
	})
	if !top || bad || !sawHeader || !sawSegment || tracks != 1 || !audioOpus || scale == 0 || endNS <= 0 {
		return 0, errVoiceContainer
	}
	return time.Duration(endNS), nil
}

func webmCluster(body []byte, scale uint64, endNS *float64) bool {
	var base uint64
	return ebmlChildren(body, nil, func(c ebmlElement) bool {
		switch c.id {
		case ebmlTimecode:
			base = ebmlUint(c.body)
		case ebmlSimple:
			return webmBlock(c.body, 0, base, scale, endNS)
		case ebmlGroup:
			var duration uint64
			var block []byte
			ok := ebmlChildren(c.body, nil, func(g ebmlElement) bool {
				switch g.id {
				case ebmlBlock:
					block = g.body
				case ebmlBlockDur:
					duration = ebmlUint(g.body)
				}
				return true
			})
			return ok && block != nil && webmBlock(block, duration, base, scale, endNS)
		}
		return true
	})
}

func webmBlock(b []byte, durationTicks, base, scale uint64, endNS *float64) bool {
	_, n := ebmlSize(b)
	if n == 0 || len(b) < n+4 || b[n+2]&0x06 != 0 {
		return false
	}
	relative := int16(binary.BigEndian.Uint16(b[n : n+2]))
	ticks := int64(base) + int64(relative)
	if ticks < 0 {
		return false
	}
	frame := b[n+3:]
	if len(frame) == 0 {
		return false
	}
	ns := float64(ticks) * float64(scale)
	if durationTicks > 0 {
		ns += float64(durationTicks) * float64(scale)
	} else {
		ns += opusPacketDuration(frame) * 1e9
	}
	if ns > *endNS {
		*endNS = ns
	}
	return true
}

// opusPacketDuration is the playing time of one packet from its table of
// contents byte (RFC 6716 section 3.1).
func opusPacketDuration(packet []byte) float64 {
	toc := packet[0]
	var frame float64
	switch config := toc >> 3; {
	case config < 12:
		frame = [...]float64{0.010, 0.020, 0.040, 0.060}[config&3]
	case config < 16:
		frame = [...]float64{0.010, 0.020}[config&1]
	default:
		frame = [...]float64{0.0025, 0.005, 0.010, 0.020}[config&3]
	}
	switch toc & 3 {
	case 0:
		return frame
	case 1, 2:
		return 2 * frame
	}
	if len(packet) < 2 {
		return frame
	}
	return float64(packet[1]&0x3f) * frame
}

func voiceBaseType(declared string) (MediaType, bool) {
	switch strings.ToLower(strings.TrimSpace(strings.Split(declared, ";")[0])) {
	case string(MediaWebM):
		return MediaWebM, true
	case string(MediaOgg):
		return MediaOgg, true
	}
	return "", false
}
