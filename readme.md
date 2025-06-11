# goParse - Advanced Code Analysis & Graph Database System

A comprehensive code analysis tool that parses TypeScript, JavaScript, and CSS codebases using Tree-sitter, extracts code entities and relationships, stores them in graph databases, and generates semantic embeddings for intelligent code search.

## 🚀 Features

### Code Analysis
- **Multi-language Support**: TypeScript, JavaScript, JSX, TSX, CSS, SCSS
- **Comprehensive Entity Extraction**: Functions, classes, interfaces, types, variables, constants, JSX elements, CSS rules
- **Relationship Mapping**: Function calls, inheritance, type usage, imports, references
- **Advanced Parsing**: Method signatures, async functions, class hierarchies, interface implementations

### Graph Database Support
- **Neo4j**: Native Cypher queries with Bolt protocol
- **Apache AGE**: PostgreSQL extension with graph capabilities
- **Oracle Graph**: Native Oracle property graph support

### Semantic Code Search
- **Vector Embeddings**: OpenAI embedding models (text-embedding-3-small, text-embedding-3-large, text-embedding-ada-002)
- **Dual Storage**: PostgreSQL with pgvector or Oracle native VECTOR datatype
- **Hybrid Search**: Combines vector similarity with keyword matching
- **Code Chunking**: Intelligent segmentation of code entities for optimal embedding generation

### Performance & Scalability
- **Incremental Processing**: Efficient handling of large codebases
- **Batch Operations**: Optimized database insertions
- **Multiple Workers**: Concurrent processing support
- **Resource Management**: Proper connection pooling and cleanup

## 🏗️ Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Tree-sitter   │    │   Graph Store    │    │ Embedding Store │
│     Parser      │───▶│                  │    │                 │
│                 │    │ • Neo4j          │    │ • PostgreSQL    │
│ • TypeScript    │    │ • Apache AGE     │    │   (pgvector)    │
│ • JavaScript    │    │ • Oracle Graph   │    │ • Oracle        │
│ • CSS/SCSS      │    │                  │    │   (VECTOR)      │
└─────────────────┘    └──────────────────┘    └─────────────────┘
         │                       │                       │
         └───────────────────────┼───────────────────────┘
                                 ▼
                    ┌─────────────────────────┐
                    │     goParse CLI         │
                    │                         │
                    │ • Configuration         │
                    │ • Progress Tracking     │
                    │ • Statistics Reporting  │
                    └─────────────────────────┘
```

## 📦 Installation

### Prerequisites

- Go 1.21 or higher
- One or more of the following databases:
    - Neo4j (for graph storage)
    - PostgreSQL with pgvector (for embeddings and Apache AGE)
    - Oracle Database 23c+ (for Oracle Graph and native vector storage)

### Clone and Build

```bash
git clone https://github.com/yourusername/goParse.git
cd goParse
go mod tidy
go build -o goparse cmd/parse/main.go
```

### Dependencies

Create a `go.mod` file in the project root:

```go
module goParse

go 1.21

require (
    github.com/joho/godotenv v1.4.0
    github.com/lib/pq v1.10.9
    github.com/pgvector/pgvector-go v0.1.1
    github.com/neo4j/neo4j-go-driver/v5 v5.12.0
    github.com/smacker/go-tree-sitter v0.0.0-20230720070738-0d0a9f78d8f8
    github.com/smacker/go-tree-sitter/css v0.0.0-20230720070738-0d0a9f78d8f8
    github.com/smacker/go-tree-sitter/javascript v0.0.0-20230720070738-0d0a9f78d8f8
    github.com/smacker/go-tree-sitter/typescript v0.0.0-20230720070738-0d0a9f78d8f8
    github.com/godror/godror v0.40.2
)
```

## ⚙️ Configuration

### Environment Variables

Create a `.env` file in the project root:

```env
# Neo4j Configuration
NEO4J_URI=neo4j+s://your-instance.databases.neo4j.io
NEO4J_USER=neo4j
NEO4J_PASS=your-password

# PostgreSQL Configuration (for Apache AGE and pgvector)
PG_HOST=localhost
PG_PORT=5432
PG_USER=postgres
PG_PASS=your-password
PG_DB=codeanalysis
PG_EMBEDDINGS_TABLE=code_embeddings
AGE_GRAPH_NAME=code_graph

# Oracle Configuration
ORACLE_USER=your-username
ORACLE_PASS=your-password
ORACLE_DSN=your-host:1521/your-service
ORACLE_GRAPH_NAME=CODE_GRAPH
ORACLE_EMBEDDINGS_TABLE=CODE_EMBEDDINGS

# OpenAI Configuration (for embeddings)
OPENAI_API_KEY=sk-your-openai-api-key
# Optional: custom endpoint for self-hosted models
OPENAI_BASE_URL=https://api.openai.com
```

### Database Setup

#### Neo4j Setup
1. Create a Neo4j instance (local or Neo4j Aura)
2. Install APOC plugin for advanced procedures
3. Set environment variables

#### PostgreSQL with Apache AGE Setup
```sql
-- Install AGE extension
CREATE EXTENSION age;

-- Install pgvector extension
CREATE EXTENSION vector;

-- Load AGE into shared libraries
LOAD 'age';

-- Set search path
SET search_path = ag_catalog, "$user", public;
```

#### Oracle Database Setup
```sql
-- Ensure Oracle Database 23c+ with Graph and Vector features
-- Create user with appropriate privileges
CREATE USER codeanalysis IDENTIFIED BY your_password;
GRANT CONNECT, RESOURCE, UNLIMITED TABLESPACE TO codeanalysis;
GRANT CREATE PROPERTY GRAPH TO codeanalysis;
```

## 🎯 Usage

### Basic Code Analysis

```bash
# Analyze a TypeScript project with Neo4j
./goparse -root /path/to/your/project

# Use Apache AGE instead of Neo4j
./goparse -root /path/to/your/project -use-age

# Use Oracle Graph
./goparse -root /path/to/your/project -use-oracle

# Generate embeddings for semantic search
./goparse -root /path/to/your/project -embeddings

# Custom embedding model and dimension
./goparse -root /path/to/your/project -embeddings \
  -embedding-model text-embedding-3-large \
  -embedding-dim 3072
```

### Advanced Options

```bash
# Skip index creation (for subsequent runs)
./goparse -root /path/to/your/project -create-indexes=false

# Analyze specific project with embeddings
./goparse -root ~/projects/vscode \
  -embeddings \
  -embedding-model text-embedding-3-small \
  -embedding-dim 1536
```

### Command Line Options

| Flag | Default | Description |
|------|---------|-------------|
| `-root` | `.` | Root directory of codebase to parse |
| `-create-indexes` | `true` | Create database indexes for better performance |
| `-use-age` | `false` | Use Apache AGE instead of Neo4j |
| `-use-oracle` | `false` | Use Oracle Graph instead of Neo4j |
| `-embeddings` | `false` | Generate embeddings for code chunks |
| `-embedding-model` | `text-embedding-3-small` | OpenAI embedding model to use |
| `-embedding-dim` | `1536` | Embedding dimension |

## 📊 Supported File Types

| Extension | Language | Features Extracted |
|-----------|----------|-------------------|
| `.ts` | TypeScript | Functions, classes, interfaces, types, variables, imports, inheritance |
| `.tsx` | TypeScript + JSX | All TypeScript features + JSX elements and props |
| `.js` | JavaScript | Functions, classes, variables, imports, inheritance |
| `.jsx` | JavaScript + JSX | All JavaScript features + JSX elements and props |
| `.css` | CSS | Class selectors, ID selectors, CSS variables |
| `.scss` | SCSS | All CSS features + SCSS-specific syntax |

## 🔍 Query Examples

### Neo4j Cypher Queries

```cypher
-- Find all functions in a file
MATCH (f:Function)-[:BELONGS_TO]->(file:File {path: 'src/utils/helper.ts'})
RETURN f.name, f.signature, f.startLine

-- Find function call relationships
MATCH (caller:Function)-[r:CALLS]->(target:Function)
RETURN caller.name, target.name, r.callLocation

-- Find class inheritance hierarchy
MATCH (child:Class)-[:EXTENDS]->(parent:Class)
RETURN child.name, parent.name

-- Find TypeScript interface implementations
MATCH (class:Class)-[:IMPLEMENTS]->(interface:Interface)
RETURN class.name, interface.name

-- Find all JSX components in React files
MATCH (jsx:JSXElement)-[:USED_IN]->(file:File)
WHERE file.path ENDS WITH '.tsx'
RETURN jsx.tagName, jsx.containingComponent, file.path

-- Complex: Find functions that use specific types
MATCH (file:File)-[:USES_TYPE]->(type:Type {name: 'UserData'})
MATCH (func:Function)-[:BELONGS_TO]->(file)
RETURN DISTINCT func.name, func.signature, file.path
```

### PostgreSQL/AGE Queries

```sql
-- AGE queries use Cypher syntax within SQL
SELECT * FROM cypher('code_graph', $$
  MATCH (f:Function)-[:BELONGS_TO]->(file:File)
  WHERE file.path = 'src/components/App.tsx'
  RETURN f.name, f.signature
$$) AS (name agtype, signature agtype);
```

### Embedding Search Queries

```sql
-- Find similar functions (PostgreSQL with pgvector)
SELECT name, file_path, content,
       embedding <=> '[0.1,0.2,...]'::vector as distance
FROM code_embeddings
WHERE chunk_type = 'function'
ORDER BY embedding <=> '[0.1,0.2,...]'::vector
LIMIT 10;

-- Hybrid search combining similarity and keywords
SELECT name, file_path, 
       VECTOR_DISTANCE(embedding, TO_VECTOR('[0.1,0.2,...]'), COSINE) as similarity,
       CASE WHEN UPPER(content) LIKE '%ASYNC%' THEN 0.1 ELSE 0 END as keyword_boost
FROM code_embeddings
WHERE chunk_type = 'function'
ORDER BY (similarity - keyword_boost)
LIMIT 10;
```

## 📈 Performance Considerations

### Optimization Tips

1. **Index Creation**: Always run with `-create-indexes=true` on first execution
2. **Batch Size**: For large codebases, consider processing in smaller chunks
3. **Memory**: Ensure adequate RAM for embedding generation (4GB+ recommended)
4. **Database Configuration**: Tune database settings for your workload

### Performance Metrics

- **Parsing Speed**: ~1000-5000 files per minute (depends on file size and complexity)
- **Graph Insert Rate**: ~10,000 entities per minute
- **Embedding Generation**: ~100-500 chunks per minute (depends on OpenAI rate limits)

### Scaling Recommendations

| Codebase Size | Recommended Setup | Estimated Time |
|---------------|-------------------|----------------|
| < 1,000 files | Local PostgreSQL/Neo4j | 5-15 minutes |
| 1,000-10,000 files | Cloud database with connection pooling | 30-90 minutes |
| 10,000+ files | Distributed setup with multiple workers | 2-6 hours |

## 🛠️ Troubleshooting

### Common Issues

#### Database Connection Errors
```bash
# Check database connectivity
ping your-database-host

# Verify credentials
psql -h localhost -U postgres -d codeanalysis

# Check Neo4j connection
cypher-shell -a neo4j://localhost:7687 -u neo4j
```

#### Tree-sitter Parsing Errors
```bash
# Verify file encoding (must be UTF-8)
file -bi your-file.ts

# Check for syntax errors in source files
tsc --noEmit your-file.ts
```

#### Embedding Generation Issues
```bash
# Verify OpenAI API key
curl -H "Authorization: Bearer $OPENAI_API_KEY" \
  https://api.openai.com/v1/models

# Check rate limits
# OpenAI has rate limits - consider implementing backoff
```

### Memory Issues

```bash
# Increase Go memory limit
export GOMEMLIMIT=4GiB
./goparse -root /large/project

# Monitor memory usage
go tool pprof http://localhost:6060/debug/pprof/heap
```

## 🧪 Testing

```bash
# Run unit tests
go test ./...

# Run with coverage
go test -cover ./...

# Benchmark performance
go test -bench=. ./internal/driver
```

## 📚 API Documentation

### Core Interfaces

#### GraphClient Interface
```go
type GraphClient interface {
    Close(ctx context.Context) error
    CreateIndexes(ctx context.Context) error
    UpsertFile(ctx context.Context, path, language string) error
    UpsertFunction(ctx context.Context, fn FunctionEntity) error
    // ... other methods
}
```

#### EmbeddingProvider Interface
```go
type EmbeddingProvider interface {
    GenerateEmbedding(ctx context.Context, text string) ([]float32, error)
    GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error)
    GetDimension() int
}
```

### Data Models

#### Core Entities
- `FunctionEntity`: Function definitions with signatures and metadata
- `ClassEntity`: Class definitions with inheritance information
- `InterfaceEntity`: Interface definitions with properties
- `TypeEntity`: Type aliases and definitions
- `ImportEntity`: Import statements with imported names
- `JSXElementEntity`: JSX elements with props and component context
- `CSSRuleEntity`: CSS rules with selectors and properties

#### Relationships
- `FunctionCallEntity`: Function call relationships
- `TypeUsageEntity`: Type usage in various contexts
- `ExtendsEntity`: Class/interface inheritance
- `ImplementsEntity`: Interface implementations

## 🤝 Contributing

### Development Setup

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Ensure all tests pass
6. Submit a pull request

### Code Style

- Follow Go conventions and `gofmt` formatting
- Add comprehensive comments for public APIs
- Use meaningful variable and function names
- Implement proper error handling

### Adding New Language Support

1. Add Tree-sitter grammar dependency
2. Implement parsing logic in `treesitter_driver.go`
3. Add language-specific entity extraction
4. Update file extension mapping
5. Add tests for the new language

---

**Happy Code Analysis!** 🚀