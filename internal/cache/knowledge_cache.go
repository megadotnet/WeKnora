package cache

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// KnowledgeCacheImpl implements KnowledgeCache using Redis
type KnowledgeCacheImpl struct {
	cacheManager CacheManager
	ttlConfig    KnowledgeCacheTTL
}

// KnowledgeCacheTTL defines TTL for different types of knowledge cache
type KnowledgeCacheTTL struct {
	SearchResults  time.Duration `yaml:"search_results" json:"search_results"`
	KnowledgeBase  time.Duration `yaml:"knowledge_base" json:"knowledge_base"`
	KnowledgeInfo  time.Duration `yaml:"knowledge_info" json:"knowledge_info"`
	ChunkInfo      time.Duration `yaml:"chunk_info" json:"chunk_info"`
}

// NewKnowledgeCache creates a new knowledge cache instance
func NewKnowledgeCache(cacheManager CacheManager, ttlConfig *KnowledgeCacheTTL) KnowledgeCache {
	if ttlConfig == nil {
		// Set default TTL values
		ttlConfig = &KnowledgeCacheTTL{
			SearchResults: 30 * time.Minute,
			KnowledgeBase: 2 * time.Hour,
			KnowledgeInfo: time.Hour,
			ChunkInfo:     time.Hour,
		}
	}

	return &KnowledgeCacheImpl{
		cacheManager: cacheManager,
		ttlConfig:    *ttlConfig,
	}
}

// generateQueryHash generates a consistent hash for search queries
func (k *KnowledgeCacheImpl) generateQueryHash(query string, kbID string, params ...interface{}) string {
	hasher := md5.New()
	hasher.Write([]byte(query))
	hasher.Write([]byte(kbID))
	
	// Include additional parameters in hash
	for _, param := range params {
		hasher.Write([]byte(fmt.Sprintf("%v", param)))
	}
	
	return hex.EncodeToString(hasher.Sum(nil))
}

// buildSearchKey builds cache key for search results
func (k *KnowledgeCacheImpl) buildSearchKey(queryHash string) string {
	return fmt.Sprintf("search:%s", queryHash)
}

// buildKnowledgeBaseKey builds cache key for knowledge base
func (k *KnowledgeCacheImpl) buildKnowledgeBaseKey(kbID string) string {
	return fmt.Sprintf("kb:%s", kbID)
}

// buildKnowledgeKey builds cache key for knowledge info
func (k *KnowledgeCacheImpl) buildKnowledgeKey(knowledgeID string) string {
	return fmt.Sprintf("knowledge:%s", knowledgeID)
}

// buildChunkKey builds cache key for chunk info
func (k *KnowledgeCacheImpl) buildChunkKey(chunkID string) string {
	return fmt.Sprintf("chunk:%s", chunkID)
}

// GetSearchResults retrieves cached search results
func (k *KnowledgeCacheImpl) GetSearchResults(ctx context.Context, queryHash string) ([]*types.SearchResult, error) {
	key := k.buildSearchKey(queryHash)
	
	data, err := k.cacheManager.Get(ctx, key)
	if err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to get search results from cache: %v", err)
		return nil, err
	}
	
	if data == nil {
		// Cache miss
		logger.Debugf(ctx, "[KnowledgeCache] Cache miss for search results: %s", queryHash)
		return nil, nil
	}
	
	var results []*types.SearchResult
	if err := k.unmarshal(data, &results); err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to unmarshal search results: %v", err)
		return nil, fmt.Errorf("failed to unmarshal search results: %w", err)
	}
	
	logger.Debugf(ctx, "[KnowledgeCache] Retrieved %d search results from cache", len(results))
	return results, nil
}

// SetSearchResults caches search results
func (k *KnowledgeCacheImpl) SetSearchResults(ctx context.Context, queryHash string, results []*types.SearchResult, ttl time.Duration) error {
	if ttl == 0 {
		ttl = k.ttlConfig.SearchResults
	}
	
	key := k.buildSearchKey(queryHash)
	
	data, err := k.marshal(results)
	if err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to marshal search results: %v", err)
		return fmt.Errorf("failed to marshal search results: %w", err)
	}
	
	if err := k.cacheManager.Set(ctx, key, data, ttl); err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to cache search results: %v", err)
		return err
	}
	
	logger.Debugf(ctx, "[KnowledgeCache] Cached %d search results with TTL %v", len(results), ttl)
	return nil
}

// GetKnowledgeBase retrieves cached knowledge base
func (k *KnowledgeCacheImpl) GetKnowledgeBase(ctx context.Context, kbID string) (*types.KnowledgeBase, error) {
	key := k.buildKnowledgeBaseKey(kbID)
	
	data, err := k.cacheManager.Get(ctx, key)
	if err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to get knowledge base from cache: %v", err)
		return nil, err
	}
	
	if data == nil {
		// Cache miss
		logger.Debugf(ctx, "[KnowledgeCache] Cache miss for knowledge base: %s", kbID)
		return nil, nil
	}
	
	var kb types.KnowledgeBase
	if err := k.unmarshal(data, &kb); err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to unmarshal knowledge base: %v", err)
		return nil, fmt.Errorf("failed to unmarshal knowledge base: %w", err)
	}
	
	logger.Debugf(ctx, "[KnowledgeCache] Retrieved knowledge base from cache: %s", kbID)
	return &kb, nil
}

// SetKnowledgeBase caches knowledge base
func (k *KnowledgeCacheImpl) SetKnowledgeBase(ctx context.Context, kbID string, kb *types.KnowledgeBase, ttl time.Duration) error {
	if ttl == 0 {
		ttl = k.ttlConfig.KnowledgeBase
	}
	
	key := k.buildKnowledgeBaseKey(kbID)
	
	data, err := k.marshal(kb)
	if err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to marshal knowledge base: %v", err)
		return fmt.Errorf("failed to marshal knowledge base: %w", err)
	}
	
	if err := k.cacheManager.Set(ctx, key, data, ttl); err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to cache knowledge base: %v", err)
		return err
	}
	
	logger.Debugf(ctx, "[KnowledgeCache] Cached knowledge base %s with TTL %v", kbID, ttl)
	return nil
}

// InvalidateKnowledgeBase removes knowledge base from cache
func (k *KnowledgeCacheImpl) InvalidateKnowledgeBase(ctx context.Context, kbID string) error {
	key := k.buildKnowledgeBaseKey(kbID)
	
	if err := k.cacheManager.Delete(ctx, key); err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to invalidate knowledge base cache: %v", err)
		return err
	}
	
	logger.Debugf(ctx, "[KnowledgeCache] Invalidated knowledge base cache: %s", kbID)
	return nil
}

// GetKnowledge retrieves cached knowledge info
func (k *KnowledgeCacheImpl) GetKnowledge(ctx context.Context, knowledgeID string) (*types.Knowledge, error) {
	key := k.buildKnowledgeKey(knowledgeID)
	
	data, err := k.cacheManager.Get(ctx, key)
	if err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to get knowledge from cache: %v", err)
		return nil, err
	}
	
	if data == nil {
		// Cache miss
		logger.Debugf(ctx, "[KnowledgeCache] Cache miss for knowledge: %s", knowledgeID)
		return nil, nil
	}
	
	var knowledge types.Knowledge
	if err := k.unmarshal(data, &knowledge); err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to unmarshal knowledge: %v", err)
		return nil, fmt.Errorf("failed to unmarshal knowledge: %w", err)
	}
	
	logger.Debugf(ctx, "[KnowledgeCache] Retrieved knowledge from cache: %s", knowledgeID)
	return &knowledge, nil
}

// SetKnowledge caches knowledge info
func (k *KnowledgeCacheImpl) SetKnowledge(ctx context.Context, knowledgeID string, knowledge *types.Knowledge, ttl time.Duration) error {
	if ttl == 0 {
		ttl = k.ttlConfig.KnowledgeInfo
	}
	
	key := k.buildKnowledgeKey(knowledgeID)
	
	data, err := k.marshal(knowledge)
	if err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to marshal knowledge: %v", err)
		return fmt.Errorf("failed to marshal knowledge: %w", err)
	}
	
	if err := k.cacheManager.Set(ctx, key, data, ttl); err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to cache knowledge: %v", err)
		return err
	}
	
	logger.Debugf(ctx, "[KnowledgeCache] Cached knowledge %s with TTL %v", knowledgeID, ttl)
	return nil
}

// GetChunk retrieves cached chunk info
func (k *KnowledgeCacheImpl) GetChunk(ctx context.Context, chunkID string) (*types.Chunk, error) {
	key := k.buildChunkKey(chunkID)
	
	data, err := k.cacheManager.Get(ctx, key)
	if err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to get chunk from cache: %v", err)
		return nil, err
	}
	
	if data == nil {
		// Cache miss
		logger.Debugf(ctx, "[KnowledgeCache] Cache miss for chunk: %s", chunkID)
		return nil, nil
	}
	
	var chunk types.Chunk
	if err := k.unmarshal(data, &chunk); err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to unmarshal chunk: %v", err)
		return nil, fmt.Errorf("failed to unmarshal chunk: %w", err)
	}
	
	logger.Debugf(ctx, "[KnowledgeCache] Retrieved chunk from cache: %s", chunkID)
	return &chunk, nil
}

// SetChunk caches chunk info
func (k *KnowledgeCacheImpl) SetChunk(ctx context.Context, chunkID string, chunk *types.Chunk, ttl time.Duration) error {
	if ttl == 0 {
		ttl = k.ttlConfig.ChunkInfo
	}
	
	key := k.buildChunkKey(chunkID)
	
	data, err := k.marshal(chunk)
	if err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to marshal chunk: %v", err)
		return fmt.Errorf("failed to marshal chunk: %w", err)
	}
	
	if err := k.cacheManager.Set(ctx, key, data, ttl); err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to cache chunk: %v", err)
		return err
	}
	
	logger.Debugf(ctx, "[KnowledgeCache] Cached chunk %s with TTL %v", chunkID, ttl)
	return nil
}

// InvalidateKnowledge removes knowledge from cache
func (k *KnowledgeCacheImpl) InvalidateKnowledge(ctx context.Context, knowledgeID string) error {
	key := k.buildKnowledgeKey(knowledgeID)
	
	if err := k.cacheManager.Delete(ctx, key); err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to invalidate knowledge cache: %v", err)
		return err
	}
	
	logger.Debugf(ctx, "[KnowledgeCache] Invalidated knowledge cache: %s", knowledgeID)
	return nil
}

// InvalidateChunk removes chunk from cache
func (k *KnowledgeCacheImpl) InvalidateChunk(ctx context.Context, chunkID string) error {
	key := k.buildChunkKey(chunkID)
	
	if err := k.cacheManager.Delete(ctx, key); err != nil {
		logger.Errorf(ctx, "[KnowledgeCache] Failed to invalidate chunk cache: %v", err)
		return err
	}
	
	logger.Debugf(ctx, "[KnowledgeCache] Invalidated chunk cache: %s", chunkID)
	return nil
}

// GenerateSearchQueryHash generates a hash for search query parameters
func (k *KnowledgeCacheImpl) GenerateSearchQueryHash(query string, kbID string, params types.SearchParams) string {
	return k.generateQueryHash(query, kbID, params.MatchCount, params.VectorThreshold, params.KeywordThreshold)
}

// Helper methods for serialization
func (k *KnowledgeCacheImpl) marshal(v interface{}) ([]byte, error) {
	if redisManager, ok := k.cacheManager.(*RedisCacheManager); ok {
		return redisManager.marshal(v)
	}
	// Fallback to basic JSON marshaling
	return k.cacheManager.(*RedisCacheManager).marshal(v)
}

func (k *KnowledgeCacheImpl) unmarshal(data []byte, v interface{}) error {
	if redisManager, ok := k.cacheManager.(*RedisCacheManager); ok {
		return redisManager.unmarshal(data, v)
	}
	// Fallback to basic JSON unmarshaling
	return k.cacheManager.(*RedisCacheManager).unmarshal(data, v)
}