package httpapi

import (
	"testing"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func TestCustomerAgentFactMentionResolvesAuthorizedSnapshot(t *testing.T) {
	fact := model.PlanMaterialClaim{ID: 35, Attribute: "规格", Value: "3斤", SourceSentence: "1号链接每份3斤", ScopeKind: "product", ProductLinkID: 8}
	fact.ObjectTargets = []model.MaterialObjectTarget{{Kind: "product", LinkID: 8}}
	catalog := model.MaterialObjectCatalog{Directory: model.MaterialObjectDirectory{LinkDisplayNames: map[int64]string{8: "徐香猕猴桃"}}}
	links := []db.QALink{{ID: 8, LinkKey: "1号链接"}, {ID: 9, LinkKey: "2号链接"}}
	selectFact := agentFactSelection{ID: fact.ID, Object: "product:8", Revision: db.MaterialClaimRevision(fact)}
	reference, err := selectedAgentFact(selectFact, []model.PlanMaterialClaim{fact}, catalog, links)
	if err != nil || reference.Object != "徐香猕猴桃" || reference.Attribute != "规格" ||
		reference.Value != "3斤" || reference.Source != "1号链接每份3斤" || reference.Shared {
		t.Fatal(reference, err)
	}
	for _, invalid := range []agentFactSelection{
		{ID: 99, Object: selectFact.Object, Revision: selectFact.Revision},
		{ID: selectFact.ID, Object: "product:9", Revision: selectFact.Revision},
		{ID: selectFact.ID, Object: selectFact.Object, Revision: "outdated"},
	} {
		if _, err := selectedAgentFact(invalid, []model.PlanMaterialClaim{fact}, catalog, links); err == nil {
			t.Fatalf("invalid fact selection accepted: %+v", invalid)
		}
	}
	if _, err := selectedAgentFact(selectFact, nil, catalog, links); err == nil {
		t.Fatal("claim from another plan was accepted")
	}
}
