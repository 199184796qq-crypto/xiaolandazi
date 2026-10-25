package livecost

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func costRequest(key string) Request {
	return Request{Key: key, TenantID: 14, RoomID: 29, Kind: KindTTS, Provider: "test-tts", Priority: PriorityNecessary, Reason: "首声供料", Characters: 20, BillableCharacters: 40, CharacterRule: ReferenceTTSCharacterRule}
}
func costReceipt(status string) Receipt { return Receipt{TenantID: 14, RoomID: 29, Status: status} }

func TestSummaryOptionalCohortsSerializeAsSubsetsWithoutChangingTotals(t *testing.T) {
	want := Summary{TotalMicros: 590_000, OptionalMicros: 390_000, MainlineOptionalMicros: 240_000,
		ReservedMicros: 250_000, UnknownMicros: 150_000, AgedUnresolvedMicros: 90_000}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil || fields["optional_micros"] != float64(390_000) || fields["mainline_optional_micros"] != float64(240_000) || fields["total_micros"] != float64(590_000) {
		t.Fatalf("summary cohorts did not survive JSON: %s err=%v", raw, err)
	}
	var old Summary
	if err := json.Unmarshal([]byte(`{"total_micros":590000,"reserved_micros":250000,"unknown_micros":150000}`), &old); err != nil || old.TotalMicros != 590_000 || old.OptionalMicros != 0 || old.MainlineOptionalMicros != 0 {
		t.Fatalf("legacy summary invented optional attribution: %+v err=%v", old, err)
	}
}

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
	if amount, e := EstimateTTS(10000, ReferenceTTSMicrosPerBillableCharacter); e != nil || amount != 800000 {
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

func TestWeightedTTSRequestAndReceiptKeepReferenceUnits(t *testing.T) {
	r := costRequest("weighted")
	r.Characters = 6 // 你好，A1！: six Unicode characters, eight billing characters.
	r.BillableCharacters = 8
	normalized, err := NormalizeRequest(r)
	if err != nil || normalized.EstimatedMicros != 640 {
		t.Fatal(normalized, err)
	}
	for _, mutate := range []func(*Request){
		func(r *Request) { r.BillableCharacters = -1 },
		func(r *Request) { r.CharacterRule = "" },
		func(r *Request) { r.CharacterRule = "supplier-unspecified" },
	} {
		bad := r
		mutate(&bad)
		if _, err := NormalizeRequest(bad); err == nil {
			t.Fatal("invalid billing character rule accepted", bad)
		}
	}
	cost := int64(640)
	receipt := costReceipt(ReceiptSuccess)
	receipt.EstimatedMicros = &cost
	receipt.UsageSource = SourceSentTextEstimate
	receipt.SentTextCharacters = 6
	receipt.BillableCharacters = 8
	receipt.CharacterRule = ReferenceTTSCharacterRule
	entry, err := ApplyReceipt(Entry{Request: normalized, Status: StatusReserved, ReservedMicros: normalized.EstimatedMicros}, receipt)
	if err != nil || entry.Status != StatusSettled || entry.ChargedMicros != 640 || entry.ReservedMicros != 0 || entry.Receipt.Characters != 0 {
		t.Fatal("API success did not settle independently of downstream use", entry, err)
	}
	raw, _ := json.Marshal(entry)
	if !strings.Contains(string(raw), `"billable_characters":8`) || !strings.Contains(string(raw), ReferenceTTSCharacterRule) {
		t.Fatal("weighted request/receipt lost durable units", string(raw))
	}
}

func TestRequestAttributionPreservesLegacyAndValidatesRoundIdentity(t *testing.T) {
	legacy, err := NormalizeRequest(costRequest("legacy"))
	if err != nil || legacy.Attribution != nil {
		t.Fatal(legacy, err)
	}
	raw, err := json.Marshal(legacy)
	if err != nil || strings.Contains(string(raw), "attribution") {
		t.Fatal("legacy request acquired an attribution identity", string(raw), err)
	}
	valid := costRequest("round")
	valid.Attribution = &Attribution{Source: " recording_mainline ", Round: 2, TabID: " stable-tab ", SourceIndex: 0}
	normalized, err := NormalizeRequest(valid)
	if err != nil || normalized.Attribution.Source != "recording_mainline" || normalized.Attribution.TabID != "stable-tab" || valid.Attribution.Source != " recording_mainline " {
		t.Fatal(normalized, valid, err)
	}
	raw, err = json.Marshal(normalized)
	var restored Request
	if err != nil || json.Unmarshal(raw, &restored) != nil || restored.Attribution == nil || *restored.Attribution != *normalized.Attribution {
		t.Fatal("round attribution did not survive durable request JSON", string(raw), err)
	}
	for _, attribution := range []Attribution{
		{Source: "interaction"}, // Real interactions are not forced into a mainline round.
		{Source: "recording_mainline", Round: 1, TabID: "tab", SourceIndex: 0},
	} {
		r := costRequest("valid-attribution")
		r.Attribution = &attribution
		if _, err := NormalizeRequest(r); err != nil {
			t.Fatal(attribution, err)
		}
	}
	for _, attribution := range []Attribution{
		{},
		{Source: strings.Repeat("x", 65)},
		{Source: "interaction", Round: -1},
		{Source: "interaction", SourceIndex: -1},
		{Source: "interaction", TabID: strings.Repeat("x", 129)},
		{Source: "recording_mainline", TabID: "tab"},
		{Source: "recording_mainline", Round: 1},
	} {
		r := costRequest("invalid-attribution")
		r.Attribution = &attribution
		if _, err := NormalizeRequest(r); err == nil {
			t.Fatal("invalid attribution accepted", attribution)
		}
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
