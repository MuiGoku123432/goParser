// cmd/monitor/main.go

package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"goParse/internal/embeddings"
	"goParse/internal/model"
	"goParse/internal/monitor"
)

// GraphClient interface that both Neo4j, AGE, and Oracle clients implement
type GraphClient interface {
	Close(ctx context.Context) error
	CreateIndexes(ctx context.Context) error
	UpsertFile(ctx context.Context, path, language string) error
	UpsertFunction(ctx context.Context, fn model.FunctionEntity) error
	UpsertImport(ctx context.Context, imp model.ImportEntity) error
	UpsertVariable(ctx context.Context, variable model.VariableEntity) error
	UpsertType(ctx context.Context, typeEntity model.TypeEntity) error
	UpsertInterface(ctx context.Context, iface model.InterfaceEntity) error
	UpsertClass(ctx context.Context, class model.ClassEntity) error
	UpsertConstant(ctx context.Context, constant model.ConstantEntity) error
	UpsertJSXElement(ctx context.Context, jsx model.JSXElementEntity) error
	UpsertCSSRule(ctx context.Context, css model.CSSRuleEntity) error
	UpsertFunctionCall(ctx context.Context, call model.FunctionCallEntity) error
	UpsertTypeUsage(ctx context.Context, usage model.TypeUsageEntity) error
	UpsertExtends(ctx context.Context, extends model.ExtendsEntity) error
	UpsertImplements(ctx context.Context, implements model.ImplementsEntity) error
	UpsertReference(ctx context.Context, ref model.ReferenceEntity) error
}

func main() {
	var root string
	var useAGE bool
	var useOracle bool
	var generateEmbeddings bool
	var embeddingModel string
	var embeddingDim int
	var pollInterval int

	flag.StringVar(&root, "root", ".", "Root directory of codebase to monitor")
	flag.BoolVar(&useAGE, "use-age", false, "Use Apache AGE instead of Neo4j")
	flag.BoolVar(&useOracle, "use-oracle", false, "Use Oracle Graph instead of Neo4j")
	flag.BoolVar(&generateEmbeddings, "embeddings", false, "Generate embeddings for code chunks")
	flag.StringVar(&embeddingModel, "embedding-model", "text-embedding-3-small", "OpenAI embedding model to use")
	flag.IntVar(&embeddingDim, "embedding-dim", 1536, "Embedding dimension")
	flag.IntVar(&pollInterval, "poll-interval", 5, "Polling interval in seconds (0 to disable polling)")
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
		if err != nil {
			log.Fatalf("Failed to create Oracle Graph client: %v", err)
		}
	} else if useAGE {
		log.Println("Using Apache AGE graph database")
		graphClient, err = model.NewAGEClient()
		if err != nil {
			log.Fatalf("Failed to create AGE client: %v", err)
		}
	} else {
		log.Println("Using Neo4j graph database")
		graphClient, err = model.NewNeo4jClient()
		if err != nil {
			log.Fatalf("Failed to create Neo4j client: %v", err)
		}
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

	// Create monitor
	monitorConfig := monitor.Config{
		RootPath:     root,
		GraphClient:  graphClient,
		EmbeddingGen: embeddingGen,
	}

	codeMonitor, err := monitor.NewMonitor(monitorConfig)
	if err != nil {
		log.Fatalf("Failed to create monitor: %v", err)
	}

	// Start monitoring
	if err := codeMonitor.Start(ctx); err != nil {
		log.Fatalf("Failed to start monitor: %v", err)
	}

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	log.Printf("Monitoring %s for changes. Press Ctrl+C to stop.", root)
	<-sigChan

	log.Println("Shutting down monitor...")
	if err := codeMonitor.Stop(); err != nil {
		log.Printf("Error stopping monitor: %v", err)
	}
}
