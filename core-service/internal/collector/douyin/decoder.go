package douyin

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"livecompanion/core/internal/model"
)

type DecodeResult struct {
	Valid  bool
	Events []model.CreateEventInput
}

type pushFrame struct {
	payload         []byte
	payloadEncoding string
	payloadType     string
	headers         map[string]string
}

type webcastMessage struct {
	method  string
	payload []byte
	msgID   uint64
}

type common struct {
	method     string
	msgID      uint64
	roomID     uint64
	createTime uint64
}

type user struct {
	id       string
	nickname string
	secUID   string
	avatar   string
}

func DecodePushFrame(data []byte) (DecodeResult, error) {
	frame, err := decodePushFrame(data)
	if err != nil {
		return DecodeResult{}, err
	}

	if frame.payloadType != "" &&
		frame.payloadType != "msg" &&
		frame.payloadType != "push" {
		return DecodeResult{}, nil
	}

	payload := frame.payload
	if shouldGunzip(frame, payload) {
		payload, err = gunzip(payload)
		if err != nil {
			return DecodeResult{}, fmt.Errorf("gunzip response: %w", err)
		}
	}

	return DecodeResponseBody(payload)
}

func DecodeResponseBody(data []byte) (DecodeResult, error) {
	payload := data
	if len(payload) >= 2 && payload[0] == 0x1f && payload[1] == 0x8b {
		var err error
		payload, err = gunzip(payload)
		if err != nil {
			return DecodeResult{}, fmt.Errorf("gunzip response body: %w", err)
		}
	}

	messages, err := decodeResponse(payload)
	if err != nil {
		return DecodeResult{}, fmt.Errorf("decode response: %w", err)
	}

	result := DecodeResult{
		Valid:  true,
		Events: make([]model.CreateEventInput, 0, len(messages)),
	}

	for _, message := range messages {
		event, ok := normalizeMessage(message)
		if !ok {
			continue
		}
		result.Events = append(result.Events, event)
	}

	return result, nil
}

func decodePushFrame(data []byte) (pushFrame, error) {
	fields, err := parseFields(data)
	if err != nil {
		return pushFrame{}, err
	}

	payload := fieldBytes(fields, 8)
	if len(payload) == 0 {
		return pushFrame{}, fmt.Errorf("%w: push frame payload missing", errInvalidProto)
	}

	frame := pushFrame{
		payload:         payload,
		payloadEncoding: fieldString(fields, 6),
		payloadType:     fieldString(fields, 7),
		headers:         make(map[string]string),
	}

	for _, raw := range repeatedBytes(fields, 5) {
		headerFields, err := parseFields(raw)
		if err != nil {
			continue
		}
		key := fieldString(headerFields, 1)
		if key == "" {
			continue
		}
		frame.headers[strings.ToLower(key)] = fieldString(headerFields, 2)
	}

	return frame, nil
}

func shouldGunzip(frame pushFrame, payload []byte) bool {
	if len(payload) >= 2 && payload[0] == 0x1f && payload[1] == 0x8b {
		return true
	}
	if strings.Contains(strings.ToLower(frame.payloadEncoding), "gzip") {
		return true
	}
	return strings.EqualFold(frame.headers["compress_type"], "gzip")
}

func gunzip(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

func decodeResponse(data []byte) ([]webcastMessage, error) {
	fields, err := parseFields(data)
	if err != nil {
		return nil, err
	}

	rawMessages := repeatedBytes(fields, 1)
	messages := make([]webcastMessage, 0, len(rawMessages))

	for _, raw := range rawMessages {
		messageFields, err := parseFields(raw)
		if err != nil {
			continue
		}

		method := fieldString(messageFields, 1)
		payload := fieldBytes(messageFields, 2)
		if method == "" || len(payload) == 0 {
			continue
		}

		messages = append(messages, webcastMessage{
			method:  method,
			payload: payload,
			msgID:   fieldVarint(messageFields, 3),
		})
	}

	return messages, nil
}

func normalizeMessage(message webcastMessage) (model.CreateEventInput, bool) {
	method := message.method

	switch {
	case strings.Contains(method, "ChatMessage"):
		return decodeChat(message)
	case strings.Contains(method, "MemberMessage"):
		return decodeMember(message)
	case strings.Contains(method, "LikeMessage"):
		return decodeLike(message)
	case strings.Contains(method, "SocialMessage"):
		return decodeSocial(message)
	case strings.Contains(method, "RoomUserSeqMessage"):
		return decodeRoomUserSeq(message)
	case strings.Contains(method, "GiftMessage"):
		return decodeGift(message)
	default:
		return model.CreateEventInput{}, false
	}
}

func decodeChat(message webcastMessage) (model.CreateEventInput, bool) {
	fields, err := parseFields(message.payload)
	if err != nil {
		return model.CreateEventInput{}, false
	}

	commonInfo := decodeCommon(fieldBytes(fields, 1))
	userInfo := decodeUser(fieldBytes(fields, 2))
	content := fieldString(fields, 3)
	if content == "" {
		return model.CreateEventInput{}, false
	}

	return newEvent(
		"chat",
		message,
		commonInfo,
		userInfo,
		content,
		map[string]any{},
	), true
}

func decodeMember(message webcastMessage) (model.CreateEventInput, bool) {
	fields, err := parseFields(message.payload)
	if err != nil {
		return model.CreateEventInput{}, false
	}

	commonInfo := decodeCommon(fieldBytes(fields, 1))
	userInfo := decodeUser(fieldBytes(fields, 2))
	memberCount := fieldVarint(fields, 3)

	return newEvent(
		"member",
		message,
		commonInfo,
		userInfo,
		"进入直播间",
		map[string]any{
			"member_count": memberCount,
		},
	), true
}

func decodeLike(message webcastMessage) (model.CreateEventInput, bool) {
	fields, err := parseFields(message.payload)
	if err != nil {
		return model.CreateEventInput{}, false
	}

	commonInfo := decodeCommon(fieldBytes(fields, 1))
	userInfo := decodeUser(fieldBytes(fields, 5))
	count := fieldVarint(fields, 2)
	total := fieldVarint(fields, 3)

	content := "点赞"
	if count > 1 {
		content += " × " + strconv.FormatUint(count, 10)
	}

	return newEvent(
		"like",
		message,
		commonInfo,
		userInfo,
		content,
		map[string]any{
			"count": count,
			"total": total,
		},
	), true
}

func decodeSocial(message webcastMessage) (model.CreateEventInput, bool) {
	fields, err := parseFields(message.payload)
	if err != nil {
		return model.CreateEventInput{}, false
	}

	commonInfo := decodeCommon(fieldBytes(fields, 1))
	userInfo := decodeUser(fieldBytes(fields, 2))
	action := fieldVarint(fields, 4)

	return newEvent(
		"follow",
		message,
		commonInfo,
		userInfo,
		"关注了主播",
		map[string]any{
			"action": action,
		},
	), true
}

func decodeRoomUserSeq(message webcastMessage) (model.CreateEventInput, bool) {
	fields, err := parseFields(message.payload)
	if err != nil {
		return model.CreateEventInput{}, false
	}

	commonInfo := decodeCommon(fieldBytes(fields, 1))
	total := fieldVarint(fields, 3)
	popularity := fieldVarint(fields, 6)
	totalUser := fieldVarint(fields, 7)

	onlineCount := total
	if onlineCount == 0 {
		onlineCount = totalUser
	}

	content := "房间人数更新"
	if onlineCount > 0 {
		content = "当前在线 " + strconv.FormatUint(onlineCount, 10)
	}

	return newEvent(
		"room",
		message,
		commonInfo,
		user{},
		content,
		map[string]any{
			"online_count": onlineCount,
			"total":        total,
			"popularity":   popularity,
			"total_user":   totalUser,
		},
	), true
}

func decodeGift(message webcastMessage) (model.CreateEventInput, bool) {
	fields, err := parseFields(message.payload)
	if err != nil {
		return model.CreateEventInput{}, false
	}

	commonInfo := decodeCommon(fieldBytes(fields, 1))
	userInfo := decodeUser(fieldBytes(fields, 7))
	giftID := fieldVarint(fields, 2)
	repeatCount := fieldVarint(fields, 5)
	comboCount := fieldVarint(fields, 6)
	repeatEnd := fieldVarint(fields, 9)

	giftName := ""
	giftIcon := ""
	diamondCount := uint64(0)
	if giftFields, giftErr := parseFields(fieldBytes(fields, 15)); giftErr == nil {
		giftName = fieldString(giftFields, 16)
		diamondCount = fieldVarint(giftFields, 12)
		if imageFields, imageErr := parseFields(fieldBytes(giftFields, 1)); imageErr == nil {
			urls := repeatedBytes(imageFields, 1)
			if len(urls) > 0 {
				giftIcon = string(urls[0])
			}
		}
	}

	count := comboCount
	if count == 0 {
		count = repeatCount
	}
	if count == 0 {
		count = 1
	}

	content := "送出礼物"
	if giftName != "" {
		content = "送出 " + giftName
	}
	if count > 1 {
		content += " × " + strconv.FormatUint(count, 10)
	}

	return newEvent(
		"gift",
		message,
		commonInfo,
		userInfo,
		content,
		map[string]any{
			"gift_id":       giftID,
			"gift_name":     giftName,
			"gift_icon":     giftIcon,
			"diamond_count": diamondCount,
			"repeat_count":  repeatCount,
			"combo_count":   comboCount,
			"repeat_end":    repeatEnd,
		},
	), true
}

func decodeCommon(data []byte) common {
	fields, err := parseFields(data)
	if err != nil {
		return common{}
	}

	return common{
		method:     fieldString(fields, 1),
		msgID:      fieldVarint(fields, 2),
		roomID:     fieldVarint(fields, 3),
		createTime: fieldVarint(fields, 4),
	}
}

func decodeUser(data []byte) user {
	fields, err := parseFields(data)
	if err != nil {
		return user{}
	}

	id := fieldString(fields, 1029)
	if id == "" {
		if value := fieldVarint(fields, 1); value > 0 {
			id = strconv.FormatUint(value, 10)
		}
	}

	avatar := ""
	if imageFields, imageErr := parseFields(fieldBytes(fields, 10)); imageErr == nil {
		urls := repeatedBytes(imageFields, 1)
		if len(urls) > 0 {
			avatar = string(urls[0])
		}
	}
	if avatar == "" {
		avatar = findHTTPURL(fieldString(fields, 9))
	}

	return user{
		id:       id,
		nickname: fieldString(fields, 3),
		secUID:   fieldString(fields, 46),
		avatar:   avatar,
	}
}

func findHTTPURL(value string) string {
	index := strings.Index(value, "http://")
	if httpsIndex := strings.Index(value, "https://"); httpsIndex >= 0 &&
		(index < 0 || httpsIndex < index) {
		index = httpsIndex
	}
	if index < 0 {
		return ""
	}

	value = value[index:]
	if end := strings.IndexAny(value, " \t\r\n"); end >= 0 {
		value = value[:end]
	}
	return value
}

func newEvent(
	eventType string,
	message webcastMessage,
	commonInfo common,
	userInfo user,
	content string,
	extra map[string]any,
) model.CreateEventInput {
	payload := map[string]any{
		"source": "douyin",
		"method": message.method,
		"msg_id": firstNonZero(message.msgID, commonInfo.msgID),
	}
	if commonInfo.roomID > 0 {
		payload["room_id"] = commonInfo.roomID
	}
	if userInfo.secUID != "" {
		payload["sec_uid"] = userInfo.secUID
	}
	if userInfo.avatar != "" {
		payload["avatar"] = userInfo.avatar
	}
	for key, value := range extra {
		payload[key] = value
	}

	rawPayload, _ := json.Marshal(payload)

	return model.CreateEventInput{
		EventType:  eventType,
		UserID:     userInfo.id,
		Nickname:   userInfo.nickname,
		Content:    content,
		OccurredAt: douyinTime(commonInfo.createTime),
		Payload:    rawPayload,
	}
}

func firstNonZero(values ...uint64) uint64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func douyinTime(value uint64) time.Time {
	if value == 0 {
		return time.Now().UTC()
	}

	if value > 10_000_000_000 {
		return time.UnixMilli(int64(value)).UTC()
	}

	return time.Unix(int64(value), 0).UTC()
}
