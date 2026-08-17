package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// KanbanBoard represents a kanban board with columns and tasks.
type KanbanBoard struct {
	Name      string        `json:"name"`
	CreatedAt time.Time     `json:"created_at"`
	Columns   []KanbanColumn `json:"columns"`
}

// KanbanColumn represents a column on the kanban board.
type KanbanColumn struct {
	Name  string       `json:"name"`
	Tasks []KanbanTask `json:"tasks"`
}

// KanbanTask represents a task on the kanban board.
type KanbanTask struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Priority    string    `json:"priority"` // "low", "medium", "high", "urgent"
	Tags        []string  `json:"tags,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// KanbanStore manages kanban boards persistently.
type KanbanStore struct {
	workspace string
	boards    map[string]*KanbanBoard
	mu        sync.RWMutex
}

// KanbanTool provides kanban board management capabilities.
type KanbanTool struct {
	store *KanbanStore
}

func NewKanbanTool(workspace string) *KanbanTool {
	store := &KanbanStore{
		workspace: workspace,
		boards:    make(map[string]*KanbanBoard),
	}
	store.loadBoards()
	return &KanbanTool{store: store}
}

func (t *KanbanTool) Name() string {
	return "kanban"
}

func (t *KanbanTool) Description() string {
	return "Manage kanban boards for task tracking. Supports creating boards, adding/moving/completing tasks, and listing board status."
}

func (t *KanbanTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action to perform: create_board, add_task, move_task, complete_task, list_boards, get_board",
				"enum":        []string{"create_board", "add_task", "move_task", "complete_task", "list_boards", "get_board"},
			},
			"board": map[string]interface{}{
				"type":        "string",
				"description": "Board name",
			},
			"column": map[string]interface{}{
				"type":        "string",
				"description": "Column name (for add_task, move_task)",
			},
			"task_id": map[string]interface{}{
				"type":        "string",
				"description": "Task ID (for move_task, complete_task)",
			},
			"title": map[string]interface{}{
				"type":        "string",
				"description": "Task title (for add_task)",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Task description (for add_task)",
			},
			"priority": map[string]interface{}{
				"type":        "string",
				"description": "Task priority",
				"enum":        []string{"low", "medium", "high", "urgent"},
			},
			"tags": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Task tags",
			},
			"target_column": map[string]interface{}{
				"type":        "string",
				"description": "Target column name (for move_task)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *KanbanTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	action, _ := args["action"].(string)
	if action == "" {
		return ErrorResult("action is required")
	}

	switch action {
	case "create_board":
		return t.createBoard(args)
	case "add_task":
		return t.addTask(args)
	case "move_task":
		return t.moveTask(args)
	case "complete_task":
		return t.completeTask(args)
	case "list_boards":
		return t.listBoards()
	case "get_board":
		return t.getBoard(args)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %s", action))
	}
}

func (t *KanbanTool) createBoard(args map[string]interface{}) *ToolResult {
	boardName, _ := args["board"].(string)
	if boardName == "" {
		return ErrorResult("board name is required")
	}

	t.store.mu.Lock()
	defer t.store.mu.Unlock()

	if _, exists := t.store.boards[boardName]; exists {
		return ErrorResult(fmt.Sprintf("board %q already exists", boardName))
	}

	board := &KanbanBoard{
		Name:      boardName,
		CreatedAt: time.Now(),
		Columns: []KanbanColumn{
			{Name: "Backlog", Tasks: []KanbanTask{}},
			{Name: "To Do", Tasks: []KanbanTask{}},
			{Name: "In Progress", Tasks: []KanbanTask{}},
			{Name: "Done", Tasks: []KanbanTask{}},
		},
	}

	t.store.boards[boardName] = board
	t.store.saveBoard(board)

	return UserResult(fmt.Sprintf("Created board %q with columns: Backlog, To Do, In Progress, Done", boardName))
}

func (t *KanbanTool) addTask(args map[string]interface{}) *ToolResult {
	boardName, _ := args["board"].(string)
	columnName, _ := args["column"].(string)
	title, _ := args["title"].(string)

	if boardName == "" || title == "" {
		return ErrorResult("board and title are required")
	}
	if columnName == "" {
		columnName = "Backlog"
	}

	t.store.mu.Lock()
	defer t.store.mu.Unlock()

	board, ok := t.store.boards[boardName]
	if !ok {
		return ErrorResult(fmt.Sprintf("board %q not found", boardName))
	}

	colIdx := -1
	for i, col := range board.Columns {
		if col.Name == columnName {
			colIdx = i
			break
		}
	}
	if colIdx < 0 {
		return ErrorResult(fmt.Sprintf("column %q not found", columnName))
	}

	priority, _ := args["priority"].(string)
	if priority == "" {
		priority = "medium"
	}
	description, _ := args["description"].(string)
	var tags []string
	if t, ok := args["tags"].([]interface{}); ok {
		for _, tag := range t {
			if s, ok := tag.(string); ok {
				tags = append(tags, s)
			}
		}
	}

	task := KanbanTask{
		ID:          fmt.Sprintf("task-%d", time.Now().UnixMilli()),
		Title:       title,
		Description: description,
		Priority:    priority,
		Tags:        tags,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	board.Columns[colIdx].Tasks = append(board.Columns[colIdx].Tasks, task)
	t.store.saveBoard(board)

	return UserResult(fmt.Sprintf("Added task %q to %s/%s", task.ID, boardName, columnName))
}

func (t *KanbanTool) moveTask(args map[string]interface{}) *ToolResult {
	boardName, _ := args["board"].(string)
	taskID, _ := args["task_id"].(string)
	targetColumn, _ := args["target_column"].(string)

	if boardName == "" || taskID == "" || targetColumn == "" {
		return ErrorResult("board, task_id, and target_column are required")
	}

	t.store.mu.Lock()
	defer t.store.mu.Unlock()

	board, ok := t.store.boards[boardName]
	if !ok {
		return ErrorResult(fmt.Sprintf("board %q not found", boardName))
	}

	// Find and remove task from source column
	var task *KanbanTask
	for i, col := range board.Columns {
		for j, t := range col.Tasks {
			if t.ID == taskID {
				task = &board.Columns[i].Tasks[j]
				board.Columns[i].Tasks = append(col.Tasks[:j], col.Tasks[j+1:]...)
				break
			}
		}
		if task != nil {
			break
		}
	}

	if task == nil {
		return ErrorResult(fmt.Sprintf("task %q not found", taskID))
	}

	// Find target column and add task
	for i, col := range board.Columns {
		if col.Name == targetColumn {
			task.UpdatedAt = time.Now()
			board.Columns[i].Tasks = append(board.Columns[i].Tasks, *task)
			t.store.saveBoard(board)
			return UserResult(fmt.Sprintf("Moved task %q to %s", taskID, targetColumn))
		}
	}

	return ErrorResult(fmt.Sprintf("target column %q not found", targetColumn))
}

func (t *KanbanTool) completeTask(args map[string]interface{}) *ToolResult {
	boardName, _ := args["board"].(string)
	taskID, _ := args["task_id"].(string)

	if boardName == "" || taskID == "" {
		return ErrorResult("board and task_id are required")
	}

	args["target_column"] = "Done"
	return t.moveTask(args)
}

func (t *KanbanTool) listBoards() *ToolResult {
	t.store.mu.RLock()
	defer t.store.mu.RUnlock()

	if len(t.store.boards) == 0 {
		return UserResult("No boards found. Create one with create_board action.")
	}

	var result string
	for name, board := range t.store.boards {
		totalTasks := 0
		for _, col := range board.Columns {
			totalTasks += len(col.Tasks)
		}
		result += fmt.Sprintf("- %s (%d tasks)\n", name, totalTasks)
	}

	return UserResult(result)
}

func (t *KanbanTool) getBoard(args map[string]interface{}) *ToolResult {
	boardName, _ := args["board"].(string)
	if boardName == "" {
		return ErrorResult("board name is required")
	}

	t.store.mu.RLock()
	defer t.store.mu.RUnlock()

	board, ok := t.store.boards[boardName]
	if !ok {
		return ErrorResult(fmt.Sprintf("board %q not found", boardName))
	}

	result := fmt.Sprintf("# %s\n\n", board.Name)
	for _, col := range board.Columns {
		result += fmt.Sprintf("## %s (%d)\n", col.Name, len(col.Tasks))
		for _, task := range col.Tasks {
			priority := ""
			if task.Priority == "high" || task.Priority == "urgent" {
				priority = " [" + task.Priority + "]"
			}
			result += fmt.Sprintf("  - %s: %s%s\n", task.ID, task.Title, priority)
		}
		result += "\n"
	}

	return UserResult(result)
}

func (s *KanbanStore) loadBoards() {
	filePath := filepath.Join(s.workspace, "kanban.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return
	}

	var boards map[string]*KanbanBoard
	if err := json.Unmarshal(data, &boards); err != nil {
		return
	}

	s.boards = boards
}

func (s *KanbanStore) saveBoard(board *KanbanBoard) {
	s.boards[board.Name] = board

	filePath := filepath.Join(s.workspace, "kanban.json")
	data, err := json.MarshalIndent(s.boards, "", "  ")
	if err != nil {
		return
	}
	os.MkdirAll(s.workspace, 0755)
	os.WriteFile(filePath, data, 0644)
}
