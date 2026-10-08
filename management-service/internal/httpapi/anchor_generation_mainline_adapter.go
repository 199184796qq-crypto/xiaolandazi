package httpapi

import (
	"errors"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"strings"
)

// Source-neutral handoff: Core sees a mainline track with text/time/safe points,
// not an Agent, a recording, a preview worker, or a TTS provider.
// The live producer must use its own room-scoped run. Preview assets are never
// promoted to the mainline implicitly, even if their text happens to match.
func liveCycleMainlineTrack(state *anchorcycle.State, segment anchorcycle.Segment) (coreclient.ProgramRefreshTrack, error) {
	var track coreclient.ProgramRefreshTrack
	if state == nil || state.TenantID <= 0 || state.PlanID <= 0 || state.RoomID <= 0 || state.Surface != "formal_live" || state.PreviewLimitMS != 0 {
		return track, errors.New("只有独立正式直播场次可以交付Core，试听场不能进入房间")
	}
	accepted := false
	for _, item := range state.Accepted {
		if item.Selected.ID == segment.Selected.ID && cycleVoiceText(item.Selected) == cycleVoiceText(segment.Selected) {
			accepted = true
			break
		}
	}
	if !accepted {
		return track, errors.New("未采纳的话术不能进入主线")
	}
	if segment.Voice == nil || segment.Voice.Status != "ready" || segment.Voice.PreviewTruncated {
		return track, errors.New("主线声音资产未完整完成")
	}
	if err := checkCycleVoiceP0(state, segment); err != nil {
		return track, err
	}
	bundle := segment.Voice.Bundle
	if strings.TrimSpace(bundle.AudioURL) == "" || bundle.AudioDurationMS <= 0 || strings.TrimSpace(bundle.SRT) == "" {
		return track, errors.New("主线缺少完整声音或字幕")
	}
	if err := validateLiveAgentVariantTimeline(bundle); err != nil {
		return track, err
	}
	if err := validateLiveAgentVariantSchedulingAssets(bundle); err != nil {
		return track, err
	}
	track = coreclient.ProgramRefreshTrack{ID: segment.Selected.ID, Label: segment.Task.Key, Text: bundle.Text, AudioURL: bundle.AudioURL, DurationMS: int(bundle.AudioDurationMS)}
	for _, part := range bundle.Timeline {
		track.Timeline = append(track.Timeline, coreclient.ProgramRefreshTimelineSegment{SegmentID: part.SegmentID, Index: part.Index, StartMS: int(part.StartMS), EndMS: int(part.EndMS), Text: part.Text, SafeCut: part.SafeCut})
	}
	for _, point := range bundle.SafePoints {
		track.SafePoints = append(track.SafePoints, coreclient.ProgramRefreshSafePoint{ID: point.ID, CutMS: int(point.CutMS), Score: point.Score, Grade: point.Grade, Kind: point.Kind, SentenceID: point.SentenceID, LeftPreview: point.LeftPreview, NextPreview: point.NextPreview, Topics: point.Topics})
	}
	return track, nil
}
