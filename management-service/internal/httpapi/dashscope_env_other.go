//go:build !windows

package httpapi

func dashScopeWindowsUserEnv(string) string {
	return ""
}
