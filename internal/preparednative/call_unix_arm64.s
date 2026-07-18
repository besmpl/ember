//go:build darwin || linux

#include "go_asm.h"
#include "textflag.h"

// nativeCallTrampoline is entered on the runtime's system stack using the
// platform C ABI. It translates one pointer-to-frame argument into Ember's
// kernel ABI.
GLOBL ·nativeCallTrampolineABI0(SB), NOPTR|RODATA, $8
DATA ·nativeCallTrampolineABI0(SB)/8, $nativeCallTrampoline(SB)

TEXT nativeCallTrampoline(SB), NOSPLIT, $0
	SUB	$16, RSP
	MOVD	R0, 0(RSP)
	MOVD	nativeCallFrame_entry(R0), R9
	MOVD	nativeCallFrame_count(R0), R1
	ADD	$nativeCallFrame_result, R0, R2
	MOVD	nativeCallFrame_arguments(R0), R0
	CALL	(R9)
	MOVD	0(RSP), R9
	MOVD	R0, nativeCallFrame_status(R9)
	ADD	$16, RSP
	RET
