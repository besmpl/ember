--!strict
type Deep<T> = {value: T}

local function evaluate(value: number): number
	do
		local truth = not not (value == value)
		local nested = -(-(-value))
		if truth then
			return (nested ^ (2 ^ 3))
		end
	end
	return 0
end

return evaluate(2)
