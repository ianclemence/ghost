//go:build !linux && !darwin

package golden

func diskFreeMB(string) int { return 0 }
