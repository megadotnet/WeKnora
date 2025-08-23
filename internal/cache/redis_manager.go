package cache

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/redis/go-redis/v9"
)

// RedisCacheManager implements CacheManager using Redis
type RedisCacheManager struct {
	client *redis.Client
	config *CacheConfig
	stats  *cacheStats
}

// cacheStats tracks cache performance metrics
type cacheStats struct {
	hitCount    int64
	missCount   int64
	setCount    int64
	deleteCount int64
}

// NewRedisCacheManager creates a new Redis cache manager
func NewRedisCacheManager(config *CacheConfig) (*RedisCacheManager, error) {
	if config == nil {
		return nil, fmt.Errorf("cache config cannot be nil")
	}

	// Set default values if not provided
	if config.PoolSize == 0 {
		config.PoolSize = 100
	}
	if config.MinIdleConns == 0 {
		config.MinIdleConns = 10
	}
	if config.MaxConnAge == 0 {
		config.MaxConnAge = 30 * time.Minute
	}
	if config.DefaultTTL == 0 {
		config.DefaultTTL = time.Hour
	}
	if config.KeyPrefix == "" {
		config.KeyPrefix = "weknora:cache:"
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 5 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 5 * time.Second
	}

	// Create Redis client with optimized connection pool
	client := redis.NewClient(&redis.Options{
		Addr:         config.Address,
		Password:     config.Password,
		DB:           config.DB,
		PoolSize:     config.PoolSize,
		MinIdleConns: config.MinIdleConns,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
		// Enable connection pool monitoring
		OnConnect: func(ctx context.Context, cn *redis.Conn) error {
			logger.Debugf(ctx, "[Cache] New Redis connection established")
			return nil
		},
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	manager := &RedisCacheManager{
		client: client,
		config: config,
		stats:  &cacheStats{},
	}

	logger.Infof(context.Background(), "[Cache] Redis cache manager initialized with pool size: %d", config.PoolSize)
	return manager, nil
}

// buildKey constructs the full cache key with prefix
func (r *RedisCacheManager) buildKey(key string) string {
	return r.config.KeyPrefix + key
}

// compress compresses data if compression is enabled
func (r *RedisCacheManager) compress(data []byte) ([]byte, error) {
	if !r.config.EnableCompression {
		return data, nil
	}

	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, r.config.CompressionLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip writer: %w", err)
	}

	if _, err := writer.Write(data); err != nil {
		return nil, fmt.Errorf("failed to compress data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close gzip writer: %w", err)
	}

	return buf.Bytes(), nil
}

// decompress decompresses data if compression is enabled
func (r *RedisCacheManager) decompress(data []byte) ([]byte, error) {
	if !r.config.EnableCompression {
		return data, nil
	}

	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer reader.Close()

	result, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress data: %w", err)
	}

	return result, nil
}

// Get retrieves a value from cache by key
func (r *RedisCacheManager) Get(ctx context.Context, key string) ([]byte, error) {
	fullKey := r.buildKey(key)
	
	data, err := r.client.Get(ctx, fullKey).Bytes()
	if err != nil {
		if err == redis.Nil {
			atomic.AddInt64(&r.stats.missCount, 1)
			return nil, nil // Cache miss
		}
		logger.Errorf(ctx, "[Cache] Failed to get key %s: %v", key, err)
		return nil, fmt.Errorf("failed to get cache key %s: %w", key, err)
	}

	atomic.AddInt64(&r.stats.hitCount, 1)
	
	// Decompress if needed
	result, err := r.decompress(data)
	if err != nil {
		logger.Errorf(ctx, "[Cache] Failed to decompress data for key %s: %v", key, err)
		return nil, fmt.Errorf("failed to decompress data: %w", err)
	}

	logger.Debugf(ctx, "[Cache] Cache hit for key: %s", key)
	return result, nil
}

// Set stores a value in cache with TTL
func (r *RedisCacheManager) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl == 0 {
		ttl = r.config.DefaultTTL
	}

	fullKey := r.buildKey(key)
	
	// Compress if needed
	data, err := r.compress(value)
	if err != nil {
		logger.Errorf(ctx, "[Cache] Failed to compress data for key %s: %v", key, err)
		return fmt.Errorf("failed to compress data: %w", err)
	}

	if err := r.client.Set(ctx, fullKey, data, ttl).Err(); err != nil {
		logger.Errorf(ctx, "[Cache] Failed to set key %s: %v", key, err)
		return fmt.Errorf("failed to set cache key %s: %w", key, err)
	}

	atomic.AddInt64(&r.stats.setCount, 1)
	logger.Debugf(ctx, "[Cache] Set key %s with TTL %v", key, ttl)
	return nil
}

// Delete removes a key from cache
func (r *RedisCacheManager) Delete(ctx context.Context, key string) error {
	fullKey := r.buildKey(key)
	
	if err := r.client.Del(ctx, fullKey).Err(); err != nil {
		logger.Errorf(ctx, "[Cache] Failed to delete key %s: %v", key, err)
		return fmt.Errorf("failed to delete cache key %s: %w", key, err)
	}

	atomic.AddInt64(&r.stats.deleteCount, 1)
	logger.Debugf(ctx, "[Cache] Deleted key: %s", key)
	return nil
}

// Exists checks if a key exists in cache
func (r *RedisCacheManager) Exists(ctx context.Context, key string) (bool, error) {
	fullKey := r.buildKey(key)
	
	count, err := r.client.Exists(ctx, fullKey).Result()
	if err != nil {
		logger.Errorf(ctx, "[Cache] Failed to check existence of key %s: %v", key, err)
		return false, fmt.Errorf("failed to check cache key existence %s: %w", key, err)
	}

	return count > 0, nil
}

// Clear clears all cache entries with the configured prefix
func (r *RedisCacheManager) Clear(ctx context.Context) error {
	pattern := r.config.KeyPrefix + "*"
	
	// Use SCAN to avoid blocking Redis
	iter := r.client.Scan(ctx, 0, pattern, 100).Iterator()
	var keys []string
	
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
		
		// Delete in batches to avoid memory issues
		if len(keys) >= 1000 {
			if err := r.client.Del(ctx, keys...).Err(); err != nil {
				logger.Errorf(ctx, "[Cache] Failed to delete batch of keys: %v", err)
				return fmt.Errorf("failed to delete batch of keys: %w", err)
			}
			keys = keys[:0] // Reset slice
		}
	}
	
	// Delete remaining keys
	if len(keys) > 0 {
		if err := r.client.Del(ctx, keys...).Err(); err != nil {
			logger.Errorf(ctx, "[Cache] Failed to delete remaining keys: %v", err)
			return fmt.Errorf("failed to delete remaining keys: %w", err)
		}
	}
	
	if err := iter.Err(); err != nil {
		logger.Errorf(ctx, "[Cache] Error during scan operation: %v", err)
		return fmt.Errorf("error during scan operation: %w", err)
	}

	logger.Infof(ctx, "[Cache] Cleared cache with pattern: %s", pattern)
	return nil
}

// Health checks cache service health
func (r *RedisCacheManager) Health(ctx context.Context) error {
	if err := r.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("Redis health check failed: %w", err)
	}
	return nil
}

// GetStats returns current cache statistics
func (r *RedisCacheManager) GetStats(ctx context.Context) (*CacheStats, error) {
	hitCount := atomic.LoadInt64(&r.stats.hitCount)
	missCount := atomic.LoadInt64(&r.stats.missCount)
	setCount := atomic.LoadInt64(&r.stats.setCount)
	deleteCount := atomic.LoadInt64(&r.stats.deleteCount)
	
	totalRequests := hitCount + missCount
	var hitRate float64
	if totalRequests > 0 {
		hitRate = float64(hitCount) / float64(totalRequests)
	}

	// Get key count from Redis (approximate)
	info, err := r.client.Info(ctx, "keyspace").Result()
	keyCount := int64(0)
	if err == nil {
		// Parse keyspace info to get approximate key count
		// This is a simplified parsing - in production, you might want more robust parsing
		_ = info // For now, we'll set keyCount to 0
	}

	return &CacheStats{
		HitCount:    hitCount,
		MissCount:   missCount,
		SetCount:    setCount,
		DeleteCount: deleteCount,
		KeyCount:    keyCount,
		HitRate:     hitRate,
	}, nil
}

// ResetStats resets cache statistics
func (r *RedisCacheManager) ResetStats(ctx context.Context) error {
	atomic.StoreInt64(&r.stats.hitCount, 0)
	atomic.StoreInt64(&r.stats.missCount, 0)
	atomic.StoreInt64(&r.stats.setCount, 0)
	atomic.StoreInt64(&r.stats.deleteCount, 0)
	
	logger.Infof(ctx, "[Cache] Cache statistics reset")
	return nil
}

// Close closes the Redis connection
func (r *RedisCacheManager) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}

// Helper function to serialize data to JSON
func (r *RedisCacheManager) marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// Helper function to deserialize data from JSON
func (r *RedisCacheManager) unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}