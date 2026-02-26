# protosort: Protobuf File Reordering Tool

## Specification v0.1

---

## 1. Purpose

`protosort` is a command-line tool that reorders top-level declarations in proto3 `.proto` files into a consistent, readable layout. It preserves all semantics, comments, and formatting within declarations while reorganizing the order in which declarations appear.

The tool **never modifies the content of any declaration** — it only changes the order in which declarations appear in the file.

**Scope:** proto3 only. Proto2 files are not supported and will be rejected with an error.

---

## 2. Design Principles

1. **Safety first.** The tool must never produce output that changes the compiled protobuf schema. A built-in verification step ensures the descriptor set is identical before and after reordering.
2. **Comments travel with their declaration.** Leading comments, trailing comments, and detached comments are associated with the nearest following declaration and move with it.
3. **Deterministic output.** Given the same input and configuration, the tool always produces the same output.
4. **Minimal disruption.** Only ordering changes. No reformatting, no renaming, no whitespace normalization within declarations.

---

## 3. File Layout

After reordering, a `.proto` file is laid out in these sections, top to bottom:

### Section 1: File Header

1. License header / file-level comments (original order)
2. `syntax` statement
3. `package` statement
4. `option` statements (alphabetized by option name)
5. `import` statements (alphabetized by path)

### Section 2: Service and Request/Response Messages (if present)

The tool auto-detects whether the file declares a `service`. If it does, this section is emitted. If not, it is skipped entirely and the body begins with Section 3.

First, each `service` block (in original declaration order if multiple exist).

Then, for each RPC in each service (in declaration order), emit:

1. The request message
2. The response message

This mirrors the Uber V2 convention: request/response pairs appear in the same order as their corresponding RPCs. A message is classified as a request/response if it appears as a direct argument or return type of an RPC in the same file.

If a request or response message is used by multiple RPCs, it appears at the position of its **first** use (first service in declaration order, first RPC within that service).

### Section 3: Core Types

All remaining types referenced by **two or more** other declarations within the same file. Ordered alphabetically by name.

A "reference" means the type appears as:
- A field type in a message
- A method input or output type in a service
- A `oneof` variant type
- A `map` value type

### Section 4: Single-Use Helper Types

Types referenced by exactly **one** other declaration in the file. Each helper is placed **immediately before** its single consumer.

When a chain of single-use types exists (A uses B, B uses C, all single-use), they form a **cluster** emitted bottom-up immediately before the root consumer:

```
// C is used only by B
message C { ... }

// B is used only by A
message B {
  C c = 1;
}

// A is used by something in Section 2 or 3
message A {
  B b = 1;
}
```

### Section 5: Unreferenced Types

Types not referenced by any other declaration in the file. These are placed last, in alphabetical order. The tool emits a warning for each unreferenced type, as they may indicate dead code or types intended for export.

---

## 4. Reference Counting Rules

Reference counting determines whether a type is "core" (2+ references), "helper" (1 reference), or "unreferenced" (0 references). The scope is **file-local only**:

- Only types **defined** in the current file are classified and reordered.
- References **to** imported types (e.g., `google.protobuf.Timestamp`) are ignored — they don't exist in this file and can't be moved.
- References **from** other files to types in this file are unknown and don't affect classification. This means types that exist solely for cross-file consumption will appear "unreferenced" within this file.

A type `T` defined in the current file has its reference count incremented once per distinct declaration that uses it:

| Context | Counts as reference? |
|---|---|
| Field type in a message | Yes |
| RPC request or response type | Yes |
| `map<K, V>` where V = T | Yes |
| `oneof` variant type | Yes |
| Nested message referencing parent | No (implicit) |
| Type used only in a comment | No |
| Type used in an `option` value | No |
| Imported type used as a field | No (not defined in this file) |

If the same declaration references `T` in multiple fields, that still counts as **one** reference from that declaration.

---

## 5. Comment Association Rules

Comments must travel with their associated declaration. The tool treats each top-level declaration as a **block** consisting of: any preceding comments + the declaration itself (from keyword to closing `}`).

### What constitutes a block

```protobuf
// This comment is part of the Vehicle block.
// So is this line.
message Vehicle {
  // This is inside the body — also part of the block.
  string id = 1; // Inline comment — part of the block.
}
```

### Association rules

1. **Leading comments** (contiguous comment lines immediately above a declaration, no blank line between): Part of the block. Move with the declaration.

2. **Detached comments** (comment block separated from the next declaration by one or more blank lines): Attached to the **following** declaration. Move with it.

```protobuf
// This is a detached comment — one blank line below.

// This is a leading comment — no blank line below.
message Foo { ... }
```

Both comment blocks above are part of the `Foo` block.

3. **Interior comments** (anything between `{` and `}`): Part of the declaration body. Not affected by reordering.

4. **Trailing inline comments** (on the same line as the declaration): Part of the declaration.

5. **Section divider comments** (freestanding comments like `// === Messages ===` not attached to any declaration): Dropped during reordering, since the tool imposes its own section structure. A `--preserve-dividers` flag can instead attach them to the next declaration.

### Between blocks

The tool normalizes inter-block spacing. After reordering, exactly **one blank line** separates each top-level block. No other whitespace normalization is performed — everything inside a declaration body is preserved byte-for-byte.

```protobuf
// Before: inconsistent spacing
message Foo { ... }



message Bar { ... }
message Baz { ... }

// After: uniform spacing
message Bar { ... }

message Baz { ... }

message Foo { ... }
```

---

## 6. Verification

### 6.1 Descriptor Set Comparison

The primary verification mechanism. Before and after reordering, the tool compiles the file using `protoc` (or `buf build`) and compares the resulting `FileDescriptorProto`.

The comparison **ignores**:
- `source_code_info` (line numbers, comments — these will change)
- Span information

The comparison **verifies byte-equality** of:
- All message descriptors (fields, nested types, options)
- All enum descriptors (values, options)
- All service descriptors (methods, options)
- Package, syntax, options, dependencies

If descriptors differ, the tool exits with a non-zero status, prints a diff of the descriptor sets, and does **not** write the output file.

### 6.2 Syntax Roundtrip Check

As a secondary check, the tool verifies that the output file parses without errors:

```
protoc --proto_path=<path> --descriptor_set_out=/dev/null <output.proto>
```

### 6.3 Content Integrity Check

The tool verifies that the set of declarations (by name and content) is identical before and after reordering. This catches bugs where a declaration might be duplicated or dropped during reordering.

Specifically:
- Count of each declaration type (message, enum, service) must match
- The body of each declaration (everything between and including `{` ... `}`) must be byte-identical before and after

---

## 7. CLI Interface

```
protosort [OPTIONS] <FILE>...

Arguments:
  <FILE|DIR>...       One or more .proto files or directories to process

Options:
  -r, --recursive     Recursively process all .proto files in directories
  -w, --write         Write changes in-place (default: print to stdout)
  -c, --check         Exit non-zero if file would change (for CI)
  -d, --diff          Print unified diff of changes
  --skip-verify       Skip protoc descriptor verification (not recommended)
  --protoc <PATH>     Path to protoc binary (default: search PATH)
  --proto-path <DIR>  Additional proto include paths (repeatable)
  --shared-order <ORDER>
                      Ordering for core types: "alpha" (default) or "dependency"
  --preserve-dividers Keep section divider comments attached to next declaration
  --strip-commented-code
                      Remove comment blocks that consist entirely of commented-out
                      protobuf declarations (e.g. "// rpc Foo(...)" or
                      "// message Bar {}") with no other prose. Useful for
                      cleaning up dead code left behind as comments.
  --dry-run           Parse and compute new order, report what would change,
                      but don't write anything
  -v, --verbose       Print reference counts and classification for each type
  -q, --quiet         Suppress warnings (unreferenced types, etc.)
```

### Exit Codes

| Code | Meaning |
|---|---|
| 0 | Success (or no changes needed in `--check` mode) |
| 1 | File would change (`--check` mode) |
| 2 | Verification failed — descriptor mismatch |
| 3 | Parse error in input file |
| 4 | I/O or protoc invocation error |

---

## 8. Implementation Notes

### 8.1 Recommended Parser

Use `bufbuild/protocompile` (Go) as the parsing library. It provides a full AST with comment attachment, which is essential for preserving comments during reordering. The `parser.AST` types preserve original source positions and associated comments.

If implementing in Rust, `protobuf-parse` or a custom parser built on `pest`/`nom` would work, but comment preservation will require more manual effort.

### 8.2 Algorithm Sketch

```
1. Parse the .proto file into an AST with comments
2. Extract the file header (syntax, package, options, imports)
3. Collect all top-level declarations with their associated comments
4. If a service exists:
   a. Emit service(s) → Section 2
   b. Classify RPC request/response messages → Section 2
5. Build reference graph among remaining types
   a. ref_count >= 2 → Section 3 (core types)
   b. ref_count == 1 → Section 4 (single-use helpers)
   c. ref_count == 0 → Section 5 (unreferenced)
6. For Section 4, compute insertion points via topological sort
7. Emit sections in order with blank line separators
8. Verify output (descriptor comparison + content integrity)
9. Write output or report diff
```

### 8.3 Edge Cases

| Case | Handling |
|---|---|
| Circular references between two types | Both become "core" (ref_count >= 2), placed in Section 3 |
| Custom option `extend` blocks (e.g., extending `google.protobuf.MessageOptions`) | Placed in file header after other `option` statements |
| Nested message definitions | Not affected — only top-level ordering changes. Inner structure is preserved. |
| Multiple services in one file | Services emitted in original order, then request/response pairs for all services in per-service RPC order (first service's RPCs first, then second service's) |
| A type used as both RPC arg and field type | Classified as request/response (Section 2 takes priority) |
| Empty file or header-only file | Output unchanged |
| `reserved` statements inside messages | Not affected (interior to declaration) |
| `map<K, V>` where K or V is a local type | V counts as a reference; K is always a scalar |
| File with no service | Section 2 is skipped, body starts with Section 3 |
| Empty service (no RPCs) | Service emitted in Section 2, no request/response pairs follow |
| Proto2 file | Rejected with error, not processed |
| Commented-out RPCs (`// rpc Foo(...)`) | Not parsed as declarations — treated as comments per Section 5 rules. See `--strip-commented-code`. |
| Types only consumed by other files | Appear "unreferenced" in Section 5 with warning (see `--quiet`) |

---

## 9. Configuration File (Optional)

For project-wide consistency, `protosort` can optionally read a `.protosort.toml` at the repository root:

```toml
[ordering]
# How to order core types
shared_order = "alpha"  # "alpha" | "dependency"

# Whether to preserve visual divider comments
preserve_dividers = false

# Remove comment blocks that are just commented-out declarations
strip_commented_code = false

[verify]
# Path to protoc (or "buf" to use buf build)
compiler = "protoc"

# Additional include paths
proto_paths = ["proto/", "third_party/"]

# Skip verification (not recommended)
skip_verify = false

[warnings]
# Warn on unreferenced types
unreferenced = true

# Warn on files with multiple services
multiple_services = true
```

---

## 10. Future Considerations

- **Cross-file reordering**: Moving types between files based on usage patterns (e.g., extracting shared types into a common file). Significantly more complex and out of scope for v1.
- **Field reordering within messages**: Reordering fields by tag number. Dangerous if anyone depends on declaration order; would need its own verification.
- **IDE integration**: LSP-based code action for "reorder this file" in VS Code / IntelliJ.
- **buf plugin**: Distributing as a `buf` plugin for integration into existing `buf` workflows.

---

## 11. Example

### Input

```protobuf
syntax = "proto3";

package acme.fleet.v1;

import "google/protobuf/timestamp.proto";

// A geographic coordinate.
message Location {
  double latitude = 1;
  double longitude = 2;
}

message ListVehiclesResponse {
  repeated Vehicle vehicles = 1;
  bool has_more = 2;
}

// Vehicle fleet management service.
service FleetAPI {
  // Get a single vehicle by ID.
  rpc GetVehicle(GetVehicleRequest) returns (GetVehicleResponse);
  // List all vehicles matching filters.
  rpc ListVehicles(ListVehiclesRequest) returns (ListVehiclesResponse);
}

message GetVehicleRequest {
  string vehicle_id = 1;
}

// The type of fuel a vehicle uses.
enum FuelType {
  FUEL_TYPE_INVALID = 0;
  FUEL_TYPE_GASOLINE = 1;
  FUEL_TYPE_DIESEL = 2;
  FUEL_TYPE_ELECTRIC = 3;
}

message ListVehiclesRequest {
  FuelType fuel_type_filter = 1;
  int32 max_results = 2;
}

message GetVehicleResponse {
  Vehicle vehicle = 1;
}

// A maintenance record for a vehicle.
message MaintenanceRecord {
  string id = 1;
  google.protobuf.Timestamp service_date = 2;
  string description = 3;
}

// A vehicle in the fleet.
message Vehicle {
  string id = 1;
  string vin = 2;
  FuelType fuel_type = 3;
  Location current_location = 4;
  repeated MaintenanceRecord maintenance_history = 5;
}
```

### Output

```protobuf
syntax = "proto3";

package acme.fleet.v1;

import "google/protobuf/timestamp.proto";

// Vehicle fleet management service.
service FleetAPI {
  // Get a single vehicle by ID.
  rpc GetVehicle(GetVehicleRequest) returns (GetVehicleResponse);
  // List all vehicles matching filters.
  rpc ListVehicles(ListVehiclesRequest) returns (ListVehiclesResponse);
}

message GetVehicleRequest {
  string vehicle_id = 1;
}

message GetVehicleResponse {
  Vehicle vehicle = 1;
}

message ListVehiclesRequest {
  FuelType fuel_type_filter = 1;
  int32 max_results = 2;
}

message ListVehiclesResponse {
  repeated Vehicle vehicles = 1;
  bool has_more = 2;
}

// The type of fuel a vehicle uses.
// (core: referenced by Vehicle, ListVehiclesRequest)
enum FuelType {
  FUEL_TYPE_INVALID = 0;
  FUEL_TYPE_GASOLINE = 1;
  FUEL_TYPE_DIESEL = 2;
  FUEL_TYPE_ELECTRIC = 3;
}

// A vehicle in the fleet.
// (core: referenced by GetVehicleResponse, ListVehiclesResponse)
message Vehicle {
  string id = 1;
  string vin = 2;
  FuelType fuel_type = 3;
  Location current_location = 4;
  repeated MaintenanceRecord maintenance_history = 5;
}

// A geographic coordinate.
// (helper: used only by Vehicle)
message Location {
  double latitude = 1;
  double longitude = 2;
}

// A maintenance record for a vehicle.
// (helper: used only by Vehicle)
message MaintenanceRecord {
  string id = 1;
  google.protobuf.Timestamp service_date = 2;
  string description = 3;
}
```

### What Changed

| Declaration | Before | After | Reason |
|---|---|---|---|
| `FleetAPI` | 5th | 1st (after header) | Service → Section 2 |
| `GetVehicleRequest` | 6th | 2nd | RPC request, 1st method → Section 2 |
| `GetVehicleResponse` | 9th | 3rd | RPC response, 1st method → Section 2 |
| `ListVehiclesRequest` | 8th | 4th | RPC request, 2nd method → Section 2 |
| `ListVehiclesResponse` | 2nd | 5th | RPC response, 2nd method → Section 2 |
| `FuelType` | 7th | 6th | Core type (Vehicle + ListVehiclesRequest) → Section 3 |
| `Vehicle` | 10th | 7th | Core type (GetVehicleResponse + ListVehiclesResponse) → Section 3 |
| `Location` | 1st | 8th | Single-use helper (only Vehicle) → Section 4, before Vehicle |
| `MaintenanceRecord` | 3rd | 9th | Single-use helper (only Vehicle) → Section 4, before Vehicle |

**Note**: The `(core: ...)` and `(helper: ...)` annotations in the output comments above are shown for illustration only. The tool does not add these annotations by default (available via `--verbose` or `--annotate` flag).

---

## 12. Verification Walkthrough

For the example above, verification proceeds as:

```bash
# 1. Compile original
protoc --descriptor_set_out=/tmp/before.pb \
       --proto_path=. input.proto

# 2. Compile reordered
protoc --descriptor_set_out=/tmp/after.pb \
       --proto_path=. output.proto

# 3. Compare (ignoring source_code_info)
protosort --internal-verify /tmp/before.pb /tmp/after.pb
# Strips source_code_info from both, then byte-compares
# Exit 0 = identical, Exit 2 = mismatch

# 4. Content integrity
# Assert: set(declaration_names_before) == set(declaration_names_after)
# Assert: for each name, body_bytes_before == body_bytes_after
```

This catches:
- Dropped declarations (declaration count mismatch)
- Duplicated declarations (declaration count mismatch)
- Mangled declaration bodies (body bytes differ)
- Changed semantics (descriptor bytes differ)
- Broken syntax (protoc compilation failure)

---

## 13. Testing

### 13.1 Unit Tests

Inline proto strings testing each classification and ordering rule in isolation. Each test provides an input string, runs the sorter, and asserts the output matches an expected string.

**Ordering rules:**
- Service moves to top of body
- Request/response pairs follow RPC declaration order
- Shared request/response (used by multiple RPCs) appears at first use
- Core types (2+ references) sorted alphabetically
- Helper types (1 reference) placed immediately before consumer
- Helper chains stack bottom-up before root consumer
- Unreferenced types sort alphabetically at bottom
- File with no service skips Section 2

**Reference counting:**
- Field type counts as reference
- `map<string, T>` value type counts as reference
- `oneof` variant counts as reference
- Multiple fields referencing same type from one message = 1 reference
- Imported types ignored
- Circular references → both become core

**Comment association:**
- Leading comment (no blank line) travels with declaration
- Detached comment (blank line gap) travels with following declaration
- Interior comments unchanged
- Trailing inline comments unchanged
- Section divider comments dropped (or preserved with flag)
- `--strip-commented-code` removes commented-out declarations
- `--strip-commented-code` preserves prose comments

**Whitespace:**
- Inter-declaration spacing normalized to one blank line
- Interior whitespace preserved byte-for-byte

**Header:**
- Options alphabetized
- Imports alphabetized
- License comment stays at top
- `syntax` before `package` before options before imports

**Edge cases:**
- Empty service (no RPCs)
- Empty file
- File with only a header
- Proto2 file rejected
- Single declaration file (unchanged)
- Type used as both RPC arg and field type → classified as request/response

### 13.2 Integration Tests

Real `.proto` files run through the full pipeline: parse → sort → emit → verify. Each test has an input file and a golden expected-output file.

**Sources for test fixtures:**
- Hand-written files covering spec scenarios (service files, supporting files, mixed)
- Adapted from [googleapis](https://github.com/googleapis/googleapis) — complex services with many RPCs and deep type hierarchies
- Adapted from [grpc-go examples](https://github.com/grpc/grpc-go) — typical service definitions
- Adapted from [buf test fixtures](https://github.com/bufbuild/buf) — edge cases around comments and formatting

Each integration test also asserts:
- `protoc` compiles the output without errors
- Descriptor set (minus `source_code_info`) is byte-identical before and after
- Declaration names and bodies match before and after

### 13.3 Idempotency Tests

Run `protosort` on every test fixture **twice**. Assert the second run produces identical output to the first. If sorting isn't idempotent, the ordering logic has a bug.

```
output1 = protosort(input)
output2 = protosort(output1)
assert output1 == output2
```

This should run against every unit and integration test input automatically.

### 13.4 Roundtrip Property Tests

Generate random valid proto3 files (services with varying RPC counts, messages with varying field types referencing each other, enums, comments in various positions). Run `protosort`, verify:

- Output compiles
- Descriptors match
- All declarations present
- Idempotent on re-run

This catches edge case interactions that hand-written tests miss. The proto3 grammar is simple enough that a generator is straightforward.

### 13.5 Test Matrix

| Test category | Count (approx) | What it covers |
|---|---|---|
| Ordering rules | ~15 | Each section placement rule |
| Reference counting | ~10 | Each ref type, edge cases |
| Comment association | ~8 | Each comment type, stripping |
| Header sorting | ~4 | Options, imports, license |
| Edge cases | ~8 | Empty files, proto2 rejection, etc. |
| Integration (golden files) | ~10 | Real-world files end-to-end |
| Idempotency | All of the above | Second pass = no change |
| Property/roundtrip | ~100 generated | Random valid protos |
