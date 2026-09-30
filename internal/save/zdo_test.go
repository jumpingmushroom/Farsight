package save

import (
	"os"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/save/savetest"
	"github.com/jumpingmushroom/farsight/internal/zpkg"
)

func readChunkFixture(t *testing.T, name string) []ZDO {
	t.Helper()
	b, err := os.ReadFile("testdata/chunked/" + name)
	if err != nil {
		t.Fatal(err)
	}
	r := zpkg.NewReader(b)
	ver := int32(r.I16())
	n := int(r.I32())
	out := make([]ZDO, n)
	for i := range out {
		if err := DecodeZDO(r, ver, &out[i]); err != nil {
			t.Fatalf("zdo %d: %v", i, err)
		}
	}
	if r.Len() != 0 {
		t.Fatalf("%d trailing bytes", r.Len())
	}
	return out
}

func TestDecodeSmallChunk(t *testing.T) {
	z := readChunkFixture(t, "small.chunk")
	if len(z) != 4 {
		t.Fatalf("got %d zdos, want 4", len(z))
	}
	if names.Name(z[0].Prefab) != "_ZoneCtrl" || z[0].Pos != [3]float32{1024, 0, 0} || z[1].Pos != [3]float32{1024, 0, 64} {
		t.Fatalf("unexpected first zdos: %+v %+v", z[0], z[1])
	}
}

func TestDecodePortalChunk(t *testing.T) {
	z := readChunkFixture(t, "portals.chunk")
	if len(z) != 16 {
		t.Fatalf("got %d zdos, want 16", len(z))
	}
	tags := map[string]int{}
	for _, p := range z {
		if names.Name(p.Prefab) != "portal_wood" {
			t.Errorf("prefab %q, want portal_wood", names.Name(p.Prefab))
		}
		tags[p.Strings[names.StableHash("tag")]]++
	}
	for _, tag := range []string{"Harbor", "Meadow", "Brynhild", "Fjell", "Thorgerdr", "Vale", "Summit", "Stonewatch"} {
		if tags[tag] != 2 {
			t.Errorf("tag %q count %d, want 2", tag, tags[tag])
		}
	}
	first := z[0]
	if first.Pos != [3]float32{-1525.9298095703125, 39.51213836669922, 982.9947509765625} {
		t.Errorf("pos %v", first.Pos)
	}
	if first.Longs[names.StableHash("creator")] != 1000000002 {
		t.Errorf("creator %v", first.Longs)
	}
}

func TestEncodeDecodeRoundTripBothVersions(t *testing.T) {
	in := savetest.ZDO{
		Pos: [3]float32{10.5, 33, -20.25}, Prefab: "bed",
		Floats:  map[string]float32{"health": 80},
		Ints:    map[string]int32{"tamed": 1},
		Longs:   map[string]int64{"owner": 1000000099},
		Strings: map[string]string{"ownerName": "Sigrun"},
	}
	for _, ver := range []int32{37, 41} {
		var w zpkg.Writer
		savetest.EncodeZDO(&w, ver, in)
		var out ZDO
		r := zpkg.NewReader(w.Bytes())
		if err := DecodeZDO(r, ver, &out); err != nil || r.Len() != 0 {
			t.Fatalf("v%d: err=%v trailing=%d", ver, err, r.Len())
		}
		if out.Pos != in.Pos || out.Prefab != names.StableHash("bed") ||
			out.Floats[names.StableHash("health")] != 80 ||
			out.Ints[names.StableHash("tamed")] != 1 ||
			out.Longs[names.StableHash("owner")] != 1000000099 ||
			out.Strings[names.StableHash("ownerName")] != "Sigrun" {
			t.Fatalf("v%d: round trip mismatch: %+v", ver, out)
		}
	}
}

func TestDecodeTruncated(t *testing.T) {
	var out ZDO
	if err := DecodeZDO(zpkg.NewReader([]byte{0x02, 0x00}), 41, &out); err == nil {
		t.Fatal("want error on truncated zdo")
	}
}

func TestDecodeAllSections(t *testing.T) {
	// Test 1: version 41, small pos, rotation with 0x8000, all data sections
	// flags: 0x2000 (small pos) | 0x1000 (rotation) | 0x01 (connections) |
	//        0x04 (vec3) | 0x08 (quats) | 0x10 (ints) | 0x80 (byte arrays)
	{
		var w zpkg.Writer
		flags := uint16(0x2000 | 0x1000 | 0x01 | 0x04 | 0x08 | 0x10 | 0x80)
		w.U16(flags)
		w.Vec2s([2]int16{-12, 34}) // small pos: x=-12, z=34
		w.I32(names.StableHash("testprefab"))
		w.U16(0x8000 | 0x1234) // rotation with 0x8000 set (2 bytes only in v41)
		// connections
		w.U8(1)     // one connection
		w.I32(9999) // connection target
		// vec3s (count 1 via NumItems for v41)
		w.NumItems(1)
		w.I32(names.StableHash("pos_key"))
		w.Vec3([3]float32{1.5, 2.5, 3.5})
		// quats (count 1, discarded)
		w.NumItems(1)
		w.I32(names.StableHash("rot_key"))
		w.Quat([4]float32{0, 0, 0.707, 0.707})
		// ints (count 1)
		w.NumItems(1)
		w.I32(names.StableHash("int_key"))
		w.I32(42)
		// byte arrays (count 1, discarded)
		w.NumItems(1)
		w.I32(names.StableHash("bytes_key"))
		w.ByteArray([]byte{1, 2, 3})

		var out ZDO
		r := zpkg.NewReader(w.Bytes())
		if err := DecodeZDO(r, 41, &out); err != nil {
			t.Fatalf("v41 all sections: %v", err)
		}
		if r.Len() != 0 {
			t.Fatalf("v41 all sections: %d trailing bytes", r.Len())
		}
		if out.Pos != [3]float32{-12, 0, 34} {
			t.Errorf("v41 small pos: got %v, want [-12, 0, 34]", out.Pos)
		}
		if out.Vec3s[names.StableHash("pos_key")] != [3]float32{1.5, 2.5, 3.5} {
			t.Errorf("v41 vec3: got %v", out.Vec3s[names.StableHash("pos_key")])
		}
		if out.Ints[names.StableHash("int_key")] != 42 {
			t.Errorf("v41 int: got %v, want 42", out.Ints[names.StableHash("int_key")])
		}
		if out.Vec3s == nil || len(out.Vec3s) != 1 {
			t.Errorf("v41 vec3s map: got %v", out.Vec3s)
		}
		// quats and byte arrays should be discarded (no maps created)
		if out.Floats != nil || out.Longs != nil || out.Strings != nil {
			t.Errorf("v41 unexpected maps: Floats=%v Longs=%v Strings=%v", out.Floats, out.Longs, out.Strings)
		}
	}

	// Test 2: version 41, rotation first u16 without 0x8000 (4-byte form)
	{
		var w zpkg.Writer
		flags := uint16(0x1000 | 0x10) // rotation + ints
		w.U16(flags)
		w.Vec3([3]float32{5, 6, 7})
		w.I32(names.StableHash("prefab2"))
		w.U16(0x0234) // rotation first u16 without 0x8000
		w.U16(0x5678) // rotation second u16 (follows because first lacks 0x8000)
		w.NumItems(1) // ints count
		w.I32(names.StableHash("int2"))
		w.I32(99)

		var out ZDO
		r := zpkg.NewReader(w.Bytes())
		if err := DecodeZDO(r, 41, &out); err != nil {
			t.Fatalf("v41 4-byte rotation: %v", err)
		}
		if r.Len() != 0 {
			t.Fatalf("v41 4-byte rotation: %d trailing bytes", r.Len())
		}
		if out.Pos != [3]float32{5, 6, 7} {
			t.Errorf("v41 4-byte rotation pos: got %v", out.Pos)
		}
		if out.Ints[names.StableHash("int2")] != 99 {
			t.Errorf("v41 4-byte rotation int: got %v, want 99", out.Ints[names.StableHash("int2")])
		}
	}

	// Test 3: version 37 (legacy), sector Vec2s, full Vec3 pos, Vec3 rotation, NumItems counts
	{
		var w zpkg.Writer
		flags := uint16(0x1000 | 0x10 | 0x04) // rotation + ints + vec3
		w.U16(flags)
		w.Vec2s([2]int16{10, 20}) // sector (legacy, discarded)
		w.Vec3([3]float32{100, 200, 300})
		w.I32(names.StableHash("prefab3"))
		w.Vec3([3]float32{0.1, 0.2, 0.3}) // rotation as Vec3 (legacy)
		// vec3s (count 1)
		w.NumItems(1)
		w.I32(names.StableHash("vec3_key"))
		w.Vec3([3]float32{11, 22, 33})
		// ints (count 1)
		w.NumItems(1)
		w.I32(names.StableHash("int3"))
		w.I32(77)

		var out ZDO
		r := zpkg.NewReader(w.Bytes())
		if err := DecodeZDO(r, 37, &out); err != nil {
			t.Fatalf("v37 legacy: %v", err)
		}
		if r.Len() != 0 {
			t.Fatalf("v37 legacy: %d trailing bytes", r.Len())
		}
		if out.Pos != [3]float32{100, 200, 300} {
			t.Errorf("v37 pos: got %v", out.Pos)
		}
		if out.Vec3s[names.StableHash("vec3_key")] != [3]float32{11, 22, 33} {
			t.Errorf("v37 vec3: got %v", out.Vec3s[names.StableHash("vec3_key")])
		}
		if out.Ints[names.StableHash("int3")] != 77 {
			t.Errorf("v37 int: got %v, want 77", out.Ints[names.StableHash("int3")])
		}
	}

	// Test 4: version 32 (< 33), one-byte counts
	{
		var w zpkg.Writer
		flags := uint16(0x10 | 0x02) // ints + floats
		w.U16(flags)
		w.Vec2s([2]int16{0, 0}) // sector (legacy)
		w.Vec3([3]float32{-1, -2, -3})
		w.I32(names.StableHash("prefab4"))
		// floats with one-byte count
		w.U8(1)
		w.I32(names.StableHash("float_key"))
		w.F32(3.14)
		// ints with one-byte count
		w.U8(2)
		w.I32(names.StableHash("int4a"))
		w.I32(11)
		w.I32(names.StableHash("int4b"))
		w.I32(22)

		var out ZDO
		r := zpkg.NewReader(w.Bytes())
		if err := DecodeZDO(r, 32, &out); err != nil {
			t.Fatalf("v32 one-byte count: %v", err)
		}
		if r.Len() != 0 {
			t.Fatalf("v32 one-byte count: %d trailing bytes", r.Len())
		}
		if out.Pos != [3]float32{-1, -2, -3} {
			t.Errorf("v32 pos: got %v", out.Pos)
		}
		f := out.Floats[names.StableHash("float_key")]
		if f < 3.13 || f > 3.15 {
			t.Errorf("v32 float: got %v", f)
		}
		if out.Ints[names.StableHash("int4a")] != 11 || out.Ints[names.StableHash("int4b")] != 22 {
			t.Errorf("v32 ints: got %v", out.Ints)
		}
	}
}
