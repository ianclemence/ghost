// Package presentation provides human-readable formatting for Ghost's
// internal state. It converts structured data into natural, user-facing
// representations that hide implementation details.
package presentation

import (
	"fmt"
	"strings"
	"time"
)

// Memory formats a memory entry for human display.
func Memory(title, kind, domain, summary string, confidence float64, learnedAt time.Time) string {
	var sb strings.Builder
	
	// Title (primary)
	if title != "" {
		sb.WriteString(title)
	} else {
		sb.WriteString("Unknown memory")
	}
	
	// Domain badge
	if domain != "" && domain != "other" {
		sb.WriteString(fmt.Sprintf(" [%s]", strings.Title(domain)))
	}
	
	// Summary (if different from title)
	if summary != "" && summary != title {
		sb.WriteString("\n")
		sb.WriteString(summary)
	}
	
	// Metadata
	sb.WriteString(fmt.Sprintf("\nLearned %s", TimeAgo(learnedAt)))
	if confidence < 0.9 {
		sb.WriteString(fmt.Sprintf(" (%.0f%% confident)", confidence*100))
	}
	
	return sb.String()
}

// Session formats a session/conversation for human display.
func Session(title string, messageCount int, lastActivity time.Time, summary string) string {
	var sb strings.Builder
	
	// Title (primary)
	if title != "" {
		sb.WriteString(title)
	} else {
		sb.WriteString("New conversation")
	}
	
	// Message count
	if messageCount > 0 {
		sb.WriteString(fmt.Sprintf("\n%d messages", messageCount))
	}
	
	// Last activity
	if !lastActivity.IsZero() {
		sb.WriteString(fmt.Sprintf("\nLast active %s", TimeAgo(lastActivity)))
	}
	
	// Summary (if available)
	if summary != "" {
		sb.WriteString("\n\n")
		sb.WriteString(summary)
	}
	
	return sb.String()
}

// Activity formats an activity event for human display.
func Activity(eventType, detail string, timestamp time.Time) string {
	var sb strings.Builder
	
	// Event description
	switch eventType {
	case "web_search":
		sb.WriteString("Searched the web")
	case "memory_write":
		sb.WriteString("Remembered something")
	case "memory_forget":
		sb.WriteString("Forgot something")
	case "skill_run":
		sb.WriteString("Used a skill")
	case "automation_run":
		sb.WriteString("Ran an automation")
	case "device_connected":
		sb.WriteString("Device connected")
	case "device_disconnected":
		sb.WriteString("Device disconnected")
	case "model_switch":
		sb.WriteString("Switched AI model")
	case "error":
		sb.WriteString("Something went wrong")
	default:
		sb.WriteString(strings.ReplaceAll(eventType, "_", " "))
	}
	
	// Detail
	if detail != "" {
		sb.WriteString(": ")
		sb.WriteString(detail)
	}
	
	// Timestamp
	if !timestamp.IsZero() {
		sb.WriteString(fmt.Sprintf("\n%s", TimeAgo(timestamp)))
	}
	
	return sb.String()
}

// Automation formats an automation/cron job for human display.
func Automation(name, schedule string, lastRun time.Time, success bool) string {
	var sb strings.Builder
	
	// Name
	if name != "" {
		sb.WriteString(name)
	} else {
		sb.WriteString("Unnamed automation")
	}
	
	// Schedule (human-readable)
	if schedule != "" {
		humanSchedule := FormatSchedule(schedule)
		sb.WriteString(fmt.Sprintf("\n%s", humanSchedule))
	}
	
	// Last run status
	if !lastRun.IsZero() {
		status := "completed"
		if !success {
			status = "failed"
		}
		sb.WriteString(fmt.Sprintf("\nLast run %s (%s)", TimeAgo(lastRun), status))
	}
	
	return sb.String()
}

// Skill formats a skill for human display.
func Skill(name, description string, enabled bool) string {
	var sb strings.Builder
	
	// Name
	if name != "" {
		sb.WriteString(name)
	} else {
		sb.WriteString("Unnamed skill")
	}
	
	// Status
	if !enabled {
		sb.WriteString(" [disabled]")
	}
	
	// Description
	if description != "" {
		sb.WriteString("\n")
		if len(description) > 100 {
			sb.WriteString(description[:97] + "...")
		} else {
			sb.WriteString(description)
		}
	}
	
	return sb.String()
}

// Device formats a device for human display.
func Device(name, status string, lastSeen time.Time) string {
	var sb strings.Builder
	
	// Name
	if name != "" {
		sb.WriteString(name)
	} else {
		sb.WriteString("Unknown device")
	}
	
	// Status
	switch status {
	case "connected":
		sb.WriteString("\nConnected")
	case "disconnected":
		sb.WriteString("\nDisconnected")
	case "needs_attention":
		sb.WriteString("\nNeeds attention")
	default:
		sb.WriteString(fmt.Sprintf("\nStatus: %s", status))
	}
	
	// Last seen
	if !lastSeen.IsZero() {
		sb.WriteString(fmt.Sprintf("\nLast seen %s", TimeAgo(lastSeen)))
	}
	
	return sb.String()
}

// Channel formats a channel for human display.
func Channel(name, status string) string {
	var sb strings.Builder
	
	// Name
	if name != "" {
		sb.WriteString(name)
	} else {
		sb.WriteString("Unknown channel")
	}
	
	// Status
	switch status {
	case "connected":
		sb.WriteString(" — Connected")
	case "disconnected":
		sb.WriteString(" — Not configured")
	case "error":
		sb.WriteString(" — Error")
	default:
		sb.WriteString(fmt.Sprintf(" — %s", status))
	}
	
	return sb.String()
}

// System formats system status for human display.
func System(healthy bool, details map[string]string) string {
	var sb strings.Builder
	
	if healthy {
		sb.WriteString("Everything is running normally.")
	} else {
		sb.WriteString("System needs attention.")
	}
	
	// Details (if any)
	if len(details) > 0 {
		sb.WriteString("\n\nDetails:")
		for key, value := range details {
			sb.WriteString(fmt.Sprintf("\n- %s: %s", strings.Title(key), value))
		}
	}
	
	return sb.String()
}

// Error formats an error for human display.
func Error(errType, message string, technicalDetails string) string {
	var sb strings.Builder
	
	// User-friendly message
	switch errType {
	case "model_unavailable":
		sb.WriteString("The cloud AI service is temporarily unavailable. Ghost is using its local model instead.")
	case "network_error":
		sb.WriteString("Ghost couldn't connect to the network. Your request was not completed.")
	case "timeout":
		sb.WriteString("The request took too long. Please try again.")
	case "permission_denied":
		sb.WriteString("Ghost doesn't have permission to do that.")
	case "not_found":
		sb.WriteString("Ghost couldn't find what you're looking for.")
	case "internal_error":
		sb.WriteString("Something went wrong inside Ghost. Your request was not completed.")
	default:
		if message != "" {
			sb.WriteString(message)
		} else {
			sb.WriteString("Something went wrong.")
		}
	}
	
	// Technical details (for interested users)
	if technicalDetails != "" {
		sb.WriteString("\n\nTechnical details:\n")
		sb.WriteString(technicalDetails)
	}
	
	return sb.String()
}

// TimeAgo formats a timestamp as a human-readable relative time.
func TimeAgo(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	
	now := time.Now()
	diff := now.Sub(t)
	
	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		minutes := int(diff.Minutes())
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "yesterday"
		}
		return fmt.Sprintf("%d days ago", days)
	case diff < 30*24*time.Hour:
		weeks := int(diff.Hours() / (24 * 7))
		if weeks == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	default:
		months := int(diff.Hours() / (24 * 30))
		if months == 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", months)
	}
}

// FormatSchedule converts a cron expression to human-readable format.
func FormatSchedule(cronExpr string) string {
	// Simple cron formatting (supports basic patterns)
	parts := strings.Fields(cronExpr)
	if len(parts) < 5 {
		return cronExpr
	}
	
	minute := parts[0]
	hour := parts[1]
	dom := parts[2]
	month := parts[3]
	dow := parts[4]
	
	// Handle common patterns
	if cronExpr == "0 8 * * *" {
		return "Every day at 8:00 AM"
	}
	if cronExpr == "0 9 * * 1-5" {
		return "Weekdays at 9:00 AM"
	}
	if cronExpr == "0 0 * * *" {
		return "Every day at midnight"
	}
	if cronExpr == "0 */2 * * *" {
		return "Every 2 hours"
	}
	if cronExpr == "*/15 * * * *" {
		return "Every 15 minutes"
	}
	
	// Build human-readable string
	var parts2 []string
	
	// Frequency
	if dom == "*" && month == "*" {
		if dow == "*" {
			parts2 = append(parts2, "Every day")
		} else if dow == "1-5" {
			parts2 = append(parts2, "Weekdays")
		} else {
			parts2 = append(parts2, "Every week")
		}
	} else {
		parts2 = append(parts2, "Monthly")
	}
	
	// Time
	if hour == "*" && minute == "*" {
		parts2 = append(parts2, "at every hour")
	} else if hour == "*" {
		parts2 = append(parts2, fmt.Sprintf("at minute %s", minute))
	} else if minute == "0" {
		parts2 = append(parts2, fmt.Sprintf("at %s:00", hour))
	} else {
		parts2 = append(parts2, fmt.Sprintf("at %s:%s", hour, minute))
	}
	
	return strings.Join(parts2, " ")
}

// Truncate truncates a string to a maximum length, preserving word boundaries.
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	
	// Find last space before maxLen
	truncated := s[:maxLen-1]
	if i := strings.LastIndex(truncated, " "); i > maxLen/2 {
		truncated = truncated[:i]
	}
	
	return truncated + "..."
}
