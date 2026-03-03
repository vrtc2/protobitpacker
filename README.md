# protobitpacker

**Bit-level binary packing for Protocol Buffers — Go library.**

Annotate your `.proto` schema with field-width hints and achieve the smallest possible
binary representation for transmission over constrained channels — LoRa, UART, CAN bus,
satellite links, or any bandwidth-limited medium.

```
Standard protobuf SensorReading  ≈ 14 bytes  (112 bits)
Bit-packed SensorReading         =  5 bytes  ( 34 bits)   ← 70% smaller
```

The library works **through protobuf reflection** — no code generator is needed.
Import it, call `Pack`/`Unpack`, and your annotated messages are serialised to the minimum
number of bits you declare.

The tradeoff is strict: **there is no backward or forward compatibility.** Both endpoints
must use identical schemas.

---

## Installation

```bash
go get github.com/vrtc2/protobitpacker
```

Add the annotation module as a buf dependency so your proto files can import `options.proto`:

```yaml
# buf.yaml
version: v2
deps:
  - buf.build/vrtc2/protobitpacker
```

```bash
buf dep update
```

---

## Quick Start

### 1. Annotate your schema

```protobuf
syntax = "proto3";
package myapp.v1;

import "bitpacker/v1/options.proto";

message EngineData {
  uint32 rpm            = 1 [(bitpacker.v1.field).bits = 13]; // 0–8191
  sint32 oil_temp_deci  = 2 [(bitpacker.v1.field).bits = 12]; // –2048..2047 (0.1 °C)
  uint32 throttle_pct   = 3 [(bitpacker.v1.field).bits = 7];  // 0–100
  bool   check_engine   = 4;                                    // 1 bit, no annotation needed
}
// Total: 13 + 12 + 7 + 1 = 33 bits → 5 bytes (standard protobuf: ~15 bytes)
```

### 2. Generate Go bindings

```bash
buf generate
```

This produces the standard `.pb.go` file. The bitpacker annotations are embedded in the
binary descriptor and read at runtime — no separate generator step needed.

### 3. Pack and unpack

```go
import (
    "github.com/vrtc2/protobitpacker/bitpacker"
    myappv1 "your/module/gen/go/myapp/v1"
)

msg := &myappv1.EngineData{
    Rpm:           5500,
    OilTempDeci:   950, // 95.0 °C
    ThrottlePct:   72,
    CheckEngine:   false,
}

// Serialise to bytes
data, err := bitpacker.Pack(msg)

// Deserialise back
out := &myappv1.EngineData{}
err = bitpacker.Unpack(data, out)
```

---

## API

```go
// Package-level functions use a shared default Packer.
func Pack(msg proto.Message) ([]byte, error)
func Unpack(data []byte, msg proto.Message) error

// Packer is the reusable handle. It caches descriptor analysis per
// message type and is safe for concurrent use.
type Packer struct{ /* ... */ }

func NewPacker() *Packer
func (p *Packer) Pack(msg proto.Message) ([]byte, error)
func (p *Packer) Unpack(data []byte, msg proto.Message) error

// Validate checks all fields in the descriptor for valid annotations,
// recursively including nested messages. Call at startup to catch
// schema mistakes early rather than on the first Pack call.
func (p *Packer) Validate(desc protoreflect.MessageDescriptor) error
```

### Error types

```go
// ValidationError — missing or invalid annotation on a field.
type ValidationError struct {
    Message string // fully-qualified message name
    Field   string // field name
    Reason  string
}

// PackError — value out of range for its declared bit width.
type PackError struct {
    Field  string
    Reason string
}

// UnpackError — bit stream is truncated or malformed.
type UnpackError struct {
    Field  string
    Reason string
}

var ErrUnexpectedEOF = errors.New("bitpacker: unexpected end of bit stream")
```

### Early validation

```go
p := bitpacker.NewPacker()
if err := p.Validate((&myappv1.EngineData{}).ProtoReflect().Descriptor()); err != nil {
    log.Fatal("schema error:", err)
}
```

---

## Annotation Reference

### `(bitpacker.v1.field).bits`

Number of bits for the field value.

```protobuf
uint32 speed_rpm = 1 [(bitpacker.v1.field).bits = 14]; // 0–16383
sint32 offset    = 2 [(bitpacker.v1.field).bits = 10]; // –512..511 (zigzag for sint)
MyEnum status    = 3 [(bitpacker.v1.field).bits = 2];  // 4 enum values
```

Applicable to: `bool` (default 1, annotation optional), all integer scalars, `enum`,
`float` (16 or 32, default 32), `double` (16, 32, or 64, default 64), and as the
element width for `repeated` numerics.

### `(bitpacker.v1.field).length_bits`

Bit width of the byte-length prefix for `string` or `bytes` fields.
Maximum data length = `2^length_bits − 1` bytes.

```protobuf
string name  = 1 [(bitpacker.v1.field).length_bits = 6]; // up to 63 bytes
bytes  frame = 2 [(bitpacker.v1.field).length_bits = 8]; // up to 255 bytes
```

### `(bitpacker.v1.field).count_bits`

Bit width of the element count for `repeated` or `map` fields. **Required** for all
`repeated` and `map` fields; no default.

```protobuf
repeated uint32 samples = 1 [
  (bitpacker.v1.field).count_bits = 4,  // up to 15 elements
  (bitpacker.v1.field).bits       = 12  // each 0–4095
];
```

### `(bitpacker.v1.field).key_bits` and `key_length_bits`

Bit widths for map keys.

```protobuf
map<uint32, uint32> table = 1 [
  (bitpacker.v1.field).count_bits = 6,
  (bitpacker.v1.field).key_bits   = 10, // integer key 0–1023
  (bitpacker.v1.field).bits       = 16  // value 0–65535
];

map<string, uint32> config = 2 [
  (bitpacker.v1.field).count_bits      = 6,
  (bitpacker.v1.field).key_length_bits = 5, // string key up to 31 bytes
  (bitpacker.v1.field).bits            = 16
];
```

### `(bitpacker.v1.oneof).selector_bits`

Bit width of the oneof discriminator. Defaults to `ceil(log2(N+1))` where N is the
number of fields. Selector `0` means no field is set; `1`..`N` select the k-th field
in declaration order.

```protobuf
oneof payload {
  option (bitpacker.v1.oneof).selector_bits = 2; // 0=none 1=raw 2=command 3=ack
  bytes  raw     = 1 [(bitpacker.v1.field).length_bits = 8];
  uint32 command = 2 [(bitpacker.v1.field).bits = 8];
  bool   ack     = 3;
}
```

---

## Wire Format

Fields are packed in **ascending field-number order**, **MSB-first**, with no tags,
delimiters, or metadata. The stream is zero-padded to a byte boundary at the end.

```
┌──────────────────────────────────────────────┐
│ field₁ bits │ field₂ bits │ … │ 0-padding   │
└──────────────────────────────────────────────┘
   MSB first     MSB first          0–7 bits
```

### Presence rules

| Field kind | Encoding |
|---|---|
| Proto3 singular scalar | Always written; no presence bit |
| `optional` scalar | 1-bit presence flag, then value bits (omitted when 0) |
| `message` field | 1-bit presence flag, then recursive pack (omitted when 0) |
| `repeated` | `count_bits`-wide count, then N × element |
| `map` | `count_bits`-wide count, then N × (key + value) |
| `oneof` | `selector_bits`-wide discriminator, then selected field (nothing when 0) |

### Type encoding

| Proto type | Encoding |
|---|---|
| `bool` | 1 bit |
| `uint32`, `uint64`, `fixed32`, `fixed64` | N-bit unsigned, MSB first |
| `int32`, `int64`, `sfixed32`, `sfixed64` | N-bit two's complement, MSB first |
| `sint32`, `sint64` | zigzag-encoded, then N-bit unsigned, MSB first |
| `float` | 32-bit (IEEE 754 binary32) or 16-bit (binary16, lossy) |
| `double` | 64-bit, 32-bit (lossy), or 16-bit (lossy) |
| `string`, `bytes` | `length_bits`-wide byte count, then raw bytes |
| `enum` | N-bit unsigned (proto enum number) |
| `message` | recursive pack (preceded by presence bit when not in oneof) |

---

## Full Example

```protobuf
syntax = "proto3";
package telemetry.v1;
import "bitpacker/v1/options.proto";

enum SensorStatus {
  SENSOR_STATUS_UNSPECIFIED = 0;
  SENSOR_STATUS_OK          = 1;
  SENSOR_STATUS_WARNING     = 2;
  SENSOR_STATUS_ERROR       = 3;
}

// 34 bits (5 bytes) without label. Standard protobuf: ~14 bytes.
message SensorReading {
  uint32          sensor_id        = 1 [(bitpacker.v1.field).bits = 12];
  sint32          temperature_deci = 2 [(bitpacker.v1.field).bits = 11]; // 0.1 °C, zigzag
  uint32          humidity_pct     = 3 [(bitpacker.v1.field).bits = 7];
  bool            alert            = 4;
  SensorStatus    status           = 5 [(bitpacker.v1.field).bits = 2];
  optional string label            = 6 [(bitpacker.v1.field).length_bits = 5]; // up to 31 bytes
}

message Packet {
  uint32 sequence = 1 [(bitpacker.v1.field).bits = 16];
  oneof payload {
    option (bitpacker.v1.oneof).selector_bits = 2;
    bytes  raw     = 2 [(bitpacker.v1.field).length_bits = 8];
    uint32 command = 3 [(bitpacker.v1.field).bits = 8];
    bool   ack     = 4;
  }
}

// 4 readings: 8(count) + 4×34 = 144 bits → 18 bytes. Protobuf: ~64 bytes.
message Burst {
  repeated SensorReading readings   = 1 [(bitpacker.v1.field).count_bits = 8];
  repeated uint32        adc_samples = 2 [
    (bitpacker.v1.field).count_bits = 4,
    (bitpacker.v1.field).bits       = 12
  ];
}

message Config {
  map<string, uint32> settings = 1 [
    (bitpacker.v1.field).count_bits      = 6,
    (bitpacker.v1.field).key_length_bits = 5,
    (bitpacker.v1.field).bits            = 16
  ];
}
```

---

## Limitations

- **No schema evolution.** Any change to field numbers, annotations, or oneof layouts
  produces an incompatible wire format. Version your schemas explicitly.
- **No self-description.** A packed byte stream is opaque without the originating schema.
  Add framing (version byte, message type tag) at a higher layer if needed.
- **Reduced-precision floats are lossy.** Converting a `double` to `bits = 32` or
  `bits = 16` is irreversible.
- **Map entry order is unspecified.** Do not rely on ordering of map entries in the stream.

---

## License

MIT — see [LICENSE](LICENSE).
