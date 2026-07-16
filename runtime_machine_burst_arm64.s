#include "textflag.h"
#include "go_asm.h"

// runMachineBurstArm64 is a static no-call leaf. The Go wrapper validates all
// descriptor and arena bounds before entering it.
TEXT ·runMachineBurstArm64(SB), NOSPLIT|NOFRAME, $0-80
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
	ADD R10, R20, R20
	MOVD (R6)(R20<<3), R23
	MOVD $const_slotTaggedMask, R24
	AND R24, R23, R25
	MOVD $const_slotTaggedPrefix, R26
	CMP R26, R25
	BNE guardValid
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
guardValid:
	ADD $1, R14, R14
	B guardLoop

guardsDone:
	CMP R12, R11
	BHS quantum
	MOVBU machineBurstOperation_op(R15), R19
	CMP $const_opNumericForCheck, R19
	BEQ numericForCheck
	CMP $const_opMove, R19
	BEQ move
	CMP $const_opMulK, R19
	BEQ mulK
	CMP $const_opIDivK, R19
	BEQ idivK
	CMP $const_opSub, R19
	BEQ sub
	CMP $const_opModK, R19
	BEQ modK
	CMP $const_opAdd, R19
	BEQ add
	CMP $const_opNumericForLoop, R19
	BEQ numericForLoop
	B fallback

numericForCheck:
	MOVWU machineBurstOperation_a(R15), R20
	ADD R10, R20, R20
	MOVD (R6)(R20<<3), R23
	MOVD $const_slotTaggedMask, R24
	AND R24, R23, R25
	MOVD $const_slotTaggedPrefix, R26
	CMP R26, R25
	BNE checkLoopUnboxed
	MOVD $268435455, R25
	AND R25, R23, R25
	SUB $1, R25, R25
	MOVD (R8)(R25<<3), R23
checkLoopUnboxed:
	FMOVD R23, F0

	MOVWU machineBurstOperation_b(R15), R20
	ADD R10, R20, R20
	MOVD (R6)(R20<<3), R23
	MOVD $const_slotTaggedMask, R24
	AND R24, R23, R25
	MOVD $const_slotTaggedPrefix, R26
	CMP R26, R25
	BNE checkLimitUnboxed
	MOVD $268435455, R25
	AND R25, R23, R25
	SUB $1, R25, R25
	MOVD (R8)(R25<<3), R23
checkLimitUnboxed:
	FMOVD R23, F1

	MOVWU machineBurstOperation_c(R15), R20
	ADD R10, R20, R20
	MOVD (R6)(R20<<3), R23
	MOVD $const_slotTaggedMask, R24
	AND R24, R23, R25
	MOVD $const_slotTaggedPrefix, R26
	CMP R26, R25
	BNE checkStepUnboxed
	MOVD $268435455, R25
	AND R25, R23, R25
	SUB $1, R25, R25
	MOVD (R8)(R25<<3), R23
checkStepUnboxed:
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
	ADD R10, R22, R22
	MOVD (R6)(R22<<3), R23
	MOVD $const_slotTaggedMask, R24
	AND R24, R23, R25
	MOVD $const_slotTaggedPrefix, R26
	CMP R26, R25
	BNE moveUnboxed
	MOVD $268435455, R25
	AND R25, R23, R25
	SUB $1, R25, R25
	MOVD (R8)(R25<<3), R23
moveUnboxed:
	FMOVD R23, F0
	B storeResult

mulK:
	MOVWU machineBurstOperation_b(R15), R22
	ADD R10, R22, R22
	MOVD (R6)(R22<<3), R23
	MOVD $const_slotTaggedMask, R24
	AND R24, R23, R25
	MOVD $const_slotTaggedPrefix, R26
	CMP R26, R25
	BNE mulLeftUnboxed
	MOVD $268435455, R25
	AND R25, R23, R25
	SUB $1, R25, R25
	MOVD (R8)(R25<<3), R24
	FMOVD R24, F0
	B mulConstant
mulLeftUnboxed:
	FMOVD R23, F0
mulConstant:
	MOVD machineBurstOperation_bits(R15), R24
	FMOVD R24, F1
	FMULD F1, F0, F0
	B storeConstantResult

idivK:
	MOVWU machineBurstOperation_b(R15), R22
	ADD R10, R22, R22
	MOVD (R6)(R22<<3), R23
	MOVD $const_slotTaggedMask, R24
	AND R24, R23, R25
	MOVD $const_slotTaggedPrefix, R26
	CMP R26, R25
	BNE idivLeftUnboxed
	MOVD $268435455, R25
	AND R25, R23, R25
	SUB $1, R25, R25
	MOVD (R8)(R25<<3), R24
	FMOVD R24, F0
	B idivConstant
idivLeftUnboxed:
	FMOVD R23, F0
idivConstant:
	MOVD machineBurstOperation_bits(R15), R24
	FMOVD R24, F1
	FDIVD F1, F0, F0
	FRINTMD F0, F0
	B storeConstantResult

modK:
	MOVWU machineBurstOperation_b(R15), R22
	ADD R10, R22, R22
	MOVD (R6)(R22<<3), R23
	MOVD $const_slotTaggedMask, R24
	AND R24, R23, R25
	MOVD $const_slotTaggedPrefix, R26
	CMP R26, R25
	BNE modLeftUnboxed
	MOVD $268435455, R25
	AND R25, R23, R25
	SUB $1, R25, R25
	MOVD (R8)(R25<<3), R24
	FMOVD R24, F0
	B modConstant
modLeftUnboxed:
	FMOVD R23, F0
modConstant:
	MOVD machineBurstOperation_bits(R15), R24
	FMOVD R24, F1
	FDIVD F1, F0, F2
	FRINTMD F2, F2
	FMULD F1, F2, F2
	FSUBD F2, F0, F0
	B storeConstantResult

sub:
	MOVWU machineBurstOperation_b(R15), R22
	ADD R10, R22, R22
	MOVD (R6)(R22<<3), R23
	MOVD $const_slotTaggedMask, R24
	AND R24, R23, R25
	MOVD $const_slotTaggedPrefix, R26
	CMP R26, R25
	BNE subLeftUnboxed
	MOVD $268435455, R25
	AND R25, R23, R25
	SUB $1, R25, R25
	MOVD (R8)(R25<<3), R23
subLeftUnboxed:
	FMOVD R23, F0
	MOVWU machineBurstOperation_c(R15), R22
	ADD R10, R22, R22
	MOVD (R6)(R22<<3), R23
	MOVD $const_slotTaggedMask, R24
	AND R24, R23, R25
	MOVD $const_slotTaggedPrefix, R26
	CMP R26, R25
	BNE subRightUnboxed
	MOVD $268435455, R25
	AND R25, R23, R25
	SUB $1, R25, R25
	MOVD (R8)(R25<<3), R23
subRightUnboxed:
	FMOVD R23, F1
	FSUBD F1, F0, F0
	B storeResult

add:
	MOVWU machineBurstOperation_b(R15), R22
	ADD R10, R22, R22
	MOVD (R6)(R22<<3), R23
	MOVD $const_slotTaggedMask, R24
	AND R24, R23, R25
	MOVD $const_slotTaggedPrefix, R26
	CMP R26, R25
	BNE addLeftUnboxed
	MOVD $268435455, R25
	AND R25, R23, R25
	SUB $1, R25, R25
	MOVD (R8)(R25<<3), R23
addLeftUnboxed:
	FMOVD R23, F0
	MOVWU machineBurstOperation_c(R15), R22
	ADD R10, R22, R22
	MOVD (R6)(R22<<3), R23
	MOVD $const_slotTaggedMask, R24
	AND R24, R23, R25
	MOVD $const_slotTaggedPrefix, R26
	CMP R26, R25
	BNE addRightUnboxed
	MOVD $268435455, R25
	AND R25, R23, R25
	SUB $1, R25, R25
	MOVD (R8)(R25<<3), R23
addRightUnboxed:
	FMOVD R23, F1
	FADDD F1, F0, F0
	B storeResult

numericForLoop:
	MOVWU machineBurstOperation_a(R15), R22
	ADD R10, R22, R22
	MOVD (R6)(R22<<3), R23
	MOVD $const_slotTaggedMask, R24
	AND R24, R23, R25
	MOVD $const_slotTaggedPrefix, R26
	CMP R26, R25
	BNE latchLoopUnboxed
	MOVD $268435455, R25
	AND R25, R23, R25
	SUB $1, R25, R25
	MOVD (R8)(R25<<3), R23
latchLoopUnboxed:
	FMOVD R23, F0
	MOVWU machineBurstOperation_b(R15), R22
	ADD R10, R22, R22
	MOVD (R6)(R22<<3), R23
	MOVD $const_slotTaggedMask, R24
	AND R24, R23, R25
	MOVD $const_slotTaggedPrefix, R26
	CMP R26, R25
	BNE latchStepUnboxed
	MOVD $268435455, R25
	AND R25, R23, R25
	SUB $1, R25, R25
	MOVD (R8)(R25<<3), R23
latchStepUnboxed:
	FMOVD R23, F1
	FADDD F1, F0, F0
	B storeResult

storeConstantResult:
	MOVD $const_slotTaggedMask, R25
	AND R25, R23, R26
	MOVD $const_slotTaggedPrefix, R27
	CMP R27, R26
	BNE storeResult
	MOVD $const_slotTaggedMask, R25
	AND R25, R24, R26
	CMP R27, R26
	BNE storeResult
	SUB $1, R9, R25
	MOVD R24, (R8)(R25<<3)

storeResult:
	MOVWU machineBurstOperation_a(R15), R21
	ADD R10, R21, R21
	FMOVD F0, R24
	MOVD $const_slotTaggedMask, R25
	AND R25, R24, R26
	MOVD $const_slotTaggedPrefix, R27
	CMP R27, R26
	BNE storeUnboxed
	MOVD R24, (R8)(R21<<3)
	MOVD $(const_slotTaggedPrefix | (const_slotTagBoxedNumber << const_slotTagShift) | (1 << const_slotGenerationShift)), R25
	ADD $1, R21, R26
	ORR R26, R25, R25
	MOVD R25, (R6)(R21<<3)
	B operationDone
storeUnboxed:
	MOVD R24, (R6)(R21<<3)

operationDone:
	ADD $1, R11, R11
	ADD $1, R13, R13
	B operationLoop

iterationDone:
	MOVD $0, R13
	MOVD $0, R14
	B operationLoop

complete:
	MOVD $const_machineBurstComplete, R17
	MOVW R17, machineBurstControl_status(R0)
	MOVWU machineBurstRegion_exitPC(R1), R17
	MOVW R17, machineBurstControl_nextPC(R0)
	MOVD $-1, R17
	MOVW R17, machineBurstControl_failingPC(R0)
	MOVW R11, machineBurstControl_retired(R0)
	RET

quantum:
	MOVD $const_machineBurstQuantum, R17
	MOVW R17, machineBurstControl_status(R0)
	MOVW R16, machineBurstControl_nextPC(R0)
	MOVD $-1, R17
	MOVW R17, machineBurstControl_failingPC(R0)
	MOVW R11, machineBurstControl_retired(R0)
	RET

fallback:
	MOVD $const_machineBurstFallback, R17
	MOVW R17, machineBurstControl_status(R0)
	MOVW R16, machineBurstControl_nextPC(R0)
	MOVW R16, machineBurstControl_failingPC(R0)
	MOVW R11, machineBurstControl_retired(R0)
	RET
