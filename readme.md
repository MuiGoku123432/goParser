# Go Code Parser for Neo4j

A Go-based code parser that uses Tree-sitter to analyze TypeScript, JavaScript, and CSS codebases and stores the results in a Neo4j graph database.

## Features

The parser extracts and creates nodes for:
- **Files** - Source code files with their paths and languages
- **Functions** - Function declarations, methods, and arrow functions
- **Imports** - ES6 imports and CommonJS requires
- **Variables** - Variable declarations (excluding function assignments)
- **Types** - TypeScript type aliases
- **Interfaces** - TypeScript interfaces
- **Classes** - ES6 classes
- **Constants** - Const declarations

And creates relationships for:
- **BELONGS_TO** - Functions, Types, Interfaces, Classes belong to Files
- **IMPORTS** - Files import modules
- **DEFINED_IN** - Variables and Constants are defined in Files
- **CALLS** - Function call relationships (work in progress)
- **USES_TYPE** - Type usage relationships (TypeScript only)
- **EXTENDS** - Class/Interface inheritance (work in progress)
- **IMPLEMENTS** - Class implements Interface (work in progress)

## Setup

1. Install dependencies:
```bash
go mod init goParse
go get github.com/neo4j/neo4j-go-driver/v5
go get github.com/smacker/go-tree-sitter
go get github.com/joho/godotenv
```

2. Set up Neo4j credentials in `.env` file:
```
NEO4J_URI=neo4j+s://your-instance.databases.neo4j.io
NEO4J_USER=neo4j
NEO4J_PASS=your-password
```

3. Build and run:
```bash
# Parse a codebase
go run cmd/parse/main.go -root ~/projects/your-typescript-project

# Or build first
go build -o codeparser cmd/parse/main.go
./codeparser -root ~/projects/your-typescript-project
```

## Troubleshooting

### Parse Errors

If you encounter parse errors like "failed to compile class query", run the diagnostic tool:

```bash
go run cmd/diagnose/main.go -file path/to/problematic/file.ts
```

This will show you:
- The AST structure Tree-sitter generates
- Which queries work and which fail
- The actual node names used by the parser

### Common Issues

1. **Query compilation errors**: The tree-sitter-go library has specific query syntax requirements. If advanced queries fail, we fall back to simpler queries and manually traverse the AST.

2. **Missing entities**: Some TypeScript-specific constructs may not be fully supported by the grammar. Check the diagnostic output to see what's available.

3. **Relationship creation failures**: These often occur when the target node doesn't exist yet. The parser continues on errors, so you may need to run it twice to establish all relationships.

## Neo4j Queries

Once your code is parsed, you can query it in Neo4j:

```cypher
// Find all functions in a file
MATCH (f:Function)-[:BELONGS_TO]->(file:File {path: "/path/to/file.ts"})
RETURN f.name, f.startLine, f.endLine

// Find all imports of a specific module
MATCH (file:File)-[:IMPORTS]->(i:Import {module: "react"})
RETURN file.path

// Find all classes and their methods
MATCH (c:Class)-[:BELONGS_TO]->(file:File)
OPTIONAL MATCH (f:Function)-[:BELONGS_TO]->(file)
WHERE f.name CONTAINS c.name
RETURN c.name, collect(f.name) as methods, file.path

// Find call relationships (when implemented)
MATCH (caller:Function)-[:CALLS]->(callee:Function)
RETURN caller.name, callee.name
```

## Extending the Parser

To add support for new languages or constructs:

1. Add the language grammar to the imports
2. Update `supportedExts` map
3. Add extraction methods following the pattern in `treesitter_driver.go`
4. Add new entity types to `model/graph.go`
5. Update `main.go` to call the new upsert methods

## Current Limitations

- Function call relationships are captured but may not correctly link to the target function
- Type usage tracking is basic and may miss complex scenarios
- Extends/Implements relationships need more work to properly resolve
- No support for parsing JSX/TSX specific constructs (treated as JS/TS)
- CSS/SCSS files are recognized but no entities are extracted