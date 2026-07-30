package ember

import "testing"

const backendArrayHoleProbeSource = `
local function kernel(seed)
    local values = {}
    for i = 1, 30 + seed % 2 do
        values[i] = {score = i * 3 + seed % 3, live = true}
    end
    local total = 0
    for tick = 1, 70 + seed % 2 do
        local i = 1
        while i <= rawlen(values) do
            local row = values[i]
            row.score = row.score + tick % 6
            total = total + row.score
            if row.score % 13 == 0 then
                table.remove(values, i)
            else
                i = i + 1
            end
        end
        if tick % 5 == 0 then
            table.insert(values, {score = tick + seed % 3, live = true})
        end
    end
    return total + rawlen(values)
end
return kernel
`

func TestBackendArrayHoleProbe(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendArrayHoleProbeSource)
	if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName: "ember", functionName: "backendGeneratedArrayHoleProbe",
	}); err != nil {
		t.Fatalf("current rejection: %v", err)
	}
}
