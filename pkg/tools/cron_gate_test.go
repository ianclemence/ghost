package tools

import "testing"

func TestCheckCronCommand_AllowsSafeCommands(t *testing.T) {
	safe := []string{
		"echo hello",
		"ls -la /var/ghost",
		"df -h",
		"free",
		"cat /proc/cpuinfo",
		"uptime",
		"curl -s https://example.com",
	}
	for _, cmd := range safe {
		if err := CheckCronCommand(cmd); err != nil {
			t.Errorf("safe command %q should be allowed, got: %v", cmd, err)
		}
	}
}

func TestCheckCronCommand_BlocksDestructive(t *testing.T) {
	blocked := []string{
		"rm -rf /var/ghost",
		"rm -r /home",
		"sudo rm -rf /",
		"mkfs.ext4 /dev/sda1",
		"dd if=/dev/zero of=/dev/sda",
		"shutdown now",
		"reboot",
		"poweroff",
		"rm /var/ghost/data/admin.hash",
		"cat /var/ghost/data/.secrets.json",
	}
	for _, cmd := range blocked {
		if err := CheckCronCommand(cmd); err == nil {
			t.Errorf("destructive command %q should be blocked", cmd)
		}
	}
}
