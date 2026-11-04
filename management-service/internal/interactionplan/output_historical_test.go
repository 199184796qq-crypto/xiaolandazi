package interactionplan

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// Supplier text only; no credentials, signed URLs or live calls. 51669's
// completion reason was not archived in the first fixture, so it remains
// unknown there. The full historical pull also covers the literal 留空 marker.
func TestSession640OriginalFormatReceipts(t *testing.T) {
	raw, err := os.ReadFile("testdata/session640-format.json")
	if err != nil {
		t.Fatal(err)
	}
	var receipts []struct {
		ID           int    `json:"exchange_id"`
		Raw          string `json:"raw"`
		Body         string `json:"body"`
		FinishReason string `json:"finish_reason"`
	}
	if err := json.Unmarshal(raw, &receipts); err != nil {
		t.Fatal(err)
	}
	for _, receipt := range receipts {
		t.Run(fmt.Sprint(receipt.ID), func(t *testing.T) {
			got := parseBatch(fixture(t), batchFixture(1), receipt.Raw, receipt.FinishReason)
			if got.Items[0].Err != nil || got.Items[0].Body != receipt.Body {
				t.Fatalf("original receipt body lost: err=%v body=%q", got.Items[0].Err, got.Items[0].Body)
			}
		})
	}
}
