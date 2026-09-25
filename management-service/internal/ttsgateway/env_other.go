//go:build !windows

package ttsgateway

func windowsUserEnv(string) string { return "" }
