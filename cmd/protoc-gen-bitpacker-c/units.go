package main

import (
	"github.com/vrtc2/protobitpacker/bitpacker"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// unit is either a scalarUnit or a oneofUnit.
type unit interface{ isUnit() }

type scalarUnit struct {
	fd          protoreflect.FieldDescriptor
	fo          bpOpts
	isOptional  bool
	isMessage   bool
	isTimestamp bool
}

func (s scalarUnit) isUnit() {}

type oneofFieldUnit struct {
	fd protoreflect.FieldDescriptor
	fo bpOpts
}

type oneofUnit struct {
	od           protoreflect.OneofDescriptor
	selectorBits uint32
	fields       []oneofFieldUnit
}

func (o oneofUnit) isUnit() {}

// bpOpts holds the resolved bitpacker annotation values for a field.
type bpOpts struct {
	bits          uint32
	lengthBits    uint32
	countBits     uint32
	isFixed       bool
	isSigned      bool
	decimalPlaces uint32
}

func bpFieldOpts(fd protoreflect.FieldDescriptor) bpOpts {
	fo := bitpacker.GetFieldOpts(fd)
	o := bpOpts{
		bits:       fo.GetBits(),
		lengthBits: fo.GetLengthBits(),
		countBits:  fo.GetCountBits(),
	}
	if fo.GetFixed() != nil {
		o.isFixed = true
		o.isSigned = true
		o.decimalPlaces = fo.GetFixed().GetDecimalPlaces()
	} else if fo.GetUfixed() != nil {
		o.isFixed = true
		o.isSigned = false
		o.decimalPlaces = fo.GetUfixed().GetDecimalPlaces()
	}
	return o
}

// collectUnits builds the ordered list of encoding units for a message,
// matching the order produced by bitpacker.AnalyzeMessage.
// Returns (nil, nil) if the message has missing/invalid bitpacker annotations
// (i.e. it is not a bitpacker-annotated message and should be skipped).
func collectUnits(msg *protogen.Message) ([]unit, error) {
	if msg.Desc.IsMapEntry() {
		return nil, nil
	}
	schema, err := bitpacker.AnalyzeMessage(msg.Desc)
	if err != nil {
		if _, ok := err.(*bitpacker.ValidationError); ok {
			return nil, nil // not a bitpacker-annotated message, skip silently
		}
		return nil, err
	}

	// Build a lookup from field number → *protogen.Field for oneof field resolution
	fieldByNum := map[protoreflect.FieldNumber]*protogen.Field{}
	for _, f := range msg.Fields {
		fieldByNum[f.Desc.Number()] = f
	}

	var units []unit
	for _, eu := range schema.Units {
		switch u := eu.(type) {
		case *bitpacker.ScalarFieldUnit:
			su := scalarUnit{
				fd:          u.Fd,
				fo:          bpFieldOpts(u.Fd),
				isOptional:  u.IsOptional,
				isMessage:   u.IsMessage,
				isTimestamp: u.IsTimestamp,
			}
			units = append(units, su)

		case *bitpacker.OneofUnit:
			ou := oneofUnit{
				od:           u.Od,
				selectorBits: u.SelectorBits,
			}
			for _, sf := range u.Fields {
				ou.fields = append(ou.fields, oneofFieldUnit{
					fd: sf.Fd,
					fo: bpFieldOpts(sf.Fd),
				})
			}
			units = append(units, ou)
		}
	}
	return units, nil
}
