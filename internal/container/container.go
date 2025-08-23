// Package container implements dependency injection container setup
// Provides centralized configuration for services, repositories, and handlers
// This package is responsible for wiring up all dependencies and ensuring proper lifecycle management
package container

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	esv7 "github.com/elastic/go-elasticsearch/v7"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/panjf2000/ants/v2"
	"go.uber.org/dig"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/application/repository"
	elasticsearchRepoV7 "github.com/Tencent/WeKnora/internal/application/repository/retriever/elasticsearch/v7"
	elasticsearchRepoV8 "github.com/Tencent/WeKnora/internal/application/repository/retriever/elasticsearch/v8"
	postgresRepo "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	"github.com/Tencent/WeKnora/internal/application/service"
	chatpipline "github.com/Tencent/WeKnora/internal/application/service/chat_pipline"
	"github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/cache"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/models/utils/ollama"
	"github.com/Tencent/WeKnora/internal/router"
	"github.com/Tencent/WeKnora/internal/stream"
	"github.com/Tencent/WeKnora/internal/tracing"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/services/docreader/src/client"
)

// BuildContainer constructs the dependency injection container
// Registers all components, services, repositories and handlers needed by the application
// Creates a fully configured application container with proper dependency resolution
// Parameters:
//   - container: Base dig container to add dependencies to
//
// Returns:
//   - Configured container with all application dependencies registered
func BuildContainer(container *dig.Container) *dig.Container {
	// Register resource cleaner for proper cleanup of resources
	must(container.Provide(NewResourceCleaner, dig.As(new(interfaces.ResourceCleaner))))

	// Core infrastructure configuration
	must(container.Provide(config.LoadConfig))
	must(container.Provide(initTracer))
	must(container.Provide(initDatabase))
	must(container.Provide(initFileService))
	must(container.Provide(initAntsPool))
	must(container.Provide(initCacheFactory))

	// Register goroutine pool cleanup handler
	must(container.Invoke(registerPoolCleanup))

	// Initialize retrieval engine registry for search capabilities
	must(container.Provide(initRetrieveEngineRegistry))

	// External service clients
	must(container.Provide(initDocReaderClient))
	must(container.Provide(initOllamaService))
	must(container.Provide(stream.NewStreamManager))

	// Data repositories layer
	must(container.Provide(repository.NewTenantRepository))
	must(container.Provide(repository.NewKnowledgeBaseRepository))
	must(container.Provide(repository.NewKnowledgeRepository))
	must(container.Provide(repository.NewChunkRepository))
	must(container.Provide(repository.NewSessionRepository))
	must(container.Provide(repository.NewMessageRepository))
	must(container.Provide(repository.NewModelRepository))

	// Business service layer
	must(container.Provide(service.NewTenantService))
	must(container.Provide(service.NewKnowledgeBaseService))
	must(container.Provide(service.NewKnowledgeService))
	must(container.Provide(service.NewSessionService))
	must(container.Provide(service.NewMessageService))
	must(container.Provide(service.NewChunkService))
	must(container.Provide(embedding.NewBatchEmbedder))
	must(container.Provide(service.NewTestDataService))
	must(container.Provide(service.NewModelService))
	must(container.Provide(service.NewDatasetService))
	must(container.Provide(service.NewEvaluationService))

	// Chat pipeline components for processing chat requests
	must(container.Provide(chatpipline.NewEventManager))
	must(container.Invoke(chatpipline.NewPluginTracing))
	must(container.Invoke(chatpipline.NewPluginSearch))
	must(container.Invoke(chatpipline.NewPluginRerank))
	must(container.Invoke(chatpipline.NewPluginMerge))
	must(container.Invoke(chatpipline.NewPluginIntoChatMessage))
	must(container.Invoke(chatpipline.NewPluginChatCompletion))
	must(container.Invoke(chatpipline.NewPluginChatCompletionStream))
	must(container.Invoke(chatpipline.NewPluginStreamFilter))
	must(container.Invoke(chatpipline.NewPluginFilterTopK))
	must(container.Invoke(chatpipline.NewPluginPreprocess))
	must(container.Invoke(chatpipline.NewPluginRewrite))

	// HTTP handlers layer
	must(container.Provide(handler.NewTenantHandler))
	must(container.Provide(handler.NewKnowledgeBaseHandler))
	must(container.Provide(handler.NewKnowledgeHandler))
	must(container.Provide(handler.NewChunkHandler))
	must(container.Provide(handler.NewSessionHandler))
	must(container.Provide(handler.NewMessageHandler))
	must(container.Provide(handler.NewTestDataHandler))
	must(container.Provide(handler.NewModelHandler))
	must(container.Provide(handler.NewEvaluationHandler))
	must(container.Provide(handler.NewInitializationHandler))

	// Router configuration
	must(container.Provide(router.NewRouter))

	return container
}

// must is a helper function for error handling
// Panics if the error is not nil, useful for configuration steps that must succeed
// Parameters:
//   - err: Error to check
func must(err error) {
	if err != nil {
		panic(err)
	}
}

// initTracer initializes OpenTelemetry tracer
// Sets up distributed tracing for observability across the application
// Parameters:
//   - None
//
// Returns:
//   - Configured tracer instance
//   - Error if initialization fails
func initTracer() (*tracing.Tracer, error) {
	return tracing.InitTracer()
}

// initDatabase initializes database connection
// Creates and configures database connection based on environment configuration
// Supports multiple database backends (PostgreSQL)
// Parameters:
//   - cfg: Application configuration
//
// Returns:
//   - Configured database connection
//   - Error if connection fails
func initDatabase(cfg *config.Config) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch os.Getenv("DB_DRIVER") {
	case "postgres":
		dsn := fmt.Sprintf(
			"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
			os.Getenv("DB_HOST"),
			os.Getenv("DB_PORT"),
			os.Getenv("DB_USER"),
			os.Getenv("DB_PASSWORD"),
			os.Getenv("DB_NAME"),
			"disable",
		)
		dialector = postgres.Open(dsn)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", os.Getenv("DB_DRIVER"))
	}
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, err
	}

	// Get underlying SQL DB object
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	// Configure connection pool parameters
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(time.Duration(10) * time.Minute)

	return db, nil
}

// initFileService initializes file storage service
// Creates the appropriate file storage service based on configuration
// Supports multiple storage backends (MinIO, COS, local filesystem)
// Parameters:
//   - cfg: Application configuration
//
// Returns:
//   - Configured file service implementation
//   - Error if initialization fails
func initFileService(cfg *config.Config) (interfaces.FileService, error) {
	switch os.Getenv("STORAGE_TYPE") {
	case "minio":
		if os.Getenv("MINIO_ENDPOINT") == "" ||
			os.Getenv("MINIO_ACCESS_KEY_ID") == "" ||
			os.Getenv("MINIO_SECRET_ACCESS_KEY") == "" ||
			os.Getenv("MINIO_BUCKET_NAME") == "" {
			return nil, fmt.Errorf("missing MinIO configuration")
		}
		return file.NewMinioFileService(
			os.Getenv("MINIO_ENDPOINT"),
			os.Getenv("MINIO_ACCESS_KEY_ID"),
			os.Getenv("MINIO_SECRET_ACCESS_KEY"),
			os.Getenv("MINIO_BUCKET_NAME"),
			false,
		)
	case "cos":
		if os.Getenv("COS_APP_ID") == "" ||
			os.Getenv("COS_REGION") == "" ||
			os.Getenv("COS_SECRET_ID") == "" ||
			os.Getenv("COS_SECRET_KEY") == "" ||
			os.Getenv("COS_PATH_PREFIX") == "" {
			return nil, fmt.Errorf("missing COS configuration")
		}
		return file.NewCosFileService(
			os.Getenv("COS_APP_ID"),
			os.Getenv("COS_REGION"),
			os.Getenv("COS_SECRET_ID"),
			os.Getenv("COS_SECRET_KEY"),
			os.Getenv("COS_PATH_PREFIX"),
		)
	case "local":
		return file.NewLocalFileService(os.Getenv("LOCAL_STORAGE_BASE_DIR")), nil
	case "dummy":
		return file.NewDummyFileService(), nil
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", os.Getenv("STORAGE_TYPE"))
	}
}

// initRetrieveEngineRegistry initializes the retrieval engine registry
// Sets up and configures various search engine backends based on configuration
// Supports multiple retrieval engines (PostgreSQL, ElasticsearchV7, ElasticsearchV8)
// Parameters:
//   - db: Database connection
//   - cfg: Application configuration
//   - cacheFactory: Cache factory for caching retrieval results
//
// Returns:
//   - Configured retrieval engine registry
//   - Error if initialization fails
func initRetrieveEngineRegistry(db *gorm.DB, cfg *config.Config, cacheFactory *cache.CacheFactory) (interfaces.RetrieveEngineRegistry, error) {
	registry := retriever.NewRetrieveEngineRegistry()
	retrieveDriver := strings.Split(os.Getenv("RETRIEVE_DRIVER"), ",")
	log := logger.GetLogger(context.Background())

	if slices.Contains(retrieveDriver, "postgres") {
		postgresRepo := postgresRepo.NewPostgresRetrieveEngineRepository(db)
		baseEngine := retriever.NewKVHybridRetrieveEngine(postgresRepo, types.PostgresRetrieverEngineType)
		
		// Wrap with caching if cache factory is available
		var engine interfaces.RetrieveEngineService
		if cacheFactory != nil {
			engine = retriever.NewCachedRetrieveEngine(baseEngine, cacheFactory)
			log.Infof("Postgres retrieve engine wrapped with caching")
		} else {
			engine = baseEngine
		}
		
		if err := registry.Register(engine); err != nil {
			log.Errorf("Register postgres retrieve engine failed: %v", err)
		} else {
			log.Infof("Register postgres retrieve engine success")
		}
	}
	if slices.Contains(retrieveDriver, "elasticsearch_v8") {
		client, err := elasticsearch.NewTypedClient(elasticsearch.Config{
			Addresses: []string{os.Getenv("ELASTICSEARCH_ADDR")},
			Username:  os.Getenv("ELASTICSEARCH_USERNAME"),
			Password:  os.Getenv("ELASTICSEARCH_PASSWORD"),
		})
		if err != nil {
			log.Errorf("Create elasticsearch_v8 client failed: %v", err)
		} else {
			elasticsearchRepo := elasticsearchRepoV8.NewElasticsearchEngineRepository(client, cfg)
			baseEngine := retriever.NewKVHybridRetrieveEngine(elasticsearchRepo, types.ElasticsearchRetrieverEngineType)
			
			// Wrap with caching if cache factory is available
			var engine interfaces.RetrieveEngineService
			if cacheFactory != nil {
				engine = retriever.NewCachedRetrieveEngine(baseEngine, cacheFactory)
				log.Infof("Elasticsearch v8 retrieve engine wrapped with caching")
			} else {
				engine = baseEngine
			}
			
			if err := registry.Register(engine); err != nil {
				log.Errorf("Register elasticsearch_v8 retrieve engine failed: %v", err)
			} else {
				log.Infof("Register elasticsearch_v8 retrieve engine success")
			}
		}
	}

	if slices.Contains(retrieveDriver, "elasticsearch_v7") {
		client, err := esv7.NewClient(esv7.Config{
			Addresses: []string{os.Getenv("ELASTICSEARCH_ADDR")},
			Username:  os.Getenv("ELASTICSEARCH_USERNAME"),
			Password:  os.Getenv("ELASTICSEARCH_PASSWORD"),
		})
		if err != nil {
			log.Errorf("Create elasticsearch_v7 client failed: %v", err)
		} else {
			elasticsearchRepo := elasticsearchRepoV7.NewElasticsearchEngineRepository(client, cfg)
			baseEngine := retriever.NewKVHybridRetrieveEngine(elasticsearchRepo, types.ElasticsearchRetrieverEngineType)
			
			// Wrap with caching if cache factory is available
			var engine interfaces.RetrieveEngineService
			if cacheFactory != nil {
				engine = retriever.NewCachedRetrieveEngine(baseEngine, cacheFactory)
				log.Infof("Elasticsearch v7 retrieve engine wrapped with caching")
			} else {
				engine = baseEngine
			}
			
			if err := registry.Register(engine); err != nil {
				log.Errorf("Register elasticsearch_v7 retrieve engine failed: %v", err)
			} else {
				log.Infof("Register elasticsearch_v7 retrieve engine success")
			}
		}
	}
	return registry, nil
}

// initAntsPool initializes the goroutine pool
// Creates a managed goroutine pool for concurrent task execution
// Parameters:
//   - cfg: Application configuration
//
// Returns:
//   - Configured goroutine pool
//   - Error if initialization fails
func initAntsPool(cfg *config.Config) (*ants.Pool, error) {
	// Default to 5 if not specified in config
	poolSize := os.Getenv("CONCURRENCY_POOL_SIZE")
	if poolSize == "" {
		poolSize = "5"
	}
	poolSizeInt, err := strconv.Atoi(poolSize)
	if err != nil {
		return nil, err
	}
	// Set up the pool with pre-allocation for better performance
	return ants.NewPool(poolSizeInt, ants.WithPreAlloc(true))
}

// registerPoolCleanup registers the goroutine pool for cleanup
// Ensures proper cleanup of the goroutine pool when application shuts down
// Parameters:
//   - pool: Goroutine pool
//   - cleaner: Resource cleaner
func registerPoolCleanup(pool *ants.Pool, cleaner interfaces.ResourceCleaner) {
	cleaner.RegisterWithName("AntsPool", func() error {
		pool.Release()
		return nil
	})
}

// initDocReaderClient initializes the document reader client
// Creates a client for interacting with the document reader service
// Parameters:
//   - cfg: Application configuration
//
// Returns:
//   - Configured document reader client
//   - Error if initialization fails
func initDocReaderClient(cfg *config.Config) (*client.Client, error) {
	// Use the DocReader URL from environment or config
	docReaderURL := os.Getenv("DOCREADER_ADDR")
	if docReaderURL == "" && cfg.DocReader != nil {
		docReaderURL = cfg.DocReader.Addr
	}
	return client.NewClient(docReaderURL)
}

// initOllamaService initializes the Ollama service client
// Creates a client for interacting with Ollama API for model inference
// Parameters:
//   - None
//
// Returns:
//   - Configured Ollama service client
//   - Error if initialization fails
func initOllamaService() (*ollama.OllamaService, error) {
	// Get Ollama service from existing factory function
	return ollama.GetOllamaService()
}

// initCacheFactory initializes the cache factory
// Creates and configures Redis-based cache system for improved performance
// Parameters:
//   - cfg: Application configuration
//
// Returns:
//   - Configured cache factory
//   - Error if initialization fails
func initCacheFactory(cfg *config.Config) (*cache.CacheFactory, error) {
	log := logger.GetLogger(context.Background())
	
	// Check if cache is enabled
	if cfg.Cache == nil || !cfg.Cache.Enabled {
		log.Infof("[Cache] Cache is disabled, skipping initialization")
		return nil, nil
	}
	
	// Create cache configuration from app config
	cacheConfig := &cache.CacheConfig{
		Address:           cfg.Cache.Address,
		Password:          cfg.Cache.Password,
		DB:                cfg.Cache.DB,
		PoolSize:          cfg.Cache.PoolSize,
		MinIdleConns:      cfg.Cache.MinIdleConns,
		MaxConnAge:        cfg.Cache.MaxConnAge,
		DefaultTTL:        cfg.Cache.DefaultTTL,
		KeyPrefix:         cfg.Cache.KeyPrefix,
		ReadTimeout:       cfg.Cache.ReadTimeout,
		WriteTimeout:      cfg.Cache.WriteTimeout,
		EnableCompression: cfg.Cache.EnableCompression,
		CompressionLevel:  cfg.Cache.CompressionLevel,
	}
	
	// Set defaults if not provided in config
	if cacheConfig.Address == "" {
		cacheConfig.Address = os.Getenv("REDIS_ADDR")
		if cacheConfig.Address == "" {
			cacheConfig.Address = "localhost:6379"
		}
	}
	
	if cacheConfig.Password == "" {
		cacheConfig.Password = os.Getenv("REDIS_PASSWORD")
	}
	
	if cacheConfig.KeyPrefix == "" {
		cacheConfig.KeyPrefix = "weknora:cache:"
	}
	
	if cacheConfig.PoolSize == 0 {
		cacheConfig.PoolSize = 100
	}
	
	if cacheConfig.MinIdleConns == 0 {
		cacheConfig.MinIdleConns = 10
	}
	
	if cacheConfig.MaxConnAge == 0 {
		cacheConfig.MaxConnAge = 30 * time.Minute
	}
	
	if cacheConfig.DefaultTTL == 0 {
		cacheConfig.DefaultTTL = time.Hour
	}
	
	if cacheConfig.ReadTimeout == 0 {
		cacheConfig.ReadTimeout = 5 * time.Second
	}
	
	if cacheConfig.WriteTimeout == 0 {
		cacheConfig.WriteTimeout = 5 * time.Second
	}
	
	// Create cache factory
	factory, err := cache.NewCacheFactory(cacheConfig)
	if err != nil {
		log.Errorf("[Cache] Failed to initialize cache factory: %v", err)
		return nil, fmt.Errorf("failed to initialize cache factory: %w", err)
	}
	
	// Perform health check
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	if err := factory.HealthCheck(ctx); err != nil {
		log.Errorf("[Cache] Cache health check failed: %v", err)
		return nil, fmt.Errorf("cache health check failed: %w", err)
	}
	
	// Perform cache warmup
	if err := factory.WarmupCache(ctx); err != nil {
		log.Warnf("[Cache] Cache warmup failed: %v", err)
		// Don't fail initialization on warmup failure
	}
	
	log.Infof("[Cache] Cache factory initialized successfully with Redis at %s", cacheConfig.Address)
	return factory, nil
}
