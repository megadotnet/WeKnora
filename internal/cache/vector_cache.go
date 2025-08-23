package cache

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"math"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// VectorCacheImpl implements VectorCache using Redis
type VectorCacheImpl struct {
	cacheManager CacheManager
	ttlConfig    VectorCacheTTL
}

// VectorCacheTTL defines TTL for different types of vector cache
type VectorCacheTTL struct {
	VectorResults     time.Duration `yaml:"vector_results" json:"vector_results"`
	Embeddings        time.Duration `yaml:"embeddings" json:"embeddings"`
	SimilarityResults time.Duration `yaml:"similarity_results" json:"similarity_results"`
	QueryEmbeddings   time.Duration `yaml:"query_embeddings" json:"query_embeddings"`
}

// NewVectorCache creates a new vector cache instance
func NewVectorCache(cacheManager CacheManager, ttlConfig *VectorCacheTTL) VectorCache {
	if ttlConfig == nil {
		// Set default TTL values
		ttlConfig = &VectorCacheTTL{
			VectorResults:     15 * time.Minute,
			Embeddings:        24 * time.Hour,  // Embeddings are stable, cache longer
			SimilarityResults: 30 * time.Minute,
			QueryEmbeddings:   time.Hour,
		}
	}

	return &VectorCacheImpl{
		cacheManager: cacheManager,
		ttlConfig:    *ttlConfig,
	}
}

// generateVectorHash generates a consistent hash for vector data
func (v *VectorCacheImpl) generateVectorHash(embedding []float32, params ...interface{}) string {
	hasher := md5.New()
	
	// Hash the embedding vector (sample some values to avoid large hash input)
	for i := 0; i < len(embedding) && i < 100; i += max(1, len(embedding)/50) {
		hasher.Write([]byte(fmt.Sprintf("%.6f", embedding[i])))
	}
	
	// Include additional parameters in hash
	for _, param := range params {
		hasher.Write([]byte(fmt.Sprintf("%v", param)))
	}
	
	return hex.EncodeToString(hasher.Sum(nil))
}

// generateContentHash generates a hash for content to cache embeddings
func (v *VectorCacheImpl) generateContentHash(content string) string {
	hasher := md5.New()
	hasher.Write([]byte(content))
	return hex.EncodeToString(hasher.Sum(nil))
}

// buildVectorResultKey builds cache key for vector search results
func (v *VectorCacheImpl) buildVectorResultKey(vectorHash string) string {
	return fmt.Sprintf("vector_result:%s", vectorHash)
}

// buildEmbeddingKey builds cache key for embeddings
func (v *VectorCacheImpl) buildEmbeddingKey(contentHash string) string {
	return fmt.Sprintf("embedding:%s", contentHash)
}

// buildSimilarityKey builds cache key for similarity results
func (v *VectorCacheImpl) buildSimilarityKey(queryHash string) string {
	return fmt.Sprintf("similarity:%s", queryHash)
}

// buildQueryEmbeddingKey builds cache key for query embeddings
func (v *VectorCacheImpl) buildQueryEmbeddingKey(queryHash string) string {
	return fmt.Sprintf("query_emb:%s", queryHash)
}

// GetVectorResults retrieves cached vector search results
func (v *VectorCacheImpl) GetVectorResults(ctx context.Context, vectorHash string) ([]*types.RetrieveResult, error) {
	key := v.buildVectorResultKey(vectorHash)
	
	data, err := v.cacheManager.Get(ctx, key)
	if err != nil {
		logger.Errorf(ctx, "[VectorCache] Failed to get vector results from cache: %v", err)
		return nil, err
	}
	
	if data == nil {
		// Cache miss
		logger.Debugf(ctx, "[VectorCache] Cache miss for vector results: %s", vectorHash)
		return nil, nil
	}
	
	var results []*types.RetrieveResult
	if err := v.unmarshal(data, &results); err != nil {
		logger.Errorf(ctx, "[VectorCache] Failed to unmarshal vector results: %v", err)
		return nil, fmt.Errorf("failed to unmarshal vector results: %w", err)
	}
	
	logger.Debugf(ctx, "[VectorCache] Retrieved %d vector results from cache", len(results))
	return results, nil
}

// SetVectorResults caches vector search results
func (v *VectorCacheImpl) SetVectorResults(ctx context.Context, vectorHash string, results []*types.RetrieveResult, ttl time.Duration) error {
	if ttl == 0 {
		ttl = v.ttlConfig.VectorResults
	}
	
	key := v.buildVectorResultKey(vectorHash)
	
	data, err := v.marshal(results)
	if err != nil {
		logger.Errorf(ctx, "[VectorCache] Failed to marshal vector results: %v", err)
		return fmt.Errorf("failed to marshal vector results: %w", err)
	}
	
	if err := v.cacheManager.Set(ctx, key, data, ttl); err != nil {
		logger.Errorf(ctx, "[VectorCache] Failed to cache vector results: %v", err)
		return err
	}
	
	logger.Debugf(ctx, "[VectorCache] Cached %d vector results with TTL %v", len(results), ttl)
	return nil
}

// GetEmbedding retrieves cached embedding
func (v *VectorCacheImpl) GetEmbedding(ctx context.Context, contentHash string) ([]float32, error) {
	key := v.buildEmbeddingKey(contentHash)
	
	data, err := v.cacheManager.Get(ctx, key)
	if err != nil {
		logger.Errorf(ctx, "[VectorCache] Failed to get embedding from cache: %v", err)
		return nil, err
	}
	
	if data == nil {
		// Cache miss
		logger.Debugf(ctx, "[VectorCache] Cache miss for embedding: %s", contentHash)
		return nil, nil
	}
	
	var embedding []float32
	if err := v.unmarshal(data, &embedding); err != nil {
		logger.Errorf(ctx, "[VectorCache] Failed to unmarshal embedding: %v", err)
		return nil, fmt.Errorf("failed to unmarshal embedding: %w", err)
	}
	
	logger.Debugf(ctx, "[VectorCache] Retrieved embedding from cache, dimension: %d", len(embedding))
	return embedding, nil
}

// SetEmbedding caches embedding
func (v *VectorCacheImpl) SetEmbedding(ctx context.Context, contentHash string, embedding []float32, ttl time.Duration) error {
	if ttl == 0 {
		ttl = v.ttlConfig.Embeddings
	}
	
	key := v.buildEmbeddingKey(contentHash)
	
	data, err := v.marshal(embedding)
	if err != nil {
		logger.Errorf(ctx, "[VectorCache] Failed to marshal embedding: %v", err)
		return fmt.Errorf("failed to marshal embedding: %w", err)
	}
	
	if err := v.cacheManager.Set(ctx, key, data, ttl); err != nil {
		logger.Errorf(ctx, "[VectorCache] Failed to cache embedding: %v", err)
		return err
	}
	
	logger.Debugf(ctx, "[VectorCache] Cached embedding with dimension %d and TTL %v", len(embedding), ttl)
	return nil
}

// GetSimilarityResults retrieves cached similarity results
func (v *VectorCacheImpl) GetSimilarityResults(ctx context.Context, queryHash string) ([]*types.IndexWithScore, error) {
	key := v.buildSimilarityKey(queryHash)
	
	data, err := v.cacheManager.Get(ctx, key)
	if err != nil {
		logger.Errorf(ctx, "[VectorCache] Failed to get similarity results from cache: %v", err)
		return nil, err
	}
	
	if data == nil {
		// Cache miss
		logger.Debugf(ctx, "[VectorCache] Cache miss for similarity results: %s", queryHash)
		return nil, nil
	}
	
	var results []*types.IndexWithScore
	if err := v.unmarshal(data, &results); err != nil {
		logger.Errorf(ctx, "[VectorCache] Failed to unmarshal similarity results: %v", err)
		return nil, fmt.Errorf("failed to unmarshal similarity results: %w", err)
	}
	
	logger.Debugf(ctx, "[VectorCache] Retrieved %d similarity results from cache", len(results))
	return results, nil
}

// SetSimilarityResults caches similarity results
func (v *VectorCacheImpl) SetSimilarityResults(ctx context.Context, queryHash string, results []*types.IndexWithScore, ttl time.Duration) error {
	if ttl == 0 {
		ttl = v.ttlConfig.SimilarityResults
	}
	
	key := v.buildSimilarityKey(queryHash)
	
	data, err := v.marshal(results)
	if err != nil {
		logger.Errorf(ctx, "[VectorCache] Failed to marshal similarity results: %v", err)
		return fmt.Errorf("failed to marshal similarity results: %w", err)
	}
	
	if err := v.cacheManager.Set(ctx, key, data, ttl); err != nil {
		logger.Errorf(ctx, "[VectorCache] Failed to cache similarity results: %v", err)
		return err
	}
	
	logger.Debugf(ctx, "[VectorCache] Cached %d similarity results with TTL %v", len(results), ttl)
	return nil
}

// GetQueryEmbedding retrieves cached query embedding
func (v *VectorCacheImpl) GetQueryEmbedding(ctx context.Context, query string) ([]float32, error) {
	queryHash := v.generateContentHash(query)
	key := v.buildQueryEmbeddingKey(queryHash)
	
	data, err := v.cacheManager.Get(ctx, key)
	if err != nil {
		logger.Errorf(ctx, "[VectorCache] Failed to get query embedding from cache: %v", err)
		return nil, err
	}
	
	if data == nil {
		// Cache miss
		logger.Debugf(ctx, "[VectorCache] Cache miss for query embedding: %s", query[:min(50, len(query))])
		return nil, nil
	}
	
	var embedding []float32
	if err := v.unmarshal(data, &embedding); err != nil {
		logger.Errorf(ctx, "[VectorCache] Failed to unmarshal query embedding: %v", err)
		return nil, fmt.Errorf("failed to unmarshal query embedding: %w", err)
	}
	
	logger.Debugf(ctx, "[VectorCache] Retrieved query embedding from cache, dimension: %d", len(embedding))
	return embedding, nil
}

// SetQueryEmbedding caches query embedding
func (v *VectorCacheImpl) SetQueryEmbedding(ctx context.Context, query string, embedding []float32, ttl time.Duration) error {
	if ttl == 0 {
		ttl = v.ttlConfig.QueryEmbeddings
	}
	
	queryHash := v.generateContentHash(query)
	key := v.buildQueryEmbeddingKey(queryHash)
	
	data, err := v.marshal(embedding)
	if err != nil {
		logger.Errorf(ctx, "[VectorCache] Failed to marshal query embedding: %v", err)
		return fmt.Errorf("failed to marshal query embedding: %w", err)
	}
	
	if err := v.cacheManager.Set(ctx, key, data, ttl); err != nil {
		logger.Errorf(ctx, "[VectorCache] Failed to cache query embedding: %v", err)
		return err
	}
	
	logger.Debugf(ctx, "[VectorCache] Cached query embedding with dimension %d and TTL %v", len(embedding), ttl)
	return nil
}

// GenerateVectorHash generates a hash for vector search parameters
func (v *VectorCacheImpl) GenerateVectorHash(embedding []float32, kbIDs []string, topK int, threshold float64) string {
	return v.generateVectorHash(embedding, kbIDs, topK, threshold)
}

// GenerateContentHash generates a hash for content
func (v *VectorCacheImpl) GenerateContentHash(content string) string {
	return v.generateContentHash(content)
}

// CalculateEmbeddingSimilarity calculates cosine similarity between two embeddings
func (v *VectorCacheImpl) CalculateEmbeddingSimilarity(embedding1, embedding2 []float32) float64 {
	if len(embedding1) != len(embedding2) {
		return 0.0
	}
	
	var dotProduct, norm1, norm2 float64
	
	for i := 0; i < len(embedding1); i++ {
		dotProduct += float64(embedding1[i]) * float64(embedding2[i])
		norm1 += float64(embedding1[i]) * float64(embedding1[i])
		norm2 += float64(embedding2[i]) * float64(embedding2[i])
	}
	
	if norm1 == 0 || norm2 == 0 {
		return 0.0
	}
	
	return dotProduct / (math.Sqrt(norm1) * math.Sqrt(norm2))
}

// InvalidateVectorResults removes vector results from cache
func (v *VectorCacheImpl) InvalidateVectorResults(ctx context.Context, vectorHash string) error {
	key := v.buildVectorResultKey(vectorHash)
	
	if err := v.cacheManager.Delete(ctx, key); err != nil {
		logger.Errorf(ctx, "[VectorCache] Failed to invalidate vector results cache: %v", err)
		return err
	}
	
	logger.Debugf(ctx, "[VectorCache] Invalidated vector results cache: %s", vectorHash)
	return nil
}

// InvalidateEmbedding removes embedding from cache
func (v *VectorCacheImpl) InvalidateEmbedding(ctx context.Context, contentHash string) error {
	key := v.buildEmbeddingKey(contentHash)
	
	if err := v.cacheManager.Delete(ctx, key); err != nil {
		logger.Errorf(ctx, "[VectorCache] Failed to invalidate embedding cache: %v", err)
		return err
	}
	
	logger.Debugf(ctx, "[VectorCache] Invalidated embedding cache: %s", contentHash)
	return nil
}

// BatchInvalidateEmbeddings removes multiple embeddings from cache
func (v *VectorCacheImpl) BatchInvalidateEmbeddings(ctx context.Context, contentHashes []string) error {
	for _, contentHash := range contentHashes {
		if err := v.InvalidateEmbedding(ctx, contentHash); err != nil {
			logger.Errorf(ctx, "[VectorCache] Failed to invalidate embedding %s: %v", contentHash, err)
			// Continue with other invalidations
		}
	}
	
	logger.Debugf(ctx, "[VectorCache] Batch invalidated %d embeddings", len(contentHashes))
	return nil
}

// Helper methods for serialization
func (v *VectorCacheImpl) marshal(data interface{}) ([]byte, error) {
	if redisManager, ok := v.cacheManager.(*RedisCacheManager); ok {
		return redisManager.marshal(data)
	}
	// Fallback to basic JSON marshaling
	return v.cacheManager.(*RedisCacheManager).marshal(data)
}

func (v *VectorCacheImpl) unmarshal(data []byte, result interface{}) error {
	if redisManager, ok := v.cacheManager.(*RedisCacheManager); ok {
		return redisManager.unmarshal(data, result)
	}
	// Fallback to basic JSON unmarshaling
	return v.cacheManager.(*RedisCacheManager).unmarshal(data, result)
}

// Helper function for min calculation
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Helper function for max calculation
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}