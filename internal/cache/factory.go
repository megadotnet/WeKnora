package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
)

// CacheFactory provides cache instances based on configuration
type CacheFactory struct {
	cacheManager   CacheManager
	knowledgeCache KnowledgeCache
	vectorCache    VectorCache
	config         *CacheConfig
}

// NewCacheFactory creates a new cache factory
func NewCacheFactory(config *CacheConfig) (*CacheFactory, error) {
	if config == nil {
		return nil, fmt.Errorf("cache config cannot be nil")
	}

	// Create Redis cache manager
	cacheManager, err := NewRedisCacheManager(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Redis cache manager: %w", err)
	}

	// Create knowledge cache with default TTL settings
	knowledgeTTL := &KnowledgeCacheTTL{
		SearchResults: 30 * time.Minute,
		KnowledgeBase: 2 * time.Hour,
		KnowledgeInfo: time.Hour,
		ChunkInfo:     time.Hour,
	}
	knowledgeCache := NewKnowledgeCache(cacheManager, knowledgeTTL)

	// Create vector cache with default TTL settings
	vectorTTL := &VectorCacheTTL{
		VectorResults:     15 * time.Minute,
		Embeddings:        24 * time.Hour,
		SimilarityResults: 30 * time.Minute,
		QueryEmbeddings:   time.Hour,
	}
	vectorCache := NewVectorCache(cacheManager, vectorTTL)

	factory := &CacheFactory{
		cacheManager:   cacheManager,
		knowledgeCache: knowledgeCache,
		vectorCache:    vectorCache,
		config:         config,
	}

	logger.Infof(context.Background(), "[CacheFactory] Cache factory initialized successfully")
	return factory, nil
}

// GetCacheManager returns the underlying cache manager
func (f *CacheFactory) GetCacheManager() CacheManager {
	return f.cacheManager
}

// GetKnowledgeCache returns the knowledge cache instance
func (f *CacheFactory) GetKnowledgeCache() KnowledgeCache {
	return f.knowledgeCache
}

// GetVectorCache returns the vector cache instance
func (f *CacheFactory) GetVectorCache() VectorCache {
	return f.vectorCache
}

// GetConfig returns the cache configuration
func (f *CacheFactory) GetConfig() *CacheConfig {
	return f.config
}

// HealthCheck performs health check on all cache components
func (f *CacheFactory) HealthCheck(ctx context.Context) error {
	if err := f.cacheManager.Health(ctx); err != nil {
		return fmt.Errorf("cache manager health check failed: %w", err)
	}
	
	logger.Debugf(ctx, "[CacheFactory] Health check passed")
	return nil
}

// Close closes all cache connections
func (f *CacheFactory) Close() error {
	if redisManager, ok := f.cacheManager.(*RedisCacheManager); ok {
		return redisManager.Close()
	}
	return nil
}

// DefaultCacheConfig returns a default cache configuration
func DefaultCacheConfig() *CacheConfig {
	return &CacheConfig{
		Address:           "localhost:6379",
		Password:          "",
		DB:                0,
		PoolSize:          100,
		MinIdleConns:      10,
		MaxConnAge:        30 * time.Minute,
		DefaultTTL:        time.Hour,
		KeyPrefix:         "weknora:cache:",
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		EnableCompression: true,
		CompressionLevel:  6, // Best speed/compression ratio
	}
}

// CacheManagerFromConfig creates a cache manager from configuration map
func CacheManagerFromConfig(configMap map[string]interface{}) (*CacheFactory, error) {
	config := &CacheConfig{}
	
	// Parse configuration from map
	if addr, ok := configMap["address"].(string); ok {
		config.Address = addr
	} else {
		config.Address = "localhost:6379"
	}
	
	if pwd, ok := configMap["password"].(string); ok {
		config.Password = pwd
	}
	
	if db, ok := configMap["db"].(int); ok {
		config.DB = db
	}
	
	if poolSize, ok := configMap["pool_size"].(int); ok {
		config.PoolSize = poolSize
	} else {
		config.PoolSize = 100
	}
	
	if minIdle, ok := configMap["min_idle_conns"].(int); ok {
		config.MinIdleConns = minIdle
	} else {
		config.MinIdleConns = 10
	}
	
	if maxAge, ok := configMap["max_conn_age"].(string); ok {
		if duration, err := time.ParseDuration(maxAge); err == nil {
			config.MaxConnAge = duration
		} else {
			config.MaxConnAge = 30 * time.Minute
		}
	} else {
		config.MaxConnAge = 30 * time.Minute
	}
	
	if ttl, ok := configMap["default_ttl"].(string); ok {
		if duration, err := time.ParseDuration(ttl); err == nil {
			config.DefaultTTL = duration
		} else {
			config.DefaultTTL = time.Hour
		}
	} else {
		config.DefaultTTL = time.Hour
	}
	
	if prefix, ok := configMap["key_prefix"].(string); ok {
		config.KeyPrefix = prefix
	} else {
		config.KeyPrefix = "weknora:cache:"
	}
	
	if readTimeout, ok := configMap["read_timeout"].(string); ok {
		if duration, err := time.ParseDuration(readTimeout); err == nil {
			config.ReadTimeout = duration
		} else {
			config.ReadTimeout = 5 * time.Second
		}
	} else {
		config.ReadTimeout = 5 * time.Second
	}
	
	if writeTimeout, ok := configMap["write_timeout"].(string); ok {
		if duration, err := time.ParseDuration(writeTimeout); err == nil {
			config.WriteTimeout = duration
		} else {
			config.WriteTimeout = 5 * time.Second
		}
	} else {
		config.WriteTimeout = 5 * time.Second
	}
	
	if compress, ok := configMap["enable_compression"].(bool); ok {
		config.EnableCompression = compress
	} else {
		config.EnableCompression = true
	}
	
	if level, ok := configMap["compression_level"].(int); ok {
		config.CompressionLevel = level
	} else {
		config.CompressionLevel = 6
	}
	
	return NewCacheFactory(config)
}

// CacheMetrics provides cache performance metrics
type CacheMetrics struct {
	KnowledgeCache struct {
		SearchResultsHits   int64 `json:"search_results_hits"`
		SearchResultsMisses int64 `json:"search_results_misses"`
		KnowledgeBaseHits   int64 `json:"knowledge_base_hits"`
		KnowledgeBaseMisses int64 `json:"knowledge_base_misses"`
	} `json:"knowledge_cache"`
	
	VectorCache struct {
		EmbeddingHits       int64 `json:"embedding_hits"`
		EmbeddingMisses     int64 `json:"embedding_misses"`
		VectorResultsHits   int64 `json:"vector_results_hits"`
		VectorResultsMisses int64 `json:"vector_results_misses"`
		SimilarityHits      int64 `json:"similarity_hits"`
		SimilarityMisses    int64 `json:"similarity_misses"`
	} `json:"vector_cache"`
	
	Overall CacheStats `json:"overall"`
}

// GetMetrics returns comprehensive cache metrics
func (f *CacheFactory) GetMetrics(ctx context.Context) (*CacheMetrics, error) {
	stats, err := f.cacheManager.(*RedisCacheManager).GetStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get cache stats: %w", err)
	}
	
	metrics := &CacheMetrics{
		Overall: *stats,
	}
	
	// In a real implementation, you would track specific metrics for knowledge and vector caches
	// For now, we'll return the overall stats
	
	return metrics, nil
}

// WarmupCache performs cache warmup operations
func (f *CacheFactory) WarmupCache(ctx context.Context) error {
	logger.Infof(ctx, "[CacheFactory] Starting cache warmup...")
	
	// Test basic cache operations
	testKey := "warmup:test"
	testValue := []byte("warmup test value")
	
	if err := f.cacheManager.Set(ctx, testKey, testValue, time.Minute); err != nil {
		return fmt.Errorf("cache warmup set failed: %w", err)
	}
	
	if _, err := f.cacheManager.Get(ctx, testKey); err != nil {
		return fmt.Errorf("cache warmup get failed: %w", err)
	}
	
	if err := f.cacheManager.Delete(ctx, testKey); err != nil {
		return fmt.Errorf("cache warmup delete failed: %w", err)
	}
	
	logger.Infof(ctx, "[CacheFactory] Cache warmup completed successfully")
	return nil
}