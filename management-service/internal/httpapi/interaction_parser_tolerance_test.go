package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
)

func TestPreparedTolerantReplyKeepsTaskIdentityDiagnosticsAndExpiry(t *testing.T) {
	for _, expire := range []bool{false, true} {
		in := httpBatchFixture(2)
		items, err := batchInputsV2("batch", in, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		got, _, err := preparedBatchResultsV2(context.Background(), httpBatchSnapshot(t), in, items, func(_ context.Context, req agentgateway.Request) (agentgateway.Response, error) {
			calls++
			if expire {
				in[0].ExpiresAt = time.Now().Add(-time.Second)
			}
			return agentgateway.Response{Text: "编号：U02\n类型：提问\n歧义：否\n意图：发货咨询｜置信度：80｜置信度：30｜\n建议：后台说明不能口播\n话术：乙项按方案发货。\n结束：U02\n编号：U01\n类型：提问\n类型：提问\n歧义：否\n意图：咨询｜置信度：90\n正文：甲项按方案发货。\n结束：U01", FinishReason: "stop"}, nil
		})
		if err != nil || calls != 1 || got[1].Error != "" || got[1].Envelope.Body != "乙项按方案发货。" || got[1].Task != in[1].Task || got[1].Envelope.Revision != in[1].Revision || len(got[1].Parse.Warnings) == 0 {
			t.Fatal(got, err, calls)
		}
		if expire {
			if got[0].Error == "" || got[0].Envelope.Body != "" || got[0].Parse.Outcome != "rejected" {
				t.Fatal("expired extraction became executable", got)
			}
		} else if got[0].Error != "" || got[0].Envelope.Body != "甲项按方案发货。" {
			t.Fatal(got)
		}
		raw, _ := json.Marshal(got[1])
		if !strings.Contains(string(raw), `"parse":`) || !strings.Contains(string(raw), `"raw_sha256":`) || got[1].Parse.FinishReason != "stop" {
			t.Fatal(string(raw))
		}
		// These diagnostics must not contaminate future model inputs/history.
		envelope, _ := json.Marshal(got[1].Envelope)
		if strings.Contains(string(envelope), "raw_sha256") || strings.Contains(string(envelope), "conflicting_confidence") || strings.Contains(string(envelope), "后台说明") {
			t.Fatal(string(envelope))
		}
	}
}

func TestTolerantParserStillDiscardsCancelledModelCompletion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := httpBatchFixture(1)
	items, _ := batchInputsV2("cancel", in, time.Now())
	got, _, err := preparedBatchResultsV2(ctx, httpBatchSnapshot(t), in, items, func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
		cancel()
		return agentgateway.Response{Text: "编号：U01\n类型：提问\n歧义：否\n意图：咨询｜置信度：80｜\n话术：按方案发货。\n结束：U01", FinishReason: "stop"}, nil
	})
	if err != context.Canceled || len(got) != 0 {
		t.Fatal(got, err)
	}
}

// Structural fallback still goes through the existing task/revision/expiry
// binding. A syntactically recovered body is not a playback receipt.
func TestPreparedFiveFieldGroupFallbackKeepsTaskBinding(t *testing.T) {
	for _, expire := range []bool{false, true} {
		in := httpBatchFixture(2)
		items, err := batchInputsV2("group-fallback", in, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		got, _, err := preparedBatchResultsV2(context.Background(), httpBatchSnapshot(t), in, items, func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
			calls++
			if expire {
				in[1].ExpiresAt = time.Now().Add(-time.Second)
			}
			return agentgateway.Response{Text: "意图：甲项咨询\n类型：提问\n歧义：否\n澄清句：\n话术：甲项按方案说明。\n意图：乙项咨询\n类型：提问\n歧义：否\n澄清句：\n话术：乙项按方案说明。", FinishReason: "stop"}, nil
		})
		if err != nil || calls != 1 || len(got) != 2 || got[0].Error != "" || got[0].Task != in[0].Task || got[0].Envelope.Body != "甲项按方案说明。" || got[0].Parse.Assignment != "group_order" {
			t.Fatal(got, err, calls)
		}
		if expire {
			if got[1].Error == "" || got[1].Envelope.Body != "" || got[1].Parse.Reason != "candidate_expired" {
				t.Fatal(got)
			}
		} else if got[1].Error != "" || got[1].Task != in[1].Task || got[1].Envelope.Revision != in[1].Revision || got[1].Envelope.Body != "乙项按方案说明。" {
			t.Fatal(got)
		}
	}
}
