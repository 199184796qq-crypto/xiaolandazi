package main

import (
	"context"
	"fmt"
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
	"livecompanion/management/internal/semantic"
	assetstorage "livecompanion/management/internal/storage"
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/wechatpay"
	"livecompanion/management/internal/workinbox"
)

func main() {
	cfg := config.Load()
	stylePluginRegistry, loadedStylePlugins, stylePluginRoot, err := loadStylePluginRegistry(cfg.AnchorStylePluginDir)
	if err != nil {
		log.Fatalf("load anchor-style plugins: %v", err)
	}
	log.Printf("loaded %d anchor-style plugins from %s", len(loadedStylePlugins), stylePluginRoot)
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
	if err := store.MigrateRoomDeletions(ctx); err != nil {
		log.Fatalf("migrate room deletions: %v", err)
	}
	if err := store.MigrateAdminAudit(ctx); err != nil {
		log.Fatalf("migrate audit database: %v", err)
	}
	if err := store.MigrateSystemSettings(ctx); err != nil {
		log.Fatalf("migrate system settings database: %v", err)
	}
	if err := store.MigrateSpeechModels(ctx); err != nil {
		log.Fatalf("migrate speech model settings: %v", err)
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
	if err := store.MigrateWechatPay(ctx); err != nil {
		log.Fatalf("migrate wechat payment database: %v", err)
	}
	if err := store.MigrateWechatRefunds(ctx); err != nil {
		log.Fatalf("migrate wechat refund database: %v", err)
	}
	if err := store.MigrateBeanEconomy(ctx); err != nil {
		log.Fatalf("migrate bean economy database: %v", err)
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
	if err := store.MigrateLiveContentRefresh(ctx); err != nil {
		log.Fatalf("migrate live content refresh: %v", err)
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

	executionLeaderLease, err := coordination.NewLeaderLease(
		cfg.RedisHost,
		cfg.RedisPort,
		cfg.RedisPassword,
		cfg.RedisDB,
		fmt.Sprintf("livecompanion:cluster:leader:live-runtime-executor:%s", cfg.RuntimeExecutionRealm),
		cfg.NodeID,
		time.Duration(cfg.ReconcileLeaderTTLSeconds)*time.Second,
	)
	if err != nil {
		log.Fatalf("create runtime execution leader lease: %v", err)
	}
	defer executionLeaderLease.Close()
	executionLeaderLease.Start(appCtx)

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
	api.SetStylePluginRegistry(stylePluginRegistry)
	wechatService, err := wechatpay.New(ctx, cfg.WechatPay)
	if err != nil {
		log.Fatalf("initialize wechat pay: %v", err)
	}
	api.SetWechatPay(wechatService)
	go api.RunWechatPaymentReconciliation(appCtx)
	go api.RunWechatRefundReconciliation(appCtx)
	api.SetXiaozhiInternalToken(cfg.XiaozhiInternalToken)
	api.SetExecutionRealm(cfg.RuntimeExecutionRealm)
	go api.RunSpeechAnalysisAudioCleanup(appCtx)
	go api.RunCoreStatusWatch(appCtx)
	go api.RunRoomDeletionCleanup(appCtx)
	go api.RunLiveContentRefresh(appCtx)
	runtimeReconciler := liveruntime.NewReconciler(
		store,
		core,
		cfg.RuntimeReconcileWorkers,
		cfg.RuntimeReconcileBatch,
		executionLeaderLease,
	)
	runtimeReconciler.SetExecutionRealm(cfg.RuntimeExecutionRealm)
	runtimeReconciler.SetAuditRecorder(auditStore)
	go runtimeReconciler.Run(appCtx)
	agentGateway := agentgateway.NewFromEnv().WithSpeechModels(store)
	semanticEmbedder := semantic.NewFromEnv()
	semanticService := semantic.NewService(semanticEmbedder, store)
	api.SetSemanticMetricsReader(semanticService)
	if semanticEmbedder.Enabled() {
		log.Printf("semantic embedding enabled model=%s", semanticEmbedder.Model())
	} else {
		log.Printf("semantic embedding disabled")
	}
	clusterWorker := questioncluster.New(store, core, agentGateway, executionLeaderLease)
	clusterWorker.SetExecutionRealm(cfg.RuntimeExecutionRealm)
	clusterWorker.SetSemanticEmbedder(semanticEmbedder)
	clusterWorker.SetSemanticService(semanticService)
	go clusterWorker.Run(appCtx)
	decisionWorker := decisionexecutor.New(store, core, agentGateway, ttsgateway.NewFromEnv(), executionLeaderLease)
	decisionWorker.SetExecutionRealm(cfg.RuntimeExecutionRealm)
	decisionWorker.SetSemanticEmbedder(semanticEmbedder)
	decisionWorker.SetSemanticService(semanticService)
	api.SetSpeechMissionReader(decisionWorker)
	go decisionWorker.Run(appCtx)

	// recent_speech vectors carry a short TTL. Physically purge expired rows so
	// the semantic table does not grow forever; only the cluster leader performs
	// the cleanup to avoid duplicate maintenance work across nodes.
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		cleanupExpiredSemantic := func() {
			if !leaderLease.IsLeader() {
				return
			}
			cleanupCtx, cancel := context.WithTimeout(appCtx, 10*time.Second)
			defer cancel()
			count, err := store.DeleteExpiredSemanticDocuments(cleanupCtx, time.Now().UTC())
			if err != nil {
				log.Printf("delete expired semantic documents: %v", err)
				return
			}
			if count > 0 {
				log.Printf("deleted %d expired semantic documents", count)
			}
		}
		cleanupExpiredSemantic()
		for {
			select {
			case <-appCtx.Done():
				return
			case <-ticker.C:
				cleanupExpiredSemantic()
			}
		}
	}()

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

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-appCtx.Done():
				return
			case <-ticker.C:
				if !leaderLease.IsLeader() {
					continue
				}
				runCtx, cancel := context.WithTimeout(appCtx, 20*time.Second)
				if err := store.ReleaseCommerceEarnings(runCtx); err != nil {
					log.Printf("release commerce earnings: %v", err)
				}
				cancel()
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
