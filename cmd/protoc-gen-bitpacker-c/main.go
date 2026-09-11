package main

import (
	"flag"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"
)

func main() {
	var flags flag.FlagSet
	protogen.Options{ParamFunc: flags.Set}.Run(func(gen *protogen.Plugin) error {
		// Declare proto3 optional support so buf doesn't warn.
		gen.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)

		// buf calls this plugin once per file-to-generate, so we emit the runtime
		// header only when the current invocation actually generates something.
		runtimeEmitted := false
		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}
			// Check whether this file has any bitpacker-annotated messages before
			// emitting the runtime header, to avoid producing it for unannotated files
			// (e.g. options.proto itself).
			if !runtimeEmitted && fileHasBitpackerMessages(f) {
				emitRuntime(gen)
				runtimeEmitted = true
			}
			if err := generateFile(gen, f); err != nil {
				return err
			}
		}
		return nil
	})
}

// fileHasBitpackerMessages returns true if any message in f has valid bitpacker annotations.
func fileHasBitpackerMessages(f *protogen.File) bool {
	for _, msg := range f.Messages {
		if msgHasBitpackerUnits(msg) {
			return true
		}
	}
	return false
}

func msgHasBitpackerUnits(msg *protogen.Message) bool {
	if msg.Desc.IsMapEntry() {
		return false
	}
	units, _ := collectUnits(msg)
	if units != nil {
		return true
	}
	for _, nested := range msg.Messages {
		if msgHasBitpackerUnits(nested) {
			return true
		}
	}
	return false
}

// emitRuntime writes bitpacker_runtime.h into the output root.
func emitRuntime(gen *protogen.Plugin) {
	g := gen.NewGeneratedFile("bitpacker_runtime.h", "")
	g.P(runtimeHeader)
}
