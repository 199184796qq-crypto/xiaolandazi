package paidpipeline

import (
	"testing"
	"time"
)

func TestTimeReportRandomDelayStaysWithinSixtyToOneEightyMinutes(t *testing.T) {
	for i := 0; i < 4096; i++ {
		delay := nextTimeReportDelay()
		if delay < time.Hour || delay > 3*time.Hour || delay%time.Minute != 0 {
			t.Fatalf("report delay=%s outside 2 hours plus -60..+60 minutes", delay)
		}
	}
}
