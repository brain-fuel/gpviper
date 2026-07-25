# Compatibility contract

The pinned baseline is Viper v1.21.0. `API_MANIFEST.csv` contains every unique
exported declaration found in the root and public subpackages: all 192
declarations are implemented. Declaration coverage is complete; exhaustive
behavioral and performance parity remains separately gated.

## Supported behavior

- Case-insensitive dotted keys.
- Precedence: `Set` > changed flag > environment > config > default.
- Explicit and automatic environment lookup, prefixes, replacers, and empty
  environment policy.
- Aliases, including aliases of ordinary dotted keys.
- `pflag` and Viper-compatible `FlagValue` bindings.
- `MergeConfigMap`, `ReadConfig`, and reader-based `MergeConfig` for JSON,
  YAML, and TOML.
- Scalar signed/unsigned integer, bool, float, time, duration, and byte-size
  conversions; integer/string slices; and string-keyed map conversions.
- Recursive case normalization for maps supplied through mutable sources.
- Public configuration error types, parse-error unwrapping, supported-extension
  inventory, and package-global `GetViper`.
- Stable sorted `AllKeys`; nested `AllSettings` output.
- Parent-object reconstruction and isolated `Sub` views for nested maps.
- `Unmarshal`, `UnmarshalExact`, and `UnmarshalKey` with weak conversion,
  duration/slice defaults, custom decode hooks, and exact unused-field errors.
- Instance/global construction options for environment-key replacement and
  default decode hooks.
- Custom nested-key delimiters and optional conversion based on default-value
  types.
- `afero` filesystem injection, explicit/discovered configuration files,
  search paths, extension inference, `ConfigFileUsed`, `ReadInConfig`, and
  merge-in behavior.
- Byte-for-byte compatible JSON/YAML/TOML writer output, configurable file
  permissions, safe writes, forced writes, and typed already-exists failures.
- Custom encoder/decoder registries and construction options.
- Config-file discovery through custom finders, structured logging options,
  experimental struct-key environment binding, and debug output.
- Remote-provider registration, secure-provider metadata, read/watch updates,
  channel updates, package-global entry points, and reset behavior.

Input maps, slices, getters returning slices, and snapshots do not alias facade
state. A flag that was not changed does not override lower sources.

## Explicit differences and open proof

- The upstream remote channel watcher races concurrent reads. The compatibility
  entry point consumes the same response values but serializes mutation, so the
  GoForge race suite remains clean.
- Reader APIs require `SetConfigType`; filesystem APIs infer type from the
  selected filename.
- The package-global facade exists for migration. New code should own a `Viper`
  instance and publish immutable snapshots.
- Viper has historically documented that concurrent reads and writes are not
  safe. This facade serializes mutable instance operations; snapshots require
  no lock.

`ReloadStream` is the non-compatibility replacement for ambient watch/reload
mutation. It serializes effectful loaders, assigns monotonic versions, publishes
both success and failure, and never mutates snapshots already handed to readers.
