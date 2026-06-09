// @title           重明 (Mutong) API
// @version         1.0
// @description     面向 Kubernetes 集群的资源可视化与智能运维（AIOps）平台
// @host            localhost:8888
// @BasePath        /
// @securityDefinitions.apikey ApiKeyAuth
// @in              header
// @name            Authorization

// cmd/myapp/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gitee.com/tddh/mutong/config"
	"gitee.com/tddh/mutong/controllers"
	oauth2ctrl "gitee.com/tddh/mutong/controllers/oauth2"
	"gitee.com/tddh/mutong/interfaces"
	alert_interfaces "gitee.com/tddh/mutong/interfaces/alert"
	alert_models "gitee.com/tddh/mutong/models/alert"
	oauth2model "gitee.com/tddh/mutong/models/oauth2"
	"gitee.com/tddh/mutong/services"
	alert "gitee.com/tddh/mutong/services/alert"
	authsvc "gitee.com/tddh/mutong/services/auth"
	diagnosis_svc "gitee.com/tddh/mutong/services/diagnosis"
	execService "gitee.com/tddh/mutong/services/executor"
	logsearch "gitee.com/tddh/mutong/services/logsearch"
	prometheus_svc "gitee.com/tddh/mutong/services/prometheus"
	"gitee.com/tddh/mutong/services/prompt"
	retrospective_svc "gitee.com/tddh/mutong/services/retrospective"
	"gitee.com/tddh/mutong/services/search"
	"gitee.com/tddh/mutong/services/trace"
	_ "github.com/apache/skywalking-go"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/google/gops/agent"
	"github.com/google/uuid"
	"github.com/grafana/pyroscope-go"
	"github.com/ory/fosite"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"
	kubernetes "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var rootCmd = &cobra.Command{
	Use:   "mutong.ink",
	Short: "mutong",
	Run:   runApp,
}

var (
	configPath      string
	logLevel        string
	scopeServerAddr string
)

func init() {
	rootCmd.Flags().StringVarP(&configPath, "config", "c", "configs/", "config directory path")
	rootCmd.Flags().StringVarP(&logLevel, "log-level", "", "info", "error info warning debug ")
	rootCmd.Flags().StringVarP(&scopeServerAddr, "scope-server-addr", "", "http://localhost", "scope server address")
}

func runApp(cmd *cobra.Command, args []string) {
	maxProcs := runtime.NumCPU()
	if envProcs := os.Getenv("MUTONG_MAX_PROCS"); envProcs != "" {
		if parsed, err := strconv.Atoi(envProcs); err == nil && parsed > 0 {
			maxProcs = parsed
		}
	}
	runtime.GOMAXPROCS(maxProcs)

	cfg := initializeConfig(configPath)
	logger := cfg.Logger
	pyroscopeServer(logger)

	// 确保资源在退出时关闭
	defer func() {
		if err := cfg.Close(); err != nil {
			logger.Error("Error closing resources", zap.Error(err))
		}
		logger.Info("All resources closed successfully")
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Signal handler goroutine panic recovered", zap.Any("panic", r))
			}
		}()
		sig := <-sigCh
		logger.Info("Received signal, initiating graceful shutdown...", zap.Any("signal", sig))
		cancel()
	}()

	oauth2Provider, rdb := initOAuth2(cfg)

	// Create session manager for middleware and OIDC flows
	var sessions *authsvc.SessionManager
	if sessionSecret := os.Getenv("MUTONG_SESSION_SECRET"); sessionSecret != "" {
		sessions = authsvc.NewSessionManager([]byte(sessionSecret), 24*time.Hour)
	} else if cfg.Auth.GlobalSecret != "" {
		sessions = authsvc.NewSessionManager([]byte(cfg.Auth.GlobalSecret), 24*time.Hour)
	}

	// Create OIDC client for access token validation and route registration
	var oidcClient *authsvc.ZitadelOIDCClient
	if cfg.Auth.Mode == "hybrid" || cfg.Auth.Mode == "zitadel" {
		c, err := authsvc.NewZitadelOIDCClient(context.Background(), cfg.Auth.Zitadel)
		if err != nil {
			logger.Warn("Failed to initialize Zitadel OIDC client", zap.Error(err))
		} else {
			oidcClient = c
		}
	}

	roleSvc, userSvc, k8sresourceSvc, alertProcessor, graphDB, labelSyncer, traceSyncer := initializeServices(cfg)
	ctrls := initializeControllers(roleSvc, userSvc, k8sresourceSvc)
	ctrls.InitCollectWithContext(ctx)

	var inspectionProcessor interfaces.InspectionProcessor
	var err error
	inspectionProcessor, err = cfg.InitInspectionService(cfg.GetLogger(), cfg.GetGraphDB())
	if err != nil {
		logger.Warn("Failed to initialize inspection service", zap.Error(err))
	} else if inspectionProcessor != nil {
		if err := inspectionProcessor.Start(); err != nil {
			logger.Warn("Failed to start inspection service", zap.Error(err))
		}
	}

	defer func() {
		if inspectionProcessor != nil {
			inspectionProcessor.Stop()
		}
	}()
	engine, k8sExec, diagEngine, chatManager := initializeGin(ctrls, userSvc, cfg.Logger, alertProcessor, inspectionProcessor, graphDB, k8sresourceSvc, cfg, oauth2Provider, rdb, sessions, oidcClient)

	addr := os.Getenv("MUTONG_ADDR")
	if addr == "" {
		addr = cfg.Server.Address
	}
	port := os.Getenv("MUTONG_PORT")
	if port == "" {
		port = fmt.Sprintf("%d", cfg.Server.Port)
	}

	srv := &http.Server{
		Addr:         addr + ":" + port,
		Handler:      engine,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeoutSec) * time.Second,
		IdleTimeout:  time.Duration(cfg.Server.IdleTimeoutSec) * time.Second,
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("HTTP server goroutine panic recovered", zap.Any("panic", r))
			}
		}()
		logger.Info("HTTP server starting", zap.String("port", port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Server error", zap.Error(err))
		}
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Shutdown handler goroutine panic recovered", zap.Any("panic", r))
			}
		}()
		<-ctx.Done()
		logger.Info("Context cancelled, shutting down server...")
		k8sresourceSvc.Stop()
		if k8sExec != nil {
			k8sExec.Stop()
		}
		if diagEngine != nil {
			diagEngine.Stop()
		}
		if chatManager != nil {
			chatManager.Stop()
		}
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Duration(cfg.Server.ShutdownTimeoutSec)*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("Server forced to shutdown", zap.Error(err))
		}
	}()

	if cfg.BusinessTopology.Enabled && labelSyncer != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("BusinessLabelSyncer goroutine panic recovered", zap.Any("panic", r))
				}
			}()
			labelSyncer.Start()
		}()
		defer labelSyncer.Stop()
	}

	if cfg.NebulaCleanup.Enabled {
		retentionDays := cfg.NebulaCleanup.RetentionDays
		if retentionDays <= 0 {
			retentionDays = 7
		}
		cleanupInterval := cfg.NebulaCleanup.CleanupInterval
		if cleanupInterval <= 0 {
			cleanupInterval = 24
		}
		batchSize := cfg.NebulaCleanup.BatchSize
		if batchSize <= 0 {
			batchSize = 1000
		}
		k8sresourceSvc.StartPeriodicCleanup(time.Duration(cleanupInterval)*time.Hour, retentionDays, batchSize)
	}

	if traceSyncer != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("TraceTopologySyncer goroutine panic recovered", zap.Any("panic", r))
				}
			}()
			traceSyncer.Start()
		}()
		defer traceSyncer.Stop()
	}

	<-ctx.Done()
}

func pyroscopeServer(logger *zap.Logger) {
	if scopeServerAddr == "" {
		return
	}

	_, err := pyroscope.Start(pyroscope.Config{
		ApplicationName: "mutong.ink",
		ServerAddress:   scopeServerAddr,
	})
	if err != nil {
		panic(err)
	}

	if err := agent.Listen(agent.Options{ShutdownCleanup: true}); err != nil {
		logger.Error("Failed to start gops agent", zap.Error(err))
	}
}

func initializeConfig(path string) *config.Config {
	return config.NewConfig(path)
}

func initializeServices(cfg *config.Config) (interfaces.RoleInterface, interfaces.UserInterface, *services.K8sResoureService, alert_interfaces.AlertProcessor, interfaces.GraphDB, *services.BusinessLabelSyncer, *services.TraceTopologySyncer) {
	roleSvc := services.NewRoleService()
	userSvc := services.NewUserService(cfg.DB)

	k8sresourceSvc := services.NewK8sResourceService(
		cfg.GetLogger(),
		cfg.GetGraphDB(),
		cfg.GetCache(),
		cfg.Cluster,
		cfg.GetMessageQueue(),
		cfg.Kafka.Topic,
		roleSvc,
		userSvc,
		cfg.Kafka.MaxRetries,
		cfg.Kafka.RetryBackoffMs,
		&cfg.Cache,
		cfg,
		cfg.Kafka.BusinessWorkloadClient,
		cfg.Kafka.BusinessWorkloadTopic,
		cfg.Kafka.WorkloadKinds,
	)

	// 初始化告警服务
	bizTopoSvc := services.NewBusinessTopologyService(cfg.GetLogger(), cfg.GetGraphDB())
	alertProcessor, err := cfg.InitAlertService(bizTopoSvc)
	if err != nil {
		cfg.Logger.Warn("Failed to initialize alert service", zap.Error(err))
		// 告警服务初始化失败不阻止主程序启动
		alertProcessor = nil
	}

	var labelSyncer *services.BusinessLabelSyncer
	if cfg.BusinessTopology.Enabled {
		nsMapper := services.NewNamespaceMapper(cfg.BusinessTopology)
		businessWorkloadMQ := cfg.GetBusinessWorkloadMessageQueue()

		labelSyncer = services.NewBusinessLabelSyncer(
			cfg.GetLogger(),
			cfg.GetGraphDB(),
			nsMapper,
			cfg.GetCache(),
			cfg.Cluster,
			businessWorkloadMQ,
			cfg.BusinessTopology.AppNameNormalization,
		)
	}

	var traceSyncer *services.TraceTopologySyncer
	if cfg.BusinessTopology.Enabled {
		traceMQ := cfg.GetTraceMessageQueue()
		if traceMQ == nil {
			cfg.Logger.Warn("Trace Kafka client not configured, TraceTopologySyncer disabled")
		} else {
			clusterName := ""
			if cfg.Cluster != nil && len(cfg.Cluster.K8sClusterClient) > 0 {
				for name := range cfg.Cluster.K8sClusterClient {
					clusterName = name
					break
				}
			}
			var err error
			traceSyncer, err = services.NewTraceTopologySyncer(
				cfg.GetLogger(),
				cfg.GetGraphDB(),
				traceMQ,
				clusterName,
				cfg.BusinessTopology.KnownServices,
			)
			if err != nil {
				cfg.Logger.Warn("Failed to initialize TraceTopologySyncer", zap.Error(err))
			}
		}
	}

	// BLS 与 TraceTopologySyncer 共享 relationCache 做 BusinessApp 去重
	if labelSyncer != nil && traceSyncer != nil {
		labelSyncer.SetRelationCache(traceSyncer.RelationCache())
	}

	return roleSvc, userSvc, k8sresourceSvc.(*services.K8sResoureService), alertProcessor, cfg.GetGraphDB(), labelSyncer, traceSyncer
}

func initializeControllers(roleSvc interfaces.RoleInterface, userSvc interfaces.UserInterface, k8sresourceSvc interfaces.K8sResourceInterface) *controllers.Controllers {
	return controllers.NewControllers(roleSvc, userSvc, k8sresourceSvc)
}

func TraceIDMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if ctx.GetHeader("X-Trace-ID") == "" {
			traceID := uuid.NewString()
			ctx.Request.Header.Set("X-Trace-ID", traceID)
			ctx.Header("X-Trace-ID", traceID)
		}
		ctx.Next()
	}
}

func initOAuth2(cfg *config.Config) (fosite.OAuth2Provider, *redis.Client) {
	if cfg.DB == nil {
		return nil, nil
	}
	var rdb *redis.Client
	if redisAddr := os.Getenv("MUTONG_REDIS_ADDR"); redisAddr != "" {
		rdb = redis.NewClient(&redis.Options{Addr: redisAddr})
	} else if cfg.Diagnosis.Session.Redis != "" {
		rdb = redis.NewClient(&redis.Options{
			Addr:     cfg.Diagnosis.Session.Redis,
			Password: cfg.Diagnosis.Session.Password,
			DB:       cfg.Diagnosis.Session.DB,
		})
	}
	oauth2Cfg := &authsvc.OAuth2Config{
		IssuerURL:            os.Getenv("MUTONG_ISSUER_URL"),
		AccessTokenLifespan:  1 * time.Hour,
		RefreshTokenLifespan: 168 * time.Hour,
		AuthCodeLifespan:     10 * time.Minute,
	}
	if oauth2Cfg.IssuerURL == "" {
		oauth2Cfg.IssuerURL = "http://localhost:8888"
	}
	return authsvc.NewOAuth2Provider(cfg.DB, oauth2Cfg), rdb
}

func allowedOrigins() []string {
	origins := os.Getenv("MUTONG_ALLOWED_ORIGINS")
	if origins == "" {
		return []string{"*"}
	}
	return strings.Split(origins, ",")
}

func initializeGin(ctrls *controllers.Controllers, userSvc interfaces.UserInterface, logger *zap.Logger, alertProcessor alert_interfaces.AlertProcessor, inspectionProcessor interfaces.InspectionProcessor, db interfaces.GraphDB, k8sSvc *services.K8sResoureService, cfg *config.Config, oauth2Provider fosite.OAuth2Provider, rdb *redis.Client, sessions *authsvc.SessionManager, oidcClient *authsvc.ZitadelOIDCClient) (*gin.Engine, *execService.K8sExecutor, *diagnosis_svc.Engine, *diagnosis_svc.ChatSessionManager) {
	var execCtrl *controllers.ExecutorController
	var k8sExec *execService.K8sExecutor
	var diagEngine *diagnosis_svc.Engine
	var chatManager *diagnosis_svc.ChatSessionManager

	engine := gin.New()

	// Request logging
	engine.Use(gin.Logger())
	engine.Use(gin.Recovery())

	// Global middleware
	engine.Use(TraceIDMiddleware())
	engine.Use(cors.New(cors.Config{
		AllowOrigins:     allowedOrigins(),
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type", "Accept", "Authorization", "X-Trace-ID", "X-Role", "X-Admin-Key"},
		AllowCredentials: true,
	}))
	engine.Use(controllers.RateLimitMiddleware(cfg.Server.RateLimitPerSec, time.Second))
	engine.Use(controllers.BearerTokenMiddleware(cfg.DB, sessions, func() authsvc.AccessTokenValidator {
		if oidcClient == nil {
			return nil
		}
		return oidcClient
	}(), oauth2Provider))
	engine.Use(controllers.RequireAuthMiddleware())

	// Redirect legacy topology URL before static files take over
	engine.Use(func(ctx *gin.Context) {
		if ctx.Request.URL.Path == "/view/topology/resource_graph.html" {
			ctx.Redirect(http.StatusMovedPermanently, "/view/topology")
			ctx.Abort()
			return
		}
		ctx.Next()
	})

	// Serve frontend static files with auth guard for browser access
	viewDirs := []string{"./artifacts/view", "./view/dist", "./view"}
	viewServed := false
	for _, dir := range viewDirs {
		if _, err := os.Stat(dir); err == nil {
			fs := http.FileServer(http.Dir(dir))
			handler := http.StripPrefix("/view", fs)
			engine.GET("/view/*filepath", func(c *gin.Context) {
				path := c.Request.URL.Path
				// Allow public pages and static assets through without auth
				if path == "/view/login.html" || path == "/view/consent.html" ||
					strings.HasSuffix(path, ".css") || strings.HasSuffix(path, ".js") ||
					strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".svg") ||
					strings.HasSuffix(path, ".ico") || strings.HasSuffix(path, ".woff2") ||
					strings.HasPrefix(path, "/view/src/") {
					handler.ServeHTTP(c.Writer, c.Request)
					return
				}
				hasAuth := c.GetHeader("Authorization") != ""
				_, hasCookieErr := c.Cookie("mutong_session")
				_, hasUser := c.Get("user_id")
				if !hasAuth && hasCookieErr != nil && !hasUser {
					c.Redirect(http.StatusFound, "/view/login.html")
					c.Abort()
					return
				}
				handler.ServeHTTP(c.Writer, c.Request)
			})
			viewServed = true
			logger.Info("Serving frontend from", zap.String("dir", dir))
			break
		}
	}
	if !viewServed {
		logger.Warn("No frontend build found, serving will fail")
	}

	engine.GET("/", func(ctx *gin.Context) {
		ctx.Redirect(http.StatusMovedPermanently, "/view/index.html")
	})

	// Legacy path compatibility — redirect to new auth routes
	engine.GET("/login", func(ctx *gin.Context) {
		ctx.Redirect(http.StatusMovedPermanently, "/view/login.html")
	})
	engine.GET("/oidc/login", func(ctx *gin.Context) {
		ctx.Redirect(http.StatusMovedPermanently, "/api/auth/oidc/login")
	})
	engine.POST("/oidc/login", func(ctx *gin.Context) {
		ctx.Redirect(http.StatusMovedPermanently, "/api/auth/oidc/login")
	})

	// Swagger UI
	engine.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Register OAuth 2.0 endpoints (public)
	deviceH := &oauth2ctrl.DeviceHandler{DB: cfg.DB}
	oauth2ctrl.RegisterOAuth2Routes(engine, oauth2Provider, os.Getenv("MUTONG_ISSUER_URL"), deviceH)
	deviceH.RegisterDeviceRoutes(engine)

	var loginLimit *authsvc.LoginLimiter
	if rdb != nil {
		loginLimit = authsvc.NewLoginLimiter(rdb)
	}

	// Register auth endpoints (login, me, logout)
	authCtrl := controllers.NewAuthController(cfg.DB, sessions, loginLimit)
	authCtrl.OIDCAvailable = oidcClient != nil
	authCtrl.AuthMode = cfg.Auth.Mode
	authCtrl.RegisterRoutes(engine)

	if oidcClient != nil {
		oidcCtrl := controllers.NewOIDCController(userSvc, oidcClient, sessions)
		oidcCtrl.RegisterRoutes(engine)
	} else if cfg.Auth.Mode == "hybrid" || cfg.Auth.Mode == "zitadel" {
		oidcCtrl := controllers.NewOIDCController(userSvc, nil, sessions)
		oidcCtrl.RegisterRoutes(engine)
	}

	// Seed OAuth2 clients and initial admin
	oauth2model.SeedClients(cfg.DB)
	initPass := os.Getenv("MUTONG_INIT_ADMIN_PASSWORD")
	if initPass == "" {
		logger.Warn("MUTONG_INIT_ADMIN_PASSWORD not set, admin account will NOT be created")
	} else {
		if err := authCtrl.CreateInitialAdmin("admin", initPass); err != nil {
			logger.Error("Failed to create initial admin", zap.Error(err))
		}
	}

	// Initialize Casbin RBAC
	if cfg.DB != nil {
		if err := authsvc.InitCasbin(cfg.DB); err != nil {
			logger.Warn("Failed to initialize Casbin RBAC", zap.Error(err))
		}
	}

	ctrls.RegisterRoutes(engine)

	// Executor controller wiring (if enabled and available K8s client)
	if cfg != nil && cfg.GetExecutorConf().Enabled {
		// try to grab first k8s client
		var k8sClient *kubernetes.Clientset
		if cfg.Cluster != nil {
			for _, cluster := range cfg.Cluster.K8sClusterClient {
				k8sClient = cluster.RootKubeClientSet
				break
			}
		}
		if k8sClient != nil {
			k8sExec = execService.NewK8sExecutor(logger, k8sClient, cfg)
			execCtrl = controllers.NewExecutorController(logger, k8sExec, cfg, nil)
			execCtrl.RegisterRoutes(engine)
		}
	}

	// Initialize Elasticsearch service once — reuse for both log controller and diagnosis engine
	esQuerySvc := initializeElasticsearch(cfg, logger)

	if esQuerySvc != nil {
		logCtrl := controllers.NewLogController(logger, esQuerySvc.AsLogQuerier())
		logCtrl.RegisterRoutes(engine)
	}

	// Prometheus metrics controller
	promSvcGlobal := initializePrometheus(cfg, logger)
	if promSvcGlobal != nil {
		metricsCtrl := controllers.NewMetricsController(logger, promSvcGlobal.AsMetricsQuerier())
		metricsCtrl.RegisterRoutes(engine)
		monitoringCtrl := controllers.NewMonitoringController(logger, promSvcGlobal.AsMetricsQuerier())
		monitoringCtrl.RegisterRoutes(engine)
		logger.Info("Prometheus metrics API configured")
	} else {
		metricsCtrl := controllers.NewMetricsController(logger, nil)
		metricsCtrl.RegisterRoutes(engine)
		monitoringCtrl := controllers.NewMonitoringController(logger, nil)
		monitoringCtrl.RegisterRoutes(engine)
		logger.Info("Prometheus not configured, metrics API returns 503")
	}

	// OpenTelemetry tracing controller
	if cfg.OpenTelemetry.Enabled && cfg.OpenTelemetry.CollectorURL != "" {
		otelSvc := trace.NewOTelQueryService(cfg.OpenTelemetry.CollectorURL)
		traceCtrl := controllers.NewTraceController(otelSvc)
		traceCtrl.RegisterRoutes(engine)
		logger.Info("OpenTelemetry tracing API configured", zap.String("collector_url", cfg.OpenTelemetry.CollectorURL))
	} else {
		logger.Info("OpenTelemetry not configured, tracing API disabled")
	}

	if alertProcessor != nil {
		alertSources := make(map[string]*alert_models.AlertSource)
		for i := range cfg.AlertSources.Sources {
			alertSources[cfg.AlertSources.Sources[i].Name] = &cfg.AlertSources.Sources[i]
		}
		alertCtrl := controllers.NewAlertController(logger, alertProcessor, alertSources)
		alertCtrl.RegisterRoutes(engine)
	}

	if inspectionProcessor != nil {
		inspCtrl := controllers.NewInspectionController(logger, inspectionProcessor)
		inspCtrl.RegisterRoutes(engine)
	}

	statsCtrl := controllers.NewStatsController(logger, db, k8sSvc)
	statsCtrl.RegisterRoutes(engine)

	clusterCtrl := controllers.NewClusterController(logger, cfg.Cluster, db)
	clusterCtrl.RegisterRoutes(engine)

	// Terminal controller (requires K8s client)
	var k8sClientForTerminal *kubernetes.Clientset
	var restConfigForTerminal *rest.Config
	if cfg.Cluster != nil {
		for _, cluster := range cfg.Cluster.K8sClusterClient {
			k8sClientForTerminal = cluster.RootKubeClientSet
			restConfigForTerminal = cluster.RootRestConfig
			break
		}
	}
	terminalCtrl := controllers.NewTerminalController(logger, k8sClientForTerminal, restConfigForTerminal)
	terminalCtrl.RegisterRoutes(engine)

	if alertProcessor != nil {
		diagEngine := diagnosis_svc.NewEngine(logger, db, alertProcessor)

		llmProvider := cfg.GetLLMProvider()
		if llmProvider != nil {
			if einoP, ok := llmProvider.(*diagnosis_svc.EinoLLMProvider); ok {
				if mgr, err := prompt.NewManager("configs/prompts"); err == nil {
					einoP.SetPromptManager(mgr)
				}
			}
			diagEngine.WithLLMProvider(llmProvider)
			logger.Info("LLM provider configured", zap.String("provider", cfg.Diagnosis.LLM.Provider), zap.String("model", cfg.Diagnosis.LLM.Model))
		} else {
			logger.Info("LLM provider not configured, diagnosis will use rule-based path only")
		}

		if promSvcGlobal != nil {
			diagEngine.WithMetricsQuerier(promSvcGlobal.AsMetricsQuerier())
		}

		if esQuerySvc != nil {
			diagEngine.WithLogQuerier(esQuerySvc.AsLogQuerier())
		}

		if cfg.DB != nil {
			diagEngine.WithDB(cfg.DB)
		}

		diagEngine.WithMetrics(&diagnosis_svc.DiagnosisMetrics{})

		diagStatsInterval := 5 * time.Minute
		if cfg.Diagnosis.StatsSyncInterval > 0 {
			diagStatsInterval = time.Duration(cfg.Diagnosis.StatsSyncInterval) * time.Second
		}
		diagEngine.StartStatsSync(diagStatsInterval)
		logger.Info("Diagnosis stats persistence enabled", zap.Duration("sync_interval", diagStatsInterval))

		var redisStorage *diagnosis_svc.RedisStorage
		var pgStorage *diagnosis_svc.PostgresStorage

		if cfg.DB != nil {
			pgStorage = diagnosis_svc.NewPostgresStorage(cfg.DB, logger)
		}

		sessionConf := cfg.Diagnosis.Session
		if sessionConf.Enabled && sessionConf.Redis != "" {
			redisTTL := 30 * time.Minute
			if sessionConf.TTL > 0 {
				redisTTL = time.Duration(sessionConf.TTL) * time.Second
			}
			redisStorage = diagnosis_svc.NewRedisStorage(sessionConf.Redis, sessionConf.Password, sessionConf.DB, redisTTL, logger)
			logger.Info("Diagnosis session Redis enabled", zap.String("addr", sessionConf.Redis), zap.Int("db", sessionConf.DB))
		} else {
			logger.Info("Diagnosis session Redis not configured, using memory-only mode")
		}

		chatManager := diagnosis_svc.NewChatSessionManager(logger, llmProvider, diagEngine, redisStorage, pgStorage)

		cacheTTL := 15 * time.Minute
		if cfg.Diagnosis.CacheTTL > 0 {
			cacheTTL = time.Duration(cfg.Diagnosis.CacheTTL) * time.Second
		}

		var logQ interfaces.LogQuerier
		if esQuerySvc != nil {
			logQ = esQuerySvc.AsLogQuerier()
		}
		var k8sC *kubernetes.Clientset
		if cfg.Cluster != nil {
			for _, cluster := range cfg.Cluster.K8sClusterClient {
				k8sC = cluster.RootKubeClientSet
				break
			}
		}

		// 创建 ContextCollector（诊断数据预收集并行管道）
		diagCollector := diagnosis_svc.NewContextCollector(
			logger,
			alertProcessor,
			promSvcGlobal.AsMetricsQuerier(),
			logQ,
			k8sC,
			diagEngine.GetTopologyQuerier(),
			diagEngine.GetImpactAssessor(),
			diagEngine.GetKnowledgeBase(),
			diagEngine.GetHybridRetriever(),
			4,
		)
		diagEngine.WithCollector(diagCollector)
		logger.Info(
			"ContextCollector configured for diagnosis pipeline",
			zap.Int("max_concurrent", 4),
		)

		diagCtrl := controllers.NewDiagnosisController(logger, diagEngine, promSvcGlobal.AsMetricsQuerier(), chatManager, cacheTTL, k8sC, logQ, cfg.GetCache(), inspectionProcessor, k8sSvc.GetInformerFactory)

		if cfg.ExternalSearch.Enabled {
			sanitCfg := diagnosis_svc.SanitizerConfig{
				Enabled:                 cfg.Sanitizer.Enabled,
				HighPIIBlockThreshold:   cfg.Sanitizer.HighPIIBlockThreshold,
				BannedTerms:             cfg.Sanitizer.BannedTerms,
				MaxQueryLength:          cfg.Sanitizer.MaxQueryLength,
				PromptInjectionPatterns: cfg.Sanitizer.PromptInjectionPatterns,
			}
			for _, r := range cfg.Sanitizer.Rules {
				sanitCfg.Rules = append(sanitCfg.Rules, diagnosis_svc.SanitizerRule{
					Name:    r.Name,
					Pattern: r.Pattern,
					Enabled: r.Enabled,
				})
			}
			if len(sanitCfg.Rules) == 0 {
				sanitCfg = diagnosis_svc.DefaultSanitizerConfig()
			}

			sanitizer, serr := diagnosis_svc.NewSanitizer("main-app", sanitCfg)
			if serr != nil {
				logger.Warn("Failed to create sanitizer", zap.Error(serr))
			} else {
				var tavilyClient *search.TavilyClient
				if cfg.ExternalSearch.Tavily.APIKey != "" {
					tavilyClient = search.NewTavilyClient(cfg.ExternalSearch.Tavily)
					logger.Info("External knowledge base search enabled", zap.String("engine", "tavily"))
				}
				var githubClient *search.GitHubClient
				if cfg.ExternalSearch.GitHub.Token != "" {
					githubClient = search.NewGitHubClient(cfg.ExternalSearch.GitHub)
					logger.Info("GitHub Issues search enabled", zap.String("engine", "github"))
				}
				if tavilyClient != nil || githubClient != nil {
					diagCtrl.SetExternalSearch(tavilyClient, githubClient, sanitizer, nil)
				}
			}
		}

		diagCtrl.RegisterRoutes(engine)
		// Attach diagnosis engine to executor controller if available
		if execCtrl != nil {
			execCtrl.SetDiagnosisEngine(diagEngine)
		}

		// Wire auto-diagnosis pipeline if executor is enabled and we have an AlertService
		if aSvc, ok := alertProcessor.(*alert.AlertService); ok {
			if redisStorage != nil {
				aSvc.WithDiagnosisCacheInvalidator(redisStorage)
			}
			if cfg.DB != nil {
				aSvc.WithDB(cfg.DB)
			}

			alertStatsInterval := 5 * time.Minute
			if cfg.Alert.StatsSyncInterval > 0 {
				alertStatsInterval = time.Duration(cfg.Alert.StatsSyncInterval) * time.Second
			}
			aSvc.StartStatsSync(alertStatsInterval)
			logger.Info("Alert stats persistence enabled", zap.Duration("sync_interval", alertStatsInterval))

			var autoDiag *execService.AutoDiagnosisPipeline
			if k8sExec != nil {
				br := execService.NewRemediationBridge()
				autoDiag = execService.NewAutoDiagnosisPipeline(logger, diagEngine, br, k8sExec, cfg.GetExecutorConf().Enabled)
				logger.Info("Auto-diagnosis pipeline configured", zap.Bool("enabled", cfg.GetExecutorConf().Enabled))
			}
			if autoDiag != nil {
				aSvc.SetAutoDiagnosisPipeline(autoDiag)
			}
		}

		// Register status endpoint if we have a concrete AlertService
		if aSvc, ok := alertProcessor.(*alert.AlertService); ok && diagEngine != nil {
			statusCtrl := controllers.NewStatusController(logger, diagEngine, aSvc, db)
			statusCtrl.RegisterRoutes(engine)
		}

		retroSvc := retrospective_svc.NewService(logger, db, alertProcessor, cfg.DB, llmProvider, redisStorage)
		if diagEngine != nil {
			retroSvc.WithHybridRetriever(diagEngine.GetHybridRetriever())
		}
		// Wire retrospective generator to MCP server
		if diagCtrl != nil && retroSvc != nil {
			diagCtrl.SetRetrospectiveGenerator(func(ctx context.Context, fingerprint string) (string, error) {
				report, err := retroSvc.GeneratePostmortem(ctx, fingerprint)
				if err != nil {
					return "", err
				}
				summary := map[string]interface{}{
					"report_id":             report.ID,
					"incident_title":        report.IncidentTitle,
					"severity":              report.Severity,
					"duration":              report.Duration,
					"root_cause":            report.RootCause.Final,
					"resolution":            report.Resolution.Final,
					"lessons_learned_count": len(report.LessonsLearned),
					"action_items_count":    len(report.ActionItems),
					"generated_at":          report.GeneratedAt.Format(time.RFC3339),
				}
				b, _ := json.Marshal(summary)
				return string(b), nil
			})
		}
		retroCtrl := controllers.NewRetrospectiveController(logger, retroSvc)
		retroCtrl.RegisterRoutes(engine)
	}

	bizTopoSvc := services.NewBusinessTopologyService(logger, db)
	bizTopoCtrl := controllers.NewBusinessTopologyController(bizTopoSvc)
	bizTopoCtrl.RegisterRoutes(engine)

	return engine, k8sExec, diagEngine, chatManager
}

func initializePrometheus(cfg *config.Config, logger *zap.Logger) *prometheus_svc.QueryService {
	promCfg := prometheus_svc.PrometheusConfig{
		Enabled:       cfg.Prometheus.Enabled,
		URL:           cfg.Prometheus.URL,
		Timeout:       cfg.Prometheus.Timeout,
		MetricMapping: convertMetricMapping(cfg.ResourceProfiles),
		// Cache configuration is optional and loaded from config file
		CacheEnabled: cfg.Prometheus.Cache.Enabled,
		CacheTTL:     cfg.Prometheus.Cache.TTL,
		CacheMaxSize: cfg.Prometheus.Cache.MaxSize,
	}
	if !promCfg.Enabled || promCfg.URL == "" {
		logger.Info("Prometheus not configured, metrics queries disabled")
		return nil
	}
	return prometheus_svc.NewQueryService(logger, promCfg)
}

func initializeElasticsearch(cfg *config.Config, logger *zap.Logger) *logsearch.ESQueryService {
	esCfg := cfg.Elasticsearch
	if len(esCfg.Addresses) == 0 {
		logger.Info("Elasticsearch not configured, log queries disabled")
		return nil
	}
	svc, err := logsearch.NewESQueryService(logger, esCfg.Addresses, esCfg.IndexPattern, esCfg.ServiceToIndex, esCfg.Username, esCfg.Password, esCfg.Timeout)
	if err != nil {
		logger.Warn("Failed to initialize Elasticsearch client", zap.Error(err))
		return nil
	}
	return svc
}

func main() {
	_ = rootCmd.Execute()
}

func convertMetricMapping(resourceProfiles map[string]config.ResourceProfile) map[string][]prometheus_svc.MetricQueryConfig {
	mapping := make(map[string][]prometheus_svc.MetricQueryConfig)
	labelKeys := map[string]string{"Pod": "pod", "Node": "instance", "Deployment": "deployment", "Service": "service", "StatefulSet": "statefulset", "DaemonSet": "daemonset", "PVC": "persistentvolumeclaim", "Ingress": "ingress"}

	for kind, profile := range resourceProfiles {
		if len(profile.Metrics) == 0 {
			continue
		}
		lk := labelKeys[kind]
		if lk == "" {
			lk = "name"
		}
		var configs []prometheus_svc.MetricQueryConfig
		for _, m := range profile.Metrics {
			tmpl := strings.NewReplacer("{{namespace}}", "%[1]s", "{{name}}", "%[2]s").Replace(m.PromQL)
			configs = append(configs, prometheus_svc.MetricQueryConfig{
				ResourceKind:  kind,
				MetricName:    m.Name,
				QueryTemplate: tmpl,
				LabelKey:      lk,
				Unit:          m.Unit,
				Description:   m.Name,
				Category:      "custom",
				ForUI:         true,
				ForLLM:        true,
			})
		}
		mapping[kind] = configs
	}
	return mapping
}
