package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// DBAnalyzer provides database performance analysis capabilities
type DBAnalyzer struct {
	db *sql.DB
}

// QueryStat represents database query statistics
type QueryStat struct {
	Query      string  `json:"query"`
	Calls      int64   `json:"calls"`
	TotalTime  float64 `json:"total_time"`
	MeanTime   float64 `json:"mean_time"`
	MaxTime    float64 `json:"max_time"`
	Rows       int64   `json:"rows"`
}

// IndexStat represents index usage statistics
type IndexStat struct {
	SchemaName string `json:"schema_name"`
	TableName  string `json:"table_name"`
	IndexName  string `json:"index_name"`
	IndexScans int64  `json:"index_scans"`
	TupRead    int64  `json:"tup_read"`
	TupFetch   int64  `json:"tup_fetch"`
	IndexSize  int64  `json:"index_size"`
}

// TableStat represents table statistics
type TableStat struct {
	SchemaName     string    `json:"schema_name"`
	TableName      string    `json:"table_name"`
	RowCount       int64     `json:"row_count"`
	TableSize      int64     `json:"table_size"`
	IndexSize      int64     `json:"index_size"`
	SeqScans       int64     `json:"seq_scans"`
	IndexScans     int64     `json:"index_scans"`
	InsertCount    int64     `json:"insert_count"`
	UpdateCount    int64     `json:"update_count"`
	DeleteCount    int64     `json:"delete_count"`
	LastAutoVacuum *time.Time `json:"last_auto_vacuum"`
	LastAnalyze    *time.Time `json:"last_analyze"`
}

// NewDBAnalyzer creates a new database analyzer
func NewDBAnalyzer() (*DBAnalyzer, error) {
	dbHost := getEnvDefault("DB_HOST", "localhost")
	dbPort := getEnvDefault("DB_PORT", "5432")
	dbUser := getEnvDefault("DB_USER", "postgres")
	dbPassword := os.Getenv("DB_PASSWORD")
	dbName := getEnvDefault("DB_NAME", "weknora")

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DBAnalyzer{db: db}, nil
}

// AnalyzeSlowQueries identifies slow queries that need optimization
func (d *DBAnalyzer) AnalyzeSlowQueries(ctx context.Context, minMeanTime float64) ([]QueryStat, error) {
	query := `
		SELECT 
			query,
			calls,
			total_exec_time as total_time,
			mean_exec_time as mean_time,
			max_exec_time as max_time,
			rows
		FROM pg_stat_statements 
		WHERE mean_exec_time > $1 
		ORDER BY mean_exec_time DESC 
		LIMIT 20
	`

	rows, err := d.db.QueryContext(ctx, query, minMeanTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query slow queries: %w", err)
	}
	defer rows.Close()

	var stats []QueryStat
	for rows.Next() {
		var stat QueryStat
		err := rows.Scan(&stat.Query, &stat.Calls, &stat.TotalTime, &stat.MeanTime, &stat.MaxTime, &stat.Rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan query stat: %w", err)
		}
		stats = append(stats, stat)
	}

	return stats, rows.Err()
}

// AnalyzeUnusedIndexes identifies indexes that are not being used
func (d *DBAnalyzer) AnalyzeUnusedIndexes(ctx context.Context) ([]IndexStat, error) {
	query := `
		SELECT 
			schemaname,
			tablename,
			indexname,
			idx_scan,
			idx_tup_read,
			idx_tup_fetch,
			pg_relation_size(indexrelid) as index_size
		FROM pg_stat_user_indexes
		WHERE idx_scan = 0
		AND schemaname = 'public'
		ORDER BY pg_relation_size(indexrelid) DESC
	`

	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query unused indexes: %w", err)
	}
	defer rows.Close()

	var stats []IndexStat
	for rows.Next() {
		var stat IndexStat
		err := rows.Scan(&stat.SchemaName, &stat.TableName, &stat.IndexName, 
			&stat.IndexScans, &stat.TupRead, &stat.TupFetch, &stat.IndexSize)
		if err != nil {
			return nil, fmt.Errorf("failed to scan index stat: %w", err)
		}
		stats = append(stats, stat)
	}

	return stats, rows.Err()
}

// AnalyzeTableStats provides comprehensive table statistics
func (d *DBAnalyzer) AnalyzeTableStats(ctx context.Context) ([]TableStat, error) {
	query := `
		SELECT 
			schemaname,
			tablename,
			n_tup_ins as insert_count,
			n_tup_upd as update_count,
			n_tup_del as delete_count,
			seq_scan as seq_scans,
			idx_scan as index_scans,
			pg_relation_size(relid) as table_size,
			last_autovacuum,
			last_autoanalyze
		FROM pg_stat_user_tables
		WHERE schemaname = 'public'
		ORDER BY pg_relation_size(relid) DESC
	`

	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query table stats: %w", err)
	}
	defer rows.Close()

	var stats []TableStat
	for rows.Next() {
		var stat TableStat
		var lastVacuum, lastAnalyze sql.NullTime
		
		err := rows.Scan(&stat.SchemaName, &stat.TableName, &stat.InsertCount,
			&stat.UpdateCount, &stat.DeleteCount, &stat.SeqScans, &stat.IndexScans,
			&stat.TableSize, &lastVacuum, &lastAnalyze)
		if err != nil {
			return nil, fmt.Errorf("failed to scan table stat: %w", err)
		}

		if lastVacuum.Valid {
			stat.LastAutoVacuum = &lastVacuum.Time
		}
		if lastAnalyze.Valid {
			stat.LastAnalyze = &lastAnalyze.Time
		}

		// Get row count for each table
		countQuery := fmt.Sprintf("SELECT reltuples::bigint FROM pg_class WHERE relname = '%s'", stat.TableName)
		err = d.db.QueryRowContext(ctx, countQuery).Scan(&stat.RowCount)
		if err != nil {
			log.Printf("Warning: Could not get row count for table %s: %v", stat.TableName, err)
		}

		stats = append(stats, stat)
	}

	return stats, rows.Err()
}

// CheckMissingIndexes analyzes queries to suggest missing indexes
func (d *DBAnalyzer) CheckMissingIndexes(ctx context.Context) ([]string, error) {
	var suggestions []string

	// Check for frequently scanned tables without appropriate indexes
	query := `
		SELECT 
			schemaname,
			tablename,
			seq_scan,
			seq_tup_read,
			idx_scan,
			n_tup_ins + n_tup_upd + n_tup_del as modifications
		FROM pg_stat_user_tables
		WHERE schemaname = 'public'
		AND seq_scan > 1000
		AND seq_scan > idx_scan
		ORDER BY seq_scan DESC
	`

	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query table scan stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var schemaName, tableName string
		var seqScan, seqTupRead, idxScan, modifications int64
		
		err := rows.Scan(&schemaName, &tableName, &seqScan, &seqTupRead, &idxScan, &modifications)
		if err != nil {
			continue
		}

		if seqScan > idxScan*2 {
			suggestion := fmt.Sprintf("Table '%s' has high sequential scan ratio (%d seq vs %d idx). Consider adding indexes on frequently queried columns.",
				tableName, seqScan, idxScan)
			suggestions = append(suggestions, suggestion)
		}
	}

	return suggestions, rows.Err()
}

// AnalyzeVectorPerformance analyzes vector search performance
func (d *DBAnalyzer) AnalyzeVectorPerformance(ctx context.Context) (map[string]interface{}, error) {
	results := make(map[string]interface{})

	// Check embeddings table statistics
	var embeddingStats struct {
		RowCount     int64
		TableSize    int64
		IndexSize    int64
		AvgDimension float64
	}

	query := `
		SELECT 
			COUNT(*) as row_count,
			pg_total_relation_size('embeddings') as table_size,
			COALESCE(AVG(dimension), 0) as avg_dimension
		FROM embeddings
	`

	err := d.db.QueryRowContext(ctx, query).Scan(&embeddingStats.RowCount, &embeddingStats.TableSize, &embeddingStats.AvgDimension)
	if err != nil {
		return nil, fmt.Errorf("failed to get embeddings stats: %w", err)
	}

	results["embeddings_stats"] = embeddingStats

	// Check HNSW index effectiveness
	hnswQuery := `
		SELECT 
			indexname,
			idx_scan,
			idx_tup_read,
			idx_tup_fetch
		FROM pg_stat_user_indexes 
		WHERE indexname LIKE '%hnsw%'
		OR indexname LIKE '%embedding%'
	`

	rows, err := d.db.QueryContext(ctx, hnswQuery)
	if err != nil {
		return results, fmt.Errorf("failed to query HNSW index stats: %w", err)
	}
	defer rows.Close()

	var hnswIndexes []map[string]interface{}
	for rows.Next() {
		var indexName string
		var idxScan, idxTupRead, idxTupFetch int64
		
		err := rows.Scan(&indexName, &idxScan, &idxTupRead, &idxTupFetch)
		if err != nil {
			continue
		}

		hnswIndexes = append(hnswIndexes, map[string]interface{}{
			"index_name":    indexName,
			"scans":         idxScan,
			"tuples_read":   idxTupRead,
			"tuples_fetch":  idxTupFetch,
		})
	}

	results["hnsw_indexes"] = hnswIndexes
	return results, nil
}

// GenerateOptimizationReport creates a comprehensive optimization report
func (d *DBAnalyzer) GenerateOptimizationReport(ctx context.Context) error {
	fmt.Println("=== WeKnora Database Performance Analysis Report ===")
	fmt.Println("Generated at:", time.Now().Format(time.RFC3339))
	fmt.Println()

	// Analyze slow queries
	fmt.Println("1. SLOW QUERIES ANALYSIS")
	fmt.Println("========================")
	slowQueries, err := d.AnalyzeSlowQueries(ctx, 100.0) // queries taking more than 100ms on average
	if err != nil {
		fmt.Printf("Error analyzing slow queries: %v\n", err)
	} else {
		if len(slowQueries) == 0 {
			fmt.Println("✅ No slow queries found (mean time > 100ms)")
		} else {
			for i, query := range slowQueries {
				fmt.Printf("Query #%d:\n", i+1)
				fmt.Printf("  Mean Time: %.2f ms\n", query.MeanTime)
				fmt.Printf("  Total Calls: %d\n", query.Calls)
				fmt.Printf("  Query: %s\n", truncateString(query.Query, 100))
				fmt.Println()
			}
		}
	}

	// Analyze unused indexes
	fmt.Println("2. UNUSED INDEXES ANALYSIS")
	fmt.Println("==========================")
	unusedIndexes, err := d.AnalyzeUnusedIndexes(ctx)
	if err != nil {
		fmt.Printf("Error analyzing unused indexes: %v\n", err)
	} else {
		if len(unusedIndexes) == 0 {
			fmt.Println("✅ All indexes are being used")
		} else {
			fmt.Printf("⚠️  Found %d unused indexes:\n", len(unusedIndexes))
			for _, index := range unusedIndexes {
				fmt.Printf("  - %s.%s (%s) - Size: %d bytes\n", 
					index.TableName, index.IndexName, index.SchemaName, index.IndexSize)
			}
		}
	}

	// Analyze table statistics
	fmt.Println("\n3. TABLE STATISTICS")
	fmt.Println("===================")
	tableStats, err := d.AnalyzeTableStats(ctx)
	if err != nil {
		fmt.Printf("Error analyzing table stats: %v\n", err)
	} else {
		for _, stat := range tableStats {
			fmt.Printf("Table: %s\n", stat.TableName)
			fmt.Printf("  Rows: %d, Size: %d bytes\n", stat.RowCount, stat.TableSize)
			fmt.Printf("  Seq Scans: %d, Index Scans: %d\n", stat.SeqScans, stat.IndexScans)
			
			if stat.SeqScans > stat.IndexScans && stat.RowCount > 1000 {
				fmt.Printf("  ⚠️  High sequential scan ratio - consider adding indexes\n")
			}
			fmt.Println()
		}
	}

	// Check for missing indexes
	fmt.Println("4. MISSING INDEX SUGGESTIONS")
	fmt.Println("============================")
	suggestions, err := d.CheckMissingIndexes(ctx)
	if err != nil {
		fmt.Printf("Error checking missing indexes: %v\n", err)
	} else {
		if len(suggestions) == 0 {
			fmt.Println("✅ No obvious missing indexes detected")
		} else {
			for _, suggestion := range suggestions {
				fmt.Printf("💡 %s\n", suggestion)
			}
		}
	}

	// Analyze vector performance
	fmt.Println("\n5. VECTOR SEARCH PERFORMANCE")
	fmt.Println("============================")
	vectorStats, err := d.AnalyzeVectorPerformance(ctx)
	if err != nil {
		fmt.Printf("Error analyzing vector performance: %v\n", err)
	} else {
		if embStats, ok := vectorStats["embeddings_stats"]; ok {
			fmt.Printf("Embeddings table statistics: %+v\n", embStats)
		}
		if hnswStats, ok := vectorStats["hnsw_indexes"]; ok {
			fmt.Printf("HNSW index statistics: %+v\n", hnswStats)
		}
	}

	fmt.Println("\n=== END OF REPORT ===")
	return nil
}

// Close closes the database connection
func (d *DBAnalyzer) Close() error {
	return d.db.Close()
}

// Helper functions
func getEnvDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Main function for standalone execution
func main() {
	analyzer, err := NewDBAnalyzer()
	if err != nil {
		log.Fatal("Failed to create database analyzer:", err)
	}
	defer analyzer.Close()

	ctx := context.Background()
	if err := analyzer.GenerateOptimizationReport(ctx); err != nil {
		log.Fatal("Failed to generate optimization report:", err)
	}
}