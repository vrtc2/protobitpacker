package bitpacker_test

import (
	"testing"

	"github.com/vrtc2/protobitpacker/bitpacker"
	examplev1 "github.com/vrtc2/protobitpacker/gen/go/bitpacker/v1/example"
	"google.golang.org/protobuf/proto"
)

func TestPackUnpackSensorReading_NoLabel(t *testing.T) {
	msg := &examplev1.SensorReading{
		SensorId:        42,
		TemperatureDeci: -100, // zigzag: ((-100<<1)^(-100>>31)) = 199
		HumidityPct:     75,
		Alert:           true,
		Status:          examplev1.SensorStatus_SENSOR_STATUS_WARNING,
	}

	data, err := bitpacker.Pack(msg)
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

	data, err := bitpacker.Pack(msg)
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
	data, err := bitpacker.Pack(msg)
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

	data, err := bitpacker.Pack(msg)
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

	data, err := bitpacker.Pack(msg)
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

	data, err := bitpacker.Pack(msg)
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

	data, err := bitpacker.Pack(msg)
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

	data, err := bitpacker.Pack(msg)
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

	data, err := bitpacker.Pack(msg)
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

	data, err := bitpacker.Pack(msg)
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
	_, err := bitpacker.Pack(msg)
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
	}
	for _, m := range validMsgs {
		if err := p.Validate(m.ProtoReflect().Descriptor()); err != nil {
			t.Errorf("Validate(%T) unexpected error: %v", m, err)
		}
	}
}
