package cache

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// CacheManager defines the interface for cache operations
type CacheManager interface {
	// Get retrieves a value from cache by key
	Get(ctx context.Context, key string) ([]byte, error)
	
	// Set stores a value in cache with TTL
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	
	// Delete removes a key from cache
	Delete(ctx context.Context, key string) error
	
	// Exists checks if a key exists in cache
	Exists(ctx context.Context, key string) (bool, error)
	
	// Clear clears all cache entries (use with caution)
	Clear(ctx context.Context) error
	
	// Health checks cache service health
	Health(ctx context.Context) error
}

// KnowledgeCache provides caching for knowledge base operations
type KnowledgeCache interface {
	// GetSearchResults retrieves cached search results
	GetSearchResults(ctx context.Context, queryHash string) ([]*types.SearchResult, error)
	
	// SetSearchResults caches search results
	SetSearchResults(ctx context.Context, queryHash string, results []*types.SearchResult, ttl time.Duration) error
	
	// GetKnowledgeBase retrieves cached knowledge base
	GetKnowledgeBase(ctx context.Context, kbID string) (*types.KnowledgeBase, error)
	
	// SetKnowledgeBase caches knowledge base
	SetKnowledgeBase(ctx context.Context, kbID string, kb *types.KnowledgeBase, ttl time.Duration) error
	
	// InvalidateKnowledgeBase removes knowledge base from cache
	InvalidateKnowledgeBase(ctx context.Context, kbID string) error
}

// VectorCache provides caching for vector operations
type VectorCache interface {
	// GetVectorResults retrieves cached vector search results
	GetVectorResults(ctx context.Context, vectorHash string) ([]*types.RetrieveResult, error)
	
	// SetVectorResults caches vector search results
	SetVectorResults(ctx context.Context, vectorHash string, results []*types.RetrieveResult, ttl time.Duration) error
	
	// GetEmbedding retrieves cached embedding
	GetEmbedding(ctx context.Context, contentHash string) ([]float32, error)
	
	// SetEmbedding caches embedding
	SetEmbedding(ctx context.Context, contentHash string, embedding []float32, ttl time.Duration) error
	
	// GetSimilarityResults retrieves cached similarity results
	GetSimilarityResults(ctx context.Context, queryHash string) ([]*types.IndexWithScore, error)
	
	// SetSimilarityResults caches similarity results
	SetSimilarityResults(ctx context.Context, queryHash string, results []*types.IndexWithScore, ttl time.Duration) error
}

// CacheConfig holds cache configuration
type CacheConfig struct {
	// Redis connection settings
	Address  string        `yaml:"address" json:"address"`
	Password string        `yaml:"password" json:"password"`
	DB       int           `yaml:"db" json:"db"`
	
	// Pool settings
	PoolSize     int           `yaml:"pool_size" json:"pool_size"`
	MinIdleConns int           `yaml:"min_idle_conns" json:"min_idle_conns"`
	MaxConnAge   time.Duration `yaml:"max_conn_age" json:"max_conn_age"`
	
	// Cache settings
	DefaultTTL    time.Duration `yaml:"default_ttl" json:"default_ttl"`
	KeyPrefix     string        `yaml:"key_prefix" json:"key_prefix"`
	
	// Performance settings
	ReadTimeout  time.Duration `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout" json:"write_timeout"`
	
	// Compression settings
	EnableCompression bool `yaml:"enable_compression" json:"enable_compression"`
	CompressionLevel  int  `yaml:"compression_level" json:"compression_level"`
}

// CacheStats provides cache statistics
type CacheStats struct {
	HitCount    int64 `json:"hit_count"`
	MissCount   int64 `json:"miss_count"`
	SetCount    int64 `json:"set_count"`
	DeleteCount int64 `json:"delete_count"`
	KeyCount    int64 `json:"key_count"`
	HitRate     float64 `json:"hit_rate"`
}

// StatsReporter provides cache statistics reporting
type StatsReporter interface {
	// GetStats returns current cache statistics
	GetStats(ctx context.Context) (*CacheStats, error)
	
	// ResetStats resets cache statistics
	ResetStats(ctx context.Context) error
}