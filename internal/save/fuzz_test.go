package save

import (
	"os"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/zpkg"
)

// fuzzFixture reads a committed testdata fixture as fuzz seed material.
func fuzzFixture(f *testing.F, name string) []byte {
	f.Helper()
	b, err := os.ReadFile("testdata/chunked/" + name)
	if err != nil {
		f.Fatal(err)
	}
	return b
}

// FuzzReadChunkFile exercises the .chunk decoder (version + ZDO stream)
// against corrupt-but-arbitrary byte streams: it must never panic or hang,
// only return an error.
func FuzzReadChunkFile(f *testing.F) {
	f.Add(fuzzFixture(f, "small.chunk"))
	f.Add(fuzzFixture(f, "portals.chunk"))
	f.Add([]byte{})
	f.Add([]byte{0x29, 0x00, 0xff, 0xff, 0xff, 0x7f})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = readChunkFile(data, func(*ZDO) {})
	})
}

// FuzzReadChunkIndex exercises the .chunks index decoder, which sizes a
// slice allocation from an untrusted count.
func FuzzReadChunkIndex(f *testing.F) {
	f.Add(fuzzFixture(f, "main.chunks"))
	f.Add([]byte{})
	f.Add([]byte{0x29, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0x7f})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = readChunkIndex(data)
	})
}

// FuzzReadDB2 exercises the gzip-wrapped zone/global-key/location decoder,
// which sizes two slice allocations from untrusted counts and decompresses
// an attacker-controlled gzip stream.
func FuzzReadDB2(f *testing.F) {
	f.Add(fuzzFixture(f, "main.db2"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = readDB2(data)
	})
}

// FuzzReadFWL exercises the .fwl/.fwl2 world-meta decoder.
func FuzzReadFWL(f *testing.F) {
	f.Add(fuzzFixture(f, "main.fwl2"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = readFWL(data)
	})
}

// FuzzDecodeZDO exercises the ZDO record decoder directly against arbitrary
// bytes and versions.
func FuzzDecodeZDO(f *testing.F) {
	small := fuzzFixture(f, "small.chunk")
	f.Add(small, int32(41))
	f.Add(small, int32(37))
	f.Add([]byte{}, int32(41))
	f.Add([]byte{0xff, 0xff}, int32(32))
	f.Fuzz(func(t *testing.T, data []byte, version int32) {
		r := zpkg.NewReader(data)
		var z ZDO
		_ = DecodeZDO(r, version, &z)
	})
}
