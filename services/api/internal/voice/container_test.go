package voice

import "testing"

func TestSniffIdentifiesContainersFromBytesNotMime(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want Container
		ok   bool
	}{
		{"webm", webmStream(1000), WebM, true},
		{"mp4 isom", mp4("isom", 1000, 5000), M4A, true},
		{"mp4 m4a brand", mp4("M4A ", 1000, 5000), M4A, true},
		{"matroska is not webm", append([]byte{0x1A, 0x45, 0xDF, 0xA3, 0x88, 0x42, 0x82, 0x85}, []byte("matroska")...), "", false},
		{"heic", mp4("heic", 1, 1), "", false},
		{"wav", append([]byte("RIFF\x00\x00\x00\x00WAVE"), 0, 0), "", false},
		{"mp3", []byte("ID3\x04\x00\x00\x00\x00\x00\x00\x00\x00"), "", false},
		{"text", []byte("hello world, definitely not audio"), "", false},
		{"empty", nil, "", false},
		{"short", []byte{0x1A, 0x45}, "", false},
	}
	for _, c := range cases {
		got, ok := sniff(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: got (%q,%v), want (%q,%v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestWebMDurationFromClusters(t *testing.T) {
	for _, ms := range []int{500, 4000, 12000, 29980, 45000} {
		got, ok := webmDurationMs(webmStream(ms))
		if !ok || got < float64(ms)-1 || got > float64(ms)+1 {
			t.Errorf("stream %d ms: got %v ok=%v", ms, got, ok)
		}
	}
}

func TestWebMDurationFromInfo(t *testing.T) {
	got, ok := webmDurationMs(webmWithDuration(6200))
	if !ok || got != 6200 {
		t.Fatalf("got %v ok=%v", got, ok)
	}
}

func TestMeasurementDegradesToUnknownNotToAGuess(t *testing.T) {
	for name, in := range map[string][]byte{
		"truncated header": webmHeader()[:6],
		"garbage":          {0x1A, 0x45, 0xDF, 0xA3, 0xFF, 0xFF, 0xFF},
		"empty":            nil,
	} {
		if _, ok := webmDurationMs(in); ok {
			t.Errorf("%s measured something", name)
		}
	}
	if _, ok := mp4DurationMs(mp4("isom", 1000, 0)); ok {
		t.Error("fragmented mp4 has no movie duration")
	}
	if _, ok := mp4DurationMs([]byte("not an mp4 at all")); ok {
		t.Error("garbage measured")
	}
}

func TestMP4DurationFromMovieHeader(t *testing.T) {
	got, ok := mp4DurationMs(mp4("isom", 1000, 6500))
	if !ok || got != 6500 {
		t.Fatalf("got %v ok=%v", got, ok)
	}
	got, ok = mp4DurationMs(mp4("mp42", 44100, 44100*3))
	if !ok || got != 3000 {
		t.Fatalf("got %v ok=%v", got, ok)
	}
}

func TestDurationBuckets(t *testing.T) {
	for ms, want := range map[int]string{500: "lt_2s", 1999: "lt_2s", 2000: "2_5s", 4999: "2_5s", 5000: "5_15s", 14999: "5_15s", 15000: "15_30s", 30000: "15_30s"} {
		if got := DurationBucket(ms); got != want {
			t.Errorf("%d: %s, want %s", ms, got, want)
		}
	}
}

func FuzzMeasureNeverPanics(f *testing.F) {
	f.Add(webmStream(2000))
	f.Add(mp4("isom", 1000, 1000))
	f.Fuzz(func(t *testing.T, in []byte) {
		sniff(in)
		webmDurationMs(in)
		mp4DurationMs(in)
	})
}
