local value = 0
if false then
	value = value + 1
else
	value = value + 2
end
if false then
	value = value + 3
else
	value = value + 4
end
while false do
	value = value + 5
end
for index = 1, 2 do
	value = value + index
end
if value > 100 then
	value = value + 6
elseif value > 50 then
	value = value + 7
else
	value = value + 8
end
return value
