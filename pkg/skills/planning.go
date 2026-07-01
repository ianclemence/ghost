package skills

import (
	"fmt"
	"strings"
)

type PlanningSkill struct{}

type Plan struct {
	Title       string
	Description string
	Tasks       []PlanTask
	Files       []string
	Notes       []string
}

type PlanTask struct {
	ID          string
	Description string
	Status      string
	Files       []string
	Notes       string
}

func NewPlanningSkill() *PlanningSkill {
	return &PlanningSkill{}
}

func (s *PlanningSkill) Name() string {
	return "planning"
}

func (s *PlanningSkill) Description() string {
	return "Create structured plans with actionable tasks, file paths, and implementation details."
}

func (s *PlanningSkill) GeneratePlan(title, description string, tasks []PlanTask) *Plan {
	plan := &Plan{
		Title:       title,
		Description: description,
		Tasks:       make([]PlanTask, len(tasks)),
	}

	for i, task := range tasks {
		plan.Tasks[i] = PlanTask{
			ID:          fmt.Sprintf("task-%d", i+1),
			Description: task.Description,
			Status:      "pending",
			Files:       task.Files,
			Notes:       task.Notes,
		}
	}

	return plan
}

func (s *PlanningSkill) FormatAsMarkdown(plan *Plan) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# %s\n\n", plan.Title))
	if plan.Description != "" {
		sb.WriteString(fmt.Sprintf("%s\n\n", plan.Description))
	}

	sb.WriteString("## Tasks\n\n")
	for _, task := range plan.Tasks {
		status := "[ ]"
		if task.Status == "completed" {
			status = "[x]"
		} else if task.Status == "in_progress" {
			status = "[~]"
		}

		sb.WriteString(fmt.Sprintf("%s %s: %s\n", status, task.ID, task.Description))

		if len(task.Files) > 0 {
			sb.WriteString(fmt.Sprintf("    Files: %s\n", strings.Join(task.Files, ", ")))
		}
		if task.Notes != "" {
			sb.WriteString(fmt.Sprintf("    Notes: %s\n", task.Notes))
		}
		sb.WriteString("\n")
	}

	if len(plan.Files) > 0 {
		sb.WriteString("## Files\n\n")
		for _, f := range plan.Files {
			sb.WriteString(fmt.Sprintf("- %s\n", f))
		}
		sb.WriteString("\n")
	}

	if len(plan.Notes) > 0 {
		sb.WriteString("## Notes\n\n")
		for _, note := range plan.Notes {
			sb.WriteString(fmt.Sprintf("- %s\n", note))
		}
	}

	return sb.String()
}

func (s *PlanningSkill) UpdateTaskStatus(plan *Plan, taskID, status string) bool {
	for i, task := range plan.Tasks {
		if task.ID == taskID {
			plan.Tasks[i].Status = status
			return true
		}
	}
	return false
}

func (s *PlanningSkill) GetProgress(plan *Plan) (completed, total int) {
	total = len(plan.Tasks)
	for _, task := range plan.Tasks {
		if task.Status == "completed" {
			completed++
		}
	}
	return
}

func (s *PlanningSkill) ValidatePlan(plan *Plan) []string {
	var issues []string

	if plan.Title == "" {
		issues = append(issues, "Plan title is required")
	}

	if len(plan.Tasks) == 0 {
		issues = append(issues, "Plan must have at least one task")
	}

	for _, task := range plan.Tasks {
		if task.Description == "" {
			issues = append(issues, fmt.Sprintf("Task %s has no description", task.ID))
		}
	}

	return issues
}
