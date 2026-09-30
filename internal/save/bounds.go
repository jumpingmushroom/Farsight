package save

import "fmt"

// checkCount validates a count read from an untrusted save file before it is
// used to size an allocation (e.g. as a slice capacity hint). A corrupt file
// can claim an arbitrary int32 count; without this check a negative count
// panics make(), and a huge count (e.g. 0x7fffffff) allocates gigabytes for
// a few-byte file. remaining is the number of bytes left to read at the
// point the count was read, and minRecordSize is the smallest on-disk size
// of one record, so a count that could not possibly be satisfied by the
// remaining bytes is rejected too.
func checkCount(n int32, remaining, minRecordSize int) (int, error) {
	if n < 0 {
		return 0, fmt.Errorf("save: bad count %d", n)
	}
	if minRecordSize > 0 && int64(n) > int64(remaining)/int64(minRecordSize) {
		return 0, fmt.Errorf("save: bad count %d", n)
	}
	return int(n), nil
}
