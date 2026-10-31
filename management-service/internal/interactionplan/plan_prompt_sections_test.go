package interactionplan

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/planprompt"
)

type sectionPromptSource struct{ prompt string }

func (s *sectionPromptSource) GetPublishedLiveAgentPlanVersionForRoom(context.Context, int64, int64) (model.LiveAgentPlanVersion, error) {
	return model.LiveAgentPlanVersion{ID: 308, PlanID: 4}, nil
}
func (s *sectionPromptSource) GetLiveAgentPlanPrompt(context.Context, int64, int64) (string, time.Time, error) {
	return s.prompt, time.Time{}, nil
}

func TestSplitPromptUsesExistingInteractionFreezeAndSingleBatchCall(t *testing.T) {
	sections := planprompt.Sections{PersonaMD: "真诚自然", FactsMD: "次日上午发货", SpeechRequirementsMD: "主线单独讲解规则", InteractionRequirementsMD: "只回答当前问题"}
	source := &sectionPromptSource{prompt: sections.Interaction()}
	snapshot, err := Freeze(context.Background(), source, model.LiveRuntimeSession{TenantID: 14, RoomID: 26})
	if err != nil || snapshot.Prompt != sections.Interaction() {
		t.Fatal(snapshot, err)
	}
	for _, n := range []int{1, 10} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			calls := 0
			items := batchFixture(n)
			result, err := GenerateBatch(context.Background(), snapshot, items, func(_ context.Context, request agentgateway.Request) (agentgateway.Response, error) {
				calls++
				all := ""
				for _, message := range request.Messages {
					all += message.Content
				}
				if strings.Contains(all, sections.SpeechRequirementsMD) || strings.Count(all, sections.Interaction()) != 1 {
					t.Fatal("wrong prompt routing", all)
				}
				text := ""
				for _, item := range items {
					text += blockText(item.ID, "一般次日上午发货。")
				}
				return agentgateway.Response{Text: text}, nil
			})
			if err != nil || calls != 1 || len(result.Items) != n {
				t.Fatal(result, err, calls)
			}
		})
	}
}
