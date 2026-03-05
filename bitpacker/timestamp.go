package bitpacker

import (
	bitpackerv1 "github.com/vrtc2/protobitpacker/gen/go/bitpacker/v1"
)

// tsParams extracts encoding parameters for a google.protobuf.Timestamp field.
// bitsN defaults to 64 when not explicitly set in annotations.
// granularity is the number of time units per second (1, 1_000, 1_000_000, or 1_000_000_000).
func tsParams(u *scalarFieldUnit) (bitsN uint32, epochSecs int64, granularity int64, forwardOnly bool, rolling bool) {
	bitsN = u.bits
	if bitsN == 0 {
		bitsN = 64
	}
	granularity = 1
	tso := getFieldOpts(u.fd).GetTimestamp()
	if tso != nil {
		epochSecs = tso.GetEpochSeconds()
		switch tso.GetGranularity() {
		case bitpackerv1.TimestampGranularity_TIMESTAMP_GRANULARITY_MILLISECONDS:
			granularity = 1_000
		case bitpackerv1.TimestampGranularity_TIMESTAMP_GRANULARITY_MICROSECONDS:
			granularity = 1_000_000
		case bitpackerv1.TimestampGranularity_TIMESTAMP_GRANULARITY_NANOSECONDS:
			granularity = 1_000_000_000
		}
		forwardOnly = tso.GetForwardOnly()
		rolling = tso.GetRolling()
	}
	return
}
