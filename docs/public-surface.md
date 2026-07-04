# Public Surface

Ember's public surface should stay smaller than its implementation.

## Initial Import

```go
import "github.com/besmpl/ember"
```

The root package starts as the default import. Add subpackages only when a
slice proves that the split reduces public complexity or creates a useful
testable seam.

## Surface Rules

- Export only names that callers need for a proven slice.
- Document exported behavior, errors, defaults, and side effects.
- Keep host integration explicit; do not hide Hearth or process ownership in
  the runtime.
- Do not expose internal bytecode, parser, stack, or GC machinery for
  convenience.
- Prefer stable plain values and small interfaces over large configuration
  graphs.

## Current Surface

- `Compile(source string) (*Proto, error)` compiles the currently supported
  source slice.
- `Run(proto *Proto) ([]Value, error)` executes without host globals.
- `RunWithGlobals(proto *Proto, globals map[string]Value) ([]Value, error)`
  executes with explicit host-provided globals.
- `Value` exposes kind-specific inspectors for nil, booleans, numbers,
  strings, and tables.
- `NewTable() *Table` creates a host-visible Luau table.
- `TableValue(*Table) Value` passes a table through scripts and host globals.
- `Value.Table() (*Table, bool)` returns the backing table object.
- `(*Table).Get(Value) (Value, error)` and `(*Table).Set(Value, Value) error`
  provide table storage through the same key semantics the VM currently
  supports: booleans, strings, numbers except NaN, and table identity keys.
  Missing keys read as nil; setting nil deletes the key. Nil and NaN keys
  return errors.
- `HostFunc` and `HostFuncValue` let adapters expose Go callbacks to scripts.

Do not expose parser, bytecode, register, or stack details unless a later slice
proves callers need them.

## Likely Future Packages

These are candidates, not commitments:

- `bytecode`: instruction formats and decoding.
- `compile`: source-to-bytecode compilation.
- `vm`: execution internals if the root package gets too crowded.
- `conformance`: test harness helpers.
- `hearth`: optional Hearth adapter above the core runtime.

Do not create these until implementation pressure proves they are useful.
