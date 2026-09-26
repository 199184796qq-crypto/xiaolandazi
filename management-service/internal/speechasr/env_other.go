//go:build !windows

package speechasr

func windowsUserEnv(string) string {
	return ""
}
