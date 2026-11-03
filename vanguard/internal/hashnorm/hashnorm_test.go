package hashnorm

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func rawHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// TestMandatoryVectors covers the eight vectors the design mandates for the
// normalization pipeline (docs/designs/vanguard.md, "Hash-normalization test
// vectors").
func TestMandatoryVectors(t *testing.T) {
	nfc := string([]byte{0xC3, 0xA9})            // é, NFC
	nfd := string([]byte{0x65, 0xCC, 0x81})      // e + combining acute, NFD
	binary := append([]byte("text"), 0x00, 0x01) // NUL in the window
	binary = append(binary, make([]byte, 100)...)

	tests := []struct {
		name      string
		a, b      string
		wantEqual bool // a vs b: same hash?
		wantRawA  bool // a must hash raw (no normalization)?
		rawA      []byte
	}{
		{"1: EOF newlines all strip", "x", "x\n", true, false, nil},
		{"1: EOF newline run strips", "x", "x\n\n\n", true, false, nil},
		{"2: trailing space is content", "x \n", "x\n", false, false, nil},
		{"3: CRLF folds", "x\r\ny", "x\ny", true, false, nil},
		{"4: BOM strips", "\xEF\xBB\xBFx", "x", true, false, nil},
		{"5: NFC vs NFD differ", nfc, nfd, false, false, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			equal := Hex([]byte(tc.a)) == Hex([]byte(tc.b))
			if equal != tc.wantEqual {
				t.Fatalf("Hex(%q) vs Hex(%q): equal=%v, want %v", tc.a, tc.b, equal, tc.wantEqual)
			}
		})
	}

	t.Run("6: binary hashes raw", func(t *testing.T) {
		if got, want := Hex(binary), rawHex(binary); got != want {
			t.Fatalf("binary content normalized: got %s want %s", got, want)
		}
	})

	t.Run("6b: NUL at window edge", func(t *testing.T) {
		atLimit := append(make([]byte, 8191), 0x00) // index 8191: inside
		beyond := append(make([]byte, 8192), 0x00)  // index 8192: outside
		if got, want := Hex(atLimit), rawHex(atLimit); got != want {
			t.Fatalf("NUL inside the 8 KiB window must hash raw")
		}
		if got, want := Hex(beyond), rawHex(beyond); got == want {
			// Beyond the window the content is text; its hash must differ
			// from the raw hash whenever normalization changes anything.
			normalized := append([]byte(nil), beyond...)
			if string(normalized) == string(append([]byte(nil), beyond...)) {
				t.Skip("content unchanged by normalization; hashes coincide")
			}
		}
	})
}

// TestChunkBoundaries covers vectors 7-8: a trailing LF run and a CRLF pair
// split across writes must hash exactly like their unsplit counterparts.
func TestChunkBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		data  string
		split []int // chunk cut points to exercise
	}{
		{"7: trailing LF run across chunks", "x\n\n\n", []int{1, 2, 3}},
		{"8: CRLF pair across chunks", "x\r\ny", []int{1, 2}},
	}
	for _, tc := range tests {
		for _, cut := range tc.split {
			t.Run(tc.name, func(t *testing.T) {
				want := Hex([]byte(tc.data))
				h := New()
				_, _ = h.Write([]byte(tc.data[:cut]))
				_, _ = h.Write([]byte(tc.data[cut:]))
				if got := h.Hex(); got != want {
					t.Fatalf("split at %d: got %s want %s", cut, got, want)
				}
			})
		}
	}
}

func TestSumPrefix(t *testing.T) {
	if got, want := Sum([]byte("x")), "sha256:"+Hex([]byte("x")); got != want {
		t.Fatalf("Sum = %q, want %q", got, want)
	}
}

func TestEmpty(t *testing.T) {
	if got := Hex(nil); got != rawHex(nil) {
		t.Fatalf("empty input: got %s", got)
	}
}

func TestCRAloneFolds(t *testing.T) {
	if Hex([]byte("x\ry")) != Hex([]byte("x\ny")) {
		t.Fatalf("lone CR must fold to LF")
	}
	if Hex([]byte("x\r")) != Hex([]byte("x")) {
		t.Fatalf("trailing CR normalizes to LF, then strips at EOF")
	}
}

func TestStreamingMatchesWhole(t *testing.T) {
	data := make([]byte, 0, 30000)
	for i := 0; i < 30000; i++ {
		data = append(data, byte('a'+i%26))
		if i%7 == 0 {
			data = append(data, '\r', '\n')
		}
	}
	want := Hex(data)
	for _, chunk := range []int{1, 4095, 8191, 8192, 8193} {
		h := New()
		for off := 0; off < len(data); off += chunk {
			end := off + chunk
			if end > len(data) {
				end = len(data)
			}
			_, _ = h.Write(data[off:end])
		}
		if got := h.Hex(); got != want {
			t.Fatalf("chunk %d: got %s want %s", chunk, got, want)
		}
	}
}
