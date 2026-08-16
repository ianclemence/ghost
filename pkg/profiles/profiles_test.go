package profiles

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateProfile(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir)

	p, err := manager.Create("test-bot", "A test bot", nil)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if p.Name != "test-bot" {
		t.Errorf("expected name 'test-bot', got %s", p.Name)
	}
	if p.Title != "test-bot" {
		t.Errorf("expected title 'test-bot', got %s", p.Title)
	}
	if p.Group != "" {
		t.Errorf("expected empty group, got %s", p.Group)
	}
}

func TestDuplicateProfile(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir)

	manager.Create("original", "Original bot", nil)
	original, _ := manager.Get("original")
	original.Avatar = &Avatar{Shape: AvatarShapeCircle, Color: "#3b82f6"}
	original.Group = "Research"
	original.Save()

	dupe, err := manager.Duplicate("original", "copy")
	if err != nil {
		t.Fatalf("Duplicate failed: %v", err)
	}

	if dupe.Name != "copy" {
		t.Errorf("expected name 'copy', got %s", dupe.Name)
	}

 copy, _ := manager.Get("copy")
	if copy.Avatar == nil || copy.Avatar.Shape != AvatarShapeCircle {
		t.Error("avatar not copied")
	}
	if copy.Group != "Research" {
		t.Errorf("expected group 'Research', got %s", copy.Group)
	}

	configPath := filepath.Join(tmpDir, "profiles", "copy", "config.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config.json not created for duplicate")
	}
}

func TestUniqueName(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir)

	manager.Create("bot", "", nil)

	name := manager.UniqueName("bot")
	if name != "bot-2" {
		t.Errorf("expected 'bot-2', got %s", name)
	}

	manager.Create("bot-2", "", nil)
	name = manager.UniqueName("bot")
	if name != "bot-3" {
		t.Errorf("expected 'bot-3', got %s", name)
	}
}

func TestSetGroup(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir)

	manager.Create("test-bot", "", nil)

	if err := manager.SetGroup("test-bot", "Ops"); err != nil {
		t.Fatalf("SetGroup failed: %v", err)
	}

	p, _ := manager.Get("test-bot")
	if p.Group != "Ops" {
		t.Errorf("expected group 'Ops', got %s", p.Group)
	}

	manager.SetGroup("test-bot", "")
	p, _ = manager.Get("test-bot")
	if p.Group != "" {
		t.Errorf("expected empty group, got %s", p.Group)
	}
}

func TestGroupRoster(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir)

	manager.Create("bot1", "", nil)
	manager.Create("bot2", "", nil)
	manager.Create("bot3", "", nil)
	manager.SetGroup("bot1", "Research")
	manager.SetGroup("bot2", "Research")
	manager.SetGroup("bot3", "Ops")

	ungrouped, groups, err := manager.GroupRoster()
	if err != nil {
		t.Fatalf("GroupRoster failed: %v", err)
	}

	if len(ungrouped) != 0 {
		t.Errorf("expected 0 ungrouped, got %d", len(ungrouped))
	}
	if len(groups["Research"]) != 2 {
		t.Errorf("expected 2 in Research, got %d", len(groups["Research"]))
	}
	if len(groups["Ops"]) != 1 {
		t.Errorf("expected 1 in Ops, got %d", len(groups["Ops"]))
	}
}

func TestListGroups(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir)

	manager.Create("a", "", nil)
	manager.Create("b", "", nil)
	manager.SetGroup("a", "Alpha")
	manager.SetGroup("b", "Beta")

	groups, err := manager.ListGroups()
	if err != nil {
		t.Fatalf("ListGroups failed: %v", err)
	}

	if len(groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(groups))
	}
	if groups[0] != "Alpha" || groups[1] != "Beta" {
		t.Errorf("unexpected groups: %v", groups)
	}
}

func TestSetAvatar(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir)

	manager.Create("test-bot", "", nil)

	avatar := &Avatar{Shape: AvatarShapeHexagon, Color: "#ec4899"}
	if err := manager.UpdateAvatar("test-bot", avatar); err != nil {
		t.Fatalf("UpdateAvatar failed: %v", err)
	}

	p, _ := manager.Get("test-bot")
	if p.Avatar == nil {
		t.Fatal("avatar is nil")
	}
	if p.Avatar.Shape != AvatarShapeHexagon {
		t.Errorf("expected shape hexagon, got %s", p.Avatar.Shape)
	}
	if p.Avatar.Color != "#ec4899" {
		t.Errorf("expected color #ec4899, got %s", p.Avatar.Color)
	}
}

func TestCreateChannel(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir)

	manager.Create("bot1", "", nil)
	manager.Create("bot2", "", nil)

	ch, err := manager.CreateChannel("General", "Main discussion", "bot1", []string{"bot1", "bot2"})
	if err != nil {
		t.Fatalf("CreateChannel failed: %v", err)
	}

	if ch.Name != "General" {
		t.Errorf("expected name 'General', got %s", ch.Name)
	}
	if ch.ID != "general" {
		t.Errorf("expected id 'general', got %s", ch.ID)
	}
	if len(ch.Members) != 2 {
		t.Errorf("expected 2 members, got %d", len(ch.Members))
	}

	ch1Path := filepath.Join(tmpDir, "profiles", "bot1", "channels", "general.json")
	if _, err := os.Stat(ch1Path); os.IsNotExist(err) {
		t.Error("channel not created for bot1")
	}
	ch2Path := filepath.Join(tmpDir, "profiles", "bot2", "channels", "general.json")
	if _, err := os.Stat(ch2Path); os.IsNotExist(err) {
		t.Error("channel not created for bot2")
	}
}

func TestSendChannelMessage(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir)

	manager.Create("bot1", "", nil)
	manager.Create("bot2", "", nil)
	manager.CreateChannel("Test", "", "bot1", []string{"bot1", "bot2"})

	if err := manager.SendChannelMessage("test", "bot1", "Hello from bot1"); err != nil {
		t.Fatalf("SendChannelMessage failed: %v", err)
	}

	messages, err := manager.ReadChannelHistory("test", 10)
	if err != nil {
		t.Fatalf("ReadChannelHistory failed: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Sender != "bot1" {
		t.Errorf("expected sender 'bot1', got %s", messages[0].Sender)
	}
	if messages[0].Content != "Hello from bot1" {
		t.Errorf("expected content 'Hello from bot1', got %s", messages[0].Content)
	}
}

func TestChannelNonMemberReject(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir)

	manager.Create("bot1", "", nil)
	manager.Create("bot2", "", nil)
	manager.CreateChannel("Private", "", "bot1", []string{"bot1"})

	err := manager.SendChannelMessage("private", "bot2", "Unauthorized")
	if err == nil {
		t.Error("expected error for non-member send")
	}
}

func TestAvatarSVG(t *testing.T) {
	shapes := []string{
		AvatarShapeCircle, AvatarShapeSquircle, AvatarShapePill,
		AvatarShapeTriangle, AvatarShapeHexagon, AvatarShapeCloud, AvatarShapeDrop,
	}

	for _, shape := range shapes {
		svg := &AvatarSVG{Shape: shape, Color: "#3b82f6", Name: "test", Size: 64}
		result := svg.Render()
		if result == "" {
			t.Errorf("empty SVG for shape %s", shape)
		}
		if len(result) < 50 {
			t.Errorf("SVG too short for shape %s: %d bytes", shape, len(result))
		}
	}
}

func TestDefaultAvatar(t *testing.T) {
	avatar1 := DefaultAvatar("bot-a")
	avatar2 := DefaultAvatar("bot-b")

	if avatar1.Shape == "" {
		t.Error("default avatar has empty shape")
	}
	if avatar1.Color == "" {
		t.Error("default avatar has empty color")
	}

	found := false
	for _, s := range AvatarShapes {
		if avatar1.Shape == s {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("invalid default avatar shape: %s", avatar1.Shape)
	}

	_ = avatar2
}
