package appliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirstbootServiceTemplate(t *testing.T) {
	data, err := os.ReadFile("../../ghost-firstboot.service.template")
	if err != nil {
		t.Fatalf("failed to read firstboot service template: %v", err)
	}
	content := string(data)

	// Verify Type=oneshot (required for proper sequencing with ghost.service)
	if !strings.Contains(content, "Type=oneshot") {
		t.Error("firstboot service should be Type=oneshot")
	}

	// Verify RemainAfterExit=yes (keeps service marked as active after exit)
	if !strings.Contains(content, "RemainAfterExit=yes") {
		t.Error("firstboot service should have RemainAfterExit=yes")
	}

	// Verify Before=ghost.service (ensures firstboot completes before ghost starts)
	if !strings.Contains(content, "Before=ghost.service") {
		t.Error("firstboot service should have Before=ghost.service")
	}

	// Verify ConditionPathExists (only runs when setup is not complete)
	if !strings.Contains(content, "ConditionPathExists=") {
		t.Error("firstboot service should have ConditionPathExists")
	}

	// Verify -wait flag is used (blocks until setup completes)
	if !strings.Contains(content, "-wait") {
		t.Error("firstboot service should use -wait flag for oneshot lifecycle")
	}

	// Verify WantedBy=multi-user.target
	if !strings.Contains(content, "WantedBy=multi-user.target") {
		t.Error("firstboot service should be wanted by multi-user.target")
	}
}

func TestGhostServiceTemplate(t *testing.T) {
	data, err := os.ReadFile("../../ghost.service.template")
	if err != nil {
		t.Fatalf("failed to read ghost service template: %v", err)
	}
	content := string(data)

	// Verify ConditionPathExists (only starts after setup is complete)
	if !strings.Contains(content, "ConditionPathExists=") {
		t.Error("ghost service should have ConditionPathExists")
	}

	// Verify After includes ghost-firstboot.service (waits for firstboot)
	if !strings.Contains(content, "ghost-firstboot.service") {
		t.Error("ghost service should depend on ghost-firstboot.service")
	}

	// Verify Restart=always
	if !strings.Contains(content, "Restart=always") {
		t.Error("ghost service should have Restart=always")
	}

	// Verify EnvironmentFile
	if !strings.Contains(content, "EnvironmentFile=") {
		t.Error("ghost service should have EnvironmentFile")
	}

	// Verify WantedBy=multi-user.target
	if !strings.Contains(content, "WantedBy=multi-user.target") {
		t.Error("ghost service should be wanted by multi-user.target")
	}
}

func TestServiceTemplatesHaveUser(t *testing.T) {
	tests := []struct {
		file     string
		hasUser  bool
	}{
		{"../../ghost-firstboot.service.template", true},
		{"../../ghost.service.template", true},
	}

	for _, tt := range tests {
		data, err := os.ReadFile(tt.file)
		if err != nil {
			t.Fatalf("failed to read %s: %v", tt.file, err)
		}
		content := string(data)

		if tt.hasUser && !strings.Contains(content, "User=") {
			t.Errorf("%s should have User= directive", filepath.Base(tt.file))
		}
	}
}
