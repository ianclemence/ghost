package appliance

import "testing"

func fp(dir, name string) (string, bool) { return dir + "/" + name, true }

func TestFindGhostBinariesOrderAndDedup(t *testing.T) {
	path := "/home/u/.local/bin:/usr/local/bin:/usr/bin:/usr/local/bin"
	bins := FindGhostBinaries(path, fp)
	if len(bins) != 3 {
		t.Fatalf("want 3 unique dirs yielding a binary, got %d: %+v", len(bins), bins)
	}
	if bins[0].Path != "/home/u/.local/bin/ghost" || bins[0].Index != 0 {
		t.Errorf("first must be the PATH winner, got %+v", bins[0])
	}
}

func TestFindGhostBinariesSkipsMissing(t *testing.T) {
	path := "/a:/b"
	missing := func(dir, name string) (string, bool) { return "", false }
	if bins := FindGhostBinaries(path, missing); len(bins) != 0 {
		t.Fatalf("missing dirs must yield nothing, got %+v", bins)
	}
}

func TestDetectBinaryShadowWarnsOnOlderWinner(t *testing.T) {
	bins := []BinaryPath{{Path: "/home/u/.local/bin/ghost", Index: 0}, {Path: "/usr/local/bin/ghost", Index: 1}}
	versions := map[string]string{
		"/home/u/.local/bin/ghost": "v0.23.20",
		"/usr/local/bin/ghost":     "v0.23.26",
	}
	sh, ok := DetectBinaryShadow(bins, func(p string) string { return versions[p] })
	if !ok {
		t.Fatal("an older winner must be reported")
	}
	if sh.Wins != "/home/u/.local/bin/ghost" || sh.Shadows != "/usr/local/bin/ghost" {
		t.Errorf("shadow paths wrong: %+v", sh)
	}
	if sh.WinsVersion != "v0.23.20" || sh.ShadowsVersion != "v0.23.26" {
		t.Errorf("versions wrong: %+v", sh)
	}
}

func TestDetectBinaryShadowSilentWhenSameVersion(t *testing.T) {
	bins := []BinaryPath{{Path: "/a/ghost"}, {Path: "/b/ghost"}}
	same := func(string) string { return "v1.0.0" }
	if _, ok := DetectBinaryShadow(bins, same); ok {
		t.Error("identical versions are harmless duplicates, not a shadow")
	}
}

func TestDetectBinaryShadowSilentWhenSingle(t *testing.T) {
	bins := []BinaryPath{{Path: "/a/ghost"}}
	if _, ok := DetectBinaryShadow(bins, func(string) string { return "v1" }); ok {
		t.Error("one binary cannot shadow anything")
	}
}

func TestDetectBinaryShadowIgnoresUnknownVersions(t *testing.T) {
	bins := []BinaryPath{{Path: "/a/ghost"}, {Path: "/b/ghost"}}
	// Winner version unknown: we cannot honestly claim a shadow.
	unknown := func(string) string { return "" }
	if _, ok := DetectBinaryShadow(bins, unknown); ok {
		t.Error("unknown versions must not produce a warning")
	}
}
