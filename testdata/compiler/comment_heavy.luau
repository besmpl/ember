--!nonstrict
-- repository-owned comment-heavy compiler fixture
-- comments are retained by the diagnostic lexer but discarded by compilation
-- this line carries punctuation: {} [] () <> :: -> ~=
local value = 0 -- initialize the accumulator
-- the next assignments intentionally carry trailing comments
value = value + 1 -- one
value = value + 2 -- two
value = value + 3 -- three
-- a block comment follows
--[[
  block comments may span lines and contain source-looking tokens:
  local not_a_statement = "still a comment"
]]
value = value + 4 -- four
value = value + 5 -- five
-- final comment before the return
return value -- deterministic result
