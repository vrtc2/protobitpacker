package bitpacker

// bitWriter writes bits MSB-first into a growing []byte.
//
// Bit layout:
//
//	byte[0]:  bit7(pos=0)  bit6(pos=1)  …  bit0(pos=7)
//	byte[1]:  bit7(pos=8)  bit6(pos=9)  …  bit0(pos=15)
type bitWriter struct {
	buf []byte
	pos int // total bits written
}

// writeBit writes a single bit (0 or 1).
func (w *bitWriter) writeBit(v int) {
	byteIdx := w.pos >> 3
	bitIdx := 7 - (w.pos & 7)
	if byteIdx >= len(w.buf) {
		w.buf = append(w.buf, 0)
	}
	if v != 0 {
		w.buf[byteIdx] |= 1 << uint(bitIdx)
	}
	w.pos++
}

// writeBits writes the low n bits of val, MSB first. n must be 0–64.
func (w *bitWriter) writeBits(val uint64, n int) {
	for i := n - 1; i >= 0; i-- {
		w.writeBit(int((val >> uint(i)) & 1))
	}
}

// writeRawBytes writes each byte of b into the stream, MSB-first within each byte.
func (w *bitWriter) writeRawBytes(b []byte) {
	for _, byt := range b {
		w.writeBits(uint64(byt), 8)
	}
}

// bytes returns the packed byte slice (trailing bits zero-padded to byte boundary).
func (w *bitWriter) bytes() []byte {
	return w.buf
}

// bitReader reads bits MSB-first from a []byte.
type bitReader struct {
	buf []byte
	pos int // total bits consumed
}

// readBit reads a single bit (0 or 1).
func (r *bitReader) readBit() (int, error) {
	byteIdx := r.pos >> 3
	if byteIdx >= len(r.buf) {
		return 0, ErrUnexpectedEOF
	}
	bitIdx := 7 - (r.pos & 7)
	v := int((r.buf[byteIdx] >> uint(bitIdx)) & 1)
	r.pos++
	return v, nil
}

// readBits reads n bits and returns them in the low n bits of a uint64 (MSB first).
func (r *bitReader) readBits(n int) (uint64, error) {
	var val uint64
	for i := 0; i < n; i++ {
		bit, err := r.readBit()
		if err != nil {
			return 0, err
		}
		val = (val << 1) | uint64(bit)
	}
	return val, nil
}

// readRawBytes reads n bytes (n*8 bits) from the stream.
func (r *bitReader) readRawBytes(n int) ([]byte, error) {
	out := make([]byte, n)
	for i := range out {
		v, err := r.readBits(8)
		if err != nil {
			return nil, err
		}
		out[i] = byte(v)
	}
	return out, nil
}

// remaining returns the number of bits remaining (including padding bits).
func (r *bitReader) remaining() int {
	total := len(r.buf) * 8
	if r.pos > total {
		return 0
	}
	return total - r.pos
}
