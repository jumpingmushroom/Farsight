package zpkg

import (
	"encoding/binary"
	"math"
)

// Writer is the inverse of Reader. It exists for tests and test fixtures.
type Writer struct{ b []byte }

func (w *Writer) Bytes() []byte { return w.b }
func (w *Writer) Raw(b []byte)  { w.b = append(w.b, b...) }
func (w *Writer) U8(v uint8)    { w.b = append(w.b, v) }
func (w *Writer) U16(v uint16)  { w.b = binary.LittleEndian.AppendUint16(w.b, v) }
func (w *Writer) I16(v int16)   { w.U16(uint16(v)) }
func (w *Writer) U32(v uint32)  { w.b = binary.LittleEndian.AppendUint32(w.b, v) }
func (w *Writer) I32(v int32)   { w.U32(uint32(v)) }
func (w *Writer) I64(v int64)   { w.b = binary.LittleEndian.AppendUint64(w.b, uint64(v)) }
func (w *Writer) F32(v float32) { w.U32(math.Float32bits(v)) }
func (w *Writer) F64(v float64) { w.I64(int64(math.Float64bits(v))) }

func (w *Writer) Bool(v bool) {
	if v {
		w.U8(1)
	} else {
		w.U8(0)
	}
}

func (w *Writer) Vec3(v [3]float32) { w.F32(v[0]); w.F32(v[1]); w.F32(v[2]) }
func (w *Writer) Quat(v [4]float32) { w.F32(v[0]); w.F32(v[1]); w.F32(v[2]); w.F32(v[3]) }
func (w *Writer) Vec2s(v [2]int16)  { w.I16(v[0]); w.I16(v[1]) }

func (w *Writer) Str(s string) {
	n := len(s)
	for n >= 0x80 {
		w.U8(byte(n) | 0x80)
		n >>= 7
	}
	w.U8(byte(n))
	w.b = append(w.b, s...)
}

func (w *Writer) NumItems(n int) {
	if n < 0x80 {
		w.U8(uint8(n))
		return
	}
	w.U8(uint8(n>>8) | 0x80)
	w.U8(uint8(n))
}

func (w *Writer) ByteArray(b []byte) { w.I32(int32(len(b))); w.Raw(b) }
