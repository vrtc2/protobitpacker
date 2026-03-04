package bitpacker

import (
	bitpackerv1 "github.com/vrtc2/protobitpacker/gen/go/bitpacker/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// OverflowStrategy controls what happens when a value exceeds its declared bit width.
type OverflowStrategy uint8

const (
	// OverflowError returns a *PackError (current / default behaviour).
	OverflowError OverflowStrategy = iota
	// OverflowModulo wraps the value: v % 2^bits.
	OverflowModulo
	// OverflowClamp saturates to the maximum (or minimum) representable value.
	OverflowClamp
	// OverflowCropLeft keeps the last N bytes/elements when length/count overflows.
	// For numeric types falls back to OverflowClamp.
	OverflowCropLeft
	// OverflowCropRight keeps the first N bytes/elements when length/count overflows.
	// For numeric types falls back to OverflowClamp.
	OverflowCropRight
)

// protoOverflowToGo converts a proto OverflowStrategy enum to the Go type.
func protoOverflowToGo(s bitpackerv1.OverflowStrategy) OverflowStrategy {
	switch s {
	case bitpackerv1.OverflowStrategy_OVERFLOW_STRATEGY_ERROR:
		return OverflowError
	case bitpackerv1.OverflowStrategy_OVERFLOW_STRATEGY_MODULO:
		return OverflowModulo
	case bitpackerv1.OverflowStrategy_OVERFLOW_STRATEGY_CLAMP:
		return OverflowClamp
	case bitpackerv1.OverflowStrategy_OVERFLOW_STRATEGY_CROP_LEFT:
		return OverflowCropLeft
	case bitpackerv1.OverflowStrategy_OVERFLOW_STRATEGY_CROP_RIGHT:
		return OverflowCropRight
	default:
		return OverflowError
	}
}

// effectiveStrategy returns the per-field strategy if set, otherwise the pack-level default.
func effectiveStrategy(fd protoreflect.FieldDescriptor, def OverflowStrategy) OverflowStrategy {
	fo := getFieldOpts(fd)
	if fo.GetOverflow() != bitpackerv1.OverflowStrategy_OVERFLOW_STRATEGY_UNSPECIFIED {
		return protoOverflowToGo(fo.GetOverflow())
	}
	return def
}
