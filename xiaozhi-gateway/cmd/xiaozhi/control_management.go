package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"time"
)

func (g *gateway) deviceUpload(ctx context.Context, path, filename, mediaType string, data []byte, lease controlLease, out any, fields ...map[string]string) error {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	headers := make(textproto.MIMEHeader)
	headers.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	headers.Set("Content-Type", mediaType)
	part, err := form.CreatePart(headers)
	if err != nil {
		return err
	}
	_, _ = part.Write(data)
	_ = form.WriteField("question", "记录设备环境")
	if len(fields) > 0 {
		for key, value := range fields[0] {
			if value != "" {
				_ = form.WriteField(key, value)
			}
		}
	}
	if err := form.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.managementBaseURL+path, bytes.NewReader(body.Bytes()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("X-Xiaozhi-Internal-Token", g.cfg.internalToken)
	req.Header.Set("X-Device-MAC", lease.MAC)
	req.Header.Set("X-Request-ID", lease.RequestID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("device upload http=%d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(out)
}

type voiceRecognition struct {
	Text              string `json:"text"`
	RequestID         string `json:"request_id"`
	EventID           int64  `json:"event_id"`
	Status            string `json:"status"`
	Kind              string `json:"kind"`
	SpeakerAddressing string `json:"speaker_addressing"`
	BillingMode       string `json:"billing_mode"`
	ChargedBeans      int64  `json:"charged_beans"`
}

type commandUnderstanding struct {
	RequestID    string  `json:"request_id"`
	Status       string  `json:"status"`
	Action       string  `json:"action"`
	Operation    string  `json:"operation"`
	Value        *int    `json:"value"`
	Message      string  `json:"message"`
	Confidence   float64 `json:"confidence"`
	BillingMode  string  `json:"billing_mode"`
	ChargedBeans int64   `json:"charged_beans"`
}

func validateUnderstoodCommand(out commandUnderstanding) (deviceCommand, error) {
	if out.Status != "understood" || out.Confidence < 0.90 {
		return deviceCommand{}, errors.New("command was not understood")
	}
	command := deviceCommand{Action: out.Action, Operation: out.Operation, Value: out.Value}
	switch command.Action {
	case "volume":
		switch command.Operation {
		case "set":
			if command.Value == nil || *command.Value < 0 || *command.Value > 99 {
				return deviceCommand{}, errors.New("invalid semantic volume target")
			}
		case "adjust":
			if command.Value == nil || (*command.Value != -10 && *command.Value != 10) {
				return deviceCommand{}, errors.New("invalid semantic volume adjustment")
			}
		case "mute", "unmute", "query":
			if command.Value != nil {
				return deviceCommand{}, errors.New("unexpected semantic volume value")
			}
		default:
			return deviceCommand{}, errors.New("invalid semantic volume operation")
		}
	case "font_size":
		switch command.Operation {
		case "set":
			if command.Value == nil || *command.Value < 0 || *command.Value > 2 {
				return deviceCommand{}, errors.New("invalid semantic font target")
			}
		case "adjust":
			if command.Value == nil || (*command.Value != -1 && *command.Value != 1) {
				return deviceCommand{}, errors.New("invalid semantic font adjustment")
			}
		case "reset", "query":
			if command.Value != nil {
				return deviceCommand{}, errors.New("unexpected semantic font value")
			}
		default:
			return deviceCommand{}, errors.New("invalid semantic font operation")
		}
	case "capture":
		if command.Operation != "set" || command.Value != nil {
			return deviceCommand{}, errors.New("invalid semantic capture operation")
		}
	default:
		return deviceCommand{}, errors.New("invalid semantic action")
	}
	return command, nil
}

func (g *gateway) understandDeviceCommand(ctx context.Context, lease controlLease, text string) (commandUnderstanding, error) {
	var out commandUnderstanding
	if err := g.managementRequest(ctx, "/internal/v1/xiaozhi/command-understand", map[string]any{
		"hardware_mac": lease.MAC, "request_id": lease.RequestID, "text": text,
	}, &out); err != nil {
		return commandUnderstanding{}, err
	}
	if out.RequestID != lease.RequestID || out.BillingMode != "disabled" || out.ChargedBeans != 0 {
		return commandUnderstanding{}, errors.New("invalid command understanding receipt")
	}
	if out.Status != "understood" && out.Status != "clarify" && out.Status != "unsupported" {
		return commandUnderstanding{}, errors.New("invalid command understanding status")
	}
	if out.Status == "understood" {
		if _, err := validateUnderstoodCommand(out); err != nil {
			return commandUnderstanding{}, err
		}
	}
	return out, nil
}

func (g *gateway) transcribeDeviceVoice(ctx context.Context, lease controlLease, packets [][]byte, sampleRate int) (string, error) {
	out, err := g.recogniseDeviceVoice(ctx, lease, packets, sampleRate)
	return out.Text, err
}

func (g *gateway) recogniseDeviceVoice(ctx context.Context, lease controlLease, packets [][]byte, sampleRate int, wakeText ...string) (voiceRecognition, error) {
	ogg, err := microphoneOgg(packets, sampleRate)
	if err != nil {
		return voiceRecognition{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var out voiceRecognition
	wake := ""
	if len(wakeText) > 0 && wakeText[0] == "小蓝小蓝" {
		wake = wakeText[0]
	}
	if err := g.deviceUpload(ctx, "/internal/v1/xiaozhi/voice", "command.ogg", "audio/ogg", ogg, lease, &out, map[string]string{"wake_text": wake}); err != nil {
		return voiceRecognition{}, err
	}
	// A cached executed/expired request can still contain its old transcript.
	// Never replay relative controls from such a terminal response after restart.
	validKind := (out.Kind == "" || out.Kind == "command") && out.Status == "recognized"
	if out.Kind == "wake_greeting" && out.Status == "succeeded" && isWakeGreetingText(out.Text) {
		validKind = true
	}
	if out.RequestID != lease.RequestID || out.EventID <= 0 || !validKind || out.BillingMode != "disabled" || out.ChargedBeans != 0 {
		return voiceRecognition{}, errors.New("invalid device voice response")
	}
	out.SpeakerAddressing = validAddressing(out.SpeakerAddressing)
	return out, nil
}

func (g *gateway) dispatchDeviceCommand(ctx context.Context, lease controlLease, command deviceCommand) error {
	var out struct {
		RequestID       string `json:"request_id"`
		EventID         int64  `json:"event_id"`
		Status          string `json:"status"`
		DispatchAllowed bool   `json:"dispatch_allowed"`
		BillingMode     string `json:"billing_mode"`
		ChargedBeans    int64  `json:"charged_beans"`
	}
	if err := g.managementRequest(ctx, "/internal/v1/xiaozhi/command-dispatch", map[string]any{
		"hardware_mac": lease.MAC, "request_id": lease.RequestID, "action": command.Action, "operation": command.Operation,
	}, &out); err != nil {
		return err
	}
	if !out.DispatchAllowed || out.RequestID != lease.RequestID || out.EventID <= 0 || out.Status != "executing" || out.BillingMode != "disabled" || out.ChargedBeans != 0 {
		return errors.New("invalid device dispatch response")
	}
	return nil
}

func (g *gateway) persistCommandEvent(ctx context.Context, lease controlLease, command deviceCommand, status string, value *int, message string) error {
	body := map[string]any{"hardware_mac": lease.MAC, "request_id": lease.RequestID, "action": command.Action, "operation": command.Operation, "status": status, "value": value, "message": message}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		var out struct {
			RequestID    string `json:"request_id"`
			EventID      int64  `json:"event_id"`
			Status       string `json:"status"`
			BillingMode  string `json:"billing_mode"`
			ChargedBeans int64  `json:"charged_beans"`
		}
		lastErr = g.managementRequest(ctx, "/internal/v1/xiaozhi/command-events", body, &out)
		if lastErr == nil {
			if out.RequestID == lease.RequestID && out.EventID > 0 && out.Status == status && out.BillingMode == "disabled" && out.ChargedBeans == 0 {
				return nil
			}
			return errors.New("invalid command event receipt")
		}
	}
	return lastErr
}

func (g *gateway) recordCommandEvent(lease controlLease, command deviceCommand, status string, value *int, message string) {
	// Audit is separate from the live receive loop. Only serialised, bounded
	// command results reach this path; it never bills or invokes Core business.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := g.persistCommandEvent(ctx, lease, command, status, value, message); err != nil {
			log.Printf("device command audit unavailable request_id=%s status=%s", lease.RequestID, status)
		}
	}()
}
