package httpapi

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
)

func TestMaterialSourceRowsSplitAndSelect(t *testing.T) {
	raw := "  第一行  [/][/]第二行【/】第三行\r\n \n" + strings.Repeat("同文\n", 10)
	rows := splitMaterialSourceRows(raw)
	if len(rows) != 13 {
		t.Fatal(rows)
	}
	for i, want := range []string{"第一行", "第二行", "第三行", "同文"} {
		row := rows[i]
		if row.Number != i+1 || row.Text != want || raw[row.start:row.end] != want {
			t.Fatal(row, want)
		}
	}
	for _, invalid := range [][]int{nil, {0}, {14}, {1, 1}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}} {
		if _, err := materialSelectedSourceRows(rows, invalid); err == nil {
			t.Fatal("accepted invalid selection", invalid)
		}
	}
	selected, err := materialSelectedSourceRows(rows, []int{4, 1})
	if err != nil || !reflect.DeepEqual(selected, []materialSourceRow{rows[0], rows[3]}) {
		t.Fatal(selected, err)
	}
}

func TestMaterialSourceRowRequestOnlySelectedText(t *testing.T) {
	raw := "1号链接：\n规格：3L\n条件甲\n2号链接：\n规格：3L"
	rows := splitMaterialSourceRows(raw)
	doc := newMaterialImportDocument(raw, []materialLinkObject{{ID: 21, LinkKey: "1"}, {ID: 22, LinkKey: "2"}}, nil)
	selected := []materialSourceRow{rows[1], rows[4]}
	if err := validateMaterialReviewRows(selected, []int{5}); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]int{{1}, {5, 5}, {2, 5, 5}} {
		if err := validateMaterialReviewRows(selected, invalid); err == nil {
			t.Fatalf("accepted invalid review rows %v", invalid)
		}
	}
	objects := materialRowObjects(doc.links, model.MaterialObjectCatalog{})
	intro := "本直播间主营家居用品。"
	body := doc.rowAnalysisBody(selected, objects, []int{5}, " \n"+intro+" ")
	var payload struct {
		RoomIntro string `json:"room_intro"`
		Rows      []struct {
			Number        int
			Text, Heading string
			ReviewEmpty   bool `json:"review_empty"`
		}
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RoomIntro != intro || strings.Count(body, intro) != 1 ||
		len(payload.Rows) != 2 || payload.Rows[0].Number != 2 || payload.Rows[1].Number != 5 ||
		payload.Rows[0].Heading != "1号链接：" || payload.Rows[1].Heading != "2号链接：" || strings.Contains(body, "条件甲") ||
		payload.Rows[0].ReviewEmpty || !payload.Rows[1].ReviewEmpty || strings.Count(body, "review_empty") != 1 {
		t.Fatal(body)
	}
	if body := doc.rowAnalysisBody(selected, objects, nil, " \n"); strings.Contains(body, `"room_intro"`) {
		t.Fatal(body)
	}
	request := materialSourceRowsRequest(&agentgateway.SpeechModel{Model: "fixture", MaxTokens: 6000}, body)
	if request.ResponseFormat != agentgateway.ResponseText || request.MaxTokens != 6000 ||
		len(request.Messages) != 2 || request.Messages[1].Content != body {
		t.Fatal(request)
	}
}

func TestMaterialSourceRowIntroCannotSupplyEvidence(t *testing.T) {
	raw := "本直播间主营家居用品。\n已有几十年历史。"
	rows := splitMaterialSourceRows(raw)
	doc := newMaterialImportDocument(raw, nil, nil)
	objects := materialRowObjects(nil, model.MaterialObjectCatalog{})
	response := agentgateway.Response{Text: `{"rows":[{"number":2,"items":[
		{"key":"brand_history","topic":"品牌历史","value":"几十年","source_sentence":"已有几十年历史。"},
		{"key":"business_type","topic":"业务类型","value":"家居用品","source_sentence":"本直播间主营家居用品。"},
		{"key":"brand_history","topic":"品牌历史","value":"几十年","source_sentence":"已有几十年历史。","source_context":["本直播间主营家居用品。"]}
	]}]}`}
	results, err := doc.decodeRowResults(response, rows[1:], objects, 1, 1)
	if err != nil || len(results) != 1 || len(results[0].Items) != 1 || len(results[0].Warnings) != 1 {
		t.Fatal(results, err)
	}
	item := results[0].Items[0]
	if item.Value != "几十年" || item.SourceSentence != rows[1].Text || item.SourceRef == nil || item.SourceRef.ID != doc.id {
		t.Fatal(item)
	}
}

func TestMaterialSourceRowsIncludeBoundedPreviousLineWithinSection(t *testing.T) {
	longLine := strings.Repeat("甲", materialSourcePreviousLineLimit) + "末尾"
	raw := "前一段[/]本段开头\n" + longLine + "\n需要分析【/】下一段"
	rows := splitMaterialSourceRows(raw)
	selected := []materialSourceRow{rows[1], rows[3], rows[4]}
	doc := newMaterialImportDocument(raw, nil, nil)
	var payload struct {
		Rows []struct {
			Number       int
			PreviousLine string `json:"previous_line"`
		}
	}
	body := doc.rowAnalysisBody(selected, nil, nil, "")
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Rows) != 3 || payload.Rows[0].PreviousLine != "" ||
		payload.Rows[1].Number != 4 || payload.Rows[1].PreviousLine != strings.Repeat("甲", materialSourcePreviousLineLimit) ||
		payload.Rows[2].PreviousLine != "" {
		t.Fatal(body)
	}
	preview, err := json.Marshal(rows)
	if err != nil || strings.Contains(string(preview), `"previous_line"`) {
		t.Fatal(string(preview), err)
	}
}

func TestMaterialSourceRowsKeepAttributeWhenReturnedOwnerIsUnsubstantiated(t *testing.T) {
	raw := "这是北门店官方直播间。\n经营了几十年。"
	rows := splitMaterialSourceRows(raw)
	catalog := model.MaterialObjectCatalog{Stores: []model.MaterialStoreObject{{Name: "北门店"}}}
	doc := newMaterialImportDocument(raw, nil, catalog.Stores)
	objects := materialRowObjects(nil, catalog)
	response := agentgateway.Response{Text: `{"rows":[{"number":2,"items":[
		{"key":"operating_history","topic":"经营年限","value":"几十年","source_sentence":"经营了几十年。","object_targets":[{"kind":"store","store_name":"北门店"}]}
	]}]}`}
	result, err := doc.decodeRowResults(response, rows[1:], objects, 1, 1)
	if err != nil || len(result) != 1 || len(result[0].Items) != 1 {
		t.Fatal(result, err)
	}
	item := result[0].Items[0]
	if item.Value != "几十年" || item.SourceSentence != rows[1].Text || item.ScopeKind != "unresolved" ||
		len(item.ObjectTargets) != 0 || !item.NeedsConfirmation {
		t.Fatal(item)
	}
}

func TestMaterialSourceRowsKeepEvidencePerSelectedRow(t *testing.T) {
	raw := "1号链接：\n产品甲，规格：3L\n相邻描述：7天\n2号链接：\n产品甲，规格：3L"
	rows := splitMaterialSourceRows(raw)
	doc := newMaterialImportDocument(raw, []materialLinkObject{{ID: 21, LinkKey: "1"}, {ID: 22, LinkKey: "2"}}, nil)
	objects := materialRowObjects(doc.links, model.MaterialObjectCatalog{})
	selected := []materialSourceRow{rows[1], rows[3], rows[4]}
	response := agentgateway.Response{Text: `{"rows":[
		{"number":5,"items":[{"destination":"fact","attribute":"规格","value":"3L","source_sentence":"产品甲，规格：3L","object_targets":[{"kind":"product","link_id":22}]}]},
		{"number":2,"items":[
			{"destination":"fact","attribute":"名称","value":"产品甲","source_sentence":"产品甲，规格：3L","object_targets":[{"kind":"product","link_id":21}]},
			{"destination":"fact","attribute":"规格","value":"3L","source_sentence":"产品甲，规格：3L","object_targets":[{"kind":"product","link_id":21}]},
			{"destination":"fact","attribute":"期限","value":"7天","source_sentence":"相邻描述：7天","object_targets":[{"kind":"product","link_id":21}]}]},
		{"number":3,"items":[{"value":"7天","source_sentence":"相邻描述：7天"}]}
	]}`}
	results, err := doc.decodeRowResults(response, selected, objects, 1, 1)
	if err != nil || len(results) != 3 || results[0].Number != 2 || results[1].Number != 4 || results[2].Number != 5 {
		t.Fatal(results, err)
	}
	if len(results[0].Items) != 2 || len(results[0].Warnings) != 1 || results[1].Error == "" || len(results[2].Items) != 1 {
		t.Fatal(results)
	}
	for _, index := range []int{0, 2} {
		item := results[index].Items[0]
		want := selected[index]
		if item.SourceRef.ID != doc.id || item.SourceRef.Ranges[0].Start != want.start ||
			item.SourceRef.Ranges[0].End != want.end || len(item.SourceRef.Ranges) != 2 {
			t.Fatal(item.SourceRef, want)
		}
	}
	response.FinishReason = "length"
	if _, err := doc.decodeRowResults(response, selected, objects, 1, 1); err == nil {
		t.Fatal("accepted truncated result")
	}
	response = agentgateway.Response{Text: `{"rows":[{"number":2,"items":[]},{"number":2,"items":[]}]}`}
	duplicate, err := doc.decodeRowResults(response, selected[:1], objects, 1, 1)
	if err != nil || duplicate[0].Error == "" {
		t.Fatal(duplicate, err)
	}
}

func TestMaterialSourceRowsUseCurrentDirectoryForAtomicOwnership(t *testing.T) {
	catalog := model.MaterialObjectCatalog{
		Directory: model.MaterialObjectDirectory{
			GlobalDisplayName: "通用说明",
			Objects:           []model.MaterialNamedObject{{ID: 21, DisplayName: "轻享套装"}},
			RemovedLinkIDs:    []int64{22},
			LinkDisplayNames:  map[int64]string{20: "加量款"},
		},
		Stores: []model.MaterialStoreObject{{Name: "北门店"}},
	}
	links := materialAnalysisLinks([]materialLinkObject{
		{ID: 20, LinkKey: "1", DisplayName: "旧显示名"}, {ID: 21, LinkKey: "2"}, {ID: 22, LinkKey: "3"},
	}, catalog.Directory)
	raw := "轻享套装：商品甲，内含3件，售价66元；加量款：售价88元。\n轻享套装和加量款都支持预约。\n通用说明：活动仅限本周。\n某款可以自提。"
	rows := splitMaterialSourceRows(raw)
	doc := newMaterialImportDocument(raw, links, catalog.Stores)
	objects := materialRowObjects(doc.links, catalog)
	var payload struct{ Objects []materialRowObject }
	if err := json.Unmarshal([]byte(doc.rowAnalysisBody(rows, objects, nil, "")), &payload); err != nil {
		t.Fatal(err)
	}
	wantObjects := []materialRowObject{
		{MaterialObjectTarget: model.MaterialObjectTarget{Kind: "plan"}, DisplayName: "通用说明"},
		{MaterialObjectTarget: model.MaterialObjectTarget{Kind: "product", LinkID: 20, LinkKey: "1"}, DisplayName: "加量款"},
		{MaterialObjectTarget: model.MaterialObjectTarget{Kind: "product", LinkID: 21, LinkKey: "2"}, DisplayName: "2号链接"},
		{MaterialObjectTarget: model.MaterialObjectTarget{Kind: "object", ObjectID: 21}, DisplayName: "轻享套装"},
		{MaterialObjectTarget: model.MaterialObjectTarget{Kind: "store", StoreName: "北门店"}, DisplayName: "北门店"},
	}
	if !reflect.DeepEqual(payload.Objects, wantObjects) {
		t.Fatalf("unexpected model directory: %+v", payload.Objects)
	}
	response := agentgateway.Response{Text: `{"rows":[
		{"number":1,"items":[
			{"key":"product_name","value":"商品甲","source_sentence":"轻享套装：商品甲，内含3件，售价66元；加量款：售价88元。","object_targets":[{"kind":"object","object_id":21}]},
			{"key":"quantity","value":"3件","source_sentence":"轻享套装：商品甲，内含3件，售价66元；加量款：售价88元。","object_targets":[{"kind":"object","object_id":21}]},
			{"key":"price","value":"66元","source_sentence":"轻享套装：商品甲，内含3件，售价66元；加量款：售价88元。","object_targets":[{"kind":"object","object_id":21}]},
			{"key":"price","value":"88元","source_sentence":"轻享套装：商品甲，内含3件，售价66元；加量款：售价88元。","object_targets":[{"kind":"product","link_id":20}]}]},
		{"number":2,"items":[{"key":"booking","value":"支持预约","source_sentence":"轻享套装和加量款都支持预约。","object_targets":[{"kind":"object","object_id":21},{"kind":"product","link_id":20}]}]},
		{"number":3,"items":[{"key":"period","value":"本周","source_sentence":"通用说明：活动仅限本周。","object_targets":[{"kind":"plan"}]}]},
		{"number":4,"items":[
			{"key":"pickup","value":"自提","source_sentence":"某款可以自提。","object_targets":[{"kind":"product","link_id":22}]},
			{"key":"pickup","value":"可以自提","source_sentence":"某款可以自提。","object_targets":[{"kind":"store","store_name":"目录外门店"}]},
			{"key":"pickup","value":"支持自提","source_sentence":"某款可以自提。","scope_kind":"plan"}]}
	]}`}
	result, err := doc.decodeRowResults(response, rows, objects, 1, 1)
	if err != nil || len(result) != 4 {
		t.Fatal(result, err)
	}
	wantTargets := [][]model.MaterialObjectTarget{
		{wantObjects[3].MaterialObjectTarget}, {wantObjects[3].MaterialObjectTarget},
		{wantObjects[3].MaterialObjectTarget}, {wantObjects[1].MaterialObjectTarget},
		{wantObjects[3].MaterialObjectTarget, wantObjects[1].MaterialObjectTarget}, {wantObjects[0].MaterialObjectTarget},
	}
	index := 0
	for _, group := range result[:3] {
		for _, item := range group.Items {
			if !reflect.DeepEqual(item.ObjectTargets, wantTargets[index]) || item.SourceSentence != rows[group.Number-1].Text {
				t.Fatalf("item %d has wrong targets or evidence: %+v", index, item)
			}
			index++
		}
	}
	if index != len(wantTargets) || len(result[3].Items) != 3 {
		t.Fatal(result)
	}
	for _, item := range result[3].Items {
		if item.ScopeKind != "unresolved" || len(item.ObjectTargets) != 0 || !item.NeedsConfirmation {
			t.Fatalf("invalid or missing target was assigned: %+v", item)
		}
	}
}

func TestMaterialSourceRowsKeepQualifiedEstimateWithUnresolvedOwner(t *testing.T) {
	raw := "线下门店购买，加上运费后，一只算下来一百多。\n备注："
	rows := splitMaterialSourceRows(raw)
	doc := newMaterialImportDocument(raw, nil, nil)
	objects := materialRowObjects(doc.links, model.MaterialObjectCatalog{})
	response := agentgateway.Response{Text: `{"rows":[{"number":1,"items":[{
		"destination":"fact","key":"unit_total_with_shipping","topic":"含运费单只总价","value":"一百多元",
		"scope_kind":"unresolved","object_targets":[],"fact_type":"conditional",
		"conditions":[{"field":"channel","op":"eq","values":["线下门店"],"raw":"线下门店购买"},
		{"field":"other","op":"eq","values":["含运费"],"raw":"加上运费后"}],
		"source_sentence":"线下门店购买，加上运费后，一只算下来一百多。","source_quote":"一百多"
	}]},{"number":2,"items":[]}]}`}
	results, err := doc.decodeRowResults(response, rows, objects, 1, 1)
	if err != nil || len(results) != 2 || len(results[0].Items) != 1 || results[0].Error != "" || len(results[0].Warnings) != 0 {
		t.Fatal(results, err)
	}
	item := results[0].Items[0]
	if item.Value != "一百多元" || item.SourceSentence != rows[0].Text || item.ScopeKind != "unresolved" ||
		!item.NeedsConfirmation || item.ConditionIssue != "" || len(materialPredicates(item.Conditions)) != 2 {
		t.Fatalf("unexpected estimate: %+v", item)
	}
	if results[1].Error != "" || len(results[1].Items) != 0 {
		t.Fatal("empty result should remain valid", results[1])
	}
	response.Text = strings.Replace(response.Text, "一百多元", "120元", 1)
	results, err = doc.decodeRowResults(response, rows, objects, 1, 1)
	if err != nil || len(results[0].Items) != 0 || results[0].Error == "" {
		t.Fatal("an invented exact price must not pass the evidence check", results, err)
	}
}
