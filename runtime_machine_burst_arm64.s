#include "textflag.h"
#include "go_asm.h"

// runMachineBurstArm64 is a static no-call leaf. The Go wrapper validates all
// descriptor and arena bounds before entering it.
TEXT ·runMachineBurstArm64(SB), NOSPLIT|NOFRAME, $0-88
	MOVD control+0(FP), R0
	MOVD region+8(FP), R1
	MOVD operations+16(FP), R2
	MOVD operationCount+24(FP), R3
	MOVD guards+32(FP), R4
	MOVD guardCount+40(FP), R5
	MOVD registers+48(FP), R6
	MOVD registerCount+56(FP), R7
	MOVD numberBits+64(FP), R8
	MOVD numberCount+72(FP), R9
	MOVD workspace+80(FP), R7
	ADD $256, R7, R27 // dirty-register array
	MOVWU machineBurstControl_quantum(R0), R12
	MOVWU machineBurstControl_base(R0), R10
	MOVD $0, R11 // retired
	MOVD $0, R13 // operation index
	MOVD $0, R14 // guard index

operationLoop:
	CMP R3, R13
	BHS iterationDone
	LSL $5, R13, R15
	ADD R2, R15, R15
	MOVWU machineBurstOperation_pc(R15), R16

guardLoop:
	CMP R5, R14
	BHS guardsDone
	LSL $3, R14, R17
	ADD R4, R17, R17
	MOVWU machineBurstGuard_firstPC(R17), R19
	CMP R16, R19
	BNE guardsDone
	MOVWU machineBurstGuard_register(R17), R20
	MOVD R20, R22
	ADD R10, R20, R20
	MOVD (R6)(R20<<3), R23
	MOVD $const_slotTaggedMask, R24
	AND R24, R23, R25
	MOVD $const_slotTaggedPrefix, R26
	CMP R26, R25
	BNE guardStore
	LSR $const_slotTagShift, R23, R25
	AND $15, R25, R25
	CMP $const_slotTagBoxedNumber, R25
	BNE fallback
	LSR $const_slotGenerationShift, R23, R25
	AND $65535, R25, R25
	CMP $1, R25
	BNE fallback
	MOVD $268435455, R25
	AND R25, R23, R25
	CBZ R25, fallback
	CMP R9, R25
	BHI fallback
	SUB $1, R25, R25
	MOVD (R8)(R25<<3), R23
guardStore:
	MOVD R23, (R7)(R22<<3)
	ADD $1, R14, R14
	B guardLoop

guardsDone:
	CMP R12, R11
	BHS quantum
	MOVBU machineBurstOperation_op(R15), R19
	ADR opcodeTable, R17
	LSL $2, R19, R19
	ADD R17, R19, R17
	JMP (R17)

// Opcodes are validated by the Go wrapper. Keeping a complete table through
// NUMERIC_FOR_LOOP makes the indirect branch locally bounded even if that
// validation changes later.
opcodeTable:
	B fallback // 0
	B fallback // LOAD_CONST
	B fallback // LOAD_GLOBAL
	B fallback // SET_GLOBAL
	B move
	B fallback // NEW_TABLE
	B fallback // SET_FIELD
	B fallback // 7
	B fallback // SET_STRING_FIELD
	B fallback // SET_STRING_FIELD_INDEX
	B fallback // GET_STRING_FIELD
	B fallback // GET_STRING_FIELD_INDEX
	B fallback // 12
	B fallback // 13
	B fallback // SET_INDEX
	B fallback // GET_INDEX
	B fallback // CLOSURE
	B fallback // GET_UPVALUE
	B fallback // SET_UPVALUE
	B fallback // VARARG
	B fallback // PREPARE_ITER
	B fallback // ARRAY_NEXT
	B fallback // ARRAY_NEXT_JUMP2
	B add
	B sub
	B fallback // MUL
	B fallback // DIV
	B fallback // MOD
	B fallback // IDIV
	B fallback // POW
	B fallback // NEG
	B fallback // LEN
	B fallback // CONCAT
	B fallback // CONCAT_CHAIN
	B fallback // ADD_K
	B fallback // SUB_K
	B mulK
	B fallback // DIV_K
	B modK
	B idivK
	B fallback // EQUAL
	B fallback // NOT_EQUAL
	B fallback // LESS
	B fallback // LESS_EQUAL
	B fallback // GREATER
	B fallback // GREATER_EQUAL
	B numericForCheck
	B numericForLoop

numericForCheck:
	MOVWU machineBurstOperation_a(R15), R20
	ADD R10, R20, R20
	MOVD (R7)(R20<<3), R23
	FMOVD R23, F0

	MOVWU machineBurstOperation_b(R15), R20
	ADD R10, R20, R20
	MOVD (R7)(R20<<3), R23
	FMOVD R23, F1

	MOVWU machineBurstOperation_c(R15), R20
	ADD R10, R20, R20
	MOVD (R7)(R20<<3), R23
	FMOVD R23, F2
	FCMPD F0, F0
	BNE fallback
	FCMPD F1, F1
	BNE fallback
	FCMPD F2, F2
	BNE fallback
	ADD $1, R11, R11
	FCMPD $0.0, F2
	BGT positiveStep
	FCMPD F1, F0
	BLT complete
	B checkDone
positiveStep:
	FCMPD F1, F0
	BGT complete
	B checkDone

checkDone:
	ADD $1, R13, R13
	B operationLoop

move:
	MOVWU machineBurstOperation_b(R15), R22
	MOVD (R7)(R22<<3), R23
	FMOVD R23, F0
	B storeResult

mulK:
	MOVWU machineBurstOperation_b(R15), R22
	MOVD (R7)(R22<<3), R23
	FMOVD R23, F0
	MOVD machineBurstOperation_bits(R15), R24
	FMOVD R24, F1
	FMULD F1, F0, F0
	B storeConstantResult

idivK:
	MOVWU machineBurstOperation_b(R15), R22
	MOVD (R7)(R22<<3), R23
	FMOVD R23, F0
	MOVD machineBurstOperation_bits(R15), R24
	FMOVD R24, F1
	FDIVD F1, F0, F0
	FRINTMD F0, F0
	B storeConstantResult

modK:
	MOVWU machineBurstOperation_b(R15), R22
	MOVD (R7)(R22<<3), R23
	FMOVD R23, F0
	MOVD machineBurstOperation_bits(R15), R24
	FMOVD R24, F1
	FDIVD F1, F0, F2
	FRINTMD F2, F2
	FMULD F1, F2, F2
	FSUBD F2, F0, F0
	B storeConstantResult

sub:
	MOVWU machineBurstOperation_b(R15), R22
	MOVD (R7)(R22<<3), R23
	FMOVD R23, F0
	MOVWU machineBurstOperation_c(R15), R22
	MOVD (R7)(R22<<3), R23
	FMOVD R23, F1
	FSUBD F1, F0, F0
	B storeResult

add:
	MOVWU machineBurstOperation_b(R15), R22
	MOVD (R7)(R22<<3), R23
	FMOVD R23, F0
	MOVWU machineBurstOperation_c(R15), R22
	MOVD (R7)(R22<<3), R23
	FMOVD R23, F1
	FADDD F1, F0, F0
	B storeResult

numericForLoop:
	MOVWU machineBurstOperation_a(R15), R22
	MOVD (R7)(R22<<3), R23
	FMOVD R23, F0
	MOVWU machineBurstOperation_b(R15), R22
	MOVD (R7)(R22<<3), R23
	FMOVD R23, F1
	FADDD F1, F0, F0
	B storeResult

storeConstantResult:
	MOVD $const_slotTaggedMask, R25
	AND R25, R24, R26
	MOVD $const_slotTaggedPrefix, R23
	CMP R23, R26
	BNE storeResult
	MOVWU machineBurstOperation_b(R15), R22
	MOVD (R7)(R22<<3), R23
	AND R25, R23, R26
	MOVD $const_slotTaggedPrefix, R25
	CMP R25, R26
	BNE storeResult
	SUB $1, R9, R25
	MOVD R24, (R8)(R25<<3)

storeResult:
	MOVWU machineBurstOperation_a(R15), R21
	FMOVD F0, R24
	MOVD R24, (R7)(R21<<3)
	MOVD $1, R25
	MOVD R25, (R27)(R21<<3)
	MOVD $const_slotTaggedMask, R25
	AND R25, R24, R26
	MOVD $const_slotTaggedPrefix, R23
	CMP R23, R26
	BNE operationDone
	ADD R10, R21, R21
	MOVD R24, (R8)(R21<<3)

operationDone:
	ADD $1, R11, R11
	ADD $1, R13, R13
	B operationLoop

iterationDone:
	MOVD $0, R13
	B operationLoopNoGuards

operationLoopNoGuards:
	CMP R3, R13
	BHS iterationDoneNoGuards
	LSL $5, R13, R15
	ADD R2, R15, R15
	MOVWU machineBurstOperation_pc(R15), R16
	B guardsDone

iterationDoneNoGuards:
	MOVD $0, R13
	B operationLoopNoGuards

complete:
	MOVD $const_machineBurstComplete, R17
	MOVW R17, machineBurstControl_status(R0)
	MOVWU machineBurstRegion_exitPC(R1), R17
	MOVW R17, machineBurstControl_nextPC(R0)
	MOVD $-1, R17
	MOVW R17, machineBurstControl_failingPC(R0)
	MOVW R11, machineBurstControl_retired(R0)
	B flush

quantum:
	MOVD $const_machineBurstQuantum, R17
	MOVW R17, machineBurstControl_status(R0)
	MOVW R16, machineBurstControl_nextPC(R0)
	MOVD $-1, R17
	MOVW R17, machineBurstControl_failingPC(R0)
	MOVW R11, machineBurstControl_retired(R0)
	B flush

fallback:
	MOVD $const_machineBurstFallback, R17
	MOVW R17, machineBurstControl_status(R0)
	MOVW R16, machineBurstControl_nextPC(R0)
	MOVW R16, machineBurstControl_failingPC(R0)
	MOVW R11, machineBurstControl_retired(R0)

flush:
	MOVD $0, R20
flushLoop:
	CMP $const_machineBurstWorkspaceRegisters, R20
	BEQ flushDone
	MOVD (R27)(R20<<3), R21
	CBZ R21, flushNext
	MOVD (R7)(R20<<3), R24
	MOVD $const_slotTaggedMask, R25
	AND R25, R24, R26
	MOVD $const_slotTaggedPrefix, R23
	ADD R10, R20, R21
	CMP R23, R26
	BNE flushUnboxed
	MOVD R24, (R8)(R21<<3)
	MOVD $(const_slotTaggedPrefix | (const_slotTagBoxedNumber << const_slotTagShift) | (1 << const_slotGenerationShift)), R25
	ADD $1, R21, R26
	ORR R26, R25, R25
	MOVD R25, (R6)(R21<<3)
	B flushNext
flushUnboxed:
	MOVD R24, (R6)(R21<<3)
flushNext:
	ADD $1, R20, R20
	B flushLoop
flushDone:
	RET
