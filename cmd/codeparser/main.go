// cmd/parse/main.go

package main

import (
	"context"
	"flag"
	"goParse/internal/driver"
	"goParse/internal/model"
	"log"
	"os"
	"path/filepath"
)

var supportedExts = map[string]bool{
	".ts":   true,
	".tsx":  true,
	".js":   true,
	".jsx":  true,
	".css":  true,
	".scss": true,
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
	flag.StringVar(&root, "root", ".", "Root directory of codebase to parse (e.g. ~/projects/vscode)")
	flag.BoolVar(&createIndexes, "create-indexes", true, "Create Neo4j indexes for better performance")
	flag.Parse()

	// Ensure the root path exists
	if _, err := os.Stat(root); err != nil {
		log.Fatalf("Root path does not exist: %v", err)
	}

	// 2) Connect to Neo4j Aura using credentials from .env or env vars
	ctx := context.Background()
	neo4jClient, err := model.NewNeo4jClient()
	if err != nil {
		log.Fatalf("Failed to create Neo4j client: %v", err)
	}
	defer func() {
		if err := neo4jClient.Close(ctx); err != nil {
			log.Printf("Error closing Neo4j driver: %v", err)
		}
	}()

	// 3) Create indexes if requested
	if createIndexes {
		log.Println("Creating Neo4j indexes...")
		if err := neo4jClient.CreateIndexes(ctx); err != nil {
			log.Printf("Warning: Failed to create some indexes: %v", err)
		} else {
			log.Println("Indexes created successfully")
		}
	}

	// 4) Instantiate the Tree-sitter driver
	tsDriver := driver.NewTreeSitterDriver()

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
		if err := neo4jClient.UpsertFile(ctx, pf.FilePath, pf.Language); err != nil {
			log.Printf("Failed to upsert file %s: %v", pf.FilePath, err)
		} else {
			stats.Files++
		}

		// 8) Upsert Imports
		for _, imp := range pf.Imports {
			imp.FilePath = relPath
			if err := neo4jClient.UpsertImport(ctx, imp); err != nil {
				log.Printf("Failed to upsert import %s in %s: %v", imp.Module, pf.FilePath, err)
			} else {
				stats.Imports++
			}
		}

		// 9) Upsert Functions
		for _, fn := range pf.Funcs {
			fn.FilePath = relPath
			if err := neo4jClient.UpsertFunction(ctx, fn); err != nil {
				log.Printf("Failed to upsert function %s in %s: %v", fn.Name, pf.FilePath, err)
			} else {
				stats.Functions++
			}
		}

		// 10) Upsert Variables
		for _, v := range pf.Variables {
			v.FilePath = relPath
			if err := neo4jClient.UpsertVariable(ctx, v); err != nil {
				log.Printf("Failed to upsert variable %s in %s: %v", v.Name, pf.FilePath, err)
			} else {
				stats.Variables++
			}
		}

		// 11) Upsert Types
		for _, t := range pf.Types {
			t.FilePath = relPath
			if err := neo4jClient.UpsertType(ctx, t); err != nil {
				log.Printf("Failed to upsert type %s in %s: %v", t.Name, pf.FilePath, err)
			} else {
				stats.Types++
			}
		}

		// 12) Upsert Interfaces
		for _, i := range pf.Interfaces {
			i.FilePath = relPath
			if err := neo4jClient.UpsertInterface(ctx, i); err != nil {
				log.Printf("Failed to upsert interface %s in %s: %v", i.Name, pf.FilePath, err)
			} else {
				stats.Interfaces++
			}
		}

		// 13) Upsert Classes
		for _, c := range pf.Classes {
			c.FilePath = relPath
			if err := neo4jClient.UpsertClass(ctx, c); err != nil {
				log.Printf("Failed to upsert class %s in %s: %v", c.Name, pf.FilePath, err)
			} else {
				stats.Classes++
			}
		}

		// 14) Upsert Constants
		for _, c := range pf.Constants {
			c.FilePath = relPath
			if err := neo4jClient.UpsertConstant(ctx, c); err != nil {
				log.Printf("Failed to upsert constant %s in %s: %v", c.Name, pf.FilePath, err)
			} else {
				stats.Constants++
			}
		}

		// 15) Upsert JSX Elements
		for _, jsx := range pf.JSXElements {
			jsx.FilePath = relPath
			if err := neo4jClient.UpsertJSXElement(ctx, jsx); err != nil {
				log.Printf("Failed to upsert JSX element %s in %s: %v", jsx.TagName, pf.FilePath, err)
			} else {
				stats.JSXElements++
			}
		}

		// 16) Upsert CSS Rules
		for _, css := range pf.CSSRules {
			css.FilePath = relPath
			if err := neo4jClient.UpsertCSSRule(ctx, css); err != nil {
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
			if err := neo4jClient.UpsertFunctionCall(ctx, fc); err != nil {
				log.Printf("Failed to upsert function call %s->%s in %s: %v",
					fc.CallerFunc, fc.CalledFunc, pf.FilePath, err)
			} else {
				stats.FunctionCalls++
			}
		}

		// 18) Upsert Type Usages
		for _, tu := range pf.TypeUsages {
			tu.UsingFile = relPath
			if err := neo4jClient.UpsertTypeUsage(ctx, tu); err != nil {
				log.Printf("Failed to upsert type usage %s in %s: %v", tu.UsedType, pf.FilePath, err)
			} else {
				stats.TypeUsages++
			}
		}

		// 19) Upsert Extends relationships
		for _, e := range pf.Extends {
			e.FilePath = relPath
			if err := neo4jClient.UpsertExtends(ctx, e); err != nil {
				log.Printf("Failed to upsert extends %s->%s in %s: %v",
					e.ChildName, e.ParentName, pf.FilePath, err)
			} else {
				stats.Extends++
			}
		}

		// 20) Upsert Implements relationships
		for _, i := range pf.Implements {
			i.FilePath = relPath
			if err := neo4jClient.UpsertImplements(ctx, i); err != nil {
				log.Printf("Failed to upsert implements %s->%s in %s: %v",
					i.ClassName, i.InterfaceName, pf.FilePath, err)
			} else {
				stats.Implements++
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
	log.Println("\n=== Parsing Complete ===")
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
}
