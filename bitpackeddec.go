/*

Implementation of the bit-packed decoder.

*/

package s2prot

// Bit-packed decoder.
type bitPackedDec struct {
	*bitPackedBuff            // Data source: bit-packed buffer
	typeInfos      []typeInfo // Type descriptors
}

// newBitPackedDec creates a new bit-packed decoder.
func newBitPackedDec(contents []byte, typeInfos []typeInfo) *bitPackedDec {
	return &bitPackedDec{
		bitPackedBuff: &bitPackedBuff{
			contents:  contents,
			bigEndian: true, // All bit-packed decoder uses big endian order
		},
		typeInfos: typeInfos,
	}
}

// instance decodes a value specified by its type id and returns the decoded value.
func (d *bitPackedDec) instance(typeid int) interface{} {
	b := d.bitPackedBuff // Local var for efficiency and more compact code

	ti := &d.typeInfos[typeid] // Pointer to avoid copying the struct

	// Helper function to read an integer specified by the type info
	readInt := func() int64 {
		return ti.offset64 + b.readBits(byte(ti.bits))
	}

	switch ti.s2pType {
	case s2pInt:
		return readInt()
	case s2pStruct:
		s := Struct{}
		order := make([]string, 0, 8)
		orderMap := make(map[string]int)
		add := func(name string, val interface{}) {
			if idx, exists := orderMap[name]; exists {
				// Remove the key from its previous position in order
				order = append(order[:idx], order[idx+1:]...)
				// Update indices in orderMap for keys after the removed index
				for i := idx; i < len(order); i++ {
					orderMap[order[i]] = i
				}
			}
			order = append(order, name)
			orderMap[name] = len(order) - 1
			s[name] = val
		}
		for _, f := range ti.fields {
			if f.isNameParent {
				parent := d.instance(f.typeid)
				if s2, ok := parent.(Struct); ok {
					// Copy s2 into s using parent's order if available
					if po, ok := s2["__order"].([]string); ok {
						for _, k := range po {
							if k == "__order" {
								continue
							}
							add(k, s2[k])
						}
					} else {
						for k, v := range s2 {
							if k == "__order" {
								continue
							}
							add(k, v)
						}
					}
				} else if len(ti.fields) == 1 {
					return parent
				} else {
					add(f.name, parent)
				}
			} else {
				add(f.name, d.instance(f.typeid))
			}
		}
		// store order info for ordered JSON marshalling
		s["__order"] = order
		return s
	case s2pChoice:
		tag := int(readInt())
		// Bounds: tag indexes ti.fields, so a valid tag is in [0, len). The
		// original `tag > len(ti.fields)` was off by one (the versioned decoder
		// uses >=), letting tag == len index one past the slice and panic.
		if tag < 0 || tag >= len(ti.fields) {
			return nil
		}
		f := ti.fields[tag]
		s := Struct{}
		s[f.name] = d.instance(f.typeid)
		s["__order"] = []string{f.name}
		return s
	case s2pArr:
		length := readInt()
		if length < 0 {
			return nil
		}
		// Do not preallocate make([]interface{}, length) from the attacker-
		// controlled length: a crafted input can declare a huge length and OOM-kill
		// the process (a fatal runtime throw recover() cannot catch). Grow with
		// append so memory tracks elements actually decoded; a length that outruns
		// the buffer hits EOF (panic, recovered) after a bounded number of reads.
		arr := make([]interface{}, 0, clampSliceCap(length))
		for i := int64(0); i < length; i++ {
			arr = append(arr, d.instance(ti.typeid))
		}
		return arr
	case s2pBitArr:
		// length may be > 64, so simple readBits() is not enough
		length := int(readInt())
		// length bits are read from the buffer; reject a length no input could back
		// before sizing the (length+7)/8-byte buffer from it.
		if length < 0 || length > b.bitsLeft() {
			panic(errInvalidLength)
		}
		buf := make([]byte, (length+7)/8)    // Number of required bytes
		copy(buf, b.readUnaligned(length/8)) // Number of whole bytes:
		if remaining := byte(length % 8); remaining != 0 {
			buf[len(buf)-1] = byte(b.readBits(remaining))
		}
		return BitArr{Count: length, Data: buf}
	case s2pBlob:
		length := readInt()
		// length bytes are read aligned; reject before make([]byte, length).
		if length < 0 || length > int64(b.bytesLeft()) {
			panic(errInvalidLength)
		}
		return string(b.readAligned(int(length)))
	case s2pOptional:
		if b.readBits1() {
			return d.instance(ti.typeid)
		}
		return nil
	case s2pBool:
		return b.readBits1()
	case s2pFourCC:
		return string(b.readUnaligned(4))
	case s2pNull:
		return nil
	}

	return nil
}
