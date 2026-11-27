package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

func TestMaterialImportUniversalChunkBoundaries(t *testing.T) {
	raw := "【/】   [/]资料 A：长度 4.75m[/]资料 B：" + strings.Repeat("🧵混合字体 text", 190) + "【/】\r\n"
	chunks := materialSourceChunks(raw, materialChunkRunes)
	if len(chunks) < 3 {
		t.Fatal(chunks)
	}
	var recovered strings.Builder
	for i, chunk := range chunks {
		main := raw[chunk.start:chunk.end]
		if !utf8.ValidString(main) || utf8.RuneCountInString(main) > materialChunkRunes || strings.Contains(main, "[/]") || strings.Contains(main, "【/】") {
			t.Fatal(chunk)
		}
		left, right := chunk.context(raw)
		if left < chunk.sectionStart || right > chunk.sectionEnd || !utf8.ValidString(raw[left:right]) {
			t.Fatal(chunk)
		}
		if i > 0 && chunks[i-1].sectionStart == chunk.sectionStart && chunks[i-1].end != chunk.start {
			t.Fatal("gap in source", chunk)
		}
		recovered.WriteString(main)
	}
	if got := recovered.String(); got != "资料 A：长度 4.75m资料 B："+strings.Repeat("🧵混合字体 text", 190) {
		t.Fatal("lost inline content")
	}
}

func TestMaterialImportEvidenceSupportsIndependentAttributes(t *testing.T) {
	original := "型号甲\r\n每箱12套，湿度超过60%时不可堆叠。"
	chunk := materialSourceChunks(original, materialChunkRunes)[0]
	doc := newMaterialImportDocument(original, nil, nil)
	item := model.PlanMaterialClaimInput{
		Value: "12套", Content: "每箱12套",
		SourceSentence:       "每箱12套，湿度超过60%时不可堆叠。",
		MaterialClaimDetails: model.MaterialClaimDetails{SourceContext: []string{"型号甲"}},
	}
	doc.bindEvidence(&item, chunk)
	if item.SourceRef == nil || len(item.SourceRef.Ranges) != 2 {
		t.Fatal(item.SourceIssue, item.SourceRef)
	}
	if err := validateMaterialImportEvidence(&item, original); err != nil {
		t.Fatal(err)
	}
	changed := item
	changed.Value = "13套"
	if validateMaterialImportEvidence(&changed, original) == nil {
		t.Fatal("accepted a number absent from the source")
	}
	changed = item
	changed.SourceSentence = "每箱13套"
	if validateMaterialImportEvidence(&changed, original) == nil {
		t.Fatal("accepted a changed quote")
	}
	changed = item
	changed.Conditions = json.RawMessage(`[{"op":"eq","raw":"室外使用"}]`)
	if validateMaterialImportEvidence(&changed, original) == nil {
		t.Fatal("accepted an unsupported condition")
	}
}

func TestEditedMaterialSourceKeepsInitialEvidenceAndValidatesCorrection(t *testing.T) {
	original := "每箱12套，限周末自取。"
	initial := &model.MaterialSourceReference{ID: materialSourceID(original), Ranges: []model.MaterialSourceRange{{Start: 0, End: len(original)}}}
	claim := model.PlanMaterialClaimInput{
		Value: "13套", Content: "每箱13套",
		Conditions:     json.RawMessage(`[{"op":"eq","raw":"限周末自取"}]`),
		SourceSentence: " 每箱13套，限周末自取。 ", SourceQuote: "每箱12套", SourceSentenceEdited: true,
		MaterialClaimDetails: model.MaterialClaimDetails{SourceRef: initial, SourceIssue: "原句待核对"},
	}
	if err := bindEditedMaterialSource(&claim); err != nil {
		t.Fatal(err)
	}
	if claim.SourceSentence != "每箱13套，限周末自取。" || claim.SourceQuote != claim.SourceSentence ||
		claim.InitialSourceRef != initial || claim.SourceRef.ID == initial.ID || claim.SourceSentenceEdited || claim.SourceIssue != "" {
		t.Fatalf("incorrect corrected source: %+v", claim)
	}
	if err := validateMaterialImportEvidence(&claim, claim.SourceSentence); err != nil {
		t.Fatal(err)
	}

	claim.SourceSentence, claim.SourceSentenceEdited = "  核对后确认每箱13套，限周末自取。", true
	if err := bindEditedMaterialSource(&claim); err != nil || claim.InitialSourceRef != initial {
		t.Fatalf("second edit lost the initial source: %+v, %v", claim, err)
	}

	for _, invalid := range []string{"", "每箱12套，限周末自取。", "每箱13套。"} {
		edited := claim
		edited.SourceSentence = invalid
		edited.SourceSentenceEdited = true
		if err := bindEditedMaterialSource(&edited); err == nil || edited.SourceRef != claim.SourceRef {
			t.Fatalf("accepted invalid edit or mutated its evidence: %q", invalid)
		}
	}
}

func TestMaterialImportModelJSONKeepsTextAndRejectsAmbiguity(t *testing.T) {
	value, err := decodeMaterialImportJSON("说明：\n" + `{"items":[{"content":"保留,}和False","source_sentence":"保留,}和False",},],}`)
	if err != nil {
		t.Fatal(err)
	}
	items, err := materialClaimItems(value, false)
	if err != nil || len(items) != 1 || items[0].Content != "保留,}和False" {
		t.Fatal(items, err)
	}
	for _, invalid := range []string{
		`{"items":[{"value":"A","value":"B"}]}`,
		`{"items":[{"value":"A"}`,
		`{"items":[]}{"items":[]}`,
	} {
		if _, err := decodeMaterialImportJSON(invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
}

func TestMaterialImportRepeatedFieldUsesItsObjectHeading(t *testing.T) {
	for _, tc := range []struct {
		name, raw, quoted, want, heading string
		links                            []materialLinkObject
		stores                           []model.MaterialStoreObject
		owner                            model.MaterialObjectTarget
	}{
		{
			name:   "same field across links with formatting differences",
			raw:    "【资料】\r\n1号链接：\r\n规格： 900g(一整只)\r\n2号链接：\r\n规格： 900g(一整只)",
			quoted: "规格:900g（一整只）", want: "规格： 900g(一整只)", heading: "2号链接：",
			links: []materialLinkObject{{ID: 81, LinkKey: "1号"}, {ID: 82, LinkKey: "2"}},
			owner: model.MaterialObjectTarget{Kind: "product", LinkID: 82},
		},
		{
			name:   "store catalog headings",
			raw:    "城西店：\n服务：到店自提\n【城东店】\n服务：到店自提",
			quoted: "服务：到店自提", want: "服务：到店自提", heading: "【城东店】",
			stores: []model.MaterialStoreObject{{Name: "城西店"}, {Name: "城东店"}},
			owner:  model.MaterialObjectTarget{Kind: "store", StoreName: "城东店"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := newMaterialImportDocument(tc.raw, tc.links, tc.stores)
			item := model.PlanMaterialClaimInput{
				Value: "可用", Content: "可用", SourceSentence: tc.quoted,
				MaterialClaimDetails: model.MaterialClaimDetails{ObjectTargets: []model.MaterialObjectTarget{tc.owner}},
			}
			doc.bindEvidence(&item, materialSourceChunks(tc.raw, materialChunkRunes)[0])
			if item.SourceIssue != "" || item.SourceRef == nil || len(item.SourceRef.Ranges) != 2 {
				t.Fatal(item.SourceIssue, item.SourceRef)
			}
			main, heading := item.SourceRef.Ranges[0], item.SourceRef.Ranges[1]
			if item.SourceSentence != tc.want || tc.raw[heading.Start:heading.End] != tc.heading || main.Start < heading.End {
				t.Fatal("incorrect source block", item.SourceSentence, item.SourceRef.Ranges)
			}
			if err := validateMaterialImportEvidence(&item, tc.raw); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMaterialImportRepeatedFieldStillNeedsUnambiguousEvidence(t *testing.T) {
	links := []materialLinkObject{{ID: 81, LinkKey: "1"}, {ID: 82, LinkKey: "2"}}
	for _, tc := range []struct {
		name, raw string
		owner     int64
	}{
		{"same owner repeated", "1号链接：\n容量：3L\n容量：3L", 81},
		{"missing owner", "1号链接：\n容量：3L\n2号链接：\n容量：3L", 0},
		{"unknown heading breaks inheritance", "1号链接：\n其它内容\n3号链接：\n容量：3L\n4号链接：\n容量：3L", 81},
		{"explicit separator breaks inheritance", "1号链接：\n其它内容[/]容量：3L\n容量：3L", 81},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := newMaterialImportDocument(tc.raw, links, nil)
			item := model.PlanMaterialClaimInput{
				SourceSentence:       "容量：3L",
				MaterialClaimDetails: model.MaterialClaimDetails{ObjectTargets: []model.MaterialObjectTarget{{Kind: "product", LinkID: tc.owner}}},
			}
			chunks := materialSourceChunks(tc.raw, materialChunkRunes)
			doc.bindEvidence(&item, chunks[len(chunks)-1])
			if item.SourceIssue == "" || item.SourceRef != nil {
				t.Fatal("accepted ambiguous evidence", item.SourceRef)
			}
		})
	}
}

func TestMaterialImportOwnerContextCanReferenceHeadingOutsideOverlap(t *testing.T) {
	raw := "【北区服务】\n" + strings.Repeat("独立条款。\n", 120) + "受理时间： 每周一\n"
	doc := newMaterialImportDocument(raw, nil, []model.MaterialStoreObject{{Name: "北区服务"}})
	chunks := materialSourceChunks(raw, 150)
	chunk := chunks[len(chunks)-1]
	left, _ := chunk.context(raw)
	if left < len("【北区服务】") {
		t.Fatal("heading still in overlap")
	}
	item := model.PlanMaterialClaimInput{
		SourceSentence: "受理时间:每周一", Value: "每周一", Content: "每周一受理",
		MaterialClaimDetails: model.MaterialClaimDetails{SourceContext: []string{"北区服务"}},
	}
	doc.bindEvidence(&item, chunk)
	if item.SourceRef == nil || len(item.SourceRef.Ranges) != 2 {
		t.Fatal(item.SourceIssue, item.SourceRef)
	}
	if err := validateMaterialImportEvidence(&item, raw); err != nil {
		t.Fatal(err)
	}
}

func TestMaterialImportDeduplicationPrefersVerifiedSource(t *testing.T) {
	ambiguous := model.PlanMaterialClaimInput{Content: "每周一受理", MaterialClaimDetails: model.MaterialClaimDetails{SourceIssue: "待核对"}}
	verified := ambiguous
	verified.SourceIssue = ""
	verified.SourceRef = &model.MaterialSourceReference{ID: "verified"}
	items := dedupeMaterialClaims([]model.PlanMaterialClaimInput{ambiguous, verified})
	if len(items) != 1 || items[0].SourceRef != verified.SourceRef || items[0].SourceIssue != "" {
		t.Fatal("lost verified evidence", items)
	}
}
