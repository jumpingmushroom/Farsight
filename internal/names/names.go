// Package names maps Valheim's stable string hashes back to prefab and
// location names.
package names

import (
	_ "embed"
	"strings"
	"unicode/utf16"
)

// StableHash is Valheim's String.GetStableHashCode: two interleaved djb2-xor
// hashes over UTF-16 code units, stopping at a NUL, combined with a
// multiplier. int32 arithmetic wraps exactly like C#'s unchecked int.
func StableHash(s string) int32 {
	u := utf16.Encode([]rune(s))
	var n1, n2 int32 = 5381, 5381
	for i := 0; i < len(u) && u[i] != 0; i += 2 {
		n1 = ((n1 << 5) + n1) ^ int32(u[i])
		if i == len(u)-1 || u[i+1] == 0 {
			break
		}
		n2 = ((n2 << 5) + n2) ^ int32(u[i+1])
	}
	return n1 + n2*1566083941
}

//go:embed names.txt
var raw string

var table = func() map[int32]string {
	m := make(map[int32]string, 8000)
	for _, line := range strings.Split(raw, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			m[StableHash(line)] = line
		}
	}
	return m
}()

func Lookup(h int32) (string, bool) { s, ok := table[h]; return s, ok }
func Name(h int32) string           { return table[h] }
func Count() int                    { return len(table) }
