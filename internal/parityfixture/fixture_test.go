package parityfixture

import "testing"

func TestBuildGuestBatchSeedsAndWrapsOneProgram(t *testing.T) {
	fixture, err := BuildGuestBatch("local total = 0\nreturn total + 7", GuestBatchVariant{
		CaseName:  "__case",
		BatchName: "__batch",
	})
	if err != nil {
		t.Fatal(err)
	}
	const wantSeeded = "local total = (__seed % 3)\nreturn total + 7"
	if fixture.SeededSource != wantSeeded {
		t.Fatalf("seeded source = %q, want %q", fixture.SeededSource, wantSeeded)
	}
	const wantProgram = `local __case = function(__seed)
local total = (__seed % 3)
return total + 7
end
local __batch = function(__n, __seed)
    local __checksum = 0
    for __i = 1, __n do
        local __value = __case(__seed + __i)
        __checksum = __checksum + __value * (__i % 7 + 1)
    end
    return __checksum
end
`
	if fixture.Program != wantProgram {
		t.Fatalf("program = %q, want %q", fixture.Program, wantProgram)
	}
}

func TestBuildGuestBatchSeedsFirstExecutableInteger(t *testing.T) {
	fixture, err := BuildGuestBatch(
		"-- 20\nlocal label = \"30\"\nlocal value = 10\nreturn value + 2",
		GuestBatchVariant{CaseName: "caseFn", BatchName: "batchFn"},
	)
	if err != nil {
		t.Fatal(err)
	}
	const want = "-- 20\nlocal label = \"30\"\nlocal value = (10 + (__seed % 3))\nreturn value + 2"
	if fixture.SeededSource != want {
		t.Fatalf("seeded source = %q, want %q", fixture.SeededSource, want)
	}
}

func TestBuildGuestBatchTransformsHoldoutIdentity(t *testing.T) {
	fixture, err := BuildGuestBatch("return 1 + 2", GuestBatchVariant{
		CaseName:  "__holdout_case",
		BatchName: "__holdout_batch",
		Holdout:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = "-- identity-holdout-v1\nreturn (1.0 + (__seed % 3)) + 2"
	if fixture.SeededSource != want {
		t.Fatalf("holdout source = %q, want %q", fixture.SeededSource, want)
	}
}
