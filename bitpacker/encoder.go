package bitpacker

import (
	"fmt"
	"math"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func (p *Packer) encodeMessage(w *bitWriter, msg protoreflect.Message, schema *messageSchema) error {
	for _, unit := range schema.units {
		switch u := unit.(type) {
		case *scalarFieldUnit:
			if err := p.encodeField(w, msg, u); err != nil {
				return err
			}
		case *oneofUnit:
			if err := p.encodeOneof(w, msg, u); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Packer) encodeField(w *bitWriter, msg protoreflect.Message, u *scalarFieldUnit) error {
	fd := u.fd

	if u.isOptional {
		if msg.Has(fd) {
			w.writeBits(1, 1)
		} else {
			w.writeBits(0, 1)
			return nil
		}
	}

	if u.isMessage {
		if msg.Has(fd) {
			w.writeBits(1, 1)
			nested := msg.Get(fd).Message()
			nestedSchema, err := p.getOrAnalyze(fd.Message())
			if err != nil {
				return err
			}
			return p.encodeMessage(w, nested, nestedSchema)
		}
		w.writeBits(0, 1)
		return nil
	}

	if fd.IsList() {
		list := msg.Get(fd).List()
		n := list.Len()
		w.writeBits(uint64(n), int(u.countBits))
		for i := 0; i < n; i++ {
			if err := p.encodeValue(w, list.Get(i), u); err != nil {
				return err
			}
		}
		return nil
	}

	if fd.IsMap() {
		m := msg.Get(fd).Map()
		w.writeBits(uint64(m.Len()), int(u.countBits))
		var encErr error
		m.Range(func(k protoreflect.MapKey, v protoreflect.Value) bool {
			if err := p.encodeMapKey(w, k, u); err != nil {
				encErr = err
				return false
			}
			if err := p.encodeValue(w, v, u); err != nil {
				encErr = err
				return false
			}
			return true
		})
		return encErr
	}

	return p.encodeValue(w, msg.Get(fd), u)
}

func (p *Packer) encodeOneof(w *bitWriter, msg protoreflect.Message, u *oneofUnit) error {
	whichFd := msg.WhichOneof(u.od)
	if whichFd == nil {
		w.writeBits(0, int(u.selectorBits))
		return nil
	}

	// Find index (1-based)
	idx := -1
	for i, su := range u.fields {
		if su.fd.Number() == whichFd.Number() {
			idx = i + 1
			break
		}
	}
	if idx < 0 {
		return &PackError{Field: string(whichFd.Name()), Reason: "field not found in oneof"}
	}

	w.writeBits(uint64(idx), int(u.selectorBits))
	su := u.fields[idx-1]

	// If oneof field is a message, encode without a presence bit (selector serves as presence)
	if whichFd.Kind() == protoreflect.MessageKind || whichFd.Kind() == protoreflect.GroupKind {
		nested := msg.Get(whichFd).Message()
		nestedSchema, err := p.getOrAnalyze(whichFd.Message())
		if err != nil {
			return err
		}
		return p.encodeMessage(w, nested, nestedSchema)
	}

	return p.encodeValue(w, msg.Get(whichFd), su)
}

func (p *Packer) encodeValue(w *bitWriter, val protoreflect.Value, u *scalarFieldUnit) error {
	fd := u.fd

	// For map values, use the map value descriptor
	valueFd := fd
	if fd.IsMap() {
		valueFd = fd.MapValue()
	}

	return p.encodeScalar(w, val, valueFd, u.bits, u.lengthBits)
}

func (p *Packer) encodeScalar(w *bitWriter, val protoreflect.Value, fd protoreflect.FieldDescriptor, bitsN uint32, lengthBits uint32) error {
	fieldName := string(fd.Name())

	switch fd.Kind() {
	case protoreflect.BoolKind:
		if val.Bool() {
			w.writeBits(1, 1)
		} else {
			w.writeBits(0, 1)
		}

	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		v := val.Uint()
		if bitsN < 64 && v >= (1<<bitsN) {
			return &PackError{Field: fieldName, Reason: fmt.Sprintf("value %d overflows %d bits", v, bitsN)}
		}
		w.writeBits(v, int(bitsN))

	case protoreflect.Int32Kind, protoreflect.Sfixed32Kind:
		v := val.Int()
		minVal := -(int64(1) << (bitsN - 1))
		maxVal := (int64(1) << (bitsN - 1)) - 1
		if v < minVal || v > maxVal {
			return &PackError{Field: fieldName, Reason: fmt.Sprintf("value %d overflows signed %d bits", v, bitsN)}
		}
		mask := uint64((1 << bitsN) - 1)
		w.writeBits(uint64(v)&mask, int(bitsN))

	case protoreflect.Sint32Kind:
		v := int32(val.Int())
		zz := zigzag32(v)
		if bitsN < 64 && uint64(zz) >= (1<<bitsN) {
			return &PackError{Field: fieldName, Reason: fmt.Sprintf("zigzag value %d overflows %d bits", zz, bitsN)}
		}
		w.writeBits(uint64(zz), int(bitsN))

	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		v := val.Uint()
		if bitsN < 64 && v >= (1<<bitsN) {
			return &PackError{Field: fieldName, Reason: fmt.Sprintf("value %d overflows %d bits", v, bitsN)}
		}
		w.writeBits(v, int(bitsN))

	case protoreflect.Int64Kind, protoreflect.Sfixed64Kind:
		v := val.Int()
		if bitsN < 64 {
			minVal := -(int64(1) << (bitsN - 1))
			maxVal := (int64(1) << (bitsN - 1)) - 1
			if v < minVal || v > maxVal {
				return &PackError{Field: fieldName, Reason: fmt.Sprintf("value %d overflows signed %d bits", v, bitsN)}
			}
		}
		mask := uint64(math.MaxUint64)
		if bitsN < 64 {
			mask = (1 << bitsN) - 1
		}
		w.writeBits(uint64(v)&mask, int(bitsN))

	case protoreflect.Sint64Kind:
		v := val.Int()
		zz := zigzag64(v)
		if bitsN < 64 && zz >= (1<<bitsN) {
			return &PackError{Field: fieldName, Reason: fmt.Sprintf("zigzag value %d overflows %d bits", zz, bitsN)}
		}
		w.writeBits(zz, int(bitsN))

	case protoreflect.FloatKind:
		f := float64(val.Float())
		fo := getFieldOpts(fd)
		if fo.Fixed != nil || fo.Ufixed != nil {
			fp := fo.Fixed
			if fp == nil {
				fp = fo.Ufixed
			}
			scaled := math.Round(f * math.Pow10(int(fp.GetDecimalPlaces())))
			if fo.Ufixed != nil {
				w.writeBits(uint64(scaled), int(bitsN))
			} else {
				w.writeBits(uint64(int64(scaled)), int(bitsN))
			}
			break
		}
		bits := bitsN
		if bits == 0 {
			bits = 32
		}
		switch bits {
		case 16:
			w.writeBits(uint64(float32ToFloat16(float32(f))), 16)
		default: // 32
			w.writeBits(uint64(math.Float32bits(float32(f))), 32)
		}

	case protoreflect.DoubleKind:
		f := val.Float()
		fo := getFieldOpts(fd)
		if fo.Fixed != nil || fo.Ufixed != nil {
			fp := fo.Fixed
			if fp == nil {
				fp = fo.Ufixed
			}
			scaled := math.Round(f * math.Pow10(int(fp.GetDecimalPlaces())))
			if fo.Ufixed != nil {
				w.writeBits(uint64(scaled), int(bitsN))
			} else {
				w.writeBits(uint64(int64(scaled)), int(bitsN))
			}
			break
		}
		bits := bitsN
		if bits == 0 {
			bits = 64
		}
		switch bits {
		case 16:
			w.writeBits(uint64(float32ToFloat16(float32(f))), 16)
		case 32:
			w.writeBits(uint64(math.Float32bits(float32(f))), 32)
		default: // 64
			w.writeBits(math.Float64bits(f), 64)
		}

	case protoreflect.StringKind:
		s := []byte(val.String())
		w.writeBits(uint64(len(s)), int(lengthBits))
		w.writeRawBytes(s)

	case protoreflect.BytesKind:
		b := val.Bytes()
		w.writeBits(uint64(len(b)), int(lengthBits))
		w.writeRawBytes(b)

	case protoreflect.EnumKind:
		v := val.Enum()
		if bitsN < 64 && uint64(v) >= (1<<bitsN) {
			return &PackError{Field: fieldName, Reason: fmt.Sprintf("enum value %d overflows %d bits", v, bitsN)}
		}
		w.writeBits(uint64(v), int(bitsN))

	case protoreflect.MessageKind, protoreflect.GroupKind:
		nested := val.Message()
		nestedSchema, err := p.getOrAnalyze(fd.Message())
		if err != nil {
			return err
		}
		return p.encodeMessage(w, nested, nestedSchema)
	}

	return nil
}

func (p *Packer) encodeMapKey(w *bitWriter, key protoreflect.MapKey, u *scalarFieldUnit) error {
	fd := u.fd
	keyFd := fd.MapKey()

	switch keyFd.Kind() {
	case protoreflect.StringKind:
		s := []byte(key.String())
		w.writeBits(uint64(len(s)), int(u.keyLengthBits))
		w.writeRawBytes(s)
	case protoreflect.BoolKind:
		if key.Bool() {
			w.writeBits(1, 1)
		} else {
			w.writeBits(0, 1)
		}
	case protoreflect.Sint32Kind:
		v := int32(key.Int())
		w.writeBits(uint64(zigzag32(v)), int(u.keyBits))
	case protoreflect.Sint64Kind:
		v := key.Int()
		w.writeBits(zigzag64(v), int(u.keyBits))
	case protoreflect.Int32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sfixed64Kind:
		v := key.Int()
		mask := uint64(math.MaxUint64)
		if u.keyBits < 64 {
			mask = (1 << u.keyBits) - 1
		}
		w.writeBits(uint64(v)&mask, int(u.keyBits))
	default:
		v := key.Uint()
		w.writeBits(v, int(u.keyBits))
	}
	return nil
}

// zigzag helpers
func zigzag32(v int32) uint32 { return uint32((v << 1) ^ (v >> 31)) }
func zagzig32(v uint32) int32 { return int32((v >> 1) ^ -(v & 1)) }
func zigzag64(v int64) uint64 { return uint64((v << 1) ^ (v >> 63)) }
func zagzig64(v uint64) int64 { return int64((v >> 1) ^ -(v & 1)) }

// float16 conversion helpers (IEEE 754 binary16)
func float32ToFloat16(f float32) uint16 {
	bits := math.Float32bits(f)
	sign := uint16((bits >> 31) & 0x1)
	exp := int((bits >> 23) & 0xFF)
	mant := bits & 0x7FFFFF

	if exp == 0xFF { // NaN or Inf
		if mant != 0 {
			return sign<<15 | 0x7C00 | 1 // NaN
		}
		return sign<<15 | 0x7C00 // Inf
	}

	exp -= 127  // remove float32 bias
	exp += 15   // add float16 bias
	if exp >= 31 {
		return sign<<15 | 0x7C00 // overflow → Inf
	}
	if exp <= 0 {
		// subnormal or underflow
		if exp < -10 {
			return sign << 15 // underflow → 0
		}
		mant = (mant | 0x800000) >> uint(1-exp)
		return sign<<15 | uint16(mant>>13)
	}
	return sign<<15 | uint16(exp)<<10 | uint16(mant>>13)
}

func float16ToFloat32(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := int((h >> 10) & 0x1F)
	mant := uint32(h & 0x3FF)

	if exp == 31 { // NaN or Inf
		return math.Float32frombits(sign | 0x7F800000 | mant)
	}
	if exp == 0 { // subnormal
		if mant == 0 {
			return math.Float32frombits(sign)
		}
		// normalize
		exp = 1
		for mant&0x400 == 0 {
			mant <<= 1
			exp--
		}
		mant &^= 0x400
	}
	exp += 127 - 15
	return math.Float32frombits(sign | uint32(exp)<<23 | mant<<13)
}
