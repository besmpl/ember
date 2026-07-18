//go:build windows && amd64

#include "go_asm.h"
#include "textflag.h"

// nativeCallTrampoline is entered through runtime.cgocall using the Windows
// x64 ABI. Generated kernels retain Ember's private DI/SI/DX boundary. Preserve
// every Windows nonvolatile register the current kernel ISA may modify.
GLOBL ·nativeCallTrampolineABI0(SB), NOPTR|RODATA, $8
DATA ·nativeCallTrampolineABI0(SB)/8, $nativeCallTrampoline(SB)

TEXT nativeCallTrampoline(SB), NOSPLIT, $64
	MOVOU X6, 0(SP)
	MOVOU X7, 16(SP)
	MOVQ DI, 32(SP)
	MOVQ SI, 40(SP)
	MOVQ R12, 48(SP)
	MOVQ CX, R12
	MOVQ nativeCallFrame_entry(R12), AX
	MOVQ nativeCallFrame_count(R12), SI
	LEAQ nativeCallFrame_result(R12), DX
	MOVQ nativeCallFrame_arguments(R12), DI
	CALL AX
	MOVQ AX, nativeCallFrame_status(R12)
	MOVOU 0(SP), X6
	MOVOU 16(SP), X7
	MOVQ 32(SP), DI
	MOVQ 40(SP), SI
	MOVQ 48(SP), R12
	RET
