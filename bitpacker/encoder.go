package bitpacker

import (
	"fmt"
	"math"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func (p *Packer) encodeMessage(w *bitWriter, msg protoreflect.Message, schema *messageSchema, strategy OverflowStrategy) error {
	for _, unit := range schema.units {
		switch u := unit.(type) {
		case *scalarFieldUnit:
			if err := p.encodeField(w, msg, u, strategy); err != nil {
				return err
			}
		case *oneofUnit:
			if err := p.encodeOneof(w, msg, u, strategy); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Packer) encodeField(w *bitWriter, msg protoreflect.Message, u *scalarFieldUnit, strategy OverflowStrategy) error {
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
			return p.encodeMessage(w, nested, nestedSchema, strategy)
		}
		w.writeBits(0, 1)
		return nil
	}

	if fd.IsList() {
		list := msg.Get(fd).List()
		n := list.Len()
		maxCount := uint64((uint64(1) << u.countBits) - 1)
		if uint64(n) > maxCount {
			eff := effectiveStrategy(fd, strategy)
			switch eff {
			case OverflowError:
				return &PackError{Field: string(fd.Name()), Reason: fmt.Sprintf("list length %d overflows count_bits %d", n, u.countBits)}
			case OverflowModulo:
				n = int(uint64(n) & maxCount)
			case OverflowClamp, OverflowCropRight:
				n = int(maxCount)
			case OverflowCropLeft:
				// keep last maxCount elements
				start := n - int(maxCount)
				w.writeBits(maxCount, int(u.countBits))
				for i := start; i < list.Len(); i++ {
					if err := p.encodeValue(w, list.Get(i), u, strategy); err != nil {
						return err
					}
				}
				return nil
			}
		}
		w.writeBits(uint64(n), int(u.countBits))
		for i := 0; i < n; i++ {
			if err := p.encodeValue(w, list.Get(i), u, strategy); err != nil {
				return err
			}
		}
		return nil
	}

	if fd.IsMap() {
		m := msg.Get(fd).Map()
		mapLen := m.Len()
		maxCount := uint64((uint64(1) << u.countBits) - 1)
		if uint64(mapLen) > maxCount {
			eff := effectiveStrategy(fd, strategy)
			switch eff {
			case OverflowError:
				return &PackError{Field: string(fd.Name()), Reason: fmt.Sprintf("map length %d overflows count_bits %d", mapLen, u.countBits)}
			default:
				// For maps, clamp: write only the first maxCount entries
				mapLen = int(maxCount)
			}
		}
		w.writeBits(uint64(mapLen), int(u.countBits))
		written := 0
		var encErr error
		m.Range(func(k protoreflect.MapKey, v protoreflect.Value) bool {
			if written >= mapLen {
				return false
			}
			if err := p.encodeMapKey(w, k, u); err != nil {
				encErr = err
				return false
			}
			if err := p.encodeValue(w, v, u, strategy); err != nil {
				encErr = err
				return false
			}
			written++
			return true
		})
		return encErr
	}

	return p.encodeValue(w, msg.Get(fd), u, strategy)
}

func (p *Packer) encodeOneof(w *bitWriter, msg protoreflect.Message, u *oneofUnit, strategy OverflowStrategy) error {
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
		return p.encodeMessage(w, nested, nestedSchema, strategy)
	}

	return p.encodeValue(w, msg.Get(whichFd), su, strategy)
}

func (p *Packer) encodeValue(w *bitWriter, val protoreflect.Value, u *scalarFieldUnit, strategy OverflowStrategy) error {
	fd := u.fd

	// For map values, use the map value descriptor
	valueFd := fd
	if fd.IsMap() {
		valueFd = fd.MapValue()
	}

	return p.encodeScalar(w, val, valueFd, u.bits, u.lengthBits, strategy)
}

func (p *Packer) encodeScalar(w *bitWriter, val protoreflect.Value, fd protoreflect.FieldDescriptor, bitsN uint32, lengthBits uint32, strategy OverflowStrategy) error {
	fieldName := string(fd.Name())
	eff := effectiveStrategy(fd, strategy)

	switch fd.Kind() {
	case protoreflect.BoolKind:
		if val.Bool() {
			w.writeBits(1, 1)
		} else {
			w.writeBits(0, 1)
		}

	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		v := val.Uint()
		if bitsN < 64 && v >= (uint64(1)<<bitsN) {
			switch eff {
			case OverflowError:
				return &PackError{Field: fieldName, Reason: fmt.Sprintf("value %d overflows %d bits", v, bitsN)}
			case OverflowModulo:
				v = v & ((uint64(1) << bitsN) - 1)
			default: // Clamp, CropLeft, CropRight → clamp
				v = (uint64(1) << bitsN) - 1
			}
		}
		w.writeBits(v, int(bitsN))

	case protoreflect.Int32Kind, protoreflect.Sfixed32Kind:
		v := val.Int()
		minVal := -(int64(1) << (bitsN - 1))
		maxVal := (int64(1) << (bitsN - 1)) - 1
		if v < minVal || v > maxVal {
			switch eff {
			case OverflowError:
				return &PackError{Field: fieldName, Reason: fmt.Sprintf("value %d overflows signed %d bits", v, bitsN)}
			case OverflowModulo:
				// two's-complement wrap: mask bitsN bits then sign-extend
				mask := uint64((uint64(1) << bitsN) - 1)
				wrapped := uint64(v) & mask
				// sign-extend
				if wrapped>>(bitsN-1) != 0 {
					v = int64(wrapped) | ^int64(mask)
				} else {
					v = int64(wrapped)
				}
			default: // Clamp, CropLeft, CropRight → clamp
				if v < minVal {
					v = minVal
				} else {
					v = maxVal
				}
			}
		}
		mask := uint64((uint64(1) << bitsN) - 1)
		w.writeBits(uint64(v)&mask, int(bitsN))

	case protoreflect.Sint32Kind:
		v := int32(val.Int())
		zz := zigzag32(v)
		if bitsN < 64 && uint64(zz) >= (uint64(1)<<bitsN) {
			switch eff {
			case OverflowError:
				return &PackError{Field: fieldName, Reason: fmt.Sprintf("zigzag value %d overflows %d bits", zz, bitsN)}
			case OverflowModulo:
				zz = zz & uint32((uint64(1)<<bitsN)-1)
			default: // Clamp, CropLeft, CropRight → clamp
				zz = uint32((uint64(1) << bitsN) - 1)
			}
		}
		w.writeBits(uint64(zz), int(bitsN))

	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		v := val.Uint()
		if bitsN < 64 && v >= (uint64(1)<<bitsN) {
			switch eff {
			case OverflowError:
				return &PackError{Field: fieldName, Reason: fmt.Sprintf("value %d overflows %d bits", v, bitsN)}
			case OverflowModulo:
				v = v & ((uint64(1) << bitsN) - 1)
			default: // Clamp, CropLeft, CropRight → clamp
				v = (uint64(1) << bitsN) - 1
			}
		}
		w.writeBits(v, int(bitsN))

	case protoreflect.Int64Kind, protoreflect.Sfixed64Kind:
		v := val.Int()
		if bitsN < 64 {
			minVal := -(int64(1) << (bitsN - 1))
			maxVal := (int64(1) << (bitsN - 1)) - 1
			if v < minVal || v > maxVal {
				switch eff {
				case OverflowError:
					return &PackError{Field: fieldName, Reason: fmt.Sprintf("value %d overflows signed %d bits", v, bitsN)}
				case OverflowModulo:
					mask := (uint64(1) << bitsN) - 1
					wrapped := uint64(v) & mask
					if wrapped>>(bitsN-1) != 0 {
						v = int64(wrapped) | ^int64(mask)
					} else {
						v = int64(wrapped)
					}
				default: // Clamp, CropLeft, CropRight → clamp
					if v < minVal {
						v = minVal
					} else {
						v = maxVal
					}
				}
			}
		}
		mask := uint64(math.MaxUint64)
		if bitsN < 64 {
			mask = (uint64(1) << bitsN) - 1
		}
		w.writeBits(uint64(v)&mask, int(bitsN))

	case protoreflect.Sint64Kind:
		v := val.Int()
		zz := zigzag64(v)
		if bitsN < 64 && zz >= (uint64(1)<<bitsN) {
			switch eff {
			case OverflowError:
				return &PackError{Field: fieldName, Reason: fmt.Sprintf("zigzag value %d overflows %d bits", zz, bitsN)}
			case OverflowModulo:
				zz = zz & ((uint64(1) << bitsN) - 1)
			default: // Clamp, CropLeft, CropRight → clamp
				zz = (uint64(1) << bitsN) - 1
			}
		}
		w.writeBits(zz, int(bitsN))

	case protoreflect.FloatKind:
		f := float64(val.Float())
		fo := getFieldOpts(fd)
		if fo.GetFixed() != nil || fo.GetUfixed() != nil {
			fp := fo.GetFixed()
			if fp == nil {
				fp = fo.GetUfixed()
			}
			scaled := math.Round(f * math.Pow10(int(fp.GetDecimalPlaces())))
			if fo.GetUfixed() != nil {
				// unsigned: check against [0, 2^bits-1]
				maxV := float64(uint64(1)<<bitsN) - 1
				if scaled < 0 || scaled > maxV {
					switch eff {
					case OverflowError:
						return &PackError{Field: fieldName, Reason: fmt.Sprintf("ufixed float value %v overflows %d bits", scaled, bitsN)}
					case OverflowModulo:
						scaled = math.Mod(scaled, maxV+1)
						if scaled < 0 {
							scaled += maxV + 1
						}
					default: // Clamp
						if scaled < 0 {
							scaled = 0
						} else {
							scaled = maxV
						}
					}
				}
				w.writeBits(uint64(scaled), int(bitsN))
			} else {
				// signed fixed-point
				minV := -float64(int64(1) << (bitsN - 1))
				maxV := float64(int64(1)<<(bitsN-1)) - 1
				if scaled < minV || scaled > maxV {
					switch eff {
					case OverflowError:
						return &PackError{Field: fieldName, Reason: fmt.Sprintf("fixed float value %v overflows signed %d bits", scaled, bitsN)}
					case OverflowModulo:
						mask := float64(uint64(1) << bitsN)
						scaled = math.Mod(scaled, mask)
						if scaled > maxV {
							scaled -= mask
						} else if scaled < minV {
							scaled += mask
						}
					default: // Clamp
						if scaled < minV {
							scaled = minV
						} else {
							scaled = maxV
						}
					}
				}
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
		if fo.GetFixed() != nil || fo.GetUfixed() != nil {
			fp := fo.GetFixed()
			if fp == nil {
				fp = fo.GetUfixed()
			}
			scaled := math.Round(f * math.Pow10(int(fp.GetDecimalPlaces())))
			if fo.GetUfixed() != nil {
				maxV := float64(uint64(1)<<bitsN) - 1
				if scaled < 0 || scaled > maxV {
					switch eff {
					case OverflowError:
						return &PackError{Field: fieldName, Reason: fmt.Sprintf("ufixed double value %v overflows %d bits", scaled, bitsN)}
					case OverflowModulo:
						scaled = math.Mod(scaled, maxV+1)
						if scaled < 0 {
							scaled += maxV + 1
						}
					default: // Clamp
						if scaled < 0 {
							scaled = 0
						} else {
							scaled = maxV
						}
					}
				}
				w.writeBits(uint64(scaled), int(bitsN))
			} else {
				minV := -float64(int64(1) << (bitsN - 1))
				maxV := float64(int64(1)<<(bitsN-1)) - 1
				if scaled < minV || scaled > maxV {
					switch eff {
					case OverflowError:
						return &PackError{Field: fieldName, Reason: fmt.Sprintf("fixed double value %v overflows signed %d bits", scaled, bitsN)}
					case OverflowModulo:
						mask := float64(uint64(1) << bitsN)
						scaled = math.Mod(scaled, mask)
						if scaled > maxV {
							scaled -= mask
						} else if scaled < minV {
							scaled += mask
						}
					default: // Clamp
						if scaled < minV {
							scaled = minV
						} else {
							scaled = maxV
						}
					}
				}
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
		maxLen := uint64(0)
		if lengthBits < 64 {
			maxLen = (uint64(1) << lengthBits) - 1
		}
		if uint64(len(s)) > maxLen {
			switch eff {
			case OverflowError:
				return &PackError{Field: fieldName, Reason: fmt.Sprintf("string length %d overflows length_bits %d", len(s), lengthBits)}
			case OverflowModulo:
				s = s[:uint64(len(s))%maxLen]
			case OverflowCropLeft:
				s = s[uint64(len(s))-maxLen:]
			default: // Clamp, CropRight → keep first maxLen bytes
				s = s[:maxLen]
			}
		}
		w.writeBits(uint64(len(s)), int(lengthBits))
		w.writeRawBytes(s)

	case protoreflect.BytesKind:
		b := val.Bytes()
		maxLen := uint64(0)
		if lengthBits < 64 {
			maxLen = (uint64(1) << lengthBits) - 1
		}
		if uint64(len(b)) > maxLen {
			switch eff {
			case OverflowError:
				return &PackError{Field: fieldName, Reason: fmt.Sprintf("bytes length %d overflows length_bits %d", len(b), lengthBits)}
			case OverflowModulo:
				b = b[:uint64(len(b))%maxLen]
			case OverflowCropLeft:
				b = b[uint64(len(b))-maxLen:]
			default: // Clamp, CropRight → keep first maxLen bytes
				b = b[:maxLen]
			}
		}
		w.writeBits(uint64(len(b)), int(lengthBits))
		w.writeRawBytes(b)

	case protoreflect.EnumKind:
		v := val.Enum()
		if bitsN < 64 && uint64(v) >= (uint64(1)<<bitsN) {
			switch eff {
			case OverflowError:
				return &PackError{Field: fieldName, Reason: fmt.Sprintf("enum value %d overflows %d bits", v, bitsN)}
			case OverflowModulo:
				v = protoreflect.EnumNumber(uint64(v) & ((uint64(1) << bitsN) - 1))
			default: // Clamp, CropLeft, CropRight → clamp
				v = protoreflect.EnumNumber((uint64(1) << bitsN) - 1)
			}
		}
		w.writeBits(uint64(v), int(bitsN))

	case protoreflect.MessageKind, protoreflect.GroupKind:
		nested := val.Message()
		nestedSchema, err := p.getOrAnalyze(fd.Message())
		if err != nil {
			return err
		}
		return p.encodeMessage(w, nested, nestedSchema, strategy)
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
