package anchorcycle

// MainlineReuseTarget is an output mix, separate from provider token caching.
const MainlineReuseTarget = 70

func (s *State) FirstLiveOpening() bool {
	return s != nil && s.Surface == "formal_live" && s.Phase == "opening" && s.OpeningCount == 0
}

func (s *State) ReuseTaskKey() string {
	return s.reuseTaskKey(s.CurrentFissionSide())
}

func (s *State) reuseTaskKey(side string) string {
	if s.Surface != "formal_live" {
		return s.Task().Key
	}
	phase := s.Phase
	if phase == "opening" {
		phase += ":" + s.OpeningVersion()
	}
	return phase + ":" + s.Task().Key + ":" + side
}

func CandidateReused(c Candidate) bool {
	return c.ReuseOrigin == "library" || c.ReuseOrigin == "session_pool" || c.LibraryEntryID != "" || c.ScoreScope == "speech_library_reused" || c.ScoreScope == "speech_pool_reused"
}

// MainlineReuseWindow counts accepted ordinary mainline outputs only. A retry,
// failed cache read or rejected candidate never spends another quota. Definite
// delivery failures are removed; unknown acknowledgements stay reserved.
// Only the last nine eligible outputs affect the next ten-output window, so a
// cold empty library cannot accumulate an unlimited debt of future reuses.
func (s *State) MainlineReuseWindow() (total, reused int) {
	for i := len(s.Accepted) - 1; i >= 0 && total < 9; i-- {
		seg := s.Accepted[i]
		if seg.Task.ProductMode == "lecture" || seg.Task.ProductMode == "auxiliary" || seg.Task.SellingPoint == "塑价兑现" || seg.DeliveryStatus != nil && seg.DeliveryStatus.Status == "failed" || seg.Quota != nil && seg.Quota.Returned {
			continue
		}
		if seg.Phase != "opening" && seg.Phase != "body" && seg.Phase != "closing" {
			continue
		}
		if seg.Phase == "opening" && (seg.OpeningVersion == "完整版" || i == 0 && s.AcceptedOffset == 0 && seg.OpeningVersion == "") {
			continue
		}
		total++
		if CandidateReused(seg.Selected) {
			reused++
		}
	}
	return
}

func (s *State) formalMainlineShouldReuse() bool {
	if !s.SpeechReuseEnabled() || s.FirstLiveOpening() || s.PreviewLimitMS > 0 || s.PriceHook != nil || s.P0PendingRepair != nil || s.SegmentDirective != "" || len(s.Attempts) > 0 || s.Task().ProductMode == "lecture" || s.Task().ProductMode == "auxiliary" {
		return false
	}
	total, reused := s.MainlineReuseWindow()
	return reused*100 < (total+1)*MainlineReuseTarget
}
