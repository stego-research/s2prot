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

// putVarInt encodes v in the versioned decoder's varint format (7 data bits per
// byte, LSB first, high bit = continue; the value's low bit is the sign).
func putVarInt(v int64) []byte {
	var u uint64
	if v < 0 {
		u = (uint64(-v) << 1) | 1
	} else {
		u = uint64(v) << 1
	}
	var out []byte
	for {
		b := byte(u & 0x7f)
		if u >>= 7; u != 0 {
			out = append(out, b|0x80)
		} else {
			out = append(out, b)
			return out
		}
	}
}

// TestVersionedChoiceBadTagStaysInSync pins the PR #10 review fix: when a
// versioned choice carries an out-of-range tag, the decoder must still skip the
// choice's (self-describing) value, or every later field desyncs. The struct
// here has a choice field (bad tag) followed by an int field; the int must still
// decode correctly.
func TestVersionedChoiceBadTagStaysInSync(t *testing.T) {
	typeInfos := []typeInfo{
		{s2pType: s2pStruct, fields: []field{{name: "c", typeid: 1, tag: 0}, {name: "x", typeid: 2, tag: 1}},
			tagIndex: map[int]int{0: 0, 1: 1}},
		{s2pType: s2pChoice, fields: []field{{name: "only", typeid: 2}}}, // valid tag is only 0
		{s2pType: s2pInt},
	}
	var buf []byte
	buf = append(buf, 0x05)             // struct field-type
	buf = append(buf, putVarInt(2)...)  // field count = 2
	buf = append(buf, putVarInt(0)...)  // field tag 0 (the choice)
	buf = append(buf, 0x03)             // choice field-type
	buf = append(buf, putVarInt(99)...) // out-of-range choice tag
	buf = append(buf, 0x09)             // choice value: vint field-type
	buf = append(buf, putVarInt(42)...) // choice value = 42 (must be skipped)
	buf = append(buf, putVarInt(1)...)  // field tag 1 (the int)
	buf = append(buf, 0x09)             // int field-type (vint)
	buf = append(buf, putVarInt(7)...)  // int value = 7

	d := newVersionedDec(buf, typeInfos)
	s, ok := d.instance(0).(Struct)
	if !ok {
		t.Fatalf("expected a Struct, got %T", d.instance(0))
	}
	if s["x"] != int64(7) {
		t.Fatalf("stream desynced: expected x=7 after a bad-tag choice, got x=%v", s["x"])
	}
	if s["c"] != nil {
		t.Fatalf("expected nil for the out-of-range choice, got %v", s["c"])
	}
}

// TestVersionedNegativeArrayLengthRejected pins the PR #10 review fix that a
// negative versioned length aborts decoding (panic, recovered upstream) rather
// than silently returning and leaving the payload unconsumed.
func TestVersionedNegativeArrayLengthRejected(t *testing.T) {
	typeInfos := []typeInfo{
		{s2pType: s2pArr, typeid: 1},
		{s2pType: s2pInt},
	}
	buf := []byte{0x00}                 // array field-type
	buf = append(buf, putVarInt(-1)...) // negative length

	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic for negative array length, got none")
		}
	}()
	newVersionedDec(buf, typeInfos).instance(0)
}
