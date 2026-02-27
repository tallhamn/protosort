package main

import (
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var defaultOpts = Options{Quiet: true}

// ============================================================
// Golden-file integration tests
// ============================================================

func TestGolden(t *testing.T) {
	pairs, err := filepath.Glob("testdata/*_input.proto")
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) == 0 {
		t.Fatal("no golden-file test pairs found in testdata/")
	}

	// Golden files requiring non-default options are tested separately.
	skipGolden := map[string]bool{"section_headers": true}

	for _, inputPath := range pairs {
		expectedPath := strings.Replace(inputPath, "_input.proto", "_expected.proto", 1)
		name := strings.TrimPrefix(inputPath, "testdata/")
		name = strings.TrimSuffix(name, "_input.proto")

		if skipGolden[name] {
			continue
		}

		t.Run(name, func(t *testing.T) {
			inputBytes, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatalf("reading input: %v", err)
			}
			expectedBytes, err := os.ReadFile(expectedPath)
			if err != nil {
				t.Fatalf("reading expected %s: %v", expectedPath, err)
			}

			output, _, err := Sort(string(inputBytes), defaultOpts)
			if err != nil {
				t.Fatalf("Sort failed: %v", err)
			}

			expected := string(expectedBytes)
			if output != expected {
				t.Errorf("output mismatch.\nDiff:\n%s",
					DiffStrings(expected, output, "expected", "got"))
			}
		})
	}
}

// ============================================================
// Idempotency: run Sort twice on every fixture, second pass = no change
// ============================================================

func TestIdempotency_AllFixtures(t *testing.T) {
	pairs, err := filepath.Glob("testdata/*_input.proto")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(pairs) == 0 {
		t.Fatal("no fixture files found")
	}
	for _, inputPath := range pairs {
		name := strings.TrimPrefix(inputPath, "testdata/")
		name = strings.TrimSuffix(name, "_input.proto")

		t.Run(name, func(t *testing.T) {
			inputBytes, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatalf("reading input: %v", err)
			}

			pass1, _, err := Sort(string(inputBytes), defaultOpts)
			if err != nil {
				t.Fatalf("first Sort failed: %v", err)
			}

			pass2, _, err := Sort(pass1, defaultOpts)
			if err != nil {
				t.Fatalf("second Sort failed: %v", err)
			}

			if pass1 != pass2 {
				t.Errorf("not idempotent.\nPass 1:\n%s\nPass 2:\n%s", pass1, pass2)
			}
		})
	}
}

// ============================================================
// Content integrity: every fixture passes verification
// ============================================================

func TestContentIntegrity_AllFixtures(t *testing.T) {
	pairs, err := filepath.Glob("testdata/*_input.proto")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(pairs) == 0 {
		t.Fatal("no fixture files found")
	}
	for _, inputPath := range pairs {
		name := strings.TrimPrefix(inputPath, "testdata/")
		name = strings.TrimSuffix(name, "_input.proto")

		t.Run(name, func(t *testing.T) {
			inputBytes, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatalf("reading input: %v", err)
			}
			original := string(inputBytes)
			sorted, _, err := Sort(original, defaultOpts)
			if err != nil {
				t.Fatalf("Sort failed: %v", err)
			}
			if err := verifyContentIntegrity(original, sorted, defaultOpts); err != nil {
				t.Errorf("integrity check failed: %v", err)
			}
		})
	}
}

// ============================================================
// Scanner tests
// ============================================================

func TestScan_BasicElements(t *testing.T) {
	input := `syntax = "proto3";

package test.v1;

import "google/protobuf/timestamp.proto";

option go_package = "test/v1";

message Foo {
  string name = 1;
}

enum Bar {
  BAR_UNSPECIFIED = 0;
}

service Svc {
  rpc Get(GetReq) returns (GetRes);
}
`
	blocks, err := ScanFile(input)
	if err != nil {
		t.Fatalf("ScanFile: %v", err)
	}

	want := []struct {
		kind BlockKind
		name string
	}{
		{BlockSyntax, "proto3"},
		{BlockPackage, "test.v1"},
		{BlockImport, "google/protobuf/timestamp.proto"},
		{BlockOption, "go_package"},
		{BlockMessage, "Foo"},
		{BlockEnum, "Bar"},
		{BlockService, "Svc"},
	}

	if len(blocks) != len(want) {
		var got []string
		for _, b := range blocks {
			got = append(got, b.Kind.String()+":"+b.Name)
		}
		t.Fatalf("expected %d blocks, got %d: %v", len(want), len(blocks), got)
	}
	for i, w := range want {
		if blocks[i].Kind != w.kind || blocks[i].Name != w.name {
			t.Errorf("block[%d]: want %v:%q, got %v:%q",
				i, w.kind, w.name, blocks[i].Kind, blocks[i].Name)
		}
	}
}

func TestScan_OptionWithBraces(t *testing.T) {
	input := `syntax = "proto3";

option (google.api.http) = {
  get: "/v1/{id}"
};

message Foo {
  string val = 1;
}
`
	blocks, err := ScanFile(input)
	if err != nil {
		t.Fatalf("ScanFile: %v", err)
	}
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	if blocks[1].Kind != BlockOption {
		t.Errorf("block[1]: want option, got %v", blocks[1].Kind)
	}
}

func TestScan_BlockComment(t *testing.T) {
	input := `syntax = "proto3";

/* Block comment
   spanning multiple lines. */
message Foo {
  string val = 1;
}
`
	blocks, err := ScanFile(input)
	if err != nil {
		t.Fatalf("ScanFile: %v", err)
	}
	var msg *Block
	for _, b := range blocks {
		if b.Kind == BlockMessage {
			msg = b
		}
	}
	if msg == nil {
		t.Fatal("no message block")
	}
	if !strings.Contains(msg.Comments, "Block comment") {
		t.Errorf("block comment not associated with message: %q", msg.Comments)
	}
}

func TestScan_StringWithBraces(t *testing.T) {
	input := `syntax = "proto3";

message Foo {
  string pattern = 1; // contains "{bar}"
}
`
	blocks, err := ScanFile(input)
	if err != nil {
		t.Fatalf("ScanFile: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
}

func TestScan_NestedMessages(t *testing.T) {
	input := `syntax = "proto3";

message Outer {
  message Inner {
    string val = 1;
  }
  Inner inner = 1;
}
`
	blocks, err := ScanFile(input)
	if err != nil {
		t.Fatalf("ScanFile: %v", err)
	}
	// Should be 2 blocks: syntax + Outer (Inner is nested, not top-level)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
}

func TestScan_ExtendBlock(t *testing.T) {
	input := `syntax = "proto3";

extend google.protobuf.MessageOptions {
  string my_option = 51234;
}

message Foo {
  string val = 1;
}
`
	blocks, err := ScanFile(input)
	if err != nil {
		t.Fatalf("ScanFile: %v", err)
	}
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	if blocks[1].Kind != BlockExtend {
		t.Errorf("block[1]: want extend, got %v", blocks[1].Kind)
	}
}

func TestScan_ImportPublic(t *testing.T) {
	input := `syntax = "proto3";

import public "other.proto";

message Foo {
  string val = 1;
}
`
	blocks, err := ScanFile(input)
	if err != nil {
		t.Fatalf("ScanFile: %v", err)
	}
	var imp *Block
	for _, b := range blocks {
		if b.Kind == BlockImport {
			imp = b
		}
	}
	if imp == nil {
		t.Fatal("no import block")
	}
	if imp.Name != "other.proto" {
		t.Errorf("import name: want %q, got %q", "other.proto", imp.Name)
	}
}

// ============================================================
// Reference counting tests (table-driven)
// ============================================================

func TestRefCounts(t *testing.T) {
	tests := []struct {
		name   string
		blocks []*Block
		want   map[string]int
	}{
		{
			name: "field type counts as reference",
			blocks: []*Block{
				{Kind: BlockMessage, Name: "A", DeclText: "message A { B b = 1; }"},
				{Kind: BlockMessage, Name: "B", DeclText: "message B { string v = 1; }"},
			},
			want: map[string]int{"B": 1},
		},
		{
			name: "map value type counts",
			blocks: []*Block{
				{Kind: BlockMessage, Name: "A", DeclText: "message A { map<string, V> m = 1; }"},
				{Kind: BlockMessage, Name: "V", DeclText: "message V { string v = 1; }"},
			},
			want: map[string]int{"V": 1},
		},
		{
			name: "oneof variant counts",
			blocks: []*Block{
				{Kind: BlockMessage, Name: "E", DeclText: "message E {\n  oneof p {\n    X x = 1;\n    Y y = 2;\n  }\n}"},
				{Kind: BlockMessage, Name: "X", DeclText: "message X { string v = 1; }"},
				{Kind: BlockMessage, Name: "Y", DeclText: "message Y { string v = 1; }"},
			},
			want: map[string]int{"X": 1, "Y": 1},
		},
		{
			name: "RPC request/response counts",
			blocks: []*Block{
				{Kind: BlockService, Name: "S", DeclText: "service S { rpc Do(Req) returns (Res); }"},
				{Kind: BlockMessage, Name: "Req", DeclText: "message Req { string v = 1; }"},
				{Kind: BlockMessage, Name: "Res", DeclText: "message Res { string v = 1; }"},
			},
			want: map[string]int{"Req": 1, "Res": 1},
		},
		{
			name: "multiple fields same type from one message = 1 reference",
			blocks: []*Block{
				{Kind: BlockMessage, Name: "A", DeclText: "message A { T x = 1; T y = 2; }"},
				{Kind: BlockMessage, Name: "T", DeclText: "message T { string v = 1; }"},
			},
			want: map[string]int{"T": 1},
		},
		{
			name: "two messages referencing same type = 2 references",
			blocks: []*Block{
				{Kind: BlockMessage, Name: "A", DeclText: "message A { T x = 1; }"},
				{Kind: BlockMessage, Name: "B", DeclText: "message B { T y = 1; }"},
				{Kind: BlockMessage, Name: "T", DeclText: "message T { string v = 1; }"},
			},
			want: map[string]int{"T": 2},
		},
		{
			name: "imported types ignored",
			blocks: []*Block{
				{Kind: BlockMessage, Name: "A", DeclText: "message A { google.protobuf.Timestamp ts = 1; }"},
			},
			want: map[string]int{},
		},
		{
			name: "scalar types ignored",
			blocks: []*Block{
				{Kind: BlockMessage, Name: "A", DeclText: "message A { string s = 1; int32 n = 2; bool b = 3; }"},
			},
			want: map[string]int{},
		},
		{
			name: "circular references boosted to core",
			blocks: []*Block{
				{Kind: BlockMessage, Name: "A", DeclText: "message A { B b = 1; }"},
				{Kind: BlockMessage, Name: "B", DeclText: "message B { A a = 1; }"},
			},
			want: map[string]int{"A": 2, "B": 2},
		},
		{
			name: "enum counts as type reference",
			blocks: []*Block{
				{Kind: BlockMessage, Name: "A", DeclText: "message A { Status s = 1; }"},
				{Kind: BlockEnum, Name: "Status", DeclText: "enum Status { UNKNOWN = 0; }"},
			},
			want: map[string]int{"Status": 1},
		},
		{
			name: "qualified import does not collide with local type",
			blocks: []*Block{
				{Kind: BlockMessage, Name: "A", DeclText: "message A { other.pkg.Foo f = 1; }"},
				{Kind: BlockMessage, Name: "Foo", DeclText: "message Foo { string v = 1; }"},
			},
			want: map[string]int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counts := BuildRefCounts(tt.blocks)
			for name, wantCount := range tt.want {
				if counts[name] != wantCount {
					t.Errorf("refCount[%s]: want %d, got %d", name, wantCount, counts[name])
				}
			}
			// Check no unexpected counts
			for name, count := range counts {
				if _, ok := tt.want[name]; !ok && count > 0 {
					t.Errorf("unexpected refCount[%s] = %d", name, count)
				}
			}
		})
	}
}

// ============================================================
// RPC extraction
// ============================================================

func TestExtractRPCs(t *testing.T) {
	block := &Block{
		Kind: BlockService,
		Name: "Svc",
		DeclText: `service Svc {
  rpc Alpha(AlphaReq) returns (AlphaRes);
  rpc Beta(BetaReq) returns (BetaRes);
  rpc Gamma(GammaReq) returns (GammaRes);
}`,
	}
	rpcs := ExtractRPCs(block)
	if len(rpcs) != 3 {
		t.Fatalf("want 3 RPCs, got %d", len(rpcs))
	}
	want := []RPC{
		{"Alpha", "AlphaReq", "AlphaRes"},
		{"Beta", "BetaReq", "BetaRes"},
		{"Gamma", "GammaReq", "GammaRes"},
	}
	for i, w := range want {
		if rpcs[i] != w {
			t.Errorf("rpc[%d]: want %+v, got %+v", i, w, rpcs[i])
		}
	}
}

func TestExtractRPCs_QualifiedTypes(t *testing.T) {
	block := &Block{
		Kind:     BlockService,
		Name:     "Svc",
		DeclText: `service Svc { rpc Do(pkg.v1.Req) returns (pkg.v1.Res); }`,
	}
	rpcs := ExtractRPCs(block)
	if len(rpcs) != 1 {
		t.Fatalf("want 1 RPC, got %d", len(rpcs))
	}
	// Qualified type names are preserved as-is (they won't match local types)
	if rpcs[0].RequestType != "pkg.v1.Req" || rpcs[0].ResponseType != "pkg.v1.Res" {
		t.Errorf("expected qualified types preserved, got %+v", rpcs[0])
	}
}

func TestExtractRPCs_Streaming(t *testing.T) {
	block := &Block{
		Kind: BlockService,
		Name: "Svc",
		DeclText: `service Svc {
  rpc UnaryToStream(Req) returns (stream Res);
  rpc StreamToUnary(stream Req2) returns (Res2);
  rpc BiDi(stream BidiReq) returns (stream BidiRes);
}`,
	}
	rpcs := ExtractRPCs(block)
	if len(rpcs) != 3 {
		t.Fatalf("want 3 RPCs, got %d", len(rpcs))
	}
	want := []RPC{
		{"UnaryToStream", "Req", "Res"},
		{"StreamToUnary", "Req2", "Res2"},
		{"BiDi", "BidiReq", "BidiRes"},
	}
	for i, w := range want {
		if rpcs[i] != w {
			t.Errorf("rpc[%d]: want %+v, got %+v", i, w, rpcs[i])
		}
	}
}

func TestExtractRPCs_NonService(t *testing.T) {
	block := &Block{Kind: BlockMessage, Name: "Foo", DeclText: "message Foo {}"}
	if rpcs := ExtractRPCs(block); rpcs != nil {
		t.Errorf("expected nil RPCs for non-service block, got %v", rpcs)
	}
}

// ============================================================
// Field type extraction
// ============================================================

func TestExtractFieldTypes_Regular(t *testing.T) {
	block := &Block{
		Kind: BlockMessage, Name: "M",
		DeclText: `message M {
  string id = 1;
  Foo foo = 2;
  repeated Bar bars = 3;
  optional Baz baz = 4;
}`,
	}
	types := ExtractFieldTypes(block)
	want := map[string]bool{"Foo": true, "Bar": true, "Baz": true}
	if len(types) != len(want) {
		t.Fatalf("want %d types, got %d: %v", len(want), len(types), types)
	}
	for _, typ := range types {
		if !want[typ] {
			t.Errorf("unexpected type %q", typ)
		}
	}
}

func TestExtractFieldTypes_MapValue(t *testing.T) {
	block := &Block{
		Kind: BlockMessage, Name: "M",
		DeclText: `message M { map<string, Setting> m = 1; }`,
	}
	types := ExtractFieldTypes(block)
	if len(types) != 1 || types[0] != "Setting" {
		t.Errorf("want [Setting], got %v", types)
	}
}

func TestExtractFieldTypes_Oneof(t *testing.T) {
	block := &Block{
		Kind: BlockMessage, Name: "M",
		DeclText: `message M {
  oneof payload {
    CreateEvt create = 1;
    DeleteEvt delete = 2;
  }
}`,
	}
	types := ExtractFieldTypes(block)
	want := map[string]bool{"CreateEvt": true, "DeleteEvt": true}
	for _, typ := range types {
		if !want[typ] {
			t.Errorf("unexpected type %q", typ)
		}
		delete(want, typ)
	}
	for typ := range want {
		t.Errorf("missing type %q", typ)
	}
}

func TestExtractFieldTypes_IgnoresScalars(t *testing.T) {
	block := &Block{
		Kind: BlockMessage, Name: "M",
		DeclText: `message M {
  string s = 1;
  int32 n = 2;
  bool b = 3;
  double d = 4;
  bytes raw = 5;
}`,
	}
	types := ExtractFieldTypes(block)
	if len(types) != 0 {
		t.Errorf("expected no types for scalar-only message, got %v", types)
	}
}

// ============================================================
// Ordering rule tests
// ============================================================

func TestSort_ServiceMovesToTop(t *testing.T) {
	input := `syntax = "proto3";

message Foo {
  string name = 1;
}

service MySvc {
  rpc Get(GetReq) returns (GetRes);
}

message GetReq {
  string id = 1;
}

message GetRes {
  Foo foo = 1;
}
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	assertOrder(t, output, "service MySvc", "message GetReq")
}

func TestSort_RPCPairOrder(t *testing.T) {
	input := `syntax = "proto3";

message BRes { string v = 1; }
message AReq { string v = 1; }

service S {
  rpc A(AReq) returns (ARes);
  rpc B(BReq) returns (BRes);
}

message ARes { string v = 1; }
message BReq { string v = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	assertOrder(t, output,
		"message AReq", "message ARes",
		"message BReq", "message BRes")
}

func TestSort_SharedRPCMessage_AppearsAtFirstUse(t *testing.T) {
	input := `syntax = "proto3";

message SharedReq { string id = 1; }
message Res1 { string v = 1; }
message Res2 { string v = 1; }

service S {
  rpc First(SharedReq) returns (Res1);
  rpc Second(SharedReq) returns (Res2);
}
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	// SharedReq should appear once, at first RPC position
	assertOrder(t, output, "service S", "message SharedReq", "message Res1")
	// Should only appear once
	if strings.Count(output, "message SharedReq") != 1 {
		t.Error("SharedReq should appear exactly once")
	}
}

func TestSort_CoreAlphabetical(t *testing.T) {
	input := `syntax = "proto3";

message Zebra { string v = 1; }
message Apple { string v = 1; }
message U1 { Zebra z = 1; }
message U2 { Zebra z = 1; }
message U3 { Apple a = 1; }
message U4 { Apple a = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	assertOrder(t, output, "message Apple", "message Zebra")
}

func TestSort_HelperBeforeConsumer(t *testing.T) {
	input := `syntax = "proto3";

message Consumer { Helper h = 1; }
message Other { Consumer c = 1; }
message Helper { string v = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	assertOrder(t, output, "message Helper", "message Consumer")
}

func TestSort_HelperChainBottomUp(t *testing.T) {
	input := `syntax = "proto3";

message A { B b = 1; }
message C { string v = 1; }
message B { C c = 1; }
message X { A a = 1; }
message Y { A a = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	// A is core (2 refs: X,Y). B is helper for A. C is helper for B.
	// Chain: C, B, A
	assertOrder(t, output, "message C", "message B", "message A")
}

func TestSort_UnreferencedLast(t *testing.T) {
	input := `syntax = "proto3";

message Orphan { string v = 1; }
message Used { string v = 1; }
message C1 { Used u = 1; }
message C2 { Used u = 1; }
`
	output, warnings, err := Sort(input, Options{})
	if err != nil {
		t.Fatal(err)
	}
	assertOrder(t, output, "message Used", "message Orphan")

	found := false
	for _, w := range warnings {
		if strings.Contains(w, "Orphan") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning about unreferenced Orphan")
	}
}

func TestSort_UnreferencedAlphabetical(t *testing.T) {
	input := `syntax = "proto3";

message Zeta { string v = 1; }
message Alpha { string v = 1; }
message Mid { string v = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	assertOrder(t, output, "message Alpha", "message Mid", "message Zeta")
}

func TestSort_NoService_SkipsSection2(t *testing.T) {
	input := `syntax = "proto3";

message Foo { Bar b = 1; }
message Baz { Bar b = 1; }
message Bar { string v = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	// Bar is core (2 refs). Foo and Baz are unreferenced.
	assertOrder(t, output, "message Bar", "message Baz")
	assertOrder(t, output, "message Bar", "message Foo")
	if strings.Contains(output, "service") {
		t.Error("no service should appear in output")
	}
}

func TestSort_EmptyService(t *testing.T) {
	input := `syntax = "proto3";

message Foo { string v = 1; }

service Empty {
}
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	assertOrder(t, output, "service Empty", "message Foo")
}

func TestSort_MultipleServices_PreserveOrder(t *testing.T) {
	input := `syntax = "proto3";

service Second { rpc Do(B) returns (B); }
service First { rpc Do(A) returns (A); }
message A { string v = 1; }
message B { string v = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	// Services preserve original declaration order
	assertOrder(t, output, "service Second", "service First")
}

func TestSort_TypeUsedAsBothRPCAndField(t *testing.T) {
	input := `syntax = "proto3";

service S { rpc Do(Shared) returns (Res); }
message Res { Shared s = 1; }
message Shared { string v = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	// Shared is RPC request → Section 2 takes priority
	assertOrder(t, output, "service S", "message Shared")
}

func TestSort_StreamingRPC_ClassifiesCorrectly(t *testing.T) {
	input := `syntax = "proto3";

service S {
  rpc ServerStream(Req) returns (stream Res);
  rpc ClientStream(stream Req2) returns (Res2);
  rpc BiDi(stream BidiReq) returns (stream BidiRes);
}

message Req { string v = 1; }
message Res { string v = 1; }
message Req2 { string v = 1; }
message Res2 { string v = 1; }
message BidiReq { string v = 1; }
message BidiRes { string v = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	// All request/response messages should follow the service in RPC order
	assertOrder(t, output, "service S",
		"message Req", "message Res",
		"message Req2", "message Res2",
		"message BidiReq", "message BidiRes")
}

func TestSort_QualifiedRPCType_NoCollision(t *testing.T) {
	input := `syntax = "proto3";

service S { rpc Do(other.pkg.Empty) returns (other.pkg.Result); }
message Empty { string v = 1; }
message Result { string v = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	// Empty and Result should be unreferenced (the RPC uses imported types)
	// They should come after the service, sorted alphabetically
	assertOrder(t, output, "service S", "message Empty", "message Result")
}

func TestSort_CircularReferences_BothCore(t *testing.T) {
	input := `syntax = "proto3";

message A { B b = 1; }
message B { A a = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	// Both should be present and sorted alphabetically (both core)
	if !strings.Contains(output, "message A") || !strings.Contains(output, "message B") {
		t.Error("both types should appear in output")
	}
	assertOrder(t, output, "message A", "message B")
}

// ============================================================
// Header sorting tests
// ============================================================

func TestSort_OptionsAlphabetized(t *testing.T) {
	input := `syntax = "proto3";

option java_package = "com.test";
option go_package = "test/v1";
option cc_enable_arenas = "true";
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	assertOrder(t, output, "cc_enable_arenas", "go_package", "java_package")
}

func TestSort_ImportsAlphabetized(t *testing.T) {
	input := `syntax = "proto3";

import "z/file.proto";
import "a/file.proto";
import "m/file.proto";
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	assertOrder(t, output, `"a/file.proto"`, `"m/file.proto"`, `"z/file.proto"`)
}

func TestSort_LicenseStaysAtTop(t *testing.T) {
	input := `// Copyright 2024 Test Corp.
// All rights reserved.

syntax = "proto3";

message Foo { string v = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(output, "// Copyright") {
		t.Errorf("license should be first line, got:\n%s", output[:80])
	}
	assertOrder(t, output, "Copyright", "syntax")
}

func TestSort_SyntaxBeforePackageBeforeOptionsBeforeImports(t *testing.T) {
	input := `syntax = "proto3";

import "foo.proto";
option go_package = "test";
package test.v1;
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	assertOrder(t, output, "syntax", "package", "option", "import")
}

// ============================================================
// Comment association tests
// ============================================================

func TestSort_LeadingCommentTravels(t *testing.T) {
	input := `syntax = "proto3";

// Bravo's comment.
message Bravo { string v = 1; }

// Alpha's comment.
message Alpha { string v = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	// Both are unreferenced → alphabetical → Alpha before Bravo
	assertOrder(t, output, "Alpha's comment", "message Alpha", "Bravo's comment", "message Bravo")
}

func TestSort_DetachedCommentTravels(t *testing.T) {
	input := `syntax = "proto3";

// Detached comment.

// Leading comment.
message Foo { string v = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "Detached comment") {
		t.Error("detached comment should be preserved")
	}
	if !strings.Contains(output, "Leading comment") {
		t.Error("leading comment should be preserved")
	}
}

func TestSort_InteriorCommentUnchanged(t *testing.T) {
	input := `syntax = "proto3";

message Foo {
  // Interior comment.
  string val = 1; // Inline comment.
}
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "// Interior comment.") {
		t.Error("interior comment should be preserved")
	}
	if !strings.Contains(output, "// Inline comment.") {
		t.Error("inline comment should be preserved")
	}
}

// ============================================================
// Whitespace tests
// ============================================================

func TestSort_NormalizesInterBlockSpacing(t *testing.T) {
	input := `syntax = "proto3";



message Foo { string v = 1; }


message Bar { string v = 1; }
message Baz { string v = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "\n\n\n") {
		t.Errorf("should not have triple newlines:\n%q", output)
	}
}

func TestSort_PreservesInteriorWhitespace(t *testing.T) {
	input := `syntax = "proto3";

message Foo {
  string   name    = 1;

  int32    age     = 2;
}
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	// The interior whitespace should be byte-identical
	if !strings.Contains(output, "string   name    = 1;") {
		t.Error("interior whitespace should be preserved")
	}
	if !strings.Contains(output, "int32    age     = 2;") {
		t.Error("interior whitespace should be preserved")
	}
}

func TestSort_FileEndsWithNewline(t *testing.T) {
	input := `syntax = "proto3";

message Foo { string v = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(output, "\n") {
		t.Error("file should end with newline")
	}
	if strings.HasSuffix(output, "\n\n") {
		t.Error("file should not end with blank line")
	}
}

// ============================================================
// Edge cases
// ============================================================

func TestSort_Proto2Rejected(t *testing.T) {
	input := `syntax = "proto2";

message Foo {
  required string name = 1;
}
`
	_, _, err := Sort(input, Options{})
	if err == nil || !strings.Contains(err.Error(), "proto2") {
		t.Errorf("expected proto2 error, got: %v", err)
	}
}

func TestSort_EmptyFile(t *testing.T) {
	output, _, err := Sort("", defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	if output != "" {
		t.Error("empty input should produce empty output")
	}
}

func TestSort_HeaderOnly(t *testing.T) {
	input := `syntax = "proto3";

package test.v1;
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "syntax") || !strings.Contains(output, "package") {
		t.Error("header should be preserved")
	}
}

func TestSort_SingleDeclaration(t *testing.T) {
	input := `syntax = "proto3";

message Only { string v = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "message Only") {
		t.Error("single declaration should be preserved")
	}
}

// ============================================================
// --strip-commented-code tests
// ============================================================

func TestSort_StripCommentedCode(t *testing.T) {
	input := `syntax = "proto3";

// rpc OldMethod(OldReq) returns (OldRes);

// This is a real comment about Foo.
message Foo { string v = 1; }
`
	output, _, err := Sort(input, Options{Quiet: true, StripCommented: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "OldMethod") {
		t.Error("commented-out RPC should be stripped")
	}
	if !strings.Contains(output, "real comment about Foo") {
		t.Error("prose comment should be preserved")
	}
}

func TestSort_StripCommentedCode_PreservesProseComments(t *testing.T) {
	input := `syntax = "proto3";

// This describes the purpose of the message.
// It has multiple lines of explanation.
message Foo { string v = 1; }
`
	output, _, err := Sort(input, Options{Quiet: true, StripCommented: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "purpose of the message") {
		t.Error("prose comments should not be stripped")
	}
}

// ============================================================
// Verification tests
// ============================================================

func TestVerifyIntegrity_Pass(t *testing.T) {
	original := `syntax = "proto3";
message Foo { string v = 1; }
message Bar { string v = 1; }
`
	sorted := `syntax = "proto3";
message Bar { string v = 1; }
message Foo { string v = 1; }
`
	if err := verifyContentIntegrity(original, sorted, defaultOpts); err != nil {
		t.Errorf("should pass: %v", err)
	}
}

func TestVerifyIntegrity_MissingDecl(t *testing.T) {
	original := `syntax = "proto3";
message Foo { string v = 1; }
message Bar { string v = 1; }
`
	sorted := `syntax = "proto3";
message Foo { string v = 1; }
`
	if err := verifyContentIntegrity(original, sorted, defaultOpts); err == nil {
		t.Error("should fail for missing declaration")
	}
}

func TestVerifyIntegrity_AlteredBody(t *testing.T) {
	original := `syntax = "proto3";
message Foo { string name = 1; }
`
	sorted := `syntax = "proto3";
message Foo { int32 name = 1; }
`
	if err := verifyContentIntegrity(original, sorted, defaultOpts); err == nil {
		t.Error("should fail for altered body")
	}
}

func TestVerifyIntegrity_ExtraDecl(t *testing.T) {
	original := `syntax = "proto3";
message Foo { string v = 1; }
`
	sorted := `syntax = "proto3";
message Foo { string v = 1; }
message Bar { string v = 1; }
`
	if err := verifyContentIntegrity(original, sorted, defaultOpts); err == nil {
		t.Error("should fail for extra declaration")
	}
}

// ============================================================
// Roundtrip property tests (random valid proto3 files)
// ============================================================

func TestProperty_RandomProtos(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < 100; i++ {
		proto := generateRandomProto(rng)

		output, _, err := Sort(proto, defaultOpts)
		if err != nil {
			t.Fatalf("iteration %d: Sort failed: %v\nInput:\n%s", i, err, proto)
		}

		// Must be idempotent
		output2, _, err := Sort(output, defaultOpts)
		if err != nil {
			t.Fatalf("iteration %d: second Sort failed: %v", i, err)
		}
		if output != output2 {
			t.Errorf("iteration %d: not idempotent", i)
		}

		// Content integrity
		if err := verifyContentIntegrity(proto, output, defaultOpts); err != nil {
			t.Errorf("iteration %d: integrity check failed: %v", i, err)
		}
	}
}

// generateRandomProto creates a random but valid proto3 file.
func generateRandomProto(rng *rand.Rand) string {
	var b strings.Builder
	b.WriteString("syntax = \"proto3\";\n\npackage test.v1;\n")

	// Random set of type names
	allNames := []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Zeta", "Eta", "Theta", "Iota", "Kappa"}
	numTypes := 3 + rng.Intn(8) // 3-10 types
	names := allNames[:numTypes]

	// Optionally add enums
	enumNames := []string{"Status", "Priority", "Category"}
	numEnums := rng.Intn(3) // 0-2 enums
	var enums []string
	for i := 0; i < numEnums; i++ {
		enums = append(enums, enumNames[i])
	}

	// Optionally add a service
	hasService := rng.Intn(3) > 0 // 2/3 chance
	if hasService {
		numRPCs := 1 + rng.Intn(3)
		// Optionally add a comment before the service
		if rng.Intn(2) == 0 {
			b.WriteString("\n// Service for handling operations.\n")
		}
		b.WriteString("service TestSvc {\n")
		for j := 0; j < numRPCs && j*2+1 < len(names); j++ {
			req := names[j*2]
			res := names[j*2+1]
			// Optionally use streaming
			streamPrefix := ""
			streamSuffix := ""
			switch rng.Intn(4) {
			case 1:
				streamSuffix = "stream "
			case 2:
				streamPrefix = "stream "
			case 3:
				streamPrefix = "stream "
				streamSuffix = "stream "
			}
			b.WriteString("  rpc Method" + req + "(" + streamPrefix + req + ") returns (" + streamSuffix + res + ");\n")
		}
		b.WriteString("}\n")
	}

	// Emit enums
	for _, name := range enums {
		if rng.Intn(2) == 0 {
			b.WriteString("\n// " + name + " enum type.\n")
		}
		b.WriteString("enum " + name + " {\n")
		b.WriteString("  " + strings.ToUpper(name) + "_UNSPECIFIED = 0;\n")
		b.WriteString("  " + strings.ToUpper(name) + "_VALUE = 1;\n")
		b.WriteString("}\n")
	}

	// Emit messages in shuffled order
	perm := rng.Perm(len(names))
	for _, idx := range perm {
		name := names[idx]

		// Optionally add a leading comment
		if rng.Intn(3) == 0 {
			b.WriteString("\n// " + name + " is a message type.\n")
		}

		b.WriteString("message " + name + " {\n")
		b.WriteString("  string id = 1;\n")

		fieldNum := 2

		// Randomly reference other types as regular fields
		for _, otherIdx := range rng.Perm(len(names)) {
			other := names[otherIdx]
			if other == name {
				continue
			}
			if rng.Intn(4) == 0 { // 25% chance
				b.WriteString("  " + other + " ref_" + strings.ToLower(other) + " = " + strconv.Itoa(fieldNum) + ";\n")
				fieldNum++
			}
			if fieldNum > 5 {
				break
			}
		}

		// Optionally add a map field
		if fieldNum <= 5 && len(names) > 1 && rng.Intn(4) == 0 {
			other := names[rng.Intn(len(names))]
			if other != name {
				b.WriteString("  map<string, " + other + "> map_" + strings.ToLower(other) + " = " + strconv.Itoa(fieldNum) + ";\n")
				fieldNum++
			}
		}

		// Optionally add a oneof
		if fieldNum <= 5 && len(names) > 2 && rng.Intn(4) == 0 {
			b.WriteString("  oneof payload {\n")
			count := 0
			for _, otherIdx := range rng.Perm(len(names)) {
				other := names[otherIdx]
				if other == name || count >= 2 {
					break
				}
				b.WriteString("    " + other + " oneof_" + strings.ToLower(other) + " = " + strconv.Itoa(fieldNum) + ";\n")
				fieldNum++
				count++
			}
			b.WriteString("  }\n")
		}

		// Optionally reference an enum
		if fieldNum <= 6 && len(enums) > 0 && rng.Intn(3) == 0 {
			e := enums[rng.Intn(len(enums))]
			b.WriteString("  " + e + " " + strings.ToLower(e) + " = " + strconv.Itoa(fieldNum) + ";\n")
		}

		b.WriteString("}\n")
	}

	return b.String()
}

// ============================================================
// Comment association tests (new)
// ============================================================

func TestSort_PreserveDividers(t *testing.T) {
	input := `syntax = "proto3";

// === Messages ===

message Beta { string v = 1; }

message Alpha { string v = 1; }
`
	opts := Options{Quiet: true, PreserveDividers: true}
	output, _, err := Sort(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	// Divider should survive and be attached to the first declaration after it
	if !strings.Contains(output, "=== Messages ===") {
		t.Error("divider comment should be preserved when --preserve-dividers is set")
	}
	// Alpha should still come before Beta (both unreferenced, alphabetical)
	assertOrder(t, output, "message Alpha", "message Beta")
}

func TestSort_DividerDroppedByDefault(t *testing.T) {
	input := `syntax = "proto3";

// === Messages ===

message Beta { string v = 1; }

// --- Services ---
message Alpha { string v = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "=== Messages ===") {
		t.Error("divider should be stripped by default")
	}
	if strings.Contains(output, "--- Services ---") {
		t.Error("divider should be stripped by default")
	}
}

func TestSort_BlockCommentStyleSurvives(t *testing.T) {
	input := `syntax = "proto3";

/* Block-style comment for Foo. */
message Foo { string v = 1; }

// Line-style comment for Bar.
message Bar { string v = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "/* Block-style comment for Foo. */") {
		t.Error("block comment style should be preserved")
	}
	if !strings.Contains(output, "// Line-style comment for Bar.") {
		t.Error("line comment style should be preserved")
	}
}

func TestSort_TrailingCommentOnClosingBrace(t *testing.T) {
	input := `syntax = "proto3";

message Foo {
  string v = 1;
} // end Foo
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "} // end Foo") {
		t.Error("trailing comment on closing brace should be preserved")
	}
}

// ============================================================
// Ordering rule tests (new)
// ============================================================

func TestSort_InterleavedRPCRequestResponse_MultipleServices(t *testing.T) {
	input := `syntax = "proto3";

service Svc1 {
  rpc A(A1Req) returns (A1Res);
}

service Svc2 {
  rpc B(B1Req) returns (B1Res);
}

message B1Res { string v = 1; }
message A1Req { string v = 1; }
message A1Res { string v = 1; }
message B1Req { string v = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	// Services preserve original order, then their RPC messages follow
	assertOrder(t, output, "service Svc1", "service Svc2",
		"message A1Req", "message A1Res",
		"message B1Req", "message B1Res")
}

func TestSort_Section2MessageAlsoUsedAsFieldType_AppearsOnce(t *testing.T) {
	input := `syntax = "proto3";

service S { rpc Do(Req) returns (Res); }
message Req { string v = 1; }
message Res { Req nested = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	// Req is both an RPC type and referenced as field type — Section 2 wins
	if strings.Count(output, "message Req") != 1 {
		t.Error("Req should appear exactly once")
	}
	assertOrder(t, output, "service S", "message Req", "message Res")
}

// ============================================================
// Reference counting tests (new)
// ============================================================

func TestRefCounts_SelfReferencing(t *testing.T) {
	blocks := []*Block{
		{Kind: BlockMessage, Name: "TreeNode", DeclText: "message TreeNode { TreeNode child = 1; }"},
	}
	counts := BuildRefCounts(blocks)
	if counts["TreeNode"] != 0 {
		t.Errorf("self-referencing type should have ref_count=0, got %d", counts["TreeNode"])
	}
}

func TestRefCounts_FieldWithOptions(t *testing.T) {
	// Field options like [(validate.rules).string.min_len = 1] should not confuse type extraction
	blocks := []*Block{
		{Kind: BlockMessage, Name: "A", DeclText: `message A {
  string name = 1 [(validate.rules).string.min_len = 1];
  Foo foo = 2;
}`},
		{Kind: BlockMessage, Name: "Foo", DeclText: "message Foo { string v = 1; }"},
	}
	counts := BuildRefCounts(blocks)
	if counts["Foo"] != 1 {
		t.Errorf("Foo ref_count: want 1, got %d", counts["Foo"])
	}
}

// ============================================================
// Edge case tests (new)
// ============================================================

func TestSort_ReservedStatements(t *testing.T) {
	input := `syntax = "proto3";

message Foo {
  reserved 2, 15, 9 to 11;
  reserved "bar", "baz";
  string name = 1;
}
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "reserved 2, 15, 9 to 11;") {
		t.Error("reserved field numbers should be preserved")
	}
	if !strings.Contains(output, `reserved "bar", "baz";`) {
		t.Error("reserved field names should be preserved")
	}
}

func TestSort_UnreferencedTypeWarning(t *testing.T) {
	input := `syntax = "proto3";

message Orphan1 { string v = 1; }
message Orphan2 { string v = 1; }
`
	_, warnings, err := Sort(input, Options{})
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, w := range warnings {
		if strings.Contains(w, "Orphan1") {
			found["Orphan1"] = true
		}
		if strings.Contains(w, "Orphan2") {
			found["Orphan2"] = true
		}
	}
	if !found["Orphan1"] || !found["Orphan2"] {
		t.Errorf("expected warnings for both orphans, got warnings: %v", warnings)
	}
}

// ============================================================
// CLI integration tests (new)
// ============================================================

func TestCLI_CheckExitCode(t *testing.T) {
	// --check should return exit code 1 if file would change
	input := `syntax = "proto3";

message B { string v = 1; }
message A { string v = 1; }
`
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "test.proto")
	if err := os.WriteFile(inputFile, []byte(input), 0644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	code := processFile(inputFile, Options{Check: true, Quiet: true})
	if code != 1 {
		t.Errorf("check mode should return 1 for changed file, got %d", code)
	}
}

func TestCLI_CheckExitCode_NoChange(t *testing.T) {
	// Already sorted — should return 0
	input := `syntax = "proto3";

message A { string v = 1; }

message B { string v = 1; }
`
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "test.proto")
	if err := os.WriteFile(inputFile, []byte(input), 0644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	code := processFile(inputFile, Options{Check: true, Quiet: true})
	if code != 0 {
		t.Errorf("check mode should return 0 for already-sorted file, got %d", code)
	}
}

func TestCLI_WriteInPlace(t *testing.T) {
	input := `syntax = "proto3";

message B { string v = 1; }

message A { string v = 1; }
`
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "test.proto")
	if err := os.WriteFile(inputFile, []byte(input), 0644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	code := processFile(inputFile, Options{Write: true, Quiet: true})
	if code != 0 {
		t.Errorf("write mode should return 0, got %d", code)
	}

	content, err := os.ReadFile(inputFile)
	if err != nil {
		t.Fatalf("reading back file: %v", err)
	}
	if !strings.Contains(string(content), "message A") {
		t.Error("file should have been written with sorted content")
	}
	assertOrder(t, string(content), "message A", "message B")
}

func TestCLI_DiffOutput(t *testing.T) {
	a := "line1\nline2\nline3\n"
	b := "line1\nchanged\nline3\n"
	diff := DiffStrings(a, b, "a", "b")
	if !strings.Contains(diff, "--- a") {
		t.Error("diff should contain --- header")
	}
	if !strings.Contains(diff, "+++ b") {
		t.Error("diff should contain +++ header")
	}
	if !strings.Contains(diff, "-line2") {
		t.Error("diff should show removed line")
	}
	if !strings.Contains(diff, "+changed") {
		t.Error("diff should show added line")
	}
	if !strings.Contains(diff, "@@") {
		t.Error("diff should contain hunk headers")
	}
}

func TestCLI_DryRun(t *testing.T) {
	input := `syntax = "proto3";

message B { string v = 1; }

message A { string v = 1; }
`
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "test.proto")
	if err := os.WriteFile(inputFile, []byte(input), 0644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	code := processFile(inputFile, Options{DryRun: true, Quiet: true})
	if code != 0 {
		t.Errorf("dry-run should return 0, got %d", code)
	}

	// File should NOT be modified
	content, err := os.ReadFile(inputFile)
	if err != nil {
		t.Fatalf("reading back file: %v", err)
	}
	if string(content) != input {
		t.Error("dry-run should not modify the file")
	}
}

func TestCLI_QuietSuppressesWarnings(t *testing.T) {
	input := `syntax = "proto3";

message Orphan { string v = 1; }
`
	_, warnings, err := Sort(input, Options{Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("quiet mode should suppress warnings, got %v", warnings)
	}
}

func TestCLI_ExitCodeMatrix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		opts     Options
		wantCode int
	}{
		{
			name:     "proto2 returns 3",
			input:    `syntax = "proto2"; message Foo { required string v = 1; }`,
			opts:     Options{},
			wantCode: 3,
		},
		{
			name:     "success returns 0",
			input:    "syntax = \"proto3\";\n\nmessage Foo { string v = 1; }\n",
			opts:     Options{Quiet: true},
			wantCode: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			inputFile := filepath.Join(tmpDir, "test.proto")
			if err := os.WriteFile(inputFile, []byte(tt.input), 0644); err != nil {
				t.Fatalf("writing test file: %v", err)
			}

			code := processFile(inputFile, tt.opts)
			if code != tt.wantCode {
				t.Errorf("want exit code %d, got %d", tt.wantCode, code)
			}
		})
	}
}

func TestCLI_Annotate(t *testing.T) {
	input := `syntax = "proto3";

service S { rpc Do(Req) returns (Res); }
message Req { string v = 1; }
message Res { string v = 1; }
message Shared { string v = 1; }
message U1 { Shared s = 1; }
message U2 { Shared s = 1; }
message Helper { string v = 1; }
message Consumer { Helper h = 1; U1 u = 1; U2 u2 = 2; }
message Orphan { string v = 1; }
`
	opts := Options{Quiet: true, Annotate: true}
	output, _, err := Sort(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "// (request/response)") {
		t.Error("request/response annotation missing")
	}
	if !strings.Contains(output, "// (core:") {
		t.Error("core annotation missing")
	}
	if !strings.Contains(output, "// (helper:") {
		t.Error("helper annotation missing")
	}
	if !strings.Contains(output, "// (unreferenced)") {
		t.Error("unreferenced annotation missing")
	}
}

// ============================================================
// Shared-order dependency test
// ============================================================

func TestSort_SharedOrderDependency(t *testing.T) {
	// C depends on nothing, B depends on C, A depends on B
	// All are core (2+ refs each)
	input := `syntax = "proto3";

message A { B b = 1; }
message B { C c = 1; }
message C { string v = 1; }
message X { A a = 1; C c = 1; }
message Y { B b = 1; A a = 1; }
`
	opts := Options{Quiet: true, SharedOrder: "dependency"}
	output, _, err := Sort(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	// In dependency order: C before B before A (dependencies first)
	assertOrder(t, output, "message C", "message B", "message A")
}

// ============================================================
// isSectionDivider tightening test
// ============================================================

func TestIsSectionDivider_FalsePositive(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"// === Messages ===", true},
		{"// --- Types ---", true},
		{"// ### Enums", true},
		{"// === Core Types ===", true},
		{"// --- See docs for details ---", false}, // prose, not a divider
		{"// --- This is a long explanatory comment about something ---", false},
		{"// regular comment", false},
	}
	for _, tt := range tests {
		got := isSectionDivider(tt.line)
		if got != tt.want {
			t.Errorf("isSectionDivider(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

// ============================================================
// Config tests
// ============================================================

func TestConfig_LoadAndMerge(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, ".protosort.toml")
	os.WriteFile(configFile, []byte(`
[ordering]
shared_order = "dependency"
preserve_dividers = true

[verify]
verify = true
proto_paths = ["proto/", "third_party/"]
`), 0644)

	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatal(err)
	}

	opts := Options{SharedOrder: "alpha"}
	MergeConfig(&opts, cfg, map[string]bool{})

	if opts.SharedOrder != "dependency" {
		t.Errorf("SharedOrder: want dependency, got %s", opts.SharedOrder)
	}
	if !opts.PreserveDividers {
		t.Error("PreserveDividers should be true from config")
	}
	if !opts.Verify {
		t.Error("Verify should be true from config")
	}
	if len(opts.ProtoPaths) != 2 {
		t.Errorf("ProtoPaths: want 2, got %d", len(opts.ProtoPaths))
	}
}

func TestConfig_CLIFlagsOverrideConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, ".protosort.toml")
	os.WriteFile(configFile, []byte(`
[ordering]
shared_order = "dependency"
`), 0644)

	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatal(err)
	}

	opts := Options{SharedOrder: "alpha"}
	// Simulate that --shared-order was explicitly set
	MergeConfig(&opts, cfg, map[string]bool{"shared-order": true})

	if opts.SharedOrder != "alpha" {
		t.Errorf("CLI flag should override config, got %s", opts.SharedOrder)
	}
}

// ============================================================
// VerboseReport Section 2 classification test
// ============================================================

func TestVerboseReport_ShowsRequestResponse(t *testing.T) {
	blocks := []*Block{
		{Kind: BlockService, Name: "S", DeclText: "service S { rpc Do(Req) returns (Res); }"},
		{Kind: BlockMessage, Name: "Req", DeclText: "message Req { string v = 1; }"},
		{Kind: BlockMessage, Name: "Res", DeclText: "message Res { string v = 1; }"},
		{Kind: BlockMessage, Name: "Other", DeclText: "message Other { string v = 1; }"},
	}
	// Populate RPCs
	for _, b := range blocks {
		if b.Kind == BlockService {
			b.RPCs = ExtractRPCs(b)
		}
	}
	report := VerboseReport(blocks)
	if !strings.Contains(report, "request/response") {
		t.Errorf("VerboseReport should show request/response classification:\n%s", report)
	}
	if !strings.Contains(report, "unreferenced") {
		t.Errorf("VerboseReport should show unreferenced classification:\n%s", report)
	}
}

func TestVerboseReport_ShowsRequestResponse_WithoutPrePopulatedRPCs(t *testing.T) {
	// Regression: VerboseReport must work even when RPCs are NOT pre-populated
	// (as happens when called from processFile with a fresh ScanFile result).
	blocks := []*Block{
		{Kind: BlockService, Name: "S", DeclText: "service S { rpc Do(Req) returns (Res); }"},
		{Kind: BlockMessage, Name: "Req", DeclText: "message Req { string v = 1; }"},
		{Kind: BlockMessage, Name: "Res", DeclText: "message Res { string v = 1; }"},
		{Kind: BlockMessage, Name: "Other", DeclText: "message Other { string v = 1; }"},
	}
	// Deliberately do NOT populate RPCs — VerboseReport should handle this.
	report := VerboseReport(blocks)
	if !strings.Contains(report, "request/response") {
		t.Errorf("VerboseReport should auto-populate RPCs and show request/response:\n%s", report)
	}
}

// ============================================================
// Annotate idempotency regression test
// ============================================================

func TestAnnotate_Idempotent(t *testing.T) {
	input := `syntax = "proto3";

service S { rpc Do(Req) returns (Res); }
message Req { string v = 1; }
message Res { string v = 1; }
message Shared { string v = 1; }
message U1 { Shared s = 1; }
message U2 { Shared s = 1; }
message Orphan { string v = 1; }
`
	opts := Options{Quiet: true, Annotate: true}
	pass1, _, err := Sort(input, opts)
	if err != nil {
		t.Fatalf("first Sort failed: %v", err)
	}
	pass2, _, err := Sort(pass1, opts)
	if err != nil {
		t.Fatalf("second Sort failed: %v", err)
	}
	if pass1 != pass2 {
		t.Errorf("--annotate is not idempotent.\nPass 1:\n%s\nPass 2:\n%s", pass1, pass2)
	}
}

func TestAnnotate_PreservesExistingComments(t *testing.T) {
	input := `syntax = "proto3";

// Important documentation about Foo.
message Foo { string v = 1; }
`
	opts := Options{Quiet: true, Annotate: true}
	output, _, err := Sort(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "Important documentation about Foo") {
		t.Error("existing comments should be preserved when annotating")
	}
	if !strings.Contains(output, "// (unreferenced)") {
		t.Error("annotation should be added")
	}
}

// ============================================================
// Typed errors test
// ============================================================

func TestSort_TypedErrors(t *testing.T) {
	// Proto2 should return Proto2Error
	_, _, err := Sort(`syntax = "proto2"; message Foo {}`, Options{})
	if err == nil {
		t.Fatal("expected error for proto2")
	}
	var proto2Err *Proto2Error
	if !errors.As(err, &proto2Err) {
		t.Errorf("expected Proto2Error, got %T: %v", err, err)
	}
}

// ============================================================
// File permissions test
// ============================================================

func TestCLI_WritePreservesPermissions(t *testing.T) {
	input := `syntax = "proto3";

message B { string v = 1; }

message A { string v = 1; }
`
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "test.proto")
	if err := os.WriteFile(inputFile, []byte(input), 0755); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	code := processFile(inputFile, Options{Write: true, Quiet: true})
	if code != 0 {
		t.Fatalf("write failed with code %d", code)
	}

	info, _ := os.Stat(inputFile)
	if info.Mode().Perm() != 0755 {
		t.Errorf("file permissions changed: want 0755, got %o", info.Mode().Perm())
	}
}

// ============================================================
// Helpers
// ============================================================

// ============================================================
// Import/option spacing tests
// ============================================================

func TestSort_ImportsGroupedWithoutBlankLines(t *testing.T) {
	input := `syntax = "proto3";

import "z/file.proto";
import "a/file.proto";
import "m/file.proto";
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	// Imports should be grouped together without blank lines between them
	want := "import \"a/file.proto\";\nimport \"m/file.proto\";\nimport \"z/file.proto\";\n"
	if !strings.Contains(output, want) {
		t.Errorf("imports should be grouped without blank lines, got:\n%s", output)
	}
}

func TestSort_OptionsGroupedWithoutBlankLines(t *testing.T) {
	input := `syntax = "proto3";

option java_package = "com.test";
option go_package = "test/v1";
option cc_enable_arenas = "true";
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	// Options should be grouped together without blank lines between them
	want := "option cc_enable_arenas = \"true\";\noption go_package = \"test/v1\";\noption java_package = \"com.test\";\n"
	if !strings.Contains(output, want) {
		t.Errorf("options should be grouped without blank lines, got:\n%s", output)
	}
}

// ============================================================
// RPC sorting tests
// ============================================================

func TestSort_SortRPCsAlpha(t *testing.T) {
	input := `syntax = "proto3";

service UserService {
  rpc DeleteUser(DeleteUserRequest) returns (DeleteUserResponse);
  rpc CreateUser(CreateUserRequest) returns (CreateUserResponse);
  rpc GetUser(GetUserRequest) returns (GetUserResponse);
}

message DeleteUserRequest { string id = 1; }
message DeleteUserResponse {}
message CreateUserRequest { string name = 1; }
message CreateUserResponse { string id = 1; }
message GetUserRequest { string id = 1; }
message GetUserResponse { string name = 1; }
`
	opts := Options{Quiet: true, SortRPCs: "alpha"}
	output, _, err := Sort(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	// RPCs should be sorted alphabetically: Create, Delete, Get
	// Request/response pairs should follow new RPC order
	assertOrder(t, output,
		"message CreateUserRequest", "message CreateUserResponse",
		"message DeleteUserRequest", "message DeleteUserResponse",
		"message GetUserRequest", "message GetUserResponse")
}

func TestSort_SortRPCsGrouped(t *testing.T) {
	input := `syntax = "proto3";

service UserService {
  rpc DeleteUser(DeleteUserRequest) returns (DeleteUserResponse);
  rpc ListTrips(ListTripsRequest) returns (ListTripsResponse);
  rpc CreateUser(CreateUserRequest) returns (CreateUserResponse);
  rpc GetTrip(GetTripRequest) returns (GetTripResponse);
  rpc GetUser(GetUserRequest) returns (GetUserResponse);
  rpc CreateTrip(CreateTripRequest) returns (CreateTripResponse);
}

message DeleteUserRequest { string id = 1; }
message DeleteUserResponse {}
message ListTripsRequest { string user_id = 1; }
message ListTripsResponse { string v = 1; }
message CreateUserRequest { string name = 1; }
message CreateUserResponse { string id = 1; }
message GetTripRequest { string id = 1; }
message GetTripResponse { string v = 1; }
message GetUserRequest { string id = 1; }
message GetUserResponse { string name = 1; }
message CreateTripRequest { string name = 1; }
message CreateTripResponse { string id = 1; }
`
	opts := Options{Quiet: true, SortRPCs: "grouped"}
	output, _, err := Sort(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	// Grouped: Trip methods together, User methods together
	// Within groups: alphabetical by full name
	// Trip group: CreateTrip, GetTrip, ListTrips
	// User group: CreateUser, DeleteUser, GetUser
	assertOrder(t, output,
		"message CreateTripRequest", "message CreateTripResponse",
		"message GetTripRequest", "message GetTripResponse",
		"message ListTripsRequest", "message ListTripsResponse",
		"message CreateUserRequest", "message CreateUserResponse",
		"message DeleteUserRequest", "message DeleteUserResponse",
		"message GetUserRequest", "message GetUserResponse")
}

func TestSort_SortRPCsDisabledByDefault(t *testing.T) {
	input := `syntax = "proto3";

service S {
  rpc Zulu(ZReq) returns (ZRes);
  rpc Alpha(AReq) returns (ARes);
}

message ZReq { string v = 1; }
message ZRes { string v = 1; }
message AReq { string v = 1; }
message ARes { string v = 1; }
`
	output, _, err := Sort(input, defaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	// Without --sort-rpcs, original RPC order preserved: Zulu before Alpha
	assertOrder(t, output, "message ZReq", "message ZRes", "message AReq", "message ARes")
}

func TestSort_SortRPCsIdempotent(t *testing.T) {
	input := `syntax = "proto3";

service S {
  rpc Delete(DReq) returns (DRes);
  rpc Create(CReq) returns (CRes);
  rpc Get(GReq) returns (GRes);
}

message DReq { string v = 1; }
message DRes { string v = 1; }
message CReq { string v = 1; }
message CRes { string v = 1; }
message GReq { string v = 1; }
message GRes { string v = 1; }
`
	opts := Options{Quiet: true, SortRPCs: "alpha"}
	pass1, _, err := Sort(input, opts)
	if err != nil {
		t.Fatalf("first Sort: %v", err)
	}
	pass2, _, err := Sort(pass1, opts)
	if err != nil {
		t.Fatalf("second Sort: %v", err)
	}
	if pass1 != pass2 {
		t.Errorf("--sort-rpcs alpha not idempotent.\nPass 1:\n%s\nPass 2:\n%s", pass1, pass2)
	}
}

func TestRPCGroupKey(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"GetUser", "User"},
		{"CreateUser", "User"},
		{"DeleteUser", "User"},
		{"ListUsers", "Users"},
		{"UpdateUser", "User"},
		{"BatchCreateUsers", "Users"},
		{"BatchGetUsers", "Users"},
		{"WatchTrip", "Trip"},
		{"StreamEvents", "Events"},
		{"SearchProducts", "Products"},
		{"SetConfig", "Config"},
		{"AddItem", "Item"},
		{"RemoveItem", "Item"},
		{"StartJob", "Job"},
		{"StopJob", "Job"},
		{"RunTask", "Task"},
		{"CheckHealth", "Health"},
		{"CancelOperation", "Operation"},
		// No prefix match — return full name
		{"Healthcheck", "Healthcheck"},
		{"Getaway", "Getaway"}, // "Get" + lowercase 'a' → no strip
		// Name equals prefix exactly → return full name
		{"Get", "Get"},
		{"Create", "Create"},
	}
	for _, tt := range tests {
		got := rpcGroupKey(tt.name)
		if got != tt.want {
			t.Errorf("rpcGroupKey(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestSort_SortRPCsWithComments(t *testing.T) {
	input := `syntax = "proto3";

service S {
  // Deletes a user.
  rpc DeleteUser(DReq) returns (DRes);
  // Creates a user.
  rpc CreateUser(CReq) returns (CRes);
}

message DReq { string v = 1; }
message DRes { string v = 1; }
message CReq { string v = 1; }
message CRes { string v = 1; }
`
	opts := Options{Quiet: true, SortRPCs: "alpha"}
	output, _, err := Sort(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	// Comments should travel with their RPC
	assertOrder(t, output, "Creates a user", "rpc CreateUser", "Deletes a user", "rpc DeleteUser")
}

func TestSort_SortRPCsWithOptionBody(t *testing.T) {
	input := `syntax = "proto3";

service S {
  rpc DeleteUser(DReq) returns (DRes) {
    option (google.api.http) = {
      delete: "/v1/users/{id}"
    };
  }
  rpc CreateUser(CReq) returns (CRes);
}

message DReq { string v = 1; }
message DRes { string v = 1; }
message CReq { string v = 1; }
message CRes { string v = 1; }
`
	opts := Options{Quiet: true, SortRPCs: "alpha"}
	output, _, err := Sort(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	// CreateUser should come before DeleteUser even though Delete has option body
	assertOrder(t, output, "rpc CreateUser", "rpc DeleteUser")
	// The option body should be preserved
	if !strings.Contains(output, "delete: \"/v1/users/{id}\"") {
		t.Error("RPC option body should be preserved")
	}
}

func TestSort_SortRPCsContentIntegrity(t *testing.T) {
	input := `syntax = "proto3";

service S {
  rpc Zulu(ZReq) returns (ZRes);
  rpc Alpha(AReq) returns (ARes);
}

message ZReq { string v = 1; }
message ZRes { string v = 1; }
message AReq { string v = 1; }
message ARes { string v = 1; }
`
	opts := Options{Quiet: true, SortRPCs: "alpha"}
	output, _, err := Sort(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyContentIntegrity(input, output, opts); err != nil {
		t.Errorf("content integrity failed with --sort-rpcs: %v", err)
	}
}

// ============================================================
// Section header tests
// ============================================================

func TestSort_SectionHeaders_Golden(t *testing.T) {
	inputBytes, err := os.ReadFile("testdata/section_headers_input.proto")
	if err != nil {
		t.Fatalf("reading input: %v", err)
	}
	expectedBytes, err := os.ReadFile("testdata/section_headers_expected.proto")
	if err != nil {
		t.Fatalf("reading expected: %v", err)
	}

	opts := Options{Quiet: true, SectionHeaders: true}
	output, _, err := Sort(string(inputBytes), opts)
	if err != nil {
		t.Fatalf("Sort failed: %v", err)
	}

	if output != string(expectedBytes) {
		t.Errorf("output mismatch.\nDiff:\n%s",
			DiffStrings(string(expectedBytes), output, "expected", "got"))
	}
}

func TestSort_SectionHeaders(t *testing.T) {
	input := `syntax = "proto3";

service S {
  rpc GetOrg(GetOrgRequest) returns (GetOrgResponse);
}

message GetOrgRequest { string id = 1; }
message GetOrgResponse { string v = 1; }
message Shared { string v = 1; }
message U1 { Shared s = 1; }
message U2 { Shared s = 1; }
message Orphan { string v = 1; }
`
	opts := Options{Quiet: true, SectionHeaders: true}
	output, _, err := Sort(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	// Services get no header — "service S" is self-evident
	if strings.Contains(output, "// Services") {
		t.Error("Services header should not be injected")
	}
	if !strings.Contains(output, "// Types for GetOrg") {
		t.Error("missing Types for GetOrg header")
	}
	if !strings.Contains(output, "// Shared Types") {
		t.Error("missing Shared Types header")
	}
	if !strings.Contains(output, "// Unreferenced Types") {
		t.Error("missing Unreferenced Types header")
	}
	assertOrder(t, output,
		"service S",
		"// Types for GetOrg", "message GetOrgRequest",
		"// Shared Types", "message Shared",
		"// Unreferenced Types", "message Orphan")
}

func TestSort_SectionHeaders_RPCSubHeaders(t *testing.T) {
	input := `syntax = "proto3";

service S {
  rpc GetOrg(GetOrgReq) returns (GetOrgRes);
  rpc ListOrgs(ListOrgsReq) returns (ListOrgsRes);
}

message GetOrgReq { string id = 1; }
message GetOrgRes { string v = 1; }
message ListOrgsReq { string v = 1; }
message ListOrgsRes { string v = 1; }
`
	opts := Options{Quiet: true, SectionHeaders: true}
	output, _, err := Sort(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "// Types for GetOrg") {
		t.Error("missing Types for GetOrg sub-header")
	}
	if !strings.Contains(output, "// Types for ListOrgs") {
		t.Error("missing Types for ListOrgs sub-header")
	}
	assertOrder(t, output,
		"// Types for GetOrg", "message GetOrgReq", "message GetOrgRes",
		"// Types for ListOrgs", "message ListOrgsReq", "message ListOrgsRes")
}

func TestSort_SectionHeaders_Idempotent(t *testing.T) {
	input := `syntax = "proto3";

service S { rpc Do(Req) returns (Res); }
message Req { string v = 1; }
message Res { string v = 1; }
message Shared { string v = 1; }
message U1 { Shared s = 1; }
message U2 { Shared s = 1; }
message Orphan { string v = 1; }
`
	opts := Options{Quiet: true, SectionHeaders: true}
	pass1, _, err := Sort(input, opts)
	if err != nil {
		t.Fatalf("first Sort: %v", err)
	}
	pass2, _, err := Sort(pass1, opts)
	if err != nil {
		t.Fatalf("second Sort: %v", err)
	}
	if pass1 != pass2 {
		t.Errorf("not idempotent.\nDiff:\n%s",
			DiffStrings(pass1, pass2, "pass1", "pass2"))
	}
}

func TestSort_SectionHeaders_StrippedWhenDisabled(t *testing.T) {
	input := `syntax = "proto3";

service S { rpc Do(Req) returns (Res); }
message Req { string v = 1; }
message Res { string v = 1; }
message Orphan { string v = 1; }
`
	// First sort with headers
	opts := Options{Quiet: true, SectionHeaders: true}
	withHeaders, _, err := Sort(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(withHeaders, "// Types for Do") {
		t.Fatal("headers should be present after first sort")
	}

	// Re-sort without headers — should strip them
	opts.SectionHeaders = false
	withoutHeaders, _, err := Sort(withHeaders, opts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(withoutHeaders, sectionHeaderBanner) {
		t.Error("section headers should be stripped when --section-headers is disabled")
	}
}

func TestSort_SectionHeaders_NoService(t *testing.T) {
	input := `syntax = "proto3";

message Foo { Bar b = 1; }
message Baz { Bar b = 1; }
message Bar { string v = 1; }
message Orphan { string v = 1; }
`
	opts := Options{Quiet: true, SectionHeaders: true}
	output, _, err := Sort(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	// No service → no headers at all (headers only add value with service context)
	if strings.Contains(output, sectionHeaderBanner) {
		t.Error("no section headers expected when there are no services")
	}
}

func TestSort_SectionHeaders_EmptySection(t *testing.T) {
	// Only services and RPC types, no shared or unreferenced
	input := `syntax = "proto3";

service S { rpc Do(Req) returns (Res); }
message Req { string v = 1; }
message Res { string v = 1; }
`
	opts := Options{Quiet: true, SectionHeaders: true}
	output, _, err := Sort(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "// Services") {
		t.Error("Services header should not be injected")
	}
	if !strings.Contains(output, "// Types for Do") {
		t.Error("missing Types for Do header")
	}
	// Empty sections should have no headers
	if strings.Contains(output, "// Shared Types") {
		t.Error("Shared Types header should not appear when section is empty")
	}
	if strings.Contains(output, "// Unreferenced Types") {
		t.Error("Unreferenced Types header should not appear when section is empty")
	}
}

func TestSort_SectionHeaders_ContentIntegrity(t *testing.T) {
	input := `syntax = "proto3";

service S {
  rpc GetOrg(GetOrgReq) returns (GetOrgRes);
  rpc ListOrgs(ListOrgsReq) returns (ListOrgsRes);
}

message GetOrgReq { string id = 1; }
message GetOrgRes { string v = 1; }
message ListOrgsReq { string v = 1; }
message ListOrgsRes { string v = 1; }
message Shared { string v = 1; }
message U1 { Shared s = 1; }
message U2 { Shared s = 1; }
message Orphan { string v = 1; }
`
	opts := Options{Quiet: true, SectionHeaders: true}
	output, _, err := Sort(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyContentIntegrity(input, output, opts); err != nil {
		t.Errorf("content integrity failed: %v", err)
	}
}

// assertOrder verifies that the given substrings appear in order within text.
func assertOrder(t *testing.T, text string, substrs ...string) {
	t.Helper()
	prev := -1
	prevStr := ""
	for _, s := range substrs {
		idx := strings.Index(text[prev+1:], s)
		if idx < 0 {
			t.Errorf("substring %q not found after %q in:\n%s", s, prevStr, text)
			return
		}
		absIdx := prev + 1 + idx
		prev = absIdx
		prevStr = s
	}
}
