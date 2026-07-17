package parityfixture

import (
	"fmt"
	"strings"
)

type GuestBatchVariant struct {
	CaseName  string
	BatchName string
	Holdout   bool
}

type GuestBatchFixture struct {
	Program      string
	SeededSource string
}

func BuildGuestBatch(source string, variant GuestBatchVariant) (GuestBatchFixture, error) {
	seeded, err := seedNumericLiterals(source)
	if err != nil {
		return GuestBatchFixture{}, err
	}
	if variant.Holdout {
		seeded, err = transformHoldoutSource(seeded)
		if err != nil {
			return GuestBatchFixture{}, err
		}
	}
	program := fmt.Sprintf(`local %s = function(__seed)
%s
end
local %s = function(__n, __seed)
    local __checksum = 0
    for __i = 1, __n do
        local __value = %s(__seed + __i)
        __checksum = __checksum + __value * (__i %% 7 + 1)
    end
    return __checksum
end
`, variant.CaseName, seeded, variant.BatchName, variant.CaseName)
	return GuestBatchFixture{Program: program, SeededSource: seeded}, nil
}

func transformHoldoutSource(source string) (string, error) {
	for index := 0; index < len(source); {
		switch {
		case source[index] == '"' || source[index] == '\'':
			var copied strings.Builder
			next, err := copyQuoted(&copied, source, index, source[index])
			if err != nil {
				return "", err
			}
			index = next
		case index+1 < len(source) && source[index:index+2] == "--":
			next := strings.IndexByte(source[index:], '\n')
			if next < 0 {
				return "", fmt.Errorf("holdout parity source: no numeric literal")
			}
			index += next + 1
		case index+1 < len(source) && source[index:index+2] == "[[":
			end := strings.Index(source[index+2:], "]]")
			if end < 0 {
				return "", fmt.Errorf("holdout parity source: unterminated long string")
			}
			index += end + 4
		case source[index] >= '0' && source[index] <= '9' && numberBoundary(source, index):
			end := index + 1
			for end < len(source) && source[end] >= '0' && source[end] <= '9' {
				end++
			}
			if end < len(source) && (source[end] == '.' || identifierByte(source[end])) {
				return "", fmt.Errorf("holdout parity source: unsupported numeric literal near %q", source[index:])
			}
			return "-- identity-holdout-v1\n" + source[:end] + ".0" + source[end:], nil
		default:
			index++
		}
	}
	return "", fmt.Errorf("holdout parity source: no numeric literal")
}

func seedNumericLiterals(source string) (string, error) {
	for _, declaration := range []string{
		"local total = 0",
		"local score = 0",
		"local removed = 0",
		"local a = 0",
		"local cash = 0",
		"local misses = 0",
	} {
		if index := strings.Index(source, declaration); index >= 0 {
			value := index + len(declaration) - 1
			return source[:value] + "(__seed % 3)" + source[value+1:], nil
		}
	}
	var output strings.Builder
	output.Grow(len(source) + len(source)/2)
	for index := 0; index < len(source); {
		switch {
		case source[index] == '"' || source[index] == '\'':
			next, err := copyQuoted(&output, source, index, source[index])
			if err != nil {
				return "", err
			}
			index = next
		case index+1 < len(source) && source[index:index+2] == "--":
			next := strings.IndexByte(source[index:], '\n')
			if next < 0 {
				output.WriteString(source[index:])
				index = len(source)
				continue
			}
			next += index + 1
			output.WriteString(source[index:next])
			index = next
		case index+1 < len(source) && source[index:index+2] == "[[":
			end := strings.Index(source[index+2:], "]]")
			if end < 0 {
				return "", fmt.Errorf("seed parity source: unterminated long string")
			}
			end += index + 4
			output.WriteString(source[index:end])
			index = end
		case source[index] >= '0' && source[index] <= '9' && numberBoundary(source, index):
			end := index + 1
			for end < len(source) && source[end] >= '0' && source[end] <= '9' {
				end++
			}
			if end < len(source) && (source[end] == '.' || identifierByte(source[end])) {
				return "", fmt.Errorf("seed parity source: unsupported numeric literal near %q", source[index:])
			}
			fmt.Fprintf(&output, "(%s + (__seed %% 3))", source[index:end])
			output.WriteString(source[end:])
			return output.String(), nil
		default:
			output.WriteByte(source[index])
			index++
		}
	}
	return "", fmt.Errorf("seed parity source: no numeric literals")
}

func copyQuoted(output *strings.Builder, source string, start int, quote byte) (int, error) {
	for index := start; index < len(source); index++ {
		output.WriteByte(source[index])
		if source[index] == '\\' {
			index++
			if index >= len(source) {
				return 0, fmt.Errorf("seed parity source: unterminated escape")
			}
			output.WriteByte(source[index])
			continue
		}
		if index > start && source[index] == quote {
			return index + 1, nil
		}
	}
	return 0, fmt.Errorf("seed parity source: unterminated quoted string")
}

func numberBoundary(source string, index int) bool {
	return index == 0 || !identifierByte(source[index-1])
}

func identifierByte(value byte) bool {
	return value == '_' ||
		value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9'
}
