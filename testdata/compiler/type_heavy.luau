--!strict
type Scalar = number | string?
type Pair<T, U...> = {left: T}
type Identity<T> = (T) -> T

local value: Scalar = 1
local function identity<T>(input: T): T
	return input
end

local casted = (value :: number)
return identity(casted)
