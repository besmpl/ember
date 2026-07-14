# Architecture Decision Records

Use ADRs for compatibility, runtime, or public-interface decisions that future
work will need to understand.

Keep each ADR focused:

- context;
- decision;
- consequences;
- alternatives considered.

Current runtime decisions:

- `0001-go-native-runtime-mapping.md`: public Go-object mapping and default Go
  lifetime.
- `0004-runtime-slot-handle-ownership.md`: private slot/handle ownership when
  measured internal VM pressure justifies a runtime heap.
- `0005-execution-path-retention.md`: retain one canonical direct VM and delete
  the narrow slot, compact-call, and loop-kernel experiments.
