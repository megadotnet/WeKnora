package retriever

import (
	"context"
	"fmt"

	"github.com/Tencent/WeKnora/internal/cache"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// CachedRetrieveEngineService wraps a retrieval engine with caching capabilities
type CachedRetrieveEngineService struct {
	underlying   interfaces.RetrieveEngineService
	cacheFactory *cache.CacheFactory
}

// NewCachedRetrieveEngine creates a new cached retrieval engine wrapper
func NewCachedRetrieveEngine(
	underlying interfaces.RetrieveEngineService,
	cacheFactory *cache.CacheFactory,
) interfaces.RetrieveEngineService {
	if cacheFactory == nil {
		// If no cache factory, return the underlying engine directly
		return underlying
	}
	
	return &CachedRetrieveEngineService{
		underlying:   underlying,
		cacheFactory: cacheFactory,
	}
}

// EngineType returns the type of the underlying retrieval engine
func (c *CachedRetrieveEngineService) EngineType() types.RetrieverEngineType {
	return c.underlying.EngineType()
}

// Retrieve performs retrieval with caching support
func (c *CachedRetrieveEngineService) Retrieve(ctx context.Context, params types.RetrieveParams) ([]*types.RetrieveResult, error) {
	// Only cache vector retrieval results as they are more expensive to compute
	if params.RetrieverType == types.VectorRetrieverType && len(params.Embedding) > 0 {
		vectorCache := c.cacheFactory.GetVectorCache()
		
		// Generate cache key based on vector and parameters
		vectorHash := vectorCache.(*cache.VectorCacheImpl).GenerateVectorHash(
			params.Embedding, 
			params.KnowledgeBaseIDs, 
			params.TopK, 
			params.Threshold,
		)
		
		// Try to get from cache first
		if cachedResults, err := vectorCache.GetVectorResults(ctx, vectorHash); err == nil && cachedResults != nil {
			logger.Debugf(ctx, "[CachedRetriever] Vector search results retrieved from cache, hash: %s", vectorHash)
			return cachedResults, nil
		} else if err != nil {
			logger.Warnf(ctx, "[CachedRetriever] Failed to get vector results from cache: %v", err)
		}
		
		// Cache miss, retrieve from underlying engine
		results, err := c.underlying.Retrieve(ctx, params)
		if err != nil {
			return nil, err
		}
		
		// Cache the results if successful
		if results != nil && len(results) > 0 {
			if cacheErr := vectorCache.SetVectorResults(ctx, vectorHash, results, 0); cacheErr != nil {
				logger.Warnf(ctx, "[CachedRetriever] Failed to cache vector results: %v", cacheErr)
			} else {
				logger.Debugf(ctx, "[CachedRetriever] Vector search results cached, hash: %s", vectorHash)
			}
		}
		
		return results, nil
	}
	
	// For non-vector retrieval or when caching is not applicable, use underlying engine directly
	return c.underlying.Retrieve(ctx, params)
}

// Index creates embeddings with caching support
func (c *CachedRetrieveEngineService) Index(ctx context.Context, embedder embedding.Embedder, indexInfo *types.IndexInfo, retrieverTypes []types.RetrieverType) error {
	// Check if we need vector embeddings
	needsEmbedding := false
	for _, retrieverType := range retrieverTypes {
		if retrieverType == types.VectorRetrieverType {
			needsEmbedding = true
			break
		}
	}
	
	if needsEmbedding {
		vectorCache := c.cacheFactory.GetVectorCache()
		contentHash := vectorCache.(*cache.VectorCacheImpl).GenerateContentHash(indexInfo.Content)
		
		// Try to get cached embedding first
		if cachedEmbedding, err := vectorCache.GetEmbedding(ctx, contentHash); err == nil && cachedEmbedding != nil {
			logger.Debugf(ctx, "[CachedRetriever] Embedding retrieved from cache for content hash: %s", contentHash)
			
			// Create a custom embedder that returns cached embedding
			cachedEmbedder := &CachedEmbedder{
				underlying:      embedder,
				cachedEmbedding: cachedEmbedding,
				content:         indexInfo.Content,
			}
			
			return c.underlying.Index(ctx, cachedEmbedder, indexInfo, retrieverTypes)
		}
		
		// Use original embedder and cache the result
		originalIndex := c.underlying.Index(ctx, embedder, indexInfo, retrieverTypes)
		if originalIndex == nil {
			// Try to cache the embedding after successful indexing
			if embedding, err := embedder.Embed(ctx, indexInfo.Content); err == nil {
				if cacheErr := vectorCache.SetEmbedding(ctx, contentHash, embedding, 0); cacheErr != nil {
					logger.Warnf(ctx, "[CachedRetriever] Failed to cache embedding: %v", cacheErr)
				} else {
					logger.Debugf(ctx, "[CachedRetriever] Embedding cached for content hash: %s", contentHash)
				}
			}
		}
		
		return originalIndex
	}
	
	// For non-vector indexing, use underlying engine directly
	return c.underlying.Index(ctx, embedder, indexInfo, retrieverTypes)
}

// BatchIndex creates embeddings for multiple content items with caching support
func (c *CachedRetrieveEngineService) BatchIndex(ctx context.Context, embedder embedding.Embedder, indexInfoList []*types.IndexInfo, retrieverTypes []types.RetrieverType) error {
	// Check if we need vector embeddings
	needsEmbedding := false
	for _, retrieverType := range retrieverTypes {
		if retrieverType == types.VectorRetrieverType {
			needsEmbedding = true
			break
		}
	}
	
	if !needsEmbedding {
		// For non-vector indexing, use underlying engine directly
		return c.underlying.BatchIndex(ctx, embedder, indexInfoList, retrieverTypes)
	}
	
	vectorCache := c.cacheFactory.GetVectorCache()
	
	// Check which embeddings are already cached
	cachedEmbeddings := make(map[string][]float32)
	uncachedInfos := make([]*types.IndexInfo, 0)
	
	for _, indexInfo := range indexInfoList {
		contentHash := vectorCache.(*cache.VectorCacheImpl).GenerateContentHash(indexInfo.Content)
		if cachedEmbedding, err := vectorCache.GetEmbedding(ctx, contentHash); err == nil && cachedEmbedding != nil {
			cachedEmbeddings[indexInfo.Content] = cachedEmbedding
			logger.Debugf(ctx, "[CachedRetriever] Embedding retrieved from cache for batch item: %s", contentHash)
		} else {
			uncachedInfos = append(uncachedInfos, indexInfo)
		}
	}
	
	logger.Infof(ctx, "[CachedRetriever] Batch indexing: %d cached, %d uncached embeddings", 
		len(cachedEmbeddings), len(uncachedInfos))
	
	// If all embeddings are cached, create a batch cached embedder
	if len(uncachedInfos) == 0 {
		batchCachedEmbedder := &BatchCachedEmbedder{
			underlying:        embedder,
			cachedEmbeddings:  cachedEmbeddings,
		}
		return c.underlying.BatchIndex(ctx, batchCachedEmbedder, indexInfoList, retrieverTypes)
	}
	
	// Mixed case: some cached, some not - delegate to underlying and cache new embeddings
	err := c.underlying.BatchIndex(ctx, embedder, indexInfoList, retrieverTypes)
	if err == nil && len(uncachedInfos) > 0 {
		// Cache the newly generated embeddings
		contents := make([]string, len(uncachedInfos))
		for i, info := range uncachedInfos {
			contents[i] = info.Content
		}
		
		// Generate embeddings for uncached content
		if embeddings, embErr := embedder.BatchEmbedWithPool(ctx, embedder, contents); embErr == nil {
			// Cache each embedding
			for i, embedding := range embeddings {
				contentHash := vectorCache.(*cache.VectorCacheImpl).GenerateContentHash(contents[i])
				if cacheErr := vectorCache.SetEmbedding(ctx, contentHash, embedding, 0); cacheErr != nil {
					logger.Warnf(ctx, "[CachedRetriever] Failed to cache batch embedding: %v", cacheErr)
				}
			}
			logger.Infof(ctx, "[CachedRetriever] Cached %d new embeddings from batch", len(embeddings))
		}
	}
	
	return err
}

// DeleteByChunkIDList deletes vectors and invalidates related cache entries
func (c *CachedRetrieveEngineService) DeleteByChunkIDList(ctx context.Context, indexIDList []string, dimension int) error {
	err := c.underlying.DeleteByChunkIDList(ctx, indexIDList, dimension)
	
	// Note: We could implement cache invalidation for specific chunks here,
	// but since we don't have a direct mapping from chunk ID to content hash,
	// we'll let cache entries expire naturally or implement cache invalidation
	// at a higher level when we know the content
	
	return err
}

// DeleteByKnowledgeIDList deletes vectors and invalidates related cache entries
func (c *CachedRetrieveEngineService) DeleteByKnowledgeIDList(ctx context.Context, knowledgeIDList []string, dimension int) error {
	err := c.underlying.DeleteByKnowledgeIDList(ctx, knowledgeIDList, dimension)
	
	// Note: Similar to DeleteByChunkIDList, we could implement more sophisticated
	// cache invalidation here if needed
	
	return err
}

// Support returns the retriever types supported by the underlying engine
func (c *CachedRetrieveEngineService) Support() []types.RetrieverType {
	return c.underlying.Support()
}

// EstimateStorageSize delegates to the underlying engine
func (c *CachedRetrieveEngineService) EstimateStorageSize(ctx context.Context, embedder embedding.Embedder, indexInfoList []*types.IndexInfo, retrieverTypes []types.RetrieverType) int64 {
	return c.underlying.EstimateStorageSize(ctx, embedder, indexInfoList, retrieverTypes)
}

// CopyIndices delegates to the underlying engine
func (c *CachedRetrieveEngineService) CopyIndices(ctx context.Context, sourceKnowledgeBaseID string, sourceToTargetKBIDMap map[string]string, sourceToTargetChunkIDMap map[string]string, targetKnowledgeBaseID string, dimension int) error {
	return c.underlying.CopyIndices(ctx, sourceKnowledgeBaseID, sourceToTargetKBIDMap, sourceToTargetChunkIDMap, targetKnowledgeBaseID, dimension)
}

// CachedEmbedder wraps an embedder to return cached embeddings for specific content
type CachedEmbedder struct {
	underlying      embedding.Embedder
	cachedEmbedding []float32
	content         string
}

// Embed returns cached embedding for the specific content, otherwise delegates to underlying embedder
func (c *CachedEmbedder) Embed(ctx context.Context, content string) ([]float32, error) {
	if content == c.content {
		return c.cachedEmbedding, nil
	}
	return c.underlying.Embed(ctx, content)
}

// BatchEmbed delegates to underlying embedder
func (c *CachedEmbedder) BatchEmbed(ctx context.Context, contents []string) ([][]float32, error) {
	return c.underlying.BatchEmbed(ctx, contents)
}

// BatchEmbedWithPool delegates to underlying embedder
func (c *CachedEmbedder) BatchEmbedWithPool(ctx context.Context, embedder embedding.Embedder, contents []string) ([][]float32, error) {
	return c.underlying.BatchEmbedWithPool(ctx, embedder, contents)
}

// GetDimensions returns the embedding dimensions from underlying embedder
func (c *CachedEmbedder) GetDimensions() int {
	return c.underlying.GetDimensions()
}

// GetModelName returns the model name from underlying embedder
func (c *CachedEmbedder) GetModelName() string {
	return c.underlying.GetModelName()
}

// GetModelID returns the model ID from underlying embedder
func (c *CachedEmbedder) GetModelID() string {
	return c.underlying.GetModelID()
}

// BatchCachedEmbedder wraps an embedder to return cached embeddings for multiple content items
type BatchCachedEmbedder struct {
	underlying       embedding.Embedder
	cachedEmbeddings map[string][]float32
}

// Embed returns cached embedding if available, otherwise delegates to underlying embedder
func (b *BatchCachedEmbedder) Embed(ctx context.Context, content string) ([]float32, error) {
	if cached, ok := b.cachedEmbeddings[content]; ok {
		return cached, nil
	}
	return b.underlying.Embed(ctx, content)
}

// BatchEmbed returns cached embeddings where available, generates missing ones
func (b *BatchCachedEmbedder) BatchEmbed(ctx context.Context, contents []string) ([][]float32, error) {
	results := make([][]float32, len(contents))
	uncachedIndices := make([]int, 0)
	uncachedContents := make([]string, 0)
	
	// Collect cached embeddings and identify uncached content
	for i, content := range contents {
		if cached, ok := b.cachedEmbeddings[content]; ok {
			results[i] = cached
		} else {
			uncachedIndices = append(uncachedIndices, i)
			uncachedContents = append(uncachedContents, content)
		}
	}
	
	// Generate embeddings for uncached content
	if len(uncachedContents) > 0 {
		uncachedEmbeddings, err := b.underlying.BatchEmbed(ctx, uncachedContents)
		if err != nil {
			return nil, fmt.Errorf("failed to generate embeddings for uncached content: %w", err)
		}
		
		// Fill in the uncached results
		for i, embedding := range uncachedEmbeddings {
			results[uncachedIndices[i]] = embedding
		}
	}
	
	return results, nil
}

// BatchEmbedWithPool delegates to underlying embedder for uncached content
func (b *BatchCachedEmbedder) BatchEmbedWithPool(ctx context.Context, embedder embedding.Embedder, contents []string) ([][]float32, error) {
	return b.BatchEmbed(ctx, contents)
}

// GetDimensions returns the embedding dimensions from underlying embedder
func (b *BatchCachedEmbedder) GetDimensions() int {
	return b.underlying.GetDimensions()
}

// GetModelName returns the model name from underlying embedder
func (b *BatchCachedEmbedder) GetModelName() string {
	return b.underlying.GetModelName()
}

// GetModelID returns the model ID from underlying embedder
func (b *BatchCachedEmbedder) GetModelID() string {
	return b.underlying.GetModelID()
}