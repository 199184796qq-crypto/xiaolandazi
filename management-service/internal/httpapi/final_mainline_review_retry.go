package httpapi

import (
	"context"
	"errors"
	"strings"
	"time"

	"livecompanion/management/internal/anchorcycle"
)

// Retry the audit of the same immutable text, never its generation or local
// rewrite. Each attempt owns its deadline; cancellation of the live run always
// wins. Both raw receipts remain available even when the second one also fails.
func runFinalMainlineReview(ctx context.Context, timeout time.Duration, audit *anchorcycle.FinalContentAudit, call func(context.Context) (anchorcycle.FinalReviewAttempt, error)) (lastErr error) {
	audit.Decision, audit.Reason, audit.Quote, audit.Error = "", "", "", ""
	audit.Attempts = nil
	audit.ExchangeID, audit.InputTokens, audit.OutputTokens, audit.LatencyMS = 0, 0, 0, 0
	audit.Raw = ""
	defer func() {
		if lastErr != nil {
			audit.Error = lastErr.Error()
		}
	}()
	var last error
	for i := 0; i < 2; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		started := time.Now()
		result, err := call(attemptCtx)
		if err == nil {
			err = attemptCtx.Err()
		}
		cancel()
		result.LatencyMS = time.Since(started).Milliseconds()
		conclusion := anchorcycle.ReviewConclusion{}
		if err == nil {
			conclusion, err = anchorcycle.ParseReviewConclusion(result.Raw)
			if err == nil && conclusion.Decision == "有问题" && strings.TrimSpace(conclusion.Reason) == "" {
				err = errors.New("审核判有问题但未说明具体原因")
			}
		}
		if err != nil {
			result.Error = err.Error()
		}
		audit.Attempts = append(audit.Attempts, result)
		audit.ExchangeID, audit.Raw = result.ExchangeID, result.Raw
		audit.InputTokens += result.InputTokens
		audit.OutputTokens += result.OutputTokens
		audit.LatencyMS += result.LatencyMS
		if err == nil {
			audit.Decision, audit.Reason, audit.Quote = conclusion.Decision, conclusion.Reason, conclusion.Quote
			if audit.Quote != "" && !strings.Contains(audit.Reason, audit.Quote) {
				audit.Reason = "原句：“" + audit.Quote + "”；" + audit.Reason
			}
			return nil
		}
		last = err
	}
	return last
}
