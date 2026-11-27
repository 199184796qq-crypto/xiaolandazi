package httpapi

import (
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func TestCustomerAgentFactEditPreviewAndScope(t *testing.T) {
	for _, text := range []string{"修改 1号链接 品名 为 青提", "请将第１号链接的商品名称改成青提"} {
		edit, ok := parseAgentFactEdit(text)
		if !ok || edit.linkNumber != 1 || agentFactAttributeKey(edit.attribute) != "商品名称" || edit.value != "青提" {
			t.Fatalf("parse %q: %+v, %v", text, edit, ok)
		}
	}
	for _, text := range []string{"教我怎么修改 1号链接 品名 为 青提", "修改 1号链接 品名", "修改 0号链接 品名 为 青提", "修改 1号链接 品名 为 青提\n还有两件事", "修改 1号链接 品名 为   "} {
		if _, ok := parseAgentFactEdit(text); ok {
			t.Fatalf("unexpected command recognition %q", text)
		}
	}
	edit, _ := parseAgentFactEdit("修改 1号链接 品名 为 青提")
	old := "1号链接的商品是葡萄，规格500克。"
	ref := &model.MaterialSourceReference{ID: materialSourceID(old), Ranges: []model.MaterialSourceRange{{Start: 0, End: len(old)}}}
	claim := model.PlanMaterialClaim{
		ID: 4, Value: "葡萄", Content: "葡萄", Attribute: "商品名称", Topic: "商品名称", Polarity: "positive",
		ScopeKind: "product", ProductLinkID: 10, SourceSentence: old, SourceQuote: "葡萄",
		MaterialClaimDetails: model.MaterialClaimDetails{Destination: "fact", SourceRef: ref,
			ObjectTargets: []model.MaterialObjectTarget{{Kind: "product", LinkID: 10}}},
	}
	other := claim
	other.ID, other.ProductLinkID, other.ObjectTargets = 9, 20, []model.MaterialObjectTarget{{Kind: "product", LinkID: 20}}
	catalog := model.MaterialObjectCatalog{}
	links := []db.QALink{{ID: 10, LinkKey: "1"}, {ID: 20, LinkKey: "2"}}
	matched, err := findAgentFactEdit(edit, []model.PlanMaterialClaim{other, claim}, catalog, links)
	if err != nil || matched.ID != 4 {
		t.Fatal(matched, err)
	}
	next, preview, err := prepareAgentFactEdit(matched, edit)
	if err != nil || preview.NewSource != "1号链接的商品是青提，规格500克。" || preview.OldSource != old ||
		next.Value != "青提" || next.Content != "青提" || next.SourceQuote != "青提" ||
		next.InitialSourceRef != ref || next.SourceRef.ID == ref.ID {
		t.Fatal(next, preview, err)
	}
	duplicate := claim
	duplicate.ID = 5
	if _, err = findAgentFactEdit(edit, []model.PlanMaterialClaim{claim, duplicate}, catalog, links); err == nil {
		t.Fatal("two matching claims were accepted")
	}
	shared := claim
	shared.ObjectTargets = []model.MaterialObjectTarget{{Kind: "product", LinkID: 10}, {Kind: "product", LinkID: 20}}
	if _, err = findAgentFactEdit(edit, []model.PlanMaterialClaim{shared}, catalog, links); err == nil {
		t.Fatal("shared target was accepted")
	}
	for _, text := range []string{"库存19箱", "库存9.5箱", "还有9到9箱", "库存五箱"} {
		if _, ok := replaceAgentFactValue(text, "9", "8"); ok {
			t.Fatalf("unsafe replacement in %q", text)
		}
	}
}

func TestCustomerAgentFactEditGrantRequiresSameActorAndScope(t *testing.T) {
	now := time.Unix(200, 0)
	conversation := strings.Repeat("a", 32)
	want := agentFactEditGrant{ActorID: 7, TenantID: 3, PlanID: 9, ConversationID: conversation,
		Library: "observed library", Objects: "observed objects", Message: "修改 1号链接 品名 为 青提", ExpiresAt: 210}
	token := signAgentFactEdit(want)
	if got, ok := verifyAgentFactEdit(token, 7, 3, 9, conversation, now); !ok || got != want {
		t.Fatal(got, ok)
	}
	for _, test := range []struct {
		token               string
		actor, tenant, plan int64
		conversation        string
		now                 time.Time
	}{
		{token, 8, 3, 9, conversation, now},
		{token, 7, 4, 9, conversation, now},
		{token, 7, 3, 10, conversation, now},
		{token, 7, 3, 9, strings.Repeat("b", 32), now},
		{token, 7, 3, 9, conversation, time.Unix(210, 0)},
		{token + "a", 7, 3, 9, conversation, now},
	} {
		if _, ok := verifyAgentFactEdit(test.token, test.actor, test.tenant, test.plan, test.conversation, test.now); ok {
			t.Fatal("invalid grant accepted")
		}
	}
}
