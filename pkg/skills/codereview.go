package skills

import (
	"fmt"
	"strings"
)

type CodeReviewSkill struct{}

type Review struct {
	FilePath    string
	Issues      []ReviewIssue
	Summary     string
	Rating      string
}

type ReviewIssue struct {
	Line        int
	Severity    string
	Category    string
	Description string
	Suggestion  string
}

func NewCodeReviewSkill() *CodeReviewSkill {
	return &CodeReviewSkill{}
}

func (s *CodeReviewSkill) Name() string {
	return "code_review"
}

func (s *CodeReviewSkill) Description() string {
	return "Review code for issues, suggest improvements, and provide structured feedback."
}

func (s *CodeReviewSkill) StartReview(filePath string) *Review {
	return &Review{
		FilePath: filePath,
		Issues:   []ReviewIssue{},
	}
}

func (s *CodeReviewSkill) AddIssue(review *Review, line int, severity, category, description, suggestion string) {
	review.Issues = append(review.Issues, ReviewIssue{
		Line:        line,
		Severity:    severity,
		Category:    category,
		Description: description,
		Suggestion:  suggestion,
	})
}

func (s *CodeReviewSkill) SetSummary(review *Review, summary string) {
	review.Summary = summary
}

func (s *CodeReviewSkill) CalculateRating(review *Review) string {
	critical := 0
	major := 0
	minor := 0

	for _, issue := range review.Issues {
		switch issue.Severity {
		case "critical":
			critical++
		case "major":
			major++
		case "minor":
			minor++
		}
	}

	if critical > 0 {
		review.Rating = "needs_work"
	} else if major > 2 {
		review.Rating = "needs_work"
	} else if major > 0 || minor > 3 {
		review.Rating = "acceptable"
	} else {
		review.Rating = "good"
	}

	return review.Rating
}

func (s *CodeReviewSkill) FormatAsMarkdown(review *Review) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Code Review: %s\n\n", review.FilePath))

	if review.Summary != "" {
		sb.WriteString(fmt.Sprintf("## Summary\n%s\n\n", review.Summary))
	}

	if review.Rating != "" {
		ratingEmoji := "✅"
		if review.Rating == "needs_work" {
			ratingEmoji = "❌"
		} else if review.Rating == "acceptable" {
			ratingEmoji = "⚠️"
		}
		sb.WriteString(fmt.Sprintf("## Rating: %s %s\n\n", ratingEmoji, review.Rating))
	}

	if len(review.Issues) > 0 {
		sb.WriteString("## Issues\n\n")

		critical := filterIssues(review.Issues, "critical")
		major := filterIssues(review.Issues, "major")
		minor := filterIssues(review.Issues, "minor")

		if len(critical) > 0 {
			sb.WriteString("### Critical\n\n")
			for _, issue := range critical {
				sb.WriteString(formatIssue(issue))
			}
		}

		if len(major) > 0 {
			sb.WriteString("### Major\n\n")
			for _, issue := range major {
				sb.WriteString(formatIssue(issue))
			}
		}

		if len(minor) > 0 {
			sb.WriteString("### Minor\n\n")
			for _, issue := range minor {
				sb.WriteString(formatIssue(issue))
			}
		}
	} else {
		sb.WriteString("## No Issues Found\n\nGreat job! No issues were detected.\n")
	}

	return sb.String()
}

func (s *CodeReviewSkill) GetIssuesByCategory(review *Review, category string) []ReviewIssue {
	var result []ReviewIssue
	for _, issue := range review.Issues {
		if issue.Category == category {
			result = append(result, issue)
		}
	}
	return result
}

func (s *CodeReviewSkill) GetIssueCount(review *Review) int {
	return len(review.Issues)
}

func (s *CodeReviewSkill) ValidateReview(review *Review) []string {
	var issues []string

	if review.FilePath == "" {
		issues = append(issues, "File path is required")
	}

	for _, issue := range review.Issues {
		if issue.Description == "" {
			issues = append(issues, fmt.Sprintf("Issue at line %d has no description", issue.Line))
		}
		if issue.Severity == "" {
			issues = append(issues, fmt.Sprintf("Issue at line %d has no severity", issue.Line))
		}
	}

	return issues
}

func filterIssues(issues []ReviewIssue, severity string) []ReviewIssue {
	var result []ReviewIssue
	for _, issue := range issues {
		if issue.Severity == severity {
			result = append(result, issue)
		}
	}
	return result
}

func formatIssue(issue ReviewIssue) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("- **Line %d** [%s] %s\n", issue.Line, issue.Category, issue.Description))
	if issue.Suggestion != "" {
		sb.WriteString(fmt.Sprintf("  Suggestion: %s\n", issue.Suggestion))
	}
	return sb.String()
}
