package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/sharedfilelock"
)

// Commands are durable, session-scoped and single-flight. The producer is the
// only writer of the supply cursor; an HTTP request never edits its position.
type scriptManualJump struct {
	RequestedAt   time.Time            `json:"requested_at,omitzero"`
	Title         string               `json:"title,omitempty"`
	RequestID     string               `json:"request_id"`
	SessionID     int64                `json:"session_id"`
	VersionID     int64                `json:"version_id"`
	ProgramID     string               `json:"program_id"`
	PieceID       string               `json:"piece_id"`
	RecordingID   int64                `json:"recording_id,omitempty"`
	RecordingName string               `json:"recording_name,omitempty"`
	Status        string               `json:"status"`
	Progress      scriptManualProgress `json:"progress,omitempty"`
	Error         string               `json:"error,omitempty"`
}

type scriptManualState struct {
	Current             *scriptManualJump           `json:"current,omitempty"`
	Receipts            map[string]scriptManualJump `json:"receipts"`
	Prepared            *volcengineCachedAudio      `json:"prepared,omitempty"`
	Generation          []scriptManualGeneration    `json:"generation,omitempty"`
	GenerationRequested string                      `json:"generation_requested,omitempty"`
}

type scriptManualPiece struct {
	ID         string                       `json:"id"`
	Title      string                       `json:"title"`
	Specified  bool                         `json:"specified,omitempty"`
	Unit       int                          `json:"-"`
	HumanKey   string                       `json:"-"`
	Recordings []db.ScriptHumanVoiceSummary `json:"recordings,omitempty"`
}
type scriptManualGroup struct {
	ID     string              `json:"id"`
	Title  string              `json:"title"`
	Pieces []scriptManualPiece `json:"pieces"`
}
type scriptManualOptions struct {
	PlayingPieceID     string              `json:"playing_piece_id,omitempty"`
	PlayingRecordingID int64               `json:"playing_recording_id,omitempty"`
	PlanID             int64               `json:"-"`
	SessionID          int64               `json:"session_id"`
	VersionID          int64               `json:"version_id"`
	ProgramID          string              `json:"program_id"`
	Enabled            bool                `json:"enabled"`
	Reason             string              `json:"reason,omitempty"`
	Groups             []scriptManualGroup `json:"groups"`
	HumanGroups        []scriptManualGroup `json:"human_groups,omitempty"`
	MainlineMode       string              `json:"mainline_mode"`
	ModeEditable       bool                `json:"mode_editable"`
	HumanSupported     bool                `json:"-"`
	Jump               *scriptManualJump   `json:"jump,omitempty"`
}

var scriptManualID = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,72}$`)
var errScriptManualBusy = errors.New("已有话术正在准备或等待切换，请稍后再选择")
var errScriptManualStale = errors.New("直播场次或方案已变化，请刷新后重选")

func scriptManualPath(tenant, room, session int64) string {
	return filepath.Join(recordingCacheMediaRoot(), "script-manual-jumps", fmt.Sprintf("%d-%d-%d.json", tenant, room, session))
}
func scriptManualActive(j *scriptManualJump) bool {
	return j != nil && (j.Status == "requested" || j.Status == "preparing" || j.Status == "waiting")
}
func readScriptManualState(path string) (scriptManualState, error) {
	state := scriptManualState{Receipts: map[string]scriptManualJump{}}
	err := readVolcengineJSON(path, &state)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if state.Receipts == nil {
		state.Receipts = map[string]scriptManualJump{}
	}
	return state, err
}
func readScriptManual(path string) (*scriptManualJump, error) {
	state, err := readScriptManualState(path)
	return state.Current, err
}
func submitScriptManual(ctx context.Context, path string, j scriptManualJump) (*scriptManualJump, error) {
	release, err := sharedfilelock.Lock(ctx, path+".lock")
	if err != nil {
		return nil, err
	}
	defer release()
	state, err := readScriptManualState(path)
	if err != nil {
		return nil, err
	}
	if old, exists := state.Receipts[j.RequestID]; exists {
		if old.SessionID != j.SessionID || old.VersionID != j.VersionID || old.ProgramID != j.ProgramID || old.PieceID != j.PieceID || old.RecordingID != j.RecordingID {
			return nil, errScriptManualStale
		}
		return &old, nil
	}
	if scriptManualActive(state.Current) {
		return nil, errScriptManualBusy
	}
	j.Status, j.Progress, j.Error = "requested", "", ""
	j.RequestedAt = time.Now().UTC()
	state.Current = &j
	state.Prepared = nil
	state.Receipts[j.RequestID] = j
	return &j, writeVolcengineJSON(path, state)
}
func updateScriptManual(ctx context.Context, path string, expected scriptManualJump, status, message string) error {
	release, err := sharedfilelock.Lock(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer release()
	state, err := readScriptManualState(path)
	if err != nil {
		return err
	}
	j := state.Current
	if j == nil || j.RequestID != expected.RequestID || j.VersionID != expected.VersionID || j.ProgramID != expected.ProgramID {
		return errScriptManualStale
	}
	j.Status, j.Error = status, message
	if status == "applied" || status == "failed" {
		state.Prepared = nil
	}
	state.Receipts[j.RequestID] = *j
	return writeVolcengineJSON(path, state)
}

// Tabs and entries use saved editor order, not the randomized playback order
// nor the opening-anchor rotation performed by scriptFirstUnits.
func scriptManualGroups(v model.LiveAgentPlanVersion, units []volcengineScriptUnit) []scriptManualGroup {
	groups := []scriptManualGroup{}
	script, ok := recordingScriptFromContext(v.GenerationContext)
	if !ok || script.Version != 2 {
		return groups
	}
	for ti, tab := range script.Tabs {
		g := scriptManualGroup{ID: tab.TabID, Title: tab.Title, Pieces: []scriptManualPiece{}}
		for ei, entry := range tab.Entries {
			pool := scriptEntryPoolID(tab, ti, entry.EntryID)
			for i, unit := range units {
				if unit.poolID() != pool {
					continue
				}
				title := strings.TrimSpace(entry.Title)
				if title == "" {
					title = fmt.Sprintf("第%d篇话术", ei+1)
				}
				g.Pieces = append(g.Pieces, scriptManualPiece{
					ID: pool, Title: title, Specified: unit.SpecPromptEnabled && scriptPiecePromptHash(unit) != "", Unit: i, HumanKey: unit.HumanKey,
				})
				break
			}
		}
		if len(g.Pieces) > 0 {
			groups = append(groups, g)
		}
	}
	return groups
}

func (s *Server) scriptManualOptions(ctx context.Context, tenant, room int64, includeRecordings bool) (scriptManualOptions, error) {
	result := scriptManualOptions{Groups: []scriptManualGroup{}}
	mode, err := readScriptMainlineMode(scriptMainlineModePath(recordingCacheMediaRoot(), tenant, room))
	if err != nil {
		return result, err
	}
	result.MainlineMode = mode
	v, err := s.store.GetPublishedLiveAgentPlanVersionForRoom(ctx, tenant, room)
	if err != nil {
		result.Reason = "请先为直播间保存话术方案"
		return result, nil
	}
	session, sessionErr := s.store.GetLiveRuntimeByRoom(ctx, tenant, room)
	p, coreErr := s.core.GetRoomProgramSnapshot(ctx, tenant, room)
	if coreErr != nil {
		return result, coreErr
	}
	if p.Running {
		// Resolve the immutable running version. Editing/publishing a new draft
		// must not relabel, purchase for, or jump within the old session.
		v, err = s.store.GetLiveAgentPlanVersion(ctx, tenant, v.PlanID, p.VersionID)
		if err != nil {
			return result, err
		}
	}
	result.PlanID, result.VersionID, result.ProgramID = v.PlanID, v.ID, p.ProgramID
	result.ModeEditable = true
	if sessionErr == nil && session.EndedAt == nil && (session.Status == "running" || session.Status == "paused") &&
		session.BelongsToExecutionRealm(s.executionRealm) {
		result.SessionID = session.ID
	}
	units, err := scriptFirstUnits(v, "")
	if err != nil || !usesScriptFirstParagraphs(v) {
		result.Reason = "当前方案不支持分片选择播放"
		return result, nil
	}
	result.Groups = scriptManualGroups(v, units)
	result.HumanSupported = true
	if includeRecordings {
		groups, legacy := s.humanScriptPlaybackGroups(ctx, tenant, v.PlanID)
		result.HumanGroups = groups
		if len(legacy.Pieces) > 0 {
			result.Groups = append(result.Groups, legacy)
		}
	}
	if len(result.Groups) == 0 && len(result.HumanGroups) == 0 && includeRecordings {
		if mode == scriptMainlineAI {
			result.Reason = "当前方案没有分组话术"
		}
		return result, nil
	}
	result.Reason = "开始 AI 助播后可选择话术"
	if result.SessionID == 0 {
		return result, nil
	}
	if !p.Running || !p.Sequential || p.Task.SessionID != fmt.Sprintf("volcengine-long-%d", session.ID) {
		return result, nil
	}
	if !p.ManualSelectionSupported {
		result.Reason = "播放服务升级后可选择话术"
		return result, nil
	}
	paused, err := s.checkLiveAnchorOwner(ctx, tenant, room, session.ID, v.ID)
	if err != nil {
		return result, err
	}
	result.Enabled = !paused
	result.Reason = ""
	if paused {
		result.Reason = "继续 AI 助播后可选择话术"
	}
	if includeRecordings && s.assetStorage != nil {
		// Reading recording metadata should not block normal script selection.
		recordings, loadErr := s.store.ListPlanScriptHumanVoices(ctx, tenant, v.PlanID)
		if loadErr == nil {
			for gi := range result.Groups {
				for pi := range result.Groups[gi].Pieces {
					piece := &result.Groups[gi].Pieces[pi]
					piece.Recordings = recordings[piece.HumanKey]
				}
			}
		}
	}
	state, err := readScriptManualState(scriptManualPath(tenant, room, session.ID))
	result.Jump = state.Current
	if result.Jump != nil && (result.Jump.VersionID != v.ID || result.Jump.ProgramID != p.ProgramID) {
		result.Jump = nil
	}
	if includeRecordings && !paused && !p.Suspended && !p.WaitingForContent && p.TrackID != "" {
		var cursor volcengineScriptCursor
		if readVolcengineJSON(scriptSupplyCheckpointPath(tenant, room, session.ID), &cursor) == nil &&
			cursor.ProgramID == p.ProgramID && cursor.VersionID == v.ID {
			result.PlayingPieceID, result.PlayingRecordingID = scriptPlayingChoice(cursor.Delivered, units, p.TrackID)
		}
	}
	return result, err
}

func (s *Server) roomScriptPlayback(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	room, ok := pathID(w, r)
	if !ok {
		return
	}
	tenant, ok := s.tenantForRoom(w, r, actor, room)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	options, err := s.scriptManualOptions(ctx, tenant, room, r.Method == http.MethodGet)
	if err != nil {
		writeError(w, http.StatusBadGateway, "话术播放状态暂时不可用，请重试")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, options)
		return
	}
	if r.Method == http.MethodPatch {
		s.updateRoomMainlineMode(w, r, ctx, tenant, room, options)
		return
	}
	var input scriptManualJump
	if readJSON(w, r, &input) != nil || !scriptManualID.MatchString(input.RequestID) || input.PieceID == "" || input.RecordingID < 0 {
		writeError(w, http.StatusBadRequest, "话术选择参数无效")
		return
	}
	if !options.Enabled || input.SessionID != options.SessionID || input.VersionID != options.VersionID || input.ProgramID != options.ProgramID {
		writeError(w, http.StatusConflict, "当前暂不能切换，请确认 AI 助播正在运行并刷新页面")
		return
	}
	var found scriptManualPiece
	input.Title = ""
	if strings.HasPrefix(input.PieceID, fissionChoicePrefix) {
		fission, index, _, err := s.loadFissionChoice(ctx, tenant, options.PlanID, input.PieceID)
		if err != nil || input.RecordingID != 0 || s.assetStorage == nil {
			writeError(w, http.StatusConflict, "所选真人话术组合暂不可用，请重新选择")
			return
		}
		input.Title = fission.State.RecipeName(fission.State.Recipes[index], index+1)
		found.ID = input.PieceID
	} else if strings.HasPrefix(input.PieceID, humanSourceChoicePrefix) {
		asset, err := s.loadHumanSourceChoice(ctx, tenant, options.PlanID, input.PieceID)
		if err != nil || input.RecordingID != 0 {
			writeError(w, http.StatusConflict, errHumanSourceChoiceChanged.Error())
			return
		}
		input.Title = humanSourceChoiceName(asset.OriginalName)
		found.ID = input.PieceID
	}
	for _, group := range options.Groups {
		for _, piece := range group.Pieces {
			if piece.ID == input.PieceID {
				found = piece
				break
			}
		}
	}
	if found.ID == "" {
		writeError(w, http.StatusBadRequest, "所选话术不属于当前直播方案")
		return
	}
	input.RecordingName = ""
	if input.RecordingID > 0 {
		if s.assetStorage == nil || found.HumanKey == "" {
			writeError(w, http.StatusConflict, "所选真人声音暂不可用，请重新选择")
			return
		}
		asset, err := s.store.ScriptHumanVoice(ctx, tenant, options.PlanID, found.HumanKey, input.RecordingID)
		if err != nil || asset.DurationMS == nil || *asset.DurationMS == 0 {
			writeError(w, http.StatusConflict, "所选真人声音暂不可用，请重新选择")
			return
		}
		input.RecordingName = asset.OriginalName
	}
	// Submit resets any caller-supplied playback state or progress.
	job, err := submitScriptManual(ctx, scriptManualPath(tenant, room, options.SessionID), input)
	if err != nil {
		if errors.Is(err, errScriptManualBusy) || errors.Is(err, errScriptManualStale) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "保存话术选择失败，请重试")
		}
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func scriptManualMatches(j *scriptManualJump, session int64, v model.LiveAgentPlanVersion, p coreclient.ProgramRefreshSnapshot) bool {
	return scriptManualActive(j) && j.SessionID == session && j.VersionID == v.ID && p.VersionID == v.ID && j.ProgramID == p.ProgramID && p.Running && p.Sequential && p.ManualSelectionSupported && p.Task.SessionID == fmt.Sprintf("volcengine-long-%d", session)
}
