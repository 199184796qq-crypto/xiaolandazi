package events

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"livecompanion/core/internal/model"
)

// userCacheTTL 是用户维度临时存储的兜底过期时间。直播正常结束由 ClearRoom
// 主动整组销毁；若进程异常退出未清理，这里保证不会永久残留。
const userCacheTTL = 24 * time.Hour

// userKey 是一个观众在本房间的维度存储（Hash）：
//   nickname 昵称 / enter_at 首次进房时间 / last_chat_at 最近弹幕时间
//   last_event_at 最近事件时间 / chat_count 弹幕次数 / event_count 事件次数
func userKey(roomID int64, userID string) string {
	return "livecompanion:room:" + strconv.FormatInt(roomID, 10) + ":user:" + userID
}

// roomUsersKey 是房间内所有 userID 的索引（ZSet），score=最近事件时间，
// 用于直播结束整组清理，也用于按进房/活跃时间遍历房间观众。
func roomUsersKey(roomID int64) string {
	return "livecompanion:room:" + strconv.FormatInt(roomID, 10) + ":users"
}

// persistUserDimension 把有 UserID 的观众事件写入用户维度临时存储。
// 无 UserID 的 room/session 事件不进入用户维度。
func (s *Store) persistUserDimension(ctx context.Context, pipe redis.Pipeliner, items []model.RoomEvent) {
	for _, item := range items {
		uid := strings.TrimSpace(item.UserID)
		if uid == "" {
			continue
		}
		uk := userKey(item.RoomID, uid)
		rk := roomUsersKey(item.RoomID)
		ts := item.OccurredAt.UTC().Unix()

		pipe.HSet(ctx, uk, "nickname", item.Nickname, "last_event_at", ts)
		switch strings.ToLower(strings.TrimSpace(item.EventType)) {
		case "member", "enter":
			// 首次进房时间只记一次，重复进房不覆盖。
			pipe.HSetNX(ctx, uk, "enter_at", ts)
		case "chat", "comment":
			pipe.HSet(ctx, uk, "last_chat_at", ts)
			pipe.HIncrBy(ctx, uk, "chat_count", 1)
		}
		pipe.HIncrBy(ctx, uk, "event_count", 1)
		pipe.ZAdd(ctx, rk, redis.Z{Score: float64(ts), Member: uid})
		pipe.Expire(ctx, uk, userCacheTTL)
		pipe.Expire(ctx, rk, userCacheTTL)
	}
}

// UserDimension 是单个观众在本房间的临时维度信息。
type UserDimension struct {
	UserID      string
	Nickname    string
	EnterAt     int64
	LastChatAt  int64
	LastEventAt int64
	ChatCount   int64
	EventCount  int64
}

// GetRoomUser 读取单个观众的维度信息。不存在或读取失败返回零值、ok=false。
func (s *Store) GetRoomUser(ctx context.Context, roomID int64, userID string) (UserDimension, bool) {
	var out UserDimension
	if s.redis == nil || roomID <= 0 || strings.TrimSpace(userID) == "" {
		return out, false
	}
	raw, err := s.redis.HGetAll(ctx, userKey(roomID, userID)).Result()
	if err != nil || len(raw) == 0 {
		return out, false
	}
	out.UserID = userID
	out.Nickname = raw["nickname"]
	out.EnterAt = atoi64(raw["enter_at"])
	out.LastChatAt = atoi64(raw["last_chat_at"])
	out.LastEventAt = atoi64(raw["last_event_at"])
	out.ChatCount = atoi64(raw["chat_count"])
	out.EventCount = atoi64(raw["event_count"])
	return out, true
}

// ListRoomUsers 按进房/活跃时间倒序返回房间内所有 userID。
// 供主动搭话、进房欢迎、点赞关注点名的观众选择使用。
func (s *Store) ListRoomUsers(ctx context.Context, roomID int64) []string {
	if s.redis == nil || roomID <= 0 {
		return nil
	}
	ids, err := s.redis.ZRevRange(ctx, roomUsersKey(roomID), 0, -1).Result()
	if err != nil {
		return nil
	}
	return ids
}

func atoi64(v string) int64 {
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}
