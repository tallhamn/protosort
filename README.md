# protosort

A command-line tool that reorders top-level declarations in proto3 `.proto` files into a consistent, readable layout.

protosort **never modifies the content of any declaration** — it only changes the order in which they appear. A built-in verification step (via `protoc`) ensures the compiled descriptor set is identical before and after reordering.

## Installation

### From source

```sh
go install github.com/tallhamn/protosort@latest
```

### From releases

Download a pre-built binary from the [GitHub Releases](https://github.com/tallhamn/protosort/releases) page.

### Build locally

```sh
git clone https://github.com/tallhamn/protosort.git
cd protosort
make build
```

## Quick start

```sh
# Preview sorted output on stdout
protosort api.proto

# See what would change
protosort --diff api.proto

# Sort in place
protosort --write api.proto

# Check in CI (exits non-zero if file would change)
protosort --check api.proto

# Recursively sort all .proto files in a directory
protosort --write --recursive proto/
```

## What it does

Given a disordered `.proto` file where types are scattered without structure:

```protobuf
syntax = "proto3";
package acme.fleet.v1;
import "google/protobuf/timestamp.proto";

message Location {
  double latitude = 1;
  double longitude = 2;
}

message ListVehiclesResponse {
  repeated Vehicle vehicles = 1;
  bool has_more = 2;
}

service FleetAPI {
  rpc GetVehicle(GetVehicleRequest) returns (GetVehicleResponse);
  rpc ListVehicles(ListVehiclesRequest) returns (ListVehiclesResponse);
}

message GetVehicleRequest {
  string vehicle_id = 1;
}

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

message MaintenanceRecord {
  string id = 1;
  google.protobuf.Timestamp service_date = 2;
  string description = 3;
}

message Vehicle {
  string id = 1;
  string vin = 2;
  FuelType fuel_type = 3;
  Location current_location = 4;
  repeated MaintenanceRecord maintenance_history = 5;
}
```

protosort produces:

```protobuf
syntax = "proto3";

package acme.fleet.v1;

import "google/protobuf/timestamp.proto";

// 1. Service first — acts as a table of contents
service FleetAPI { ... }

// 2. Request/response pairs, in RPC declaration order
message GetVehicleRequest { ... }
message GetVehicleResponse { ... }
message ListVehiclesRequest { ... }
message ListVehiclesResponse { ... }

// 3. Core types — FuelType and Vehicle are each referenced by 2+ declarations
enum FuelType { ... }

// 4. Helpers placed before their consumer — Location and MaintenanceRecord
//    are only used by Vehicle, so they appear right before it
message Location { ... }
message MaintenanceRecord { ... }
message Vehicle { ... }
```

The output follows a consistent section layout:

1. **Header** — `syntax`, `package`, sorted `option`s, sorted `import`s
2. **Services & RPC types** — each service followed by its request/response pairs in RPC order
3. **Core types** — referenced by 2+ other declarations, sorted alphabetically
4. **Helper types** — referenced by exactly 1 declaration, placed immediately before their consumer
5. **Unreferenced types** — not referenced by anything else in the file, sorted alphabetically

## Options

```
Usage: protosort [OPTIONS] <FILE|DIR>...

Options:
  -w, --write               Write changes in-place
  -c, --check               Exit non-zero if file would change (for CI)
  -d, --diff                Print unified diff of changes
  -r, --recursive           Recursively process all .proto files in directories
  --dry-run                 Report what would change without writing
  --shared-order string     Ordering for core types: alpha or dependency (default "alpha")
  --preserve-dividers       Keep section divider comments
  --strip-commented-code    Remove commented-out protobuf declarations
  --annotate                Add classification annotations to comments
  --skip-verify             Skip protoc descriptor verification
  --protoc string           Path to protoc binary
  --proto-path value        Additional proto include paths (repeatable)
  --config string           Path to .protosort.toml config file
  -v, --verbose             Print reference counts and classification
  -q, --quiet               Suppress warnings
```

## Configuration

protosort looks for a `.protosort.toml` file in the current directory or any parent up to the repository root. CLI flags override config file values.

```toml
[ordering]
shared_order = "alpha"         # "alpha" or "dependency"
preserve_dividers = false
strip_commented_code = false

[verify]
skip_verify = false
compiler = ""                  # path to protoc binary
proto_paths = []
```

## Exit codes

| Code | Meaning |
|------|---------|
| 0    | Success (or no changes needed) |
| 1    | `--check` mode: file would change |
| 2    | Verification failed (sorted output changes compiled schema) |
| 3    | Proto2 file or parse error |
| 4    | I/O or usage error |

## Verification

By default, protosort runs `protoc` to compile both the original and sorted files, normalizes the resulting descriptor sets (stripping source location info and sorting internal lists), then compares them. This ensures reordering never changes the compiled schema. Use `--skip-verify` to disable this (e.g., if `protoc` is unavailable).

## Documentation

- [Specification](protosort-spec.md) — full design spec with detailed rules for each section
- [Style Guide](style-guide.md) — concise guide to the ordering conventions

## License

[MIT](LICENSE)
