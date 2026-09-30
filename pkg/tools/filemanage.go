package tools

import (
	"context"
	"fmt"
	"github.com/ianclemence/ghost/pkg/config"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// The workspace is Ghost's own room. It may create folders, sort things into
// them, rename and delete what it made or what the owner asked it to remove,
// and it may do all of that only inside the workspace. These two tools give it
// the missing verbs (write_file already creates folders), under the same
// guards as every other write: the workspace boundary, the primary-file guard
// and the runtime-owned estate.

// workspaceOnlyPath resolves a path for a destructive operation. Reading may
// reach the folder around the workspace (Ghost's own source, in development);
// deleting and moving may not: they act on the workspace and nothing else, on
// where the path really leads, whatever the restrict setting says.
func workspaceOnlyPath(path, workspace string) (string, error) {
	if workspace == "" {
		return "", fmt.Errorf("no workspace is configured, so nothing can be deleted or moved")
	}
	resolved, err := validatePath(path, workspace, true)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	if !under(abs, resolved) {
		return "", fmt.Errorf("access denied: only files inside the workspace can be changed this way")
	}
	if real := realPath(resolved); real != "" && !under(realPath(abs), real) {
		return "", fmt.Errorf("access denied: path is outside the workspace")
	}
	return resolved, nil
}

// DeleteFileTool removes a file, or a folder when asked to.
type DeleteFileTool struct {
	workspace string
	restrict  bool
}

func NewDeleteFileTool(workspace string, restrict bool) *DeleteFileTool {
	return &DeleteFileTool{workspace: workspace, restrict: restrict}
}

func (t *DeleteFileTool) Name() string { return "delete_file" }

func (t *DeleteFileTool) Description() string {
	return "Delete a file (or, with recursive, a folder) from your workspace. Use when: the owner asks you to delete or remove a file, or you are tidying files you made. Only works inside the workspace and never on Ghost's own files. Cannot be undone."
}

func (t *DeleteFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to delete, relative to the workspace. Example: \"notes/old-draft.md\".",
			},
			"recursive": map[string]interface{}{
				"type":        "boolean",
				"description": "Set true to delete a folder and everything in it. Without it only files and empty folders are removed.",
			},
		},
		"required": []string{"path"},
	}
}

func (t *DeleteFileTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	path, _ := args["path"].(string)
	recursive, _ := args["recursive"].(bool)
	if strings.TrimSpace(path) == "" {
		return ErrorResult("path is required")
	}
	resolved, err := workspaceOnlyPath(path, t.workspace)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if t.workspace != "" {
		if abs, aerr := filepath.Abs(t.workspace); aerr == nil && filepath.Clean(resolved) == filepath.Clean(abs) {
			return ErrorResult("that is the whole workspace; name a file or folder inside it")
		}
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrorResult(fmt.Sprintf("%q does not exist, so there is nothing to delete", path))
		}
		return ErrorResult(fmt.Sprintf("cannot read %q: %v", path, err))
	}
	if err := t.checkAllowed(resolved, info, recursive); err != nil {
		return ErrorResult(err.Error())
	}
	size := info.Size()
	if info.IsDir() {
		if recursive {
			err = os.RemoveAll(resolved)
		} else {
			err = os.Remove(resolved)
		}
	} else {
		err = os.Remove(resolved)
	}
	if err != nil {
		if info.IsDir() && !recursive {
			return ErrorResult(fmt.Sprintf("%q is a folder with things in it; ask again with recursive to delete it and everything inside", path))
		}
		return ErrorResult(fmt.Sprintf("could not delete %q: %v", path, err))
	}
	what := "file"
	if info.IsDir() {
		what = "folder"
	}
	res := SilentResult(fmt.Sprintf("Deleted %s %s (%s).", what, path, fileSizeText(size)))
	return res
}

// checkAllowed applies every write guard to the target, and for a recursive
// delete to everything under it, so a folder cannot be used to reach past a
// guard that would stop the file itself.
func (t *DeleteFileTool) checkAllowed(resolved string, info os.FileInfo, recursive bool) error {
	if isDefaultWorkspaceEntry(t.workspace, resolved) {
		return defaultEntryDeny(t.workspace, resolved)
	}
	if err := guardPrimaryFile(resolved); err != nil {
		return err
	}
	if err := guardGhostEstateWrite(t.workspace, resolved); err != nil {
		return err
	}
	if !info.IsDir() || !recursive {
		return nil
	}
	return filepath.WalkDir(resolved, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		if err := guardPrimaryFile(p); err != nil {
			return fmt.Errorf("this folder holds a protected file (%s); nothing was deleted", filepath.Base(p))
		}
		if err := guardGhostEstateWrite(t.workspace, p); err != nil {
			return fmt.Errorf("this folder holds part of Ghost's own data (%s); nothing was deleted", filepath.Base(p))
		}
		return nil
	})
}

// Verify confirms the path is really gone (the evidence a deletion happened).
func (t *DeleteFileTool) Verify(ctx context.Context, args map[string]interface{}) error {
	path, _ := args["path"].(string)
	resolved, err := workspaceOnlyPath(path, t.workspace)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(resolved); err == nil {
		return fmt.Errorf("%q is still there", path)
	}
	return nil
}

// MoveFileTool renames or moves a file or folder inside the workspace.
type MoveFileTool struct {
	workspace string
	restrict  bool
}

func NewMoveFileTool(workspace string, restrict bool) *MoveFileTool {
	return &MoveFileTool{workspace: workspace, restrict: restrict}
}

func (t *MoveFileTool) Name() string { return "move_file" }

func (t *MoveFileTool) Description() string {
	return "Move or rename a file or folder inside your workspace, creating the destination folder if needed. Use when: organizing files into folders or renaming them. Never overwrites: if the destination exists it stops and says so."
}

func (t *MoveFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"from": map[string]interface{}{"type": "string", "description": "Current path. Example: \"notes.txt\"."},
			"to":   map[string]interface{}{"type": "string", "description": "New path. Example: \"notes/2026/notes.txt\"."},
		},
		"required": []string{"from", "to"},
	}
}

func (t *MoveFileTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	from, _ := args["from"].(string)
	to, _ := args["to"].(string)
	if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" {
		return ErrorResult("from and to are required")
	}
	src, err := workspaceOnlyPath(from, t.workspace)
	if err != nil {
		return ErrorResult(err.Error())
	}
	dst, err := workspaceOnlyPath(to, t.workspace)
	if err != nil {
		return ErrorResult(err.Error())
	}
	for _, p := range []string{src, dst} {
		if isDefaultWorkspaceEntry(t.workspace, p) {
			return ErrorResult(defaultEntryDeny(t.workspace, p).Error())
		}
		if err := guardPrimaryFile(p); err != nil {
			return ErrorResult(err.Error())
		}
		if err := guardGhostEstateWrite(t.workspace, p); err != nil {
			return ErrorResult(err.Error())
		}
	}
	if _, err := os.Lstat(src); err != nil {
		return ErrorResult(fmt.Sprintf("%q does not exist", from))
	}
	if _, err := os.Lstat(dst); err == nil {
		return ErrorResult(fmt.Sprintf("%q already exists; choose another name so nothing is overwritten", to))
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return ErrorResult(fmt.Sprintf("could not create the destination folder: %v", err))
	}
	if err := os.Rename(src, dst); err != nil {
		return ErrorResult(fmt.Sprintf("could not move %q: %v", from, err))
	}
	config.MatchOwnerUnder(t.workspace, dst)
	return SilentResult(fmt.Sprintf("Moved %s to %s.", from, to))
}

func fileSizeText(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}
