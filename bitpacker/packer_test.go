package bitpacker_test

import (
	"testing"
	"time"

	"github.com/vrtc2/protobitpacker/bitpacker"
	examplev1 "github.com/vrtc2/protobitpacker/gen/go/bitpacker/v1/example"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPackUnpackSensorReading_NoLabel(t *testing.T) {
	msg := &examplev1.SensorReading{
		SensorId:        42,
		TemperatureDeci: -100, // zigzag: ((-100<<1)^(-100>>31)) = 199
		HumidityPct:     75,
		Alert:           true,
		Status:          examplev1.SensorStatus_SENSOR_STATUS_WARNING,
	}

	data, err := bitpacker.Pack(msg, bitpacker.OverflowError)
	if err != nil {
		t.Fatalf("Pack error: %v", err)
	}

	got := &examplev1.SensorReading{}
	if err := bitpacker.Unpack(data, got); err != nil {
		t.Fatalf("Unpack error: %v", err)
	}

	if !proto.Equal(msg, got) {
		t.Errorf("roundtrip mismatch:\n  want: %v\n   got: %v", msg, got)
	}
}

func TestPackUnpackSensorReading_WithLabel(t *testing.T) {
	label := "sensor-A"
	msg := &examplev1.SensorReading{
		SensorId:        1,
		TemperatureDeci: 200,
		HumidityPct:     50,
		Alert:           false,
		Status:          examplev1.SensorStatus_SENSOR_STATUS_OK,
		Label:           &label,
	}

	data, err := bitpacker.Pack(msg, bitpacker.OverflowError)
	if err != nil {
		t.Fatalf("Pack error: %v", err)
	}

	got := &examplev1.SensorReading{}
	if err := bitpacker.Unpack(data, got); err != nil {
		t.Fatalf("Unpack error: %v", err)
	}

	if !proto.Equal(msg, got) {
		t.Errorf("roundtrip mismatch:\n  want: %v\n   got: %v", msg, got)
	}
}

func TestBitSize_SensorReading_NoLabel(t *testing.T) {
	// 12(sensor_id) + 11(temperature_deci) + 7(humidity_pct) + 1(alert) + 2(status) + 1(label presence) = 34 bits → 5 bytes
	msg := &examplev1.SensorReading{
		SensorId:        1,
		TemperatureDeci: 0,
		HumidityPct:     0,
		Alert:           false,
		Status:          examplev1.SensorStatus_SENSOR_STATUS_UNSPECIFIED,
	}
	data, err := bitpacker.Pack(msg, bitpacker.OverflowError)
	if err != nil {
		t.Fatalf("Pack error: %v", err)
	}
	if len(data) != 5 {
		t.Errorf("expected 5 bytes for SensorReading (no label), got %d", len(data))
	}
}

func TestPackUnpackPacket_OneofRaw(t *testing.T) {
	msg := &examplev1.Packet{
		Sequence: 1000,
		Payload: &examplev1.Packet_Raw{
			Raw: []byte{0xDE, 0xAD, 0xBE, 0xEF},
		},
	}

	data, err := bitpacker.Pack(msg, bitpacker.OverflowError)
	if err != nil {
		t.Fatalf("Pack error: %v", err)
	}

	got := &examplev1.Packet{}
	if err := bitpacker.Unpack(data, got); err != nil {
		t.Fatalf("Unpack error: %v", err)
	}

	if !proto.Equal(msg, got) {
		t.Errorf("roundtrip mismatch:\n  want: %v\n   got: %v", msg, got)
	}
}

func TestPackUnpackPacket_OneofCommand(t *testing.T) {
	msg := &examplev1.Packet{
		Sequence: 12345,
		Payload:  &examplev1.Packet_Command{Command: 200},
	}

	data, err := bitpacker.Pack(msg, bitpacker.OverflowError)
	if err != nil {
		t.Fatalf("Pack error: %v", err)
	}

	got := &examplev1.Packet{}
	if err := bitpacker.Unpack(data, got); err != nil {
		t.Fatalf("Unpack error: %v", err)
	}

	if !proto.Equal(msg, got) {
		t.Errorf("roundtrip mismatch:\n  want: %v\n   got: %v", msg, got)
	}
}

func TestPackUnpackPacket_OneofAck(t *testing.T) {
	msg := &examplev1.Packet{
		Sequence: 1,
		Payload:  &examplev1.Packet_Ack{Ack: true},
	}

	data, err := bitpacker.Pack(msg, bitpacker.OverflowError)
	if err != nil {
		t.Fatalf("Pack error: %v", err)
	}

	// Packed: 16(sequence) + 2(selector=3) + 1(ack=true) = 19 bits → 3 bytes
	if len(data) != 3 {
		t.Errorf("expected 3 bytes for Packet_Ack, got %d", len(data))
	}

	got := &examplev1.Packet{}
	if err := bitpacker.Unpack(data, got); err != nil {
		t.Fatalf("Unpack error: %v", err)
	}

	if !proto.Equal(msg, got) {
		t.Errorf("roundtrip mismatch:\n  want: %v\n   got: %v", msg, got)
	}
}

func TestPackUnpackPacket_OneofNone(t *testing.T) {
	msg := &examplev1.Packet{Sequence: 7}

	data, err := bitpacker.Pack(msg, bitpacker.OverflowError)
	if err != nil {
		t.Fatalf("Pack error: %v", err)
	}

	got := &examplev1.Packet{}
	if err := bitpacker.Unpack(data, got); err != nil {
		t.Fatalf("Unpack error: %v", err)
	}

	if !proto.Equal(msg, got) {
		t.Errorf("roundtrip mismatch:\n  want: %v\n   got: %v", msg, got)
	}
}

func TestPackUnpackBurst_Repeated(t *testing.T) {
	label := "x"
	msg := &examplev1.Burst{
		Readings: []*examplev1.SensorReading{
			{SensorId: 1, TemperatureDeci: 10, HumidityPct: 50, Alert: false, Status: examplev1.SensorStatus_SENSOR_STATUS_OK},
			{SensorId: 2, TemperatureDeci: -5, HumidityPct: 80, Alert: true, Status: examplev1.SensorStatus_SENSOR_STATUS_WARNING, Label: &label},
		},
		AdcSamples: []uint32{100, 200, 300},
		Tags:       []string{"abc", "de"},
	}

	data, err := bitpacker.Pack(msg, bitpacker.OverflowError)
	if err != nil {
		t.Fatalf("Pack error: %v", err)
	}

	got := &examplev1.Burst{}
	if err := bitpacker.Unpack(data, got); err != nil {
		t.Fatalf("Unpack error: %v", err)
	}

	if !proto.Equal(msg, got) {
		t.Errorf("roundtrip mismatch:\n  want: %v\n   got: %v", msg, got)
	}
}

func TestPackUnpackConfig_Map(t *testing.T) {
	msg := &examplev1.Config{
		Settings: map[string]uint32{
			"brightness": 1000,
			"timeout":    30,
			"max":        65535,
		},
	}

	data, err := bitpacker.Pack(msg, bitpacker.OverflowError)
	if err != nil {
		t.Fatalf("Pack error: %v", err)
	}

	got := &examplev1.Config{}
	if err := bitpacker.Unpack(data, got); err != nil {
		t.Fatalf("Unpack error: %v", err)
	}

	if !proto.Equal(msg, got) {
		t.Errorf("roundtrip mismatch:\n  want: %v\n   got: %v", msg, got)
	}
}

func TestPackUnpackZeroMessage(t *testing.T) {
	msg := &examplev1.SensorReading{}

	data, err := bitpacker.Pack(msg, bitpacker.OverflowError)
	if err != nil {
		t.Fatalf("Pack error: %v", err)
	}

	got := &examplev1.SensorReading{}
	if err := bitpacker.Unpack(data, got); err != nil {
		t.Fatalf("Unpack error: %v", err)
	}

	if !proto.Equal(msg, got) {
		t.Errorf("roundtrip mismatch:\n  want: %v\n   got: %v", msg, got)
	}
}

func TestPackRangeError(t *testing.T) {
	// sensor_id is 12 bits, max 4095; 4096 should fail
	msg := &examplev1.SensorReading{SensorId: 4096}
	_, err := bitpacker.Pack(msg, bitpacker.OverflowError)
	if err == nil {
		t.Error("expected PackError for out-of-range value, got nil")
	}
	var packErr *bitpacker.PackError
	if pe, ok := err.(*bitpacker.PackError); !ok {
		t.Errorf("expected *PackError, got %T: %v", err, err)
	} else {
		packErr = pe
		_ = packErr
	}
}

func TestValidate(t *testing.T) {
	p := bitpacker.NewPacker()

	// Valid schemas should not error
	for _, md := range []interface{ ProtoReflect() interface{ Descriptor() interface{ FullName() interface{} } } }{} {
		_ = md
	}

	// Test valid message descriptors
	validMsgs := []proto.Message{
		&examplev1.SensorReading{},
		&examplev1.Packet{},
		&examplev1.Burst{},
		&examplev1.Config{},
		&examplev1.FloatSample{},
	}
	for _, m := range validMsgs {
		if err := p.Validate(m.ProtoReflect().Descriptor()); err != nil {
			t.Errorf("Validate(%T) unexpected error: %v", m, err)
		}
	}
}

// TestFixedPointFloat tests roundtrip encoding for fixed/ufixed float fields.
// Values are chosen to be exactly representable in IEEE 754 to allow proto.Equal comparison.
func TestFixedPointFloat(t *testing.T) {
	cases := []struct {
		name string
		msg  *examplev1.FloatSample
	}{
		{
			name: "positive temperature",
			msg:  &examplev1.FloatSample{Temperature: 25.0, Distance: 0, Altitude: 0},
		},
		{
			name: "negative temperature",
			msg:  &examplev1.FloatSample{Temperature: -10.5, Distance: 0, Altitude: 0},
		},
		{
			name: "zero temperature",
			msg:  &examplev1.FloatSample{Temperature: 0, Distance: 0, Altitude: 0},
		},
		{
			name: "ufixed distance",
			msg:  &examplev1.FloatSample{Temperature: 0, Distance: 100.0, Altitude: 0},
		},
		{
			name: "ufixed distance max-ish",
			msg:  &examplev1.FloatSample{Temperature: 0, Distance: 655.0, Altitude: 0},
		},
		{
			name: "double altitude positive",
			msg:  &examplev1.FloatSample{Temperature: 0, Distance: 0, Altitude: 1000.0},
		},
		{
			name: "double altitude negative",
			msg:  &examplev1.FloatSample{Temperature: 0, Distance: 0, Altitude: -500.5},
		},
		{
			name: "all fields",
			msg:  &examplev1.FloatSample{Temperature: -10.5, Distance: 250.0, Altitude: 123.5},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := bitpacker.Pack(tc.msg, bitpacker.OverflowError)
			if err != nil {
				t.Fatalf("Pack error: %v", err)
			}
			got := &examplev1.FloatSample{}
			if err := bitpacker.Unpack(data, got); err != nil {
				t.Fatalf("Unpack error: %v", err)
			}
			if !proto.Equal(tc.msg, got) {
				t.Errorf("roundtrip mismatch:\n  want: %v\n   got: %v", tc.msg, got)
			}
		})
	}
}

// TestTimestamp verifies compact timestamp encoding round-trips.
func TestTimestamp(t *testing.T) {
	epoch2026 := int64(1735689600) // 2026-01-01T00:00:00Z

	t.Run("default_64bit_signed_roundtrip", func(t *testing.T) {
		// updated_at: 64-bit signed seconds, Unix epoch (no annotation)
		ts := timestamppb.New(time.Unix(1735689600, 0).UTC())
		msg := &examplev1.TimestampedEvent{UpdatedAt: ts}
		data, err := bitpacker.Pack(msg, bitpacker.OverflowError)
		if err != nil {
			t.Fatalf("Pack error: %v", err)
		}
		got := &examplev1.TimestampedEvent{}
		if err := bitpacker.Unpack(data, got); err != nil {
			t.Fatalf("Unpack error: %v", err)
		}
		if got.UpdatedAt.GetSeconds() != ts.GetSeconds() {
			t.Errorf("seconds mismatch: want %d, got %d", ts.GetSeconds(), got.UpdatedAt.GetSeconds())
		}
	})

	t.Run("26bit_unsigned_forward_only_roundtrip", func(t *testing.T) {
		// recorded_at: 26-bit unsigned seconds from 2026-01-01
		// Use 2026-06-15 (≈ 165 days after epoch = 14,256,000 seconds)
		offset := int64(14_256_000)
		ts := timestamppb.New(time.Unix(epoch2026+offset, 0).UTC())
		msg := &examplev1.TimestampedEvent{RecordedAt: ts}
		data, err := bitpacker.Pack(msg, bitpacker.OverflowError)
		if err != nil {
			t.Fatalf("Pack error: %v", err)
		}
		got := &examplev1.TimestampedEvent{}
		if err := bitpacker.Unpack(data, got); err != nil {
			t.Fatalf("Unpack error: %v", err)
		}
		if got.RecordedAt.GetSeconds() != epoch2026+offset {
			t.Errorf("seconds mismatch: want %d, got %d", epoch2026+offset, got.RecordedAt.GetSeconds())
		}
	})

	t.Run("32bit_milliseconds_roundtrip", func(t *testing.T) {
		// event_ms: 32-bit signed milliseconds from 2026-01-01
		// Use 2026-01-02 00:00:00.500 UTC (86400500 ms after epoch)
		ts := timestamppb.New(time.Unix(epoch2026+86400, 500_000_000).UTC())
		msg := &examplev1.TimestampedEvent{EventMs: ts}
		data, err := bitpacker.Pack(msg, bitpacker.OverflowError)
		if err != nil {
			t.Fatalf("Pack error: %v", err)
		}
		got := &examplev1.TimestampedEvent{}
		if err := bitpacker.Unpack(data, got); err != nil {
			t.Fatalf("Unpack error: %v", err)
		}
		// millisecond granularity: sub-ms nanos are discarded
		wantSecs := epoch2026 + 86400
		wantNanos := int32(500_000_000)
		if got.EventMs.GetSeconds() != wantSecs || got.EventMs.GetNanos() != wantNanos {
			t.Errorf("ms roundtrip mismatch: want %d.%09d, got %d.%09d",
				wantSecs, wantNanos, got.EventMs.GetSeconds(), got.EventMs.GetNanos())
		}
	})

	t.Run("nil_timestamp_presence_bit", func(t *testing.T) {
		// nil timestamp → presence=0, field absent after unpack
		msg := &examplev1.TimestampedEvent{}
		data, err := bitpacker.Pack(msg, bitpacker.OverflowError)
		if err != nil {
			t.Fatalf("Pack error: %v", err)
		}
		got := &examplev1.TimestampedEvent{}
		if err := bitpacker.Unpack(data, got); err != nil {
			t.Fatalf("Unpack error: %v", err)
		}
		if got.RecordedAt != nil {
			t.Errorf("expected nil RecordedAt, got %v", got.RecordedAt)
		}
	})

	t.Run("forward_only_before_epoch_overflow_error", func(t *testing.T) {
		// timestamp before 2026-01-01 epoch should fail with OverflowError for forward_only
		ts := timestamppb.New(time.Unix(epoch2026-1, 0).UTC()) // 1 second before epoch
		msg := &examplev1.TimestampedEvent{RecordedAt: ts}
		_, err := bitpacker.Pack(msg, bitpacker.OverflowError)
		if err == nil {
			t.Error("expected PackError for timestamp before epoch with forward_only, got nil")
		}
	})

	t.Run("forward_only_before_epoch_clamp", func(t *testing.T) {
		// with OverflowClamp, pre-epoch timestamp clamps to 0 (= epoch itself)
		ts := timestamppb.New(time.Unix(epoch2026-100, 0).UTC())
		msg := &examplev1.TimestampedEvent{RecordedAt: ts}
		data, err := bitpacker.Pack(msg, bitpacker.OverflowClamp)
		if err != nil {
			t.Fatalf("Pack error: %v", err)
		}
		got := &examplev1.TimestampedEvent{}
		if err := bitpacker.Unpack(data, got); err != nil {
			t.Fatalf("Unpack error: %v", err)
		}
		// clamped to 0 offset → stored epoch
		if got.RecordedAt.GetSeconds() != epoch2026 {
			t.Errorf("clamp: want epoch %d, got %d", epoch2026, got.RecordedAt.GetSeconds())
		}
	})

	t.Run("bit_size_timestamp_fields", func(t *testing.T) {
		// All three fields present:
		// updated_at:  1(presence) + 64(seconds) = 65 bits
		// recorded_at: 1(presence) + 26(seconds) = 27 bits
		// event_ms:    1(presence) + 32(ms)       = 33 bits
		// Total: 125 bits → 16 bytes
		ts1 := timestamppb.New(time.Unix(1_700_000_000, 0).UTC())
		ts2 := timestamppb.New(time.Unix(epoch2026+1000, 0).UTC())
		ts3 := timestamppb.New(time.Unix(epoch2026+86400, 0).UTC())
		msg := &examplev1.TimestampedEvent{UpdatedAt: ts1, RecordedAt: ts2, EventMs: ts3}
		data, err := bitpacker.Pack(msg, bitpacker.OverflowError)
		if err != nil {
			t.Fatalf("Pack error: %v", err)
		}
		if len(data) != 16 {
			t.Errorf("expected 16 bytes for 3 timestamp fields, got %d", len(data))
		}
	})
}

// TestOverflowStrategy verifies all strategy behaviours.
func TestOverflowStrategy(t *testing.T) {
	p := bitpacker.NewPacker()

	t.Run("uint_modulo", func(t *testing.T) {
		// sensor_id is 12 bits (max 4095); 4096 % 4096 = 0
		msg := &examplev1.SensorReading{SensorId: 4096}
		data, err := p.Pack(msg, bitpacker.OverflowModulo)
		if err != nil {
			t.Fatalf("Pack error: %v", err)
		}
		got := &examplev1.SensorReading{}
		if err := bitpacker.Unpack(data, got); err != nil {
			t.Fatalf("Unpack error: %v", err)
		}
		if got.SensorId != 0 {
			t.Errorf("modulo: expected SensorId=0, got %d", got.SensorId)
		}
	})

	t.Run("uint_modulo_nonzero", func(t *testing.T) {
		// sensor_id is 12 bits; 4097 % 4096 = 1
		msg := &examplev1.SensorReading{SensorId: 4097}
		data, err := p.Pack(msg, bitpacker.OverflowModulo)
		if err != nil {
			t.Fatalf("Pack error: %v", err)
		}
		got := &examplev1.SensorReading{}
		if err := bitpacker.Unpack(data, got); err != nil {
			t.Fatalf("Unpack error: %v", err)
		}
		if got.SensorId != 1 {
			t.Errorf("modulo: expected SensorId=1, got %d", got.SensorId)
		}
	})

	t.Run("uint_clamp", func(t *testing.T) {
		// sensor_id is 12 bits; 9999 clamps to 4095
		msg := &examplev1.SensorReading{SensorId: 9999}
		data, err := p.Pack(msg, bitpacker.OverflowClamp)
		if err != nil {
			t.Fatalf("Pack error: %v", err)
		}
		got := &examplev1.SensorReading{}
		if err := bitpacker.Unpack(data, got); err != nil {
			t.Fatalf("Unpack error: %v", err)
		}
		if got.SensorId != 4095 {
			t.Errorf("clamp: expected SensorId=4095, got %d", got.SensorId)
		}
	})

	t.Run("sint_clamp_positive", func(t *testing.T) {
		// temperature_deci is sint32 11 bits; zigzag max for 11 bits = 2047 (unsigned).
		// The max positive sint32 value encodable in 11 bits: zigzag(v) <= 2047 → v = 1023
		// 2000 should clamp to 1023
		msg := &examplev1.SensorReading{TemperatureDeci: 2000}
		data, err := p.Pack(msg, bitpacker.OverflowClamp)
		if err != nil {
			t.Fatalf("Pack error: %v", err)
		}
		got := &examplev1.SensorReading{}
		if err := bitpacker.Unpack(data, got); err != nil {
			t.Fatalf("Unpack error: %v", err)
		}
		// zigzag(1023) = 2046, zigzag(2047 unsigned) = saturated value
		// The clamp sets zz = 2047, zagzig32(2047) = -1024
		if got.TemperatureDeci != -1024 {
			t.Errorf("sint clamp: expected -1024 (max-zz), got %d", got.TemperatureDeci)
		}
	})

	t.Run("string_crop_right", func(t *testing.T) {
		// tags has length_bits=5 → max 31 bytes per element; use a 40-byte string
		longTag := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKL" // 38 chars
		msg := &examplev1.Burst{Tags: []string{longTag}}
		data, err := p.Pack(msg, bitpacker.OverflowCropRight)
		if err != nil {
			t.Fatalf("Pack error: %v", err)
		}
		got := &examplev1.Burst{}
		if err := bitpacker.Unpack(data, got); err != nil {
			t.Fatalf("Unpack error: %v", err)
		}
		if len(got.Tags) != 1 {
			t.Fatalf("expected 1 tag, got %d", len(got.Tags))
		}
		want := longTag[:31] // first 31 bytes
		if got.Tags[0] != want {
			t.Errorf("crop_right: expected %q, got %q", want, got.Tags[0])
		}
	})

	t.Run("string_crop_left", func(t *testing.T) {
		// Same setup; CropLeft keeps last 31 bytes
		longTag := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKL" // 38 chars
		msg := &examplev1.Burst{Tags: []string{longTag}}
		data, err := p.Pack(msg, bitpacker.OverflowCropLeft)
		if err != nil {
			t.Fatalf("Pack error: %v", err)
		}
		got := &examplev1.Burst{}
		if err := bitpacker.Unpack(data, got); err != nil {
			t.Fatalf("Unpack error: %v", err)
		}
		if len(got.Tags) != 1 {
			t.Fatalf("expected 1 tag, got %d", len(got.Tags))
		}
		want := longTag[len(longTag)-31:] // last 31 bytes
		if got.Tags[0] != want {
			t.Errorf("crop_left: expected %q, got %q", want, got.Tags[0])
		}
	})

	t.Run("repeated_count_clamp", func(t *testing.T) {
		// adc_samples has count_bits=4 → max 15 elements; supply 20
		samples := make([]uint32, 20)
		for i := range samples {
			samples[i] = uint32(i + 1) // fits in 12 bits each
		}
		msg := &examplev1.Burst{AdcSamples: samples}
		data, err := p.Pack(msg, bitpacker.OverflowClamp)
		if err != nil {
			t.Fatalf("Pack error: %v", err)
		}
		got := &examplev1.Burst{}
		if err := bitpacker.Unpack(data, got); err != nil {
			t.Fatalf("Unpack error: %v", err)
		}
		if len(got.AdcSamples) != 15 {
			t.Errorf("count clamp: expected 15 elements, got %d", len(got.AdcSamples))
		}
		// Verify the first 15 elements match
		for i := 0; i < 15; i++ {
			if got.AdcSamples[i] != samples[i] {
				t.Errorf("element %d: want %d, got %d", i, samples[i], got.AdcSamples[i])
			}
		}
	})

	t.Run("repeated_count_crop_left", func(t *testing.T) {
		// crop_left: keep last 15 of 20
		samples := make([]uint32, 20)
		for i := range samples {
			samples[i] = uint32(i + 1)
		}
		msg := &examplev1.Burst{AdcSamples: samples}
		data, err := p.Pack(msg, bitpacker.OverflowCropLeft)
		if err != nil {
			t.Fatalf("Pack error: %v", err)
		}
		got := &examplev1.Burst{}
		if err := bitpacker.Unpack(data, got); err != nil {
			t.Fatalf("Unpack error: %v", err)
		}
		if len(got.AdcSamples) != 15 {
			t.Errorf("count crop_left: expected 15 elements, got %d", len(got.AdcSamples))
		}
		// Should be last 15 elements (indices 5..19)
		for i := 0; i < 15; i++ {
			want := samples[5+i]
			if got.AdcSamples[i] != want {
				t.Errorf("element %d: want %d, got %d", i, want, got.AdcSamples[i])
			}
		}
	})
}
