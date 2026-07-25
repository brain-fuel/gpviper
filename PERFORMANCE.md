# Performance contract

The paired operation reads the same resolved string from a GoForge immutable
snapshot and from Viper v1.21.0. Both were populated through `SetDefault`.

```sh
go test -run '^TestSnapshotAllocationBudget$' \
  -bench 'Benchmark(SnapshotGetString|UpstreamGetString)$' \
  -benchmem -count=5
```

Completion requires the slowest GoForge run to be at least twice as fast as
the fastest upstream run and at least 50% fewer allocations.

## Completion measurement

Measured 2026-07-20 on Apple M5 Max (`darwin/arm64`):

| Operation | Five-run range | Bytes/op | Allocations/op |
|---|---:|---:|---:|
| GoForge `Snapshot.GetString` | 14.42–15.23 ns | 0 | 0 |
| upstream `Viper.GetString` | 131.3–132.9 ns | 80 | 3 |

Using the slowest GoForge run and fastest upstream run, the immutable semantic
boundary is **8.62x faster** and uses **100% fewer allocations**.
`TestSnapshotAllocationBudget` independently enforces zero snapshot allocations
and verifies that the paired upstream operation allocates. This claim applies
to resolved snapshots, not every mutable compatibility operation.
