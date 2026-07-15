package ember

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type opcode uint8

const (
	_ opcode = iota
	opLoadConst
	opLoadGlobal
	opSetGlobal
	opMove
	opNewTable
	opSetField
	_
	opSetStringField
	opSetStringFieldIndex
	opGetStringField
	opGetStringFieldIndex
	_
	_
	opSetIndex
	opGetIndex
	opClosure
	opGetUpvalue
	opSetUpvalue
	opVararg
	opPrepareIter
	opArrayNext
	opArrayNextJump2
	opAdd
	opSub
	opMul
	opDiv
	opMod
	opIDiv
	opPow
	opNeg
	opLen
	opConcat
	opConcatChain
	opAddK
	opSubK
	opMulK
	opDivK
	opModK
	opIDivK
	opEqual
	opNotEqual
	opLess
	opLessEqual
	opGreater
	opGreaterEqual
	opNumericForCheck
	opNumericForLoop
	opJumpIfNotEqualK
	opJumpIfNotLessK
	opJumpIfNotGreaterK
	opJumpIfLessK
	opJumpIfGreaterK
	opJumpIfNotLess
	opJumpIfNotGreater
	opJumpIfLess
	opJumpIfGreater
	_
	opJumpIfTableHasMetatable
	_
	_
	_
	_
	_
	_
	_
	_
	_
	opFastCall
	opCall
	opCallOne
	opCallLocalOne
	opCallUpvalueOne
	opCallMethodOne
	opJumpIfFalse
	opJump
	opReturnOne
	opReturn
	opcodeLimit
)

var allOpcodes = [...]opcode{
	opLoadConst,
	opLoadGlobal,
	opSetGlobal,
	opNewTable,
	opSetField,
	opSetStringField,
	opSetStringFieldIndex,
	opGetStringField,
	opGetStringFieldIndex,
	opSetIndex,
	opGetIndex,
	opClosure,
	opGetUpvalue,
	opSetUpvalue,
	opVararg,
	opPrepareIter,
	opArrayNext,
	opArrayNextJump2,
	opMove,
	opAdd,
	opSub,
	opMul,
	opDiv,
	opMod,
	opIDiv,
	opAddK,
	opSubK,
	opMulK,
	opDivK,
	opModK,
	opIDivK,
	opPow,
	opNeg,
	opLen,
	opConcat,
	opConcatChain,
	opEqual,
	opNotEqual,
	opLess,
	opLessEqual,
	opGreater,
	opGreaterEqual,
	opNumericForCheck,
	opNumericForLoop,
	opJumpIfNotEqualK,
	opJumpIfNotLessK,
	opJumpIfNotGreaterK,
	opJumpIfLessK,
	opJumpIfGreaterK,
	opJumpIfNotLess,
	opJumpIfNotGreater,
	opJumpIfLess,
	opJumpIfGreater,
	opJumpIfTableHasMetatable,
	opFastCall,
	opJumpIfFalse,
	opCall,
	opCallOne,
	opCallLocalOne,
	opCallUpvalueOne,
	opCallMethodOne,
	opJump,
	opReturnOne,
	opReturn,
}

// machineEligibleOpcodes is consumed by both opcode metadata and vmgen. Keep
// the compact Machine switch generated from this list so selection and
// execution cannot drift apart.
var machineEligibleOpcodes = [...]opcode{
	opLoadConst,
	opLoadGlobal,
	opSetGlobal,
	opMove,
	opNewTable,
	opSetField,
	opSetStringField,
	opSetStringFieldIndex,
	opGetStringField,
	opGetStringFieldIndex,
	opSetIndex,
	opGetIndex,
	opClosure,
	opGetUpvalue,
	opSetUpvalue,
	opVararg,
	opPrepareIter,
	opArrayNext,
	opArrayNextJump2,
	opAdd,
	opSub,
	opMul,
	opDiv,
	opMod,
	opIDiv,
	opPow,
	opNeg,
	opConcat,
	opConcatChain,
	opAddK,
	opSubK,
	opMulK,
	opDivK,
	opModK,
	opIDivK,
	opEqual,
	opNotEqual,
	opLess,
	opLessEqual,
	opGreater,
	opGreaterEqual,
	opNumericForCheck,
	opNumericForLoop,
	opJumpIfNotEqualK,
	opJumpIfNotLessK,
	opJumpIfNotGreaterK,
	opJumpIfLessK,
	opJumpIfGreaterK,
	opJumpIfNotLess,
	opJumpIfNotGreater,
	opJumpIfLess,
	opJumpIfGreater,
	opJumpIfTableHasMetatable,
	opJumpIfFalse,
	opJump,
	opFastCall,
	opCall,
	opCallOne,
	opCallLocalOne,
	opCallUpvalueOne,
	opCallMethodOne,
	opReturnOne,
	opReturn,
}

const opcodeCount = len(allOpcodes)

type opcodeMetadataEntry struct {
	name                         string
	directFrame                  bool
	machine                      opcodeMachinePolicy
	controlFlow                  opcodeControlFlowKind
	jumpTarget                   opcodeJumpTargetSlot
	operands                     opcodeOperandShape
	registerEffects              opcodeRegisterEffects
	effects                      opcodeEffects
	directFrameUnsupportedReason string
	wordcode                     wordcodeEncodingMetadata
}

type opcodeMachineSafepoint uint8

const (
	opcodeMachineSafepointUnclassified opcodeMachineSafepoint = iota
	opcodeMachineSafepointGuest
)

type opcodeMachineErrorClass uint8

const (
	opcodeMachineErrorNone opcodeMachineErrorClass = iota
	opcodeMachineErrorOperands
	opcodeMachineErrorNumericFor
	opcodeMachineErrorTable
)

// opcodeMachinePolicy is the single classification used when lowering and
// executing the compact scalar Machine. Unsupported opcodes are classified
// just as deliberately as supported ones so image selection fails closed.
type opcodeMachinePolicy struct {
	classified  bool
	eligible    bool
	guestCharge uint8
	safepoint   opcodeMachineSafepoint
	errorClass  opcodeMachineErrorClass
}

type opcodeEffects struct {
	classified                  bool
	invokesScriptOrHostCode     bool
	mayYield                    bool
	mayError                    bool
	allocatesOrObservesIdentity bool
	readsGlobals                bool
	writesGlobals               bool
	readsUpvalues               bool
	writesUpvalues              bool
	readsTables                 bool
	writesTables                bool
	readsUnknownHeap            bool
	writesUnknownHeap           bool
}

type opcodeOperandShape struct {
	a bytecodeOperandKind
	b bytecodeOperandKind
	c bytecodeOperandKind
	d bytecodeOperandKind
}

var opcodeMetadataTable = func() [opcodeLimit]opcodeMetadataEntry {
	var table [opcodeLimit]opcodeMetadataEntry
	for _, op := range allOpcodes {
		table[op].name = opcodeName(op)
		table[op].registerEffects.classified = true
		table[op].effects.classified = true
		table[op].machine = opcodeMachinePolicy{
			classified:  true,
			guestCharge: 1,
			safepoint:   opcodeMachineSafepointGuest,
		}
	}
	for _, op := range machineEligibleOpcodes {
		policy := table[op].machine
		policy.eligible = true
		table[op].machine = policy
	}
	for _, op := range []opcode{
		opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opNeg,
		opAddK, opSubK, opMulK, opDivK, opModK, opIDivK,
		opLess, opLessEqual, opGreater, opGreaterEqual,
		opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK,
		opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater,
	} {
		policy := table[op].machine
		policy.errorClass = opcodeMachineErrorOperands
		table[op].machine = policy
	}
	policy := table[opNumericForCheck].machine
	policy.errorClass = opcodeMachineErrorNumericFor
	table[opNumericForCheck].machine = policy
	for _, op := range []opcode{opNewTable, opSetField, opSetStringField, opGetStringField, opSetIndex, opGetIndex} {
		policy := table[op].machine
		policy.errorClass = opcodeMachineErrorTable
		table[op].machine = policy
	}
	for _, op := range allOpcodes {
		table[op].directFrame = true
	}
	for _, op := range allOpcodes {
		if !table[op].directFrame {
			table[op].directFrameUnsupportedReason = "opcode is not handled by the direct-frame runner"
		}
	}
	for _, op := range []opcode{opJump} {
		table[op].controlFlow = opcodeControlJump
		table[op].jumpTarget = opcodeJumpTargetB
	}
	for _, op := range []opcode{opNumericForLoop} {
		table[op].controlFlow = opcodeControlJump
		table[op].jumpTarget = opcodeJumpTargetD
	}
	for _, op := range []opcode{
		opJumpIfFalse,
	} {
		table[op].controlFlow = opcodeControlBranch
		table[op].jumpTarget = opcodeJumpTargetB
	}
	for _, op := range []opcode{
		opArrayNextJump2,
		opNumericForCheck,
		opJumpIfNotEqualK,
		opJumpIfNotLessK,
		opJumpIfNotGreaterK,
		opJumpIfLessK,
		opJumpIfGreaterK,
		opJumpIfNotLess,
		opJumpIfNotGreater,
		opJumpIfLess,
		opJumpIfGreater,
		opJumpIfTableHasMetatable,
	} {
		table[op].controlFlow = opcodeControlBranch
		table[op].jumpTarget = opcodeJumpTargetD
	}
	for _, op := range []opcode{opReturnOne, opReturn} {
		table[op].controlFlow = opcodeControlReturn
	}
	callbackMask := opcodeEffects{
		classified:                  true,
		invokesScriptOrHostCode:     true,
		mayYield:                    true,
		mayError:                    true,
		allocatesOrObservesIdentity: true,
		readsGlobals:                true,
		writesGlobals:               true,
		readsUpvalues:               true,
		writesUpvalues:              true,
		readsTables:                 true,
		writesTables:                true,
		readsUnknownHeap:            true,
		writesUnknownHeap:           true,
	}
	for _, op := range []opcode{
		opSetField,
		opGetStringField,
		opSetStringField,
		opGetStringFieldIndex,
		opSetStringFieldIndex,
		opGetIndex,
		opSetIndex,
		opPrepareIter,
		opArrayNext,
		opArrayNextJump2,
		opAdd,
		opSub,
		opMul,
		opDiv,
		opMod,
		opIDiv,
		opPow,
		opNeg,
		opAddK,
		opSubK,
		opMulK,
		opDivK,
		opModK,
		opIDivK,
		opLen,
		opConcat,
		opConcatChain,
		opEqual,
		opNotEqual,
		opLess,
		opLessEqual,
		opGreater,
		opGreaterEqual,
		opJumpIfNotEqualK,
		opJumpIfNotLessK,
		opJumpIfNotGreaterK,
		opJumpIfLessK,
		opJumpIfGreaterK,
		opJumpIfNotLess,
		opJumpIfNotGreater,
		opJumpIfLess,
		opJumpIfGreater,
		opFastCall,
		opCall,
		opCallOne,
		opCallLocalOne,
		opCallUpvalueOne,
		opCallMethodOne,
	} {
		table[op].effects = callbackMask
	}
	table[opLoadGlobal].effects.readsGlobals = true
	table[opSetGlobal].effects.writesGlobals = true
	for _, op := range []opcode{opGetUpvalue, opClosure} {
		table[op].effects.readsUpvalues = true
	}
	table[opSetUpvalue].effects.writesUpvalues = true
	for _, op := range []opcode{
		opGetStringField,
		opGetStringFieldIndex,
		opGetIndex,
		opPrepareIter,
		opArrayNext,
		opArrayNextJump2,
		opJumpIfTableHasMetatable,
		opFastCall,
		opCallMethodOne,
	} {
		table[op].effects.readsTables = true
	}
	for _, op := range []opcode{
		opSetField,
		opSetStringField,
		opSetStringFieldIndex,
		opSetIndex,
		opFastCall,
	} {
		table[op].effects.writesTables = true
	}
	for _, op := range []opcode{
		opNewTable,
		opClosure,
		opVararg,
	} {
		table[op].effects.allocatesOrObservesIdentity = true
	}
	table[opNumericForCheck].effects.mayError = true

	unused := bytecodeOperandUnused
	register := bytecodeOperandRegister
	constant := bytecodeOperandConstant
	prototype := bytecodeOperandPrototype
	upvalue := bytecodeOperandUpvalue
	jumpTarget := bytecodeOperandJumpTarget
	count := bytecodeOperandCount
	setOperands := func(op opcode, a, b, c, d bytecodeOperandKind) {
		table[op].operands = opcodeOperandShape{a: a, b: b, c: c, d: d}
	}
	setOperands(opLoadConst, register, constant, unused, unused)
	setOperands(opLoadGlobal, register, constant, bytecodeOperandGlobalSlot, unused)
	setOperands(opSetGlobal, constant, register, bytecodeOperandGlobalSlot, unused)
	setOperands(opMove, register, register, unused, unused)
	setOperands(opNewTable, register, count, count, unused)
	setOperands(opSetField, register, constant, register, unused)
	setOperands(opSetStringField, register, constant, register, unused)
	setOperands(opSetStringFieldIndex, register, constant, register, register)
	setOperands(opGetStringField, register, register, constant, unused)
	setOperands(opGetStringFieldIndex, register, register, constant, register)
	setOperands(opSetIndex, register, register, register, unused)
	setOperands(opGetIndex, register, register, register, unused)
	setOperands(opClosure, register, prototype, unused, unused)
	setOperands(opGetUpvalue, register, upvalue, unused, unused)
	setOperands(opSetUpvalue, upvalue, register, unused, unused)
	setOperands(opVararg, register, count, unused, unused)
	setOperands(opPrepareIter, register, register, register, unused)
	setOperands(opArrayNext, register, register, register, count)
	setOperands(opArrayNextJump2, register, register, register, jumpTarget)
	setOperands(opAdd, register, register, register, unused)
	setOperands(opSub, register, register, register, unused)
	setOperands(opMul, register, register, register, unused)
	setOperands(opDiv, register, register, register, unused)
	setOperands(opMod, register, register, register, unused)
	setOperands(opIDiv, register, register, register, unused)
	setOperands(opPow, register, register, register, unused)
	setOperands(opNeg, register, register, unused, unused)
	setOperands(opLen, register, register, unused, unused)
	setOperands(opConcat, register, register, register, unused)
	setOperands(opConcatChain, register, register, count, unused)
	setOperands(opAddK, register, register, constant, unused)
	setOperands(opSubK, register, register, constant, unused)
	setOperands(opMulK, register, register, constant, unused)
	setOperands(opDivK, register, register, constant, unused)
	setOperands(opModK, register, register, constant, unused)
	setOperands(opIDivK, register, register, constant, unused)
	setOperands(opEqual, register, register, register, unused)
	setOperands(opNotEqual, register, register, register, unused)
	setOperands(opLess, register, register, register, unused)
	setOperands(opLessEqual, register, register, register, unused)
	setOperands(opGreater, register, register, register, unused)
	setOperands(opGreaterEqual, register, register, register, unused)
	setOperands(opNumericForCheck, register, register, register, jumpTarget)
	setOperands(opNumericForLoop, register, register, unused, jumpTarget)
	setOperands(opJumpIfNotEqualK, register, constant, unused, jumpTarget)
	setOperands(opJumpIfNotLessK, register, constant, unused, jumpTarget)
	setOperands(opJumpIfNotGreaterK, register, constant, unused, jumpTarget)
	setOperands(opJumpIfLessK, register, constant, unused, jumpTarget)
	setOperands(opJumpIfGreaterK, register, constant, unused, jumpTarget)
	setOperands(opJumpIfNotLess, register, register, unused, jumpTarget)
	setOperands(opJumpIfNotGreater, register, register, unused, jumpTarget)
	setOperands(opJumpIfLess, register, register, unused, jumpTarget)
	setOperands(opJumpIfGreater, register, register, unused, jumpTarget)
	setOperands(opJumpIfTableHasMetatable, register, unused, unused, jumpTarget)
	setOperands(opFastCall, register, bytecodeOperandNativeID, count, count)
	setOperands(opCall, register, register, count, count)
	setOperands(opCallOne, register, register, count, count)
	setOperands(opCallLocalOne, register, register, register, count)
	setOperands(opCallUpvalueOne, register, upvalue, register, count)
	setOperands(opCallMethodOne, register, register, constant, count)
	setOperands(opJumpIfFalse, register, jumpTarget, unused, unused)
	setOperands(opJump, unused, jumpTarget, unused, unused)
	setOperands(opReturnOne, register, unused, unused, unused)
	setOperands(opReturn, register, count, unused, unused)
	for _, op := range allOpcodes {
		table[op].wordcode = wordcodeMetadataFor(op)
	}
	setRegisterEffects := func(op opcode, fixed []opcodeRegisterEffect, spans []opcodeRegisterSpan) {
		table[op].registerEffects = newOpcodeRegisterEffects(fixed, spans)
	}
	read := instructionRegisterRead
	write := instructionRegisterWrite
	readWrite := instructionRegisterReadWrite
	setRegisterEffects(opLoadConst, []opcodeRegisterEffect{registerEffect(registerEffectSlotA, 0, write)}, nil)
	setRegisterEffects(opLoadGlobal, []opcodeRegisterEffect{registerEffect(registerEffectSlotA, 0, write)}, nil)
	setRegisterEffects(opSetGlobal, []opcodeRegisterEffect{registerEffect(registerEffectSlotB, 0, read)}, nil)
	setRegisterEffects(opMove, []opcodeRegisterEffect{
		registerEffect(registerEffectSlotA, 0, write), registerEffect(registerEffectSlotB, 0, read),
	}, nil)
	setRegisterEffects(opNewTable, []opcodeRegisterEffect{registerEffect(registerEffectSlotA, 0, write)}, nil)
	setRegisterEffects(opSetField, []opcodeRegisterEffect{
		registerEffect(registerEffectSlotA, 0, read), registerEffect(registerEffectSlotC, 0, read),
	}, nil)
	setRegisterEffects(opSetStringField, []opcodeRegisterEffect{
		registerEffect(registerEffectSlotA, 0, read), registerEffect(registerEffectSlotC, 0, read),
	}, nil)
	setRegisterEffects(opSetStringFieldIndex, []opcodeRegisterEffect{
		registerEffect(registerEffectSlotA, 0, read), registerEffect(registerEffectSlotC, 0, read), registerEffect(registerEffectSlotD, 0, read),
	}, nil)
	setRegisterEffects(opGetStringField, []opcodeRegisterEffect{
		registerEffect(registerEffectSlotA, 0, write), registerEffect(registerEffectSlotB, 0, read),
	}, nil)
	setRegisterEffects(opGetStringFieldIndex, []opcodeRegisterEffect{
		registerEffect(registerEffectSlotA, 0, write), registerEffect(registerEffectSlotB, 0, read), registerEffect(registerEffectSlotD, 0, read),
	}, nil)
	setRegisterEffects(opSetIndex, []opcodeRegisterEffect{
		registerEffect(registerEffectSlotA, 0, read), registerEffect(registerEffectSlotB, 0, read), registerEffect(registerEffectSlotC, 0, read),
	}, nil)
	setRegisterEffects(opGetIndex, []opcodeRegisterEffect{
		registerEffect(registerEffectSlotA, 0, write), registerEffect(registerEffectSlotB, 0, read), registerEffect(registerEffectSlotC, 0, read),
	}, nil)
	setRegisterEffects(opClosure, []opcodeRegisterEffect{registerEffect(registerEffectSlotA, 0, write)}, nil)
	setRegisterEffects(opGetUpvalue, []opcodeRegisterEffect{registerEffect(registerEffectSlotA, 0, write)}, nil)
	setRegisterEffects(opSetUpvalue, []opcodeRegisterEffect{registerEffect(registerEffectSlotB, 0, read)}, nil)
	setRegisterEffects(opVararg, nil, []opcodeRegisterSpan{
		registerSpan(registerEffectSlotA, 0, registerEffectSlotB, registerEffectSpanOpenOrOne, write),
	})
	setRegisterEffects(opPrepareIter, []opcodeRegisterEffect{
		registerEffect(registerEffectSlotA, 0, readWrite), registerEffect(registerEffectSlotB, 0, write), registerEffect(registerEffectSlotC, 0, write),
	}, nil)
	setRegisterEffects(opArrayNext, []opcodeRegisterEffect{
		registerEffect(registerEffectSlotA, 0, read), registerEffect(registerEffectSlotB, 0, read), registerEffect(registerEffectSlotC, 0, read),
	}, []opcodeRegisterSpan{
		registerSpan(registerEffectSlotA, 0, registerEffectSlotD, registerEffectSpanPositiveCount, write),
	})
	setRegisterEffects(opArrayNextJump2, []opcodeRegisterEffect{
		registerEffect(registerEffectSlotA, 0, read), registerEffect(registerEffectSlotB, 0, read), registerEffect(registerEffectSlotC, 0, read),
		registerEffect(registerEffectSlotA, 0, write), registerEffect(registerEffectSlotA, 1, write),
	}, nil)
	for _, op := range []opcode{opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opConcat, opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual} {
		setRegisterEffects(op, []opcodeRegisterEffect{
			registerEffect(registerEffectSlotA, 0, write), registerEffect(registerEffectSlotB, 0, read), registerEffect(registerEffectSlotC, 0, read),
		}, nil)
	}
	setRegisterEffects(opConcatChain, []opcodeRegisterEffect{registerEffect(registerEffectSlotA, 0, write)}, []opcodeRegisterSpan{
		registerSpan(registerEffectSlotB, 0, registerEffectSlotC, registerEffectSpanPositiveCount, read),
	})
	for _, op := range []opcode{opAddK, opSubK, opMulK, opDivK, opModK, opIDivK} {
		setRegisterEffects(op, []opcodeRegisterEffect{
			registerEffect(registerEffectSlotA, 0, write), registerEffect(registerEffectSlotB, 0, read),
		}, nil)
	}
	for _, op := range []opcode{opNeg, opLen} {
		setRegisterEffects(op, []opcodeRegisterEffect{
			registerEffect(registerEffectSlotA, 0, write), registerEffect(registerEffectSlotB, 0, read),
		}, nil)
	}
	setRegisterEffects(opNumericForCheck, []opcodeRegisterEffect{
		registerEffect(registerEffectSlotA, 0, read), registerEffect(registerEffectSlotB, 0, read), registerEffect(registerEffectSlotC, 0, read),
	}, nil)
	setRegisterEffects(opNumericForLoop, []opcodeRegisterEffect{
		registerEffect(registerEffectSlotA, 0, readWrite), registerEffect(registerEffectSlotB, 0, read),
	}, nil)
	for _, op := range []opcode{opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK, opJumpIfTableHasMetatable} {
		setRegisterEffects(op, []opcodeRegisterEffect{registerEffect(registerEffectSlotA, 0, read)}, nil)
	}
	for _, op := range []opcode{opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater} {
		setRegisterEffects(op, []opcodeRegisterEffect{
			registerEffect(registerEffectSlotA, 0, read), registerEffect(registerEffectSlotB, 0, read),
		}, nil)
	}
	setRegisterEffects(opFastCall, []opcodeRegisterEffect{registerEffect(registerEffectSlotA, 0, write)}, []opcodeRegisterSpan{
		registerSpan(registerEffectSlotA, 0, registerEffectSlotC, registerEffectSpanPositiveCount, read),
		registerSpan(registerEffectSlotA, 0, registerEffectSlotD, registerEffectSpanPositiveCount, write),
	})
	setRegisterEffects(opCall, []opcodeRegisterEffect{
		registerEffect(registerEffectSlotA, 0, write), registerEffect(registerEffectSlotB, 0, read),
	}, []opcodeRegisterSpan{
		registerSpan(registerEffectSlotB, 1, registerEffectSlotC, registerEffectSpanSignedCount, read),
		registerSpan(registerEffectSlotA, 0, registerEffectSlotD, registerEffectSpanOpenOrOne, write),
	})
	setRegisterEffects(opCallOne, []opcodeRegisterEffect{
		registerEffect(registerEffectSlotA, 0, write), registerEffect(registerEffectSlotB, 0, read),
	}, []opcodeRegisterSpan{
		registerSpan(registerEffectSlotB, 1, registerEffectSlotC, registerEffectSpanSignedCount, read),
	})
	setRegisterEffects(opCallLocalOne, []opcodeRegisterEffect{
		registerEffect(registerEffectSlotA, 0, write), registerEffect(registerEffectSlotB, 0, read),
	}, []opcodeRegisterSpan{
		registerSpan(registerEffectSlotC, 0, registerEffectSlotD, registerEffectSpanSignedCount, read),
	})
	setRegisterEffects(opCallUpvalueOne, []opcodeRegisterEffect{registerEffect(registerEffectSlotA, 0, write)}, []opcodeRegisterSpan{
		registerSpan(registerEffectSlotC, 0, registerEffectSlotD, registerEffectSpanSignedCount, read),
	})
	setRegisterEffects(opCallMethodOne, []opcodeRegisterEffect{
		registerEffect(registerEffectSlotA, 0, write), registerEffect(registerEffectSlotA, 1, write), registerEffect(registerEffectSlotB, 0, read),
	}, []opcodeRegisterSpan{
		registerSpan(registerEffectSlotA, 2, registerEffectSlotD, registerEffectSpanSignedCount, read),
	})
	setRegisterEffects(opJumpIfFalse, []opcodeRegisterEffect{registerEffect(registerEffectSlotA, 0, read)}, nil)
	setRegisterEffects(opReturnOne, []opcodeRegisterEffect{registerEffect(registerEffectSlotA, 0, read)}, nil)
	setRegisterEffects(opReturn, nil, []opcodeRegisterSpan{
		registerSpan(registerEffectSlotA, 0, registerEffectSlotB, registerEffectSpanSignedCount, read),
	})
	return table
}()

func init() {
	if err := validateOpcodeMetadataTable(opcodeMetadataTable); err != nil {
		panic(err)
	}
}

func opcodeMetadata(op opcode) (opcodeMetadataEntry, bool) {
	if op >= opcodeLimit {
		return opcodeMetadataEntry{}, false
	}
	meta := opcodeMetadataTable[op]
	return meta, meta.name != ""
}

func validateOpcodeMetadataTable(table [opcodeLimit]opcodeMetadataEntry) error {
	for _, op := range allOpcodes {
		meta := table[op]
		if meta.name == "" {
			return fmt.Errorf("%s metadata missing name", opcodeName(op))
		}
		if !meta.effects.classified {
			return fmt.Errorf("%s effects are unclassified", opcodeName(op))
		}
		if !meta.machine.classified {
			return fmt.Errorf("%s Machine policy is unclassified", opcodeName(op))
		}
		if meta.machine.guestCharge == 0 {
			return fmt.Errorf("%s Machine guest charge is zero", opcodeName(op))
		}
		if meta.machine.safepoint == opcodeMachineSafepointUnclassified {
			return fmt.Errorf("%s Machine safepoint is unclassified", opcodeName(op))
		}
		if err := validateOpcodeRegisterEffects(meta.registerEffects); err != nil {
			return fmt.Errorf("%s %w", opcodeName(op), err)
		}
		if meta.directFrame && meta.directFrameUnsupportedReason != "" {
			return fmt.Errorf("%s direct-frame metadata has unsupported reason", opcodeName(op))
		}
		if !meta.directFrame && meta.directFrameUnsupportedReason == "" {
			return fmt.Errorf("%s direct-frame metadata missing unsupported reason", opcodeName(op))
		}
		if meta.operands == (opcodeOperandShape{}) {
			return fmt.Errorf("%s metadata missing operand shape", opcodeName(op))
		}
		if err := wordcodeValidateMetadata(op, meta); err != nil {
			return fmt.Errorf("%s %w", opcodeName(op), err)
		}
		if (meta.controlFlow == opcodeControlJump || meta.controlFlow == opcodeControlBranch) && meta.jumpTarget == opcodeJumpTargetNone {
			return fmt.Errorf("%s control flow without jump target", opcodeName(op))
		}
		if meta.controlFlow == opcodeControlReturn && meta.jumpTarget != opcodeJumpTargetNone {
			return fmt.Errorf("%s return has jump target", opcodeName(op))
		}
		if meta.effects.mayYield && !meta.effects.invokesScriptOrHostCode {
			return fmt.Errorf("%s may yield without invoking script or host code", opcodeName(op))
		}
		if !opcodeMetadataJumpTargetMatchesOperands(meta) {
			return fmt.Errorf("%s jump target metadata does not match operand shape", opcodeName(op))
		}
	}
	return nil
}

func opcodeMetadataJumpTargetMatchesOperands(meta opcodeMetadataEntry) bool {
	switch meta.jumpTarget {
	case opcodeJumpTargetNone:
		return meta.operands.b != bytecodeOperandJumpTarget && meta.operands.d != bytecodeOperandJumpTarget
	case opcodeJumpTargetB:
		return meta.operands.b == bytecodeOperandJumpTarget && meta.operands.d != bytecodeOperandJumpTarget
	case opcodeJumpTargetD:
		return meta.operands.d == bytecodeOperandJumpTarget && meta.operands.b != bytecodeOperandJumpTarget
	default:
		return false
	}
}

type instruction struct {
	op opcode
	a  int
	b  int
	c  int
	d  int
}

// encodeFixedCallCount reserves the negative int16 count space used by fixed
// one-result calls for a borrow hint. Generic opCall keeps its existing open
// result/count encoding and must not use this helper.
func encodeFixedCallCount(count int, borrowHint bool) int {
	if !borrowHint {
		return count
	}
	return -(count + 1)
}

// decodeFixedCallCount decodes both ordinary fixed counts and the negative
// borrow marker. Range validation belongs to packing and instruction
// verification; this helper intentionally stays total so static consumers do
// not duplicate the marker arithmetic.
func decodeFixedCallCount(raw int) (count int, borrowHint bool) {
	if raw < 0 {
		return -(raw + 1), true
	}
	return raw, false
}

// encodeFixedMultiResultCount reserves the negative result-count space below
// the valid open-prefix range.  An open CALL result count is -prefix-1, where
// prefix is bounded by the caller register count.  Values below that bound
// therefore unambiguously carry a fixed positive result count and a compiler
// liveness proof that the caller suffix may be borrowed.
func encodeFixedMultiResultCount(count, registers int) int {
	return -(registers + count + 1)
}

func decodeFixedMultiResultCount(raw, registers int) (count int, borrowHint bool) {
	if decodeOpenResultCallMarker(raw) {
		return raw, false
	}
	if raw >= -(registers + 1) {
		return raw, false
	}
	count = -raw - registers - 1
	if count < 2 {
		return raw, false
	}
	return count, true
}

// Open-argument CALL markers occupy a reserved int16 band. The marker carries
// the number of fixed prefix arguments before the immediately preceding open
// producer's dynamic tail. Keep this band distinct from the open-result marker
// used in the d operand.
const (
	openArgumentCallMarkerMin = -32767
	openArgumentCallMarkerMax = -32512
	openResultCallMarker      = -1 << 15
)

func encodeOpenArgumentCallMarker(prefixCount int) (int, bool) {
	if prefixCount < 0 || prefixCount > openArgumentCallMarkerMax-openArgumentCallMarkerMin {
		return 0, false
	}
	return openArgumentCallMarkerMin + prefixCount, true
}

func decodeOpenArgumentCallMarker(raw int) (prefixCount int, marked bool) {
	if raw < openArgumentCallMarkerMin || raw > openArgumentCallMarkerMax {
		return 0, false
	}
	return raw - openArgumentCallMarkerMin, true
}

func encodeOpenResultCallMarker() int { return openResultCallMarker }

func decodeOpenResultCallMarker(raw int) bool { return raw == openResultCallMarker }

func normalizeOpenArgumentCallMarkers(code []instruction) []instruction {
	var normalized []instruction
	for index, ins := range code {
		prefixCount, marked := decodeOpenArgumentCallMarker(ins.c)
		if ins.op != opCall || !marked {
			continue
		}
		if normalized == nil {
			normalized = append([]instruction(nil), code...)
		}
		normalized[index].c = -prefixCount - 1
	}
	if normalized != nil {
		return normalized
	}
	return code
}

func openResultCallFeedsOpenReturn(code []instruction, pc, result int) bool {
	if pc < 0 || pc+1 >= len(code) {
		return false
	}
	next := code[pc+1]
	if next.op != opReturn || next.b >= 0 {
		return false
	}
	prefixCount := -next.b - 1
	return prefixCount >= 0 && next.a+prefixCount == result
}

func openResultCallHasImmediateConsumer(code []instruction, pc, result int) bool {
	if pc < 0 || pc+1 >= len(code) {
		return false
	}
	next := code[pc+1]
	if next.op == opReturn && next.b < 0 {
		prefixCount := -next.b - 1
		return prefixCount >= 0 && next.a+prefixCount == result
	}
	if next.op == opCall && next.c < 0 {
		prefixCount, marked := decodeOpenArgumentCallMarker(next.c)
		if !marked {
			prefixCount = -next.c - 1
		}
		return prefixCount >= 0 && next.b+1+prefixCount == result
	}
	return false
}

func openArgumentCallHasProducer(code []instruction, pc, start, registers int) bool {
	if pc <= 0 || start < 0 || (registers >= 0 && start >= registers) {
		return false
	}
	prev := code[pc-1]
	switch prev.op {
	case opVararg:
		return prev.b < 0 && prev.a == start
	case opCall:
		if prev.d < 0 && prev.a == start {
			if registers >= 0 {
				if _, marked := decodeFixedMultiResultCount(prev.d, registers); marked {
					return false
				}
			}
			return true
		}
		return false
	case opFastCall:
		return prev.d < 0 && prev.a == start
	default:
		return false
	}
}

func normalizeOpenResultCallMarkers(code []instruction) []instruction {
	var normalized []instruction
	for index, ins := range code {
		if ins.op != opCall || !decodeOpenResultCallMarker(ins.d) {
			continue
		}
		if normalized == nil {
			normalized = append([]instruction(nil), code...)
		}
		normalized[index].d = -1
	}
	if normalized != nil {
		return normalized
	}
	return code
}

// normalizeFixedMultiResultCounts removes internal borrow markers before
// rebuilding analysis artifacts. This keeps finalization idempotent: decoded
// wordcode is analyzed with the CALL's semantic fixed result span, then the
// liveness pass may encode the marker again.
func normalizeFixedMultiResultCounts(code []instruction, registers int) []instruction {
	var normalized []instruction
	for index, ins := range code {
		count, marked := decodeFixedMultiResultCount(ins.d, registers)
		if ins.op != opCall || !marked {
			continue
		}
		if normalized == nil {
			normalized = append([]instruction(nil), code...)
		}
		normalized[index].d = count
	}
	if normalized != nil {
		return normalized
	}
	return code
}

type bytecodeOperandKind int

const (
	bytecodeOperandUnused bytecodeOperandKind = iota
	bytecodeOperandRegister
	bytecodeOperandConstant
	bytecodeOperandPrototype
	bytecodeOperandUpvalue
	bytecodeOperandJumpTarget
	bytecodeOperandGlobalSlot
	bytecodeOperandNativeID
	bytecodeOperandCount
)

type bytecodeIRInstruction struct {
	a, b, c, d  int32
	sourceStart uint32
	sourceEnd   uint32
	op          opcode
}

type bytecodeIROperandSlot uint8

const (
	bytecodeIROperandSlotA bytecodeIROperandSlot = iota
	bytecodeIROperandSlotB
	bytecodeIROperandSlotC
	bytecodeIROperandSlotD
)

// bytecodeIROperandIterator walks the used operands without exposing the IR
// storage layout or allocating a temporary slice.
type bytecodeIROperandIterator struct {
	instruction bytecodeIRInstruction
	slot        bytecodeIROperandSlot
}

func (ins bytecodeIRInstruction) opcodeValue() opcode {
	return ins.op
}

func (ins bytecodeIRInstruction) sourceSpan() sourceRange {
	span, err := ins.sourceSpanChecked()
	if err != nil {
		panic(err)
	}
	return span
}

func (ins bytecodeIRInstruction) sourceSpanChecked() (sourceRange, error) {
	maxInt := uint64(^uint(0) >> 1)
	if uint64(ins.sourceStart) > maxInt || uint64(ins.sourceEnd) > maxInt {
		return sourceRange{}, fmt.Errorf("IR source range [%d,%d) overflows int", ins.sourceStart, ins.sourceEnd)
	}
	return sourceRange{start: int(ins.sourceStart), end: int(ins.sourceEnd)}, nil
}

func (ins bytecodeIRInstruction) operandKind(slot bytecodeIROperandSlot) bytecodeOperandKind {
	meta, ok := opcodeMetadata(ins.op)
	if !ok {
		return bytecodeOperandUnused
	}
	switch slot {
	case bytecodeIROperandSlotA:
		return meta.operands.a
	case bytecodeIROperandSlotB:
		return meta.operands.b
	case bytecodeIROperandSlotC:
		return meta.operands.c
	case bytecodeIROperandSlotD:
		return meta.operands.d
	default:
		panic("invalid bytecode IR operand slot")
	}
}

func (ins bytecodeIRInstruction) operandValue(slot bytecodeIROperandSlot) int {
	switch slot {
	case bytecodeIROperandSlotA:
		return int(ins.a)
	case bytecodeIROperandSlotB:
		return int(ins.b)
	case bytecodeIROperandSlotC:
		return int(ins.c)
	case bytecodeIROperandSlotD:
		return int(ins.d)
	default:
		panic("invalid bytecode IR operand slot")
	}
}

func (ins *bytecodeIRInstruction) setOperandValue(slot bytecodeIROperandSlot, value int) bool {
	return ins.setOperandValueChecked(slot, value) == nil
}

func (ins *bytecodeIRInstruction) setOperandValueChecked(slot bytecodeIROperandSlot, value int) error {
	if ins == nil {
		return fmt.Errorf("nil bytecode IR instruction")
	}
	if slot < bytecodeIROperandSlotA || slot > bytecodeIROperandSlotD {
		return fmt.Errorf("invalid bytecode IR operand slot %d", slot)
	}
	if ins.operandKind(slot) == bytecodeOperandUnused {
		return fmt.Errorf("unused bytecode IR operand slot %d", slot)
	}
	encoded, err := int32Checked(value, "setter")
	if err != nil {
		return err
	}
	switch slot {
	case bytecodeIROperandSlotA:
		ins.a = encoded
	case bytecodeIROperandSlotB:
		ins.b = encoded
	case bytecodeIROperandSlotC:
		ins.c = encoded
	case bytecodeIROperandSlotD:
		ins.d = encoded
	default:
		return fmt.Errorf("invalid bytecode IR operand slot %d", slot)
	}
	return nil
}

func (ins bytecodeIRInstruction) operandsIter() bytecodeIROperandIterator {
	return bytecodeIROperandIterator{instruction: ins}
}

func (iterator *bytecodeIROperandIterator) next() (bytecodeIROperandSlot, bytecodeOperandKind, int, bool) {
	for iterator != nil && iterator.slot <= bytecodeIROperandSlotD {
		slot := iterator.slot
		iterator.slot++
		kind := iterator.instruction.operandKind(slot)
		if kind != bytecodeOperandUnused {
			return slot, kind, iterator.instruction.operandValue(slot), true
		}
	}
	return 0, bytecodeOperandUnused, 0, false
}

type bytecodeIRBlock struct {
	id    int
	start int
	end   int
}

type bytecodeIRLivenessBlock struct {
	block   bytecodeIRBlock
	use     registerSet
	def     registerSet
	liveIn  registerSet
	liveOut registerSet
}

type upvalueDesc struct {
	local bool
	index int
	copy  bool
}

type bytecodeBuilder struct {
	constants         []Value
	constantIndices   map[constantPoolKey]int
	constantStrings   map[string]constantStringIntern
	constantShapes    map[string]uint32
	nextConstantShape uint32
	ir                []bytecodeIRInstruction
	prototypes        []*Proto
	source            sourceRange
	sourceText        string
	conversionErr     error
}

func (b *bytecodeBuilder) recordConversionError(err error) {
	if b != nil && b.conversionErr == nil && err != nil {
		b.conversionErr = err
	}
}

type constantPoolKey struct {
	kind ValueKind
	bits uint64
}

type constantStringIntern struct {
	id  uint32
	box *stringBox
}

func (b *bytecodeBuilder) addConstant(value Value) int {
	if valueKind(value) == StringKind {
		return b.addInternedStringConstant(value.stringText(), value.stringBox())
	}
	key, keyed := b.constantKey(value)
	return b.addKeyedConstant(value, key, keyed)
}

func (b *bytecodeBuilder) resetConstants(constants []Value) {
	b.constants = nil
	b.constantIndices = nil
	b.constantStrings = nil
	b.constantShapes = nil
	b.nextConstantShape = 0
	for _, value := range constants {
		b.addConstant(value)
	}
}

func (b *bytecodeBuilder) addStringConstant(text string) int {
	return b.addInternedStringConstant(text, nil)
}

func (b *bytecodeBuilder) addInternedStringConstant(text string, candidate *stringBox) int {
	intern := b.internConstantString(text, candidate)
	return b.addKeyedConstant(stringValueFromBox(intern.box), constantPoolKey{kind: StringKind, bits: uint64(intern.id)}, true)
}

func (b *bytecodeBuilder) addKeyedConstant(value Value, key constantPoolKey, keyed bool) int {
	if keyed && b.constantIndices != nil {
		if index, ok := b.constantIndices[key]; ok {
			return index
		}
	}
	index := len(b.constants)
	b.constants = append(b.constants, value)
	if keyed {
		if b.constantIndices == nil {
			b.constantIndices = make(map[constantPoolKey]int)
		}
		b.constantIndices[key] = index
	}
	return index
}

func (b *bytecodeBuilder) constantKey(value Value) (constantPoolKey, bool) {
	kind := valueKind(value)
	key := constantPoolKey{kind: kind}
	switch kind {
	case NilKind:
		return key, true
	case BoolKind:
		if valueBool(value) {
			key.bits = 1
		}
		return key, true
	case NumberKind:
		key.bits = math.Float64bits(valueNumber(value))
		return key, true
	case HostFuncKind:
		if valueNativeID(value) == nativeFuncUnknown {
			return constantPoolKey{}, false
		}
		key.bits = uint64(valueNativeID(value))
		return key, true
	case TableKind:
		shapeID, ok := b.internConstantTableShape(value.tableRef())
		if !ok {
			return constantPoolKey{}, false
		}
		key.bits = uint64(shapeID)
		return key, true
	default:
		return constantPoolKey{}, false
	}
}

func (b *bytecodeBuilder) internConstantString(text string, candidate *stringBox) constantStringIntern {
	if intern, ok := b.constantStrings[text]; ok {
		return intern
	}
	if b.constantStrings == nil {
		b.constantStrings = make(map[string]constantStringIntern)
	}
	if candidate == nil {
		candidate = newStringBox(text)
	}
	intern := constantStringIntern{id: uint32(len(b.constantStrings) + 1), box: candidate}
	b.constantStrings[text] = intern
	return intern
}

func (b *bytecodeBuilder) internConstantTableShape(table *Table) (uint32, bool) {
	shape, ok := constantTableShapeKey(table)
	if !ok {
		return 0, false
	}
	if id, ok := b.constantShapes[shape]; ok {
		return id, true
	}
	if b.constantShapes == nil {
		b.constantShapes = make(map[string]uint32)
	}
	b.nextConstantShape++
	b.constantShapes[shape] = b.nextConstantShape
	return b.nextConstantShape, true
}

func constantTableShapeKey(table *Table) (string, bool) {
	if table == nil || len(table.array) != 0 || table.metatable != nil || table.iteration != nil || table.cold != nil {
		return "", false
	}
	var shape strings.Builder
	shape.WriteString(strconv.Itoa(cap(table.array)))
	shape.WriteByte(':')
	for _, field := range table.stringFields {
		if field.key == "" || !field.value.IsNil() {
			return "", false
		}
		shape.WriteString(strconv.Itoa(len(field.key)))
		shape.WriteByte(':')
		shape.WriteString(field.key)
	}
	return shape.String(), true
}

func (b *bytecodeBuilder) addPrototype(proto *Proto) int {
	index := len(b.prototypes)
	b.prototypes = append(b.prototypes, proto)
	return index
}

func (b *bytecodeBuilder) emit(ins instruction) int {
	return b.emitWithSource(ins, b.source)
}

func (b *bytecodeBuilder) emitWithSource(ins instruction, source sourceRange) int {
	if b == nil || b.conversionErr != nil {
		return -1
	}
	ir, err := lowerInstructionToBytecodeIRChecked(ins, source)
	if err != nil {
		b.recordConversionError(fmt.Errorf("lower %s: %w", opcodeName(ins.op), err))
		return -1
	}
	index := len(b.ir)
	b.ir = append(b.ir, ir)
	return index
}

func (b *bytecodeBuilder) emitLoadConst(target int, value Value) int {
	constant := b.addConstant(value)
	return b.emit(instruction{op: opLoadConst, a: target, b: constant})
}

func (b *bytecodeBuilder) emitJump() int {
	return b.emit(instruction{op: opJump})
}

func (b *bytecodeBuilder) emitJumpIfFalse(condition int) int {
	return b.emit(instruction{op: opJumpIfFalse, a: condition})
}

func (b *bytecodeBuilder) patchJump(at int, target int) {
	if b == nil || b.conversionErr != nil {
		return
	}
	if at < 0 || at >= len(b.ir) {
		b.recordConversionError(fmt.Errorf("patch jump at %d: instruction index out of range", at))
		return
	}
	if target < 0 {
		b.recordConversionError(fmt.Errorf("patch jump at %d: negative target %d", at, target))
		return
	}
	meta, ok := opcodeMetadata(b.ir[at].opcodeValue())
	if !ok || meta.jumpTarget == opcodeJumpTargetNone {
		b.recordConversionError(fmt.Errorf("patch jump at %d: opcode has no jump target", at))
		return
	}
	slot := bytecodeIROperandSlotB
	if meta.jumpTarget == opcodeJumpTargetD {
		slot = bytecodeIROperandSlotD
	}
	if err := b.ir[at].setOperandValueChecked(slot, target); err != nil {
		b.recordConversionError(fmt.Errorf("patch jump at %d: %w", at, err))
	}
}

func (b *bytecodeBuilder) patchJumpD(at int, target int) {
	if b == nil || b.conversionErr != nil {
		return
	}
	if at < 0 || at >= len(b.ir) {
		b.recordConversionError(fmt.Errorf("patch jump D at %d: instruction index out of range", at))
		return
	}
	if target < 0 {
		b.recordConversionError(fmt.Errorf("patch jump D at %d: negative target %d", at, target))
		return
	}
	meta, ok := opcodeMetadata(b.ir[at].opcodeValue())
	if !ok || meta.jumpTarget != opcodeJumpTargetD {
		b.recordConversionError(fmt.Errorf("patch jump D at %d: opcode has no D jump target", at))
		return
	}
	if err := b.ir[at].setOperandValueChecked(bytecodeIROperandSlotD, target); err != nil {
		b.recordConversionError(fmt.Errorf("patch jump D at %d: %w", at, err))
	}
}

func (b *bytecodeBuilder) pc() int {
	return len(b.ir)
}

func (b *bytecodeBuilder) withSourceRange(source sourceRange, emit func() error) error {
	previous := b.source
	b.source = source
	defer func() {
		b.source = previous
	}()
	return emit()
}

func (b *bytecodeBuilder) assembledCode() []instruction {
	if b == nil || b.conversionErr != nil {
		return nil
	}
	return assembleBytecodeIR(b.ir)
}

func (b *bytecodeBuilder) optimize(options optimizationOptions) {
	if b == nil || b.conversionErr != nil {
		return
	}
	b.ir = optimizeBytecodeIRWithFacts(b.ir, bytecodeIROptimizationFacts{
		constants:         b.constants,
		capturedRegisters: bytecodeBuilderCapturedRegisters(b.prototypes),
		constantPool:      b,
	}, options)
}

func bytecodeBuilderCapturedRegisters(prototypes []*Proto) []bool {
	var captured []bool
	for _, proto := range prototypes {
		if proto == nil {
			continue
		}
		for _, desc := range proto.upvalues {
			if !desc.local || desc.copy || desc.index < 0 {
				continue
			}
			for len(captured) <= desc.index {
				captured = append(captured, false)
			}
			captured[desc.index] = true
		}
	}
	return captured
}

func (b *bytecodeBuilder) proto(upvalues []upvalueDesc, registers int, params int, variadic bool) *Proto {
	if b == nil || b.conversionErr != nil {
		return nil
	}
	code := b.assembledCode()
	proto := newProtoWithDescriptors(b.constants, code, b.prototypes, upvalues, registers, params, variadic)
	proto.lines = bytecodeIRLines(b.sourceText, b.ir)
	_ = finalizeProtoExecutionArtifact(proto, code)
	return proto
}

func (b *bytecodeBuilder) finalizeProto(upvalues []upvalueDesc, registers int, params int, variadic bool) (*Proto, error) {
	if b == nil {
		return nil, fmt.Errorf("nil bytecode builder")
	}
	if b.conversionErr != nil {
		return nil, b.conversionErr
	}
	proto := b.proto(upvalues, registers, params, variadic)
	if proto == nil {
		return nil, b.conversionErr
	}
	if proto.verifyErr != nil {
		return nil, fmt.Errorf("invalid finalized prototype: %w", proto.verifyErr)
	}
	return proto, nil
}

func lowerInstructionToBytecodeIR(ins instruction, source sourceRange) bytecodeIRInstruction {
	return mustLowerInstructionToBytecodeIR(ins, source)
}

func mustLowerInstructionToBytecodeIR(ins instruction, source sourceRange) bytecodeIRInstruction {
	ir, err := lowerInstructionToBytecodeIRValues(ins, source)
	if err != nil {
		panic(err)
	}
	return ir
}

func lowerInstructionToBytecodeIRChecked(ins instruction, source sourceRange) (bytecodeIRInstruction, error) {
	if _, ok := opcodeMetadata(ins.op); !ok {
		return bytecodeIRInstruction{}, fmt.Errorf("invalid opcode %d", ins.op)
	}
	return lowerInstructionToBytecodeIRValues(ins, source)
}

func lowerInstructionToBytecodeIRValues(ins instruction, source sourceRange) (bytecodeIRInstruction, error) {
	a, err := int32Checked(ins.a, "A")
	if err != nil {
		return bytecodeIRInstruction{}, err
	}
	b, err := int32Checked(ins.b, "B")
	if err != nil {
		return bytecodeIRInstruction{}, err
	}
	c, err := int32Checked(ins.c, "C")
	if err != nil {
		return bytecodeIRInstruction{}, err
	}
	d, err := int32Checked(ins.d, "D")
	if err != nil {
		return bytecodeIRInstruction{}, err
	}
	start, err := sourceOffsetChecked(source.start, "start")
	if err != nil {
		return bytecodeIRInstruction{}, err
	}
	end, err := sourceOffsetChecked(source.end, "end")
	if err != nil {
		return bytecodeIRInstruction{}, err
	}
	return bytecodeIRInstruction{a: a, b: b, c: c, d: d, sourceStart: start, sourceEnd: end, op: ins.op}, nil
}

func int32Checked(value int, slot string) (int32, error) {
	if int64(value) < int64(-1<<31) || int64(value) > int64(1<<31-1) {
		return 0, fmt.Errorf("IR operand %s=%d overflows int32", slot, value)
	}
	return int32(value), nil
}

func sourceOffsetChecked(value int, label string) (uint32, error) {
	if value < 0 || uint64(value) > uint64(1<<32-1) {
		return 0, fmt.Errorf("IR source %s=%d outside uint32", label, value)
	}
	return uint32(value), nil
}

func lowerInstructionsToBytecodeIR(code []instruction) []bytecodeIRInstruction {
	ir := make([]bytecodeIRInstruction, len(code))
	for i, ins := range code {
		ir[i] = lowerInstructionToBytecodeIR(ins, sourceRange{})
	}
	return ir
}

type assembledBytecodeIR struct {
	code     []instruction
	oldToNew []int
	sources  []sourceRange
	lines    []int
}

func assembleFunctionBytecode(lines sourceLineMap, ir []bytecodeIRInstruction) assembledBytecodeIR {
	assembled := assembleBytecodeIRResult(ir)
	assembled.lines = sourceRangesLines(lines, assembled.sources)
	return assembled
}

func assembleBytecodeIR(ir []bytecodeIRInstruction) []instruction {
	return assembleBytecodeIRResult(ir).code
}

func assembleBytecodeIRResult(ir []bytecodeIRInstruction) assembledBytecodeIR {
	if len(ir) == 0 {
		return assembledBytecodeIR{}
	}
	drop := bytecodeIRJumpToNextInstructions(ir)
	oldToNew := make([]int, len(ir)+1)
	kept := 0
	for pc := range ir {
		oldToNew[pc] = kept
		if !drop[pc] {
			kept++
		}
	}
	oldToNew[len(ir)] = kept

	assembled := assembledBytecodeIR{
		code:     make([]instruction, 0, kept),
		oldToNew: oldToNew,
		sources:  make([]sourceRange, 0, kept),
	}
	for pc, ins := range ir {
		if drop[pc] {
			continue
		}
		ins = remapAssembledBytecodeIRJumpTargets(ins, oldToNew)
		assembled.code = append(assembled.code, assembleBytecodeIRInstruction(ins))
		assembled.sources = append(assembled.sources, ins.sourceSpan())
	}
	return assembled
}

func assembleBytecodeIRInstruction(ins bytecodeIRInstruction) instruction {
	return instruction{
		op: ins.op,
		a:  int(ins.a),
		b:  int(ins.b),
		c:  int(ins.c),
		d:  int(ins.d),
	}
}

func assembleBytecodeIRInstructionChecked(ins bytecodeIRInstruction) (instruction, error) {
	if _, ok := opcodeMetadata(ins.op); !ok {
		return instruction{}, fmt.Errorf("invalid IR opcode %d", ins.op)
	}
	if _, err := ins.sourceSpanChecked(); err != nil {
		return instruction{}, err
	}
	return assembleBytecodeIRInstruction(ins), nil
}

func bytecodeIRJumpToNextInstructions(ir []bytecodeIRInstruction) []bool {
	drop := make([]bool, len(ir))
	for pc, ins := range ir {
		if ins.op != opJump {
			continue
		}
		if ins.operandKind(bytecodeIROperandSlotB) == bytecodeOperandJumpTarget && ins.operandValue(bytecodeIROperandSlotB) == pc+1 {
			drop[pc] = true
		}
	}
	return drop
}

func remapAssembledBytecodeIRJumpTargets(ins bytecodeIRInstruction, oldToNew []int) bytecodeIRInstruction {
	remap := func(slot bytecodeIROperandSlot) {
		value := ins.operandValue(slot)
		if ins.operandKind(slot) != bytecodeOperandJumpTarget || value < 0 || value >= len(oldToNew) {
			return
		}
		ins.setOperandValue(slot, oldToNew[value])
	}
	remap(bytecodeIROperandSlotB)
	remap(bytecodeIROperandSlotD)
	return ins
}

func disassembleBytecodeIR(constants []Value, ir []bytecodeIRInstruction) []string {
	return disassembleInstructions(&Proto{constants: constants}, assembleBytecodeIR(ir))
}

func disassembleBytecodeIRWithSource(constants []Value, ir []bytecodeIRInstruction) []string {
	assembled := assembleBytecodeIRResult(ir)
	lines := disassembleInstructions(&Proto{constants: constants}, assembled.code)
	for i := range lines {
		source := assembled.sources[i]
		lines[i] = fmt.Sprintf("%04d [%d,%d) %s", i, source.start, source.end, lines[i][5:])
	}
	return lines
}

func bytecodeIRLines(source string, ir []bytecodeIRInstruction) []int {
	return assembleFunctionBytecode(newSourceLineMap(source), ir).lines
}

type sourceLineMap struct {
	sourceLen      int
	newlineOffsets []int
}

func newSourceLineMap(source string) sourceLineMap {
	lines := sourceLineMap{
		sourceLen:      len(source),
		newlineOffsets: make([]int, 0, strings.Count(source, "\n")),
	}
	for offset := 0; offset < len(source); offset++ {
		if source[offset] == '\n' {
			lines.newlineOffsets = append(lines.newlineOffsets, offset)
		}
	}
	return lines
}

func (lines sourceLineMap) line(span sourceRange) int {
	if span.end <= span.start || span.start < 0 || span.start >= lines.sourceLen {
		return -1
	}
	return sort.Search(len(lines.newlineOffsets), func(index int) bool {
		return lines.newlineOffsets[index] >= span.start
	}) + 1
}

func sourceRangesLines(lineMap sourceLineMap, sources []sourceRange) []int {
	if lineMap.sourceLen == 0 || len(sources) == 0 {
		return nil
	}
	lines := make([]int, len(sources))
	hasLine := false
	for i, sourceRange := range sources {
		line := lineMap.line(sourceRange)
		lines[i] = line
		if line > 0 {
			hasLine = true
		}
	}
	if !hasLine {
		return nil
	}
	return lines
}

func sourceRangeLine(source string, span sourceRange) int {
	return newSourceLineMap(source).line(span)
}

func bytecodeIRBlockOrder(ir []bytecodeIRInstruction) []bytecodeIRBlock {
	if len(ir) == 0 {
		return nil
	}

	leaders := make([]bool, len(ir))
	leaders[0] = true
	for pc, ins := range ir {
		if bytecodeIRInstructionTransfersControl(ins) && pc+1 < len(ir) {
			leaders[pc+1] = true
		}
		if target, ok := bytecodeIRJumpTarget(ins); ok && target >= 0 && target < len(ir) {
			leaders[target] = true
		}
	}

	blocks := make([]bytecodeIRBlock, 0, len(ir))
	for start := 0; start < len(ir); {
		end := start + 1
		for end < len(ir) && !leaders[end] {
			end++
		}
		blocks = append(blocks, bytecodeIRBlock{id: len(blocks), start: start, end: end})
		start = end
	}
	return blocks
}

func bytecodeIRInstructionTransfersControl(ins bytecodeIRInstruction) bool {
	return opcodeTransfersControl(ins.op)
}

func bytecodeIRJumpTarget(ins bytecodeIRInstruction) (int, bool) {
	switch opcodeJumpTarget(ins.op) {
	case opcodeJumpTargetB:
		return ins.operandValue(bytecodeIROperandSlotB), ins.operandKind(bytecodeIROperandSlotB) == bytecodeOperandJumpTarget
	case opcodeJumpTargetD:
		return ins.operandValue(bytecodeIROperandSlotD), ins.operandKind(bytecodeIROperandSlotD) == bytecodeOperandJumpTarget
	default:
		return 0, false
	}
}

func bytecodeIRLiveness(ir []bytecodeIRInstruction) []bytecodeIRLivenessBlock {
	blocks := bytecodeIRBlockOrder(ir)
	successors := bytecodeIRBlockSuccessors(ir, blocks)
	return bytecodeIRLivenessForGraph(ir, blocks, successors)
}

func bytecodeIRLivenessForGraph(ir []bytecodeIRInstruction, blocks []bytecodeIRBlock, successors [][]int) []bytecodeIRLivenessBlock {
	liveness := make([]bytecodeIRLivenessBlock, len(blocks))
	for i, block := range blocks {
		use, def := bytecodeIRBlockUseDef(ir, block)
		liveness[i] = bytecodeIRLivenessBlock{
			block:   block,
			use:     use,
			def:     def,
			liveIn:  registerSet{},
			liveOut: registerSet{},
		}
	}

	var out registerSet
	var in registerSet
	var outWithoutDefs registerSet
	changed := true
	for changed {
		changed = false
		for i := len(liveness) - 1; i >= 0; i-- {
			out.clear()
			for _, successor := range successors[i] {
				out.addAll(liveness[successor].liveIn)
			}

			in.assign(liveness[i].use)
			outWithoutDefs.assign(out)
			outWithoutDefs.removeAll(liveness[i].def)
			in.addAll(outWithoutDefs)

			if !liveness[i].liveOut.equal(out) || !liveness[i].liveIn.equal(in) {
				liveness[i].liveOut.assign(out)
				liveness[i].liveIn.assign(in)
				changed = true
			}
		}
	}
	return liveness
}

func bytecodeIRBlockUseDef(ir []bytecodeIRInstruction, block bytecodeIRBlock) (registerSet, registerSet) {
	use := registerSet{}
	def := registerSet{}
	for pc := block.start; pc < block.end; pc++ {
		raw := assembleBytecodeIRInstruction(ir[pc])
		reads := instructionRegisters(raw, instructionRegisterRead)
		for register, ok := reads.next(); ok; register, ok = reads.next() {
			if !def.contains(register) {
				use.add(register)
			}
		}
		writes := instructionRegisters(raw, instructionRegisterWrite)
		for register, ok := writes.next(); ok; register, ok = writes.next() {
			def.add(register)
		}
	}
	return use, def
}

func bytecodeIRBlockSuccessors(ir []bytecodeIRInstruction, blocks []bytecodeIRBlock) [][]int {
	successors := make([][]int, len(blocks))
	blockByStart := make(map[int]int, len(blocks))
	for _, block := range blocks {
		blockByStart[block.start] = block.id
	}
	for _, block := range blocks {
		if block.end == 0 {
			continue
		}
		last := ir[block.end-1]
		switch opcodeControlFlow(last.op) {
		case opcodeControlJump:
			if target, ok := bytecodeIRJumpTarget(last); ok {
				if successor, ok := blockByStart[target]; ok {
					successors[block.id] = append(successors[block.id], successor)
				}
			}
		case opcodeControlBranch:
			if target, ok := bytecodeIRJumpTarget(last); ok {
				if successor, ok := blockByStart[target]; ok {
					successors[block.id] = append(successors[block.id], successor)
				}
			}
			if successor, ok := blockByStart[block.end]; ok {
				successors[block.id] = append(successors[block.id], successor)
			}
		case opcodeControlReturn:
		default:
			if successor, ok := blockByStart[block.end]; ok {
				successors[block.id] = append(successors[block.id], successor)
			}
		}
	}
	return successors
}

// Proto is an executable Ember function prototype.
type Proto struct {
	constants        []Value
	constantKeys     []tableKey
	constantKeyOK    []bool
	constantNumbers  []float64
	constantNumberOK []bool
	globalNames      []string
	// words is the immutable executable stream. Compiler instructions remain
	// transient in the IR/assembly pipeline and are never retained here.
	words                   []wordcodeWord
	wordLines               []int
	lines                   []int
	prototypes              []*Proto
	upvalues                []upvalueDesc
	registers               int
	params                  int
	variadic                bool
	capturedLocals          []bool
	cacheSiteCount          int
	cacheIndex              *wordcodeCacheIndex
	entryNilRegisters       []int
	reuseZeroCaptureClosure bool
	verifyErr               error
	debugInfo               *protoDebugInfo
	codeImageOnce           sync.Once
	codeImage               *codeImage
	codeImageErr            error
}

// protoDebugInfo is immutable compiler metadata used by runtime diagnostics.
// Keeping it behind a pointer avoids widening prototypes that do not carry
// compiler metadata, such as hand-built test fixtures.
type protoDebugInfo struct {
	sourceName   string
	functionName string
}

func (proto *Proto) globalSlot(slot int, name string) int {
	if proto == nil || slot < 0 || slot >= len(proto.globalNames) || proto.globalNames[slot] != name {
		return -1
	}
	return slot
}

type numericForLoopDesc struct {
	checkPC     int
	loop        int
	limit       int
	step        int
	exitPC      int
	incrementPC int
}

type intrinsicOpDesc struct {
	pc         int
	op         opcode
	base       int
	args       int
	results    int
	globalName string
	field      string
	nativeID   nativeFuncID
}

type constantKindFactDesc struct {
	constant int
	kind     ValueKind
}

type registerKindFactDesc struct {
	pc       int
	register int
	kind     ValueKind
	source   string
	guarded  bool
}

type numericOperandFactDesc struct {
	pc            int
	op            opcode
	left          int
	right         int
	rightConstant bool
}

type slotKindFactDesc struct {
	pc      int
	table   int
	field   int
	slot    int
	kind    ValueKind
	source  string
	guarded bool
}

type directFrameRejection struct {
	pc     int
	op     opcode
	reason string
}

type executionArtifact struct {
	constantKeys      []tableKey
	constantKeyOK     []bool
	constantNumbers   []float64
	constantNumberOK  []bool
	capturedLocals    []bool
	entryNilRegisters []int
}

func newProto(constants []Value, code []instruction, prototypes []*Proto, upvalues []upvalueDesc, registers int, params int, variadic bool) *Proto {
	proto := newProtoWithDescriptors(constants, code, prototypes, upvalues, registers, params, variadic)
	_ = finalizeProtoExecutionArtifact(proto, code)
	return proto
}

func newProtoWithDescriptors(constants []Value, code []instruction, prototypes []*Proto, upvalues []upvalueDesc, registers int, params int, variadic bool) *Proto {
	return &Proto{
		constants:  constants,
		prototypes: prototypes,
		upvalues:   upvalues,
		registers:  registers,
		params:     params,
		variadic:   variadic,
	}
}

func finalizeProtoExecutionArtifact(proto *Proto, sourceCode ...[]instruction) error {
	if proto == nil {
		return nil
	}
	var code []instruction
	if len(sourceCode) != 0 {
		code = sourceCode[0]
	} else if len(proto.words) != 0 {
		decoded, err := decodeWordcode(proto.words, proto.cacheIndex)
		if err != nil {
			proto.verifyErr = err
			return err
		}
		code = decoded
	}
	code = normalizeOpenResultCallMarkers(code)
	code = normalizeOpenArgumentCallMarkers(code)
	code = normalizeFixedMultiResultCounts(code, proto.registers)
	assignProtoGlobalSlots(proto, code)
	artifact := buildExecutionArtifact(proto, code)
	artifact.apply(proto)
	// The analysis marker is intentionally encoded in the existing fixed-call
	// count operand. Runtime dispatch treats it as a hint and retains the
	// copied path whenever its dynamic guards fail.
	code = markBorrowableFixedCallWindows(code, proto.registers, artifact.capturedLocals)
	markReusableZeroCaptureClosures(proto, code)
	// Preserve the established semantic verifier errors before applying the
	// narrower physical-word limits. This keeps invalid source diagnostics
	// stable while still rejecting malformed wordcode for valid prototypes.
	proto.verifyErr = verifyProtoWithCode(proto, code)
	if proto.verifyErr != nil {
		return proto.verifyErr
	}
	if err := encodeProtoWords(proto, code); err != nil {
		proto.verifyErr = err
		return proto.verifyErr
	}
	proto.codeImageOnce = sync.Once{}
	proto.codeImage = nil
	proto.codeImageErr = nil
	return proto.verifyErr
}

func assignProtoGlobalSlots(proto *Proto, code []instruction) {
	if proto == nil {
		return
	}
	slots := make(map[string]int)
	names := make([]string, 0)
	slotFor := func(name string) int {
		if slot, ok := slots[name]; ok {
			return slot
		}
		slot := len(names)
		slots[name] = slot
		names = append(names, name)
		return slot
	}
	for pc, ins := range code {
		var constant int
		switch ins.op {
		case opLoadGlobal:
			constant = ins.b
		case opSetGlobal:
			constant = ins.a
		default:
			continue
		}
		if constant < 0 || constant >= len(proto.constants) {
			code[pc].c = -1
			continue
		}
		name, ok := proto.constants[constant].String()
		if !ok {
			code[pc].c = -1
			continue
		}
		code[pc].c = slotFor(name)
	}
	proto.globalNames = names
}

// encodeProtoWords publishes the immutable verifier-first wordcode
// representation. The canonical instruction slice is compiler-transient;
// wordcode uses physical word PCs and AUX words receive no source line.
func encodeProtoWords(proto *Proto, code []instruction) error {
	if proto == nil {
		return nil
	}
	words, err := encodeWordcode(code, proto.registers, len(proto.constants))
	if err != nil {
		return err
	}
	cacheSiteCount := wordcodeCacheSiteCount(code)
	var boundaries []int
	if cacheSiteCount != 0 {
		var boundaryErr error
		boundaries, boundaryErr = wordcodeBoundaries(code)
		if boundaryErr != nil {
			return boundaryErr
		}
	}
	var cacheIndex *wordcodeCacheIndex
	if cacheSiteCount != 0 {
		cacheIndex, err = buildWordcodeCacheIndex(code, boundaries, len(words))
		if err != nil {
			return err
		}
	}
	proto.words = words
	proto.cacheSiteCount = cacheSiteCount
	proto.cacheIndex = cacheIndex
	if len(proto.lines) == 0 {
		proto.wordLines = nil
	} else {
		proto.wordLines = wordcodeLinesFromWords(proto.lines, words)
	}
	return nil
}

// wordcodeLinesFromWords maps logical source lines onto physical words without
// rebuilding the boundary slice that encodeWordcode already computed. AUX
// words remain line-less implementation details.
func wordcodeLinesFromWords(logicalLines []int, words []wordcodeWord) []int {
	if len(logicalLines) == 0 || len(words) == 0 {
		return nil
	}
	lines := make([]int, len(words))
	logical := 0
	for wordPC := 0; wordPC < len(words) && logical < len(logicalLines); wordPC++ {
		lines[wordPC] = logicalLines[logical]
		logical++
		if words[wordPC]&wordcodeAuxBit != 0 {
			wordPC++
		}
	}
	return lines
}

func buildExecutionArtifact(proto *Proto, code []instruction) executionArtifact {
	constantKeys, constantKeyOK := protoConstantTableKeys(proto.constants)
	constantNumbers, constantNumberOK := protoConstantNumbers(proto.constants)
	capturedLocals := capturedLocalRegisters(proto)
	return executionArtifact{
		constantKeys:      constantKeys,
		constantKeyOK:     constantKeyOK,
		constantNumbers:   constantNumbers,
		constantNumberOK:  constantNumberOK,
		capturedLocals:    capturedLocals,
		entryNilRegisters: protoEntryNilRegisters(code, proto.params, proto.registers),
	}
}

func (artifact executionArtifact) apply(proto *Proto) {
	proto.constantKeys = artifact.constantKeys
	proto.constantKeyOK = artifact.constantKeyOK
	proto.constantNumbers = artifact.constantNumbers
	proto.constantNumberOK = artifact.constantNumberOK
	proto.capturedLocals = artifact.capturedLocals
	proto.entryNilRegisters = artifact.entryNilRegisters
}

func codeSupportsDirectFrame(code []instruction) bool {
	for _, ins := range code {
		if !directFrameOpcodeSupported(ins.op) {
			return false
		}
	}
	return true
}

func markReusableZeroCaptureClosures(proto *Proto, code []instruction) {
	if proto == nil {
		return
	}
	for _, child := range proto.prototypes {
		child.reuseZeroCaptureClosure = false
	}
	for pc, ins := range code {
		if ins.op != opClosure || ins.b < 0 || ins.b >= len(proto.prototypes) {
			continue
		}
		child := proto.prototypes[ins.b]
		if child == nil || len(child.upvalues) != 0 {
			continue
		}
		if closureValueImmediatelyCalled(code, pc, ins.a) {
			child.reuseZeroCaptureClosure = true
		}
	}
}

func closureValueImmediatelyCalled(code []instruction, pc int, register int) bool {
	if pc+1 >= len(code) {
		return false
	}
	next := code[pc+1]
	switch next.op {
	case opCall, opCallOne, opCallLocalOne:
		return next.b == register
	default:
		return false
	}
}

func protoEntryNilRegisters(code []instruction, params int, registers int) []int {
	if len(code) == 0 || registers == 0 {
		return nil
	}
	if registers > 64 {
		return protoEntryNilRegistersFromLiveness(code, params)
	}

	start := uint64(0)
	for register := 0; register < params && register < registers; register++ {
		start |= uint64(1) << register
	}

	for {
		missing := protoEntryMissingRegisterMask(code, registers, start)
		next := start | missing
		if next == start {
			break
		}
		start = next
	}

	nilMask := start
	for register := 0; register < params && register < registers; register++ {
		nilMask &^= uint64(1) << register
	}
	return registerMaskValues(nilMask, registers)
}

func detectNumericForLoops(code []instruction) []numericForLoopDesc {
	var loops []numericForLoopDesc
	for pc, ins := range code {
		if ins.op != opNumericForCheck {
			continue
		}
		loops = append(loops, numericForLoopDesc{
			checkPC:     pc,
			loop:        ins.a,
			limit:       ins.b,
			step:        ins.c,
			exitPC:      ins.d,
			incrementPC: numericForIncrementPC(code, pc, ins),
		})
	}
	return loops
}

func numericForIncrementPC(code []instruction, checkPC int, check instruction) int {
	for pc, ins := range code {
		if ins.op == opNumericForLoop &&
			ins.a == check.a &&
			ins.b == check.c &&
			ins.d == checkPC {
			return pc
		}
		if pc == 0 || ins.op != opJump || ins.b != checkPC {
			continue
		}
		increment := code[pc-1]
		if increment.op == opAdd &&
			increment.a == check.a &&
			increment.b == check.a &&
			increment.c == check.c {
			return pc - 1
		}
	}
	return -1
}

func detectIntrinsicOps(code []instruction) []intrinsicOpDesc {
	var ops []intrinsicOpDesc
	for pc, ins := range code {
		switch ins.op {
		case opFastCall:
			nativeID := nativeFuncID(ins.b)
			globalName, field := fastCallIntrinsicNames(nativeID)
			ops = append(ops, intrinsicOpDesc{
				pc:         pc,
				op:         ins.op,
				base:       ins.a,
				args:       ins.c,
				results:    ins.d,
				globalName: globalName,
				field:      field,
				nativeID:   nativeID,
			})
		}
	}
	return ops
}

func fastCallIntrinsicNames(nativeID nativeFuncID) (string, string) {
	for _, intrinsic := range baseFieldIntrinsics() {
		if intrinsic.nativeID == nativeID {
			return intrinsic.globalName, intrinsic.field
		}
	}
	switch nativeID {
	case nativeFuncRawLen:
		return "rawlen", ""
	case nativeFuncSelect:
		return "select", ""
	default:
		return "", ""
	}
}

func detectConstantKindFacts(constants []Value) []constantKindFactDesc {
	var facts []constantKindFactDesc
	for index, constant := range constants {
		kind := valueKind(constant)
		if !kindFactSupportedKind(kind) {
			continue
		}
		facts = append(facts, constantKindFactDesc{constant: index, kind: kind})
	}
	return facts
}

type registerKindState struct {
	kind    ValueKind
	ok      bool
	guarded bool
}

func detectRegisterKindFacts(proto *Proto) []registerKindFactDesc {
	code, err := protoDecodedInstructions(proto)
	if err != nil || len(code) == 0 || proto == nil || proto.registers <= 0 {
		return nil
	}
	blockStarts := registerKindBlockStarts(code)
	state := make([]registerKindState, proto.registers)
	var facts []registerKindFactDesc
	for pc, ins := range code {
		if pc > 0 && blockStarts[pc] {
			clearRegisterKindState(state)
		}
		fact, ok := registerKindFactForInstruction(proto, state, pc, ins)
		clearInstructionRegisterKinds(state, ins)
		if ok && fact.register >= 0 && fact.register < len(state) {
			state[fact.register] = registerKindState{
				kind:    fact.kind,
				ok:      true,
				guarded: fact.guarded,
			}
			facts = append(facts, fact)
		}
		if opcodeControlFlow(ins.op) != opcodeControlNone {
			clearRegisterKindState(state)
		}
	}
	return facts
}

func detectNumericOperandFacts(proto *Proto) []numericOperandFactDesc {
	var facts []numericOperandFactDesc
	detectNumericOperandFactsInto(proto, &facts, nil)
	return facts
}

func detectNumericOperandFactPCs(proto *Proto) []bool {
	code, err := protoDecodedInstructions(proto)
	if err != nil || len(code) == 0 || proto == nil {
		return nil
	}
	pcs := make([]bool, len(code))
	detectNumericOperandFactsInto(proto, nil, pcs)
	return pcs
}

func detectNumericOperandFactsInto(proto *Proto, facts *[]numericOperandFactDesc, pcs []bool) {
	code, err := protoDecodedInstructions(proto)
	if err != nil || len(code) == 0 || proto == nil || proto.registers <= 0 {
		return
	}
	blockStarts := registerKindBlockStarts(code)
	state := make([]registerKindState, proto.registers)
	for pc, ins := range code {
		if pc > 0 && blockStarts[pc] {
			clearRegisterKindState(state)
		}
		if fact, ok := numericOperandFactForInstruction(proto, state, pc, ins); ok {
			if facts != nil {
				*facts = append(*facts, fact)
			}
			if pc < len(pcs) {
				pcs[pc] = true
			}
		}
		fact, ok := registerKindFactForInstruction(proto, state, pc, ins)
		clearInstructionRegisterKinds(state, ins)
		if ok && fact.register >= 0 && fact.register < len(state) {
			state[fact.register] = registerKindState{
				kind:    fact.kind,
				ok:      true,
				guarded: fact.guarded,
			}
		}
		if opcodeControlFlow(ins.op) != opcodeControlNone {
			clearRegisterKindState(state)
		}
	}
}

func numericOperandFactForInstruction(proto *Proto, state []registerKindState, pc int, ins instruction) (numericOperandFactDesc, bool) {
	switch ins.op {
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv:
		if registerHasUnguardedKind(state, ins.b, NumberKind) && registerHasUnguardedKind(state, ins.c, NumberKind) {
			return numericOperandFactDesc{pc: pc, op: ins.op, left: ins.b, right: ins.c}, true
		}
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		if registerHasUnguardedKind(state, ins.b, NumberKind) && constantHasKind(proto, ins.c, NumberKind) {
			return numericOperandFactDesc{pc: pc, op: ins.op, left: ins.b, right: ins.c, rightConstant: true}, true
		}
	case opNeg:
		if registerHasUnguardedKind(state, ins.b, NumberKind) {
			return numericOperandFactDesc{pc: pc, op: ins.op, left: ins.b, right: -1}, true
		}
	case opLess, opLessEqual, opGreater, opGreaterEqual:
		if registerHasUnguardedKind(state, ins.b, NumberKind) && registerHasUnguardedKind(state, ins.c, NumberKind) {
			return numericOperandFactDesc{pc: pc, op: ins.op, left: ins.b, right: ins.c}, true
		}
	}
	return numericOperandFactDesc{}, false
}

func registerKindFactForInstruction(proto *Proto, state []registerKindState, pc int, ins instruction) (registerKindFactDesc, bool) {
	switch ins.op {
	case opLoadConst:
		if ins.b < 0 || ins.b >= len(proto.constants) {
			return registerKindFactDesc{}, false
		}
		kind := valueKind(proto.constants[ins.b])
		if !kindFactSupportedKind(kind) {
			return registerKindFactDesc{}, false
		}
		return registerKindFactDesc{pc: pc, register: ins.a, kind: kind, source: "constant"}, true
	case opMove:
		if source, ok := registerKindAt(state, ins.b); ok {
			return registerKindFactDesc{pc: pc, register: ins.a, kind: source.kind, source: "move", guarded: source.guarded}, true
		}
	case opNewTable:
		return registerKindFactDesc{pc: pc, register: ins.a, kind: TableKind, source: "table_literal"}, true
	case opNeg:
		if registerHasUnguardedKind(state, ins.b, NumberKind) {
			return registerKindFactDesc{pc: pc, register: ins.a, kind: NumberKind, source: "numeric"}, true
		}
	case opLen:
		return registerKindFactDesc{pc: pc, register: ins.a, kind: NumberKind, source: "length"}, true
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
		if registerHasUnguardedKind(state, ins.b, NumberKind) && registerHasUnguardedKind(state, ins.c, NumberKind) {
			return registerKindFactDesc{pc: pc, register: ins.a, kind: NumberKind, source: "numeric"}, true
		}
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		if registerHasUnguardedKind(state, ins.b, NumberKind) && constantHasKind(proto, ins.c, NumberKind) {
			return registerKindFactDesc{pc: pc, register: ins.a, kind: NumberKind, source: "numeric"}, true
		}
	case opEqual, opNotEqual:
		if equalityOperandsHaveSimpleKinds(state, ins.b, ins.c) {
			return registerKindFactDesc{pc: pc, register: ins.a, kind: BoolKind, source: "comparison"}, true
		}
	case opLess, opLessEqual, opGreater, opGreaterEqual:
		if orderedComparisonOperandsHaveSimpleKinds(state, ins.b, ins.c) {
			return registerKindFactDesc{pc: pc, register: ins.a, kind: BoolKind, source: "comparison"}, true
		}
	case opFastCall:
		if ins.d == 1 && (nativeFuncID(ins.b) == nativeFuncMathMin || nativeFuncID(ins.b) == nativeFuncSelect || nativeFuncID(ins.b) == nativeFuncRawLen) {
			return registerKindFactDesc{pc: pc, register: ins.a, kind: NumberKind, source: "guarded_intrinsic", guarded: true}, true
		}
	}
	return registerKindFactDesc{}, false
}

func registerKindBlockStarts(code []instruction) []bool {
	if len(code) == 0 {
		return nil
	}
	starts := make([]bool, len(code))
	starts[0] = true
	for pc, ins := range code {
		switch opcodeControlFlow(ins.op) {
		case opcodeControlJump, opcodeControlBranch:
			if target, ok := instructionJumpTarget(ins); ok && target >= 0 && target < len(code) {
				starts[target] = true
			}
			if pc+1 < len(code) {
				starts[pc+1] = true
			}
		case opcodeControlReturn:
			if pc+1 < len(code) {
				starts[pc+1] = true
			}
		}
	}
	return starts
}

func clearInstructionRegisterKinds(state []registerKindState, ins instruction) {
	writes := instructionRegistersBounded(ins, instructionRegisterWrite, len(state))
	for register, ok := writes.next(); ok; register, ok = writes.next() {
		state[register] = registerKindState{}
	}
}

func clearRegisterKindState(state []registerKindState) {
	for register := range state {
		state[register] = registerKindState{}
	}
}

func registerKindAt(state []registerKindState, register int) (registerKindState, bool) {
	if register < 0 || register >= len(state) || !state[register].ok {
		return registerKindState{}, false
	}
	return state[register], true
}

func registerHasUnguardedKind(state []registerKindState, register int, kind ValueKind) bool {
	fact, ok := registerKindAt(state, register)
	return ok && !fact.guarded && fact.kind == kind
}

func constantHasKind(proto *Proto, constant int, kind ValueKind) bool {
	return proto != nil &&
		constant >= 0 &&
		constant < len(proto.constants) &&
		valueKind(proto.constants[constant]) == kind
}

func equalityOperandsHaveSimpleKinds(state []registerKindState, left int, right int) bool {
	leftKind, ok := registerKindAt(state, left)
	if !ok || leftKind.guarded || !kindFactSimpleEqualityKind(leftKind.kind) {
		return false
	}
	rightKind, ok := registerKindAt(state, right)
	return ok && !rightKind.guarded && kindFactSimpleEqualityKind(rightKind.kind)
}

func orderedComparisonOperandsHaveSimpleKinds(state []registerKindState, left int, right int) bool {
	leftKind, ok := registerKindAt(state, left)
	if !ok || leftKind.guarded {
		return false
	}
	rightKind, ok := registerKindAt(state, right)
	if !ok || rightKind.guarded {
		return false
	}
	return (leftKind.kind == NumberKind && rightKind.kind == NumberKind) ||
		(leftKind.kind == StringKind && rightKind.kind == StringKind)
}

func kindFactSimpleEqualityKind(kind ValueKind) bool {
	switch kind {
	case NilKind, BoolKind, NumberKind, StringKind:
		return true
	default:
		return false
	}
}

func kindFactSupportedKind(kind ValueKind) bool {
	switch kind {
	case NilKind, BoolKind, NumberKind, StringKind, TableKind:
		return true
	default:
		return false
	}
}

type slotKindLiteralState struct {
	nextSlot int
	fields   map[string]int
	ok       bool
}

func detectSlotKindFacts(proto *Proto) []slotKindFactDesc {
	code, err := protoDecodedInstructions(proto)
	if err != nil || len(code) == 0 || proto == nil || proto.registers <= 0 {
		return nil
	}
	blockStarts := registerKindBlockStarts(code)
	registerKinds := make([]registerKindState, proto.registers)
	literalSlots := make([]slotKindLiteralState, proto.registers)
	var facts []slotKindFactDesc
	for pc, ins := range code {
		if pc > 0 && blockStarts[pc] {
			clearRegisterKindState(registerKinds)
			clearSlotKindLiteralState(literalSlots)
		}
		if fact, ok := registerKindFactForInstruction(proto, registerKinds, pc, ins); ok && fact.register >= 0 && fact.register < len(registerKinds) {
			clearInstructionRegisterKinds(registerKinds, ins)
			clearInstructionSlotKindLiterals(literalSlots, ins)
			registerKinds[fact.register] = registerKindState{
				kind:    fact.kind,
				ok:      true,
				guarded: fact.guarded,
			}
			if ins.op == opNewTable {
				literalSlots[ins.a] = slotKindLiteralState{
					fields: make(map[string]int),
					ok:     true,
				}
			}
		} else {
			clearInstructionRegisterKinds(registerKinds, ins)
			clearInstructionSlotKindLiterals(literalSlots, ins)
		}
		if fact, ok := slotKindFactForInstruction(proto, registerKinds, literalSlots, pc, ins); ok {
			facts = append(facts, fact)
		}
		if opcodeControlFlow(ins.op) != opcodeControlNone {
			clearRegisterKindState(registerKinds)
			clearSlotKindLiteralState(literalSlots)
		}
	}
	return facts
}

func slotKindFactForInstruction(proto *Proto, registerKinds []registerKindState, literalSlots []slotKindLiteralState, pc int, ins instruction) (slotKindFactDesc, bool) {
	switch ins.op {
	case opSetStringField:
		field, ok := stringConstantText(proto, ins.b)
		if !ok || ins.a < 0 || ins.a >= len(literalSlots) {
			return slotKindFactDesc{}, false
		}
		value, ok := registerKindAt(registerKinds, ins.c)
		if !ok || !kindFactSupportedKind(value.kind) {
			return slotKindFactDesc{}, false
		}
		slot, ok := literalSlotForField(literalSlots, ins.a, field)
		if !ok {
			return slotKindFactDesc{}, false
		}
		return slotKindFactDesc{
			pc:      pc,
			table:   ins.a,
			field:   ins.b,
			slot:    slot,
			kind:    value.kind,
			source:  "table_literal",
			guarded: true,
		}, true
	default:
		return slotKindFactDesc{}, false
	}
}

func literalSlotForField(slots []slotKindLiteralState, register int, field string) (int, bool) {
	if register < 0 || register >= len(slots) || !slots[register].ok {
		return -1, false
	}
	state := &slots[register]
	if slot, ok := state.fields[field]; ok {
		return slot, true
	}
	slot := state.nextSlot
	state.fields[field] = slot
	state.nextSlot++
	return slot, true
}

func clearInstructionSlotKindLiterals(slots []slotKindLiteralState, ins instruction) {
	writes := instructionRegistersBounded(ins, instructionRegisterWrite, len(slots))
	for register, ok := writes.next(); ok; register, ok = writes.next() {
		slots[register] = slotKindLiteralState{}
	}
}

func clearSlotKindLiteralState(slots []slotKindLiteralState) {
	for register := range slots {
		slots[register] = slotKindLiteralState{}
	}
}

func stringConstantText(proto *Proto, constant int) (string, bool) {
	if proto == nil || constant < 0 || constant >= len(proto.constants) {
		return "", false
	}
	value := proto.constants[constant]
	if valueKind(value) != StringKind {
		return "", false
	}
	return value.stringText(), true
}

func protoEntryMissingRegisterMask(code []instruction, registers int, start uint64) uint64 {
	states := make([]uint64, len(code))
	seen := make([]bool, len(code))
	work := []int{0}
	states[0] = start
	seen[0] = true
	missing := uint64(0)

	for len(work) > 0 {
		pc := work[len(work)-1]
		work = work[:len(work)-1]
		state := states[pc]
		ins := code[pc]
		read := instructionReadMask(ins, registers)
		missingRead := read &^ state
		missing |= missingRead
		state |= missingRead
		state |= instructionWriteMask(ins, registers)

		for _, successor := range instructionSuccessors(code, pc) {
			if successor < 0 || successor >= len(code) {
				continue
			}
			if !seen[successor] {
				seen[successor] = true
				states[successor] = state
				work = append(work, successor)
				continue
			}
			merged := states[successor] & state
			if merged != states[successor] {
				states[successor] = merged
				work = append(work, successor)
			}
		}
	}
	return missing
}

func instructionReadMask(ins instruction, registers int) uint64 {
	mask := uint64(0)
	if registers > 64 {
		registers = 64
	}
	reads := instructionRegistersBounded(ins, instructionRegisterRead, registers)
	for register, ok := reads.next(); ok; register, ok = reads.next() {
		mask |= uint64(1) << register
	}
	return mask
}

func instructionWriteMask(ins instruction, registers int) uint64 {
	mask := uint64(0)
	if registers > 64 {
		registers = 64
	}
	writes := instructionRegistersBounded(ins, instructionRegisterWrite, registers)
	for register, ok := writes.next(); ok; register, ok = writes.next() {
		mask |= uint64(1) << register
	}
	return mask
}

func instructionSuccessors(code []instruction, pc int) []int {
	ins := code[pc]
	switch opcodeControlFlow(ins.op) {
	case opcodeControlJump:
		target, _ := instructionJumpTarget(ins)
		return []int{target}
	case opcodeControlBranch:
		target, _ := instructionJumpTarget(ins)
		return []int{pc + 1, target}
	case opcodeControlReturn:
		return nil
	default:
		return []int{pc + 1}
	}
}

func registerMaskValues(mask uint64, registers int) []int {
	if mask == 0 {
		return nil
	}
	values := make([]int, 0)
	for register := 0; register < registers; register++ {
		if mask&(uint64(1)<<register) != 0 {
			values = append(values, register)
		}
	}
	return values
}

func protoEntryNilRegistersFromLiveness(code []instruction, params int) []int {
	ir := make([]bytecodeIRInstruction, len(code))
	for i, ins := range code {
		ir[i] = lowerInstructionToBytecodeIR(ins, sourceRange{})
	}
	liveness := bytecodeIRLiveness(ir)
	if len(liveness) == 0 {
		return nil
	}
	liveIn := liveness[0].liveIn.values()
	nilRegisters := liveIn[:0]
	for _, register := range liveIn {
		if register >= params {
			nilRegisters = append(nilRegisters, register)
		}
	}
	if len(nilRegisters) == 0 {
		return nil
	}
	return append([]int(nil), nilRegisters...)
}

func protoConstantTableKeys(constants []Value) ([]tableKey, []bool) {
	keys := make([]tableKey, len(constants))
	ok := make([]bool, len(constants))
	for i, constant := range constants {
		if box := constant.stringBox(); box != nil {
			keys[i] = tableKey{kind: StringKind, str: box.text, strBox: box, strHash: box.hash}
			ok[i] = true
		}
	}
	return keys, ok
}

func protoConstantNumbers(constants []Value) ([]float64, []bool) {
	numbers := make([]float64, len(constants))
	ok := make([]bool, len(constants))
	for i, constant := range constants {
		if number, numberOK := constant.Number(); numberOK {
			numbers[i] = number
			ok[i] = true
		}
	}
	return numbers, ok
}

func verifyProto(proto *Proto) error {
	code, err := protoDecodedInstructions(proto)
	if err != nil {
		return err
	}
	return verifyProtoSeenWithCode(proto, make(map[*Proto]bool), code)
}

func verifyProtoSeen(proto *Proto, seen map[*Proto]bool) error {
	code, err := protoDecodedInstructions(proto)
	if err != nil {
		return err
	}
	return verifyProtoSeenWithCode(proto, seen, code)
}

func verifyProtoWithCode(proto *Proto, code []instruction) error {
	return verifyProtoSeenWithCode(proto, make(map[*Proto]bool), code)
}

func protoDecodedInstructions(proto *Proto) ([]instruction, error) {
	if proto == nil || len(proto.words) == 0 {
		return nil, nil
	}
	return decodeWordcode(proto.words, proto.cacheIndex)
}

func verifyProtoSeenWithCode(proto *Proto, seen map[*Proto]bool, code []instruction) error {
	if proto == nil {
		return fmt.Errorf("nil prototype")
	}
	if seen[proto] {
		return nil
	}
	seen[proto] = true
	if proto.registers < 0 {
		return fmt.Errorf("negative register count %d", proto.registers)
	}
	if proto.params < 0 {
		return fmt.Errorf("negative parameter count %d", proto.params)
	}
	if proto.params > proto.registers {
		return fmt.Errorf("parameter count %d exceeds register count %d", proto.params, proto.registers)
	}
	if proto.cacheSiteCount < 0 {
		return fmt.Errorf("negative cache site count %d", proto.cacheSiteCount)
	}
	if err := proto.cacheIndex.validateWords(proto.words, proto.cacheSiteCount, len(proto.constants)); err != nil {
		return err
	}
	if want := protoEntryNilRegisters(code, proto.params, proto.registers); !equalIntSlices(proto.entryNilRegisters, want) {
		return fmt.Errorf("entry nil registers %v do not match finalized plan %v", proto.entryNilRegisters, want)
	}
	for index, upvalue := range proto.upvalues {
		if upvalue.index < 0 {
			return fmt.Errorf("upvalue %d has negative index %d", index, upvalue.index)
		}
	}
	for pc, ins := range code {
		if err := verifyInstruction(proto, pc, ins, len(code)); err != nil {
			return fmt.Errorf("instruction %d %s(%d,%d,%d,%d): %w", pc, opcodeName(ins.op), ins.a, ins.b, ins.c, ins.d, err)
		}
		if ins.op == opCall && decodeOpenResultCallMarker(ins.d) && !openResultCallHasImmediateConsumer(code, pc, ins.a) {
			return fmt.Errorf("instruction %d CALL open-result marker is not consumed by a matching open RETURN", pc)
		}
		if ins.op == opCall {
			if prefixCount, marked := decodeOpenArgumentCallMarker(ins.c); marked {
				openArgStart := ins.b + 1 + prefixCount
				if !openArgumentCallHasProducer(code, pc, openArgStart, proto.registers) {
					return fmt.Errorf("instruction %d CALL open-argument marker has no matching open producer", pc)
				}
			}
		}
	}
	if proto.lines != nil && len(proto.lines) != len(code) {
		return fmt.Errorf("line table length %d does not match code length %d", len(proto.lines), len(code))
	}
	if proto.wordLines != nil && len(proto.wordLines) != len(proto.words) {
		return fmt.Errorf("word line table length %d does not match wordcode length %d", len(proto.wordLines), len(proto.words))
	}
	for index, child := range proto.prototypes {
		if err := verifyChildUpvalues(proto, child); err != nil {
			return fmt.Errorf("prototype %d: %w", index, err)
		}
		childCode, err := protoDecodedInstructions(child)
		if err != nil {
			return fmt.Errorf("prototype %d: %w", index, err)
		}
		if err := verifyProtoSeenWithCode(child, seen, childCode); err != nil {
			return fmt.Errorf("prototype %d: %w", index, err)
		}
	}
	return nil
}

func protoSupportsDirectFrame(proto *Proto) bool {
	if proto == nil {
		return false
	}
	code, err := protoDecodedInstructions(proto)
	return err == nil && codeSupportsDirectFrame(code)
}

func protoDirectFrameRejection(proto *Proto) (directFrameRejection, bool) {
	if proto == nil {
		return directFrameRejection{pc: -1, reason: "nil prototype"}, true
	}
	code, err := protoDecodedInstructions(proto)
	if err != nil {
		return directFrameRejection{pc: -1, reason: err.Error()}, true
	}
	for pc, ins := range code {
		if !directFrameOpcodeSupported(ins.op) {
			return directFrameRejection{
				pc:     pc,
				op:     ins.op,
				reason: directFrameOpcodeUnsupportedReason(ins.op),
			}, true
		}
	}
	return directFrameRejection{}, false
}

func directFrameOpcodeSupported(op opcode) bool {
	meta, ok := opcodeMetadata(op)
	return ok && meta.directFrame
}

func directFrameOpcodeUnsupportedReason(op opcode) string {
	meta, ok := opcodeMetadata(op)
	if !ok {
		return "unknown opcode"
	}
	return meta.directFrameUnsupportedReason
}

func equalIntSlices(left []int, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalNumericForLoopDescs(left []numericForLoopDesc, right []numericForLoopDesc) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalIntrinsicOpDescs(left []intrinsicOpDesc, right []intrinsicOpDesc) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalConstantKindFactDescs(left []constantKindFactDesc, right []constantKindFactDesc) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalRegisterKindFactDescs(left []registerKindFactDesc, right []registerKindFactDesc) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalNumericOperandFactDescs(left []numericOperandFactDesc, right []numericOperandFactDesc) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalBoolSlices(left []bool, right []bool) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalSlotKindFactDescs(left []slotKindFactDesc, right []slotKindFactDesc) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func verifyChildUpvalues(parent *Proto, child *Proto) error {
	if child == nil {
		return fmt.Errorf("nil prototype")
	}
	for index, desc := range child.upvalues {
		if desc.local {
			if desc.index < 0 || desc.index >= parent.registers {
				return fmt.Errorf("upvalue %d local register index %d out of range", index, desc.index)
			}
			continue
		}
		if desc.index < 0 || desc.index >= len(parent.upvalues) {
			return fmt.Errorf("upvalue %d parent upvalue index %d out of range", index, desc.index)
		}
	}
	return nil
}

func verifyInstruction(proto *Proto, pc int, ins instruction, codeLen ...int) error {
	switch ins.op {
	case opLoadConst:
		return verifyRegisterAndConstant(proto, ins.a, ins.b)
	case opLoadGlobal:
		if err := verifyRegisterAndConstant(proto, ins.a, ins.b); err != nil {
			return err
		}
		return verifyStringConstant(proto, ins.b)
	case opSetGlobal:
		if err := verifyConstant(proto, ins.a); err != nil {
			return err
		}
		if err := verifyStringConstant(proto, ins.a); err != nil {
			return err
		}
		return verifyRegister(proto, ins.b)
	case opMove:
		return verifyRegisters(proto, ins.a, ins.b)
	case opNewTable:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if ins.b < 0 {
			return fmt.Errorf("negative table array capacity %d", ins.b)
		}
		if ins.c < 0 {
			return fmt.Errorf("negative table field capacity %d", ins.c)
		}
		return nil
	case opSetField:
		if err := verifyRegisters(proto, ins.a, ins.c); err != nil {
			return err
		}
		return verifyConstant(proto, ins.b)
	case opSetStringField:
		if err := verifyRegisters(proto, ins.a, ins.c); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.b); err != nil {
			return err
		}
		return verifyStringConstant(proto, ins.b)
	case opSetStringFieldIndex:
		if err := verifyRegisters(proto, ins.a, ins.c, ins.d); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.b); err != nil {
			return err
		}
		return verifyStringConstant(proto, ins.b)
	case opGetStringField:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.c); err != nil {
			return err
		}
		return verifyStringConstant(proto, ins.c)
	case opGetStringFieldIndex:
		if err := verifyRegisters(proto, ins.a, ins.b, ins.d); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.c); err != nil {
			return err
		}
		return verifyStringConstant(proto, ins.c)
	case opSetIndex, opGetIndex, opPrepareIter:
		return verifyRegisters(proto, ins.a, ins.b, ins.c)
	case opArrayNext:
		if err := verifyRegisters(proto, ins.a, ins.b, ins.c); err != nil {
			return err
		}
		if ins.d <= 0 {
			return fmt.Errorf("array next result count %d is not positive", ins.d)
		}
		if ins.a+ins.d > proto.registers {
			return fmt.Errorf("array next result range r%d..r%d out of range", ins.a, ins.a+ins.d-1)
		}
		return nil
	case opArrayNextJump2:
		if err := verifyRegisters(proto, ins.a, ins.b, ins.c); err != nil {
			return err
		}
		if ins.a+1 >= proto.registers {
			return fmt.Errorf("array next jump2 result range r%d..r%d out of range", ins.a, ins.a+1)
		}
		return verifyJumpTarget(proto, ins.d, codeLen...)
	case opClosure:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if ins.b < 0 || ins.b >= len(proto.prototypes) {
			return fmt.Errorf("prototype index %d out of range", ins.b)
		}
		return nil
	case opGetUpvalue:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		return verifyUpvalue(proto, ins.b)
	case opSetUpvalue:
		if err := verifyUpvalue(proto, ins.a); err != nil {
			return err
		}
		return verifyRegister(proto, ins.b)
	case opVararg:
		if !proto.variadic {
			return fmt.Errorf("vararg in non-variadic prototype")
		}
		if ins.b > 0 {
			return verifyRegisterSpan(proto, ins.a, ins.b)
		}
		return verifyRegister(proto, ins.a)
	case opConcatChain:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if ins.c <= 0 {
			return fmt.Errorf("concat chain operand count %d must be positive", ins.c)
		}
		return verifyRegisterSpan(proto, ins.b, ins.c)
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opConcat,
		opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual:
		return verifyRegisters(proto, ins.a, ins.b, ins.c)
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		return verifyConstant(proto, ins.c)
	case opNumericForCheck:
		if err := verifyRegisters(proto, ins.a, ins.b, ins.c); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d, codeLen...)
	case opNumericForLoop:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d, codeLen...)
	case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.b); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d, codeLen...)
	case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d, codeLen...)
	case opJumpIfTableHasMetatable:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.d, codeLen...)
	case opFastCall:
		nativeID := nativeFuncID(ins.b)
		if _, ok := nativeFuncByID(nativeID); !ok {
			return fmt.Errorf("unknown fast call native id %d", ins.b)
		}
		if nativeID == nativeFuncSelect && !proto.variadic {
			return fmt.Errorf("select fast call in non-variadic prototype")
		}
		if ins.c < 0 {
			return fmt.Errorf("negative fast call argument count %d", ins.c)
		}
		if ins.c > 0 {
			if err := verifyRegisterSpan(proto, ins.a, ins.c); err != nil {
				return err
			}
		} else if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if ins.d > 0 {
			return verifyRegisterSpan(proto, ins.a, ins.d)
		}
		return verifyRegister(proto, ins.a)
	case opNeg, opLen:
		return verifyRegisters(proto, ins.a, ins.b)
	case opCall:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		if decodeOpenResultCallMarker(ins.d) {
			prefixCount, openArguments := decodeOpenArgumentCallMarker(ins.c)
			if ins.c < 0 && !openArguments {
				return fmt.Errorf("open-result call marker requires fixed arguments")
			}
			if openArguments {
				if ins.b+1+prefixCount >= proto.registers {
					return fmt.Errorf("call argument register range out of range")
				}
			} else if ins.b+ins.c >= proto.registers {
				return fmt.Errorf("call argument register range out of range")
			}
			return verifyRegister(proto, ins.a)
		}
		if prefixCount, marked := decodeOpenArgumentCallMarker(ins.c); marked {
			openArgStart := ins.b + 1 + prefixCount
			if openArgStart < 0 || openArgStart >= proto.registers {
				return fmt.Errorf("open call argument register %d out of range", openArgStart)
			}
		} else if ins.c < 0 {
			prefixCount := -ins.c - 1
			openArgStart := ins.b + 1 + prefixCount
			if openArgStart < 0 || openArgStart >= proto.registers {
				return fmt.Errorf("open call argument register %d out of range", openArgStart)
			}
		} else if ins.b+ins.c >= proto.registers {
			return fmt.Errorf("call argument register range out of range")
		}
		resultCount := ins.d
		if decoded, marked := decodeFixedMultiResultCount(ins.d, proto.registers); marked {
			resultCount = decoded
		}
		if resultCount > 0 {
			return verifyRegisterSpan(proto, ins.a, resultCount)
		}
		return verifyRegister(proto, ins.a)
	case opCallOne:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		count, _, err := verifyFixedCallCount(ins.c, "fixed one-result call")
		if err != nil {
			return err
		}
		if count > 0 && ins.b+count >= proto.registers {
			return fmt.Errorf("call argument register range out of range")
		}
		return verifyRegister(proto, ins.a)
	case opCallLocalOne:
		if err := verifyRegisters(proto, ins.a, ins.b, ins.c); err != nil {
			return err
		}
		count, _, err := verifyFixedCallCount(ins.d, "local one-result call")
		if err != nil {
			return err
		}
		if count > 0 && ins.c+count > proto.registers {
			return fmt.Errorf("local call argument register range out of range")
		}
		return nil
	case opCallUpvalueOne:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if err := verifyUpvalue(proto, ins.b); err != nil {
			return err
		}
		count, _, err := verifyFixedCallCount(ins.d, "upvalue one-result call")
		if err != nil {
			return err
		}
		if count > 0 {
			return verifyRegisterSpan(proto, ins.c, count)
		}
		return verifyRegister(proto, ins.c)
	case opCallMethodOne:
		if err := verifyRegisters(proto, ins.a, ins.b); err != nil {
			return err
		}
		if err := verifyConstant(proto, ins.c); err != nil {
			return err
		}
		if err := verifyStringConstant(proto, ins.c); err != nil {
			return err
		}
		count, _, err := verifyFixedCallCount(ins.d, "method one-result call")
		if err != nil {
			return err
		}
		return verifyRegisterSpan(proto, ins.a+1, count+1)
	case opJumpIfFalse:
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		return verifyJumpTarget(proto, ins.b, codeLen...)
	case opJump:
		return verifyJumpTarget(proto, ins.b, codeLen...)
	case opReturnOne:
		return verifyRegister(proto, ins.a)
	case opReturn:
		if ins.b == 0 {
			return nil
		}
		if err := verifyRegister(proto, ins.a); err != nil {
			return err
		}
		if ins.b < 0 {
			prefixCount := -ins.b - 1
			if ins.a+prefixCount >= proto.registers {
				return fmt.Errorf("open return register range out of range")
			}
			return nil
		}
		if ins.b > 0 {
			return verifyRegisterSpan(proto, ins.a, ins.b)
		}
		return nil
	default:
		return fmt.Errorf("unknown opcode %d", ins.op)
	}
}

func verifyFixedCallCount(raw int, label string) (int, bool, error) {
	if raw < -32768 || raw > 32767 {
		return 0, false, fmt.Errorf("%s argument count %d out of int16 range", label, raw)
	}
	count, borrowHint := decodeFixedCallCount(raw)
	return count, borrowHint, nil
}

func verifyRegisterAndConstant(proto *Proto, register int, constant int) error {
	if err := verifyRegister(proto, register); err != nil {
		return err
	}
	if err := verifyConstant(proto, constant); err != nil {
		return err
	}
	return nil
}

func verifyRegisters(proto *Proto, registers ...int) error {
	for _, register := range registers {
		if err := verifyRegister(proto, register); err != nil {
			return err
		}
	}
	return nil
}

func verifyRegister(proto *Proto, register int) error {
	if register < 0 || register >= proto.registers {
		return fmt.Errorf("register index %d out of range", register)
	}
	return nil
}

func verifyRegisterSpan(proto *Proto, start int, count int) error {
	if count < 0 {
		return fmt.Errorf("negative register span %d", count)
	}
	if count == 0 {
		return verifyRegister(proto, start)
	}
	if start < 0 || start+count > proto.registers {
		return fmt.Errorf("register range %d..%d out of range", start, start+count-1)
	}
	return nil
}

func verifyConstant(proto *Proto, constant int) error {
	if constant < 0 || constant >= len(proto.constants) {
		return fmt.Errorf("constant index %d out of range", constant)
	}
	return nil
}

func verifyStringConstant(proto *Proto, constant int) error {
	value := proto.constants[constant]
	if _, ok := value.String(); !ok {
		return fmt.Errorf("constant index %d is %s, want string", constant, value.Kind())
	}
	return nil
}

func verifyNumberConstant(proto *Proto, constant int) error {
	value := proto.constants[constant]
	if _, ok := value.Number(); !ok {
		return fmt.Errorf("constant index %d is %s, want number", constant, value.Kind())
	}
	return nil
}

func verifyUpvalue(proto *Proto, upvalue int) error {
	if upvalue < 0 || upvalue >= len(proto.upvalues) {
		return fmt.Errorf("upvalue index %d out of range", upvalue)
	}
	return nil
}

func verifyJumpTarget(proto *Proto, target int, codeLen ...int) error {
	length := -1
	if len(codeLen) != 0 {
		length = codeLen[0]
	} else {
		code, err := protoDecodedInstructions(proto)
		if err != nil {
			return err
		}
		length = len(code)
	}
	if target < 0 || target > length {
		return fmt.Errorf("jump target %d out of range", target)
	}
	return nil
}

func disassembleProto(proto *Proto) []string {
	if proto == nil {
		return nil
	}
	code, err := protoDecodedInstructions(proto)
	if err != nil {
		return []string{fmt.Sprintf("<invalid wordcode: %v>", err)}
	}

	return disassembleInstructions(proto, code)
}

func disassembleInstructions(proto *Proto, code []instruction) []string {
	lines := make([]string, len(code))
	for pc, ins := range code {
		lines[pc] = fmt.Sprintf("%04d %s", pc, disassembleInstruction(proto, ins))
	}
	return lines
}

func disassembleProtoFacts(proto *Proto) []string {
	if proto == nil {
		return nil
	}
	code, decodeErr := protoDecodedInstructions(proto)

	facts := deriveProtoDiagnosticFacts(proto)
	lines := []string{
		fmt.Sprintf("direct_frame_dispatch %t", protoSupportsDirectFrame(proto)),
		disassembleCapturedLocals(proto.capturedLocals),
		disassembleEntryNilRegisters(proto.entryNilRegisters),
	}
	if rejection, ok := protoDirectFrameRejection(proto); ok {
		if decodeErr == nil && rejection.pc >= 0 && rejection.pc < len(code) {
			lines = append(lines, fmt.Sprintf(
				"direct_frame_rejection pc%d %s: %s",
				rejection.pc,
				disassembleInstruction(proto, code[rejection.pc]),
				rejection.reason,
			))
		} else {
			lines = append(lines, fmt.Sprintf("direct_frame_rejection %s", rejection.reason))
		}
	}
	for index, ok := range proto.constantKeyOK {
		if ok {
			lines = append(lines, fmt.Sprintf("constant_key k%d %s", index, disassembleTableKey(proto.constantKeys[index])))
		}
	}
	for index, ok := range proto.constantNumberOK {
		if ok {
			lines = append(lines, fmt.Sprintf("constant_number k%d %g", index, proto.constantNumbers[index]))
		}
	}
	for _, loop := range facts.numericForLoops {
		lines = append(lines, fmt.Sprintf(
			"numeric_for pc%d r%d limit r%d step r%d exit %d increment %d",
			loop.checkPC,
			loop.loop,
			loop.limit,
			loop.step,
			loop.exitPC,
			loop.incrementPC,
		))
	}
	for _, intrinsic := range facts.intrinsicOps {
		line := fmt.Sprintf(
			"intrinsic pc%d %s r%d args %d results %d",
			intrinsic.pc,
			opcodeName(intrinsic.op),
			intrinsic.base,
			intrinsic.args,
			intrinsic.results,
		)
		if intrinsic.globalName != "" {
			line += " global " + intrinsic.globalName
		}
		if intrinsic.field != "" {
			line += " field " + intrinsic.field
		}
		if nativeName, ok := baseNativeFuncName(intrinsic.nativeID); ok {
			line += " native " + nativeName
		}
		lines = append(lines, line)
	}
	for _, fact := range facts.constantKindFacts {
		lines = append(lines, fmt.Sprintf(
			"constant_kind k%d %s",
			fact.constant,
			fact.kind.String(),
		))
	}
	for _, fact := range facts.registerKindFacts {
		line := fmt.Sprintf(
			"register_kind pc%d r%d %s source %s",
			fact.pc,
			fact.register,
			fact.kind.String(),
			fact.source,
		)
		if fact.guarded {
			line += " guarded"
		}
		lines = append(lines, line)
	}
	for _, fact := range facts.numericOperandFacts {
		right := fmt.Sprintf("right r%d", fact.right)
		if fact.rightConstant {
			right = fmt.Sprintf("right k%d", fact.right)
		}
		lines = append(lines, fmt.Sprintf(
			"numeric_operand pc%d %s left r%d %s",
			fact.pc,
			opcodeName(fact.op),
			fact.left,
			right,
		))
	}
	for _, fact := range facts.slotKindFacts {
		field := fmt.Sprintf("k%d", fact.field)
		if text, ok := stringConstantText(proto, fact.field); ok {
			field = text
		}
		line := fmt.Sprintf(
			"slot_kind pc%d r%d field %s slot %d %s source %s",
			fact.pc,
			fact.table,
			field,
			fact.slot,
			fact.kind.String(),
			fact.source,
		)
		if fact.guarded {
			line += " guarded"
		}
		lines = append(lines, line)
	}
	return lines
}

func disassembleRefinementDetail(proto *Proto, base int, field int, second int, value int, other int, slot int) string {
	detail := ""
	if base >= 0 {
		detail += fmt.Sprintf(" base r%d", base)
	}
	if field >= 0 {
		detail += " field " + disassemblePredicateBranchField(proto, field, second)
	}
	if value >= 0 {
		detail += " value " + disassembleConstant(proto, value)
	}
	if other >= 0 {
		detail += fmt.Sprintf(" other r%d", other)
	}
	if slot >= 0 {
		detail += fmt.Sprintf(" slot %d", slot)
	}
	return detail
}

func disassemblePredicateBranchField(proto *Proto, field int, second int) string {
	text := fmt.Sprintf("k%d", field)
	if value, ok := stringConstantText(proto, field); ok {
		text = value
	}
	if second >= 0 {
		if value, ok := stringConstantText(proto, second); ok {
			text += "." + value
		} else {
			text += fmt.Sprintf(".k%d", second)
		}
	}
	return text
}

func nativeFuncName(nativeID nativeFuncID) string {
	if name, ok := baseNativeFuncName(nativeID); ok {
		return name
	}
	return "UNKNOWN"
}

func opcodeName(op opcode) string {
	switch op {
	case opLoadConst:
		return "LOAD_CONST"
	case opLoadGlobal:
		return "LOAD_GLOBAL"
	case opSetGlobal:
		return "SET_GLOBAL"
	case opMove:
		return "MOVE"
	case opNewTable:
		return "NEW_TABLE"
	case opSetField:
		return "SET_FIELD"
	case opSetStringField:
		return "SET_STRING_FIELD"
	case opSetStringFieldIndex:
		return "SET_STRING_FIELD_INDEX"
	case opGetStringField:
		return "GET_STRING_FIELD"
	case opGetStringFieldIndex:
		return "GET_STRING_FIELD_INDEX"
	case opSetIndex:
		return "SET_INDEX"
	case opGetIndex:
		return "GET_INDEX"
	case opClosure:
		return "CLOSURE"
	case opGetUpvalue:
		return "GET_UPVALUE"
	case opSetUpvalue:
		return "SET_UPVALUE"
	case opVararg:
		return "VARARG"
	case opPrepareIter:
		return "PREPARE_ITER"
	case opArrayNext:
		return "ARRAY_NEXT"
	case opArrayNextJump2:
		return "ARRAY_NEXT_JUMP2"
	case opAdd:
		return "ADD"
	case opSub:
		return "SUB"
	case opMul:
		return "MUL"
	case opDiv:
		return "DIV"
	case opMod:
		return "MOD"
	case opIDiv:
		return "IDIV"
	case opPow:
		return "POW"
	case opNeg:
		return "NEG"
	case opLen:
		return "LEN"
	case opConcat:
		return "CONCAT"
	case opConcatChain:
		return "CONCAT_CHAIN"
	case opAddK:
		return "ADD_K"
	case opSubK:
		return "SUB_K"
	case opMulK:
		return "MUL_K"
	case opDivK:
		return "DIV_K"
	case opModK:
		return "MOD_K"
	case opIDivK:
		return "IDIV_K"
	case opEqual:
		return "EQUAL"
	case opNotEqual:
		return "NOT_EQUAL"
	case opLess:
		return "LESS"
	case opLessEqual:
		return "LESS_EQUAL"
	case opGreater:
		return "GREATER"
	case opGreaterEqual:
		return "GREATER_EQUAL"
	case opNumericForCheck:
		return "NUMERIC_FOR_CHECK"
	case opNumericForLoop:
		return "NUMERIC_FOR_LOOP"
	case opJumpIfNotEqualK:
		return "JUMP_IF_NOT_EQUAL_K"
	case opJumpIfNotLessK:
		return "JUMP_IF_NOT_LESS_K"
	case opJumpIfNotGreaterK:
		return "JUMP_IF_NOT_GREATER_K"
	case opJumpIfLessK:
		return "JUMP_IF_LESS_K"
	case opJumpIfGreaterK:
		return "JUMP_IF_GREATER_K"
	case opJumpIfNotLess:
		return "JUMP_IF_NOT_LESS"
	case opJumpIfNotGreater:
		return "JUMP_IF_NOT_GREATER"
	case opJumpIfLess:
		return "JUMP_IF_LESS"
	case opJumpIfGreater:
		return "JUMP_IF_GREATER"
	case opJumpIfTableHasMetatable:
		return "JUMP_IF_TABLE_HAS_METATABLE"
	case opFastCall:
		return "FAST_CALL"
	case opCall:
		return "CALL"
	case opCallOne:
		return "CALL_ONE"
	case opCallLocalOne:
		return "CALL_LOCAL_ONE"
	case opCallUpvalueOne:
		return "CALL_UPVALUE_ONE"
	case opCallMethodOne:
		return "CALL_METHOD_ONE"
	case opJumpIfFalse:
		return "JUMP_IF_FALSE"
	case opJump:
		return "JUMP"
	case opReturnOne:
		return "RETURN_ONE"
	case opReturn:
		return "RETURN"
	default:
		return fmt.Sprintf("UNKNOWN_%d", op)
	}
}

func disassembleEntryNilRegisters(entryNilRegisters []int) string {
	line := "entry_nil"
	if len(entryNilRegisters) == 0 {
		return line + " none"
	}
	for _, register := range entryNilRegisters {
		line += fmt.Sprintf(" r%d", register)
	}
	return line
}

func disassembleCapturedLocals(capturedLocals []bool) string {
	line := "captured_locals"
	hasCaptured := false
	for index, captured := range capturedLocals {
		if captured {
			line += fmt.Sprintf(" r%d", index)
			hasCaptured = true
		}
	}
	if !hasCaptured {
		return line + " none"
	}
	return line
}

func disassembleTableKey(key tableKey) string {
	switch key.kind {
	case BoolKind:
		return fmt.Sprintf("boolean %t", key.bool)
	case NumberKind:
		return fmt.Sprintf("number %g", key.number)
	case StringKind:
		return fmt.Sprintf("string %q", key.str)
	case TableKind:
		return "table"
	case UserDataKind:
		return "userdata"
	default:
		return key.kind.String()
	}
}

func disassembleFixedCallCount(raw int) string {
	count, borrowHint := decodeFixedCallCount(raw)
	if borrowHint {
		return fmt.Sprintf("%d borrow", count)
	}
	return strconv.Itoa(count)
}

func disassembleInstruction(proto *Proto, ins instruction) string {
	switch ins.op {
	case opLoadConst:
		return fmt.Sprintf("LOAD_CONST r%d %s", ins.a, disassembleConstant(proto, ins.b))
	case opLoadGlobal:
		return fmt.Sprintf("LOAD_GLOBAL r%d %s", ins.a, disassembleConstant(proto, ins.b))
	case opSetGlobal:
		return fmt.Sprintf("SET_GLOBAL %s r%d", disassembleConstant(proto, ins.a), ins.b)
	case opMove:
		return fmt.Sprintf("MOVE r%d r%d", ins.a, ins.b)
	case opNewTable:
		return fmt.Sprintf("NEW_TABLE r%d %d %d", ins.a, ins.b, ins.c)
	case opSetField:
		return fmt.Sprintf("SET_FIELD r%d %s r%d", ins.a, disassembleConstant(proto, ins.b), ins.c)
	case opSetStringField:
		return fmt.Sprintf("SET_STRING_FIELD r%d %s r%d", ins.a, disassembleConstant(proto, ins.b), ins.c)
	case opSetStringFieldIndex:
		return fmt.Sprintf("SET_STRING_FIELD_INDEX r%d %s r%d r%d", ins.a, disassembleConstant(proto, ins.b), ins.c, ins.d)
	case opGetStringField:
		return fmt.Sprintf("GET_STRING_FIELD r%d r%d %s", ins.a, ins.b, disassembleConstant(proto, ins.c))
	case opGetStringFieldIndex:
		return fmt.Sprintf("GET_STRING_FIELD_INDEX r%d r%d %s r%d", ins.a, ins.b, disassembleConstant(proto, ins.c), ins.d)
	case opSetIndex:
		return fmt.Sprintf("SET_INDEX r%d r%d r%d", ins.a, ins.b, ins.c)
	case opGetIndex:
		return fmt.Sprintf("GET_INDEX r%d r%d r%d", ins.a, ins.b, ins.c)
	case opClosure:
		return fmt.Sprintf("CLOSURE r%d p%d", ins.a, ins.b)
	case opGetUpvalue:
		return fmt.Sprintf("GET_UPVALUE r%d u%d", ins.a, ins.b)
	case opSetUpvalue:
		return fmt.Sprintf("SET_UPVALUE u%d r%d", ins.a, ins.b)
	case opVararg:
		return fmt.Sprintf("VARARG r%d %d", ins.a, ins.b)
	case opPrepareIter:
		return fmt.Sprintf("PREPARE_ITER r%d r%d r%d", ins.a, ins.b, ins.c)
	case opArrayNext:
		return fmt.Sprintf("ARRAY_NEXT r%d r%d r%d %d", ins.a, ins.b, ins.c, ins.d)
	case opArrayNextJump2:
		return fmt.Sprintf("ARRAY_NEXT_JUMP2 r%d r%d r%d %d", ins.a, ins.b, ins.c, ins.d)
	case opAdd:
		return disassembleABC("ADD", ins)
	case opSub:
		return disassembleABC("SUB", ins)
	case opMul:
		return disassembleABC("MUL", ins)
	case opDiv:
		return disassembleABC("DIV", ins)
	case opMod:
		return disassembleABC("MOD", ins)
	case opIDiv:
		return disassembleABC("IDIV", ins)
	case opPow:
		return disassembleABC("POW", ins)
	case opNeg:
		return fmt.Sprintf("NEG r%d r%d", ins.a, ins.b)
	case opLen:
		return fmt.Sprintf("LEN r%d r%d", ins.a, ins.b)
	case opConcat:
		return disassembleABC("CONCAT", ins)
	case opConcatChain:
		return fmt.Sprintf("CONCAT_CHAIN r%d r%d %d", ins.a, ins.b, ins.c)
	case opAddK:
		return disassembleABK("ADD_K", proto, ins)
	case opSubK:
		return disassembleABK("SUB_K", proto, ins)
	case opMulK:
		return disassembleABK("MUL_K", proto, ins)
	case opDivK:
		return disassembleABK("DIV_K", proto, ins)
	case opModK:
		return disassembleABK("MOD_K", proto, ins)
	case opIDivK:
		return disassembleABK("IDIV_K", proto, ins)
	case opEqual:
		return disassembleABC("EQUAL", ins)
	case opNotEqual:
		return disassembleABC("NOT_EQUAL", ins)
	case opLess:
		return disassembleABC("LESS", ins)
	case opLessEqual:
		return disassembleABC("LESS_EQUAL", ins)
	case opGreater:
		return disassembleABC("GREATER", ins)
	case opGreaterEqual:
		return disassembleABC("GREATER_EQUAL", ins)
	case opNumericForCheck:
		return fmt.Sprintf("NUMERIC_FOR_CHECK r%d r%d r%d %d", ins.a, ins.b, ins.c, ins.d)
	case opNumericForLoop:
		return fmt.Sprintf("NUMERIC_FOR_LOOP r%d r%d %d", ins.a, ins.b, ins.d)
	case opJumpIfNotEqualK:
		return fmt.Sprintf("JUMP_IF_NOT_EQUAL_K r%d %s %d", ins.a, disassembleConstant(proto, ins.b), ins.d)
	case opJumpIfNotLessK:
		return fmt.Sprintf("JUMP_IF_NOT_LESS_K r%d %s %d", ins.a, disassembleConstant(proto, ins.b), ins.d)
	case opJumpIfNotGreaterK:
		return fmt.Sprintf("JUMP_IF_NOT_GREATER_K r%d %s %d", ins.a, disassembleConstant(proto, ins.b), ins.d)
	case opJumpIfLessK:
		return fmt.Sprintf("JUMP_IF_LESS_K r%d %s %d", ins.a, disassembleConstant(proto, ins.b), ins.d)
	case opJumpIfGreaterK:
		return fmt.Sprintf("JUMP_IF_GREATER_K r%d %s %d", ins.a, disassembleConstant(proto, ins.b), ins.d)
	case opJumpIfNotLess:
		return fmt.Sprintf("JUMP_IF_NOT_LESS r%d r%d %d", ins.a, ins.b, ins.d)
	case opJumpIfNotGreater:
		return fmt.Sprintf("JUMP_IF_NOT_GREATER r%d r%d %d", ins.a, ins.b, ins.d)
	case opJumpIfLess:
		return fmt.Sprintf("JUMP_IF_LESS r%d r%d %d", ins.a, ins.b, ins.d)
	case opJumpIfGreater:
		return fmt.Sprintf("JUMP_IF_GREATER r%d r%d %d", ins.a, ins.b, ins.d)
	case opJumpIfTableHasMetatable:
		return fmt.Sprintf("JUMP_IF_TABLE_HAS_METATABLE r%d %d", ins.a, ins.d)
	case opFastCall:
		return fmt.Sprintf("FAST_CALL r%d %s args %d results %d", ins.a, nativeFuncName(nativeFuncID(ins.b)), ins.c, ins.d)
	case opCall:
		return fmt.Sprintf("CALL r%d r%d %d %d", ins.a, ins.b, ins.c, ins.d)
	case opCallOne:
		return fmt.Sprintf("CALL_ONE r%d r%d %s", ins.a, ins.b, disassembleFixedCallCount(ins.c))
	case opCallLocalOne:
		return fmt.Sprintf("CALL_LOCAL_ONE r%d r%d r%d %s", ins.a, ins.b, ins.c, disassembleFixedCallCount(ins.d))
	case opCallUpvalueOne:
		return fmt.Sprintf("CALL_UPVALUE_ONE r%d u%d r%d %s", ins.a, ins.b, ins.c, disassembleFixedCallCount(ins.d))
	case opCallMethodOne:
		return fmt.Sprintf("CALL_METHOD_ONE r%d r%d %s %s", ins.a, ins.b, disassembleConstant(proto, ins.c), disassembleFixedCallCount(ins.d))
	case opJumpIfFalse:
		return fmt.Sprintf("JUMP_IF_FALSE r%d %d", ins.a, ins.b)
	case opJump:
		return fmt.Sprintf("JUMP %d", ins.b)
	case opReturnOne:
		return fmt.Sprintf("RETURN_ONE r%d", ins.a)
	case opReturn:
		return fmt.Sprintf("RETURN r%d %d", ins.a, ins.b)
	default:
		return fmt.Sprintf("UNKNOWN_%d %d %d %d %d", ins.op, ins.a, ins.b, ins.c, ins.d)
	}
}

func disassembleABC(name string, ins instruction) string {
	return fmt.Sprintf("%s r%d r%d r%d", name, ins.a, ins.b, ins.c)
}

func disassembleABK(name string, proto *Proto, ins instruction) string {
	return fmt.Sprintf("%s r%d r%d %s", name, ins.a, ins.b, disassembleConstant(proto, ins.c))
}

func disassembleConstantString(proto *Proto, index int) string {
	if proto == nil || index < 0 || index >= len(proto.constants) {
		return fmt.Sprintf("k%d", index)
	}
	value := proto.constants[index]
	if valueKind(value) != StringKind {
		return fmt.Sprintf("k%d", index)
	}
	return value.stringText()
}

func disassembleConstant(proto *Proto, index int) string {
	if proto == nil || index < 0 || index >= len(proto.constants) {
		return fmt.Sprintf("k%d(<invalid>)", index)
	}
	value := proto.constants[index]
	switch valueKind(value) {
	case NilKind:
		return fmt.Sprintf("k%d(nil)", index)
	case BoolKind:
		return fmt.Sprintf("k%d(boolean %t)", index, valueBool(value))
	case NumberKind:
		return fmt.Sprintf("k%d(number %g)", index, valueNumber(value))
	case StringKind:
		return fmt.Sprintf("k%d(string %q)", index, value.stringText())
	default:
		return fmt.Sprintf("k%d(%s)", index, value.Kind())
	}
}
