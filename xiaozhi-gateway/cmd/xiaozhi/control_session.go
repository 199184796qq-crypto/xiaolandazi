package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const maxVoiceUploadBytes = 256 << 10

type voiceResult struct {
	requestID  string
	text       string
	err        error
	kind       string
	addressing string
}

type dispatchResult struct {
	requestID string
	err       error
}

type understandingResult struct {
	requestID string
	result    commandUnderstanding
	fallback  string
	err       error
}

type controlMessage struct {
	Type      string          `json:"type"`
	State     string          `json:"state"`
	RequestID string          `json:"request_id"`
	Action    string          `json:"action"`
	Status    string          `json:"status"`
	Value     json.RawMessage `json:"value"`
	Phase     string          `json:"phase"`
	Text      string          `json:"text"`
}

type terminalControlSession struct {
	g                *gateway
	ctx              context.Context
	writer           *lockedConn
	mac, client      string
	sampleRate       int
	enabled          bool
	camera           bool
	lease            controlLease
	state            string
	command          deviceCommand
	packets          [][]byte
	packetBytes      int
	samples          int
	deadline         time.Time
	cancelASR        context.CancelFunc
	results          chan voiceResult
	understandings   chan understandingResult
	dispatches       chan dispatchResult
	terminals        chan terminalResult
	feedbackEnabled  bool
	addressing       string
	suppressFeedback bool
	feedbackPermits  map[string]feedbackPermit
	feedbackActive   string
	feedbackDeadline time.Time
	wakeText         string
}

func newTerminalControlSession(ctx context.Context, g *gateway, writer *lockedConn, mac, client string, sampleRate int, enabled, camera bool, feedback ...bool) *terminalControlSession {
	withFeedback := len(feedback) > 0 && feedback[0] && enabled
	return &terminalControlSession{g: g, ctx: ctx, writer: writer, mac: mac, client: client, sampleRate: sampleRate, enabled: enabled, camera: camera, results: make(chan voiceResult, 1), understandings: make(chan understandingResult, 1), dispatches: make(chan dispatchResult, 1), terminals: make(chan terminalResult, 1), feedbackEnabled: withFeedback, addressing: "neutral", feedbackPermits: make(map[string]feedbackPermit)}
}

func (control *terminalControlSession) status(state, message string) {
	_ = control.writer.json(map[string]any{"type": "control_status", "request_id": control.lease.RequestID, "state": state, "message": message})
}

func (control *terminalControlSession) start(p provisioning, requestID string, now time.Time) {
	if !control.enabled {
		_ = control.writer.json(map[string]any{"type": "control_status", "request_id": requestID, "state": "error", "message": "当前固件仅支持播放，请更新小蓝搭子固件后使用语音指令"})
		return
	}
	if control.state == "listening" {
		return // detect followed by start is the same recording, not a second command.
	}
	if control.state != "" {
		return // Do not change/erase the status of an in-flight command.
	}
	lease, err := control.g.controls.acquire(control.mac, control.client, requestID, p, now)
	if err != nil {
		_ = control.writer.json(map[string]any{"type": "control_status", "request_id": requestID, "state": "error", "message": err.Error()})
		return
	}
	control.lease, control.state = lease, "listening"
	control.packets, control.packetBytes, control.samples = nil, 0, 0
	control.command = deviceCommand{}
	control.wakeText = ""
	control.addressing, control.suppressFeedback = "neutral", false
	control.feedbackPermits = make(map[string]feedbackPermit)
	control.deadline = now.Add(8 * time.Second)
	control.writer.pauseRoomAudio(true)
	control.clearFeedbackPlayback()
	control.status("listening", "正在聆听，请说指令")
}

func (control *terminalControlSession) wake(requestID, text string) {
	if control.state != "listening" || (requestID != "" && requestID != control.lease.RequestID) {
		return
	}
	if strings.ReplaceAll(strings.TrimSpace(text), "，", "") == "小蓝小蓝" {
		control.wakeText = "小蓝小蓝"
	}
}

func (control *terminalControlSession) appendPacket(packet []byte) {
	if control.state != "listening" {
		return
	}
	samples, err := opusPacketSamples(packet)
	if err != nil || control.packetBytes+len(packet) > maxVoiceUploadBytes || len(control.packets) >= 3200 {
		control.finish("rejected", "语音数据格式无效或过大，请重新说一次", nil)
		return
	}
	if control.samples+samples > maxVoiceDurationSamples {
		control.process()
		return
	}
	control.samples += samples
	control.packetBytes += len(packet)
	control.packets = append(control.packets, append([]byte(nil), packet...))
	if control.samples == maxVoiceDurationSamples {
		control.process()
	}
}

func (control *terminalControlSession) process() {
	if control.state != "listening" {
		return
	}
	if len(control.packets) == 0 {
		control.finish("rejected", "没有收到语音，请靠近设备再说一次", nil)
		return
	}
	control.state, control.deadline = "processing", time.Now().Add(32*time.Second)
	// Only microphone capture needs quiet downlink. Recognition/execution can
	// take longer; resume this terminal immediately and keep Core's stream alive.
	control.writer.pauseRoomAudio(false)
	control.status("processing", "正在识别指令")
	commandCtx, cancel := context.WithCancel(control.ctx)
	control.cancelASR = cancel
	lease, packets, wakeText := control.lease, control.packets, control.wakeText
	control.packets = nil
	go func() {
		recognition, err := control.g.recogniseDeviceVoice(commandCtx, lease, packets, control.sampleRate, wakeText)
		select {
		case control.results <- voiceResult{requestID: lease.RequestID, text: recognition.Text, err: err, kind: recognition.Kind, addressing: recognition.SpeakerAddressing}:
		case <-commandCtx.Done():
		}
	}()
}

func (control *terminalControlSession) recognised(result voiceResult) {
	if control.state != "processing" || result.requestID != control.lease.RequestID {
		return // A late response from an aborted request must never execute.
	}
	if control.cancelASR != nil {
		control.cancelASR()
		control.cancelASR = nil
	}
	if result.err != nil {
		control.finish("failed", "语音识别暂不可用，请稍后再试", nil)
		return
	}
	control.addressing = validAddressing(result.addressing)
	if result.kind == "wake_greeting" && isWakeGreetingText(result.text) {
		if control.lease.BindingRole == "listener" {
			control.finish("rejected", "当前是监听设备，仅支持音量和字体设置", nil)
			return
		}
		control.greet()
		return
	}
	command, err := parseDeviceCommand(result.text)
	if err != nil {
		if normalizeCommandText(result.text) == "" {
			control.finish("rejected", err.Error(), nil)
			return
		}
		control.state, control.deadline = "understanding", time.Now().Add(7*time.Second)
		control.status("processing", "正在理解你的意思")
		commandCtx, cancel := context.WithCancel(control.ctx)
		control.cancelASR = cancel
		lease, fallback, text := control.lease, err.Error(), result.text
		go func() {
			understanding, understandErr := control.g.understandDeviceCommand(commandCtx, lease, text)
			select {
			case control.understandings <- understandingResult{requestID: lease.RequestID, result: understanding, fallback: fallback, err: understandErr}:
			case <-commandCtx.Done():
			}
		}()
		return
	}
	control.prepareDispatch(command)
}

func (control *terminalControlSession) understood(result understandingResult) {
	if control.state != "understanding" || result.requestID != control.lease.RequestID {
		return
	}
	if control.cancelASR != nil {
		control.cancelASR()
		control.cancelASR = nil
	}
	if result.err != nil {
		control.finish("rejected", result.fallback, nil)
		return
	}
	if result.result.Status != "understood" {
		message := strings.TrimSpace(result.result.Message)
		if message == "" {
			message = result.fallback
		}
		control.finish("rejected", message, nil)
		return
	}
	command, err := validateUnderstoodCommand(result.result)
	if err != nil {
		control.finish("rejected", result.fallback, nil)
		return
	}
	control.prepareDispatch(command)
}

func (control *terminalControlSession) prepareDispatch(command deviceCommand) {
	control.command = command
	if !bindingRoleAllowsCommand(control.lease.BindingRole, command) {
		control.finish("rejected", "当前是监听设备，仅支持音量和字体设置", nil)
		return
	}
	if command.Action == "capture" && !control.camera {
		control.finish("rejected", "当前设备暂不支持拍照，请更新摄像头固件", nil)
		return
	}
	control.state, control.deadline = "dispatching", time.Now().Add(6*time.Second)
	control.status("processing", "正在准备执行指令")
	commandCtx, cancel := context.WithCancel(control.ctx)
	control.cancelASR = cancel
	lease := control.lease
	go func() {
		err := control.g.dispatchDeviceCommand(commandCtx, lease, command)
		select {
		case control.dispatches <- dispatchResult{requestID: lease.RequestID, err: err}:
		case <-commandCtx.Done():
		}
	}()
}

func bindingRoleAllowsCommand(bindingRole string, command deviceCommand) bool {
	return bindingRole != "listener" || command.Action == "volume" || command.Action == "font_size"
}

func (control *terminalControlSession) dispatched(result dispatchResult) {
	if control.state != "dispatching" || result.requestID != control.lease.RequestID {
		return
	}
	if control.cancelASR != nil {
		control.cancelASR()
		control.cancelASR = nil
	}
	if result.err != nil {
		control.finish("failed", "指令授权失败或已经执行过，请重新发起", nil)
		return
	}
	command := control.command
	payload := map[string]any{"type": "device_control", "request_id": control.lease.RequestID, "action": command.Action, "operation": command.Operation}
	if command.Value != nil {
		payload["value"] = *command.Value
	}
	control.deadline = time.Now().Add(8 * time.Second)
	if command.Action == "capture" {
		address, err := control.g.captureURL(control.lease, time.Now())
		if err != nil {
			control.finish("failed", "拍照上传暂不可用，请稍后再试", nil)
			return
		}
		payload["upload_url"] = address
		payload["token"] = "" // The short, device-bound grant is the only public credential.
		control.deadline = time.Now().Add(45 * time.Second)
	}
	control.state = "pending"
	control.status("processing", "正在执行指令")
	control.feedback("accepted", acceptedMessage(control.addressing), false)
	if err := control.writer.json(payload); err != nil {
		control.finish("failed", "指令发送失败，请稍后再试", nil)
	}
}

func (control *terminalControlSession) acknowledge(message controlMessage) {
	if control.state != "pending" || message.RequestID != control.lease.RequestID || message.Action != control.command.Action {
		return // Ignore forged/stale/unrelated acknowledgements.
	}
	if message.Status == "error" {
		control.finish("failed", "设备未能执行指令，请稍后再试", nil)
		return
	}
	if message.Status != "success" {
		return
	}
	if control.command.Action == "capture" {
		if !control.g.controls.captureAccepted(control.lease) {
			control.finish("failed", "照片尚未上传成功，请重新拍照", nil)
			return
		}
		control.finish("succeeded", "照片已上传，等待环境处理", nil)
		return
	}
	var value int
	if json.Unmarshal(message.Value, &value) != nil || len(message.Value) == 0 || strings.TrimSpace(string(message.Value)) == "null" {
		control.finish("failed", "设备没有返回实际设置值，请稍后再试", nil)
		return
	}
	if control.command.Action == "volume" && (value < 0 || value > 100) {
		control.finish("failed", "设备返回的音量值无效", nil)
		return
	}
	if control.command.Action == "font_size" && (value < 0 || value > 2) {
		control.finish("failed", "设备返回的字体档位无效", nil)
		return
	}
	if control.command.Operation == "set" && control.command.Value != nil && value != *control.command.Value {
		control.finish("failed", "设备返回的实际设置值与目标不符，请重试", &value)
		return
	}
	if control.command.Operation == "mute" && value != 0 {
		control.finish("failed", "设备尚未静音，请重试", &value)
		return
	}
	if control.command.Operation == "unmute" && value == 0 {
		control.finish("failed", "设备仍处于静音状态，请重试", &value)
		return
	}
	if control.command.Action == "font_size" && control.command.Operation == "reset" && value != 1 {
		control.finish("failed", "设备尚未恢复默认字体，请重试", &value)
		return
	}
	if control.command.Action == "volume" {
		message := fmt.Sprintf("音量已调整到%d%%", value)
		if control.command.Operation == "query" {
			message = fmt.Sprintf("当前音量%d%%", value)
		}
		control.finish("succeeded", message, &value)
		return
	}
	control.finish("succeeded", "当前字体："+[]string{"小号", "中号", "大号"}[value], &value)
}

func (control *terminalControlSession) tick(now time.Time) {
	control.feedbackTick(now)
	if control.state == "finishing" {
		return
	}
	if control.state == "" || now.Before(control.deadline) {
		return
	}
	if control.state == "listening" {
		control.process()
		return
	}
	control.finish("timeout", "设备指令处理超时，请稍后再试", nil)
}

func (control *terminalControlSession) finish(status, message string, value *int) {
	if control.state == "" || control.state == "finishing" {
		return
	}
	if control.cancelASR != nil {
		control.cancelASR()
		control.cancelASR = nil
	}
	control.writer.pauseRoomAudio(false)
	if control.feedbackEnabled {
		control.persistTerminal(status, message, value)
		return
	}
	control.g.controls.release(control.lease)
	control.writer.pauseRoomAudio(false)
	if status == "succeeded" {
		control.status("done", message)
	} else {
		control.status("error", message)
	}
	control.g.recordCommandEvent(control.lease, control.command, status, value, message)
	control.state, control.packets = "", nil
}

func (control *terminalControlSession) abort() {
	control.suppressFeedback = true
	control.feedbackPermits = make(map[string]feedbackPermit)
	control.clearFeedbackPlayback()
	control.finish("rejected", "已取消本次指令", nil)
}

func (control *terminalControlSession) close() {
	control.clearFeedbackPlayback()
	if control.state != "" {
		if control.cancelASR != nil {
			control.cancelASR()
		}
		control.g.controls.release(control.lease)
		control.g.recordCommandEvent(control.lease, control.command, "failed", nil, "设备连接中断，指令未完成")
	}
	control.writer.pauseRoomAudio(false)
}
