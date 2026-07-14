	TEXT ·alternateAssemblyFixture(SB),NOSPLIT,$0-0
	BL ·armTarget(SB)
	BLR R0
	JAL ·riscvTarget(SB)
	JALR R1
	RET
