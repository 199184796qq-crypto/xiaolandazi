package anchorcycle

import (
	"fmt"
	"hash/fnv"
	"livecompanion/management/internal/lecturetask"
)

type PlaybackReceipt struct {
	Sequence uint64 `json:"sequence"`
	TrackID  string `json:"track_id"`
	PlayedMS int64  `json:"played_ms"`
	Complete bool   `json:"complete"`
	Terminal bool   `json:"terminal"`
}
type LectureAudit struct {
	Passed   bool   `json:"passed"`
	Quote    string `json:"quote"`
	Reason   string `json:"reason"`
	TextSHA  string `json:"text_sha"`
	ScopeSHA string `json:"scope_sha"`
}

func (s *State) LectureCandidateApproved(c Candidate) bool {
	if s.Task().ProductMode != "lecture" {
		return true
	}
	return c.LectureAudit != nil && c.LectureAudit.Passed && c.LectureAudit.TextSHA == ProductDigest(spokenCandidate(c)) && c.LectureAudit.ScopeSHA == ProductDigest(s.Task().LectureFacts)
}

type LectureTrack struct {
	Topic    int64 `json:"topic,omitempty"`
	Angle    int   `json:"angle,omitempty"`
	Closing  bool  `json:"closing,omitempty"`
	PlayedMS int64 `json:"played_ms,omitempty"`
}
type LectureUse struct {
	Count  int         `json:"count"`
	LastMS int64       `json:"last_ms"`
	Angles map[int]int `json:"angles"`
}
type LectureRuntime struct {
	Library           lecturetask.Library     `json:"library"`
	Active            *Task                   `json:"active,omitempty"`
	ReservedTopic     int64                   `json:"reserved_topic,omitempty"`
	ReservedCandidate string                  `json:"reserved_candidate,omitempty"`
	Tracks            map[string]LectureTrack `json:"tracks"`
	Uses              map[int64]LectureUse    `json:"uses"`
	LastReceipt       uint64                  `json:"last_receipt"`
	PlayedMS          int64                   `json:"played_ms"`
	LectureMS         int64                   `json:"lecture_ms"`
	CompletedCycles   int                     `json:"completed_cycles"`
	LastCycle         int                     `json:"last_cycle"`
	LastMS            int64                   `json:"last_ms"`
	NextCycles        int                     `json:"next_cycles"`
	RecentLectureMS   []int64                 `json:"recent_lecture_ms"`
	FailedUntilMS     map[int64]int64         `json:"failed_until_ms"`
}

func NewLectures(lib lecturetask.Library) *LectureRuntime {
	return &LectureRuntime{Library: lib, Tracks: map[string]LectureTrack{}, Uses: map[int64]LectureUse{}, FailedUntilMS: map[int64]int64{}}
}
func (s *State) BeginLecture() bool {
	r := s.Lectures
	if r == nil || !r.Library.Policy.Enabled || r.ReservedTopic != 0 || s.Surface != "formal_live" || s.Phase != "opening" || s.CycleNumber < 2 || s.PriceHook != nil || len(s.Attempts) > 0 || (s.ProductFocus != nil && s.ProductFocus.AuxiliaryActive) {
		return false
	}
	link := ""
	if s.ProductFocus != nil {
		link = s.ProductFocus.CurrentLink
	}
	eligible := []lecturetask.Task{}
	for _, t := range r.Library.Tasks {
		if t.Enabled && t.Valid && t.Applicable(link) && r.FailedUntilMS[t.ID] <= r.PlayedMS {
			eligible = append(eligible, t)
		}
	}
	n := len(eligible)
	if n == 0 {
		return false
	}
	minCycles, minMS, topicMS := 4, int64(600000), int64(1800000)
	if n >= 8 {
		minCycles, minMS, topicMS = 2, 360000, 1200000
	} else if n >= 3 {
		minCycles, minMS, topicMS = 3, 480000, 1500000
	}
	if r.Library.Policy.Frequency == "less" {
		minCycles += 2
		minMS += 240000
	} else if r.Library.Policy.Frequency == "more" {
		minCycles = max(2, minCycles-1)
		minMS = max(360000, minMS-120000)
	}
	if r.NextCycles == 0 {
		h := fnv.New32a()
		_, _ = h.Write([]byte(fmt.Sprintf("%s/%d", s.RunID, r.LastCycle)))
		r.NextCycles = minCycles + int(h.Sum32()%3)
	}
	if r.CompletedCycles-r.LastCycle < r.NextCycles || r.PlayedMS-r.LastMS < minMS || (r.LectureMS+60000)*10 > r.PlayedMS {
		return false
	}
	recent := 0
	for _, at := range r.RecentLectureMS {
		if r.PlayedMS-at < 3600000 {
			recent++
		}
	}
	if recent >= 8 {
		return false
	}
	chosen := -1
	for i, t := range eligible {
		u := r.Uses[t.ID]
		if u.LastMS > 0 && r.PlayedMS-u.LastMS < topicMS {
			continue
		}
		if chosen < 0 || u.Count < r.Uses[eligible[chosen].ID].Count || (u.Count == r.Uses[eligible[chosen].ID].Count && u.LastMS < r.Uses[eligible[chosen].ID].LastMS) {
			chosen = i
		}
	}
	if chosen < 0 {
		return false
	}
	t := eligible[chosen]
	u := r.Uses[t.ID]
	angle := 0
	for i := range t.Angles {
		if u.Angles[i] < u.Angles[angle] {
			angle = i
		}
	}
	a := t.Angles[angle]
	task := Task{SellingPoint: t.Title, Angle: a.Title + "：" + a.Goal, Key: fmt.Sprintf("lecture/%d/%d/%d", t.ID, t.Revision, angle), ProductMode: "lecture", ProductMaxChars: 220, LectureTopic: t.ID, LectureAngle: angle, LecturePurpose: t.Purpose, LectureFacts: t.Evidence()}
	task.FocusLink = link
	task.setProductLinks(t.Links)
	r.Active = &task
	r.ReservedTopic = t.ID
	s.Phase = "lecture"
	s.PendingOpeningHook = nil
	return true
}
func (s *State) RegisterLectureTrack(c Candidate, t Task, phase string) {
	if s.Lectures == nil {
		return
	}
	s.Lectures.Tracks[c.ID] = LectureTrack{Topic: t.LectureTopic, Angle: t.LectureAngle, Closing: phase == "closing"}
	if t.ProductMode == "lecture" {
		s.Lectures.ReservedCandidate = c.ID
	}
}
func (s *State) ReleaseLecture(failed bool) {
	if s.Lectures == nil {
		return
	}
	r := s.Lectures
	if failed && r.ReservedTopic > 0 {
		if r.FailedUntilMS == nil {
			r.FailedUntilMS = map[int64]int64{}
		}
		r.FailedUntilMS[r.ReservedTopic] = r.PlayedMS + 300000
	}
	r.Active = nil
	r.ReservedTopic = 0
	r.ReservedCandidate = ""
	s.Phase = "opening"
}

// A failed optional voice never consumes a cycle or blocks the normal producer.
// Called while the generation worker waits for this voice, preserving normal cursors.
func (s *State) DropLectureTrack(id string) {
	r := s.Lectures
	if r == nil {
		return
	}
	t, ok := r.Tracks[id]
	if !ok || t.Topic <= 0 {
		return
	}
	if r.FailedUntilMS == nil {
		r.FailedUntilMS = map[int64]int64{}
	}
	r.FailedUntilMS[t.Topic] = r.PlayedMS + 300000
	if r.ReservedCandidate == id {
		r.ReservedCandidate, r.ReservedTopic = "", 0
	}
	delete(r.Tracks, id)
	for i := range s.Accepted {
		if s.Accepted[i].Selected.ID == id && s.Accepted[i].Task.ProductMode == "lecture" {
			s.Accepted[i].LectureSkipped = true
		}
	}
}
func (s *State) ObservePlayback(receipts []PlaybackReceipt, total int64) {
	r := s.Lectures
	if r == nil {
		return
	}
	r.PlayedMS = max(r.PlayedMS, total)
	for _, v := range receipts {
		if v.Sequence <= r.LastReceipt {
			continue
		}
		r.LastReceipt = v.Sequence
		t, ok := r.Tracks[v.TrackID]
		if !ok {
			continue
		}
		delta := max(int64(0), v.PlayedMS-t.PlayedMS)
		t.PlayedMS = max(t.PlayedMS, v.PlayedMS)
		if t.Topic > 0 {
			r.LectureMS += delta
		}
		r.Tracks[v.TrackID] = t
		if !v.Terminal && !v.Complete {
			continue
		}
		if t.Closing && v.Complete {
			r.CompletedCycles++
		}
		if t.Topic > 0 && v.PlayedMS > 0 {
			u := r.Uses[t.Topic]
			if u.Angles == nil {
				u.Angles = map[int]int{}
			}
			if v.Complete {
				u.Count++
				u.Angles[t.Angle]++
			}
			u.LastMS = r.PlayedMS
			r.Uses[t.Topic] = u
			r.LastMS = r.PlayedMS
			r.LastCycle = r.CompletedCycles
			r.NextCycles = 0
			r.RecentLectureMS = append(r.RecentLectureMS, r.PlayedMS)
			r.RecentLectureMS = r.RecentLectureMS[max(0, len(r.RecentLectureMS)-8):]
		}
		if v.TrackID == r.ReservedCandidate {
			r.ReservedCandidate = ""
			r.ReservedTopic = 0
		}
		delete(r.Tracks, v.TrackID)
	}
}
