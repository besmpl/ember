//go:build darwin && arm64

#include "textflag.h"

// This is direct program text, not a descriptor interpreter. It mirrors the
// Go compiler's lowering so the proof isolates the static-assembly call and
// integration boundary rather than representation or semantic differences.
TEXT ·arithmeticForASM(SB), NOSPLIT|NOFRAME, $0-16
	MOVD seed+0(FP), R0
	MOVD $5270498306774157605, R1
	SMULH R0, R1, R1
	ASR $1, R1, R1
	SUB R0->63, R1, R1
	ADD R1, R0, R2
	SUB R1<<3, R2, R1
	MOVD $1, R0
	JMP asm_arithmetic_test

asm_arithmetic_loop:
	ADD R0<<1, R0, R2
	SUB R0>>1, R2, R2
	MOVD $-1085102592571150095, R3
	SMULH R2, R3, R3
	ADD R3, R2, R3
	ASR $4, R3, R3
	SUB R2->63, R3, R3
	ADD R3<<4, R3, R3
	SUB R3, R2, R2
	ADD R1, R2, R1
	ADD $1, R0, R0

asm_arithmetic_test:
	CMP $200, R0
	BLE asm_arithmetic_loop
	MOVD R1, ret+8(FP)
	RET
