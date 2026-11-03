package decisionexecutor

import "testing"

func TestWelcomeIntroRejectsLinkSpecificDetails(t *testing.T) {
	for _, text := range []string{
		"巴国布衣本地生活推1号链接10人餐，含22道菜，适合3-6人。",
		"卖南充本地团餐，1号链接团购价337元。",
		"卖南充本地团餐，每桌可用一张券。",
	} {
		if welcomeIntroScopeProblem(text) == nil {
			t.Fatalf("accepted link-specific intro: %s", text)
		}
	}
	if err := welcomeIntroScopeProblem("卖南充本地团餐，主打多人聚餐和到店堂食。"); err != nil {
		t.Fatalf("rejected category-level intro: %v", err)
	}
}
