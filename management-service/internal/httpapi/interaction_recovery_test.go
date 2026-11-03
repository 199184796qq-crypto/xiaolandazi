package httpapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
)

func TestBatchPreparationKeepsRecoveredPeopleAndDropsOnlyUnusableItem(t *testing.T) {
	in := httpBatchFixture(3)
	items, err := batchInputsV2("recovery-batch", in, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	got, _, err := preparedBatchResultsV2(context.Background(), httpBatchSnapshot(t), in, items, func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
		calls++
		return agentgateway.Response{FinishReason: "stop", Text: strings.Join([]string{
			"编号：U03\n意图：咨询发货\n类型：提问\n歧义：否\n回答：丙问题按当前方案说明。",
			"编号：U02\n意图：咨询发货\n类型：提问\n歧义：否\n回答：",
			"编号：U01\n意图：咨询发货\n类型：提问\n歧义：否\n回答：甲问题按当前方案说明。",
		}, "\n")}, nil
	})
	if err != nil || calls != 1 || len(got) != 3 || got[0].Error != "" || got[1].Error == "" || got[1].Envelope.Body != "" || got[2].Error != "" {
		t.Fatal(got, err, calls)
	}
	for _, i := range []int{0, 2} {
		if got[i].Task != in[i].Task || got[i].Envelope.Task != in[i].Task || got[i].Envelope.Tenant != in[i].Tenant || got[i].Envelope.Room != in[i].Room {
			t.Fatal("recovery must retain each person's transport identity", got[i])
		}
	}
}
