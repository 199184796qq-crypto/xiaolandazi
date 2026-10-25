package livecost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func costRequest(key string) Request {
	return Request{Key: key, TenantID: 14, RoomID: 29, Kind: KindTTS, Provider: "test-tts", Priority: PriorityNecessary, Reason: "首声供料", Characters: 20}
}
func costReceipt(status string) Receipt { return Receipt{TenantID: 14, RoomID: 29, Status: status} }

func TestReceiptEvidenceMonotonicUnknownNeverRefunded(t *testing.T) {
	r, _ := NormalizeRequest(costRequest("one"))
	entry := Entry{Request: r, Status: StatusReserved, ReservedMicros: r.EstimatedMicros}
	unknown, err := ApplyReceipt(entry, costReceipt(ReceiptUnknown))
	if err != nil || unknown.ReservedMicros != 3200 {
		t.Fatal(unknown, err)
	}
	if _, err = ApplyReceipt(unknown, costReceipt(ReceiptNotSent)); !errors.Is(err, ErrReceiptConflict) {
		t.Fatal("ambiguous call was refunded", err)
	}
	estimate, err := ApplyReceipt(unknown, costReceipt(ReceiptSuccess))
	if err != nil || estimate.ChargedMicros != 3200 || estimate.Receipt.ActualKnown || estimate.ReservedMicros != 0 {
		t.Fatal(estimate, err)
	}
	usage := costReceipt(ReceiptSuccess)
	usage.ActualKnown = true
	usage.ActualMicros = 2880
	usage.Characters = 18
	usage.UsageSource = SourceSupplierUsage
	usage.ProviderRequestID = "provider-one"
	measured, err := ApplyReceipt(estimate, usage)
	if err != nil || measured.ChargedMicros != 2880 {
		t.Fatal(measured, err)
	}
	weaker, err := ApplyReceipt(measured, costReceipt(ReceiptSuccess))
	if err != nil || weaker != measured {
		t.Fatal("estimate replaced usage", weaker, err)
	}
	invoice := usage
	invoice.ActualMicros = 2400
	invoice.UsageSource = SourceSupplierInvoice
	paid, err := ApplyReceipt(measured, invoice)
	if err != nil || paid.ChargedMicros != 2400 {
		t.Fatal(paid, err)
	}
	duplicate, err := ApplyReceipt(paid, invoice)
	if err != nil || duplicate != paid {
		t.Fatal("duplicate invoice charged", duplicate, err)
	}
	invoice.ActualMicros++
	if _, err = ApplyReceipt(paid, invoice); !errors.Is(err, ErrReceiptConflict) {
		t.Fatal("conflicting invoice accepted", err)
	}
}
func TestKnownUnsentReleaseIsIdempotentAndCannotLaterSettle(t *testing.T) {
	r, _ := NormalizeRequest(costRequest("release"))
	entry := Entry{Request: r, Status: StatusReserved, ReservedMicros: r.EstimatedMicros}
	released, err := ApplyReceipt(entry, costReceipt(ReceiptNotSent))
	if err != nil || released.ReservedMicros != 0 {
		t.Fatal(released, err)
	}
	if duplicate, e := ApplyReceipt(released, costReceipt(ReceiptNotSent)); e != nil || duplicate != released {
		t.Fatal(duplicate, e)
	}
	if _, e := ApplyReceipt(released, costReceipt(ReceiptSuccess)); !errors.Is(e, ErrReceiptConflict) {
		t.Fatal(e)
	}
}
func TestEstimatedReceiptCannotClaimSupplierPrice(t *testing.T) {
	r := costReceipt(ReceiptSuccess)
	r.ActualKnown = true
	r.ActualMicros = 1
	r.UsageSource = SourceSentTextEstimate
	if ValidateReceipt(r) == nil {
		t.Fatal("estimated cost presented as actual")
	}
	r.UsageSource = SourceSupplierUsage
	if ValidateReceipt(r) == nil {
		t.Fatal("usage without units accepted")
	}
	r.Characters = 1
	if e := ValidateReceipt(r); e != nil {
		t.Fatal(e)
	}
	r.TenantID = 15
	entry := Entry{Request: costRequest("identity"), Status: StatusReserved}
	if _, e := ApplyReceipt(entry, r); !errors.Is(e, ErrRequestConflict) {
		t.Fatal(e)
	}
}

func TestMeasuredUnitsAtReferencePriceStayEstimateAndAllowZero(t *testing.T) {
	r, _ := NormalizeRequest(costRequest("reference-price"))
	entry := Entry{Request: r, Status: StatusReserved, ReservedMicros: r.EstimatedMicros}
	for _, cost := range []int64{0, 320} {
		receipt := costReceipt(ReceiptSuccess)
		receipt.UsageSource = SourceSentTextEstimate
		receipt.InputTokens = 100
		receipt.EstimatedMicros = &cost
		next, err := ApplyReceipt(entry, receipt)
		if err != nil || next.ChargedMicros != cost || next.Receipt.ActualKnown || next.UsageSource != SourceSentTextEstimate {
			t.Fatal(next, err)
		}
	}
	cost := int64(320)
	bad := costReceipt(ReceiptSuccess)
	bad.EstimatedMicros = &cost
	bad.ActualKnown = true
	bad.UsageSource = SourceSupplierUsage
	bad.Characters = 2
	if ValidateReceipt(bad) == nil {
		t.Fatal("mixed actual and estimate accepted")
	}
}
func TestPriceIsSentTextEstimateNeverDuration(t *testing.T) {
	if SentCharacters("中 文，a\n") != 4 {
		t.Fatal("sent rune count")
	}
	if amount, e := EstimateTTS(10000, 160); e != nil || amount != 1600000 {
		t.Fatal(amount, e)
	}
	if amount, e := EstimateTokens(1000, 500, 100, 800000, 160000, 2000000); e != nil || amount != 680 {
		t.Fatal(amount, e)
	}
	if _, e := EstimateTTS(1<<62, 160); e == nil {
		t.Fatal("overflow")
	}
	r := costRequest("bad")
	r.Priority = PriorityNecessary
	r.Reason = ""
	if _, e := NormalizeRequest(r); e == nil {
		t.Fatal("necessary reason missing")
	}
}

type spoolMemoryStore struct {
	mu       sync.Mutex
	entries  map[string]Entry
	reserved int
	finished int
}

func (s *spoolMemoryStore) ReserveLiveCost(_ context.Context, r Request, _ Policy, _ time.Time) (Reservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = map[string]Entry{}
	}
	if e, ok := s.entries[r.Key]; ok {
		return Reservation{Key: r.Key, Existing: true, Status: e.Status}, nil
	}
	s.entries[r.Key] = Entry{Request: r, Status: StatusReserved, ReservedMicros: r.EstimatedMicros}
	s.reserved++
	return Reservation{Key: r.Key, Allowed: true, Status: StatusReserved}, nil
}
func (s *spoolMemoryStore) FinishLiveCost(_ context.Context, scope Scope, key string, r Receipt, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		return ErrRequestNotFound
	}
	if scope.TenantID != e.Request.TenantID {
		return ErrRequestConflict
	}
	next, err := ApplyReceipt(e, r)
	if err == nil {
		s.entries[key] = next
		s.finished++
	}
	return err
}
func (s *spoolMemoryStore) LiveCostSummary(context.Context, Scope, Policy, time.Time) (Summary, error) {
	return Summary{}, nil
}
func TestUnavailableOptionalDeniedNecessaryDurablyRecordedAndReplayed(t *testing.T) {
	policy := DefaultPolicy()
	policy.SpoolDir = t.TempDir()
	controller := New(nil, policy)
	ctx := context.Background()
	optional := costRequest("optional")
	optional.Priority = PriorityOptional
	if reservation, e := controller.Begin(ctx, optional); !errors.Is(e, ErrAccountingUnavailable) || reservation.Allowed {
		t.Fatal(reservation, e)
	}
	r := costRequest("necessary")
	res, e := controller.Begin(ctx, r)
	if !errors.Is(e, ErrAccountingUnavailable) || !res.Allowed || !res.DegradedRecorded {
		t.Fatal(res, e)
	}
	// An independently constructed controller represents a new process sharing the WAL.
	restarted := New(nil, policy)
	if again, e := restarted.Begin(ctx, r); e != nil || again.Allowed || !again.Existing {
		t.Fatal(again, e)
	}
	if e := restarted.Finish(ctx, r.Key, costReceipt(ReceiptSuccess)); !errors.Is(e, ErrAccountingUnavailable) {
		t.Fatal(e)
	}
	store := &spoolMemoryStore{}
	restarted.store = store
	replay, e := restarted.ReplaySpool(ctx)
	if e != nil || replay.Replayed != 2 || store.reserved != 1 || store.entries[r.Key].Status != StatusSettled {
		t.Fatal(replay, store, e)
	}
	if replay, e = restarted.ReplaySpool(ctx); e != nil || replay.Replayed != 0 {
		t.Fatal(replay, e)
	}
	if again, e := restarted.Begin(ctx, r); e != nil || again.Allowed || !again.Existing {
		t.Fatal("done claim allowed a second paid call", again, e)
	}
	files, _ := os.ReadDir(policy.SpoolDir)
	if len(files) != 1 || !strings.HasSuffix(files[0].Name(), ".done") {
		t.Fatal(files)
	}
}
func TestDegradedCrossProcessClaimOnlyOneAllowed(t *testing.T) {
	policy := DefaultPolicy()
	policy.SpoolDir = t.TempDir()
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, _ := New(nil, policy).Begin(context.Background(), costRequest("same"))
			if res.Allowed {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != 1 {
		t.Fatalf("allowed=%d", allowed)
	}
}

func TestOneUnmatchedOrCorruptSpoolRecordDoesNotBlockOtherReceipts(t *testing.T) {
	policy := DefaultPolicy()
	policy.SpoolDir = t.TempDir()
	controller := New(nil, policy)
	ctx := context.Background()
	if _, err := controller.Begin(ctx, costRequest("good")); !errors.Is(err, ErrAccountingUnavailable) {
		t.Fatal(err)
	}
	if err := controller.Finish(ctx, "good", costReceipt(ReceiptSuccess)); !errors.Is(err, ErrAccountingUnavailable) {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(policy.SpoolDir, "000-bad.json"), []byte(`{"version":999}`), 0600); err != nil {
		t.Fatal(err)
	}
	store := &spoolMemoryStore{}
	controller.store = store
	result, err := controller.ReplaySpool(ctx)
	if err == nil || result.Replayed != 2 || result.Pending != 1 || store.entries["good"].Status != StatusSettled {
		t.Fatal(result, err, store.entries)
	}
}
func TestInvoiceParserRequiresRequestIdentityCurrencyAndExactMicros(t *testing.T) {
	json := `[{"tenant_id":14,"room_id":29,"provider":"tts","provider_request_id":"p1","actual_micros":160,"currency":"CNY"},{"tenant_id":14,"room_id":29,"provider":"tts","provider_request_id":"p1","actual_micros":160,"currency":"CNY"}]`
	if entries, e := ParseInvoices(strings.NewReader(json), "json"); e != nil || len(entries) != 1 {
		t.Fatal(entries, e)
	}
	for _, bad := range []string{strings.Replace(json, `"CNY"`, `"USD"`, 1), strings.Replace(json, `"actual_micros":160`, `"actual_micros":1.6`, 1), strings.Replace(json, `"p1"`, `""`, 1), strings.Replace(json, `"actual_micros":160`, `"actual_micros":161`, 1), json + "{}"} {
		if _, e := ParseInvoices(strings.NewReader(bad), "json"); e == nil {
			t.Fatal("bad invoice accepted", bad)
		}
	}
	csv := "tenant_id,room_id,provider,provider_request_id,actual_micros,currency\n14,29,tts,p2,320,CNY\n"
	if entries, e := ParseInvoices(strings.NewReader(csv), "csv"); e != nil || len(entries) != 1 {
		t.Fatal(entries, e)
	}
}
