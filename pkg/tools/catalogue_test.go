package tools

import (
	"strings"
	"testing"
)

func TestCatalogueNamesWhatIsNotAlwaysOfferedAndSkipsCore(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(described{"weather_now", "Current weather."})
	reg.Register(described{"device", "Read or control smart-home devices. More text after the first sentence."})
	reg.Register(described{"browser_click", "Click"})
	reg.Register(described{"mcp_x_y", "External"})
	reg.RegisterHidden(described{"exec", "Run a shell command"}, 0)
	got := OnDemandCatalogue(reg, nil, ProfileFull)
	for _, want := range []string{"device (Read or control smart-home devices)", "browser_*", "mcp_*", "find_tools", "exec (run commands: clone and build code repositories"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	for _, not := range []string{"weather_now", "More text"} {
		if strings.Contains(got, not) {
			t.Errorf("%q must not appear: %q", not, got)
		}
	}
}

func TestCatalogueIsStableAcrossCalls(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(described{"device", "Control devices."})
	reg.Register(described{"media_play", "Play music."})
	if OnDemandCatalogue(reg, nil, ProfileFull) != OnDemandCatalogue(reg, nil, ProfileFull) {
		t.Fatal("the catalogue must not change between turns, or the prompt cache breaks")
	}
}

func TestEmptyRegistryHasNoCatalogue(t *testing.T) {
	if OnDemandCatalogue(NewToolRegistry(), nil, ProfileFull) != "" || OnDemandCatalogue(nil, nil, ProfileFull) != "" {
		t.Fatal("nothing to list")
	}
}

func TestCatalogueLeavesOutWhatThisTurnWasAlreadyHanded(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(described{"device", "Control devices."})
	reg.Register(described{"media_play", "Play music."})
	offered := NewToolRegistry()
	d, _ := reg.Get("device")
	offered.Register(d)
	got := OnDemandCatalogue(reg, offered, ProfileFull)
	if strings.Contains(got, "device") || !strings.Contains(got, "media_play") {
		t.Fatalf("a tool already in the list must not be offered again: %q", got)
	}
}
