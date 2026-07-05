package ember

import "testing"

func moduleSummaryExport(t *testing.T, exports []ModuleExport, name string, kind ModuleExportKind) ModuleExport {
	t.Helper()
	for _, item := range exports {
		if item.Name == name && item.Kind == kind {
			return item
		}
	}
	t.Fatalf("Summary exports are %#v, want %s %s export", exports, kind, name)
	return ModuleExport{}
}

func TestSourceIdentityIncludesNameAndTextHash(t *testing.T) {
	first := identifyModuleSource(Source{Name: "game/root", Text: "return 1"})
	same := identifyModuleSource(Source{Name: "game/root", Text: "return 1"})
	renamed := identifyModuleSource(Source{Name: "game/other", Text: "return 1"})
	changed := identifyModuleSource(Source{Name: "game/root", Text: "return 2"})

	if first != same {
		t.Fatalf("same source identity differs: %#v != %#v", first, same)
	}
	if first == renamed {
		t.Fatalf("renamed source kept identity %#v", first)
	}
	if first == changed {
		t.Fatalf("changed source text kept identity %#v", first)
	}
}

func TestNormalizeRequireKeyHandlesRelativeLogicalAndHostRequests(t *testing.T) {
	from, err := parseModuleKey("game/server/init")
	if err != nil {
		t.Fatalf("parseModuleKey returned error: %v", err)
	}

	tests := []struct {
		name    string
		request string
		want    string
	}{
		{name: "sibling", request: "./inventory", want: "logical:game/server/inventory"},
		{name: "parent", request: "../shared/math", want: "logical:game/shared/math"},
		{name: "logical", request: "ReplicatedStorage/packages/net", want: "logical:ReplicatedStorage/packages/net"},
		{name: "host", request: "host:clock", want: "host:clock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeRequireKey(from, tt.request)
			if err != nil {
				t.Fatalf("normalizeRequireKey returned error: %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("normalized key is %q, want %q", got.String(), tt.want)
			}
		})
	}
}

func TestNormalizeRequireKeyRejectsEscapingRelativeRequest(t *testing.T) {
	from, err := parseModuleKey("root")
	if err != nil {
		t.Fatalf("parseModuleKey returned error: %v", err)
	}

	_, err = normalizeRequireKey(from, "../outside")
	if err == nil {
		t.Fatal("normalizeRequireKey succeeded, want escape error")
	}
}
