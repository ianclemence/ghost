package agent

import (
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/skills"
	"github.com/ianclemence/ghost/pkg/tools"
)

func TestASkillWithNoDeclaredPathKeepsTheModelsTools(t *testing.T) {
	reg := tools.NewToolRegistry()
	reg.Register(tools.NewThinkTool())
	reg.Register(tools.NewSelfCheckTool(nil))

	// A skill the registry knows nothing about: unrestricted.
	restrictToCapability(reg, tools.ProfileFull, skills.GetCapability("some-user-skill"))
	for _, n := range []string{"think", "self_check"} {
		if _, ok := reg.Get(n); !ok {
			t.Errorf("%s was removed by reading an unregistered skill", n)
		}
	}

	// A skill with a closed path is still narrowed to it (read_file always stays).
	restrictToCapability(reg, tools.ProfileFull, skills.Capability{AllowedTools: []string{"self_check"}})
	if _, ok := reg.Get("think"); ok {
		t.Error("a declared path must still narrow the tools")
	}
	if _, ok := reg.Get("self_check"); !ok {
		t.Error("the declared tool must stay")
	}
}

func TestFilmWorkRunsWithoutAnApprovalCard(t *testing.T) {
	cap := skills.GetCapability("hand-drawn-animation")
	if cap.ID != "film.make" {
		t.Fatalf("the skill must declare its capability, got %q", cap.ID)
	}
	for _, tool := range []string{"film", "write_file", "edit_file", "vision"} {
		if !cap.Allows(tool) {
			t.Errorf("%s must be on the skill's path", tool)
		}
		if r := authorizedToolRisk(cap.ID, tool); r != permissions.RiskReadOnly {
			t.Errorf("%s inside the film skill is %s: the owner would be asked to approve drawing", tool, r)
		}
	}
	for _, tool := range []string{"exec", "email_send", "message", "browser_navigate"} {
		if cap.Allows(tool) {
			t.Errorf("%s must stay off the film skill's path", tool)
		}
	}
}

func TestAToolsPicturesReachTheModelAsAnImageMessage(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n" + "0123456789abcdef")
	msg, ok := resultImagesMessage([]tools.ResultImage{{Data: png, Caption: "Contact sheet"}, {Data: nil}})
	if !ok || msg.Role != "user" || len(msg.MultiContent) != 2 {
		t.Fatalf("got %+v ok=%v", msg, ok)
	}
	if msg.MultiContent[1].ImageURL == nil || !strings.HasPrefix(msg.MultiContent[1].ImageURL.URL, "data:image/png;base64,") {
		t.Errorf("the picture must go as a data URL with its real type: %+v", msg.MultiContent[1])
	}
	if _, ok := resultImagesMessage(nil); ok {
		t.Error("no pictures, no message")
	}
	big := make([]byte, maxScreenshotAttach+1)
	if _, ok := resultImagesMessage([]tools.ResultImage{{Data: big}}); ok {
		t.Error("an oversized picture must be left out, not attached")
	}
}
