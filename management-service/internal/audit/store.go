package audit

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"livecompanion/management/internal/model"
)

const streamKey = "livecompanion:mgmt:audit"

type Ledger interface {
	BeginAdminAudit(context.Context, model.AdminAuditLog) (string, error)
	CompleteAdminAudit(context.Context, string, model.AdminAuditLog) error
	ListAdminAudits(context.Context, int64) ([]model.AdminAuditLog, error)
}

type Store struct {
	client *redis.Client
	ledger Ledger
	limit  int64
}

func New(
	addr string,
	password string,
	db int,
	limit int,
	ledger Ledger,
) *Store {
	if limit <= 0 {
		limit = 10000
	}
	return &Store{
		client: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       db,
		}),
		ledger: ledger,
		limit:  int64(limit),
	}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *Store) Close() error {
	return s.client.Close()
}

func (s *Store) Begin(
	ctx context.Context,
	entry model.AdminAuditLog,
) (string, error) {
	if entry.OccurredAt.IsZero() {
		entry.OccurredAt = time.Now().UTC()
	}
	ledgerID, err := s.ledger.BeginAdminAudit(ctx, entry)
	if err != nil {
		return "", err
	}
	streamID, err := s.appendRedis(ctx, entry)
	if err != nil {
		log.Printf(
			"admin audit redis mirror failed ledger_id=%s action=%s: %v",
			ledgerID,
			entry.Action,
			err,
		)
		streamID = ""
	}
	return ledgerID + "|" + streamID, nil
}

func (s *Store) appendRedis(
	ctx context.Context,
	entry model.AdminAuditLog,
) (string, error) {
	mirrorCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	values := map[string]any{
		"occurred_at":        entry.OccurredAt.UTC().Format(time.RFC3339Nano),
		"actor_user_id":      entry.ActorUserID,
		"actor_username":     entry.ActorUsername,
		"actor_role":         entry.ActorRole,
		"actor_type":         entry.ActorType,
		"source":             entry.Source,
		"action":             entry.Action,
		"target_user_id":     entry.TargetUserID,
		"target_username":    entry.TargetUsername,
		"target_tenant_id":   entry.TargetTenantID,
		"target_room_id":     entry.TargetRoomID,
		"object_type":        entry.ObjectType,
		"object_id":          entry.ObjectID,
		"object_name":        entry.ObjectName,
		"reason":             entry.Reason,
		"before_state":       entry.BeforeState,
		"after_state":        entry.AfterState,
		"runtime_session_id": entry.RuntimeSessionID,
		"core_boot_id":       entry.CoreBootID,
		"request_id":         entry.RequestID,
		"detail_json":        entry.DetailJSON,
		"http_method":        entry.HTTPMethod,
		"path":               entry.Path,
		"client_ip":          entry.ClientIP,
		"result":             entry.Result,
	}
	return s.client.XAdd(mirrorCtx, &redis.XAddArgs{
		Stream: streamKey,
		MaxLen: s.limit,
		Approx: true,
		Values: values,
	}).Result()
}

func (s *Store) Record(
	ctx context.Context,
	entry model.AdminAuditLog,
) error {
	if entry.OccurredAt.IsZero() {
		entry.OccurredAt = time.Now().UTC()
	}
	ledgerID, err := s.ledger.BeginAdminAudit(ctx, entry)
	if err != nil {
		return err
	}
	if _, err := s.appendRedis(ctx, entry); err != nil {
		log.Printf(
			"admin audit redis mirror failed ledger_id=%s action=%s: %v",
			ledgerID,
			entry.Action,
			err,
		)
	}
	return nil
}

func (s *Store) Complete(
	ctx context.Context,
	pendingID string,
	entry model.AdminAuditLog,
) error {
	ledgerID, streamID, _ := strings.Cut(pendingID, "|")
	if err := s.ledger.CompleteAdminAudit(ctx, ledgerID, entry); err != nil {
		return err
	}

	entry.OccurredAt = time.Now().UTC()
	finalID, err := s.appendRedis(ctx, entry)
	if err != nil {
		log.Printf(
			"admin audit redis finalize mirror failed ledger_id=%s action=%s: %v",
			ledgerID,
			entry.Action,
			err,
		)
		return nil
	}
	if streamID == "" || finalID == streamID {
		return nil
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cleanupCancel()
	if err := s.client.XDel(cleanupCtx, streamKey, streamID).Err(); err != nil {
		log.Printf(
			"admin audit redis pending cleanup failed ledger_id=%s stream_id=%s: %v",
			ledgerID,
			streamID,
			err,
		)
	}
	return nil
}

func (s *Store) List(
	ctx context.Context,
	limit int64,
) ([]model.AdminAuditLog, error) {
	return s.ledger.ListAdminAudits(ctx, limit)
}
