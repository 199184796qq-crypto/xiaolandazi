//go:build !windows

package agentgateway

func windowsUserEnv(string) string {
	return ""
}
