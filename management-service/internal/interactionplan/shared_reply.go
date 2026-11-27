package interactionplan

import "livecompanion/management/internal/agentgateway"

// ParseReply applies the established five-field parser without buying another
// model request. Both the workbench and the new live route use this contract.
func ParseReply(snapshot Snapshot, input Input, response agentgateway.Response) (ItemResult, error) {
	items := []BatchItem{{ID: "U01", Input: input}}
	if err := ValidateBatch(items); err != nil {
		return ItemResult{}, err
	}
	result := parseBatch(snapshot, items, response.Text, response.FinishReason)
	item := result.Items[0]
	return item, item.Err
}
