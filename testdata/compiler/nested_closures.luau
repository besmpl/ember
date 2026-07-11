local root = 1

local function level1(first)
	local one = 2
	local function level2(second)
		local two = 3
		local function level3(third)
			local three = 4
			local function level4(fourth)
				return root + first + one + second + two + third + three + fourth
			end
			return level4
		end
		return level3
	end
	return level2
end

local level2 = level1(1)
local level3 = level2(1)
local level4 = level3(1)
return level4(1)
