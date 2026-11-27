package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func TestPrepareAgentFactMentionUpdatesNameValueAndEvidence(t *testing.T) {
	claim := model.PlanMaterialClaim{
		ClaimKey: "delivery", Topic: "不邮寄", Attribute: "不邮寄", Value: "不邮寄", Content: "不邮寄",
		Polarity: "negative", FactType: "negative", SourceQuote: "不邮寄",
		SourceSentence: "青提9.9元3斤，到店自选，不邮寄。", ScopeKind: "product", ProductLinkID: 7,
		MaterialClaimDetails: model.MaterialClaimDetails{Destination: "fact"},
	}
	old, err := prepareAgentFactMention(claim, agentFactEditReference{Attribute: claim.Attribute}, agentFactMentionChanges{Attribute: "配送方式"})
	if err != nil || old.Value != claim.Value || old.SourceSentence != claim.SourceSentence || old.Topic != "配送方式" ||
		old.ClaimKey != "delivery" || old.ProductLinkID != claim.ProductLinkID {
		t.Fatal("rename changed the fact body or identity", old, err)
	}
	ref := agentFactEditReference{Attribute: claim.Attribute, Value: claim.Value, Source: claim.SourceSentence}
	change := agentFactMentionChanges{Attribute: "配送方式", Value: "全国包邮", Polarity: "positive",
		Source: "青提9.9元3斤，到店自选，全国包邮。"}
	claim.SourceRef = &model.MaterialSourceReference{ID: materialSourceID(claim.SourceSentence),
		Ranges: []model.MaterialSourceRange{{Start: 0, End: len(claim.SourceSentence)}}}
	got, err := prepareAgentFactMention(claim, ref, change)
	if err != nil {
		t.Fatal(err)
	}
	if got.Attribute != "配送方式" || got.Topic != "配送方式" || got.Value != "全国包邮" || got.Content != "全国包邮" ||
		got.SourceQuote != "全国包邮" || got.SourceSentence != change.Source || got.Polarity != "positive" || got.FactType != "positive" ||
		got.SourceRef == nil || got.InitialSourceRef == nil || got.InitialSourceRef.ID != claim.SourceRef.ID {
		t.Fatalf("incorrect saved fields: %#v", got)
	}
	withoutPolarity := change
	withoutPolarity.Polarity = ""
	if _, err = prepareAgentFactMention(claim, ref, withoutPolarity); err == nil {
		t.Fatal("accepted a proposal that left negative metadata on a positive fact")
	}
	withoutSource := change
	withoutSource.Source = ""
	if _, err = prepareAgentFactMention(claim, ref, withoutSource); err == nil {
		t.Fatal("accepted a value without reviewed evidence")
	}
	claim.Content = "该链接此前无法发货"
	if _, err = prepareAgentFactMention(claim, ref, change); err == nil {
		t.Fatal("silently changed a paraphrased fact body")
	}
	change.Content = "青提支持全国包邮"
	if got, err = prepareAgentFactMention(claim, ref, change); err != nil || got.Content != change.Content {
		t.Fatal("explicit body change was not accepted", err)
	}
}

func TestFetchAgentFactMentionUsesServerScope(t *testing.T) {
	prior := customerAgentClient
	t.Cleanup(func() { customerAgentClient = prior })
	scope := db.AgentFactEditScope{ActorID: 7, TenantID: 3, PlanID: 4, ConversationID: strings.Repeat("a", 32)}
	preview := strings.Repeat("b", 32)
	customerAgentClient = &http.Client{Transport: agentChatTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method != "POST" || request.URL.Path != "/internal/fact-edit-preview" ||
			request.Header.Get("X-Xiaolan-Account") != "7" {
			t.Fatal("request missing private authentication and scope")
		}
		raw, err := io.ReadAll(request.Body)
		if err != nil || !strings.Contains(string(raw), preview) || !strings.Contains(string(raw), scope.ConversationID) ||
			!strings.Contains(string(raw), `"plan_id":4`) {
			t.Fatal(string(raw), err)
		}
		return &http.Response{StatusCode: http.StatusConflict, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	_, status, err := fetchAgentFactMention(context.Background(), scope, preview)
	if status != http.StatusConflict || err == nil {
		t.Fatal(status, err)
	}
}
