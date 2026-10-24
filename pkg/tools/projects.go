package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Projects are the one place a command may build things that last. Each is a
// folder directly under projects/, beside the workspace, and the command's
// sandbox binds its working directory read-write (see bwrapArgs). That bind
// only happens for a folder that exists, and a sandboxed command cannot create
// it, so the runtime owns the folder's lifecycle: a command that names
// projects/<name> as its working directory gets that folder, nothing more.
//
// This is the only directory the runtime creates for the model. It creates one
// level, under a name it validates, and refuses anything that is not a real
// folder, so a name can never reach out of projects/ and a link planted there
// can never be bound read-write into the sandbox.

const projectsDirName = "projects"

// A project name is one path segment of ordinary characters. It cannot start
// with a dot, so "." and ".." are never names and hidden folders are not
// projects.
var projectNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// projectsRoot is where user projects live: beside the workspace, in Ghost's
// home.
func projectsRoot(workspace string) string {
	return filepath.Join(filepath.Dir(workspace), projectsDirName)
}

// resolveWorkingDir makes a requested working directory absolute, the one way
// every check and the process itself will see it. A relative path is read as
// the file tools read it, against the workspace, except that "projects/..." is
// the projects folder beside it, which is how the owner and the docs name it.
func resolveWorkingDir(workspace, wd string) string {
	if wd == "" || filepath.IsAbs(wd) || workspace == "" {
		return wd
	}
	ws, err := filepath.Abs(workspace)
	if err != nil {
		return wd
	}
	clean := filepath.ToSlash(filepath.Clean(wd))
	if clean == projectsDirName || strings.HasPrefix(clean, projectsDirName+"/") {
		return filepath.Join(filepath.Dir(ws), filepath.FromSlash(clean))
	}
	return filepath.Join(ws, wd)
}

// ensureProjectDir creates the project folder that cwd is in or at, when cwd is
// inside projects/. It is a no-op for any other directory. Only the project's
// own folder is ever created: a deeper path that does not exist stays missing,
// so the command fails plainly instead of the runtime building a tree.
func ensureProjectDir(workspace, cwd string) error {
	if workspace == "" || cwd == "" {
		return nil
	}
	root, err := filepath.Abs(projectsRoot(workspace))
	if err != nil {
		return nil
	}
	abs, err := filepath.Abs(cwd)
	if err != nil || !under(root, abs) || abs == root {
		return nil
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return nil
	}
	name := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
	if !projectNameRE.MatchString(name) {
		return fmt.Errorf("%q is not a valid project name: use letters, digits, dots, dashes and underscores, starting with a letter or digit", name)
	}
	if err := ensureRealDir(root, true); err != nil {
		return err
	}
	return ensureRealDir(filepath.Join(root, name), false)
}

// ensureRealDir makes sure p is a real directory, creating it when absent. A
// symlink is refused even when it points at a directory: the sandbox binds the
// path it is given, and a link could aim that bind anywhere.
func ensureRealDir(p string, withParents bool) error {
	if _, err := os.Lstat(p); os.IsNotExist(err) {
		create := os.Mkdir
		if withParents {
			create = os.MkdirAll
		}
		// Losing a race to another creator is fine, but what won must still
		// pass the check below.
		if err := create(p, 0o755); err != nil && !os.IsExist(err) {
			return err
		}
	}
	info, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s is not a plain folder, so it cannot be used for projects", p)
	}
	return nil
}
