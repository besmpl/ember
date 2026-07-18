//go:build darwin || linux

#include "go_asm.h"
#include "textflag.h"

// nativeCallTrampoline is entered through runtime.cgocall using the System V
// C ABI. Generated x86-64 kernels use Ember's fixed internal ABI:
// DI=args, SI=count, DX=result, AX=status.
GLOBL ·nativeCallTrampolineABI0(SB), NOPTR|RODATA, $8
DATA ·nativeCallTrampolineABI0(SB)/8, $nativeCallTrampoline(SB)

TEXT nativeCallTrampoline(SB), NOSPLIT|NOFRAME, $0
	PUSHQ R12
	MOVQ DI, R12
	MOVQ nativeCallFrame_entry(R12), AX
	MOVQ nativeCallFrame_count(R12), SI
	LEAQ nativeCallFrame_result(R12), DX
	MOVQ nativeCallFrame_arguments(R12), DI
	CALL AX
	MOVQ AX, nativeCallFrame_status(R12)
	POPQ R12
	RET
