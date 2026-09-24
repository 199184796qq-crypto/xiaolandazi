//go:build windows

package httpapi

import "golang.org/x/sys/windows/registry"

func dashScopeWindowsUserEnv(name string) string {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Environment`,
		registry.QUERY_VALUE,
	)
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
