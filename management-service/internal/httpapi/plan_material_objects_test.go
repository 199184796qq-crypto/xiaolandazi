package httpapi

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestMaterialObjectTargetsAndStableLinks(t *testing.T) {
	links := []materialLinkObject{{101, "1号链接", "商品"}, {205, "2号链接", "卡券"}}
	for _, tc := range []struct {
		name string
		in   model.PlanMaterialClaimInput
		want []model.MaterialObjectTarget
	}{
		{"legacy number resolves actual ID", model.PlanMaterialClaimInput{ScopeKind: "product", ProductLinkID: 2}, []model.MaterialObjectTarget{{Kind: "product", LinkID: 205, LinkKey: "2号链接"}}},
		{"global", model.PlanMaterialClaimInput{ScopeKind: "plan"}, []model.MaterialObjectTarget{{Kind: "plan"}}},
		{"generic store is global", model.PlanMaterialClaimInput{ScopeKind: "store", StoreName: "本直播间"}, []model.MaterialObjectTarget{{Kind: "plan"}}},
		{"real store", model.PlanMaterialClaimInput{ScopeKind: "store", StoreName: "王家店"}, []model.MaterialObjectTarget{{Kind: "store", StoreName: "王家店"}}},
		{"unlinked voucher stays pending", model.PlanMaterialClaimInput{ScopeKind: "voucher", MaterialClaimDetails: model.MaterialClaimDetails{ScopeName: "双人券"}}, []model.MaterialObjectTarget{}},
		{"unknown explicit target", model.PlanMaterialClaimInput{MaterialClaimDetails: model.MaterialClaimDetails{ObjectTargets: []model.MaterialObjectTarget{{Kind: "product", LinkID: 999}}}}, []model.MaterialObjectTarget{}},
		{"mixed global never broadens", model.PlanMaterialClaimInput{MaterialClaimDetails: model.MaterialClaimDetails{ObjectTargets: []model.MaterialObjectTarget{{Kind: "plan"}, {Kind: "product", LinkID: 101}}}}, []model.MaterialObjectTarget{}},
		{"explicit empty beats legacy global", model.PlanMaterialClaimInput{ScopeKind: "plan", MaterialClaimDetails: model.MaterialClaimDetails{ObjectTargets: []model.MaterialObjectTarget{}}}, []model.MaterialObjectTarget{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolveMaterialTargets(&tc.in, links)
			if !reflect.DeepEqual(tc.in.ObjectTargets, tc.want) {
				t.Fatalf("got %+v want %+v", tc.in.ObjectTargets, tc.want)
			}
		})
	}
	// Repeated labels never turn the display number into an arbitrary DB ID.
	it := model.PlanMaterialClaimInput{ScopeKind: "product", ProductLinkID: 1}
	resolveMaterialTargets(&it, append(links, materialLinkObject{300, "1号链接", "另一卡"}))
	if it.ScopeKind != "unresolved" {
		t.Fatal("ambiguous link silently selected")
	}
}

func TestMaterialMultiObjectSingleDocument(t *testing.T) {
	it := model.PlanMaterialClaimInput{Content: "支持切块", Topic: "切块服务", MaterialClaimDetails: model.MaterialClaimDetails{Destination: "fact", ObjectTargets: []model.MaterialObjectTarget{{Kind: "product", LinkID: 205}, {Kind: "product", LinkID: 101}, {Kind: "product", LinkID: 101}}}}
	resolveMaterialTargets(&it, []materialLinkObject{{101, "1号链接", "商品"}, {205, "2号链接", "卡券"}})
	if len(it.ObjectTargets) != 2 {
		t.Fatalf("targets: %+v", it.ObjectTargets)
	}
	text := materialDocumentText(it)
	if strings.Count(text, "支持切块") != 1 || !strings.Contains(text, "1号链接") || !strings.Contains(text, "2号链接") {
		t.Fatal(text)
	}
	raw := `{"items":[{"key":"cutting","content":"支持切块","object_targets":[{"kind":"product","link_id":101},{"kind":"product","link_id":205}]}]}`
	rows, err := parseMaterialClaimItems(raw)
	if err != nil || len(rows) != 1 || len(rows[0].ObjectTargets) != 2 {
		t.Fatalf("multi-target row cloned/lost: %+v %v", rows, err)
	}
	rows, err = parseMaterialClaimItems(`{"items":[{"key":"price","content":"49元"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	row := normalizeMaterialClaim(rows[0], 14, 4)
	if row.ScopeKind != "unresolved" {
		t.Fatal("missing model ownership became global")
	}
	rows = expandMaterialClaim(model.PlanMaterialClaimInput{Content: "1号、2号均为87.6元", ScopeKind: "plan"})
	if len(rows) != 1 || len(rows[0].ObjectTargets) != 2 {
		t.Fatalf("common body duplicated: %+v", rows)
	}
}

func TestMaterialStoreMembershipValidation(t *testing.T) {
	links := []materialLinkObject{{101, "1号链接", "商品"}, {205, "2号链接", "卡券"}}
	stores := []model.MaterialStoreObject{{Name: " 王家店 ", LinkIDs: []int64{205, 101, 205}}, {Name: "李家店", LinkIDs: []int64{}}}
	if err := validateMaterialStores(stores, links); err != nil {
		t.Fatal(err)
	}
	if stores[0].Name != "王家店" || !reflect.DeepEqual(stores[0].LinkIDs, []int64{101, 205}) {
		t.Fatal(stores)
	}
	for _, bad := range [][]model.MaterialStoreObject{{{Name: "本直播间"}}, {{Name: "王家店", LinkIDs: []int64{999}}}, {{Name: "王家店"}, {Name: "王家店"}}, {{Name: ""}}} {
		if validateMaterialStores(bad, links) == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	input := model.PlanMaterialClaimInput{Content: "支持", MaterialClaimDetails: model.MaterialClaimDetails{Destination: "fact", ObjectTargets: []model.MaterialObjectTarget{{Kind: "store", StoreName: strings.Repeat("店", 101)}}}}
	normalizeMaterialTargets(&input)
	if input.ScopeKind != "unresolved" {
		t.Fatal("oversized target accepted")
	}
	encoded, _ := json.Marshal(stores)
	if strings.Contains(string(encoded), "service") {
		t.Fatal("service object created")
	}
}
