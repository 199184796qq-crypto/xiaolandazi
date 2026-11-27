//go:build windows

package rerank

import "golang.org/x/sys/windows/registry"

// windowsUserEnv is the same fallback the other DashScope adapters use: when a
// service was started from a shell that did not inherit the user environment,
// read DASHSCOPE_API_KEY from the registry instead of failing silently.
func windowsUserEnv(name string) string {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()
	value, _, err := key.GetStringValue(name)
	if err != nil {
		return ""
	}
	return value
}
