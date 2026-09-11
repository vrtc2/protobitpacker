package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "protoc-gen-bitpacker-c: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	req := &pluginpb.CodeGeneratorRequest{}
	if err := proto.Unmarshal(in, req); err != nil {
		return err
	}
	fillGoPackages(req)

	var flags flag.FlagSet
	gen, err := protogen.Options{ParamFunc: flags.Set}.New(req)
	if err != nil {
		return err
	}
	// Declare proto3 optional support so buf doesn't warn.
	gen.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)

	if err := generate(gen); err != nil {
		gen.Error(err)
	}
	out, err := proto.Marshal(gen.Response())
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(out)
	return err
}

// fillGoPackages gives every file without a go_package a placeholder one.
// The C output never uses Go import paths, but protogen refuses to load files
// without them, which would force users to enable buf managed mode just for C.
func fillGoPackages(req *pluginpb.CodeGeneratorRequest) {
	for _, fd := range req.GetProtoFile() {
		if fd.GetOptions().GetGoPackage() != "" {
			continue
		}
		if fd.Options == nil {
			fd.Options = &descriptorpb.FileOptions{}
		}
		fd.Options.GoPackage = proto.String("bitpacker-c/" + path.Dir(fd.GetName()))
	}
}

func generate(gen *protogen.Plugin) error {
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
