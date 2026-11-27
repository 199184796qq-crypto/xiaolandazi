package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/storage"
	"livecompanion/management/internal/voiceasset"
)

const scriptHumanUploadBytes = storage.MaxScriptHumanRecordingBytes
const scriptHumanMaxSeconds = 15 * 60
const scriptHumanVoiceNameLimit = 200

var scriptHumanPartID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
var scriptHumanUploadSlots = make(chan struct{}, 2)
var scriptHumanFormats = map[string]string{
	".mp3": "mp3", ".wav": "wav", ".m4a": "mov", ".aac": "aac", ".flac": "flac",
	".ogg": "ogg", ".opus": "ogg", ".webm": "matroska",
}

func scriptHumanPieceKey(group, entry string) string {
	if !scriptHumanPartID.MatchString(group) || !scriptHumanPartID.MatchString(entry) {
		return ""
	}
	return voiceasset.Digest([]string{group, entry})
}

func (s *Server) registerScriptHumanVoiceRoutes(mux *http.ServeMux) {
	const route = "/api/v1/live-agent-plans/{planID}/script/groups/{group}/pieces/{entry}/recordings"
	s.registerPlanHumanScriptRoutes(mux)
	mux.HandleFunc("GET "+route, s.handleScriptHumanVoices)
	mux.HandleFunc("POST "+route, s.handleScriptHumanVoices)
	mux.HandleFunc("PUT "+route+"/{assetID}", s.handleScriptHumanVoices)
	mux.HandleFunc("PATCH "+route+"/{assetID}", s.handleScriptHumanVoices)
	mux.HandleFunc("DELETE "+route+"/{assetID}", s.handleScriptHumanVoices)
	mux.HandleFunc("GET "+route+"/{assetID}/content", s.handleScriptHumanVoices)
	mux.HandleFunc("GET "+route+"/{assetID}/playback", s.handleScriptHumanVoices)
	mux.HandleFunc("POST "+route+"/uploads/start", s.handleScriptHumanVoices)
	mux.HandleFunc("POST "+route+"/uploads/complete", s.handleScriptHumanVoices)
}

func (s *Server) handleScriptHumanVoices(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	write := r.Method != http.MethodGet && r.Method != http.MethodHead
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), write)
	if !ok {
		return
	}
	if !s.requireLiveSupportPlanScope(w, r, actor, tenant, plan, write) {
		return
	}
	piece := scriptHumanPieceKey(r.PathValue("group"), r.PathValue("entry"))
	planLibrary := strings.Contains(r.Pattern, "/human-scripts")
	if planLibrary {
		piece = db.PlanHumanScriptScope
	}
	if piece == "" {
		writeError(w, http.StatusBadRequest, "话术片标识无效，请保存方案后重试")
		return
	}
	ctx := r.Context()
	if _, err := s.store.GetLiveAgentPlan(ctx, tenant, plan); err != nil {
		writeScriptHumanVoiceError(w, err)
		return
	}
	var assetID int64
	if r.PathValue("assetID") != "" {
		assetID, ok = namedPathID(w, r, "assetID", "真人录音")
		if !ok {
			return
		}
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if planLibrary && s.handlePlanHumanScriptExtra(w, r, tenant, plan, assetID, actor.UserID) {
		return
	}
	switch {
	case strings.HasSuffix(r.Pattern, "/uploads/start"):
		s.startScriptHumanUpload(w, r, tenant, plan, piece, actor.UserID)
		return
	case strings.HasSuffix(r.Pattern, "/uploads/complete"):
		s.completeScriptHumanUpload(w, r, tenant, plan, piece, actor.UserID)
		return
	}
	if !write && assetID > 0 {
		s.serveScriptHumanVoice(w, r, tenant, plan, piece, assetID)
		return
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut:
		s.uploadScriptHumanVoice(w, r, tenant, plan, piece, assetID, actor.UserID)
	case http.MethodPatch:
		var request struct {
			Name string `json:"name"`
		}
		if err := readJSONWithLimit(w, r, &request, 8<<10); err != nil {
			writeError(w, http.StatusBadRequest, "改名请求无效")
			return
		}
		name := strings.TrimSpace(request.Name)
		if name == "" || utf8.RuneCountInString(name) > scriptHumanVoiceNameLimit || strings.ContainsFunc(name, unicode.IsControl) {
			writeError(w, http.StatusBadRequest, "录音名称须为 1～200 字，不能包含换行或控制字符")
			return
		}
		if err := s.store.RenameScriptHumanVoice(ctx, tenant, plan, piece, assetID, name); err != nil {
			writeScriptHumanVoiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": assetID, "name": name})
	case http.MethodDelete:
		if err := s.store.ChangeScriptHumanVoice(ctx, tenant, plan, piece, assetID, actor.UserID, nil); err != nil {
			writeScriptHumanVoiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		s.writeScriptHumanVoices(w, ctx, tenant, plan, piece)
	}
}

func writeScriptHumanVoiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, db.ErrLiveAgentPlanNotFound):
		writeError(w, http.StatusNotFound, "方案或真人录音已不存在，请刷新")
	case errors.Is(err, db.ErrScriptHumanVoiceLimit):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusServiceUnavailable, "真人声音暂时无法保存或读取，请重试")
	}
}

func (s *Server) writeScriptHumanVoices(w http.ResponseWriter, ctx context.Context, tenant, plan int64, piece string) {
	s.writeScriptHumanVoiceResult(w, ctx, tenant, plan, piece, "")
}

func (s *Server) writeScriptHumanVoiceResult(w http.ResponseWriter, ctx context.Context, tenant, plan int64, piece, savedKey string) {
	assets, err := s.store.ListScriptHumanVoices(ctx, tenant, plan, piece)
	if err != nil {
		writeScriptHumanVoiceError(w, err)
		return
	}
	var fission map[int64]db.HumanFissionPartsStatus
	if piece == db.PlanHumanScriptScope {
		fission, err = s.store.ListHumanFissionPartsStatus(ctx, tenant, plan)
		if err != nil {
			writeScriptHumanVoiceError(w, err)
			return
		}
	}
	out := make([]db.ScriptHumanVoiceSummary, 0, len(assets))
	var playback *scriptHumanVoicePlayback
	for _, a := range assets {
		if a.DurationMS != nil {
			item := db.ScriptHumanVoiceSummary{ID: a.ID, Name: a.OriginalName, Duration: *a.DurationMS}
			if piece == db.PlanHumanScriptScope {
				item.TranscriptStatus = planHumanScriptASRStatus(a.Metadata)
				if status, exists := fission[a.ID]; exists {
					item.FissionClearable, item.FissionBusy = true, status.Busy
					if status.Input == db.HumanFissionInput(a) {
						item.FissionParts, item.FissionPending = status.Count, status.Pending && status.Count == 0
					}
				}
			}
			out = append(out, item)
			if a.ObjectKey == savedKey {
				if signed, err := s.scriptHumanVoicePlayback(ctx, a); err == nil {
					playback = &signed
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "max_items": db.MaxScriptHumanVoices, "playback": playback})
}

func (s *Server) uploadScriptHumanVoice(w http.ResponseWriter, r *http.Request, tenant, plan int64, piece string, oldID, actor int64) {
	oss, err := s.directAudioStore()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "录音存储暂不可用")
		return
	}
	select {
	case scriptHumanUploadSlots <- struct{}{}:
		defer func() { <-scriptHumanUploadSlots }()
	default:
		writeError(w, http.StatusTooManyRequests, "录音正在处理中，请稍后重试")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, scriptHumanUploadBytes+65536)
	if err = r.ParseMultipartForm(1 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "音频文件超过 100 MB，或上传格式无效")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "请选择需要上传的音频文件")
		return
	}
	defer file.Close()
	name, format := scriptHumanUploadFile(header.Filename, header.Size)
	if format == "" {
		writeError(w, http.StatusBadRequest, scriptHumanUploadFileError)
		return
	}
	src, err := copyAudioReaderToTemp(file, filepath.Ext(name))
	if err != nil {
		writeScriptHumanVoiceError(w, err)
		return
	}
	defer os.Remove(src)
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		writeScriptHumanVoiceError(w, err)
		return
	}
	key := db.ScriptHumanVoicePrefix(tenant, plan, piece) + hex.EncodeToString(token[:]) + ".wav"
	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
	defer cancel()
	if s.saveScriptHumanVoice(w, ctx, oss, tenant, plan, piece, oldID, actor, key, name, src, format) {
		s.writeScriptHumanVoices(w, ctx, tenant, plan, piece)
	}
}

const scriptHumanUploadFileError = "请选择 100 MB 以内的 MP3、WAV、M4A、AAC、FLAC、OGG、OPUS 或 WebM 音频"

func scriptHumanUploadFile(filename string, size int64) (string, string) {
	name := filepath.Base(strings.ReplaceAll(filename, "\\", "/"))
	name = strings.TrimSpace(strings.Map(func(c rune) rune {
		if unicode.IsControl(c) {
			return -1
		}
		return c
	}, name))
	format := scriptHumanFormats[strings.ToLower(filepath.Ext(name))]
	if size <= 0 || size > scriptHumanUploadBytes || !utf8.ValidString(name) || utf8.RuneCountInString(name) > scriptHumanVoiceNameLimit {
		return "", ""
	}
	return name, format
}

func (s *Server) saveScriptHumanVoice(w http.ResponseWriter, ctx context.Context, oss *storage.OSS, tenant, plan int64, piece string, oldID, actor int64, key, name, src, format string) bool {
	wav, err := normalizeCoreWAV(ctx, src, format, scriptHumanMaxSeconds+1)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "无法读取这段录音，请检查音频格式后重试")
		return false
	}
	defer os.Remove(wav)
	ms, err := volcengineWAVDuration(wav)
	if err != nil || ms <= 0 || ms > scriptHumanMaxSeconds*1000 {
		writeError(w, http.StatusUnprocessableEntity, "录音须包含可播放的声音，每条最多 15 分钟")
		return false
	}
	audio, err := os.Open(wav)
	if err != nil {
		writeScriptHumanVoiceError(w, err)
		return false
	}
	defer audio.Close()
	h := sha256.New()
	size, err := io.Copy(h, audio)
	if err != nil {
		writeScriptHumanVoiceError(w, err)
		return false
	}
	if _, err = audio.Seek(0, io.SeekStart); err != nil {
		writeScriptHumanVoiceError(w, err)
		return false
	}
	if err = oss.PutPrivate(ctx, key, audio, "audio/wav"); err != nil {
		writeError(w, http.StatusBadGateway, "上传录音失败，请重试")
		return false
	}
	duration := uint64(ms)
	input := &model.CreateMediaAssetInput{OriginalName: name, StorageDriver: oss.Driver(), StorageBucket: oss.Bucket(),
		ObjectKey: key, SizeBytes: uint64(size), DurationMS: &duration, ChecksumSHA256: hex.EncodeToString(h.Sum(nil))}
	if err = s.store.ChangeScriptHumanVoice(ctx, tenant, plan, piece, oldID, actor, input); err != nil {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer done()
		// A disconnect at COMMIT may already have protected this object.
		if saved, readErr := s.store.ScriptHumanVoiceObjectRegistered(cleanup, tenant, key); readErr == nil && !saved {
			_ = oss.Delete(cleanup, key)
		}
		writeScriptHumanVoiceError(w, err)
		return false
	}
	return true
}

type scriptHumanVoicePlayback struct {
	ID        int64     `json:"id"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Server) scriptHumanVoicePlayback(ctx context.Context, asset model.MediaAsset) (scriptHumanVoicePlayback, error) {
	oss, err := s.directAudioStore()
	if err != nil || asset.StorageDriver != "oss" || asset.StorageBucket != oss.Bucket() {
		return scriptHumanVoicePlayback{}, errors.New("recording storage unavailable")
	}
	const ttl = 25 * time.Minute
	expires := time.Now().UTC().Add(ttl)
	signed, err := oss.SignedURL(ctx, asset.ObjectKey, ttl)
	if err != nil {
		return scriptHumanVoicePlayback{}, err
	}
	u, err := url.Parse(signed)
	if err != nil || u.Scheme != "https" || u.Host == "" || strings.Contains(u.Hostname(), "-internal.") {
		return scriptHumanVoicePlayback{}, errors.New("public OSS endpoint unavailable")
	}
	return scriptHumanVoicePlayback{ID: asset.ID, URL: signed, ExpiresAt: expires}, nil
}

func (s *Server) serveScriptHumanVoice(w http.ResponseWriter, r *http.Request, tenant, plan int64, piece string, id int64) {
	asset, err := s.store.ScriptHumanVoice(r.Context(), tenant, plan, piece, id)
	if err != nil {
		writeScriptHumanVoiceError(w, err)
		return
	}
	playback, err := s.scriptHumanVoicePlayback(r.Context(), asset)
	if err != nil {
		writeError(w, http.StatusBadGateway, "录音暂时无法读取")
		return
	}
	if strings.HasSuffix(r.Pattern, "/playback") {
		writeJSON(w, http.StatusOK, playback)
		return
	}
	http.Redirect(w, r, playback.URL, http.StatusTemporaryRedirect)
}

// A stable roll for a playback occurrence prevents generation retries and worker
// recovery from turning one 30% decision into multiple chances.
func scriptHumanRoll(tenant, room, session, version int64, piece, occurrence string) *mathrand.Rand {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d/%d/%d/%d/%s/%s", tenant, room, session, version, piece, occurrence)))
	var seed uint64
	for _, v := range hash[:8] {
		seed = (seed << 8) | uint64(v)
	}
	return mathrand.New(mathrand.NewSource(int64(seed)))
}

func scriptHumanCachedAudio(asset model.MediaAsset, unit volcengineScriptUnit) volcengineCachedAudio {
	return volcengineCachedAudio{AssetID: asset.ID, DurationMS: int64(*asset.DurationMS), Text: unit.Text,
		HumanVoice: true, UntimedTranscript: true,
		HumanSource: &mainlineHumanSource{RecordingName: humanSourceChoiceName(asset.OriginalName)}}
}

func (s *Server) pickScriptHumanVoice(ctx context.Context, tenant, room, session int64, v model.LiveAgentPlanVersion, u volcengineScriptUnit, occurrence string) (volcengineCachedAudio, bool) {
	if u.HumanKey == "" || s.store == nil || s.assetStorage == nil {
		return volcengineCachedAudio{}, false
	}
	roll := scriptHumanRoll(tenant, room, session, v.ID, u.HumanKey, occurrence)
	if roll.Intn(100) >= 30 {
		return volcengineCachedAudio{}, false
	}
	work, done := context.WithTimeout(ctx, 4*time.Second)
	defer done()
	items, err := s.store.ListScriptHumanVoices(work, tenant, v.PlanID, u.HumanKey)
	if err != nil {
		return volcengineCachedAudio{}, false
	}
	for _, i := range roll.Perm(len(items)) {
		a := items[i]
		if work.Err() != nil {
			break
		}
		if a.DurationMS != nil && *a.DurationMS > 0 && s.scriptAudioAvailable(work, tenant, a.ID) {
			return scriptHumanCachedAudio(a, u), true
		}
	}
	return volcengineCachedAudio{}, false
}
