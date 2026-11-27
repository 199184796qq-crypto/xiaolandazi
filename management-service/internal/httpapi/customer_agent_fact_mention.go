package httpapi

import (
	"encoding/json"
	"errors"
	"slices"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type agentFactSelection struct {
	ID              int64  `json:"id"`
	Object          string `json:"object"`
	Revision        string `json:"revision"`
	ObjectsRevision string `json:"objects_revision,omitempty"`
}

type agentFactEditReference struct {
	Object     string          `json:"object"`
	Attribute  string          `json:"attribute"`
	Value      string          `json:"value"`
	Content    string          `json:"content,omitempty"`
	Source     string          `json:"source"`
	Polarity   string          `json:"polarity,omitempty"`
	Conditions json.RawMessage `json:"conditions,omitempty"`
	Exceptions json.RawMessage `json:"exceptions,omitempty"`
	Shared     bool            `json:"shared,omitempty"`
}

func selectedAgentFact(selection agentFactSelection, claims []model.PlanMaterialClaim, catalog model.MaterialObjectCatalog, links []db.QALink) (*agentFactEditReference, error) {
	changed := errors.New("所选属性已变化，请重新输入 @ 选择")
	index := slices.IndexFunc(claims, func(item model.PlanMaterialClaim) bool { return item.ID == selection.ID })
	if index < 0 || db.MaterialClaimRevision(claims[index]) != selection.Revision {
		return nil, changed
	}
	claim := claims[index]
	products := make([]materialLinkObject, 0, len(links))
	for _, link := range links {
		products = append(products, materialLinkObject{
			ID: link.ID, LinkKey: link.LinkKey, ProductName: link.ProductName, DisplayName: link.FieldLabels["display_name"],
		})
	}
	objects := materialAnalysisLinks(products, catalog.Directory)
	item := db.MaterialClaimInput(claim)
	resolveMaterialTargets(&item, objects)
	var objectLabel string
	if selection.Object == "unresolved" && len(item.ObjectTargets) == 0 {
		objectLabel = "待归属"
	}
	for _, target := range item.ObjectTargets {
		if materialFactObjectKey(target) != selection.Object || selection.Object == "" {
			continue
		}
		objectLabel = materialTargetLabel(target)
		for _, object := range materialRowObjects(objects, catalog) {
			if materialFactObjectKey(object.MaterialObjectTarget) == selection.Object && object.DisplayName != "" {
				objectLabel = object.DisplayName
				break
			}
		}
		break
	}
	if objectLabel == "" {
		return nil, changed
	}
	return &agentFactEditReference{
		Object: objectLabel, Attribute: firstAgentFactText(item.Attribute, item.Topic, item.ClaimKey, "属性"),
		Value:   firstAgentFactText(item.Value, item.Content),
		Source:  firstAgentFactText(item.SourceSentence, item.SourceQuote),
		Content: item.Content, Polarity: item.Polarity, Conditions: item.Conditions, Exceptions: item.Exceptions,
		Shared: len(item.ObjectTargets) > 1,
	}, nil
}

func firstAgentFactText(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
