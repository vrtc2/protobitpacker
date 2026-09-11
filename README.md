# protobitpacker

**Bit-level binary packing for Protocol Buffers — Go library + C bindings for embedded targets.**

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

// Serialise to bytes (the second argument is the overflow strategy)
data, err := bitpacker.Pack(msg, bitpacker.OverflowError)

// Deserialise back
out := &myappv1.EngineData{}
err = bitpacker.Unpack(data, out)
```

---

## API

```go
// Package-level functions use a shared default Packer.
// strategy selects how out-of-range values are handled (see Overflow Handling).
func Pack(msg proto.Message, strategy OverflowStrategy) ([]byte, error)
func Unpack(data []byte, msg proto.Message) error

// Packer is the reusable handle. It caches descriptor analysis per
// message type and is safe for concurrent use.
type Packer struct{ /* ... */ }

func NewPacker() *Packer
func (p *Packer) Pack(msg proto.Message, strategy OverflowStrategy) ([]byte, error)
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
`float` (16 or 32, default 32; or 1..32 when `fixed`/`ufixed` is set),
`double` (16, 32, or 64, default 64; or 1..64 when `fixed`/`ufixed` is set),
and as the element width for `repeated` numerics.

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

### `(bitpacker.v1.field).fixed` and `(bitpacker.v1.field).ufixed`

Fixed-point decimal encoding for `float` and `double` fields. Instead of raw IEEE 754
bits, the value is scaled by `10^decimal_places`, rounded to the nearest integer, and
stored in `bits` bits. On decode the integer is divided back by `10^decimal_places`.

- **`fixed`** — signed (two's complement). Supports negative values.
  Range: `[−2^(bits−1) / 10^dp .. (2^(bits−1)−1) / 10^dp]`
- **`ufixed`** — unsigned. Non-negative values only; full positive range.
  Range: `[0 .. (2^bits − 1) / 10^dp]`

`bits` must be set explicitly (1..32 for `float`, 1..64 for `double`).
`fixed` and `ufixed` are mutually exclusive.

```protobuf
// Temperature –51.2..51.1 °C, 0.1 °C steps, 10 bits signed
float temperature = 1 [(bitpacker.v1.field) = {
  bits: 10,
  fixed: { decimal_places: 1 }
}];

// Distance 0..655.35 m, 0.01 m steps, 16 bits unsigned
float distance = 2 [(bitpacker.v1.field) = {
  bits: 16,
  ufixed: { decimal_places: 2 }
}];
```

**Wire format:**

```
encode: value_on_wire = round(float_value × 10^decimal_places)
        stored as bits-wide signed (fixed) or unsigned (ufixed) integer

decode: float_value = integer_from_wire ÷ 10^decimal_places
```

### `(bitpacker.v1.field).timestamp`

Compact encoding for `google.protobuf.Timestamp` fields. The timestamp is stored as an
integer **offset from a configurable epoch**, using a configurable time unit and bit width.

**Any** `google.protobuf.Timestamp` field uses this encoding automatically. The annotation
is only needed when you want to override the defaults.

| Parameter | Default | Description |
|---|---|---|
| `bits` (from `FieldOptions.bits`) | 64 | Bit width for the stored value |
| `epoch_seconds` | 0 (Unix epoch, 1970-01-01) | Custom epoch as Unix timestamp in seconds |
| `granularity` | `SECONDS` | Time unit: `SECONDS`, `MILLISECONDS`, `MICROSECONDS`, `NANOSECONDS` |
| `forward_only` | `false` (signed) | `true` = unsigned (only future timestamps from epoch); `false` = signed (past + future) |
| `rolling` | `false` | Rolling window: encoder stores `unix_units mod 2^bits`; decoder uses current wall-clock time to reconstruct. Incompatible with `epoch_seconds` and `forward_only`. |

```protobuf
import "google/protobuf/timestamp.proto";
import "bitpacker/v1/options.proto";

message SensorReading {
  // 64-bit signed seconds from Unix epoch — smart default, no annotation needed.
  google.protobuf.Timestamp updated_at = 1;

  // 26-bit unsigned seconds from 2026-01-01 — covers ~2 years, no past timestamps.
  // 1 bit presence + 26 bits value = 27 bits = ~4 bytes (vs. ~12 bytes standard protobuf)
  google.protobuf.Timestamp recorded_at = 2 [
    (bitpacker.v1.field).bits = 26,
    (bitpacker.v1.field).timestamp = {
      epoch_seconds: 1735689600,   // 2026-01-01T00:00:00Z
      forward_only: true
    }
  ];

  // 32-bit signed milliseconds from 2026-01-01 — ±~24 days around epoch.
  google.protobuf.Timestamp event_ms = 3 [
    (bitpacker.v1.field).bits = 32,
    (bitpacker.v1.field).timestamp = {
      epoch_seconds: 1735689600,
      granularity: TIMESTAMP_GRANULARITY_MILLISECONDS
    }
  ];

  // 24-bit rolling seconds — ~194 day window, 3 bytes wire (1 presence + 24 bits).
  // No epoch needed; decoder anchors to current wall-clock time.
  google.protobuf.Timestamp rolling_at = 4 [
    (bitpacker.v1.field).bits = 24,
    (bitpacker.v1.field).timestamp = { rolling: true }
  ];
}
```

**Wire format:** 1-bit presence flag, then N-bit signed or unsigned integer offset.

```
encode: wire_value = (timestamp_in_units) - (epoch_seconds × units_per_second)
decode: timestamp  = wire_value + (epoch_seconds × units_per_second)
```

Granularity detail: sub-unit precision is discarded on encode (round-down toward zero for
positive offsets). `NANOSECONDS` preserves full `google.protobuf.Timestamp` precision.

**Overflow and `forward_only`:**
- `forward_only: false` (default): signed two's complement — values before epoch are negative.
- `forward_only: true`: unsigned — values before epoch trigger the configured
  `OverflowStrategy` (`OverflowError` by default, or `OverflowClamp` to store 0 = epoch).

**Rolling window (`rolling: true`):**

Encodes `unix_time_units mod 2^bits` — no static epoch required. The decoder uses the
current wall-clock time (`time.Now()`) to reconstruct the full timestamp:

```
encode: wire_value = unix_time_units mod 2^bits
decode: window_start = floor(now / 2^bits) * 2^bits
        timestamp    = window_start + wire_value
        if timestamp - now > 2^bits / 2: timestamp -= 2^bits  // rollover correction
```

The reconstructable window is `2^bits` time units. Decode is correct as long as the
encoded timestamp is within `±(2^bits / 2)` units of `now` at decode time.

| bits | granularity | Window size | Wire size |
|------|-------------|-------------|-----------|
| 24 | SECONDS | ~194 days | 3 bytes |
| 16 | SECONDS | ~18 hours | 2 bytes |
| 24 | MILLISECONDS | ~4.6 hours | 3 bytes |
| 32 | NANOSECONDS | ~4.3 seconds | 4 bytes |

Constraints: `rolling` is incompatible with `epoch_seconds` (ignored) and `forward_only`
(validation error). Overflow strategy is also ignored — rolling encoding never overflows.

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
│ field₁ bits │ field₂ bits │ … │ 0-padding    │
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
| `float` | 32-bit (IEEE 754 binary32) or 16-bit (binary16, lossy); or fixed-point integer when `fixed`/`ufixed` is set |
| `double` | 64-bit, 32-bit (lossy), or 16-bit (lossy); or fixed-point integer when `fixed`/`ufixed` is set |
| `string`, `bytes` | `length_bits`-wide byte count, then raw bytes |
| `enum` | N-bit unsigned (proto enum number) |
| `google.protobuf.Timestamp` | 1-bit presence, then N-bit signed/unsigned offset from epoch |
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

## C Bindings (embedded targets)

For microcontrollers (ESP32, STM32, …) where running the Go reflection library is not an
option, the repo ships a `protoc` plugin — **`protoc-gen-bitpacker-c`** — that generates pure
**C99** encode/decode code from the same annotated `.proto` files. The output is wire-compatible
with the Go library, byte-for-byte.

### Install the plugin and generate

Install it with `go install` (the binary lands in `$(go env GOPATH)/bin`, which must be on `PATH`):

```bash
go install github.com/vrtc2/protobitpacker/cmd/protoc-gen-bitpacker-c@latest
```

`@latest` resolves to the newest release tag; use `@main` for unreleased changes, or pin a
version (`@vX.Y.Z`) so generated code stays reproducible across machines and CI.

Alternatively, build it from a checkout (the binary is git-ignored — it is a build artifact):

```bash
go build -o protoc-gen-bitpacker-c ./cmd/protoc-gen-bitpacker-c
```

Add it to `buf.gen.yaml` next to the Go plugin:

```yaml
version: v2
plugins:
  - remote: buf.build/protocolbuffers/go
    out: gen/go
    opt:
      - paths=source_relative
  - local: protoc-gen-bitpacker-c   # resolved from PATH or the working directory
    out: gen/c
    opt:
      - paths=source_relative
inputs:
  - directory: proto
```

```bash
buf generate                    # plugin installed via go install
PATH="$PWD:$PATH" buf generate  # plugin built into the working directory
```

### What gets generated

For each `foo.proto` that has bitpacker-annotated messages:

| File | Contents |
|---|---|
| `bitpacker_runtime.h` | Shared header-only runtime, emitted once. Every function is `static inline` (nothing to link, zero call overhead). Depends only on `<stdint.h>`, `<stdbool.h>`, `<string.h>`, `<math.h>`. |
| `foo_bitpacker.h` | One `enum` per proto enum, a fixed-size `struct` per message, oneof discriminant enums, and the prototypes. Includes the headers of imported `.proto` files whose types it uses. |
| `foo_bitpacker.c` | The `bp_encode_<Msg>` / `bp_decode_<Msg>` implementations. |

Schemas split across several `.proto` files (and packages) work: an imported file's header is
included by its output-root-relative path (e.g. `#include "acme/common/v1/types_bitpacker.h"`),
so compile with `-I` pointing at the output root and link the `.c` files of every imported
proto. The plugin does not need `go_package` options or buf managed mode.

Public API per message:

```c
/* Returns the number of bytes written, or -1 on buffer overflow. */
int bp_encode_<Msg>(const <Msg> *msg, uint8_t *buf, uint32_t buf_size);

/* Returns the number of bytes consumed, or -1 on a truncated/invalid stream. */
int bp_decode_<Msg>(<Msg> *msg, const uint8_t *buf, uint32_t buf_len);
```

The header also declares `bp_encode_<Msg>_w` / `bp_decode_<Msg>_r`, which operate on a shared
bit cursor. They exist so generated code in importing files can nest messages without byte
alignment; application code should use the buffer API above. Unused padding bits in the last
output byte are always zeroed, so the buffer does not need to be cleared beforehand.

### Struct layout

Variable-length fields map to **fixed-size** storage sized from the annotation, so no heap
allocation is ever needed:

| Proto | Generated C |
|---|---|
| `string s [length_bits = 5]` | `char s[32]; uint16_t s_len;` (`2⁵−1` chars + NUL terminator) |
| `bytes b [length_bits = 8]` | `uint8_t b[255]; uint16_t b_len;` |
| `repeated T x [count_bits = 8]` | `T x[255]; uint16_t x_count;` |
| `optional` / message / `Timestamp` | a `bool has_<field>;` companion flag + the value |
| `google.protobuf.Empty` (any field-less message) | no storage: only `has_<field>` / `<field>_count` / the oneof tag (it is 0 bits on the wire) |
| `oneof` | a `<Msg>_which_<name>_t` tag plus a `union` of the members |

> **Memory note:** array sizes follow the *theoretical maximum* of the bit width
> (`2^bits − 1`). A `repeated <Msg> [count_bits = 8]` of a large message becomes a 255-element
> inline array. Keep `count_bits` / `length_bits` as tight as possible on RAM-constrained
> targets, and pass these structs by pointer (the generated API already does).

### Usage

```c
#include "foo_bitpacker.h"

SensorReading r = {0};
r.sensor_id        = 42;
r.temperature_deci = -123;          /* zigzag-encoded on the wire */
r.has_label        = true;
strcpy(r.label, "north");
r.label_len = 5;

uint8_t buf[64];
int n = bp_encode_SensorReading(&r, buf, sizeof buf);
if (n < 0) { /* buffer too small */ }

SensorReading back = {0};
if (bp_decode_SensorReading(&back, buf, (uint32_t)n) < 0) { /* malformed stream */ }
```

The runtime uses `round()` for fixed-point, so link the math library (`-lm`):

```bash
cc -std=c99 -Igen/c -Igen/c/<package/path> app.c foo_bitpacker.c -lm
```

### C ↔ Go compatibility

The C output is byte-for-byte identical to the Go library for every supported type, including
fixed-point `float`/`double` (both round to nearest).

> **Known divergence — `map` fields are not supported in C.** The Go library encodes maps; the
> C generator **skips them silently**. A message that contains map fields is therefore *not*
> wire-compatible across Go ↔ C. Avoid maps on any message that crosses the Go/C boundary.

---

## Overflow Handling

By default, packing a value that exceeds its declared bit width returns a `*PackError`
and the entire `Pack` call fails. For live telemetry — where one out-of-range sensor
value would otherwise drop the whole packet — you can configure a more lenient strategy.

### Pack-level default

```go
// Preserve the error-on-overflow behaviour (default)
data, err := bitpacker.Pack(msg, bitpacker.OverflowError)

// Wrap overflowing integers, crop overflowing strings/slices
data, err := bitpacker.Pack(msg, bitpacker.OverflowModulo)

// Saturate integers to max/min, truncate strings/slices to max length
data, err := bitpacker.Pack(msg, bitpacker.OverflowClamp)

// String/bytes/repeated: keep last N; integers: clamp
data, err := bitpacker.Pack(msg, bitpacker.OverflowCropLeft)

// String/bytes/repeated: keep first N; integers: clamp (same as Clamp for numbers)
data, err := bitpacker.Pack(msg, bitpacker.OverflowCropRight)
```

### Per-field override

Import and use the `OverflowStrategy` enum in your `.proto` file to override the
pack-level default for a specific field:

```protobuf
import "bitpacker/v1/options.proto";

message Telemetry {
  // Clamp this field even if the pack-level strategy is OverflowModulo
  uint32 voltage_mv = 1 [
    (bitpacker.v1.field).bits     = 12,
    (bitpacker.v1.field).overflow = OVERFLOW_STRATEGY_CLAMP
  ];
}
```

`OVERFLOW_STRATEGY_UNSPECIFIED` (the default for proto3 enums) means "inherit the
pack-level strategy", so you only need to annotate fields that differ from the default.

### Strategy semantics

| Data kind | `OverflowModulo` | `OverflowClamp` | `OverflowCropLeft` | `OverflowCropRight` |
|---|---|---|---|---|
| uint / fixed (unsigned) | `v & mask` | `min(v, 2ⁿ−1)` | → Clamp | → Clamp |
| int / sfixed (signed) | two's-complement wrap | saturate to `±(2ⁿ⁻¹−1)` | → Clamp | → Clamp |
| sint (zigzag) | modulo on zigzag value | clamp zigzag value | → Clamp | → Clamp |
| enum | `v & mask` | `min(v, 2ⁿ−1)` | → Clamp | → Clamp |
| fixed-point float | wrap scaled integer | clamp scaled integer | → Clamp | → Clamp |
| string / bytes | keep `len % max` bytes | keep first `max` bytes | keep **last** `max` bytes | keep **first** `max` bytes |
| repeated / map count | write `count % max` elems | write first `max` elems | write **last** `max` elems | write **first** `max` elems |

---

## Limitations

- **No schema evolution.** Any change to field numbers, annotations, or oneof layouts
  produces an incompatible wire format. Version your schemas explicitly.
- **No self-description.** A packed byte stream is opaque without the originating schema.
  Add framing (version byte, message type tag) at a higher layer if needed.
- **Reduced-precision floats are lossy.** Converting a `double` to `bits = 32` or
  `bits = 16` is irreversible.
- **Map entry order is unspecified.** Do not rely on ordering of map entries in the stream.
- **The C bindings do not support `map` fields.** The C generator skips them silently, so
  messages with maps are not wire-compatible between Go and C. See
  [C Bindings](#c-bindings-embedded-targets).

---

## License

MIT — see [LICENSE](LICENSE).
