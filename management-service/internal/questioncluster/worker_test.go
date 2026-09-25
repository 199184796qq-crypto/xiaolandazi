package questioncluster

import (
	"testing"
	"time"
)

func TestSelectEligibleTopicsOnlyRecentSmallDynamicBuckets(t *testing.T) {
	now := time.Now().UTC()
	topics := []topicBucket{
		{
			Topic:     "Q:配料怎么样",
			Count:     1,
			Questions: []topicQuestion{{EventID: 11, Content: "配料怎么样", OccurredAt: now.Add(-time.Minute)}},
		},
		{
			Topic:     "Q:要冷藏吗",
			Count:     3,
			Questions: []topicQuestion{{EventID: 12, Content: "要冷藏吗", OccurredAt: now.Add(-2 * time.Minute)}},
		},
		{
			Topic:     "Q:老问题",
			Count:     1,
			Questions: []topicQuestion{{EventID: 13, Content: "老问题", OccurredAt: now.Add(-10 * time.Minute)}},
		},
		{
			Topic:     "Q:已经很多",
			Count:     7,
			Questions: []topicQuestion{{EventID: 14, Content: "已经很多", OccurredAt: now.Add(-time.Minute)}},
		},
		{
			Topic:     "FAMILY:发货物流",
			Count:     1,
			Questions: []topicQuestion{{EventID: 15, Content: "多久发货", OccurredAt: now.Add(-time.Minute)}},
		},
	}

	eligible, maxEventID := selectEligibleTopics(topics, now)
	if len(eligible) != 2 {
		t.Fatalf("eligible=%#v", eligible)
	}
	if _, ok := eligible["Q:配料怎么样"]; !ok {
		t.Fatalf("recent singleton missing: %#v", eligible)
	}
	if _, ok := eligible["Q:要冷藏吗"]; !ok {
		t.Fatalf("recent small bucket missing: %#v", eligible)
	}
	if maxEventID != 12 {
		t.Fatalf("maxEventID=%d", maxEventID)
	}
}

func TestValidateGroupsRejectsUnknownLowConfidenceAndNonEligibleSources(t *testing.T) {
	topics := []topicBucket{
		{Topic: "Q:要放冰箱吗"},
		{Topic: "Q:需要冷藏吗"},
		{Topic: "FAMILY:保存保质"},
	}
	eligible := map[string]struct{}{
		"Q:要放冰箱吗": {},
		"Q:需要冷藏吗": {},
	}

	groups := validateGroups([]modelGroup{
		{
			RepresentativeTopic: "FAMILY:保存保质",
			SourceTopics:        []string{"Q:要放冰箱吗"},
			Confidence:          0.93,
		},
		{
			RepresentativeTopic: "Q:需要冷藏吗",
			SourceTopics:        []string{"不存在的桶"},
			Confidence:          0.99,
		},
		{
			RepresentativeTopic: "Q:需要冷藏吗",
			SourceTopics:        []string{"FAMILY:保存保质"},
			Confidence:          0.99,
		},
		{
			RepresentativeTopic: "Q:需要冷藏吗",
			SourceTopics:        []string{"Q:要放冰箱吗"},
			Confidence:          0.4,
		},
	}, topics, eligible)

	if len(groups) != 1 {
		t.Fatalf("groups=%#v", groups)
	}
	if groups[0].RepresentativeTopic != "FAMILY:保存保质" ||
		len(groups[0].SourceTopics) != 1 ||
		groups[0].SourceTopics[0] != "Q:要放冰箱吗" {
		t.Fatalf("unexpected validated group: %#v", groups[0])
	}
}
