package explored

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"math/rand/v2"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	m := New()
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 5000; i++ {
		m.Set(r.IntN(Size), r.IntN(Size))
	}
	e := Encode(m, SourceTables)
	if e.Source != SourceTables || e.Cell != 12 || e.Size != 2048 || e.Bits == "" {
		t.Fatalf("encoded header = %+v", e)
	}
	got, err := Decode(e)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bits(), m.Bits()) {
		t.Fatal("round trip changed the bits")
	}
	// The wire form is plain gzip + base64 of the bitset.
	gz, _ := base64.StdEncoding.DecodeString(e.Bits)
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}
	var raw bytes.Buffer
	raw.ReadFrom(zr)
	if !bytes.Equal(raw.Bytes(), m.Bits()) {
		t.Fatal("bits field is not gzip+base64 of the bitset")
	}
}

func gzB64(b []byte) string {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(b)
	zw.Close()
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestDecodeRejectsBadEncodings(t *testing.T) {
	good := Encode(New(), SourceZones)
	cases := map[string]Encoded{
		"source": {Source: "guess", Cell: 12, Size: 2048, Bits: good.Bits},
		"cell":   {Source: SourceZones, Cell: 64, Size: 2048, Bits: good.Bits},
		"size":   {Source: SourceZones, Cell: 12, Size: 1024, Bits: good.Bits},
		"base64": {Source: SourceZones, Cell: 12, Size: 2048, Bits: "!!!"},
		"gzip":   {Source: SourceZones, Cell: 12, Size: 2048, Bits: base64.StdEncoding.EncodeToString([]byte("plain"))},
		"short":  {Source: SourceZones, Cell: 12, Size: 2048, Bits: gzB64(make([]byte, Size*Size/8-1))},
		"long":   {Source: SourceZones, Cell: 12, Size: 2048, Bits: gzB64(make([]byte, Size*Size/8+1))},
	}
	for name, e := range cases {
		if _, err := Decode(e); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := Decode(good); err != nil {
		t.Fatalf("good: %v", err)
	}
}
