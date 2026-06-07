package s2prot

import (
	"runtime"
	"testing"
)

// These tests pin two decoder bugs reachable from a crafted replay:
//
//  1. Unbounded slice preallocation. s2pArr/s2pBitArr/s2pBlob size their
//     make() directly from an attacker-controlled length with no check against
//     the bytes that actually remain in the buffer, so a near-empty input can
//     drive an arbitrarily large allocation (here 64 MiB from a 4-byte input;
//     a larger declared length OOM-kills the process — a fatal runtime throw
//     that the recover()s in decodeEvents/newRep/LoadS2ProtRepFromFileBytes
//     cannot catch).
//
//  2. An off-by-one in the bit-packed choice decoder: `tag > len(fields)`
//     should be `tag >= len(fields)` (the versioned decoder gets this right),
//     so tag == len(fields) indexes one past the slice and panics.
//
// They assert the desired post-fix behavior so they double as regression tests.

func allocDelta(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestArrayLengthBoundedByBuffer proves the array length is not validated
// against the remaining input. The element type needs at least 8 bits each, so
// a 4-byte buffer can hold at most 4 elements; a declared length of 4,000,000
// must be rejected before make([]interface{}, length) (64 MiB).
func TestArrayLengthBoundedByBuffer(t *testing.T) {
	typeInfos := []typeInfo{
		{s2pType: s2pArr, typeid: 1, bits: 0, offset64: 4_000_000}, // length comes entirely from offset64
		{s2pType: s2pInt, bits: 8, offset64: 0},                    // element: 8-bit int
	}
	contents := make([]byte, 4)

	const ceiling = 4 << 20 // 4 MiB; the unguarded alloc is ~64 MiB
	delta := allocDelta(func() {
		defer func() { _ = recover() }() // element decode will run off the 4-byte buffer
		d := newBitPackedDec(contents, typeInfos)
		d.instance(0)
	})
	if delta > ceiling {
		t.Fatalf("array decode allocated %d bytes from a 4-byte input (> %d ceiling); "+
			"length not bounded by remaining buffer", delta, ceiling)
	}
}

// TestChoiceTagOffByOne pins the bit-packed choice bounds check. With one field
// and a tag of 1 (== len(fields)), the decoder must treat the tag as invalid
// and return nil rather than indexing fields[1].
func TestChoiceTagOffByOne(t *testing.T) {
	typeInfos := []typeInfo{
		{s2pType: s2pChoice, bits: 0, offset64: 1, fields: []field{{name: "a", typeid: 1}}}, // tag = 1
		{s2pType: s2pNull},
	}
	contents := make([]byte, 4)

	var res interface{}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("choice decode panicked on out-of-range tag (off-by-one: tag > len should be >=): %v", r)
			}
		}()
		d := newBitPackedDec(contents, typeInfos)
		res = d.instance(0)
	}()
	if res != nil {
		t.Fatalf("expected nil for out-of-range choice tag, got %v", res)
	}
}
