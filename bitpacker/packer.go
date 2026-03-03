// Package bitpacker implements bit-level binary packing for Protocol Buffers
// using custom annotations defined in bitpacker/v1/options.proto.
//
// The packer works entirely through protobuf reflection — no code generation
// step is required. Fields are encoded in ascending field-number order,
// MSB-first, with zero-padding to a byte boundary.
package bitpacker

import (
	"sync"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Packer packs and unpacks proto.Message values using bit-level encoding.
// It caches descriptor analysis per message type and is safe for concurrent use.
type Packer struct {
	mu    sync.RWMutex
	cache map[protoreflect.FullName]*messageSchema
}

// NewPacker creates a new Packer with an empty cache.
func NewPacker() *Packer {
	return &Packer{
		cache: make(map[protoreflect.FullName]*messageSchema),
	}
}

// Pack serialises msg into a bit-packed byte slice.
// Returns *PackError if a value is out of range for its declared bit width.
// Returns *ValidationError if a required annotation is missing.
func (p *Packer) Pack(msg proto.Message) ([]byte, error) {
	ref := msg.ProtoReflect()
	schema, err := p.getOrAnalyze(ref.Descriptor())
	if err != nil {
		return nil, err
	}
	w := &bitWriter{}
	if err := p.encodeMessage(w, ref, schema); err != nil {
		return nil, err
	}
	return w.bytes(), nil
}

// Unpack deserialises bit-packed data into msg (which must be a zeroed proto.Message).
// Returns *UnpackError if the stream is truncated or otherwise malformed.
func (p *Packer) Unpack(data []byte, msg proto.Message) error {
	ref := msg.ProtoReflect()
	schema, err := p.getOrAnalyze(ref.Descriptor())
	if err != nil {
		return err
	}
	r := &bitReader{buf: data}
	return p.decodeMessage(r, ref, schema)
}

// Validate checks that all fields in desc have valid bitpacker annotations,
// recursively including nested messages. Call at startup to catch schema mistakes early.
func (p *Packer) Validate(desc protoreflect.MessageDescriptor) error {
	_, err := p.getOrAnalyze(desc)
	return err
}

// getOrAnalyze returns a cached messageSchema, analysing on first access.
func (p *Packer) getOrAnalyze(md protoreflect.MessageDescriptor) (*messageSchema, error) {
	name := md.FullName()

	p.mu.RLock()
	schema, ok := p.cache[name]
	p.mu.RUnlock()
	if ok {
		return schema, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock
	if schema, ok = p.cache[name]; ok {
		return schema, nil
	}

	schema, err := analyzeMessage(md)
	if err != nil {
		return nil, err
	}
	p.cache[name] = schema
	return schema, nil
}

// Default is a package-level shared Packer for convenience.
var Default = NewPacker()

// Pack serialises msg using the default Packer.
func Pack(msg proto.Message) ([]byte, error) { return Default.Pack(msg) }

// Unpack deserialises bit-packed data into msg using the default Packer.
func Unpack(data []byte, msg proto.Message) error { return Default.Unpack(data, msg) }
