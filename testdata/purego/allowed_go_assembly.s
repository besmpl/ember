# This fixture is Go assembler, with no foreign call targets.

	TEXT ·pureGoAssemblyFixture(SB),NOSPLIT,$0-0
	CALL ·ordinaryGoTarget(SB)
	BL ·armTarget(SB)
	JAL ·riscvTarget(SB)
	JMP ·goTailTarget(SB)

localLoop:
	B localLoop
	RET
