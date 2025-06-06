// cmd/parse/main.go

package main

import (
	"context"
	"flag"
	"goParse/internal/driver"
	"goParse/internal/embeddings"
	"goParse/internal/model"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// GraphClient interface that both Neo4j and AGE clients implement
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

var supportedExts = map[string]bool{
	".ts":   true,
	".tsx":  true,
	".js":   true,
	".jsx":  true,
	".css":  true,
	".scss": true,
}

// extractContent extracts content from file bytes based on line numbers
func extractContent(fileContent []byte, startLine, endLine int) string {
	if startLine <= 0 || endLine <= 0 {
		return ""
	}

	lines := strings.Split(string(fileContent), "\n")
	if startLine > len(lines) {
		return ""
	}

	if endLine > len(lines) {
		endLine = len(lines)
	}

	return strings.Join(lines[startLine-1:endLine], "\n")
}

var skipDirs = map[string]bool{
	"node_modules": true,
	"out":          true,
	"dist":         true,
	"build":        true,
	".git":         true,
	".vscode-test": true,
	"resources":    true,
	"coverage":     true,
	".next":        true,
}

func main() {
	// 1) Read command-line flags
	var root string
	var createIndexes bool
	var useAGE bool
	var useOracle bool
	var generateEmbeddings bool
	var embeddingModel string
	var embeddingDim int
	flag.StringVar(&root, "root", ".", "Root directory of codebase to parse (e.g. ~/projects/vscode)")
	flag.BoolVar(&createIndexes, "create-indexes", true, "Create database indexes for better performance")
	flag.BoolVar(&useAGE, "use-age", false, "Use Apache AGE instead of Neo4j")
	flag.BoolVar(&useOracle, "use-oracle", false, "Use Oracle Graph instead of Neo4j")
	flag.BoolVar(&generateEmbeddings, "embeddings", false, "Generate embeddings for code chunks")
	flag.StringVar(&embeddingModel, "embedding-model", "text-embedding-3-small", "OpenAI embedding model to use")
	flag.IntVar(&embeddingDim, "embedding-dim", 1536, "Embedding dimension")
	flag.Parse()

	// Ensure the root path exists
	if _, err := os.Stat(root); err != nil {
		log.Fatalf("Root path does not exist: %v", err)
	}

	// 2) Connect to the appropriate graph database
	ctx := context.Background()
	var graphClient GraphClient
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

	// 3) Create indexes if requested
	if createIndexes {
		log.Println("Creating database indexes...")
		if err := graphClient.CreateIndexes(ctx); err != nil {
			log.Printf("Warning: Failed to create some indexes: %v", err)
		} else {
			log.Println("Indexes created successfully")
		}
	}

	// 4) Instantiate the Tree-sitter driver
	tsDriver := driver.NewTreeSitterDriver()

	// 5) Set up embedding generator if requested
	var embeddingGen *embeddings.CodeEmbeddingGenerator
	if generateEmbeddings {
		log.Printf("Setting up embedding generation (model: %s, dim: %d)...", embeddingModel, embeddingDim)

		// Determine which embedding store to use based on graph database choice
		useOracleEmbeddings := useOracle // Oracle Graph pairs with Oracle embeddings
		// Neo4j and Apache AGE both pair with PostgreSQL embeddings

		provider, err := embeddings.NewOpenAIProvider(embeddingModel, embeddingDim)
		if err != nil {
			log.Fatalf("Failed to create embedding provider: %v", err)
		}

		embeddingGen, err = embeddings.NewCodeEmbeddingGenerator(provider, useOracleEmbeddings)
		if err != nil {
			log.Fatalf("Failed to create embedding generator: %v", err)
		}
		defer embeddingGen.Close()

		if useOracleEmbeddings {
			log.Println("Using Oracle vector embeddings")
		} else {
			log.Println("Using PostgreSQL pgvector embeddings")
		}
	}

	// Track statistics
	stats := struct {
		Files         int
		Functions     int
		Imports       int
		Variables     int
		Types         int
		Interfaces    int
		Classes       int
		Constants     int
		JSXElements   int
		CSSRules      int
		FunctionCalls int
		TypeUsages    int
		Extends       int
		Implements    int
		Errors        int
		Embeddings    int
	}{}

	// 5) Walk the directory tree, skipping "noise" directories
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// If it's a directory to skip, return SkipDir
		if info.IsDir() {
			if skipDirs[info.Name()] {
				log.Printf("Skipping directory: %s", path)
				return filepath.SkipDir
			}
			return nil
		}

		// If not a supported extension, skip
		ext := filepath.Ext(path)
		if !supportedExts[ext] {
			return nil
		}

		// Convert to relative path for cleaner storage
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			relPath = path
		}

		// 6) Parse the file with Tree-sitter
		pf, parseErr := tsDriver.Parse(path)
		if parseErr != nil {
			log.Printf("Parse error (%s): %v", relPath, parseErr)
			stats.Errors++
			return nil // continue walking
		}

		// Update file path to relative path
		pf.FilePath = relPath

		// 7) Upsert the File node
		if err := graphClient.UpsertFile(ctx, pf.FilePath, pf.Language); err != nil {
			log.Printf("Failed to upsert file %s: %v", pf.FilePath, err)
		} else {
			stats.Files++
		}

		// 8) Upsert Imports
		for _, imp := range pf.Imports {
			imp.FilePath = relPath
			if err := graphClient.UpsertImport(ctx, imp); err != nil {
				log.Printf("Failed to upsert import %s in %s: %v", imp.Module, pf.FilePath, err)
			} else {
				stats.Imports++
			}
		}

		// 9) Upsert Functions
		for _, fn := range pf.Funcs {
			fn.FilePath = relPath
			if err := graphClient.UpsertFunction(ctx, fn); err != nil {
				log.Printf("Failed to upsert function %s in %s: %v", fn.Name, pf.FilePath, err)
			} else {
				stats.Functions++
			}
		}

		// 10) Upsert Variables
		for _, v := range pf.Variables {
			v.FilePath = relPath
			if err := graphClient.UpsertVariable(ctx, v); err != nil {
				log.Printf("Failed to upsert variable %s in %s: %v", v.Name, pf.FilePath, err)
			} else {
				stats.Variables++
			}
		}

		// 11) Upsert Types
		for _, t := range pf.Types {
			t.FilePath = relPath
			if err := graphClient.UpsertType(ctx, t); err != nil {
				log.Printf("Failed to upsert type %s in %s: %v", t.Name, pf.FilePath, err)
			} else {
				stats.Types++
			}
		}

		// 12) Upsert Interfaces
		for _, i := range pf.Interfaces {
			i.FilePath = relPath
			if err := graphClient.UpsertInterface(ctx, i); err != nil {
				log.Printf("Failed to upsert interface %s in %s: %v", i.Name, pf.FilePath, err)
			} else {
				stats.Interfaces++
			}
		}

		// 13) Upsert Classes
		for _, c := range pf.Classes {
			c.FilePath = relPath
			if err := graphClient.UpsertClass(ctx, c); err != nil {
				log.Printf("Failed to upsert class %s in %s: %v", c.Name, pf.FilePath, err)
			} else {
				stats.Classes++
			}
		}

		// 14) Upsert Constants
		for _, c := range pf.Constants {
			c.FilePath = relPath
			if err := graphClient.UpsertConstant(ctx, c); err != nil {
				log.Printf("Failed to upsert constant %s in %s: %v", c.Name, pf.FilePath, err)
			} else {
				stats.Constants++
			}
		}

		// 15) Upsert JSX Elements
		for _, jsx := range pf.JSXElements {
			jsx.FilePath = relPath
			if err := graphClient.UpsertJSXElement(ctx, jsx); err != nil {
				log.Printf("Failed to upsert JSX element %s in %s: %v", jsx.TagName, pf.FilePath, err)
			} else {
				stats.JSXElements++
			}
		}

		// 16) Upsert CSS Rules
		for _, css := range pf.CSSRules {
			css.FilePath = relPath
			if err := graphClient.UpsertCSSRule(ctx, css); err != nil {
				log.Printf("Failed to upsert CSS rule %s in %s: %v", css.Selector, pf.FilePath, err)
			} else {
				stats.CSSRules++
			}
		}

		// 17) Upsert Function Calls
		for _, fc := range pf.FunctionCalls {
			fc.CallerFile = relPath
			if fc.TargetFile != "" {
				// Convert target file to relative path
				targetRel, err := filepath.Rel(root, fc.TargetFile)
				if err == nil {
					fc.TargetFile = targetRel
				}
			}
			if err := graphClient.UpsertFunctionCall(ctx, fc); err != nil {
				log.Printf("Failed to upsert function call %s->%s in %s: %v",
					fc.CallerFunc, fc.CalledFunc, pf.FilePath, err)
			} else {
				stats.FunctionCalls++
			}
		}

		// 18) Upsert Type Usages
		for _, tu := range pf.TypeUsages {
			tu.UsingFile = relPath
			if err := graphClient.UpsertTypeUsage(ctx, tu); err != nil {
				log.Printf("Failed to upsert type usage %s in %s: %v", tu.UsedType, pf.FilePath, err)
			} else {
				stats.TypeUsages++
			}
		}

		// 19) Upsert Extends relationships
		for _, e := range pf.Extends {
			e.FilePath = relPath
			if err := graphClient.UpsertExtends(ctx, e); err != nil {
				log.Printf("Failed to upsert extends %s->%s in %s: %v",
					e.ChildName, e.ParentName, pf.FilePath, err)
			} else {
				stats.Extends++
			}
		}

		// 20) Upsert Implements relationships
		for _, i := range pf.Implements {
			i.FilePath = relPath
			if err := graphClient.UpsertImplements(ctx, i); err != nil {
				log.Printf("Failed to upsert implements %s->%s in %s: %v",
					i.ClassName, i.InterfaceName, pf.FilePath, err)
			} else {
				stats.Implements++
			}
		}

		// 21) Generate embeddings if requested
		if generateEmbeddings && embeddingGen != nil {
			// Read the file content
			fileContent, err := ioutil.ReadFile(path)
			if err != nil {
				log.Printf("Failed to read file content for embeddings %s: %v", relPath, err)
			} else {
				// Create parsed file data for embedding generation
				parsedFileData := embeddings.ParsedFileData{
					FilePath:    relPath,
					Language:    pf.Language,
					FileContent: string(fileContent),
				}

				// Convert entities to embedding format
				for _, fn := range pf.Funcs {
					parsedFileData.Functions = append(parsedFileData.Functions, embeddings.FunctionData{
						Name:      fn.Name,
						Content:   extractContent(fileContent, fn.StartLine, fn.EndLine),
						StartLine: fn.StartLine,
						EndLine:   fn.EndLine,
						Signature: fn.Signature,
						IsAsync:   fn.IsAsync,
						IsExport:  fn.IsExport,
					})
				}

				for _, class := range pf.Classes {
					parsedFileData.Classes = append(parsedFileData.Classes, embeddings.ClassData{
						Name:       class.Name,
						Content:    extractContent(fileContent, class.StartLine, class.EndLine),
						StartLine:  class.StartLine,
						EndLine:    class.EndLine,
						IsExport:   class.IsExport,
						IsAbstract: class.IsAbstract,
						Methods:    class.Methods,
					})
				}

				for _, iface := range pf.Interfaces {
					parsedFileData.Interfaces = append(parsedFileData.Interfaces, embeddings.InterfaceData{
						Name:       iface.Name,
						Content:    "", // Would need line numbers to extract
						IsExport:   iface.IsExport,
						Properties: iface.Properties,
					})
				}

				for _, typ := range pf.Types {
					parsedFileData.Types = append(parsedFileData.Types, embeddings.TypeData{
						Name:       typ.Name,
						Definition: typ.Definition,
						Kind:       typ.Kind,
						IsExport:   typ.IsExport,
					})
				}

				for _, jsx := range pf.JSXElements {
					parsedFileData.JSXElements = append(parsedFileData.JSXElements, embeddings.JSXData{
						TagName:             jsx.TagName,
						ContainingComponent: jsx.ContainingComponent,
						Props:               jsx.Props,
						Line:                jsx.Line,
					})
				}

				for _, imp := range pf.Imports {
					parsedFileData.Imports = append(parsedFileData.Imports, embeddings.ImportData{
						Module: imp.Module,
					})
				}

				// Process embeddings
				if err := embeddingGen.ProcessFile(ctx, parsedFileData); err != nil {
					log.Printf("Failed to generate embeddings for %s: %v", relPath, err)
				} else {
					// Count chunks created
					chunks := embeddings.CreateCodeChunks(parsedFileData)
					stats.Embeddings += len(chunks)
				}
			}
		}

		// Log summary for this file
		log.Printf("Processed %s: %d functions, %d imports, %d types, %d classes, %d JSX elements, %d CSS rules",
			relPath, len(pf.Funcs), len(pf.Imports), len(pf.Types), len(pf.Classes),
			len(pf.JSXElements), len(pf.CSSRules))

		return nil
	})

	if err != nil {
		log.Fatalf("Error walking directory: %v", err)
	}

	// Print final statistics
	dbType := "Neo4j"
	if useOracle {
		dbType = "Oracle Graph"
	} else if useAGE {
		dbType = "Apache AGE"
	}
	log.Printf("\n=== Parsing Complete (%s) ===", dbType)
	log.Printf("Files parsed: %d", stats.Files)
	log.Printf("Functions found: %d", stats.Functions)
	log.Printf("Imports found: %d", stats.Imports)
	log.Printf("Variables found: %d", stats.Variables)
	log.Printf("Types found: %d", stats.Types)
	log.Printf("Interfaces found: %d", stats.Interfaces)
	log.Printf("Classes found: %d", stats.Classes)
	log.Printf("Constants found: %d", stats.Constants)
	log.Printf("JSX elements found: %d", stats.JSXElements)
	log.Printf("CSS rules found: %d", stats.CSSRules)
	log.Printf("Function calls found: %d", stats.FunctionCalls)
	log.Printf("Type usages found: %d", stats.TypeUsages)
	log.Printf("Extends relationships found: %d", stats.Extends)
	log.Printf("Implements relationships found: %d", stats.Implements)
	log.Printf("Parse errors: %d", stats.Errors)

	if generateEmbeddings {
		log.Printf("\n=== Embeddings ===")
		log.Printf("Code chunks embedded: %d", stats.Embeddings)

		// Print embedding statistics
		if embeddingGen != nil {
			if embStats, err := embeddingGen.GetStats(ctx); err == nil {
				log.Printf("Embedding statistics:")
				if total, ok := embStats["total_chunks"].(int); ok {
					log.Printf("  Total chunks in store: %d", total)
				}
				if byType, ok := embStats["chunks_by_type"].(map[string]int); ok {
					log.Printf("  Chunks by type:")
					for t, count := range byType {
						log.Printf("    %s: %d", t, count)
					}
				}
				if byLang, ok := embStats["chunks_by_language"].(map[string]int); ok {
					log.Printf("  Chunks by language:")
					for lang, count := range byLang {
						log.Printf("    %s: %d", lang, count)
					}
				}
			}
		}
	}
}
