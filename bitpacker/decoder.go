package bitpacker

import (
	"math"
	"time"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (p *Packer) decodeMessage(r *bitReader, msg protoreflect.Message, schema *messageSchema) error {
	for _, unit := range schema.units {
		switch u := unit.(type) {
		case *scalarFieldUnit:
			if err := p.decodeField(r, msg, u); err != nil {
				return err
			}
		case *oneofUnit:
			if err := p.decodeOneof(r, msg, u); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Packer) decodeField(r *bitReader, msg protoreflect.Message, u *scalarFieldUnit) error {
	fd := u.fd

	if u.isOptional {
		presence, err := r.readBits(1)
		if err != nil {
			return &UnpackError{Field: string(fd.Name()), Reason: ErrUnexpectedEOF.Error()}
		}
		if presence == 0 {
			return nil
		}
	}

	if u.isTimestamp {
		return p.decodeTimestamp(r, msg, u)
	}

	if u.isMessage {
		presence, err := r.readBits(1)
		if err != nil {
			return &UnpackError{Field: string(fd.Name()), Reason: ErrUnexpectedEOF.Error()}
		}
		if presence == 0 {
			return nil
		}
		nestedMsg := msg.Mutable(fd).Message()
		nestedSchema, err := p.getOrAnalyze(fd.Message())
		if err != nil {
			return err
		}
		return p.decodeMessage(r, nestedMsg, nestedSchema)
	}

	if fd.IsList() {
		count, err := r.readBits(int(u.countBits))
		if err != nil {
			return &UnpackError{Field: string(fd.Name()), Reason: ErrUnexpectedEOF.Error()}
		}
		list := msg.Mutable(fd).List()

		if fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind {
			nestedSchema, err := p.getOrAnalyze(fd.Message())
			if err != nil {
				return err
			}
			for i := uint64(0); i < count; i++ {
				elemMsg := list.NewElement().Message()
				if err := p.decodeMessage(r, elemMsg, nestedSchema); err != nil {
					return err
				}
				list.Append(protoreflect.ValueOfMessage(elemMsg))
			}
		} else {
			for i := uint64(0); i < count; i++ {
				val, err := p.decodeScalar(r, fd, u.bits, u.lengthBits)
				if err != nil {
					return err
				}
				list.Append(val)
			}
		}
		return nil
	}

	if fd.IsMap() {
		count, err := r.readBits(int(u.countBits))
		if err != nil {
			return &UnpackError{Field: string(fd.Name()), Reason: ErrUnexpectedEOF.Error()}
		}
		m := msg.Mutable(fd).Map()
		for i := uint64(0); i < count; i++ {
			key, err := p.decodeMapKey(r, fd, u)
			if err != nil {
				return err
			}
			val, err := p.decodeScalar(r, fd.MapValue(), u.bits, u.lengthBits)
			if err != nil {
				return err
			}
			m.Set(key, val)
		}
		return nil
	}

	val, err := p.decodeScalar(r, fd, u.bits, u.lengthBits)
	if err != nil {
		return err
	}
	msg.Set(fd, val)
	return nil
}

func (p *Packer) decodeTimestamp(r *bitReader, msg protoreflect.Message, u *scalarFieldUnit) error {
	fd := u.fd
	fieldName := string(fd.Name())

	presence, err := r.readBits(1)
	if err != nil {
		return &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
	}
	if presence == 0 {
		return nil
	}

	bitsN, epochSecs, granularity, forwardOnly := tsParams(u)

	raw, err := r.readBits(int(bitsN))
	if err != nil {
		return &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
	}

	var offset int64
	if forwardOnly {
		offset = int64(raw)
	} else {
		// Sign-extend
		if bitsN < 64 && raw>>(bitsN-1) != 0 {
			raw |= ^uint64((1 << bitsN) - 1)
		}
		offset = int64(raw)
	}

	epochUnits := epochSecs * granularity
	tsUnits := offset + epochUnits

	var t time.Time
	switch granularity {
	case 1:
		t = time.Unix(tsUnits, 0).UTC()
	case 1_000:
		t = time.UnixMilli(tsUnits).UTC()
	case 1_000_000:
		t = time.UnixMicro(tsUnits).UTC()
	default: // 1_000_000_000
		t = time.Unix(tsUnits/1_000_000_000, tsUnits%1_000_000_000).UTC()
	}

	ts := timestamppb.New(t)
	tsMsg := msg.Mutable(fd).Message()
	tsFds := tsMsg.Descriptor().Fields()
	tsMsg.Set(tsFds.ByName("seconds"), protoreflect.ValueOfInt64(ts.GetSeconds()))
	tsMsg.Set(tsFds.ByName("nanos"), protoreflect.ValueOfInt32(ts.GetNanos()))
	return nil
}

func (p *Packer) decodeOneof(r *bitReader, msg protoreflect.Message, u *oneofUnit) error {
	selector, err := r.readBits(int(u.selectorBits))
	if err != nil {
		return &UnpackError{Field: string(u.od.Name()), Reason: ErrUnexpectedEOF.Error()}
	}
	if selector == 0 {
		return nil // no field set
	}

	idx := int(selector) - 1
	if idx >= len(u.fields) {
		return &UnpackError{Field: string(u.od.Name()), Reason: "selector index out of range"}
	}

	su := u.fields[idx]
	fd := su.fd

	if fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind {
		nestedMsg := msg.Mutable(fd).Message()
		nestedSchema, err := p.getOrAnalyze(fd.Message())
		if err != nil {
			return err
		}
		return p.decodeMessage(r, nestedMsg, nestedSchema)
	}

	val, err := p.decodeScalar(r, fd, su.bits, su.lengthBits)
	if err != nil {
		return err
	}
	msg.Set(fd, val)
	return nil
}

func (p *Packer) decodeScalar(r *bitReader, fd protoreflect.FieldDescriptor, bitsN uint32, lengthBits uint32) (protoreflect.Value, error) {
	fieldName := string(fd.Name())

	switch fd.Kind() {
	case protoreflect.BoolKind:
		v, err := r.readBits(1)
		if err != nil {
			return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		return protoreflect.ValueOfBool(v != 0), nil

	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		v, err := r.readBits(int(bitsN))
		if err != nil {
			return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		return protoreflect.ValueOfUint32(uint32(v)), nil

	case protoreflect.Int32Kind, protoreflect.Sfixed32Kind:
		v, err := r.readBits(int(bitsN))
		if err != nil {
			return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		// Sign-extend
		if bitsN < 32 && v>>(bitsN-1) != 0 {
			v |= ^uint64((1 << bitsN) - 1)
		}
		return protoreflect.ValueOfInt32(int32(v)), nil

	case protoreflect.Sint32Kind:
		v, err := r.readBits(int(bitsN))
		if err != nil {
			return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		return protoreflect.ValueOfInt32(zagzig32(uint32(v))), nil

	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		v, err := r.readBits(int(bitsN))
		if err != nil {
			return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		return protoreflect.ValueOfUint64(v), nil

	case protoreflect.Int64Kind, protoreflect.Sfixed64Kind:
		v, err := r.readBits(int(bitsN))
		if err != nil {
			return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		// Sign-extend
		if bitsN < 64 && v>>(bitsN-1) != 0 {
			v |= ^uint64((1 << bitsN) - 1)
		}
		return protoreflect.ValueOfInt64(int64(v)), nil

	case protoreflect.Sint64Kind:
		v, err := r.readBits(int(bitsN))
		if err != nil {
			return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		return protoreflect.ValueOfInt64(zagzig64(v)), nil

	case protoreflect.FloatKind:
		fo := getFieldOpts(fd)
		if fo.Fixed != nil || fo.Ufixed != nil {
			fp := fo.Fixed
			if fp == nil {
				fp = fo.Ufixed
			}
			v, err := r.readBits(int(bitsN))
			if err != nil {
				return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
			}
			var floatVal float64
			if fo.Ufixed != nil {
				floatVal = float64(v) / math.Pow10(int(fp.GetDecimalPlaces()))
			} else {
				signed := int64(v<<(64-bitsN)) >> (64 - bitsN)
				floatVal = float64(signed) / math.Pow10(int(fp.GetDecimalPlaces()))
			}
			return protoreflect.ValueOfFloat32(float32(floatVal)), nil
		}
		bits := bitsN
		if bits == 0 {
			bits = 32
		}
		switch bits {
		case 16:
			v, err := r.readBits(16)
			if err != nil {
				return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
			}
			return protoreflect.ValueOfFloat32(float16ToFloat32(uint16(v))), nil
		default: // 32
			v, err := r.readBits(32)
			if err != nil {
				return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
			}
			return protoreflect.ValueOfFloat32(math.Float32frombits(uint32(v))), nil
		}

	case protoreflect.DoubleKind:
		fo := getFieldOpts(fd)
		if fo.Fixed != nil || fo.Ufixed != nil {
			fp := fo.Fixed
			if fp == nil {
				fp = fo.Ufixed
			}
			v, err := r.readBits(int(bitsN))
			if err != nil {
				return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
			}
			var floatVal float64
			if fo.Ufixed != nil {
				floatVal = float64(v) / math.Pow10(int(fp.GetDecimalPlaces()))
			} else {
				signed := int64(v<<(64-bitsN)) >> (64 - bitsN)
				floatVal = float64(signed) / math.Pow10(int(fp.GetDecimalPlaces()))
			}
			return protoreflect.ValueOfFloat64(floatVal), nil
		}
		bits := bitsN
		if bits == 0 {
			bits = 64
		}
		switch bits {
		case 16:
			v, err := r.readBits(16)
			if err != nil {
				return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
			}
			return protoreflect.ValueOfFloat64(float64(float16ToFloat32(uint16(v)))), nil
		case 32:
			v, err := r.readBits(32)
			if err != nil {
				return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
			}
			return protoreflect.ValueOfFloat64(float64(math.Float32frombits(uint32(v)))), nil
		default: // 64
			v, err := r.readBits(64)
			if err != nil {
				return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
			}
			return protoreflect.ValueOfFloat64(math.Float64frombits(v)), nil
		}

	case protoreflect.StringKind:
		length, err := r.readBits(int(lengthBits))
		if err != nil {
			return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		b, err := r.readRawBytes(int(length))
		if err != nil {
			return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		return protoreflect.ValueOfString(string(b)), nil

	case protoreflect.BytesKind:
		length, err := r.readBits(int(lengthBits))
		if err != nil {
			return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		b, err := r.readRawBytes(int(length))
		if err != nil {
			return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		return protoreflect.ValueOfBytes(b), nil

	case protoreflect.EnumKind:
		v, err := r.readBits(int(bitsN))
		if err != nil {
			return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		return protoreflect.ValueOfEnum(protoreflect.EnumNumber(v)), nil

	case protoreflect.MessageKind, protoreflect.GroupKind:
		// Handled in decodeField
		return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: "unexpected message kind in decodeScalar"}
	}

	return protoreflect.Value{}, &UnpackError{Field: fieldName, Reason: "unknown field kind"}
}

func (p *Packer) decodeMapKey(r *bitReader, fd protoreflect.FieldDescriptor, u *scalarFieldUnit) (protoreflect.MapKey, error) {
	keyFd := fd.MapKey()
	fieldName := string(fd.Name())

	switch keyFd.Kind() {
	case protoreflect.StringKind:
		length, err := r.readBits(int(u.keyLengthBits))
		if err != nil {
			return protoreflect.MapKey{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		b, err := r.readRawBytes(int(length))
		if err != nil {
			return protoreflect.MapKey{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		return protoreflect.ValueOfString(string(b)).MapKey(), nil

	case protoreflect.BoolKind:
		v, err := r.readBits(1)
		if err != nil {
			return protoreflect.MapKey{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		return protoreflect.ValueOfBool(v != 0).MapKey(), nil

	case protoreflect.Sint32Kind:
		v, err := r.readBits(int(u.keyBits))
		if err != nil {
			return protoreflect.MapKey{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		return protoreflect.ValueOfInt32(zagzig32(uint32(v))).MapKey(), nil

	case protoreflect.Sint64Kind:
		v, err := r.readBits(int(u.keyBits))
		if err != nil {
			return protoreflect.MapKey{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		return protoreflect.ValueOfInt64(zagzig64(v)).MapKey(), nil

	case protoreflect.Int32Kind, protoreflect.Sfixed32Kind:
		v, err := r.readBits(int(u.keyBits))
		if err != nil {
			return protoreflect.MapKey{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		if u.keyBits < 32 && v>>(u.keyBits-1) != 0 {
			v |= ^uint64((1 << u.keyBits) - 1)
		}
		return protoreflect.ValueOfInt32(int32(v)).MapKey(), nil

	case protoreflect.Int64Kind, protoreflect.Sfixed64Kind:
		v, err := r.readBits(int(u.keyBits))
		if err != nil {
			return protoreflect.MapKey{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		if u.keyBits < 64 && v>>(u.keyBits-1) != 0 {
			v |= ^uint64((1 << u.keyBits) - 1)
		}
		return protoreflect.ValueOfInt64(int64(v)).MapKey(), nil

	default:
		v, err := r.readBits(int(u.keyBits))
		if err != nil {
			return protoreflect.MapKey{}, &UnpackError{Field: fieldName, Reason: ErrUnexpectedEOF.Error()}
		}
		switch keyFd.Kind() {
		case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
			return protoreflect.ValueOfUint32(uint32(v)).MapKey(), nil
		default:
			return protoreflect.ValueOfUint64(v).MapKey(), nil
		}
	}
}
