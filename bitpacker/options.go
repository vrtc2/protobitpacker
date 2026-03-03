package bitpacker

import (
	"math/bits"

	bitpackerv1 "github.com/vrtc2/protobitpacker/gen/go/bitpacker/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// getFieldOpts returns the (bitpacker.v1.field) extension from a FieldDescriptor.
// Returns a zero-value FieldOptions (not nil) if the extension is absent.
func getFieldOpts(fd protoreflect.FieldDescriptor) *bitpackerv1.FieldOptions {
	opts := fd.Options()
	if opts == nil {
		return &bitpackerv1.FieldOptions{}
	}
	v := proto.GetExtension(opts, bitpackerv1.E_Field)
	if v == nil {
		return &bitpackerv1.FieldOptions{}
	}
	fo, ok := v.(*bitpackerv1.FieldOptions)
	if !ok || fo == nil {
		return &bitpackerv1.FieldOptions{}
	}
	return fo
}

// getOneofOpts returns the (bitpacker.v1.oneof) extension from a OneofDescriptor.
// Returns a zero-value OneofOptions (not nil) if absent.
func getOneofOpts(od protoreflect.OneofDescriptor) *bitpackerv1.OneofOptions {
	opts := od.Options()
	if opts == nil {
		return &bitpackerv1.OneofOptions{}
	}
	v := proto.GetExtension(opts, bitpackerv1.E_Oneof)
	if v == nil {
		return &bitpackerv1.OneofOptions{}
	}
	oo, ok := v.(*bitpackerv1.OneofOptions)
	if !ok || oo == nil {
		return &bitpackerv1.OneofOptions{}
	}
	return oo
}

// minSelectorBits returns ceil(log2(n+1)) — minimum bits to discriminate n oneof cases
// plus the "no field set" (0) case.
//
//	n=0 → 1, n=1 → 1, n=2 → 2, n=3 → 2, n=4 → 3, …
func minSelectorBits(n int) uint32 {
	if n <= 1 {
		return 1
	}
	return uint32(bits.Len(uint(n)))
}
