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

	// Verify Type=simple (always-on wizard, not a oneshot)
	if !strings.Contains(content, "Type=simple") {
		t.Error("firstboot service should be Type=simple")
	}

	// Verify Restart=always (keeps the wizard always available)
	if !strings.Contains(content, "Restart=always") {
		t.Error("firstboot service should have Restart=always")
	}

	// Verify Before=ghost.service (ensures firstboot starts before ghost)
	if !strings.Contains(content, "Before=ghost.service") {
		t.Error("firstboot service should have Before=ghost.service")
	}

	// Verify -force flag is used (wizard stays available after setup)
	if !strings.Contains(content, "-force") {
		t.Error("firstboot service should use -force flag to stay available after setup")
	}

	// Verify no ConditionPathExists (wizard must run even when setup is complete)
	if strings.Contains(content, "ConditionPathExists=") {
		t.Error("firstboot service should not have ConditionPathExists")
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
