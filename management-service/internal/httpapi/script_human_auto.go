package httpapi

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"livecompanion/management/internal/sharedfilelock"
)

const scriptMainlineAI = "ai"
const scriptMainlineHuman = "human"

type scriptMainlinePreference struct {
	Mode string `json:"mode"`
}

func scriptMainlineModePath(root string, tenant, room int64) string {
	return filepath.Join(root, "script-room-playback", fmt.Sprint(tenant), fmt.Sprint(room), "mainline-mode.json")
}

func readScriptMainlineMode(path string) (string, error) {
	var preference scriptMainlinePreference
	err := readVolcengineJSON(path, &preference)
	if errors.Is(err, os.ErrNotExist) {
		return scriptMainlineAI, nil
	}
	if err == nil && preference.Mode != scriptMainlineAI && preference.Mode != scriptMainlineHuman {
		err = errors.New("主线话术模式无效")
	}
	return preference.Mode, err
}

func writeScriptMainlineMode(ctx context.Context, path, mode string) error {
	unlock, err := sharedfilelock.Lock(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer unlock()
	return writeVolcengineJSON(path, scriptMainlinePreference{Mode: mode})
}

func scriptManualMode(pieceID string) string {
	if scriptStandaloneChoice(pieceID) {
		return scriptMainlineHuman
	}
	return scriptMainlineAI
}

func (s *Server) updateRoomMainlineMode(w http.ResponseWriter, r *http.Request, ctx context.Context, tenant, room int64, options scriptManualOptions) {
	var input struct {
		SessionID int64  `json:"session_id"`
		VersionID int64  `json:"version_id"`
		ProgramID string `json:"program_id"`
		Mode      string `json:"mainline_mode"`
	}
	if readJSON(w, r, &input) != nil || (input.Mode != scriptMainlineAI && input.Mode != scriptMainlineHuman) {
		writeError(w, http.StatusBadRequest, "主线话术设置无效")
		return
	}
	if !options.ModeEditable || input.SessionID != options.SessionID || input.VersionID != options.VersionID || input.ProgramID != options.ProgramID {
		writeError(w, http.StatusConflict, "直播场次或方案已变化，请刷新后重试")
		return
	}
	if options.SessionID != 0 {
		writeError(w, http.StatusConflict, "直播中请选择一条话术切换模式")
		return
	}
	if input.Mode == scriptMainlineHuman && options.MainlineMode != scriptMainlineHuman {
		if !options.HumanSupported {
			writeError(w, http.StatusConflict, "当前方案不支持真人话术主线")
			return
		}
		sources, err := s.store.ListPlanHumanScriptSources(ctx, tenant, options.PlanID)
		if err != nil || s.assetStorage == nil || len(sources) == 0 {
			writeError(w, http.StatusConflict, "当前方案没有可用的真人原始录音")
			return
		}
	}
	if err := writeScriptMainlineMode(ctx, scriptMainlineModePath(recordingCacheMediaRoot(), tenant, room), input.Mode); err != nil {
		writeError(w, http.StatusInternalServerError, "暂时无法保存主线话术设置")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"mainline_mode": input.Mode})
}

func scriptHumanChoiceKey(choice string) string {
	if source, _, recipe, err := parseFissionChoiceID(choice); err == nil {
		return fmt.Sprintf("%s%d:%s", fissionChoicePrefix, source, recipe)
	}
	return choice
}

// Every original and combination has one entry across the whole room pool.
// With four or more entries, the previous choice remains a last resort if all
// alternatives are unavailable. A one-entry pool can play that entry again.
func scriptHumanChoiceOrder(groups []scriptManualGroup, previous string, rng *rand.Rand) []scriptManualPiece {
	var choices []scriptManualPiece
	for _, group := range groups {
		choices = append(choices, group.Pieces...)
	}
	rng.Shuffle(len(choices), func(i, j int) { choices[i], choices[j] = choices[j], choices[i] })
	if len(choices) > 3 && previous != "" {
		for i, choice := range choices {
			if scriptHumanChoiceKey(choice.ID) == previous {
				copy(choices[i:], choices[i+1:])
				choices[len(choices)-1] = choice
				break
			}
		}
	}
	return choices
}

func (s *Server) pickAutomaticHuman(ctx context.Context, tenant, plan, session int64, previous string, rng *rand.Rand, nextPrepare *time.Time, allowPreparation bool) (scriptManualPiece, volcengineCachedAudio) {
	groups, _ := s.humanScriptPlaybackGroups(ctx, tenant, plan)
	for _, choice := range scriptHumanChoiceOrder(groups, previous, rng) {
		if ctx.Err() != nil {
			break
		}
		if strings.HasPrefix(choice.ID, humanSourceChoicePrefix) {
			asset, err := s.loadHumanSourceChoice(ctx, tenant, plan, choice.ID)
			if err == nil && s.scriptAudioAvailable(ctx, tenant, asset.ID) {
				return choice, humanSourceCachedAudio(asset)
			}
			continue
		}
		now := time.Now()
		prepare := allowPreparation && !now.Before(*nextPrepare)
		command := scriptManualJump{PieceID: choice.ID, RequestedAt: now, RequestID: fmt.Sprintf("auto-%d-%s", session, shortScriptIdentity(choice.ID))}
		audio, ready, err := s.prepareManualFission(ctx, tenant, plan, command, prepare)
		if ready && err == nil {
			return choice, audio
		}
		if prepare {
			*nextPrepare = now.Add(30 * time.Second)
		}
	}
	return scriptManualPiece{}, volcengineCachedAudio{}
}
