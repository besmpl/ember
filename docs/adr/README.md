# Architecture Decision Records

Use ADRs for compatibility, runtime, or public-interface decisions that future
work will need to understand.

Some historical performance ADRs name Hearth because it supplied Ember's first
production workload. Those references are workload evidence, not runtime API or
domain-model requirements.

Keep each ADR focused:

- context;
- decision;
- consequences;
- alternatives considered.

Current runtime decisions:

- `0001-go-native-runtime-mapping.md`: public Go-object mapping and Go lifetime;
  ADR 0007 supersedes its ordinary-Go-object default for hot internal state.
- `0004-runtime-slot-handle-ownership.md`: private slot/handle ownership when
  measured internal VM pressure justifies a runtime heap; ADR 0007 supersedes
  its pointer-slab and hot-generation design while retaining explicit owners.
- `0005-execution-path-retention.md`: retain one canonical direct VM and delete
  the narrow slot, compact-call, and loop-kernel experiments; ADR 0007 makes
  that VM the migration oracle rather than the final engine.
- `0006-persistent-table-allocation-gate.md`: stop the table allocator campaign
  when fresh first-host profiles show that persistent updates are dominated by
  runtime call-boundary setup instead.
- `0007-compact-production-machine.md`: accepted `CodeImage`/owner-bound
  `Machine` architecture, pure-Go production kernel, migration invariants, and
  supersession map. ADR 0008 retains the Machine as the complete fallback but
  supersedes its final-backend and performance-target clauses.
- `0008-ssa-aot-for-luau-parity.md`: select build-time SSA/value/shape-
  specialized AOT to ordinary Go for prepared parity, retain exact Machine side
  exits, and defer static assembly to a profile-proven second lowerer.
- `0009-generated-adaptive-superword-vm.md`: records the rejected generated
  adaptive shadow-wordcode experiment, its exact P4 gate failure, and deletion
  from production after scalar wins failed to transfer to object workloads.
