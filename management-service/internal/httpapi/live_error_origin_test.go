package httpapi

import (
	"context"
	"errors"
	"io"
	"livecompanion/management/internal/errordetail"
	"net/http"
	"strings"
	"testing"
)

func TestLiveHandoffRetainsCoreReasonAndHTTPStatus(t *testing.T) {
	resp := &http.Response{StatusCode: 409, Header: http.Header{"X-Request-Id": {"req-core-actual"}}, Body: io.NopCloser(strings.NewReader(`{"error":"program version no longer matches"}`))}
	_, err := decodeLiveHandoffResponse(resp, 42)
	failure := publicLiveFailure(389, err)
	if failure.Code != "core_handoff_unconfirmed" || failure.Stage != "Core音频交付" || failure.HTTPStatus != 409 || failure.RequestID != "req-core-actual" || !strings.Contains(failure.Message, "program version") {
		t.Fatal(failure)
	}
	resp = &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"sequential":true,"version_id":42}`))}
	if _, err = decodeLiveHandoffResponse(resp, 42); err != nil {
		t.Fatal(err)
	}
}

func TestSupplyFailureRetainsTypedCauseAndStopsExactlyAsBefore(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	supply := &cycleSupply{ctx: ctx, cancel: cancel}
	cause := errordetail.New("voice_disabled", "声音配置校验", "小芳声音已停用", "重新选择声音", errors.New("specific cause"))
	supply.fail(cause)
	if !errors.Is(supply.failureCause, cause) || !supply.paused || ctx.Err() == nil {
		t.Fatal("failure origin or cancellation lost")
	}
	failure := publicLiveFailure(389, supply.failureCause)
	if failure.Code != "voice_disabled" || failure.Stage != "声音配置校验" || !strings.Contains(failure.Message, "小芳") {
		t.Fatal(failure)
	}
}
