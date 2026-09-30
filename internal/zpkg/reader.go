// Package zpkg reads and writes the little-endian primitives Valheim's
// ZPackage and .NET BinaryReader/BinaryWriter use in world saves.
package zpkg

import (
	"encoding/binary"
	"errors"
	"math"
)

var ErrShort = errors.New("zpkg: unexpected end of data")

var errBadLength = errors.New("zpkg: bad string length")

type Reader struct {
	b   []byte
	p   int
	err error
}

func NewReader(b []byte) *Reader { return &Reader{b: b} }

func (r *Reader) Err() error { return r.err }
func (r *Reader) Pos() int   { return r.p }
func (r *Reader) Len() int   { return len(r.b) - r.p }

func (r *Reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || r.p+n > len(r.b) {
		r.err = ErrShort
		r.p = len(r.b)
		return nil
	}
	v := r.b[r.p : r.p+n]
	r.p += n
	return v
}

func (r *Reader) Bytes(n int) []byte { return r.take(n) }

func (r *Reader) U8() uint8 {
	b := r.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (r *Reader) Bool() bool { return r.U8() != 0 }

func (r *Reader) U16() uint16 {
	b := r.take(2)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint16(b)
}

func (r *Reader) I16() int16 { return int16(r.U16()) }

func (r *Reader) U32() uint32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}

func (r *Reader) I32() int32 { return int32(r.U32()) }

func (r *Reader) I64() int64 {
	b := r.take(8)
	if b == nil {
		return 0
	}
	return int64(binary.LittleEndian.Uint64(b))
}

func (r *Reader) F32() float32 { return math.Float32frombits(r.U32()) }
func (r *Reader) F64() float64 { return math.Float64frombits(uint64(r.I64())) }

func (r *Reader) Vec3() [3]float32  { return [3]float32{r.F32(), r.F32(), r.F32()} }
func (r *Reader) Quat() [4]float32  { return [4]float32{r.F32(), r.F32(), r.F32(), r.F32()} }
func (r *Reader) Vec2s() [2]int16   { return [2]int16{r.I16(), r.I16()} }
func (r *Reader) ByteArray() []byte { return r.take(int(r.I32())) }

func (r *Reader) Str() string {
	n, shift := 0, 0
	for {
		b := r.U8()
		if r.err != nil {
			return ""
		}
		n |= int(b&0x7F) << shift
		if b&0x80 == 0 {
			break
		}
		shift += 7
		if shift > 28 {
			r.err = errBadLength
			return ""
		}
	}
	return string(r.take(n))
}

func (r *Reader) NumItems() int {
	n := int(r.U8())
	if n&0x80 != 0 {
		n = (n&0x7F)<<8 | int(r.U8())
	}
	return n
}
