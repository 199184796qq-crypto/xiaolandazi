package answerflow

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/danmakutest"
)

func TestRetrievedIntentAndFactsShareClaimAttributes(t *testing.T) {
	lib := vectorFixture()
	lib.Links[0].Product = "爱媛果冻橙"
	lib.Links[0].DisplayName = "橙子专区"
	price := lib.Records[0]
	price.Claim.Attribute = "直播间价格"
	price.Claim.Topic = "爱媛果冻橙"
	price.Claim.ClaimKey = "直播价"
	shipping := lib.Records[2]
	shipping.Claim.Topic = "橙子专区"
	shipping.Claim.Attribute = "是否邮寄"
	shipping.Claim.ClaimKey = "邮寄"
	dirty := vectorFact(39, 3, "爱媛果冻橙", "补充材料")
	dirty.Claim.ClaimKey = "对象主题"
	origin := vectorFact(40, 3, "杂交", "品种原文")
	origin.Claim.Attribute, origin.Claim.ClaimKey = "品种特征", "品种来源"
	productName := vectorFact(41, 3, "果冻橙", "爱媛果冻橙")
	productName.Claim.Attribute, productName.Claim.ClaimKey = "品名", "商品名称"
	lib.Records = []danmakutest.Record{price, shipping, dirty, origin, productName}

	question := "配送能邮寄吗"
	intent := `{"meaning":"问能否邮寄","category":"问题","asks":[{"id":"a1","text":"配送能邮寄吗","query":"橙子专区配送能邮寄吗","subjects":["橙子专区"],"attributes":["是否邮寄","爱媛果冻橙"],"attribute_scope":"明确"}]}`
	var packet map[string]json.RawMessage
	var calls []Stage
	service := retrievedTestService(t, lib, question, intent, &packet, &calls)
	complete := service.Complete
	service.Complete = func(ctx context.Context, stage Stage, messages []agentgateway.Message) (agentgateway.Response, error) {
		if stage == IntentStage {
			var payload struct {
				Attributes []string                           `json:"attributes"`
				Catalog    []danmakutest.IntentAttributeEntry `json:"attribute_catalog"`
			}
			if err := json.Unmarshal([]byte(messages[1].Content), &payload); err != nil {
				t.Fatal(err)
			}
			want := []danmakutest.IntentAttributeEntry{
				{Key: "直播价", Topic: "价格"}, {Key: "邮寄", Topic: "配送"},
				{Key: "品种来源", Topic: "品种来源"}, {Key: "商品名称", Topic: "商品名称"},
			}
			if !slices.Equal(payload.Attributes, []string{"价格", "品种来源", "商品名称", "配送"}) || !slices.Equal(payload.Catalog, want) {
				t.Fatalf("intent attributes = %v, catalog = %v", payload.Attributes, payload.Catalog)
			}
		}
		return complete(ctx, stage, messages)
	}
	config := RetrievalConfig{AttributeAliases: map[string]string{"是否邮寄": "配送", "直播间价格": "直播价", "品名": "商品信息"}}
	got, err := service.AnalyzeRetrieved(t.Context(), Request{Scope: Scope{Tenant: 14, Plan: 9}, Input: Input{Question: question}}, config)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Retrieval.Asks) != 1 || !slices.Equal(got.Retrieval.Asks[0].Attributes, []string{"配送"}) {
		t.Fatal(got.Retrieval.Asks)
	}
	if len(got.Recall.Hits) != 1 || got.Recall.Hits[0].ID != shipping.Claim.ID || got.Recall.Hits[0].Attribute != "配送" {
		t.Fatal(got.Recall.Hits)
	}
}
