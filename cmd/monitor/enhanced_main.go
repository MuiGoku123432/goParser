// cmd/monitor/enhanced_main.go
// Example of how to use the enhanced monitor with all features

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"goParse/internal/api"
	"goParse/internal/embeddings"
	"goParse/internal/model"
	"goParse/internal/monitor"
)

func main() {
	// Command line flags
	var root string
	var useAGE bool
	var useOracle bool
	var generateEmbeddings bool
	var embeddingModel string
	var embeddingDim int

	// Enhanced features
	var enableBatch bool
	var batchSize int
	var batchInterval int
	var enableDiff bool
	var enableGit bool
	var apiPort int

	flag.StringVar(&root, "root", ".", "Root directory of codebase to monitor")
	flag.BoolVar(&useAGE, "use-age", false, "Use Apache AGE instead of Neo4j")
	flag.BoolVar(&useOracle, "use-oracle", false, "Use Oracle Graph instead of Neo4j")
	flag.BoolVar(&generateEmbeddings, "embeddings", false, "Generate embeddings for code chunks")
	flag.StringVar(&embeddingModel, "embedding-model", "text-embedding-3-small", "OpenAI embedding model to use")
	flag.IntVar(&embeddingDim, "embedding-dim", 1536, "Embedding dimension")

	// Enhanced feature flags
	flag.BoolVar(&enableBatch, "enable-batch", false, "Enable batch processing")
	flag.IntVar(&batchSize, "batch-size", 50, "Batch size for processing")
	flag.IntVar(&batchInterval, "batch-interval", 10, "Batch flush interval in seconds")
	flag.BoolVar(&enableDiff, "enable-diff", false, "Enable diff analysis")
	flag.BoolVar(&enableGit, "enable-git", false, "Enable git integration")
	flag.IntVar(&apiPort, "api-port", 8080, "API server port (0 to disable)")

	flag.Parse()

	// Ensure the root path exists
	if _, err := os.Stat(root); err != nil {
		log.Fatalf("Root path does not exist: %v", err)
	}

	// Initialize graph client
	ctx := context.Background()
	var graphClient monitor.GraphClient
	var err error

	if useOracle {
		log.Println("Using Oracle Graph database")
		graphClient, err = model.NewOracleGraphClient()
	} else if useAGE {
		log.Println("Using Apache AGE graph database")
		graphClient, err = model.NewAGEClient()
	} else {
		log.Println("Using Neo4j graph database")
		graphClient, err = model.NewNeo4jClient()
	}

	if err != nil {
		log.Fatalf("Failed to create graph client: %v", err)
	}

	defer func() {
		if err := graphClient.Close(ctx); err != nil {
			log.Printf("Error closing graph database driver: %v", err)
		}
	}()

	// Initialize embedding generator if requested
	var embeddingGen *embeddings.CodeEmbeddingGenerator
	if generateEmbeddings {
		log.Printf("Setting up embedding generation (model: %s, dim: %d)...", embeddingModel, embeddingDim)

		provider, err := embeddings.NewOpenAIProvider(embeddingModel, embeddingDim)
		if err != nil {
			log.Fatalf("Failed to create embedding provider: %v", err)
		}

		embeddingGen, err = embeddings.NewCodeEmbeddingGenerator(provider, useOracle)
		if err != nil {
			log.Fatalf("Failed to create embedding generator: %v", err)
		}
		defer embeddingGen.Close()
	}

	// Create enhanced monitor configuration
	monitorConfig := monitor.EnhancedConfig{
		Config: monitor.Config{
			RootPath:     root,
			GraphClient:  graphClient,
			EmbeddingGen: embeddingGen,
		},
		EnableBatching:     enableBatch,
		BatchSize:          batchSize,
		BatchFlushInterval: time.Duration(batchInterval) * time.Second,
		EnableDiffAnalysis: enableDiff,
		EnableGit:          enableGit,
	}

	// Create the enhanced monitor
	codeMonitor, err := monitor.NewEnhancedMonitor(monitorConfig)
	if err != nil {
		log.Fatalf("Failed to create enhanced monitor: %v", err)
	}

	// Start monitoring
	if err := codeMonitor.Start(ctx); err != nil {
		log.Fatalf("Failed to start monitoring: %v", err)
	}

	// Start API server if enabled
	if apiPort > 0 {
		apiServer := api.NewMonitorAPI(codeMonitor)
		go func() {
			addr := fmt.Sprintf(":%d", apiPort)
			log.Printf("Starting API server on %s", addr)
			log.Printf("  Status: http://localhost:%d/api/v1/status", apiPort)
			log.Printf("  Stats:  http://localhost:%d/api/v1/stats", apiPort)
			log.Printf("  Files:  http://localhost:%d/api/v1/files", apiPort)

			if err := apiServer.Serve(addr); err != nil {
				log.Printf("API server error: %v", err)
			}
		}()
	}

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Print status
	log.Printf("Enhanced monitor started successfully!")
	log.Printf("Monitoring: %s", root)
	log.Printf("Features enabled:")
	if enableBatch {
		log.Printf("  - Batch processing (size: %d, interval: %ds)", batchSize, batchInterval)
	}
	if enableDiff {
		log.Printf("  - Diff analysis")
	}
	if enableGit {
		log.Printf("  - Git integration")
	}
	if apiPort > 0 {
		log.Printf("  - API server on port %d", apiPort)
	}
	log.Printf("Press Ctrl+C to stop.")

	// Wait for shutdown signal
	<-sigChan

	log.Println("Shutting down monitor...")
	if err := codeMonitor.Stop(); err != nil {
		log.Printf("Error stopping monitor: %v", err)
	}

	log.Println("Monitor stopped successfully.")
}
