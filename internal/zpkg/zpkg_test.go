package zpkg

import (
	"bytes"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	var w Writer
	w.U8(7)
	w.Bool(true)
	w.U16(0xBEEF)
	w.I16(-2)
	w.U32(0xDEADBEEF)
	w.I32(-5)
	w.I64(-1 << 40)
	w.F32(1.5)
	w.F64(501766.85744524375)
	w.Vec3([3]float32{1, 2, 3})
	w.Quat([4]float32{0, 0, 0, 1})
	w.Vec2s([2]int16{-2, -5})
	w.Str("MuleVikings")
	w.Str(string(bytes.Repeat([]byte("x"), 300))) // two-byte varint length
	w.NumItems(5)
	w.NumItems(300) // two-byte form
	w.ByteArray([]byte{9, 8})

	r := NewReader(w.Bytes())
	if r.U8() != 7 || !r.Bool() || r.U16() != 0xBEEF || r.I16() != -2 || r.U32() != 0xDEADBEEF || r.I32() != -5 || r.I64() != -1<<40 {
		t.Fatal("integer round trip failed")
	}
	if r.F32() != 1.5 || r.F64() != 501766.85744524375 {
		t.Fatal("float round trip failed")
	}
	if r.Vec3() != [3]float32{1, 2, 3} || r.Quat() != [4]float32{0, 0, 0, 1} || r.Vec2s() != [2]int16{-2, -5} {
		t.Fatal("vector round trip failed")
	}
	if r.Str() != "MuleVikings" || len(r.Str()) != 300 {
		t.Fatal("string round trip failed")
	}
	if r.NumItems() != 5 || r.NumItems() != 300 {
		t.Fatal("numitems round trip failed")
	}
	if !bytes.Equal(r.ByteArray(), []byte{9, 8}) {
		t.Fatal("bytearray round trip failed")
	}
	if r.Err() != nil || r.Len() != 0 {
		t.Fatalf("err=%v len=%d", r.Err(), r.Len())
	}
}

func TestShortReadIsSticky(t *testing.T) {
	r := NewReader([]byte{1, 2, 3})
	if r.I32() != 0 || r.Err() != ErrShort {
		t.Fatalf("want ErrShort, got %v", r.Err())
	}
	if r.U8() != 0 || r.Err() != ErrShort {
		t.Fatal("error must be sticky")
	}
}

func TestKnownBytes(t *testing.T) {
	// "is 41" as int16 then int32 count 4, taken from a real chunk file header.
	r := NewReader([]byte{0x29, 0x00, 0x04, 0x00, 0x00, 0x00})
	if r.I16() != 41 || r.I32() != 4 {
		t.Fatal("header decode mismatch")
	}
}
