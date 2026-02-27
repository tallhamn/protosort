# protosort

A command-line tool that reorders top-level declarations in proto3 `.proto` files into a consistent, readable layout.

protosort **never modifies the content of any declaration** — it only changes the order in which they appear. A built-in integrity check confirms that no declaration was lost, added, or altered during sorting.

It pairs well with [buf lint](https://buf.build/docs/lint/overview) — buf enforces naming and structure conventions but has no rules for declaration order within a file. protosort fills that gap.

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
# What I use
protosort --write --recursive --sort-rpcs grouped --section-headers proto/

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

message GetTripResponse {
  Trip trip = 1;
}

service FleetAPI {
  rpc DeleteVehicle(DeleteVehicleRequest) returns (DeleteVehicleResponse);
  rpc UpdateTrip(UpdateTripRequest) returns (UpdateTripResponse);
  rpc GetTrip(GetTripRequest) returns (GetTripResponse);
  rpc CreateVehicle(CreateVehicleRequest) returns (CreateVehicleResponse);
}

message CreateVehicleRequest {
  string vin = 1;
  FuelType fuel_type = 2;
}

enum FuelType {
  FUEL_TYPE_INVALID = 0;
  FUEL_TYPE_GASOLINE = 1;
  FUEL_TYPE_DIESEL = 2;
  FUEL_TYPE_ELECTRIC = 3;
}

message DeleteVehicleRequest {
  string vehicle_id = 1;
}

message DeleteVehicleResponse {
  Vehicle vehicle = 1;
}

message UpdateTripRequest {
  string trip_id = 1;
  Location destination = 2;
}

message GetTripRequest {
  string trip_id = 1;
}

message CreateVehicleResponse {
  Vehicle vehicle = 1;
}

message UpdateTripResponse {
  Trip trip = 1;
}

message Location {
  double latitude = 1;
  double longitude = 2;
}

message Trip {
  string id = 1;
  Location destination = 2;
}

message Vehicle {
  string id = 1;
  string vin = 2;
  FuelType fuel_type = 3;
  Location current_location = 4;
}
```

With `--sort-rpcs grouped --section-headers`, protosort produces:

```protobuf
syntax = "proto3";

package acme.fleet.v1;

service FleetAPI {
  rpc GetTrip(GetTripRequest) returns (GetTripResponse);
  rpc UpdateTrip(UpdateTripRequest) returns (UpdateTripResponse);
  rpc CreateVehicle(CreateVehicleRequest) returns (CreateVehicleResponse);
  rpc DeleteVehicle(DeleteVehicleRequest) returns (DeleteVehicleResponse);
}

// ============================================================================
// Types for GetTrip
// ============================================================================
message GetTripRequest { ... }
message GetTripResponse { ... }

// ============================================================================
// Types for UpdateTrip
// ============================================================================
message UpdateTripRequest { ... }
message UpdateTripResponse { ... }

// ============================================================================
// Types for CreateVehicle
// ============================================================================
message CreateVehicleRequest { ... }
message CreateVehicleResponse { ... }

// ============================================================================
// Types for DeleteVehicle
// ============================================================================
message DeleteVehicleRequest { ... }
message DeleteVehicleResponse { ... }

// ============================================================================
// Shared Types
// ============================================================================
enum FuelType { ... }
message Location { ... }
message Trip { ... }
message Vehicle { ... }
```

The output follows a consistent section layout:

1. **Header** — `syntax`, `package`, sorted `option`s, sorted `import`s
2. **Services & RPC types** — RPCs grouped by resource noun then verb (`--sort-rpcs grouped`), each followed by its request/response pair
3. **Shared types** — referenced by 2+ other declarations, sorted alphabetically
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
  --sort-rpcs string        Sort RPCs within services: alpha or grouped
  --preserve-dividers       Keep section divider comments
  --section-headers         Insert section header comments
  --strip-commented-code    Remove commented-out protobuf declarations
  --annotate                Add classification annotations to comments
  --verify                  Verify declaration integrity after sorting (uses protoc if available)
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
sort_rpcs = ""                 # "" (disabled), "alpha", or "grouped"
preserve_dividers = false
strip_commented_code = false
section_headers = false

[verify]
verify = false
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

### Built-in integrity check

Pass `--verify` to confirm that every declaration is present and unchanged after sorting. If `protoc` is in your PATH, it also compiles both versions and compares descriptor sets to confirm the reordering never changes the compiled schema.

```sh
# Built-in check (no external tools required)
protosort --verify --write api.proto

# Point to a specific protoc and include paths
protosort --verify --protoc /usr/local/bin/protoc --proto-path proto/ --write api.proto
```

### Verifying with buf

You can independently verify that sorting preserves the compiled schema using [buf](https://buf.build):

```sh
buf build -o /tmp/before.bin
protosort --write --recursive .
buf build -o /tmp/after.bin
buf breaking /tmp/after.bin --against /tmp/before.bin
```

If the last command reports no breaking changes, the reordering is safe.

## Documentation

- [Specification](protosort-spec.md) — full design spec with detailed rules for each section
- [Style Guide](style-guide.md) — concise guide to the ordering conventions

## License

[MIT](LICENSE)
