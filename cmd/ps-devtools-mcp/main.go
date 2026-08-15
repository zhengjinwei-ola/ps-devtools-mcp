package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/olaola-chat/ps-devtools-mcp/internal/appconfig"
	"github.com/olaola-chat/ps-devtools-mcp/internal/httptransport"
	"github.com/olaola-chat/ps-devtools-mcp/internal/mcpserver"
	"github.com/olaola-chat/ps-devtools-mcp/internal/redisinspect"
	"github.com/olaola-chat/ps-devtools-mcp/internal/slacknotify"
	"github.com/olaola-chat/ps-devtools-mcp/internal/testapi"
	"github.com/olaola-chat/ps-devtools-mcp/internal/testbot"
	"github.com/olaola-chat/ps-devtools-mcp/internal/testdb"
	"github.com/olaola-chat/ps-devtools-mcp/internal/testdeploy"
	"github.com/olaola-chat/ps-devtools-mcp/internal/testlogs"
	"github.com/olaola-chat/ps-devtools-mcp/internal/testredis"
	"github.com/olaola-chat/ps-devtools-mcp/internal/usersnapshot"
)

const (
	defaultQueryURL           = "http://127.0.0.1/gk/v1/external/testEnvQuery"
	defaultTimeout            = 12 * time.Second
	defaultMaxBodyMB          = 2
	shutdownTimeout           = 10 * time.Second
	deploymentWindow          = 15 * time.Minute
	deploymentShutdownTimeout = time.Minute
)

func main() {
	logger := log.New(os.Stderr, "ps-devtools-mcp ", log.LstdFlags|log.LUTC)
	config, err := appconfig.Parse(os.Args[1:], os.Getenv)
	if err != nil {
		logger.Fatal(err)
	}
	endpoint := os.Getenv("PS_TEST_QUERY_URL")
	if endpoint == "" {
		endpoint = defaultQueryURL
	}

	client, err := testdb.NewHTTPClient(endpoint, &http.Client{Timeout: defaultTimeout}, defaultMaxBodyMB<<20)
	if err != nil {
		logger.Fatal(err)
	}
	var dbClient interface {
		Query(context.Context, testdb.QueryInput) (testdb.QueryOutput, error)
	} = client
	if config.DirectDBEnabled() {
		directClient, err := testdb.OpenMySQLClient(context.Background(), testdb.MySQLConfig{
			Host: config.TestDBHost, Port: config.TestDBPort,
			User: config.TestDBUser, Password: config.TestDBPassword,
			XianshiDatabase: config.XianshiDatabase, ConfigDatabase: config.ConfigDatabase,
		})
		if err != nil {
			logger.Fatal(err)
		}
		defer directClient.Close()
		dbClient = directClient
		logger.Printf("test_database=direct host=%q port=%d user=%q", config.TestDBHost, config.TestDBPort, config.TestDBUser)
	}
	service := testdb.NewService(dbClient, logger)
	var redisClient testredis.Client = testredis.NewHTTPClient(client)
	if config.DirectRedisEnabled() {
		directRedis, err := testredis.OpenDirectClient(context.Background(), testredis.DirectConfig{
			Address: config.TestRedisAddress, Password: config.TestRedisPassword, Database: config.TestRedisDatabase,
		})
		if err != nil {
			logger.Fatal(err)
		}
		defer directRedis.Close()
		redisClient = directRedis
		logger.Printf("test_redis=direct address=%q database=%d", config.TestRedisAddress, config.TestRedisDatabase)
	}
	redisService := testredis.NewService(redisClient, logger)
	deployService := testdeploy.NewService(logger)
	if config.SlackDeployWebhookURL != "" {
		webhookClient := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		webhook, err := slacknotify.NewWebhook(config.SlackDeployWebhookURL, webhookClient)
		if err != nil {
			logger.Fatal(err)
		}
		deployService = testdeploy.NewServiceWithNotifier(webhook, logger)
	}
	deployManager := testdeploy.NewManager(deployService)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), deploymentShutdownTimeout)
		defer cancel()
		if err := deployManager.Close(ctx); err != nil {
			logger.Printf("deployment_manager_shutdown error=%q", err)
		}
	}()
	services := mcpserver.Services{
		DB: service, Redis: redisService,
		UserSnapshot:   usersnapshot.NewService(dbClient, logger),
		RedisInspector: redisinspect.NewService(redisService, logger),
		ReadOnlyAPI:    testapi.Unavailable{}, TestLogs: testlogs.Unavailable{},
		TestBot:    testbot.Unavailable{},
		TestDeploy: deployService, TestDeployJobs: deployManager,
	}
	if config.TestBotEnabled() {
		botConfig, err := testbot.LoadConfig(config.TestBotConfig)
		if err != nil {
			logger.Fatal(err)
		}
		botClient := &http.Client{Timeout: defaultTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		services.TestBot, err = testbot.NewService(botConfig, testbot.Credentials{Area: config.TestBotArea, Mobile: config.TestBotMobile, Password: config.TestBotPassword}, botClient, logger)
		if err != nil {
			logger.Fatal(err)
		}
	}
	if config.ReadOnlyAPIConfig != "" {
		apiConfig, err := testapi.LoadConfig(config.ReadOnlyAPIConfig)
		if err != nil {
			logger.Fatal(err)
		}
		apiClient := &http.Client{
			Timeout:       defaultTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
		services.ReadOnlyAPI, err = testapi.NewService(apiConfig, apiClient, logger)
		if err != nil {
			logger.Fatal(err)
		}
	}
	if config.LogMCPCommand != "" {
		bridge := testlogs.NewLazy(config.LogMCPCommand, config.LogMCPArgs, logger)
		defer bridge.Close()
		services.TestLogs = bridge
	}

	server := mcpserver.New(services)
	if config.Transport == appconfig.TransportHTTP {
		if err := runHTTP(config, server, logger); err != nil {
			logger.Fatal(err)
		}
		return
	}

	// STDIO carries MCP JSON-RPC on stdout. Diagnostics must stay on stderr or
	// they corrupt the protocol stream and disconnect the client.
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		logger.Fatal(err)
	}
}

func runHTTP(config appconfig.Config, server *mcp.Server, logger *log.Logger) error {
	tokens := make(map[string]string, len(config.AuthTokens)+1)
	for name, token := range config.AuthTokens {
		tokens[name] = token
	}
	if config.AuthToken != "" {
		tokens["legacy"] = config.AuthToken
	}
	handler := httptransport.NewHandler(server, tokens, config.MaxConcurrent, logger)
	httpServer := &http.Server{
		Addr:              config.ListenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		// Deployment is an authenticated, bounded operation but compiling a legacy
		// Go service can take several minutes on 004.
		WriteTimeout: deploymentWindow,
		IdleTimeout:  60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverError := make(chan error, 1)
	go func() {
		logger.Printf("transport=http listen=%q path=/mcp", config.ListenAddress)
		serverError <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serverError:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}
