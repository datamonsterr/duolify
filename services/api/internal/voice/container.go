package voice

import (
	"bytes"
	"encoding/binary"
	"math"
)

// Container is the recording format the API identified from the bytes themselves, whatever MIME type
// the client reported (a browser may report audio/mp4 and hold Opus, and Safari 18.4+ records WebM).
type Container string

const (
	// WebM is WebM/Opus (Chromium, Firefox, Safari 18.4+). It is sent to providers as `webm`.
	WebM Container = "webm"
	// M4A is MP4/AAC (Safari, iOS and Android WebViews). It is sent to providers as `m4a`.
	M4A Container = "m4a"
)

// mp4Brands are the major brands MediaRecorder and common encoders write for audio-only MP4.
var mp4Brands = map[string]bool{"isom": true, "iso2": true, "iso5": true, "iso6": true, "mp41": true, "mp42": true, "M4A ": true, "M4B ": true, "dash": true, "cmfc": true, "avc1": true}

// sniff identifies the container from the first bytes. ok is false for anything else.
func sniff(b []byte) (c Container, ok bool) {
	// EBML header (WebM and Matroska share it); only the "webm" doctype is accepted.
	if len(b) >= 4 && bytes.Equal(b[:4], []byte{0x1A, 0x45, 0xDF, 0xA3}) {
		head := b[:min(len(b), 64)]
		if bytes.Contains(head, []byte("webm")) {
			return WebM, true
		}
		return "", false
	}
	// ISO base media: size(4) "ftyp" brand(4).
	if len(b) >= 12 && string(b[4:8]) == "ftyp" && mp4Brands[string(b[8:12])] {
		return M4A, true
	}
	return "", false
}

// ebmlVint reads an EBML variable-length integer. With keepMarker the length marker bit stays set
// (element ids); otherwise it is cleared (sizes). unknown reports an all-ones size.
func ebmlVint(b []byte, keepMarker bool) (value uint64, n int, unknown bool) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0, false
	}
	n = 1
	for mask := byte(0x80); b[0]&mask == 0; mask >>= 1 {
		n++
	}
	if n > 8 || len(b) < n {
		return 0, 0, false
	}
	value = uint64(b[0])
	if !keepMarker {
		value &= (1 << (8 - uint(n))) - 1
	}
	allOnes := value == (1<<(8-uint(n)))-1
	for _, c := range b[1:n] {
		value = value<<8 | uint64(c)
		allOnes = allOnes && c == 0xFF
	}
	return value, n, !keepMarker && allOnes
}

// Element ids used to measure a WebM recording (Matroska specification).
const (
	idSegment       = 0x18538067
	idInfo          = 0x1549A966
	idTimecodeScale = 0x2AD7B1
	idDuration      = 0x4489
	idCluster       = 0x1F43B675
	idTimecode      = 0xE7
	idSimpleBlock   = 0xA3
	idBlockGroup    = 0xA0
	idBlock         = 0xA1
)

// webmDurationMs measures a WebM recording in milliseconds from the bytes: the Info Duration when the
// muxer wrote one, else the latest Cluster timecode plus block offset (MediaRecorder streams write no
// Duration and use unknown-size Segment and Cluster elements). ok is false when nothing could be
// measured, so the caller falls back to the declared length.
//
// The scan is flat: master elements it cares about are entered by stepping over their header, every
// other element is skipped by its size, and an unknown-size element that is not a master ends the scan.
func webmDurationMs(b []byte) (ms float64, ok bool) {
	scale := uint64(1_000_000)
	var info, latest float64
	var clusterTime uint64
	var haveInfo, haveBlock bool
	for steps := 0; len(b) > 0 && steps < 100_000; steps++ {
		id, n, _ := ebmlVint(b, true)
		if n == 0 {
			break
		}
		size, m, unknown := ebmlVint(b[n:], false)
		if m == 0 {
			break
		}
		body := b[n+m:]
		switch id {
		case idSegment, idInfo, idCluster, idBlockGroup:
			b = body // enter: the children follow the header
			continue
		}
		if unknown || size > uint64(len(body)) {
			break
		}
		payload := body[:size]
		switch id {
		case idTimecodeScale:
			scale = beUint(payload)
		case idTimecode:
			clusterTime = beUint(payload)
		case idDuration:
			switch len(payload) {
			case 4:
				info, haveInfo = float64(math.Float32frombits(binary.BigEndian.Uint32(payload))), true
			case 8:
				info, haveInfo = math.Float64frombits(binary.BigEndian.Uint64(payload)), true
			}
		case idSimpleBlock, idBlock:
			// track number (vint), int16 timecode relative to the Cluster, flags.
			if _, t, _ := ebmlVint(payload, false); t > 0 && len(payload) >= t+3 {
				rel := int16(binary.BigEndian.Uint16(payload[t : t+2]))
				if at := float64(int64(clusterTime) + int64(rel)); at > latest {
					latest = at
				}
				haveBlock = true
			}
		}
		b = body[size:]
	}
	switch {
	case haveInfo && info > 0:
		return info * float64(scale) / 1e6, true
	case haveBlock:
		return latest * float64(scale) / 1e6, true
	}
	return 0, false
}

func beUint(b []byte) uint64 {
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

// mp4DurationMs reads moov/mvhd for a finished MP4. Fragmented recordings (MediaRecorder) carry a zero
// movie duration, so ok is false and the declared length bounds the request instead.
func mp4DurationMs(b []byte) (ms float64, ok bool) {
	var find func(data []byte, depth int) bool
	find = func(data []byte, depth int) bool {
		for len(data) >= 8 {
			size := uint64(binary.BigEndian.Uint32(data[:4]))
			kind := string(data[4:8])
			header := 8
			if size == 1 && len(data) >= 16 {
				size, header = binary.BigEndian.Uint64(data[8:16]), 16
			} else if size == 0 {
				size = uint64(len(data))
			}
			if size < uint64(header) || size > uint64(len(data)) {
				return false
			}
			body := data[header:size]
			switch kind {
			case "moov":
				if depth < 2 && find(body, depth+1) {
					return true
				}
			case "mvhd":
				var scale, dur uint64
				switch {
				case len(body) >= 20 && body[0] == 0:
					scale, dur = uint64(binary.BigEndian.Uint32(body[12:16])), uint64(binary.BigEndian.Uint32(body[16:20]))
				case len(body) >= 32 && body[0] == 1:
					scale, dur = uint64(binary.BigEndian.Uint32(body[20:24])), binary.BigEndian.Uint64(body[24:32])
				}
				if scale > 0 && dur > 0 && dur != math.MaxUint32 {
					ms = float64(dur) * 1000 / float64(scale)
					return true
				}
				return false
			}
			data = data[size:]
		}
		return false
	}
	if find(b, 0) {
		return ms, true
	}
	return 0, false
}

// measuredDurationMs measures the recording itself when the container allows it.
func measuredDurationMs(c Container, b []byte) (float64, bool) {
	if c == WebM {
		return webmDurationMs(b)
	}
	return mp4DurationMs(b)
}
