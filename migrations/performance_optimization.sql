-- WeKnora Database Performance Optimization Script
-- This script adds missing indexes and optimizes database queries based on SAAM architecture analysis

-- =============================================================================
-- ANALYSIS SUMMARY
-- =============================================================================
-- Analyzed current query patterns and identified missing indexes for:
-- 1. Multi-tenant queries (tenant_id filtering)
-- 2. Time-based queries (created_at, updated_at ordering)
-- 3. Status filtering queries
-- 4. Foreign key relationships
-- 5. Full-text search optimization
-- 6. Vector similarity search optimization

-- =============================================================================
-- TENANT TABLE OPTIMIZATIONS
-- =============================================================================

-- Optimize tenant API key lookups (frequently used for authentication)
CREATE INDEX IF NOT EXISTS idx_tenants_api_key_active ON tenants(api_key) WHERE status = 'active';

-- Optimize storage quota queries for resource management
CREATE INDEX IF NOT EXISTS idx_tenants_storage_usage ON tenants(storage_used, storage_quota) WHERE status = 'active';

-- =============================================================================
-- MODELS TABLE OPTIMIZATIONS
-- =============================================================================

-- Optimize multi-tenant model queries with type and source filtering
CREATE INDEX IF NOT EXISTS idx_models_tenant_type_status ON models(tenant_id, type, status) WHERE deleted_at IS NULL;

-- Optimize default model queries
CREATE INDEX IF NOT EXISTS idx_models_default_active ON models(tenant_id, type, is_default) WHERE status = 'active' AND deleted_at IS NULL;

-- Composite index for complex model filtering queries
CREATE INDEX IF NOT EXISTS idx_models_complex_query ON models(tenant_id, source, type, status) WHERE deleted_at IS NULL;

-- =============================================================================
-- KNOWLEDGE_BASES TABLE OPTIMIZATIONS
-- =============================================================================

-- Optimize knowledge base listing by tenant with status filtering
CREATE INDEX IF NOT EXISTS idx_kb_tenant_status ON knowledge_bases(tenant_id, deleted_at) WHERE deleted_at IS NULL;

-- Optimize embedding model lookups
CREATE INDEX IF NOT EXISTS idx_kb_embedding_model ON knowledge_bases(embedding_model_id) WHERE deleted_at IS NULL;

-- Optimize time-based queries for knowledge base management
CREATE INDEX IF NOT EXISTS idx_kb_created_updated ON knowledge_bases(tenant_id, created_at DESC, updated_at DESC) WHERE deleted_at IS NULL;

-- =============================================================================
-- KNOWLEDGES TABLE OPTIMIZATIONS
-- =============================================================================

-- Optimize knowledge listing by knowledge base and status
CREATE INDEX IF NOT EXISTS idx_knowledge_kb_status ON knowledges(knowledge_base_id, parse_status, enable_status) WHERE deleted_at IS NULL;

-- Optimize file hash lookups for duplicate detection
CREATE INDEX IF NOT EXISTS idx_knowledge_file_hash ON knowledges(tenant_id, file_hash) WHERE deleted_at IS NULL;

-- Optimize knowledge queries by type and source
CREATE INDEX IF NOT EXISTS idx_knowledge_type_source ON knowledges(tenant_id, type, source) WHERE deleted_at IS NULL;

-- Optimize processing status queries
CREATE INDEX IF NOT EXISTS idx_knowledge_processing ON knowledges(tenant_id, parse_status, processed_at) WHERE deleted_at IS NULL;

-- =============================================================================
-- SESSIONS TABLE OPTIMIZATIONS
-- =============================================================================

-- Optimize session listing by tenant with time ordering
CREATE INDEX IF NOT EXISTS idx_sessions_tenant_time ON sessions(tenant_id, created_at DESC) WHERE deleted_at IS NULL;

-- Optimize knowledge base session queries
CREATE INDEX IF NOT EXISTS idx_sessions_kb_tenant ON sessions(knowledge_base_id, tenant_id) WHERE deleted_at IS NULL;

-- =============================================================================
-- MESSAGES TABLE OPTIMIZATIONS
-- =============================================================================

-- Optimize message queries by session with role and time ordering
CREATE INDEX IF NOT EXISTS idx_messages_session_role_time ON messages(session_id, role, created_at ASC) WHERE deleted_at IS NULL;

-- Optimize recent message queries
CREATE INDEX IF NOT EXISTS idx_messages_session_recent ON messages(session_id, created_at DESC) WHERE deleted_at IS NULL AND is_completed = true;

-- Optimize request ID lookups
CREATE INDEX IF NOT EXISTS idx_messages_request_id ON messages(session_id, request_id) WHERE deleted_at IS NULL;

-- =============================================================================
-- CHUNKS TABLE OPTIMIZATIONS
-- =============================================================================

-- Optimize chunk queries by knowledge with type filtering
CREATE INDEX IF NOT EXISTS idx_chunks_knowledge_type ON chunks(tenant_id, knowledge_id, chunk_type, chunk_index ASC) WHERE deleted_at IS NULL;

-- Optimize parent-child chunk relationships
CREATE INDEX IF NOT EXISTS idx_chunks_parent_child ON chunks(parent_chunk_id, chunk_index ASC) WHERE deleted_at IS NULL AND parent_chunk_id IS NOT NULL;

-- Optimize enabled chunks queries
CREATE INDEX IF NOT EXISTS idx_chunks_enabled ON chunks(tenant_id, knowledge_base_id, is_enabled) WHERE deleted_at IS NULL;

-- Optimize chunk content search (for non-vector search)
CREATE INDEX IF NOT EXISTS idx_chunks_content_search ON chunks USING gin(to_tsvector('english', content)) WHERE deleted_at IS NULL AND chunk_type = 'text';

-- =============================================================================
-- EMBEDDINGS TABLE OPTIMIZATIONS (PostgreSQL with pgvector)
-- =============================================================================

-- Optimize knowledge base vector queries
CREATE INDEX IF NOT EXISTS idx_embeddings_kb_dimension ON embeddings(knowledge_base_id, dimension) WHERE knowledge_base_id IS NOT NULL;

-- Optimize chunk and knowledge filtering for vector search
CREATE INDEX IF NOT EXISTS idx_embeddings_knowledge_chunk ON embeddings(knowledge_id, chunk_id) WHERE knowledge_id IS NOT NULL;

-- Optimize source type filtering
CREATE INDEX IF NOT EXISTS idx_embeddings_source_type ON embeddings(source_type, knowledge_base_id);

-- =============================================================================
-- POSTGRESQL-SPECIFIC OPTIMIZATIONS
-- =============================================================================

-- Enable parallel query processing for large datasets
-- ALTER SYSTEM SET max_parallel_workers_per_gather = 4;
-- ALTER SYSTEM SET max_parallel_workers = 8;

-- Optimize work memory for complex queries
-- ALTER SYSTEM SET work_mem = '256MB';

-- Optimize shared buffers for vector operations
-- ALTER SYSTEM SET shared_buffers = '1GB';

-- Enable JIT compilation for complex queries
-- ALTER SYSTEM SET jit = on;

-- =============================================================================
-- MYSQL-SPECIFIC OPTIMIZATIONS (if using MySQL)
-- =============================================================================

-- Optimize InnoDB buffer pool for better performance
-- SET GLOBAL innodb_buffer_pool_size = 1073741824; -- 1GB

-- Enable query cache for repeated queries
-- SET GLOBAL query_cache_type = ON;
-- SET GLOBAL query_cache_size = 268435456; -- 256MB

-- =============================================================================
-- MAINTENANCE COMMANDS
-- =============================================================================

-- PostgreSQL maintenance commands (run periodically)
-- VACUUM ANALYZE tenants;
-- VACUUM ANALYZE models;
-- VACUUM ANALYZE knowledge_bases;
-- VACUUM ANALYZE knowledges;
-- VACUUM ANALYZE sessions;
-- VACUUM ANALYZE messages;
-- VACUUM ANALYZE chunks;
-- VACUUM ANALYZE embeddings;

-- Reindex vector indexes periodically for optimal performance
-- REINDEX INDEX CONCURRENTLY embeddings_unique_source;
-- REINDEX INDEX CONCURRENTLY embeddings_search_idx;

-- =============================================================================
-- QUERY OPTIMIZATION VIEWS (for common queries)
-- =============================================================================

-- View for active knowledge bases with their statistics
CREATE OR REPLACE VIEW v_active_knowledge_bases AS
SELECT 
    kb.id,
    kb.name,
    kb.tenant_id,
    kb.created_at,
    COUNT(k.id) as knowledge_count,
    COUNT(c.id) as chunk_count,
    SUM(k.storage_size) as total_storage_size
FROM knowledge_bases kb
LEFT JOIN knowledges k ON kb.id = k.knowledge_base_id AND k.deleted_at IS NULL
LEFT JOIN chunks c ON kb.id = c.knowledge_base_id AND c.deleted_at IS NULL
WHERE kb.deleted_at IS NULL
GROUP BY kb.id, kb.name, kb.tenant_id, kb.created_at;

-- View for session statistics
CREATE OR REPLACE VIEW v_session_stats AS
SELECT 
    s.id,
    s.tenant_id,
    s.knowledge_base_id,
    s.created_at,
    COUNT(m.id) as message_count,
    MAX(m.created_at) as last_message_at
FROM sessions s
LEFT JOIN messages m ON s.id = m.session_id AND m.deleted_at IS NULL
WHERE s.deleted_at IS NULL
GROUP BY s.id, s.tenant_id, s.knowledge_base_id, s.created_at;

-- =============================================================================
-- PERFORMANCE MONITORING QUERIES
-- =============================================================================

-- Query to identify slow queries (PostgreSQL)
-- SELECT query, calls, total_time, mean_time, rows 
-- FROM pg_stat_statements 
-- WHERE mean_time > 1000 
-- ORDER BY mean_time DESC LIMIT 10;

-- Query to check index usage (PostgreSQL)
-- SELECT schemaname, tablename, indexname, idx_scan, idx_tup_read, idx_tup_fetch
-- FROM pg_stat_user_indexes
-- WHERE idx_scan = 0
-- ORDER BY schemaname, tablename;

-- =============================================================================
-- NOTES AND RECOMMENDATIONS
-- =============================================================================

-- 1. Monitor query performance regularly using database-specific tools
-- 2. Consider partitioning large tables (embeddings, chunks) by tenant_id or date
-- 3. Implement read replicas for read-heavy workloads
-- 4. Use connection pooling to manage database connections efficiently
-- 5. Regular maintenance: VACUUM, ANALYZE, and index rebuilding
-- 6. Consider materialized views for complex analytical queries
-- 7. Monitor and tune database configuration parameters based on workload
-- 8. Implement database monitoring with tools like pg_stat_statements