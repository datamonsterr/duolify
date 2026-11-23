package voice

import (
	"bytes"
	"encoding/binary"
	"math"
	"mime/multipart"
	"os"
	"strconv"
	"testing"
)

// ebml builds one element: id bytes, size vint, payload.
func ebml(id []byte, payload []byte) []byte {
	size := len(payload)
	var vint []byte
	switch {
	case size < 127:
		vint = []byte{0x80 | byte(size)}
	default:
		vint = []byte{0x40 | byte(size>>8), byte(size)}
	}
	return append(append(append([]byte{}, id...), vint...), payload...)
}

func unknownSize(id []byte) []byte {
	return append(append([]byte{}, id...), 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)
}

func uintBytes(v uint64) []byte {
	out := binary.BigEndian.AppendUint64(nil, v)
	return bytes.TrimLeft(out, "\x00")
}

// webmHeader is the EBML header with the `webm` doctype.
func webmHeader() []byte {
	return ebml([]byte{0x1A, 0x45, 0xDF, 0xA3}, ebml([]byte{0x42, 0x82}, []byte("webm")))
}

// webmStream mimics MediaRecorder: unknown-size Segment and Cluster, no Duration, blocks every 20 ms.
func webmStream(lastMs int) []byte {
	b := webmHeader()
	b = append(b, unknownSize([]byte{0x18, 0x53, 0x80, 0x67})...)
	b = append(b, ebml([]byte{0x15, 0x49, 0xA9, 0x66}, ebml([]byte{0x2A, 0xD7, 0xB1}, uintBytes(1_000_000)))...)
	for start := 0; start <= lastMs; start += 5000 {
		b = append(b, unknownSize([]byte{0x1F, 0x43, 0xB6, 0x75})...)
		b = append(b, ebml([]byte{0xE7}, uintBytes(uint64(start)))...)
		for rel := 0; start+rel <= lastMs && rel < 5000; rel += 20 {
			block := append([]byte{0x81}, binary.BigEndian.AppendUint16(nil, uint16(rel))...)
			block = append(block, 0x80, 0xDE, 0xAD)
			b = append(b, ebml([]byte{0xA3}, block)...)
		}
	}
	return b
}

// webmWithDuration writes a finished file: known sizes and an Info Duration (float64, milliseconds).
func webmWithDuration(ms float64) []byte {
	dur := binary.BigEndian.AppendUint64(nil, math.Float64bits(ms))
	info := ebml([]byte{0x15, 0x49, 0xA9, 0x66}, append(ebml([]byte{0x2A, 0xD7, 0xB1}, uintBytes(1_000_000)), ebml([]byte{0x44, 0x89}, dur)...))
	return append(webmHeader(), ebml([]byte{0x18, 0x53, 0x80, 0x67}, info)...)
}

func box(kind string, payload []byte) []byte {
	return append(binary.BigEndian.AppendUint32(nil, uint32(8+len(payload))), append([]byte(kind), payload...)...)
}

// mp4 builds ftyp plus (optionally) moov/mvhd with the given timescale and duration.
func mp4(brand string, timescale, duration uint32) []byte {
	b := box("ftyp", append([]byte(brand), 0, 0, 0, 0))
	if duration == 0 {
		return append(b, box("moov", box("mvhd", make([]byte, 100)))...)
	}
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:], timescale)
	binary.BigEndian.PutUint32(mvhd[16:], duration)
	return append(b, box("moov", box("mvhd", mvhd))...)
}

type upload struct {
	audio      []byte
	purpose    string
	lessonID   string
	durationMs string
	omit       string
}

// multipartBody builds the request body and its Content-Type.
func multipartBody(t testing.TB, u upload) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	field := func(name, value string) {
		if name == u.omit {
			return
		}
		if err := w.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	field("purpose", u.purpose)
	field("lesson_id", u.lessonID)
	field("duration_ms", u.durationMs)
	if u.omit != "audio" {
		part, err := w.CreateFormFile("audio", "recording.bin")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(u.audio)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func itoa(n int) string { return strconv.Itoa(n) }

func readDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out, err
}
