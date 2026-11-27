//go:build !windows

package rerank

func windowsUserEnv(string) string { return "" }
