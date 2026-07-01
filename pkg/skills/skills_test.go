package skills

import (
	"testing"
)

func TestPlanningSkillName(t *testing.T) {
	skill := NewPlanningSkill()
	if skill.Name() != "planning" {
		t.Fatalf("expected name 'planning', got %s", skill.Name())
	}
}

func TestPlanningSkillDescription(t *testing.T) {
	skill := NewPlanningSkill()
	if skill.Description() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestPlanningSkillGeneratePlan(t *testing.T) {
	skill := NewPlanningSkill()
	plan := skill.GeneratePlan("Test Plan", "A test plan", []PlanTask{
		{Description: "Task 1"},
		{Description: "Task 2"},
	})

	if plan.Title != "Test Plan" {
		t.Fatalf("expected title 'Test Plan', got %s", plan.Title)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(plan.Tasks))
	}
}

func TestPlanningSkillFormatAsMarkdown(t *testing.T) {
	skill := NewPlanningSkill()
	plan := skill.GeneratePlan("Test Plan", "Description", []PlanTask{
		{Description: "Task 1"},
	})

	md := skill.FormatAsMarkdown(plan)
	if md == "" {
		t.Fatal("expected non-empty markdown")
	}
}

func TestPlanningSkillUpdateTaskStatus(t *testing.T) {
	skill := NewPlanningSkill()
	plan := skill.GeneratePlan("Test", "", []PlanTask{
		{Description: "Task 1"},
	})

	if !skill.UpdateTaskStatus(plan, "task-1", "completed") {
		t.Fatal("expected true for valid task")
	}

	completed, total := skill.GetProgress(plan)
	if completed != 1 || total != 1 {
		t.Fatalf("expected 1/1 progress, got %d/%d", completed, total)
	}
}

func TestPlanningSkillUpdateInvalidTask(t *testing.T) {
	skill := NewPlanningSkill()
	plan := skill.GeneratePlan("Test", "", []PlanTask{
		{Description: "Task 1"},
	})

	if skill.UpdateTaskStatus(plan, "invalid", "completed") {
		t.Fatal("expected false for invalid task")
	}
}

func TestPlanningSkillValidatePlan(t *testing.T) {
	skill := NewPlanningSkill()

	emptyPlan := &Plan{}
	issues := skill.ValidatePlan(emptyPlan)
	if len(issues) == 0 {
		t.Fatal("expected validation issues for empty plan")
	}

	validPlan := skill.GeneratePlan("Title", "", []PlanTask{
		{Description: "Task"},
	})
	issues = skill.ValidatePlan(validPlan)
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %d", len(issues))
	}
}

func TestDebuggingSkillName(t *testing.T) {
	skill := NewDebuggingSkill()
	if skill.Name() != "debugging" {
		t.Fatalf("expected name 'debugging', got %s", skill.Name())
	}
}

func TestDebuggingSkillDescription(t *testing.T) {
	skill := NewDebuggingSkill()
	if skill.Description() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestDebuggingSkillStartDebug(t *testing.T) {
	skill := NewDebuggingSkill()
	session := skill.StartDebug("Test issue")

	if session.Issue != "Test issue" {
		t.Fatalf("expected issue 'Test issue', got %s", session.Issue)
	}
	if session.Phase != "investigation" {
		t.Fatalf("expected phase 'investigation', got %s", session.Phase)
	}
}

func TestDebuggingSkillAddEvidence(t *testing.T) {
	skill := NewDebuggingSkill()
	session := skill.StartDebug("Issue")

	skill.AddEvidence(session, "Evidence 1")
	skill.AddEvidence(session, "Evidence 2")

	if len(session.Evidence) != 2 {
		t.Fatalf("expected 2 evidence, got %d", len(session.Evidence))
	}
}

func TestDebuggingSkillAddHypothesis(t *testing.T) {
	skill := NewDebuggingSkill()
	session := skill.StartDebug("Issue")

	skill.AddHypothesis(session, "Hypothesis 1", 0.8)
	skill.AddHypothesis(session, "Hypothesis 2", 0.6)

	if len(session.Hypotheses) != 2 {
		t.Fatalf("expected 2 hypotheses, got %d", len(session.Hypotheses))
	}

	top := skill.GetTopHypothesis(session)
	if top.Description != "Hypothesis 1" {
		t.Fatalf("expected top hypothesis 'Hypothesis 1', got %s", top.Description)
	}
}

func TestDebuggingSkillUpdatePhase(t *testing.T) {
	skill := NewDebuggingSkill()
	session := skill.StartDebug("Issue")

	if err := skill.UpdatePhase(session, "pattern_analysis"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.Phase != "pattern_analysis" {
		t.Fatalf("expected phase 'pattern_analysis', got %s", session.Phase)
	}
}

func TestDebuggingSkillInvalidPhase(t *testing.T) {
	skill := NewDebuggingSkill()
	session := skill.StartDebug("Issue")

	if err := skill.UpdatePhase(session, "invalid"); err == nil {
		t.Fatal("expected error for invalid phase")
	}
}

func TestDebuggingSkillSetRootCause(t *testing.T) {
	skill := NewDebuggingSkill()
	session := skill.StartDebug("Issue")

	skill.SetRootCause(session, "Root cause found")
	if session.RootCause != "Root cause found" {
		t.Fatalf("expected root cause, got %s", session.RootCause)
	}
	if session.Phase != "implementation" {
		t.Fatalf("expected phase 'implementation', got %s", session.Phase)
	}
}

func TestDebuggingSkillFormatAsReport(t *testing.T) {
	skill := NewDebuggingSkill()
	session := skill.StartDebug("Issue")
	skill.AddEvidence(session, "Evidence 1")
	skill.AddHypothesis(session, "Hypothesis 1", 0.8)

	report := skill.FormatAsReport(session)
	if report == "" {
		t.Fatal("expected non-empty report")
	}
}

func TestCodeReviewSkillName(t *testing.T) {
	skill := NewCodeReviewSkill()
	if skill.Name() != "code_review" {
		t.Fatalf("expected name 'code_review', got %s", skill.Name())
	}
}

func TestCodeReviewSkillDescription(t *testing.T) {
	skill := NewCodeReviewSkill()
	if skill.Description() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestCodeReviewSkillStartReview(t *testing.T) {
	skill := NewCodeReviewSkill()
	review := skill.StartReview("test.go")

	if review.FilePath != "test.go" {
		t.Fatalf("expected file path 'test.go', got %s", review.FilePath)
	}
}

func TestCodeReviewSkillAddIssue(t *testing.T) {
	skill := NewCodeReviewSkill()
	review := skill.StartReview("test.go")

	skill.AddIssue(review, 10, "major", "style", "Missing comment", "Add comment")
	skill.AddIssue(review, 20, "minor", "style", "Trailing whitespace", "Remove whitespace")

	if skill.GetIssueCount(review) != 2 {
		t.Fatalf("expected 2 issues, got %d", skill.GetIssueCount(review))
	}
}

func TestCodeReviewSkillCalculateRating(t *testing.T) {
	skill := NewCodeReviewSkill()

	goodReview := &Review{}
	skill.AddIssue(goodReview, 1, "minor", "style", "Minor issue", "")
	rating := skill.CalculateRating(goodReview)
	if rating != "good" {
		t.Fatalf("expected 'good', got %s", rating)
	}

	badReview := &Review{}
	skill.AddIssue(badReview, 1, "critical", "security", "Security issue", "")
	rating = skill.CalculateRating(badReview)
	if rating != "needs_work" {
		t.Fatalf("expected 'needs_work', got %s", rating)
	}
}

func TestCodeReviewSkillFormatAsMarkdown(t *testing.T) {
	skill := NewCodeReviewSkill()
	review := skill.StartReview("test.go")
	skill.AddIssue(review, 10, "major", "style", "Issue description", "Fix suggestion")
	skill.SetSummary(review, "Review summary")

	md := skill.FormatAsMarkdown(review)
	if md == "" {
		t.Fatal("expected non-empty markdown")
	}
}

func TestCodeReviewSkillGetIssuesByCategory(t *testing.T) {
	skill := NewCodeReviewSkill()
	review := skill.StartReview("test.go")
	skill.AddIssue(review, 1, "major", "security", "Security issue", "")
	skill.AddIssue(review, 2, "minor", "style", "Style issue", "")

	securityIssues := skill.GetIssuesByCategory(review, "security")
	if len(securityIssues) != 1 {
		t.Fatalf("expected 1 security issue, got %d", len(securityIssues))
	}
}

func TestCodeReviewSkillValidateReview(t *testing.T) {
	skill := NewCodeReviewSkill()

	emptyReview := &Review{}
	issues := skill.ValidateReview(emptyReview)
	if len(issues) == 0 {
		t.Fatal("expected validation issues for empty review")
	}

	validReview := skill.StartReview("test.go")
	issues = skill.ValidateReview(validReview)
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %d", len(issues))
	}
}
