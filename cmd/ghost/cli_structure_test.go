package main

import "testing"

// wantsHelp must recognise help requests and nothing else, and only as the
// first argument (so `ghost skills list --help` is not treated as group help).
func TestWantsHelp(t *testing.T) {
	for _, h := range []string{"--help", "-h", "help"} {
		if !wantsHelp([]string{h}) {
			t.Errorf("wantsHelp(%q) = false, want true", h)
		}
	}
	for _, n := range [][]string{{}, {"list"}, {"--json"}} {
		if wantsHelp(n) {
			t.Errorf("wantsHelp(%v) = true, want false", n)
		}
	}
	if wantsHelp([]string{"list", "--help"}) {
		t.Errorf("a --help after a subcommand must not be treated as group help")
	}
}

// Every known top-level command must resolve a help printer without panicking.
// This guards the single place `ghost <cmd> --help` lands.
func TestPrintCommandHelpCoversAllCommands(t *testing.T) {
	commands := []string{
		"agent", "serve", "gateway", "dev", "dashboard", "onboard",
		"update", "auto-update", "updater", "reset", "verify",
		"relay", "auth", "mcp", "state", "skills", "connector",
		"speech", "eval", "stt", "tts", "version", "help",
	}
	for _, c := range commands {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("printCommandHelp(%q) panicked: %v", c, r)
				}
			}()
			printCommandHelp(c)
		}()
	}
}

// The OpenClaw migration command was removed; assert the name is no longer
// special-cased as an appliance ops command.
func TestMigrateNoLongerAnOpsCommand(t *testing.T) {
	if isApplianceOpsCommand("migrate") {
		t.Errorf("migrate must no longer be a recognised ops command")
	}
}
