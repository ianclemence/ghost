//go:build !unix

package uploads

func matchOwner(workspace, path string) {}
