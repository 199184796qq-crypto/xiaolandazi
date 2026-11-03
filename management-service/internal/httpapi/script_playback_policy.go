package httpapi

import (
	"context"
	"errors"
	"log"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptplay"
	"livecompanion/management/internal/sharedfilelock"
)

const scriptContentScale = 1e9

// Projection is only a forecast: acknowledged queue audio is never written as
// actual listening history. Include the unplayed source tail and pending tracks
// so a future append cannot hide repetition in the already-buffered audio.
func scriptPolicyForecastQueue(cursor volcengineScriptCursor, p coreclient.ProgramRefreshSnapshot, now time.Time) []scriptPCMSpan {
	wanted := map[string]bool{p.TrackID: true}
	for _, x := range p.FutureWindow {
		wanted[x.ID] = true
	}
	progress := map[string]int64{}
	terminal := map[string]bool{}
	for _, r := range p.PlaybackReceipts {
		progress[r.TrackID] = r.PlayedMS
		terminal[r.TrackID] = r.Terminal || r.Complete
	}
	at := now.UnixMilli()
	remaining := max(0, p.BufferedMainlineMS)
	var spans []scriptPCMSpan
	for _, track := range cursor.Delivered {
		if !wanted[track.TrackID] || terminal[track.TrackID] || track.PolicyDurationMS <= 0 || track.PolicyText == "" {
			continue
		}
		played := max(track.PolicyRecordedMS, progress[track.TrackID])
		left := min(max(0, track.Block.DurationMS-played), remaining)
		if left <= 0 {
			continue
		}
		scale := scriptContentScale / float64(track.PolicyDurationMS)
		spans = append(spans, scriptPCMSpan{Content: scriptContentID(track.PolicyText), Track: track.TrackID, Start: at, End: at + left, From: float64(track.PolicyOffsetMS+played) * scale, To: float64(track.PolicyOffsetMS+played+left) * scale})
		at += left
		remaining -= left
	}
	return spans
}

type scriptPCMSpan struct {
	HeardMS int64   `json:"heard_ms,omitempty"`
	Content string  `json:"content"`
	Track   string  `json:"track"`
	Start   int64   `json:"start"`
	End     int64   `json:"end"`
	From    float64 `json:"from"`
	To      float64 `json:"to"`
}
type scriptPCMProgress struct {
	Played int64
	Seen   int64
}
type scriptPolicyCycle struct {
	Members []scriptplay.Version `json:"members"`
	Used    map[string]bool      `json:"used"`
	Pending map[string]string    `json:"pending"`
	Round   int                  `json:"round"`
}
type scriptRoomPolicy struct {
	Tenant, Room, Plan int64
	Cycles             map[string]*scriptPolicyCycle `json:"cycles"`
	Progress           map[string]scriptPCMProgress  `json:"progress"`
	Spans              []scriptPCMSpan               `json:"spans"`
	RepeatRate         float64                       `json:"repeat_rate"`
	ForecastRate       float64                       `json:"forecast_rate"`
	SupplyShortage     bool                          `json:"supply_shortage"`
	UpdatedAt          int64                         `json:"updated_at"`
}

func scriptContentID(text string) string {
	return shortScriptIdentity(strings.Join(strings.Fields(text), ""))
}
func scriptRoomPolicyPath(root string, tenant, room, plan int64) string {
	return filepath.Join(root, "script-playback-policy", strconv.FormatInt(tenant, 10), strconv.FormatInt(room, 10), strconv.FormatInt(plan, 10)+".json")
}
func readScriptRoomPolicy(path string, tenant, room, plan int64) (scriptRoomPolicy, error) {
	st := scriptRoomPolicy{Tenant: tenant, Room: room, Plan: plan}
	err := readVolcengineJSON(path, &st)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return st, err
	}
	if tenant <= 0 || room <= 0 || plan <= 0 || st.Tenant != tenant || st.Room != room || st.Plan != plan {
		return st, errors.New("播放策略记录归属不一致")
	}
	if st.Cycles == nil {
		st.Cycles = map[string]*scriptPolicyCycle{}
	}
	if st.Progress == nil {
		st.Progress = map[string]scriptPCMProgress{}
	}
	return st, nil
}
func scriptCycleKey(v model.LiveAgentPlanVersion, u volcengineScriptUnit) string {
	return shortScriptIdentity(u.poolID() + scriptRewriteFactsKey(v) + scriptFirstParagraphKey(v, "policy"))
}

// Foreground originals and emergency cache replays use the same accounting as
// normal pool selections. Reconcile an instance that changed host before it
// was enqueued; a discarded selection must not strand a version reservation.
func reserveScriptPolicyAudio(root string, v model.LiveAgentPlanVersion, u volcengineScriptUnit, tenant, room int64, instance string, a volcengineCachedAudio) error {
	path := scriptRoomPolicyPath(root, tenant, room, v.PlanID)
	release, err := sharedfilelock.Lock(context.Background(), path+".lock")
	if err != nil {
		return err
	}
	defer release()
	st, err := readScriptRoomPolicy(path, tenant, room, v.PlanID)
	if err != nil {
		return err
	}
	key := scriptCycleKey(v, u)
	cycle := st.Cycles[key]
	if cycle == nil {
		cycle = &scriptPolicyCycle{Used: map[string]bool{}, Pending: map[string]string{}}
		st.Cycles[key] = cycle
	}
	for _, c := range st.Cycles {
		delete(c.Pending, instance)
	}
	if cycle.Pending == nil {
		cycle.Pending = map[string]string{}
	}
	if cycle.Used == nil {
		cycle.Used = map[string]bool{}
	}
	text := a.Text
	if text == "" {
		text = u.Text
	}
	content := scriptContentID(text)
	found := false
	for _, x := range cycle.Members {
		if scriptContentID(x.Text) == content {
			found = true
			break
		}
	}
	if !found {
		for _, x := range scriptCompatiblePool(root, v, u).Versions {
			if x.AudioRef == strconv.FormatInt(a.AssetID, 10) {
				cycle.Members = append(cycle.Members, x)
				break
			}
		}
	}
	cycle.Pending[instance] = content
	return writeVolcengineJSON(path, st)
}

// Count source positions already heard inside the window. Partial edge clips
// and multiple overlapping repeats must not count the same unique part twice.
// Offsets are normalized, so changing voice/rate cannot mint a new text version.
func scriptRepeatRate(spans []scriptPCMSpan, end int64) float64 {
	type interval struct{ a, b float64 }
	seen := map[string][]interval{}
	total, repeated := float64(0), float64(0)
	from := end - scriptPolicyWindowMS
	ordered := append([]scriptPCMSpan(nil), spans...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Start < ordered[j].Start })
	for _, s := range ordered {
		lo, hi := max(from, s.Start), min(end, s.End)
		if hi <= lo || s.End <= s.Start || s.To <= s.From {
			continue
		}
		a := s.From + (s.To-s.From)*float64(lo-s.Start)/float64(s.End-s.Start)
		b := s.From + (s.To-s.From)*float64(hi-s.Start)/float64(s.End-s.Start)
		old := seen[s.Content]
		overlap := float64(0)
		for _, x := range old {
			overlap += math.Max(0, math.Min(b, x.b)-math.Max(a, x.a))
		}
		duration := float64(hi - lo)
		if s.HeardMS > 0 {
			duration = float64(s.HeardMS) * float64(hi-lo) / float64(s.End-s.Start)
		}
		total += duration
		repeated += duration * math.Min(1, overlap/(b-a))
		old = append(old, interval{a, b})
		sort.Slice(old, func(i, j int) bool { return old[i].a < old[j].a })
		merged := old[:0]
		for _, x := range old {
			if len(merged) > 0 && x.a <= merged[len(merged)-1].b {
				merged[len(merged)-1].b = math.Max(merged[len(merged)-1].b, x.b)
			} else {
				merged = append(merged, x)
			}
		}
		seen[s.Content] = merged
	}
	if total == 0 {
		return 0
	}
	return repeated / total
}

func scriptForecastRate(st scriptRoomPolicy, content string, duration, now int64) float64 {
	// Evaluate the candidate progressively, including edge positions where an old
	// span leaves the window. Endpoint-only checks can miss a middle violation.
	ends := []int64{now + duration}
	for d := int64(15000); d < duration; d += 15000 {
		ends = append(ends, now+d)
	}
	for _, s := range st.Spans {
		d := s.End + scriptPolicyWindowMS
		if d > now && d < now+duration {
			ends = append(ends, d)
		}
	}
	worst := float64(0)
	for _, end := range ends {
		candidate := scriptPCMSpan{Content: content, Start: now, End: end, To: scriptContentScale * float64(end-now) / float64(duration)}
		rate := scriptRepeatRate(append(append([]scriptPCMSpan(nil), st.Spans...), candidate), end)
		worst = math.Max(worst, rate)
	}
	return worst
}

// Persist a selection reservation. A retry of the same instance returns the
// same asset; only PCM confirmation consumes a member of the room's cycle.
func scriptPolicyPoolAudio(root string, v model.LiveAgentPlanVersion, u volcengineScriptUnit, tenant, room, session int64, instance string, buffered int64, rng *rand.Rand, projection []scriptPCMSpan, observed ...time.Time) (volcengineCachedAudio, bool, error) {
	path := scriptRoomPolicyPath(root, tenant, room, v.PlanID)
	release, err := sharedfilelock.Lock(context.Background(), path+".lock")
	if err != nil {
		return volcengineCachedAudio{}, false, err
	}
	defer release()
	st, err := readScriptRoomPolicy(path, tenant, room, v.PlanID)
	if err != nil {
		return volcengineCachedAudio{}, false, err
	}
	key := scriptCycleKey(v, u)
	cycle := st.Cycles[key]
	if cycle == nil {
		cycle = &scriptPolicyCycle{Used: map[string]bool{}, Pending: map[string]string{}}
		st.Cycles[key] = cycle
	}
	if cycle.Used == nil {
		cycle.Used = map[string]bool{}
	}
	if cycle.Pending == nil {
		cycle.Pending = map[string]string{}
	}
	// Reservations of a finished session are not evidence of playback. The next
	// session releases them, but keeps actual coverage and rolling history.
	prefix := "long-" + strconv.FormatInt(session, 10) + "-"
	for id := range cycle.Pending {
		if !strings.HasPrefix(id, prefix) {
			delete(cycle.Pending, id)
		}
	}
	ready := scriptCompatiblePool(root, v, u).Versions
	// A cycle keeps retired metadata while physical paid audio remains intact.
	valid := cycle.Members[:0]
	for _, x := range cycle.Members {
		if _, ok := scriptVersionAudio(root, u, v, x); ok {
			valid = append(valid, x)
		}
	}
	cycle.Members = valid
	members := map[string]bool{}
	for _, x := range cycle.Members {
		members[scriptContentID(x.Text)] = true
	}
	joinFill := len(ready) > len(cycle.Members) || len(cycle.Members) == 0
	for _, x := range ready {
		if joinFill && !members[scriptContentID(x.Text)] {
			cycle.Members = append(cycle.Members, x)
			members[scriptContentID(x.Text)] = true
		}
	}
	if len(cycle.Members) == 0 {
		return volcengineCachedAudio{}, false, nil
	}
	if reserved := cycle.Pending[instance]; reserved != "" {
		for _, x := range cycle.Members {
			if scriptContentID(x.Text) == reserved {
				a, ok := scriptVersionAudio(root, u, v, x)
				return a, ok, nil
			}
		}
	}
	complete := true
	for _, x := range cycle.Members {
		if !cycle.Used[scriptContentID(x.Text)] {
			complete = false
			break
		}
	}
	if complete {
		cycle.Round++
		cycle.Members = append([]scriptplay.Version(nil), ready...)
		cycle.Used = map[string]bool{}
		cycle.Pending = map[string]string{}
	}
	reserved := map[string]bool{}
	for _, content := range cycle.Pending {
		reserved[content] = true
	}
	var choices []scriptplay.Version
	for _, x := range cycle.Members {
		content := scriptContentID(x.Text)
		if !cycle.Used[content] && !reserved[content] {
			choices = append(choices, x)
		}
	}
	if len(choices) == 0 {
		return volcengineCachedAudio{}, false, errors.New("当前版本轮次等待已排队音频的PCM回执")
	}
	observedAt := time.Now().UTC()
	if len(observed) > 0 {
		observedAt = observed[0]
	}
	now := observedAt.UnixMilli() + max(0, buffered)
	// Random order is intentional only among candidates that meet the window
	// check and mainline cooldown; shortage chooses minimum predicted repetition.
	rng.Shuffle(len(choices), func(i, j int) { choices[i], choices[j] = choices[j], choices[i] })
	best := math.Inf(1)
	var chosen scriptplay.Version
	compliant := false
	forecastState := st
	forecastState.Spans = append(append([]scriptPCMSpan(nil), st.Spans...), projection...)
	for _, x := range choices {
		content := scriptContentID(x.Text)
		forecast := scriptForecastRate(forecastState, content, x.DurationMS, now)
		since := int64(0)
		lastEnd := int64(0)
		for _, span := range st.Spans {
			if span.Content == content {
				lastEnd = max(lastEnd, span.End)
			}
		}
		for _, span := range st.Spans {
			if span.Start >= lastEnd {
				if span.HeardMS > 0 {
					since += span.HeardMS
				} else {
					since += span.End - span.Start
				}
			}
		}
		cool := lastEnd == 0 || since >= scriptPolicyCooldownMS
		allowed := forecast <= .2+1e-9 && cool
		if (allowed && !compliant) || (allowed == compliant && forecast < best-1e-9) {
			chosen = x
			best = forecast
			compliant = allowed
		}
	}
	if len(st.Spans) == 0 && cycle.Round == 0 && len(cycle.Used) == 0 {
		for _, x := range choices {
			if x.Kind == scriptplay.KindOriginal {
				chosen = x
				best = 0
				compliant = true
				break
			}
		}
	}
	a, ok := scriptVersionAudio(root, u, v, chosen)
	if !ok {
		return a, false, errors.New("候选声音已失效")
	}
	cycle.Pending[instance] = scriptContentID(chosen.Text)
	if st.SupplyShortage != !compliant {
		log.Printf("script policy supply tenant=%d room=%d shortage=%t rolling90=%.4f forecast=%.4f", tenant, room, !compliant, st.RepeatRate, best)
	}
	st.ForecastRate = best
	st.SupplyShortage = !compliant
	st.UpdatedAt = time.Now().UTC().UnixMilli()
	if err = writeVolcengineJSON(path, st); err != nil {
		return a, false, err
	}
	return a, true, nil
}

// PCM observations are monotonic and idempotent. Poll intervals containing an
// interruption are split: unplayed wall time is never added to the denominator.
func recordScriptPolicyPCM(root string, tenant, room, plan int64, track scriptDeliveredTrack, played int64, started string, now time.Time) error {
	if track.PolicyText == "" || track.PolicyDurationMS <= 0 || played <= 0 {
		return nil
	}
	path := scriptRoomPolicyPath(root, tenant, room, plan)
	release, err := sharedfilelock.Lock(context.Background(), path+".lock")
	if err != nil {
		return err
	}
	defer release()
	st, err := readScriptRoomPolicy(path, tenant, room, plan)
	if err != nil {
		return err
	}
	old := st.Progress[track.TrackID]
	played = min(played, track.Block.DurationMS)
	if played <= old.Played {
		return nil
	}
	end := now.UTC().UnixMilli()
	delta := played - old.Played
	start := end - delta
	if old.Seen == 0 {
		if at, e := time.Parse(time.RFC3339Nano, started); e == nil {
			start = at.UnixMilli()
			end = start + delta
		}
	} else if start < old.Seen {
		start = old.Seen
		end = max(end, start+delta)
	}
	content := scriptContentID(track.PolicyText)
	scale := scriptContentScale / float64(track.PolicyDurationMS)
	span := scriptPCMSpan{HeardMS: delta, Content: content, Track: track.TrackID, Start: start, End: end, From: float64(track.PolicyOffsetMS+old.Played) * scale, To: float64(track.PolicyOffsetMS+played) * scale}
	n := len(st.Spans)
	if n > 0 && st.Spans[n-1].Track == span.Track && span.Start-st.Spans[n-1].End <= 1500 {
		st.Spans[n-1].End = span.End
		st.Spans[n-1].To = span.To
		st.Spans[n-1].HeardMS += delta
	} else {
		st.Spans = append(st.Spans, span)
	}
	st.Progress[track.TrackID] = scriptPCMProgress{Played: played, Seen: now.UnixMilli()}
	for _, cycle := range st.Cycles {
		if cycle.Pending[track.InstanceID] == content {
			cycle.Used[content] = true
			delete(cycle.Pending, track.InstanceID)
		}
	}
	cutoff := now.UnixMilli() - scriptPolicyWindowMS
	kept := st.Spans[:0]
	for _, s := range st.Spans {
		if s.End > cutoff {
			kept = append(kept, s)
		}
	}
	st.Spans = kept
	for id, p := range st.Progress {
		if p.Seen < cutoff-10*60000 {
			delete(st.Progress, id)
		}
	}
	st.RepeatRate = scriptRepeatRate(st.Spans, now.UnixMilli())
	st.UpdatedAt = now.UnixMilli()
	return writeVolcengineJSON(path, st)
}
