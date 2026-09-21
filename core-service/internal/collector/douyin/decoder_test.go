package douyin

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"testing"
	"time"
)

func TestDecodePushFrameChat(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)

	common := protoMessage(
		protoString(1, "WebcastChatMessage"),
		protoVarint(2, 901),
		protoVarint(3, 777),
		protoVarint(4, uint64(now.UnixMilli())),
	)

	user := protoMessage(
		protoVarint(1, 12345),
		protoString(3, "测试观众"),
		protoString(1029, "u-12345"),
	)

	chat := protoMessage(
		protoBytes(1, common),
		protoBytes(2, user),
		protoString(3, "这个多少钱？"),
	)

	message := protoMessage(
		protoString(1, "WebcastChatMessage"),
		protoBytes(2, chat),
		protoVarint(3, 901),
	)

	response := protoMessage(protoBytes(1, message))
	compressed := gzipBytes(t, response)

	header := protoMessage(
		protoString(1, "compress_type"),
		protoString(2, "gzip"),
	)

	frame := protoMessage(
		protoBytes(5, header),
		protoString(6, "gzip"),
		protoString(7, "msg"),
		protoBytes(8, compressed),
	)

	result, err := DecodePushFrame(frame)
	if err != nil {
		t.Fatalf("DecodePushFrame() error = %v", err)
	}
	if !result.Valid {
		t.Fatal("expected valid frame")
	}
	if len(result.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(result.Events))
	}

	event := result.Events[0]
	if event.EventType != "chat" {
		t.Fatalf("event type = %q, want chat", event.EventType)
	}
	if event.UserID != "u-12345" {
		t.Fatalf("user id = %q", event.UserID)
	}
	if event.Nickname != "测试观众" {
		t.Fatalf("nickname = %q", event.Nickname)
	}
	if event.Content != "这个多少钱？" {
		t.Fatalf("content = %q", event.Content)
	}
	if diff := event.OccurredAt.Sub(now); diff < -time.Millisecond || diff > time.Millisecond {
		t.Fatalf("occurred_at = %v, want about %v", event.OccurredAt, now)
	}
}

func TestDecodeRoomUserSeq(t *testing.T) {
	common := protoMessage(
		protoString(1, "WebcastRoomUserSeqMessage"),
		protoVarint(2, 902),
		protoVarint(3, 888),
	)

	roomUserSeq := protoMessage(
		protoBytes(1, common),
		protoVarint(3, 126),
		protoVarint(6, 999),
		protoVarint(7, 326),
	)

	message := protoMessage(
		protoString(1, "WebcastRoomUserSeqMessage"),
		protoBytes(2, roomUserSeq),
		protoVarint(3, 902),
	)

	response := protoMessage(protoBytes(1, message))
	frame := protoMessage(protoBytes(8, response))

	result, err := DecodePushFrame(frame)
	if err != nil {
		t.Fatalf("DecodePushFrame() error = %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(result.Events))
	}
	if result.Events[0].EventType != "room" {
		t.Fatalf("event type = %q, want room", result.Events[0].EventType)
	}
	if !bytes.Contains(result.Events[0].Payload, []byte(`"online_count":126`)) {
		t.Fatalf("payload = %s", result.Events[0].Payload)
	}
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func protoMessage(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}

func protoVarint(field int, value uint64) []byte {
	output := appendVarint(nil, uint64(field<<3))
	return appendVarint(output, value)
}

func protoString(field int, value string) []byte {
	return protoBytes(field, []byte(value))
}

func protoBytes(field int, value []byte) []byte {
	output := appendVarint(nil, uint64(field<<3|2))
	output = appendVarint(output, uint64(len(value)))
	return append(output, value...)
}

func appendVarint(dst []byte, value uint64) []byte {
	var buffer [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buffer[:], value)
	return append(dst, buffer[:n]...)
}
