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

## Likely Future Packages

These are candidates, not commitments:

- `bytecode`: instruction formats and decoding.
- `compile`: source-to-bytecode compilation.
- `vm`: execution internals if the root package gets too crowded.
- `conformance`: test harness helpers.
- `hearth`: optional Hearth adapter above the core runtime.

Do not create these until implementation pressure proves they are useful.
