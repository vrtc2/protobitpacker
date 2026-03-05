package bitpacker

import (
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// encodingUnit is either a *scalarFieldUnit or a *oneofUnit.
type encodingUnit interface{ isEncodingUnit() }

// scalarFieldUnit handles regular fields, proto3-optional fields, and message fields.
type scalarFieldUnit struct {
	fd            protoreflect.FieldDescriptor
	bits          uint32
	lengthBits    uint32
	countBits     uint32
	keyBits       uint32
	keyLengthBits uint32
	isOptional    bool // proto3 optional (synthetic oneof) → emit 1-bit presence
	isMessage     bool // nested message → emit 1-bit presence + recurse
	isTimestamp   bool // google.protobuf.Timestamp → compact integer encoding
}

func (s *scalarFieldUnit) isEncodingUnit() {}

// oneofUnit handles a real (non-synthetic) oneof group.
type oneofUnit struct {
	od           protoreflect.OneofDescriptor
	selectorBits uint32
	fields       []*scalarFieldUnit // in declaration order
}

func (o *oneofUnit) isEncodingUnit() {}

// messageSchema is the pre-analyzed encoding plan for a MessageDescriptor.
type messageSchema struct {
	units []encodingUnit
}

// analyzeMessage builds the ordered encoding plan for a MessageDescriptor.
// Returns *ValidationError for missing/invalid annotations.
func analyzeMessage(md protoreflect.MessageDescriptor) (*messageSchema, error) {
	schema := &messageSchema{}
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
				schema.units = append(schema.units, unit)
			}
		} else {
			// Regular field, proto3-optional, or message field
			unit, err := buildScalarUnit(fd, md)
			if err != nil {
				return nil, err
			}
			schema.units = append(schema.units, unit)
		}
	}

	return schema, nil
}

func buildOneofUnit(od protoreflect.OneofDescriptor, md protoreflect.MessageDescriptor) (*oneofUnit, error) {
	opts := getOneofOpts(od)
	n := od.Fields().Len()
	minBits := minSelectorBits(n)

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

	unit := &oneofUnit{
		od:           od,
		selectorBits: selectorBits,
	}

	for j := 0; j < n; j++ {
		fd := od.Fields().Get(j)
		su, err := buildScalarUnit(fd, md)
		if err != nil {
			return nil, err
		}
		// Fields inside a oneof are never "optional" in the proto3-optional sense
		su.isOptional = false
		unit.fields = append(unit.fields, su)
	}

	return unit, nil
}

func buildScalarUnit(fd protoreflect.FieldDescriptor, md protoreflect.MessageDescriptor) (*scalarFieldUnit, error) {
	opts := getFieldOpts(fd)

	unit := &scalarFieldUnit{
		fd:            fd,
		bits:          opts.Bits,
		lengthBits:    opts.LengthBits,
		countBits:     opts.CountBits,
		keyBits:       opts.KeyBits,
		keyLengthBits: opts.KeyLengthBits,
	}

	od := fd.ContainingOneof()
	if od != nil && od.IsSynthetic() {
		unit.isOptional = true
	}

	if fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind {
		if !fd.IsList() && !fd.IsMap() {
			if fd.Message().FullName() == "google.protobuf.Timestamp" {
				unit.isTimestamp = true
			} else {
				unit.isMessage = true
			}
		}
	}

	// Validate
	if err := validateScalarUnit(unit, fd, md); err != nil {
		return nil, err
	}

	return unit, nil
}

func validateScalarUnit(u *scalarFieldUnit, fd protoreflect.FieldDescriptor, md protoreflect.MessageDescriptor) error {
	msgName := string(md.FullName())
	fieldName := string(fd.Name())

	// repeated / map: need count_bits
	if fd.IsList() || fd.IsMap() {
		if u.countBits == 0 {
			return &ValidationError{Message: msgName, Field: fieldName, Reason: "repeated/map field requires count_bits > 0"}
		}
	}

	// map key requirements
	if fd.IsMap() {
		keyFd := fd.MapKey()
		switch keyFd.Kind() {
		case protoreflect.StringKind, protoreflect.BytesKind:
			if u.keyLengthBits == 0 {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "map with string/bytes key requires key_length_bits > 0"}
			}
		default:
			if u.keyBits == 0 {
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
		if u.bits != 0 && u.bits != 1 {
			return &ValidationError{Message: msgName, Field: fieldName, Reason: "bool field: bits must be 0 or 1"}
		}
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		if !fd.IsMap() && !fd.IsList() {
			if u.bits == 0 {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "integer field requires bits > 0"}
			}
		} else if u.bits == 0 {
			return &ValidationError{Message: msgName, Field: fieldName, Reason: "integer element requires bits > 0"}
		}
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		if u.bits == 0 {
			return &ValidationError{Message: msgName, Field: fieldName, Reason: "integer field requires bits > 0"}
		}
	case protoreflect.FloatKind:
		fo := getFieldOpts(fd)
		if fo.Fixed != nil || fo.Ufixed != nil {
			if fo.Fixed != nil && fo.Ufixed != nil {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "float field: fixed and ufixed are mutually exclusive"}
			}
			if u.bits == 0 || u.bits > 32 {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "float field with fixed/ufixed: bits must be 1..32"}
			}
		} else if u.bits != 0 && u.bits != 16 && u.bits != 32 {
			return &ValidationError{Message: msgName, Field: fieldName, Reason: "float field: bits must be 0, 16, or 32"}
		}
	case protoreflect.DoubleKind:
		fo := getFieldOpts(fd)
		if fo.Fixed != nil || fo.Ufixed != nil {
			if fo.Fixed != nil && fo.Ufixed != nil {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "double field: fixed and ufixed are mutually exclusive"}
			}
			if u.bits == 0 || u.bits > 64 {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "double field with fixed/ufixed: bits must be 1..64"}
			}
		} else if u.bits != 0 && u.bits != 16 && u.bits != 32 && u.bits != 64 {
			return &ValidationError{Message: msgName, Field: fieldName, Reason: "double field: bits must be 0, 16, 32, or 64"}
		}
	case protoreflect.StringKind, protoreflect.BytesKind:
		if u.lengthBits == 0 {
			return &ValidationError{Message: msgName, Field: fieldName, Reason: "string/bytes field requires length_bits > 0"}
		}
	case protoreflect.EnumKind:
		if u.bits == 0 {
			return &ValidationError{Message: msgName, Field: fieldName, Reason: "enum field requires bits > 0"}
		}
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if u.isTimestamp {
			fo := getFieldOpts(fd)
			if fo.Fixed != nil || fo.Ufixed != nil {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "timestamp field: incompatible with fixed/ufixed"}
			}
			if u.lengthBits != 0 || u.countBits != 0 {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "timestamp field: incompatible with length_bits/count_bits"}
			}
			if u.bits > 64 {
				return &ValidationError{Message: msgName, Field: fieldName, Reason: "timestamp field: bits must be 0..64"}
			}
		}
		// nested message: no annotation required, validated recursively
	}

	return nil
}
