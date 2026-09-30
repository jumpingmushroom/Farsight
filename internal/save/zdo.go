package save

import "github.com/jumpingmushroom/farsight/internal/zpkg"

type ZDO struct {
	Pos     [3]float32
	Prefab  int32
	Floats  map[int32]float32
	Vec3s   map[int32][3]float32
	Ints    map[int32]int32
	Longs   map[int32]int64
	Strings map[int32]string
}

const (
	flagConnections = 0x01
	flagFloats      = 0x02
	flagVec3        = 0x04
	flagQuats       = 0x08
	flagInts        = 0x10
	flagLongs       = 0x20
	flagStrings     = 0x40
	flagByteArrays  = 0x80
	flagAnyData     = 0xFF
	flagRotation    = 0x1000
	flagSmallPos    = 0x2000
)

// DecodeZDO reads one ZDO record written by the given world version.
func DecodeZDO(r *zpkg.Reader, version int32, z *ZDO) error {
	*z = ZDO{}
	chunked := version >= VersionChunkedSave
	flags := r.U16()
	if !chunked {
		r.Vec2s() // sector, recomputed by the game from the position
	}
	if flags&flagSmallPos != 0 {
		s := r.Vec2s()
		z.Pos = [3]float32{float32(s[0]), 0, float32(s[1])}
	} else {
		z.Pos = r.Vec3()
	}
	z.Prefab = r.I32()
	if flags&flagRotation != 0 {
		if chunked {
			if r.U16()&0x8000 == 0 {
				r.U16()
			}
		} else {
			r.Vec3()
		}
	}
	if flags&flagAnyData == 0 {
		return r.Err()
	}
	count := func() int {
		if version < VersionNumItems {
			return int(r.U8())
		}
		return r.NumItems()
	}
	if flags&flagConnections != 0 {
		r.U8()
		r.I32()
	}
	if flags&flagFloats != 0 {
		n := count()
		// No capacity hint: n is an untrusted count from the file (defence
		// in depth against a huge count sizing a large allocation).
		z.Floats = make(map[int32]float32)
		for i := 0; i < n; i++ {
			k := r.I32()
			z.Floats[k] = r.F32()
		}
	}
	if flags&flagVec3 != 0 {
		n := count()
		z.Vec3s = make(map[int32][3]float32)
		for i := 0; i < n; i++ {
			k := r.I32()
			z.Vec3s[k] = r.Vec3()
		}
	}
	if flags&flagQuats != 0 {
		n := count()
		for i := 0; i < n; i++ {
			r.I32()
			r.Quat()
		}
	}
	if flags&flagInts != 0 {
		n := count()
		z.Ints = make(map[int32]int32)
		for i := 0; i < n; i++ {
			k := r.I32()
			z.Ints[k] = r.I32()
		}
	}
	if flags&flagLongs != 0 {
		n := count()
		z.Longs = make(map[int32]int64)
		for i := 0; i < n; i++ {
			k := r.I32()
			z.Longs[k] = r.I64()
		}
	}
	if flags&flagStrings != 0 {
		n := count()
		z.Strings = make(map[int32]string)
		for i := 0; i < n; i++ {
			k := r.I32()
			z.Strings[k] = r.Str()
		}
	}
	if flags&flagByteArrays != 0 {
		n := count()
		for i := 0; i < n; i++ {
			r.I32()
			r.ByteArray()
		}
	}
	return r.Err()
}
