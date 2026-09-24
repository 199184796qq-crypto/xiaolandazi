package coordination

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const roomLeasePrefix = "livecompanion:cluster:room-lease"

var acquireRoomLeaseScript = redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if current then
  local owner = string.match(current, "^(.-)|")
  if owner == ARGV[1] then
    redis.call("PEXPIRE", KEYS[1], ARGV[2])
    return current
  end
  return ""
end

local fence = redis.call("INCR", KEYS[2])
local value = ARGV[1] .. "|" .. tostring(fence)
redis.call("PSETEX", KEYS[1], ARGV[2], value)
return value
`)

var renewRoomLeaseScript = redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if current == ARGV[1] then
  redis.call("PEXPIRE", KEYS[1], ARGV[2])
  return 1
end
return 0
`)

var releaseRoomLeaseScript = redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if current == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

type RoomLease struct {
	TenantID int64
	RoomID   int64
	Owner    string
	Fence    uint64
	value    string
}

type RoomLeaseState struct {
	Owner string
	Fence uint64
}

type RoomLeases struct {
	client        *redis.Client
	nodeID        string
	ttl           time.Duration
	renewInterval time.Duration
}

func NewRoomLeases(
	client *redis.Client,
	nodeID string,
	ttl time.Duration,
	renewInterval time.Duration,
) (*RoomLeases, error) {
	if client == nil {
		return nil, fmt.Errorf("redis client is required")
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil, fmt.Errorf("core node id is required")
	}
	if ttl < 5*time.Second {
		ttl = 15 * time.Second
	}
	if renewInterval <= 0 || renewInterval >= ttl {
		renewInterval = ttl / 3
	}
	return &RoomLeases{
		client:        client,
		nodeID:        nodeID,
		ttl:           ttl,
		renewInterval: renewInterval,
	}, nil
}

func (l *RoomLeases) NodeID() string {
	return l.nodeID
}

func (l *RoomLeases) TTL() time.Duration {
	return l.ttl
}

func (l *RoomLeases) RenewInterval() time.Duration {
	return l.renewInterval
}

func (l *RoomLeases) Acquire(
	ctx context.Context,
	tenantID int64,
	roomID int64,
) (RoomLease, bool, error) {
	leaseKey, fenceKey := roomLeaseKeys(tenantID, roomID)
	value, err := acquireRoomLeaseScript.Run(
		ctx,
		l.client,
		[]string{leaseKey, fenceKey},
		l.nodeID,
		l.ttl.Milliseconds(),
	).Text()
	if err != nil {
		return RoomLease{}, false, fmt.Errorf("acquire room lease: %w", err)
	}
	if strings.TrimSpace(value) == "" {
		return RoomLease{}, false, nil
	}
	owner, fence, err := parseLeaseValue(value)
	if err != nil {
		return RoomLease{}, false, err
	}
	if owner != l.nodeID {
		return RoomLease{}, false, nil
	}
	return RoomLease{
		TenantID: tenantID,
		RoomID:   roomID,
		Owner:    owner,
		Fence:    fence,
		value:    value,
	}, true, nil
}

func (l *RoomLeases) Renew(ctx context.Context, lease RoomLease) (bool, error) {
	leaseKey, _ := roomLeaseKeys(lease.TenantID, lease.RoomID)
	result, err := renewRoomLeaseScript.Run(
		ctx,
		l.client,
		[]string{leaseKey},
		lease.value,
		l.ttl.Milliseconds(),
	).Int64()
	if err != nil {
		return false, fmt.Errorf("renew room lease: %w", err)
	}
	return result == 1, nil
}

func (l *RoomLeases) Release(ctx context.Context, lease RoomLease) error {
	leaseKey, _ := roomLeaseKeys(lease.TenantID, lease.RoomID)
	if _, err := releaseRoomLeaseScript.Run(
		ctx,
		l.client,
		[]string{leaseKey},
		lease.value,
	).Int64(); err != nil {
		return fmt.Errorf("release room lease: %w", err)
	}
	return nil
}

func (l *RoomLeases) Current(
	ctx context.Context,
	tenantID int64,
	roomID int64,
) (RoomLeaseState, bool, error) {
	leaseKey, _ := roomLeaseKeys(tenantID, roomID)
	value, err := l.client.Get(ctx, leaseKey).Result()
	if err != nil {
		if err == redis.Nil {
			return RoomLeaseState{}, false, nil
		}
		return RoomLeaseState{}, false, fmt.Errorf("read room lease: %w", err)
	}
	owner, fence, err := parseLeaseValue(value)
	if err != nil {
		return RoomLeaseState{}, false, err
	}
	return RoomLeaseState{Owner: owner, Fence: fence}, true, nil
}

func roomLeaseKeys(tenantID int64, roomID int64) (string, string) {
	base := fmt.Sprintf("%s:%d:%d", roomLeasePrefix, tenantID, roomID)
	return base, base + ":fence"
}

func parseLeaseValue(value string) (string, uint64, error) {
	owner, fenceRaw, ok := strings.Cut(value, "|")
	if !ok || strings.TrimSpace(owner) == "" {
		return "", 0, fmt.Errorf("invalid room lease value")
	}
	fence, err := strconv.ParseUint(fenceRaw, 10, 64)
	if err != nil || fence == 0 {
		return "", 0, fmt.Errorf("invalid room lease fence")
	}
	return owner, fence, nil
}
