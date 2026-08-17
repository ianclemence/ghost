package tools

import (
	"fmt"
	"regexp"
	"strings"
)

// CronCommandDenyPatterns are regexes matched against a scheduled command
// before it is executed. Matches are refused, so a cron job that a user or
// the agent created via natural language cannot silently run something
// destructive on the appliance. This is PicoClaw-style "cron security
// gating": scheduled commands are not allowed to touch the system the way an
// interactive, human-approved exec would.
var CronCommandDenyPatterns = []*regexp.Regexp{
	// Wiping or resetting the filesystem.
	regexp.MustCompile(`(?i)\brm\s+(-[^ ]*r|.*-r\s+)`),
	regexp.MustCompile(`(?i)\brm\s+-rf\b`),
	regexp.MustCompile(`(?i)\bmkfs\.?[a-z0-9]*\b`),
	regexp.MustCompile(`(?i)\bdd\s+if=/dev/zero\b`),
	regexp.MustCompile(`(?i)\bwipefs\b`),
	// Shutting down or rebooting the appliance without approval.
	regexp.MustCompile(`(?i)\b(shutdown|reboot|poweroff|halt)\b`),
	// Messing with the admin credential or secrets.
	regexp.MustCompile(`(?i)admin\.hash`),
	regexp.MustCompile(`(?i)\.secrets\.json`),
	regexp.MustCompile(`(?i)\bchmod\s+[0-7]{3,4}\s+.*(?:etc|usr|var|boot)`),
	// Dangerous shell metacharacter chaining that suggests multi-command
	// injection hidden in a scheduled job.
	regexp.MustCompile(`(?i)\bsudo\s+rm\b`),
}

// CheckCronCommand returns an error if a scheduled command is blocked by the
// deny policy. An empty command is allowed (callers guard that already).
func CheckCronCommand(command string) error {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return nil
	}
	for _, re := range CronCommandDenyPatterns {
		if re.MatchString(trimmed) {
			return fmt.Errorf("scheduled command blocked by security policy (matches %q)", re.String())
		}
	}
	return nil
}
