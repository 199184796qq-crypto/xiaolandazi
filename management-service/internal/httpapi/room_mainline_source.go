package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type mainlineProgramSource struct {
	ProgramID string `json:"program_id"`
	TrackID   string `json:"track_id"`
	VersionID int64  `json:"version_id"`
	SessionID string `json:"session_id"`
	Task      struct {
		SessionID string `json:"session_id"`
	} `json:"task"`
}

type mainlineHumanSource struct {
	RecordingName   string `json:"recording_name,omitempty"`
	CombinationName string `json:"combination_name,omitempty"`
}

type mainlineScriptSource struct {
	StandaloneHuman bool   `json:"standalone_human,omitempty"`
	GroupName       string `json:"group_name,omitempty"`
	PieceName       string `json:"piece_name,omitempty"`
	RecordingName   string `json:"recording_name,omitempty"`
	Specified       bool   `json:"specified,omitempty"`
}

func (h *mainlineHumanSource) standaloneLabel() *mainlineScriptSource {
	if h == nil || h.RecordingName == "" {
		return nil
	}
	return &mainlineScriptSource{StandaloneHuman: true, GroupName: h.RecordingName, PieceName: h.CombinationName}
}

func (s *Server) writeWorkbenchSpeechRuntime(w http.ResponseWriter, r *http.Request, resp *http.Response, tenant, room int64, query url.Values) {
	defer resp.Body.Close()
	var snapshot map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil || snapshot == nil {
		writeError(w, http.StatusBadGateway, "直播口播状态暂时不可用")
		return
	}
	// Read from the room's owner, including when the public audio host is another node.
	if engine := s.workbenchAudioEngine(r.Context(), tenant, room, query); len(engine) != 0 {
		snapshot["audio_engine"] = engine
	}
	var program mainlineProgramSource
	if json.Unmarshal(snapshot["program"], &program) == nil {
		if source := s.currentMainlineScriptSource(r.Context(), tenant, room, program); source != nil {
			snapshot["mainline_source"], _ = json.Marshal(source)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) workbenchAudioEngine(ctx context.Context, tenant, room int64, query url.Values) json.RawMessage {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	resp, err := s.core.DoRoomOwner(ctx, tenant, room, http.MethodGet, fmt.Sprintf("/internal/v1/rooms/%d/audio-engine", room), query, nil)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var body json.RawMessage
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil
	}
	return body
}

func (s *Server) currentMainlineScriptSource(ctx context.Context, tenant, room int64, program mainlineProgramSource) *mainlineScriptSource {
	if program.ProgramID == "" || program.TrackID == "" || program.VersionID <= 0 {
		return nil
	}
	sessionID := program.SessionID
	if sessionID == "" {
		sessionID = program.Task.SessionID
	}
	suffix, ok := strings.CutPrefix(sessionID, "volcengine-long-")
	if !ok {
		return nil
	}
	session, err := strconv.ParseInt(suffix, 10, 64)
	if err != nil || session <= 0 {
		return nil
	}
	var cursor volcengineScriptCursor
	if readVolcengineJSON(scriptSupplyCheckpointPath(tenant, room, session), &cursor) != nil ||
		cursor.VersionID != program.VersionID || (cursor.ProgramID != "" && cursor.ProgramID != program.ProgramID) {
		return nil
	}
	track, ok := mainlineTrackDelivery(cursor, session, program.TrackID)
	if !ok {
		return nil
	}
	if track.Unit < 0 {
		if !track.HumanVoice {
			return nil
		}
		if source := track.HumanSource.standaloneLabel(); source != nil {
			return source
		}
	}
	plan, err := s.store.GetLiveAgentPlanForRoom(ctx, tenant, room)
	if err != nil {
		return nil
	}
	if track.Unit < 0 {
		return s.legacyStandaloneMainlineSource(ctx, tenant, plan.ID, track)
	}
	version, err := s.store.GetLiveAgentPlanVersion(ctx, tenant, plan.ID, program.VersionID)
	if err != nil {
		return nil
	}
	units, err := scriptFirstUnits(version, "")
	if err != nil || track.Unit >= len(units) {
		return nil
	}
	unit := units[track.Unit]
	if track.HumanVoice && track.HumanSource == nil && unit.HumanKey != "" {
		if asset, err := s.store.ScriptHumanVoice(ctx, tenant, plan.ID, unit.HumanKey, track.AssetID); err == nil {
			track.HumanSource = &mainlineHumanSource{RecordingName: humanSourceChoiceName(asset.OriginalName)}
		}
	}
	return mainlineSourceForUnit(version, unit, track)
}

func mainlineTrackDelivery(cursor volcengineScriptCursor, session int64, trackID string) (scriptDeliveredTrack, bool) {
	if cursor.ProgramID != "" {
		for _, delivery := range cursor.Delivered {
			if delivery.TrackID == trackID {
				return delivery, true
			}
		}
	}
	// The first PCM can start before the successful publish updates the checkpoint.
	pending := cursor.Pending
	if pending != nil && pending.ID != "" {
		plainID := strings.TrimSuffix(trackID, "-raw")
		blockID := scriptFirstTrackID(session, cursor.Sequence, pending.Unit, pending.NextBlock)
		if plainID == pending.ID || plainID == blockID {
			return scriptDeliveredTrack{Unit: pending.Unit, PieceID: pending.HumanChoice, AssetID: pending.Audio.AssetID,
				HumanVoice: pending.Audio.HumanVoice, HumanSource: pending.Audio.HumanSource}, true
		}
	}
	return scriptDeliveredTrack{}, false
}

func (s *Server) legacyStandaloneMainlineSource(ctx context.Context, tenant, plan int64, track scriptDeliveredTrack) *mainlineScriptSource {
	id := track.AssetID
	fission := strings.HasPrefix(track.PieceID, fissionChoicePrefix)
	var recipe string
	if fission {
		var err error
		id, _, recipe, err = parseFissionChoiceID(track.PieceID)
		if err != nil {
			return nil
		}
	}
	asset, err := s.store.ScriptHumanVoice(ctx, tenant, plan, db.PlanHumanScriptScope, id)
	if err != nil {
		return nil
	}
	source := mainlineHumanSource{RecordingName: humanSourceChoiceName(asset.OriginalName)}
	if fission {
		job, err := s.store.HumanFission(ctx, tenant, plan, id)
		if err == nil {
			if index := job.State.RecipeIndex(recipe); index >= 0 {
				source.CombinationName = job.State.RecipeName(job.State.Recipes[index], index+1)
			}
		}
	}
	return source.standaloneLabel()
}

func mainlineSourceForUnit(version model.LiveAgentPlanVersion, unit volcengineScriptUnit, track scriptDeliveredTrack) *mainlineScriptSource {
	source := &mainlineScriptSource{Specified: unit.SpecPromptEnabled}
	if track.HumanVoice && track.HumanSource != nil {
		source.RecordingName = track.HumanSource.RecordingName
	}

	script, _ := recordingScriptFromContext(version.GenerationContext)
	for tabIndex, tab := range script.Tabs {
		for entryIndex, entry := range tab.Entries {
			if unit.Pool != "" && unit.Pool == scriptEntryPoolID(tab, tabIndex, entry.EntryID) {
				source.GroupName = tab.Title
				source.PieceName = strings.TrimSpace(entry.Title)
				if source.PieceName == "" {
					source.PieceName = fmt.Sprintf("第%d篇话术", entryIndex+1)
				}
				return source
			}
		}
	}
	// Older snapshots can have no entry IDs. A title is safe only when
	// every matching source belongs to the same group.
	for _, tab := range script.Tabs {
		for _, text := range tab.texts() {
			if strings.TrimSpace(text) != strings.TrimSpace(unit.Text) {
				continue
			}
			if source.GroupName != "" && source.GroupName != tab.Title {
				source.GroupName = ""
				source.PieceName = ""
				return source
			}
			source.GroupName = tab.Title
			source.PieceName = "主线话术"
		}
	}
	return source
}
