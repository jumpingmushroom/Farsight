// Package worldgen ports Valheim's WorldGenerator (biomes, heights, lakes,
// rivers) and the Unity natives it depends on.
package worldgen

// URandom reproduces UnityEngine.Random (xorshift128) bit for bit.
type URandom struct{ s [4]uint32 }

func NewURandom(seed int32) *URandom {
	s0 := uint32(seed)
	s1 := 1812433253*s0 + 1
	s2 := 1812433253*s1 + 1
	s3 := 1812433253*s2 + 1
	return &URandom{s: [4]uint32{s0, s1, s2, s3}}
}

func (r *URandom) Next() uint32 {
	t := r.s[0] ^ (r.s[0] << 11)
	r.s[0], r.s[1], r.s[2] = r.s[1], r.s[2], r.s[3]
	r.s[3] = r.s[3] ^ (r.s[3] >> 19) ^ t ^ (t >> 8)
	return r.s[3]
}

// Value is Random.value: the low 23 bits over 2^23-1.
func (r *URandom) Value() float32 {
	return float32(r.Next()&0x7FFFFF) / float32(0x7FFFFF)
}

// RangeFloat is Random.Range(float, float). Unity computes t*(min-max)+max.
func (r *URandom) RangeFloat(min, max float32) float32 {
	t := r.Value()
	return t*(min-max) + max
}

// RangeInt is Random.Range(int, int), max-exclusive, with Unity's plain
// modulo (no rejection sampling).
func (r *URandom) RangeInt(min, max int32) int32 {
	v := int64(r.Next())
	lo, hi := int64(min), int64(max)
	switch {
	case hi > lo:
		return int32(lo + v%(hi-lo))
	case hi < lo:
		return int32(lo - v%(hi-lo))
	default:
		return min
	}
}
