package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/audit"
	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/coordination"
	"livecompanion/management/internal/coreclient"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/decisionexecutor"
	"livecompanion/management/internal/httpapi"
	"livecompanion/management/internal/liveruntime"
	"livecompanion/management/internal/mailer"
	"livecompanion/management/internal/questioncluster"
	assetstorage "livecompanion/management/internal/storage"
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/workinbox"
)

func main() {
	cfg := config.Load()
	appCtx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	store, err := appdb.Open(cfg)
	if err != nil {
		log.Fatalf("open management database: %v", err)
	}
	defer store.Close()

	// Cloud RDS adds network latency to each idempotent migration statement.
	// Keep startup migrations bounded, but give the full migration chain enough
	// time to complete instead of sharing the old local-MySQL 20s budget.
	ctx, cancel := context.WithTimeout(appCtx, 3*time.Minute)
	defer cancel()

	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migrate management database: %v", err)
	}
	if err := store.MigrateAdminAudit(ctx); err != nil {
		log.Fatalf("migrate audit database: %v", err)
	}
	if err := store.MigrateSystemSettings(ctx); err != nil {
		log.Fatalf("migrate system settings database: %v", err)
	}
	if err := store.MigrateOrganizations(ctx); err != nil {
		log.Fatalf("migrate organization database: %v", err)
	}
	if err := store.MigrateAuth(ctx); err != nil {
		log.Fatalf("migrate authentication database: %v", err)
	}
	if err := store.MigrateCommercial(ctx); err != nil {
		log.Fatalf("migrate commercial database: %v", err)
	}
	if err := store.MigrateOperatingFinance(ctx); err != nil {
		log.Fatalf("migrate operating finance database: %v", err)
	}
	if err := store.MigrateSales(ctx); err != nil {
		log.Fatalf("migrate sales database: %v", err)
	}

	if cfg.Env == "development" {
		if _, err := store.EnsureDevelopmentSeed(
			ctx,
			cfg.DevTenantCode,
			cfg.DevTenantName,
		); err != nil {
			log.Fatalf("seed development tenant: %v", err)
		}
		if err := store.EnsureDefaultTimeCards(ctx); err != nil {
			log.Fatalf("seed default time cards: %v", err)
		}
	}

	defaultAdminHash, err := auth.HashPassword("12345678")
	if err != nil {
		log.Fatalf("hash default admin password: %v", err)
	}
	if err := store.EnsureDefaultAdmin(
		ctx,
		"Admin",
		"超级系统管理员",
		defaultAdminHash,
	); err != nil {
		log.Fatalf("seed default admin: %v", err)
	}
	if err := store.MigrateStaff(ctx); err != nil {
		log.Fatalf("migrate staff database: %v", err)
	}

	if err := store.MigrateInvitations(ctx); err != nil {
		log.Fatalf("migrate invitations database: %v", err)
	}
	if err := store.MigrateResources(ctx); err != nil {
		log.Fatalf("migrate organization resource database: %v", err)
	}
	if err := store.MigrateInventory(ctx); err != nil {
		log.Fatalf("migrate inventory database: %v", err)
	}
	if err := store.MigrateLiveRuntime(ctx); err != nil {
		log.Fatalf("migrate live runtime database: %v", err)
	}
	if err := store.MigrateSpeechAnalysis(ctx); err != nil {
		log.Fatalf("migrate speech analysis database: %v", err)
	}
	if err := store.EnsureDefaultSpeechAnalysisProfile(ctx); err != nil {
		log.Fatalf("ensure default speech analysis profile: %v", err)
	}
	if err := store.EnsureDefaultWarehouse(ctx); err != nil {
		log.Fatalf("ensure default warehouse: %v", err)
	}
	_ = store.DeleteExpiredSessions(ctx, time.Now().UTC())

	auditStore := audit.New(
		net.JoinHostPort(cfg.RedisHost, cfg.RedisPort),
		cfg.RedisPassword,
		cfg.RedisDB,
		cfg.AuditLogLimit,
		store,
	)
	if err := auditStore.Ping(ctx); err != nil {
		log.Printf(
			"management audit redis mirror unavailable; MySQL ledger remains authoritative: %v",
			err,
		)
	}
	defer auditStore.Close()

	leaderLease, err := coordination.NewLeaderLease(
		cfg.RedisHost,
		cfg.RedisPort,
		cfg.RedisPassword,
		cfg.RedisDB,
		"livecompanion:cluster:leader:live-runtime-reconciler",
		cfg.NodeID,
		time.Duration(cfg.ReconcileLeaderTTLSeconds)*time.Second,
	)
	if err != nil {
		log.Fatalf("create management leader lease: %v", err)
	}
	defer leaderLease.Close()
	leaderLease.Start(appCtx)

	authResolver := auth.NewResolver(cfg.Env, store)
	core := coreclient.New(cfg.CoreBaseURL, cfg.CoreToken)
	mailerClient := mailer.New(mailer.Config{
		Host:      cfg.SMTPHost,
		Port:      cfg.SMTPPort,
		Username:  cfg.SMTPUsername,
		Password:  cfg.SMTPPassword,
		FromEmail: cfg.SMTPFromEmail,
		FromName:  cfg.SMTPFromName,
		TLSMode:   cfg.SMTPTLSMode,
	})
	assetStorage, err := assetstorage.NewRegistry(assetstorage.Config{
		Driver:             cfg.StorageDriver,
		LocalRoot:          cfg.MediaRoot,
		OSSEndpoint:        cfg.OSSEndpoint,
		OSSPublicEndpoint:  cfg.OSSPublicEndpoint,
		OSSBucket:          cfg.OSSBucket,
		OSSAccessKeyID:     cfg.OSSAccessKeyID,
		OSSAccessKeySecret: cfg.OSSAccessKeySecret,
	})
	if err != nil {
		log.Fatalf("initialize media storage: %v", err)
	}
	if err := store.MigrateWorkInbox(ctx); err != nil {
		log.Fatalf("migrate work inbox: %v", err)
	}
	inboxRedis := redis.NewClient(&redis.Options{Addr: net.JoinHostPort(cfg.RedisHost, cfg.RedisPort), Password: cfg.RedisPassword, DB: cfg.RedisDB, DialTimeout: 500 * time.Millisecond, ReadTimeout: 500 * time.Millisecond, WriteTimeout: 500 * time.Millisecond, PoolTimeout: 500 * time.Millisecond, ContextTimeoutEnabled: true, MaxRetries: -1})
	defer inboxRedis.Close()
	inbox := workinbox.New(store, inboxRedis, net.JoinHostPort(cfg.DBHost, cfg.DBPort)+"/"+cfg.DBName)
	go inbox.Run(appCtx)
	api := httpapi.New(
		store,
		authResolver,
		core,
		auditStore,
		mailerClient,
		cfg.Env,
		cfg.AvatarDir,
		cfg.PublicWebURL,
		assetStorage,
		time.Duration(cfg.OSSURLExpirySeconds)*time.Second,
		leaderLease,
	)
	api.SetWorkInbox(inbox)
	go api.RunSpeechAnalysisAudioCleanup(appCtx)
	go api.RunCoreStatusWatch(appCtx)
	runtimeReconciler := liveruntime.NewReconciler(
		store,
		core,
		cfg.RuntimeReconcileWorkers,
		cfg.RuntimeReconcileBatch,
		leaderLease,
	)
	runtimeReconciler.SetAuditRecorder(auditStore)
	go runtimeReconciler.Run(appCtx)
	agentGateway := agentgateway.NewFromEnv()
	clusterWorker := questioncluster.New(store, core, agentGateway, leaderLease)
	go clusterWorker.Run(appCtx)
	decisionWorker := decisionexecutor.New(store, core, agentGateway, ttsgateway.NewFromEnv(), leaderLease)
	api.SetSpeechMissionReader(decisionWorker)
	go decisionWorker.Run(appCtx)

	// Pending device orders hold concrete inventory immediately. Only the
	// cluster leader releases expired holds so multiple management nodes never
	// race the same order. The payment path also checks expiry synchronously.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		releaseExpired := func() {
			if !leaderLease.IsLeader() {
				return
			}
			ctx, cancel := context.WithTimeout(appCtx, 20*time.Second)
			defer cancel()
			count, err := store.ReleaseExpiredDeviceOrderHolds(ctx)
			if err != nil {
				log.Printf("release expired device order holds: %v", err)
				return
			}
			if count > 0 {
				log.Printf("released %d expired device order holds", count)
			}
		}

		releaseExpired()
		for {
			select {
			case <-appCtx.Done():
				return
			case <-ticker.C:
				releaseExpired()
			}
		}
	}()

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf(
		"management-service listening on %s node=%s",
		cfg.Addr,
		cfg.NodeID,
	)
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()

	select {
	case <-appCtx.Done():
		log.Printf("management-service shutdown requested")
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			log.Printf("management-service stopped unexpectedly: %v", err)
		}
	}
	stop()

	shutdownCtx, shutdownCancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("management-service graceful shutdown: %v", err)
	}
}
