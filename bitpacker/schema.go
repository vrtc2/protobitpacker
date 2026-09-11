package bitpacker

import (
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// EncodingUnit is either a *ScalarFieldUnit or a *OneofUnit.
// The unexported method prevents external implementations; use type assertions.
type EncodingUnit interface{ isEncodingUnit() }

// ScalarFieldUnit handles regular fields, proto3-optional fields, and message fields.
type ScalarFieldUnit struct {
	Fd            protoreflect.FieldDescriptor
	Bits          uint32
	LengthBits    uint32
	CountBits     uint32
	KeyBits       uint32
	KeyLengthBits uint32
	IsOptional    bool // proto3 optional (synthetic oneof) → emit 1-bit presence
	IsMessage     bool // nested message → emit 1-bit presence + recurse
	IsTimestamp   bool // google.protobuf.Timestamp → compact integer encoding
}

func (s *ScalarFieldUnit) isEncodingUnit() {}

// OneofUnit handles a real (non-synthetic) oneof group.
type OneofUnit struct {
	Od           protoreflect.OneofDescriptor
	SelectorBits uint32
	Fields       []*ScalarFieldUnit // in declaration order
}

func (o *OneofUnit) isEncodingUnit() {}

// MessageSchema is the pre-analyzed encoding plan for a MessageDescriptor.
type MessageSchema struct {
	Units []EncodingUnit
}

// AnalyzeMessage builds the ordered encoding plan for a MessageDescriptor.
// Returns *ValidationError for missing/invalid annotations.
func AnalyzeMessage(md protoreflect.MessageDescriptor) (*MessageSchema, error) {
	schema := &MessageSchema{}
	seenOneofs := map[protoreflect.FullName]bool{}

	fds := md.Fields()
	for i := 0; i < fds.Len(); i++ {
		fd := fds.Get(i)
		od := fd.ContainingOneof()

		if od != nil && !od.IsSynthetic() {
			// Real oneof — emit unit once for the whole group
			key := od.FullName()
			if !seenOneofs[key] {
				seenOneofs[key] = true
				unit, err := buildOneofUnit(od, md)
				if err != nil {
					return nil, err
				}
				schema.Units = append(schema.Units, unit)
			}
		} else {
			// Regular field, proto3-optional, or message field
			unit, err := buildScalarUnit(fd, md)
			if err != nil {
				return nil, err
			}
			schema.Units = append(schema.Units, unit)
		}
	}

	return schema, nil
}

func buildOneofUnit(od protoreflect.OneofDescriptor, md protoreflect.MessageDescriptor) (*OneofUnit, error) {
	opts := GetOneofOpts(od)
	n := od.Fields().Len()
	minBits := MinSelectorBits(n)

	selectorBits := opts.SelectorBits
	if selectorBits == 0 {
		selectorBits = minBits
	} else if selectorBits < minBits {
		return nil, &ValidationError{
			Message: string(md.FullName()),
			Field:   string(od.Name()),
			Reason:  fmt.Sprintf("selector_bits %d < minimum %d for %d fields", selectorBits, minBits, n),
		}
	}

	unit := &OneofUnit{
		Od:           od,
		SelectorBits: selectorBits,
	}

	for j := 0; j < n; j++ {
		fd := od.Fields().Get(j)
		su, err := buildScalarUnit(fd, md)
		if err != nil {
			return nil, err
		}
		// Fields inside a oneof are never "optional" in the proto3-optional sense
		su.IsOptional = false
		unit.Fields = append(unit.Fields, su)
	}

	return unit, nil
}

func buildScalarUnit(fd protoreflect.FieldDescriptor, md protoreflect.MessageDescriptor) (*ScalarFieldUnit, error) {
	opts := GetFieldOpts(fd)

	unit := &ScalarFieldUnit{
		Fd:            fd,
		Bits:          opts.Bits,
		LengthBits:    opts.LengthBits,
		CountBits:     opts.CountBits,
		KeyBits:       opts.KeyBits,
		KeyLengthBits: opts.KeyLengthBits,
	}

	od := fd.ContainingOneof()
	// Message-kind fields (IsMessage, IsTimestamp) handle their own presence bit internally,
	// so IsOptional must not be set for them — it would produce a double presence bit on wire.
	if od != nil && od.IsSynthetic() &&
		fd.Kind() != protoreflect.MessageKind &&
		fd.Kind() != protoreflect.GroupKind {
		unit.IsOptional = true
	}

	if fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind {
		if !fd.IsList() && !fd.IsMap() {
			if fd.Message().FullName() == "google.protobuf.Timestamp" {
				unit.IsTimestamp = true
			} else {
				unit.IsMessage = true
			}
		}
	}

	// Validate
	if err := validateScalarUnit(unit, fd, md); err != nil {
		return nil, err
	}

	return unit, nil
}

func validateScalarUnit(u *ScalarFieldUnit, fd protoreflect.FieldDescriptor, md protoreflect.MessageDescriptor) error {
	msgName := string(md.FullName())
	fieldName := string(fd.Name())

	// repeated / map: need count_bits
	if fd.IsList() || fd.IsMap() {
		if u.CountBits == 0 {
			return &ValidationError{Message: msgName, Field: fieldName, Reason: "repeated/map field requires count_bits > 0"}
		}
	}

	// map key requirements
	if fd.IsMap() {
		keyFd := fd.MapKey()
		switch keyFd.Kind() {
		case protoreflect.StringKind, protoreflect.BytesKind:
			if u.KeyLengthBits == 0 {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "map with string/bytes key requires key_length_bits > 0"}
			}
		default:
			if u.KeyBits == 0 {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "map with integer key requires key_bits > 0"}
			}
		}
	}

	// value validation
	valueFd := fd
	if fd.IsMap() {
		valueFd = fd.MapValue()
	}

	switch valueFd.Kind() {
	case protoreflect.BoolKind:
		if u.Bits != 0 && u.Bits != 1 {
			return &ValidationError{Message: msgName, Field: fieldName, Reason: "bool field: bits must be 0 or 1"}
		}
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		if !fd.IsMap() && !fd.IsList() {
			if u.Bits == 0 {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "integer field requires bits > 0"}
			}
		} else if u.Bits == 0 {
			return &ValidationError{Message: msgName, Field: fieldName, Reason: "integer element requires bits > 0"}
		}
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		if u.Bits == 0 {
			return &ValidationError{Message: msgName, Field: fieldName, Reason: "integer field requires bits > 0"}
		}
	case protoreflect.FloatKind:
		fo := GetFieldOpts(fd)
		if fo.Fixed != nil || fo.Ufixed != nil {
			if fo.Fixed != nil && fo.Ufixed != nil {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "float field: fixed and ufixed are mutually exclusive"}
			}
			if u.Bits == 0 || u.Bits > 32 {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "float field with fixed/ufixed: bits must be 1..32"}
			}
		} else if u.Bits != 0 && u.Bits != 16 && u.Bits != 32 {
			return &ValidationError{Message: msgName, Field: fieldName, Reason: "float field: bits must be 0, 16, or 32"}
		}
	case protoreflect.DoubleKind:
		fo := GetFieldOpts(fd)
		if fo.Fixed != nil || fo.Ufixed != nil {
			if fo.Fixed != nil && fo.Ufixed != nil {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "double field: fixed and ufixed are mutually exclusive"}
			}
			if u.Bits == 0 || u.Bits > 64 {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "double field with fixed/ufixed: bits must be 1..64"}
			}
		} else if u.Bits != 0 && u.Bits != 16 && u.Bits != 32 && u.Bits != 64 {
			return &ValidationError{Message: msgName, Field: fieldName, Reason: "double field: bits must be 0, 16, 32, or 64"}
		}
	case protoreflect.StringKind, protoreflect.BytesKind:
		if u.LengthBits == 0 {
			return &ValidationError{Message: msgName, Field: fieldName, Reason: "string/bytes field requires length_bits > 0"}
		}
	case protoreflect.EnumKind:
		if u.Bits == 0 {
			return &ValidationError{Message: msgName, Field: fieldName, Reason: "enum field requires bits > 0"}
		}
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if u.IsTimestamp {
			fo := GetFieldOpts(fd)
			if fo.Fixed != nil || fo.Ufixed != nil {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "timestamp field: incompatible with fixed/ufixed"}
			}
			if u.LengthBits != 0 || u.CountBits != 0 {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "timestamp field: incompatible with length_bits/count_bits"}
			}
			if u.Bits > 64 {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "timestamp field: bits must be 0..64"}
			}
			tso := fo.GetTimestamp()
			if tso.GetRolling() {
				if tso.GetForwardOnly() {
					return &ValidationError{Message: msgName, Field: fieldName, Reason: "timestamp field: rolling is incompatible with forward_only"}
				}
				if tso.GetEpochSeconds() != 0 {
					return &ValidationError{Message: msgName, Field: fieldName, Reason: "timestamp field: rolling ignores epoch_seconds, remove it to avoid confusion"}
				}
			}
		}
		// nested message: no annotation required, validated recursively
	}

	return nil
}
