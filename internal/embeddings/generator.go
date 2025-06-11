// internal/embeddings/generator.go

package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// EmbeddingProvider defines the interface for embedding generation
type EmbeddingProvider interface {
	GenerateEmbedding(ctx context.Context, text string) ([]float32, error)
	GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error)
	GetDimension() int
}

// OpenAIProvider implements embedding generation using OpenAI API
type OpenAIProvider struct {
	apiKey     string
	model      string
	dimension  int
	endpoint   string
	httpClient *http.Client
}

// NewOpenAIProvider creates a new OpenAI embedding provider
func NewOpenAIProvider(model string, dimension int) (*OpenAIProvider, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
	}

	endpoint := os.Getenv("OPENAI_BASE_URL")
	if endpoint == "" {
		endpoint = "https://api.openai.com"
	}
	endpoint = strings.TrimRight(endpoint, "/") + "/v1/embeddings"

	// Validate model and dimension combinations
	validModels := map[string][]int{
		"text-embedding-3-small": {512, 1536},
		"text-embedding-3-large": {256, 1024, 3072},
		"text-embedding-ada-002": {1536},
	}

	if model == "" {
		model = "text-embedding-3-small"
	}

	validDims, ok := validModels[model]
	if !ok {
		return nil, fmt.Errorf("unsupported model: %s", model)
	}

	// Check if dimension is valid for the model
	dimensionValid := false
	for _, d := range validDims {
		if dimension == d || dimension == 0 {
			dimensionValid = true
			if dimension == 0 {
				dimension = d // Use default dimension
			}
			break
		}
	}

	if !dimensionValid {
		return nil, fmt.Errorf("invalid dimension %d for model %s", dimension, model)
	}

	return &OpenAIProvider{
		apiKey:    apiKey,
		model:     model,
		dimension: dimension,
		endpoint:  endpoint,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// GetDimension returns the embedding dimension
func (p *OpenAIProvider) GetDimension() int {
	return p.dimension
}

// GenerateEmbedding generates an embedding for a single text
func (p *OpenAIProvider) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := p.GenerateEmbeddings(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return embeddings[0], nil
}

// GenerateEmbeddings generates embeddings for multiple texts
func (p *OpenAIProvider) GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	// OpenAI API request structure
	request := map[string]interface{}{
		"model": p.model,
		"input": texts,
	}

	// Add dimension parameter for models that support it
	if p.model == "text-embedding-3-small" || p.model == "text-embedding-3-large" {
		request["dimensions"] = p.dimension
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response
	var response struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract embeddings
	embeddings := make([][]float32, len(response.Data))
	for i, data := range response.Data {
		embeddings[i] = data.Embedding
	}

	return embeddings, nil
}

// CodeEmbeddingGenerator handles the generation of embeddings for code chunks
type CodeEmbeddingGenerator struct {
	provider    EmbeddingProvider
	pgStore     *PostgresEmbeddingStore
	oracleStore *OracleEmbeddingStore
	batchSize   int
	useOracle   bool
}

// NewCodeEmbeddingGenerator creates a new code embedding generator
func NewCodeEmbeddingGenerator(provider EmbeddingProvider, useOracle bool) (*CodeEmbeddingGenerator, error) {
	gen := &CodeEmbeddingGenerator{
		provider:  provider,
		batchSize: 100, // Process 100 chunks at a time
		useOracle: useOracle,
	}

	if useOracle {
		store, err := NewOracleEmbeddingStore(provider.GetDimension())
		if err != nil {
			return nil, fmt.Errorf("failed to create Oracle store: %w", err)
		}
		gen.oracleStore = store
	} else {
		store, err := NewPostgresEmbeddingStore(provider.GetDimension())
		if err != nil {
			return nil, fmt.Errorf("failed to create PostgreSQL store: %w", err)
		}
		gen.pgStore = store
	}

	return gen, nil
}

// Close closes the underlying store
func (g *CodeEmbeddingGenerator) Close() error {
	if g.useOracle && g.oracleStore != nil {
		return g.oracleStore.Close()
	} else if g.pgStore != nil {
		return g.pgStore.Close()
	}
	return nil
}

// ProcessFile generates embeddings for all chunks in a parsed file
func (g *CodeEmbeddingGenerator) ProcessFile(ctx context.Context, parsedFile ParsedFileData) error {
	// Create chunks from parsed file
	chunks := CreateCodeChunks(parsedFile)
	if len(chunks) == 0 {
		return nil
	}

	// Process chunks in batches
	for i := 0; i < len(chunks); i += g.batchSize {
		end := i + g.batchSize
		if end > len(chunks) {
			end = len(chunks)
		}

		batch := chunks[i:end]
		if err := g.processBatch(ctx, batch); err != nil {
			return fmt.Errorf("failed to process batch %d-%d: %w", i, end, err)
		}
	}

	return nil
}

// processBatch processes a batch of chunks
func (g *CodeEmbeddingGenerator) processBatch(ctx context.Context, chunks []CodeChunk) error {
	// Prepare texts for embedding
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = g.prepareTextForEmbedding(chunk)
	}

	// Generate embeddings
	embeddings, err := g.provider.GenerateEmbeddings(ctx, texts)
	if err != nil {
		return fmt.Errorf("failed to generate embeddings: %w", err)
	}

	// Update chunks with embeddings
	for i, chunk := range chunks {
		if i < len(embeddings) {
			chunk.Embedding = embeddings[i]
			chunk.EmbeddingDim = len(embeddings[i])

			// Store in database
			if g.useOracle {
				if err := g.oracleStore.UpsertChunk(ctx, chunk); err != nil {
					return fmt.Errorf("failed to store chunk %s: %w", chunk.ID, err)
				}
			} else {
				if err := g.pgStore.UpsertChunk(ctx, chunk); err != nil {
					return fmt.Errorf("failed to store chunk %s: %w", chunk.ID, err)
				}
			}
		}
	}

	return nil
}

// prepareTextForEmbedding formats a chunk for embedding generation
func (g *CodeEmbeddingGenerator) prepareTextForEmbedding(chunk CodeChunk) string {
	var parts []string

	// Add context about the chunk type
	parts = append(parts, fmt.Sprintf("Type: %s", chunk.Type))

	// Add the name/identifier
	if chunk.Name != "" {
		parts = append(parts, fmt.Sprintf("Name: %s", chunk.Name))
	}

	// Add file context
	parts = append(parts, fmt.Sprintf("File: %s", chunk.FilePath))

	// Add language
	if chunk.Language != "" {
		parts = append(parts, fmt.Sprintf("Language: %s", chunk.Language))
	}

	// Add relevant metadata
	switch chunk.Type {
	case ChunkTypeFunction:
		if sig, ok := chunk.Metadata["signature"].(string); ok && sig != "" {
			parts = append(parts, fmt.Sprintf("Signature: %s", sig))
		}
	case ChunkTypeClass:
		if methods, ok := chunk.Metadata["methods"].([]string); ok && len(methods) > 0 {
			parts = append(parts, fmt.Sprintf("Methods: %s", strings.Join(methods, ", ")))
		}
	case ChunkTypeInterface:
		if props, ok := chunk.Metadata["properties"].([]string); ok && len(props) > 0 {
			parts = append(parts, fmt.Sprintf("Properties: %s", strings.Join(props, ", ")))
		}
	}

	// Add the main content
	parts = append(parts, "\n--- Content ---\n")
	parts = append(parts, chunk.Content)

	return strings.Join(parts, "\n")
}

// SearchSimilarCode searches for similar code chunks
func (g *CodeEmbeddingGenerator) SearchSimilarCode(ctx context.Context, query string, limit int, filters map[string]interface{}) ([]CodeChunk, error) {
	// Generate embedding for the query
	embedding, err := g.provider.GenerateEmbedding(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	// Search in the appropriate store
	if g.useOracle {
		return g.oracleStore.SearchSimilar(ctx, embedding, limit, filters)
	}
	return g.pgStore.SearchSimilar(ctx, embedding, limit, filters)
}

// HybridSearch performs both semantic and keyword search (Oracle only)
func (g *CodeEmbeddingGenerator) HybridSearch(ctx context.Context, query string, keywords string, limit int, filters map[string]interface{}) ([]CodeChunk, error) {
	if !g.useOracle {
		return nil, fmt.Errorf("hybrid search is only available with Oracle store")
	}

	// Generate embedding for the semantic part
	embedding, err := g.provider.GenerateEmbedding(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	return g.oracleStore.HybridSearch(ctx, embedding, keywords, limit, filters)
}

// GetStats returns statistics about the embeddings
func (g *CodeEmbeddingGenerator) GetStats(ctx context.Context) (map[string]interface{}, error) {
	if g.useOracle {
		return g.oracleStore.GetStats(ctx)
	}
	return g.pgStore.GetStats(ctx)
}

// Example usage function
func ExampleUsage() {
	ctx := context.Background()

	// Create OpenAI provider with text-embedding-3-small model
	provider, err := NewOpenAIProvider("text-embedding-3-small", 1536)
	if err != nil {
		panic(err)
	}

	// Create generator for PostgreSQL
	generator, err := NewCodeEmbeddingGenerator(provider, false)
	if err != nil {
		panic(err)
	}
	defer generator.Close()

	// Example parsed file data
	parsedFile := ParsedFileData{
		FilePath:    "src/utils/helper.ts",
		Language:    "typescript",
		FileContent: "// File content here",
		Functions: []FunctionData{
			{
				Name:      "calculateSum",
				Content:   "export function calculateSum(a: number, b: number): number {\n  return a + b;\n}",
				StartLine: 1,
				EndLine:   3,
				Signature: "(a: number, b: number): number",
				IsExport:  true,
			},
		},
	}

	// Process the file
	if err := generator.ProcessFile(ctx, parsedFile); err != nil {
		panic(err)
	}

	// Search for similar code
	results, err := generator.SearchSimilarCode(ctx,
		"function that adds two numbers",
		5,
		map[string]interface{}{
			"chunk_type": "function",
			"language":   "typescript",
		},
	)
	if err != nil {
		panic(err)
	}

	for _, chunk := range results {
		fmt.Printf("Found: %s (similarity: %.2f)\n",
			chunk.Name,
			chunk.Metadata["similarity_score"])
	}
}
