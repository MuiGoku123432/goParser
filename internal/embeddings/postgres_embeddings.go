// internal/embeddings/postgres_embeddings.go

package embeddings

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
	"github.com/pgvector/pgvector-go"
)

// ChunkType represents the type of code chunk
type ChunkType string

const (
	ChunkTypeFunction  ChunkType = "function"
	ChunkTypeClass     ChunkType = "class"
	ChunkTypeInterface ChunkType = "interface"
	ChunkTypeType      ChunkType = "type"
	ChunkTypeJSX       ChunkType = "jsx_component"
	ChunkTypeFile      ChunkType = "file"
	ChunkTypeImports   ChunkType = "imports"
)

// CodeChunk represents a chunk of code to be embedded
type CodeChunk struct {
	ID           string                 `json:"id"`
	Type         ChunkType              `json:"type"`
	Name         string                 `json:"name"`
	FilePath     string                 `json:"file_path"`
	Content      string                 `json:"content"`
	StartLine    int                    `json:"start_line"`
	EndLine      int                    `json:"end_line"`
	Language     string                 `json:"language"`
	Metadata     map[string]interface{} `json:"metadata"`
	Embedding    []float32              `json:"-"`
	EmbeddingDim int                    `json:"embedding_dim"`
}

// PostgresEmbeddingStore manages code embeddings in PostgreSQL
type PostgresEmbeddingStore struct {
	db           *sql.DB
	tableName    string
	embeddingDim int
}

// init loads environment variables
func init() {
	_ = godotenv.Load()
}

// NewPostgresEmbeddingStore creates a new PostgreSQL embedding store
func NewPostgresEmbeddingStore(embeddingDim int) (*PostgresEmbeddingStore, error) {
	host := os.Getenv("PG_HOST")
	if host == "" {
		host = "localhost"
	}

	port := os.Getenv("PG_PORT")
	if port == "" {
		port = "5432"
	}

	user := os.Getenv("PG_USER")
	if user == "" {
		user = "postgres"
	}

	pass := os.Getenv("PG_PASS")
	dbname := os.Getenv("PG_DB")
	if dbname == "" {
		dbname = "postgres"
	}

	tableName := os.Getenv("PG_EMBEDDINGS_TABLE")
	if tableName == "" {
		tableName = "code_embeddings"
	}

	// Build connection string
	connStr := fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=disable",
		host, port, user, dbname)
	if pass != "" {
		connStr += fmt.Sprintf(" password=%s", pass)
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	store := &PostgresEmbeddingStore{
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

// initialize creates necessary extensions and tables
func (s *PostgresEmbeddingStore) initialize() error {
	// Create pgvector extension
	_, err := s.db.Exec("CREATE EXTENSION IF NOT EXISTS vector")
	if err != nil {
		return fmt.Errorf("failed to create vector extension: %w", err)
	}

	// Create embeddings table
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR(255) PRIMARY KEY,
			chunk_type VARCHAR(50) NOT NULL,
			name VARCHAR(500) NOT NULL,
			file_path VARCHAR(1000) NOT NULL,
			content TEXT NOT NULL,
			start_line INTEGER,
			end_line INTEGER,
			language VARCHAR(20),
			metadata JSONB,
			embedding vector(%d),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`, pq.QuoteIdentifier(s.tableName), s.embeddingDim)

	_, err = s.db.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create embeddings table: %w", err)
	}

	// Create indexes
	indexes := []string{
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_chunk_type ON %s(chunk_type)",
			s.tableName, pq.QuoteIdentifier(s.tableName)),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_file_path ON %s(file_path)",
			s.tableName, pq.QuoteIdentifier(s.tableName)),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_name ON %s(name)",
			s.tableName, pq.QuoteIdentifier(s.tableName)),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_language ON %s(language)",
			s.tableName, pq.QuoteIdentifier(s.tableName)),
		// Create IVFFlat index for similarity search
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_embedding ON %s USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)",
			s.tableName, pq.QuoteIdentifier(s.tableName)),
	}

	for _, idx := range indexes {
		if _, err := s.db.Exec(idx); err != nil {
			// Log but don't fail - index might already exist
			fmt.Printf("Warning: Failed to create index: %v\n", err)
		}
	}

	return nil
}

// Close closes the database connection
func (s *PostgresEmbeddingStore) Close() error {
	return s.db.Close()
}

// UpsertChunk inserts or updates a code chunk with its embedding
func (s *PostgresEmbeddingStore) UpsertChunk(ctx context.Context, chunk CodeChunk) error {
	metadataJSON, err := json.Marshal(chunk.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := fmt.Sprintf(`
		INSERT INTO %s 
		(id, chunk_type, name, file_path, content, start_line, end_line, language, metadata, embedding)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) 
		DO UPDATE SET
			chunk_type = EXCLUDED.chunk_type,
			name = EXCLUDED.name,
			file_path = EXCLUDED.file_path,
			content = EXCLUDED.content,
			start_line = EXCLUDED.start_line,
			end_line = EXCLUDED.end_line,
			language = EXCLUDED.language,
			metadata = EXCLUDED.metadata,
			embedding = EXCLUDED.embedding,
			updated_at = CURRENT_TIMESTAMP
	`, pq.QuoteIdentifier(s.tableName))

	_, err = s.db.ExecContext(ctx, query,
		chunk.ID,
		string(chunk.Type),
		chunk.Name,
		chunk.FilePath,
		chunk.Content,
		chunk.StartLine,
		chunk.EndLine,
		chunk.Language,
		metadataJSON,
		pgvector.NewVector(chunk.Embedding),
	)

	return err
}

// SearchSimilar finds the most similar code chunks to the given embedding
func (s *PostgresEmbeddingStore) SearchSimilar(ctx context.Context, embedding []float32, limit int, filters map[string]interface{}) ([]CodeChunk, error) {
	// Build filter conditions
	conditions := []string{"embedding IS NOT NULL"}
	args := []interface{}{pgvector.NewVector(embedding)}
	argCount := 1

	if chunkType, ok := filters["chunk_type"].(string); ok {
		argCount++
		conditions = append(conditions, fmt.Sprintf("chunk_type = $%d", argCount))
		args = append(args, chunkType)
	}

	if language, ok := filters["language"].(string); ok {
		argCount++
		conditions = append(conditions, fmt.Sprintf("language = $%d", argCount))
		args = append(args, language)
	}

	if filePath, ok := filters["file_path"].(string); ok {
		argCount++
		conditions = append(conditions, fmt.Sprintf("file_path LIKE $%d", argCount))
		args = append(args, "%"+filePath+"%")
	}

	whereClause := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT 
			id, chunk_type, name, file_path, content, 
			start_line, end_line, language, metadata,
			embedding <=> $1 as distance
		FROM %s
		WHERE %s
		ORDER BY embedding <=> $1
		LIMIT $%d
	`, pq.QuoteIdentifier(s.tableName), whereClause, argCount+1)

	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to search similar chunks: %w", err)
	}
	defer rows.Close()

	var chunks []CodeChunk
	for rows.Next() {
		var chunk CodeChunk
		var metadataJSON []byte
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
			&metadataJSON,
			&distance,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		if err := json.Unmarshal(metadataJSON, &chunk.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		// Add distance to metadata
		if chunk.Metadata == nil {
			chunk.Metadata = make(map[string]interface{})
		}
		chunk.Metadata["similarity_distance"] = distance

		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

// GetChunk retrieves a specific chunk by ID
func (s *PostgresEmbeddingStore) GetChunk(ctx context.Context, id string) (*CodeChunk, error) {
	query := fmt.Sprintf(`
		SELECT 
			id, chunk_type, name, file_path, content, 
			start_line, end_line, language, metadata
		FROM %s
		WHERE id = $1
	`, pq.QuoteIdentifier(s.tableName))

	var chunk CodeChunk
	var metadataJSON []byte

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&chunk.ID,
		&chunk.Type,
		&chunk.Name,
		&chunk.FilePath,
		&chunk.Content,
		&chunk.StartLine,
		&chunk.EndLine,
		&chunk.Language,
		&metadataJSON,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get chunk: %w", err)
	}

	if err := json.Unmarshal(metadataJSON, &chunk.Metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &chunk, nil
}

// DeleteChunksForFile removes all chunks for a specific file
func (s *PostgresEmbeddingStore) DeleteChunksForFile(ctx context.Context, filePath string) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE file_path = $1", pq.QuoteIdentifier(s.tableName))
	_, err := s.db.ExecContext(ctx, query, filePath)
	return err
}

// GetStats returns statistics about the embeddings store
func (s *PostgresEmbeddingStore) GetStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Total chunks
	var totalChunks int
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s", pq.QuoteIdentifier(s.tableName))).Scan(&totalChunks)
	if err != nil {
		return nil, err
	}
	stats["total_chunks"] = totalChunks

	// Chunks by type
	query := fmt.Sprintf(`
		SELECT chunk_type, COUNT(*) 
		FROM %s 
		GROUP BY chunk_type
	`, pq.QuoteIdentifier(s.tableName))

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
	`, pq.QuoteIdentifier(s.tableName))

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

	return stats, nil
}

// CreateCodeChunks generates chunks from parsed code entities
func CreateCodeChunks(parsedFile ParsedFileData) []CodeChunk {
	var chunks []CodeChunk

	// Create chunks for functions
	for _, fn := range parsedFile.Functions {
		chunk := CodeChunk{
			ID:        fmt.Sprintf("func_%s_%s", parsedFile.FilePath, fn.Name),
			Type:      ChunkTypeFunction,
			Name:      fn.Name,
			FilePath:  parsedFile.FilePath,
			Content:   fn.Content, // This would need to be extracted from source
			StartLine: fn.StartLine,
			EndLine:   fn.EndLine,
			Language:  parsedFile.Language,
			Metadata: map[string]interface{}{
				"signature": fn.Signature,
				"is_async":  fn.IsAsync,
				"is_export": fn.IsExport,
			},
		}
		chunks = append(chunks, chunk)
	}

	// Create chunks for classes
	for _, class := range parsedFile.Classes {
		chunk := CodeChunk{
			ID:        fmt.Sprintf("class_%s_%s", parsedFile.FilePath, class.Name),
			Type:      ChunkTypeClass,
			Name:      class.Name,
			FilePath:  parsedFile.FilePath,
			Content:   class.Content,
			StartLine: class.StartLine,
			EndLine:   class.EndLine,
			Language:  parsedFile.Language,
			Metadata: map[string]interface{}{
				"is_export":   class.IsExport,
				"is_abstract": class.IsAbstract,
				"methods":     class.Methods,
			},
		}
		chunks = append(chunks, chunk)
	}

	// Create chunks for interfaces
	for _, iface := range parsedFile.Interfaces {
		chunk := CodeChunk{
			ID:       fmt.Sprintf("interface_%s_%s", parsedFile.FilePath, iface.Name),
			Type:     ChunkTypeInterface,
			Name:     iface.Name,
			FilePath: parsedFile.FilePath,
			Content:  iface.Content,
			Language: parsedFile.Language,
			Metadata: map[string]interface{}{
				"is_export":  iface.IsExport,
				"properties": iface.Properties,
			},
		}
		chunks = append(chunks, chunk)
	}

	// Create chunks for types
	for _, typ := range parsedFile.Types {
		chunk := CodeChunk{
			ID:       fmt.Sprintf("type_%s_%s", parsedFile.FilePath, typ.Name),
			Type:     ChunkTypeType,
			Name:     typ.Name,
			FilePath: parsedFile.FilePath,
			Content:  typ.Definition,
			Language: parsedFile.Language,
			Metadata: map[string]interface{}{
				"kind":      typ.Kind,
				"is_export": typ.IsExport,
			},
		}
		chunks = append(chunks, chunk)
	}

	// Create a chunk for JSX components (group by containing component)
	jsxByComponent := make(map[string][]interface{})
	for _, jsx := range parsedFile.JSXElements {
		if jsx.ContainingComponent != "" {
			jsxByComponent[jsx.ContainingComponent] = append(
				jsxByComponent[jsx.ContainingComponent],
				map[string]interface{}{
					"tag":   jsx.TagName,
					"props": jsx.Props,
					"line":  jsx.Line,
				},
			)
		}
	}

	for component, elements := range jsxByComponent {
		chunk := CodeChunk{
			ID:       fmt.Sprintf("jsx_%s_%s", parsedFile.FilePath, component),
			Type:     ChunkTypeJSX,
			Name:     component + "_jsx",
			FilePath: parsedFile.FilePath,
			Language: parsedFile.Language,
			Metadata: map[string]interface{}{
				"component_name": component,
				"jsx_elements":   elements,
			},
		}
		chunks = append(chunks, chunk)
	}

	// Create a chunk for imports (useful for understanding dependencies)
	if len(parsedFile.Imports) > 0 {
		importContent := "// Imports for " + parsedFile.FilePath + "\n"
		importModules := []string{}
		for _, imp := range parsedFile.Imports {
			importModules = append(importModules, imp.Module)
			importContent += fmt.Sprintf("import from '%s'\n", imp.Module)
		}

		chunk := CodeChunk{
			ID:       fmt.Sprintf("imports_%s", parsedFile.FilePath),
			Type:     ChunkTypeImports,
			Name:     "imports",
			FilePath: parsedFile.FilePath,
			Content:  importContent,
			Language: parsedFile.Language,
			Metadata: map[string]interface{}{
				"modules": importModules,
			},
		}
		chunks = append(chunks, chunk)
	}

	// If no specific chunks were created, create a file-level chunk
	if len(chunks) == 0 && parsedFile.FileContent != "" {
		chunk := CodeChunk{
			ID:       fmt.Sprintf("file_%s", parsedFile.FilePath),
			Type:     ChunkTypeFile,
			Name:     parsedFile.FilePath,
			FilePath: parsedFile.FilePath,
			Content:  parsedFile.FileContent,
			Language: parsedFile.Language,
			Metadata: map[string]interface{}{
				"is_fallback": true,
			},
		}
		chunks = append(chunks, chunk)
	}

	return chunks
}

// ParsedFileData represents the data from parsing a file (simplified)
type ParsedFileData struct {
	FilePath    string
	Language    string
	FileContent string
	Functions   []FunctionData
	Classes     []ClassData
	Interfaces  []InterfaceData
	Types       []TypeData
	JSXElements []JSXData
	Imports     []ImportData
}

// Simplified data structures for parsed entities
type FunctionData struct {
	Name      string
	Content   string
	StartLine int
	EndLine   int
	Signature string
	IsAsync   bool
	IsExport  bool
}

type ClassData struct {
	Name       string
	Content    string
	StartLine  int
	EndLine    int
	IsExport   bool
	IsAbstract bool
	Methods    []string
}

type InterfaceData struct {
	Name       string
	Content    string
	IsExport   bool
	Properties []string
}

type TypeData struct {
	Name       string
	Definition string
	Kind       string
	IsExport   bool
}

type JSXData struct {
	TagName             string
	ContainingComponent string
	Props               []string
	Line                int
}

type ImportData struct {
	Module string
}
