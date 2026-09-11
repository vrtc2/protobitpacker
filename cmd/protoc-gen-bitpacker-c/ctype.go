package main

import (
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// cScalarType returns the C type for a scalar field kind.
func cScalarType(kind protoreflect.Kind) string {
	switch kind {
	case protoreflect.BoolKind:
		return "bool"
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "uint32_t"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return "int32_t"
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "uint64_t"
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return "int64_t"
	case protoreflect.FloatKind:
		return "float"
	case protoreflect.DoubleKind:
		return "double"
	default:
		return "uint32_t"
	}
}

// cEnumTypeName returns the C typedef name for a proto enum.
// e.g. "bitpacker.v1.example.SensorStatus" → "SensorStatus_t"
func cEnumTypeName(ed protoreflect.EnumDescriptor) string {
	return fmt.Sprintf("%s_t", ed.Name())
}

// cMsgTypeName returns the C typedef name for a proto message.
// e.g. "bitpacker.v1.example.SensorReading" → "SensorReading"
func cMsgTypeName(md protoreflect.MessageDescriptor) string {
	return string(md.Name())
}

// cOneofWhichTypeName returns the C enum typedef name for the oneof discriminant.
// e.g. msg "Packet", oneof "payload" → "Packet_which_payload_t"
func cOneofWhichTypeName(msgName string, oneofName string) string {
	return fmt.Sprintf("%s_which_%s_t", msgName, oneofName)
}

// cOneofEnumerantName returns a single enumerant in the discriminant enum.
// e.g. msgName="Packet" oneofName="payload" fieldName="raw" → "PACKET_WHICH_PAYLOAD_RAW"
func cOneofEnumerantName(msgName, oneofName, fieldName string) string {
	return fmt.Sprintf("%s_WHICH_%s_%s", toUpper(msgName), toUpper(oneofName), toUpper(fieldName))
}

// cOneofNoneEnumerant returns the "none" enumerant.
func cOneofNoneEnumerant(msgName, oneofName string) string {
	return fmt.Sprintf("%s_WHICH_%s_NONE", toUpper(msgName), toUpper(oneofName))
}

// maxArraySize returns (1<<bits) - 1, the maximum element count for a repeated/string field.
func maxArraySize(bits uint32) uint32 {
	if bits == 0 {
		return 0
	}
	return (1 << bits) - 1
}

// toUpper converts CamelCase or snake_case to UPPER_SNAKE_CASE naively.
func toUpper(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			b = append(b, c-32)
		} else {
			b = append(b, c)
		}
	}
	return string(b)
}
