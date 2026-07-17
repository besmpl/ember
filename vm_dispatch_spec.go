package ember

//go:generate go run ./cmd/ember-vmgen

// directFrameSemanticSpec is the closed, workload-independent semantic
// inventory for generated direct-frame handlers. The generator joins these
// adaptive families and tiling boundaries to the canonical opcode metadata;
// benchmark names, source identity, and literal values have no representation
// in this schema.
const directFrameSemanticSpec = `
# opcode                       specialization family tiling boundary cache layout
opLoadConst                    none                  pure            none
opLoadGlobal                   global-read           guarded         global
opSetGlobal                    none                  barrier         none
opNewTable                     none                  barrier         none
opSetField                     field-write           barrier         field
opSetStringField               field-write           barrier         field
opSetStringFieldIndex          field-write           barrier         field
opGetStringField               field-read            guarded         field
opGetStringFieldIndex          field-read            guarded         field
opSetIndex                     index-write           barrier         index
opGetIndex                     index-read            guarded         index
opClosure                      none                  barrier         none
opGetUpvalue                   none                  pure            none
opSetUpvalue                   none                  barrier         none
opVararg                       none                  barrier         none
opPrepareIter                  iteration             barrier         iteration
opArrayNext                    iteration             guarded         iteration
opArrayNextJump2               iteration             terminal        iteration
opMove                         none                  pure            none
opAdd                          number-binary         guarded         type
opSub                          number-binary         guarded         type
opMul                          number-binary         guarded         type
opDiv                          number-binary         guarded         type
opMod                          number-binary         guarded         type
opIDiv                         number-binary         guarded         type
opAddK                         number-constant       guarded         type
opSubK                         number-constant       guarded         type
opMulK                         number-constant       guarded         type
opDivK                         number-constant       guarded         type
opModK                         number-constant       guarded         type
opIDivK                        number-constant       guarded         type
opPow                          number-binary         guarded         type
opNeg                          number-unary          guarded         type
opLen                          length                guarded         type
opConcat                       concat                guarded         type
opConcatChain                  concat                guarded         type
opEqual                        compare               guarded         type
opNotEqual                     compare               guarded         type
opLess                         compare               guarded         type
opLessEqual                    compare               guarded         type
opGreater                      compare               guarded         type
opGreaterEqual                 compare               guarded         type
opNumericForCheck              numeric-loop          terminal        type
opNumericForLoop               numeric-loop          terminal        type
opJumpIfNotEqualK              compare               terminal        type
opJumpIfNotLessK               compare               terminal        type
opJumpIfNotGreaterK            compare               terminal        type
opJumpIfLessK                  compare               terminal        type
opJumpIfGreaterK               compare               terminal        type
opJumpIfNotLess                compare               terminal        type
opJumpIfNotGreater             compare               terminal        type
opJumpIfLess                   compare               terminal        type
opJumpIfGreater                compare               terminal        type
opJumpIfTableHasMetatable      table-shape           terminal        shape
opFastCall                     builtin-call          barrier         call
opJumpIfFalse                  truth-branch          terminal        type
opCall                         direct-call           barrier         call
opCallOne                      direct-call           barrier         call
opCallLocalOne                 direct-call           barrier         call
opCallUpvalueOne               direct-call           barrier         call
opCallMethodOne                direct-call           barrier         call
opJump                         none                  terminal        none
opReturnOne                    none                  barrier         none
opReturn                       none                  barrier         none
`

// directFrameFusionSpec is the closed inventory of multi-instruction handlers.
// Entries describe semantic shapes only: they cannot name sources, benchmarks,
// constants, or profiles. The instruction cap bounds tiling and execution.
const directFrameFusionSpec = `
# name             family        instruction cap
numeric-for-trace  numeric-loop  16
fixed-self-call    direct-call   1
fixed-self-call-trace direct-call 3
compact-self-function direct-call 16
`
