package bitpacker

import (
	"testing"
)

func TestBitWriterMSBFirst(t *testing.T) {
	// Write 0b10110101 (0xB5) as 8 bits and verify byte value
	w := &bitWriter{}
	w.writeBits(0xB5, 8)
	got := w.bytes()
	if len(got) != 1 || got[0] != 0xB5 {
		t.Errorf("expected [0xB5], got %v", got)
	}
}

func TestBitWriterRoundtrip(t *testing.T) {
	cases := []struct {
		val  uint64
		bits int
	}{
		{0, 1},
		{1, 1},
		{3, 2},
		{7, 3},
		{255, 8},
		{1023, 10},
		{0xDEADBEEF, 32},
		{0xDEADBEEFCAFEBABE, 64},
	}

	for _, tc := range cases {
		w := &bitWriter{}
		w.writeBits(tc.val, tc.bits)

		r := &bitReader{buf: w.bytes()}
		got, err := r.readBits(tc.bits)
		if err != nil {
			t.Errorf("readBits(%d) error: %v", tc.bits, err)
			continue
		}
		if got != tc.val {
			t.Errorf("bits=%d: wrote %d, read %d", tc.bits, tc.val, got)
		}
	}
}

func TestBitWriterMultiField(t *testing.T) {
	// Write two fields: 5-bit value 23 then 3-bit value 5
	// 23 = 0b10111, 5 = 0b101
	// Combined: 10111 101 = 1011 1101 = 0xBD
	w := &bitWriter{}
	w.writeBits(23, 5)
	w.writeBits(5, 3)

	got := w.bytes()
	if len(got) != 1 || got[0] != 0xBD {
		t.Errorf("expected [0xBD], got %v (len=%d)", got, len(got))
	}

	r := &bitReader{buf: got}
	v1, _ := r.readBits(5)
	v2, _ := r.readBits(3)
	if v1 != 23 || v2 != 5 {
		t.Errorf("expected 23,5 got %d,%d", v1, v2)
	}
}

func TestBitWriterRawBytes(t *testing.T) {
	// Write 0xFF 0x00 as raw bytes, then a 4-bit value 0xA
	// Stream: 11111111 00000000 1010xxxx
	w := &bitWriter{}
	w.writeRawBytes([]byte{0xFF, 0x00})
	w.writeBits(0xA, 4)

	b := w.bytes()
	if len(b) != 3 {
		t.Fatalf("expected 3 bytes, got %d", len(b))
	}
	if b[0] != 0xFF {
		t.Errorf("byte[0]: want 0xFF, got 0x%02X", b[0])
	}
	if b[1] != 0x00 {
		t.Errorf("byte[1]: want 0x00, got 0x%02X", b[1])
	}
	// byte[2] = 1010_0000 = 0xA0 (4 data bits + 4 padding zeros)
	if b[2] != 0xA0 {
		t.Errorf("byte[2]: want 0xA0, got 0x%02X", b[2])
	}

	r := &bitReader{buf: b}
	rb, _ := r.readRawBytes(2)
	if rb[0] != 0xFF || rb[1] != 0x00 {
		t.Errorf("readRawBytes: got %v", rb)
	}
	v, _ := r.readBits(4)
	if v != 0xA {
		t.Errorf("readBits(4): want 0xA, got %d", v)
	}
}

func TestBitReaderEOF(t *testing.T) {
	r := &bitReader{buf: []byte{0xAB}}
	// Read 8 bits successfully
	_, err := r.readBits(8)
	if err != nil {
		t.Fatal("unexpected error reading 8 bits from 1-byte buffer")
	}
	// Next read should fail
	_, err = r.readBits(1)
	if err != ErrUnexpectedEOF {
		t.Errorf("expected ErrUnexpectedEOF, got %v", err)
	}
}

func TestBitReaderRemaining(t *testing.T) {
	r := &bitReader{buf: []byte{0x00, 0x00}}
	if r.remaining() != 16 {
		t.Errorf("expected 16 remaining, got %d", r.remaining())
	}
	r.readBits(5)
	if r.remaining() != 11 {
		t.Errorf("expected 11 remaining after reading 5, got %d", r.remaining())
	}
}

func TestBitWriterPaddingZero(t *testing.T) {
	// Write 3 bits (value 0b101 = 5), padding should be 0
	w := &bitWriter{}
	w.writeBits(5, 3)
	b := w.bytes()
	// 101 00000 = 0xA0
	if len(b) != 1 || b[0] != 0xA0 {
		t.Errorf("expected [0xA0], got %v", b)
	}
}
