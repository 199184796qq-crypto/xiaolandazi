package speechanalysis

import "testing"

func TestAnalysisUsesManagementRoleWithoutProviderCredentials(t *testing.T) {
	t.Setenv("MANAGEMENT_BASE_URL", "http://127.0.0.1:8080/")
	t.Setenv("CORE_INTERNAL_TOKEN", "fixture-core-token")
	t.Setenv("DASHSCOPE_API_KEY", "")
	s, err := analysisProviderConfig("management")
	if err != nil || s.BaseURL != "http://127.0.0.1:8080/internal/v1/model-analysis" || s.APIKey != "fixture-core-token" {
		t.Fatal("analysis bypassed unified model connection", s, err)
	}
}
