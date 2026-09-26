package model

import "time"

const (
	SpeechAnalysisQueued    = "queued"
	SpeechAnalysisUploading = "uploading"
	SpeechAnalysisASR       = "transcribing"
	SpeechAnalysisAnalyzing = "analyzing"
	SpeechAnalysisRendering = "rendering"
	SpeechAnalysisReady     = "ready"
	SpeechAnalysisFailed    = "failed"
)

type SpeechAnalysisTask struct {
	ID                  int64      `json:"id"`
	TenantID            int64      `json:"tenant_id"`
	RoomID              int64      `json:"room_id"`
	RecordingID         string     `json:"recording_id"`
	RecordingStartedAt  time.Time  `json:"recording_started_at"`
	Status              string     `json:"status"`
	Stage               string     `json:"stage"`
	Progress            int        `json:"progress"`
	ErrorMessage        string     `json:"error_message,omitempty"`
	ASRTaskID           string     `json:"asr_task_id,omitempty"`
	AudioObjectKey      string     `json:"audio_object_key,omitempty"`
	TranscriptObjectKey string     `json:"transcript_object_key,omitempty"`
	ReportObjectKey     string     `json:"report_object_key,omitempty"`
	ReportFileName      string     `json:"report_file_name,omitempty"`
	CreatedByUserID     int64      `json:"created_by_user_id"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	FinishedAt          *time.Time `json:"finished_at,omitempty"`
}
