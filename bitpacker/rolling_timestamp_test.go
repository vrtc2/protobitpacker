package bitpacker_test

import (
	"testing"
	"time"

	"github.com/vrtc2/protobitpacker/bitpacker"
	examplev1 "github.com/vrtc2/protobitpacker/gen/go/bitpacker/v1/example"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var nowSecs = int64(1777451807)

func rollingRoundtrip24(t *testing.T, ts *timestamppb.Timestamp) *timestamppb.Timestamp {
	t.Helper()
	msg := &examplev1.TimestampedEvent{RollingSecs: ts}
	data, err := bitpacker.Pack(msg, bitpacker.OverflowError)
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	got := &examplev1.TimestampedEvent{}
	if err := bitpacker.Unpack(data, got); err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	return got.RollingSecs
}

// TestRollingTimestampWindowBoundary covers the full ±½-window neighbourhood,
// including the case where the encoded timestamp crosses into the next window.
func TestRollingTimestampWindowBoundary(t *testing.T) {
	const windowSize = int64(1) << 24 // 16 777 216 s ≈ 194 days
	const halfWindow = windowSize / 2

	// How many seconds remain until the next window boundary.
	remaining := windowSize - (nowSecs % windowSize)

	cases := []struct {
		name   string
		offset int64 // seconds relative to testNow
	}{
		{"now", 0},
		{"past_1h", -3600},
		{"past_11d", -1_000_000},
		{"future_1h", 3600},
		{"future_11d", 1_000_000},
		// This timestamp crosses into the next rolling window.
		// Without the symmetric-window fix the decoder returns a value one
		// full window earlier than expected.
		{"future_next_window", remaining + 1},
		// One step before the window boundary — must NOT trigger an incorrect advance.
		{"future_window_edge_minus1", remaining - 1},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Only test offsets that are within ½ window of now on BOTH sides;
			// larger past offsets legitimately can't round-trip with rolling encode.
			if tc.offset < -halfWindow || tc.offset > halfWindow {
				t.Skipf("offset %d is outside ±½-window (%d); skipping", tc.offset, halfWindow)
			}

			wantSecs := nowSecs + tc.offset
			ts := timestamppb.New(time.Unix(wantSecs, 0).UTC())
			got := rollingRoundtrip24(t, ts)

			if got.GetSeconds() != wantSecs {
				t.Errorf("offset=%+d: want seconds=%d, got=%d (diff=%d)",
					tc.offset, wantSecs, got.GetSeconds(), got.GetSeconds()-wantSecs)
			}
		})
	}
}

// TestRollingTimestamp16BitWindowBoundary does the same for the 16-bit field
// (window ≈ 18 h). The window is small enough that crossing it in CI is common.
func TestRollingTimestamp16BitWindowBoundary(t *testing.T) {
	const windowSize = int64(1) << 16 // 65 536 s ≈ 18 h
	const halfWindow = windowSize / 2

	remaining := windowSize - (nowSecs % windowSize)

	cases := []struct {
		name   string
		offset int64
	}{
		{"now", 0},
		{"past_1h", -3600},
		{"future_1h", 3600},
		{"future_next_window", remaining + 1},
		{"future_window_edge_minus1", remaining - 1},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.offset < -halfWindow || tc.offset > halfWindow {
				t.Skipf("offset %d is outside ±½-window (%d); skipping", tc.offset, halfWindow)
			}

			wantSecs := nowSecs + tc.offset
			ts := timestamppb.New(time.Unix(wantSecs, 0).UTC())

			msg := &examplev1.TimestampedEvent{RollingSecs_16: ts}
			data, err := bitpacker.Pack(msg, bitpacker.OverflowError)
			if err != nil {
				t.Fatalf("Pack: %v", err)
			}
			got := &examplev1.TimestampedEvent{}
			if err := bitpacker.Unpack(data, got); err != nil {
				t.Fatalf("Unpack: %v", err)
			}

			if got.RollingSecs_16.GetSeconds() != wantSecs {
				t.Errorf("offset=%+d: want seconds=%d, got=%d (diff=%d)",
					tc.offset, wantSecs, got.RollingSecs_16.GetSeconds(), got.RollingSecs_16.GetSeconds()-wantSecs)
			}
		})
	}
}
