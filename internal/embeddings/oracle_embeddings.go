// internal/embeddings/oracle_embeddings.go

package embeddings

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// OracleEmbeddingStore manages code embeddings in Oracle Database
type OracleEmbeddingStore struct {
	db           *sql.DB
	tableName    string
	embeddingDim int
}

// init loads environment variables
func init() {
	_ = godotenv.Load()
}

// NewOracleEmbeddingStore creates a new Oracle embedding store
func NewOracleEmbeddingStore(embeddingDim int) (*OracleEmbeddingStore, error) {
	user := os.Getenv("ORACLE_USER")
	pass := os.Getenv("ORACLE_PASS")
	dsn := os.Getenv("ORACLE_DSN")

	if user == "" || pass == "" || dsn == "" {
		return nil, fmt.Errorf("ORACLE_USER, ORACLE_PASS, and ORACLE_DSN environment variables must be set")
	}

	tableName := os.Getenv("ORACLE_EMBEDDINGS_TABLE")
	if tableName == "" {
		tableName = "CODE_EMBEDDINGS"
	}

	// Build connection string
	connStr := fmt.Sprintf(`user="%s" password="%s" connectString="%s"`, user, pass, dsn)

	db, err := sql.Open("godror", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Set larger array size for better performance with vectors
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(10)

	store := &OracleEmbeddingStore{
		db:           db,
		tableName:    tableName,
		embeddingDim: embeddingDim,
	}

	// Initialize the store
	if err := store.initialize(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize store: %w", err)
	}

	return store, nil
}

// initialize creates necessary tables and indexes
func (s *OracleEmbeddingStore) initialize() error {
	// Create embeddings table with vector column
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE %s (
			id VARCHAR2(255) PRIMARY KEY,
			chunk_type VARCHAR2(50) NOT NULL,
			name VARCHAR2(500) NOT NULL,
			file_path VARCHAR2(1000) NOT NULL,
			content CLOB NOT NULL,
			start_line NUMBER,
			end_line NUMBER,
			language VARCHAR2(20),
			metadata CLOB CHECK (metadata IS JSON),
			embedding VECTOR(%d, FLOAT32),
			created_at TIMESTAMP DEFAULT SYSTIMESTAMP,
			updated_at TIMESTAMP DEFAULT SYSTIMESTAMP
		)
	`, s.tableName, s.embeddingDim)

	// Check if table exists
	var count int
	err := s.db.QueryRow(fmt.Sprintf(
		"SELECT COUNT(*) FROM user_tables WHERE table_name = '%s'",
		strings.ToUpper(s.tableName),
	)).Scan(&count)

	if err != nil {
		return fmt.Errorf("failed to check if table exists: %w", err)
	}

	if count == 0 {
		_, err = s.db.Exec(createTableSQL)
		if err != nil {
			return fmt.Errorf("failed to create embeddings table: %w", err)
		}
	}

	// Create indexes
	indexes := []struct {
		name   string
		column string
		unique bool
		vector bool
	}{
		{fmt.Sprintf("IDX_%s_CHUNK_TYPE", s.tableName), "chunk_type", false, false},
		{fmt.Sprintf("IDX_%s_FILE_PATH", s.tableName), "file_path", false, false},
		{fmt.Sprintf("IDX_%s_NAME", s.tableName), "name", false, false},
		{fmt.Sprintf("IDX_%s_LANGUAGE", s.tableName), "language", false, false},
		// Vector similarity index (HNSW - Hierarchical Navigable Small World)
		{fmt.Sprintf("IDX_%s_EMBEDDING_HNSW", s.tableName), "embedding", false, true},
	}

	for _, idx := range indexes {
		// Check if index exists
		var indexCount int
		err := s.db.QueryRow(fmt.Sprintf(
			"SELECT COUNT(*) FROM user_indexes WHERE index_name = '%s'",
			strings.ToUpper(idx.name),
		)).Scan(&indexCount)

		if err != nil || indexCount > 0 {
			continue // Skip if error or index exists
		}

		var createIndexSQL string
		if idx.vector {
			// Create vector similarity index using HNSW algorithm
			createIndexSQL = fmt.Sprintf(`
				CREATE VECTOR INDEX %s ON %s (embedding) 
				ORGANIZATION NEIGHBOR PARTITIONS 
				DISTANCE COSINE 
				WITH TARGET ACCURACY 95
			`, idx.name, s.tableName)
		} else {
			createIndexSQL = fmt.Sprintf(
				"CREATE INDEX %s ON %s(%s)",
				idx.name, s.tableName, idx.column,
			)
		}

		if _, err := s.db.Exec(createIndexSQL); err != nil {
			// Log but don't fail
			fmt.Printf("Warning: Failed to create index %s: %v\n", idx.name, err)
		}
	}

	// Create trigger for updated_at
	triggerSQL := fmt.Sprintf(`
		CREATE OR REPLACE TRIGGER %s_UPDATE_TRIGGER
		BEFORE UPDATE ON %s
		FOR EACH ROW
		BEGIN
			:NEW.updated_at := SYSTIMESTAMP;
		END;
	`, s.tableName, s.tableName)

	_, _ = s.db.Exec(triggerSQL)

	return nil
}

// Close closes the database connection
func (s *OracleEmbeddingStore) Close() error {
	return s.db.Close()
}

// floatSliceToOracleVector converts float32 slice to Oracle vector format
func floatSliceToOracleVector(vec []float32) string {
	parts := make([]string, len(vec))
	for i, v := range vec {
		parts[i] = fmt.Sprintf("%f", v)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// UpsertChunk inserts or updates a code chunk with its embedding
func (s *OracleEmbeddingStore) UpsertChunk(ctx context.Context, chunk CodeChunk) error {
	metadataJSON, err := json.Marshal(chunk.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Convert embedding to Oracle vector format
	vectorStr := floatSliceToOracleVector(chunk.Embedding)

	query := fmt.Sprintf(`
		MERGE INTO %s t
		USING (SELECT :1 AS id FROM DUAL) s
		ON (t.id = s.id)
		WHEN MATCHED THEN
			UPDATE SET
				chunk_type = :2,
				name = :3,
				file_path = :4,
				content = :5,
				start_line = :6,
				end_line = :7,
				language = :8,
				metadata = :9,
				embedding = TO_VECTOR(:10),
				updated_at = SYSTIMESTAMP
		WHEN NOT MATCHED THEN
			INSERT (id, chunk_type, name, file_path, content, start_line, end_line, language, metadata, embedding)
			VALUES (:1, :2, :3, :4, :5, :6, :7, :8, :9, TO_VECTOR(:10))
	`, s.tableName)

	_, err = s.db.ExecContext(ctx, query,
		chunk.ID,
		string(chunk.Type),
		chunk.Name,
		chunk.FilePath,
		chunk.Content,
		chunk.StartLine,
		chunk.EndLine,
		chunk.Language,
		string(metadataJSON),
		vectorStr,
	)

	return err
}

// SearchSimilar finds the most similar code chunks to the given embedding
func (s *OracleEmbeddingStore) SearchSimilar(ctx context.Context, embedding []float32, limit int, filters map[string]interface{}) ([]CodeChunk, error) {
	// Build filter conditions
	conditions := []string{"embedding IS NOT NULL"}
	args := []interface{}{}
	argCount := 0

	// Convert query embedding to Oracle vector format
	queryVector := floatSliceToOracleVector(embedding)

	if chunkType, ok := filters["chunk_type"].(string); ok {
		argCount++
		conditions = append(conditions, fmt.Sprintf("chunk_type = :%d", argCount))
		args = append(args, chunkType)
	}

	if language, ok := filters["language"].(string); ok {
		argCount++
		conditions = append(conditions, fmt.Sprintf("language = :%d", argCount))
		args = append(args, language)
	}

	if filePath, ok := filters["file_path"].(string); ok {
		argCount++
		conditions = append(conditions, fmt.Sprintf("file_path LIKE :%d", argCount))
		args = append(args, "%"+filePath+"%")
	}

	whereClause := strings.Join(conditions, " AND ")

	// Use VECTOR_DISTANCE for similarity search
	query := fmt.Sprintf(`
		SELECT 
			id, chunk_type, name, file_path, content, 
			start_line, end_line, language, metadata,
			VECTOR_DISTANCE(embedding, TO_VECTOR('%s'), COSINE) as distance
		FROM %s
		WHERE %s
		ORDER BY distance
		FETCH FIRST %d ROWS ONLY
	`, queryVector, s.tableName, whereClause, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to search similar chunks: %w", err)
	}
	defer rows.Close()

	var chunks []CodeChunk
	for rows.Next() {
		var chunk CodeChunk
		var metadataStr string
		var distance float64

		err := rows.Scan(
			&chunk.ID,
			&chunk.Type,
			&chunk.Name,
			&chunk.FilePath,
			&chunk.Content,
			&chunk.StartLine,
			&chunk.EndLine,
			&chunk.Language,
			&metadataStr,
			&distance,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		if err := json.Unmarshal([]byte(metadataStr), &chunk.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		// Add similarity score to metadata (1 - distance for cosine)
		if chunk.Metadata == nil {
			chunk.Metadata = make(map[string]interface{})
		}
		chunk.Metadata["similarity_score"] = 1.0 - distance
		chunk.Metadata["distance"] = distance

		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

// GetChunk retrieves a specific chunk by ID
func (s *OracleEmbeddingStore) GetChunk(ctx context.Context, id string) (*CodeChunk, error) {
	query := fmt.Sprintf(`
		SELECT 
			id, chunk_type, name, file_path, content, 
			start_line, end_line, language, metadata
		FROM %s
		WHERE id = :1
	`, s.tableName)

	var chunk CodeChunk
	var metadataStr string

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&chunk.ID,
		&chunk.Type,
		&chunk.Name,
		&chunk.FilePath,
		&chunk.Content,
		&chunk.StartLine,
		&chunk.EndLine,
		&chunk.Language,
		&metadataStr,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get chunk: %w", err)
	}

	if err := json.Unmarshal([]byte(metadataStr), &chunk.Metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &chunk, nil
}

// DeleteChunksForFile removes all chunks for a specific file
func (s *OracleEmbeddingStore) DeleteChunksForFile(ctx context.Context, filePath string) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE file_path = :1", s.tableName)
	_, err := s.db.ExecContext(ctx, query, filePath)
	return err
}

// GetStats returns statistics about the embeddings store
func (s *OracleEmbeddingStore) GetStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Total chunks
	var totalChunks int
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s", s.tableName)).Scan(&totalChunks)
	if err != nil {
		return nil, err
	}
	stats["total_chunks"] = totalChunks

	// Chunks with embeddings
	var chunksWithEmbeddings int
	err = s.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE embedding IS NOT NULL", s.tableName)).Scan(&chunksWithEmbeddings)
	if err != nil {
		return nil, err
	}
	stats["chunks_with_embeddings"] = chunksWithEmbeddings

	// Chunks by type
	query := fmt.Sprintf(`
		SELECT chunk_type, COUNT(*) 
		FROM %s 
		GROUP BY chunk_type
		ORDER BY COUNT(*) DESC
	`, s.tableName)

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chunksByType := make(map[string]int)
	for rows.Next() {
		var chunkType string
		var count int
		if err := rows.Scan(&chunkType, &count); err != nil {
			return nil, err
		}
		chunksByType[chunkType] = count
	}
	stats["chunks_by_type"] = chunksByType

	// Chunks by language
	query2 := fmt.Sprintf(`
		SELECT language, COUNT(*) 
		FROM %s 
		WHERE language IS NOT NULL
		GROUP BY language
		ORDER BY COUNT(*) DESC
	`, s.tableName)

	rows2, err := s.db.QueryContext(ctx, query2)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()

	chunksByLang := make(map[string]int)
	for rows2.Next() {
		var lang string
		var count int
		if err := rows2.Scan(&lang, &count); err != nil {
			return nil, err
		}
		chunksByLang[lang] = count
	}
	stats["chunks_by_language"] = chunksByLang

	// Average content length
	var avgContentLength float64
	err = s.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT AVG(LENGTH(content)) FROM %s", s.tableName)).Scan(&avgContentLength)
	if err == nil {
		stats["avg_content_length"] = avgContentLength
	}

	return stats, nil
}

// BatchUpsertChunks efficiently inserts multiple chunks
func (s *OracleEmbeddingStore) BatchUpsertChunks(ctx context.Context, chunks []CodeChunk) error {
	// Oracle doesn't have native batch MERGE, so we'll use PL/SQL block
	plsql := fmt.Sprintf(`
		DECLARE
			TYPE t_ids IS TABLE OF VARCHAR2(255);
			TYPE t_types IS TABLE OF VARCHAR2(50);
			TYPE t_names IS TABLE OF VARCHAR2(500);
			TYPE t_paths IS TABLE OF VARCHAR2(1000);
			TYPE t_contents IS TABLE OF CLOB;
			TYPE t_starts IS TABLE OF NUMBER;
			TYPE t_ends IS TABLE OF NUMBER;
			TYPE t_langs IS TABLE OF VARCHAR2(20);
			TYPE t_metas IS TABLE OF CLOB;
			TYPE t_vecs IS TABLE OF VARCHAR2(32767);
			
			l_ids t_ids := :1;
			l_types t_types := :2;
			l_names t_names := :3;
			l_paths t_paths := :4;
			l_contents t_contents := :5;
			l_starts t_starts := :6;
			l_ends t_ends := :7;
			l_langs t_langs := :8;
			l_metas t_metas := :9;
			l_vecs t_vecs := :10;
		BEGIN
			FOR i IN 1..l_ids.COUNT LOOP
				MERGE INTO %s t
				USING (SELECT l_ids(i) AS id FROM DUAL) s
				ON (t.id = s.id)
				WHEN MATCHED THEN
					UPDATE SET
						chunk_type = l_types(i),
						name = l_names(i),
						file_path = l_paths(i),
						content = l_contents(i),
						start_line = l_starts(i),
						end_line = l_ends(i),
						language = l_langs(i),
						metadata = l_metas(i),
						embedding = TO_VECTOR(l_vecs(i)),
						updated_at = SYSTIMESTAMP
				WHEN NOT MATCHED THEN
					INSERT (id, chunk_type, name, file_path, content, start_line, end_line, language, metadata, embedding)
					VALUES (l_ids(i), l_types(i), l_names(i), l_paths(i), l_contents(i), l_starts(i), l_ends(i), l_langs(i), l_metas(i), TO_VECTOR(l_vecs(i)));
			END LOOP;
		END;
	`, s.tableName)

	// Prepare arrays
	ids := make([]string, len(chunks))
	types := make([]string, len(chunks))
	names := make([]string, len(chunks))
	paths := make([]string, len(chunks))
	contents := make([]string, len(chunks))
	starts := make([]int, len(chunks))
	ends := make([]int, len(chunks))
	langs := make([]string, len(chunks))
	metas := make([]string, len(chunks))
	vecs := make([]string, len(chunks))

	for i, chunk := range chunks {
		ids[i] = chunk.ID
		types[i] = string(chunk.Type)
		names[i] = chunk.Name
		paths[i] = chunk.FilePath
		contents[i] = chunk.Content
		starts[i] = chunk.StartLine
		ends[i] = chunk.EndLine
		langs[i] = chunk.Language

		metaJSON, _ := json.Marshal(chunk.Metadata)
		metas[i] = string(metaJSON)

		vecs[i] = floatSliceToOracleVector(chunk.Embedding)
	}

	_, err := s.db.ExecContext(ctx, plsql,
		ids,
		types,
		names,
		paths,
		contents,
		starts,
		ends,
		langs,
		metas,
		vecs,
	)

	return err
}

// HybridSearch performs both vector similarity and keyword search
func (s *OracleEmbeddingStore) HybridSearch(ctx context.Context, embedding []float32, keywords string, limit int, filters map[string]interface{}) ([]CodeChunk, error) {
	// Build filter conditions
	conditions := []string{"embedding IS NOT NULL"}
	args := []interface{}{}
	argCount := 0

	// Convert query embedding to Oracle vector format
	queryVector := floatSliceToOracleVector(embedding)

	// Add keyword search condition if provided
	if keywords != "" {
		argCount++
		// Use Oracle Text search if available, otherwise simple LIKE
		conditions = append(conditions, fmt.Sprintf("(UPPER(content) LIKE UPPER(:%d) OR UPPER(name) LIKE UPPER(:%d))", argCount, argCount))
		args = append(args, "%"+keywords+"%")
	}

	// Add other filters
	if chunkType, ok := filters["chunk_type"].(string); ok {
		argCount++
		conditions = append(conditions, fmt.Sprintf("chunk_type = :%d", argCount))
		args = append(args, chunkType)
	}

	whereClause := strings.Join(conditions, " AND ")

	// Combine vector similarity and keyword relevance
	query := fmt.Sprintf(`
		SELECT 
			id, chunk_type, name, file_path, content, 
			start_line, end_line, language, metadata,
			VECTOR_DISTANCE(embedding, TO_VECTOR('%s'), COSINE) as vec_distance,
			CASE 
				WHEN UPPER(content) LIKE UPPER(:keyword) THEN 0.2
				WHEN UPPER(name) LIKE UPPER(:keyword) THEN 0.1
				ELSE 0
			END as keyword_score
		FROM %s
		WHERE %s
		ORDER BY (vec_distance - keyword_score)
		FETCH FIRST %d ROWS ONLY
	`, queryVector, s.tableName, whereClause, limit)

	// Add keyword parameter
	if keywords != "" {
		query = strings.Replace(query, ":keyword", fmt.Sprintf("'%%%s%%'", keywords), -1)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to perform hybrid search: %w", err)
	}
	defer rows.Close()

	var chunks []CodeChunk
	for rows.Next() {
		var chunk CodeChunk
		var metadataStr string
		var vecDistance, keywordScore float64

		err := rows.Scan(
			&chunk.ID,
			&chunk.Type,
			&chunk.Name,
			&chunk.FilePath,
			&chunk.Content,
			&chunk.StartLine,
			&chunk.EndLine,
			&chunk.Language,
			&metadataStr,
			&vecDistance,
			&keywordScore,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		if err := json.Unmarshal([]byte(metadataStr), &chunk.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		// Add scores to metadata
		if chunk.Metadata == nil {
			chunk.Metadata = make(map[string]interface{})
		}
		chunk.Metadata["vector_similarity"] = 1.0 - vecDistance
		chunk.Metadata["keyword_score"] = keywordScore
		chunk.Metadata["combined_score"] = (1.0 - vecDistance) + keywordScore

		chunks = append(chunks, chunk)
	}

	return chunks, nil
}
