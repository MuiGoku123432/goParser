// internal/model/age_graph.go

package model

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
)

// AGEClient wraps a PostgreSQL connection with Apache AGE support
type AGEClient struct {
	db        *sql.DB
	graphName string
}

// init loads environment variables from .env (if present).
func init() {
	_ = godotenv.Load()
}

// NewAGEClient reads PG_HOST, PG_PORT, PG_USER, PG_PASS, PG_DB from env and connects.
func NewAGEClient() (*AGEClient, error) {
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

	graphName := os.Getenv("AGE_GRAPH_NAME")
	if graphName == "" {
		graphName = "code_graph"
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

	client := &AGEClient{
		db:        db,
		graphName: graphName,
	}

	// Initialize AGE
	if err := client.initializeAGE(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize AGE: %w", err)
	}

	return client, nil
}

// initializeAGE sets up Apache AGE extension and creates the graph if needed
func (c *AGEClient) initializeAGE() error {
	// Create AGE extension if not exists
	_, err := c.db.Exec("CREATE EXTENSION IF NOT EXISTS age CASCADE")
	if err != nil {
		return fmt.Errorf("failed to create AGE extension: %w", err)
	}

	// Load AGE
	_, err = c.db.Exec("LOAD 'age'")
	if err != nil {
		return fmt.Errorf("failed to load AGE: %w", err)
	}

	// Set search path
	_, err = c.db.Exec("SET search_path = ag_catalog, \"$user\", public")
	if err != nil {
		return fmt.Errorf("failed to set search path: %w", err)
	}

	// Create graph if not exists
	var exists bool
	err = c.db.QueryRow("SELECT EXISTS(SELECT 1 FROM ag_catalog.ag_graph WHERE name = $1)", c.graphName).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if graph exists: %w", err)
	}

	if !exists {
		query := fmt.Sprintf("SELECT create_graph('%s')", c.graphName)
		_, err = c.db.Exec(query)
		if err != nil {
			return fmt.Errorf("failed to create graph: %w", err)
		}
	}

	return nil
}

// Close terminates the PostgreSQL connection
func (c *AGEClient) Close(ctx context.Context) error {
	return c.db.Close()
}

// executeCypher executes a Cypher query within AGE
func (c *AGEClient) executeCypher(ctx context.Context, cypher string, params map[string]any) error {
	// Convert params to AGE format
	paramStr := ""
	if len(params) > 0 {
		var paramPairs []string
		for k, v := range params {
			paramPairs = append(paramPairs, fmt.Sprintf("%s: %s", k, c.formatValue(v)))
		}
		paramStr = fmt.Sprintf("WITH {%s} AS params ", strings.Join(paramPairs, ", "))
	}

	// Build the AGE query
	query := fmt.Sprintf(`
		SELECT * FROM cypher('%s', $$
			%s%s
		$$) as (result agtype);
	`, c.graphName, paramStr, cypher)

	_, err := c.db.ExecContext(ctx, query)
	return err
}

// formatValue formats a value for AGE Cypher queries
func (c *AGEClient) formatValue(v any) string {
	switch val := v.(type) {
	case string:
		// Escape single quotes and wrap in quotes
		escaped := strings.ReplaceAll(val, "'", "''")
		return fmt.Sprintf("'%s'", escaped)
	case int, int32, int64:
		return fmt.Sprintf("%d", val)
	case bool:
		return fmt.Sprintf("%t", val)
	case []string:
		var items []string
		for _, item := range val {
			items = append(items, c.formatValue(item))
		}
		return fmt.Sprintf("[%s]", strings.Join(items, ", "))
	case nil:
		return "null"
	default:
		return fmt.Sprintf("'%v'", val)
	}
}

// File Operations

// UpsertFile ensures a :File node exists with the given path and language
func (c *AGEClient) UpsertFile(ctx context.Context, path, language string) error {
	cypher := `
		MERGE (f:File {path: params.path})
		ON CREATE SET f.language = params.language, f.created = localdatetime()
		ON MATCH SET f.language = params.language, f.updated = localdatetime()
	`
	params := map[string]any{
		"path":     path,
		"language": language,
	}
	return c.executeCypher(ctx, cypher, params)
}

// Function Operations

// UpsertFunction ensures a :Function node exists and creates BELONGS_TO→File
func (c *AGEClient) UpsertFunction(ctx context.Context, fn FunctionEntity) error {
	cypher := `
		MERGE (func:Function {name: params.name, file: params.file})
		ON CREATE SET 
			func.startLine = params.startLine, 
			func.endLine = params.endLine,
			func.signature = params.signature,
			func.isAsync = params.isAsync,
			func.isExport = params.isExport,
			func.created = localdatetime()
		ON MATCH SET 
			func.startLine = params.startLine, 
			func.endLine = params.endLine,
			func.signature = params.signature,
			func.isAsync = params.isAsync,
			func.isExport = params.isExport,
			func.updated = localdatetime()
		WITH func
		MATCH (f:File {path: params.file})
		MERGE (func)-[:BELONGS_TO]->(f)
	`
	params := map[string]any{
		"name":      fn.Name,
		"file":      fn.FilePath,
		"startLine": fn.StartLine,
		"endLine":   fn.EndLine,
		"signature": fn.Signature,
		"isAsync":   fn.IsAsync,
		"isExport":  fn.IsExport,
	}
	return c.executeCypher(ctx, cypher, params)
}

// Import Operations

// UpsertImport ensures a :Import node exists and creates IMPORTS→File
func (c *AGEClient) UpsertImport(ctx context.Context, imp ImportEntity) error {
	cypher := `
		MERGE (i:Import {module: params.module})
		ON CREATE SET 
			i.created = localdatetime()
		ON MATCH SET 
			i.updated = localdatetime()
		WITH i
		MATCH (f:File {path: params.file})
		MERGE (f)-[r:IMPORTS]->(i)
		ON CREATE SET 
			r.importedNames = params.importedNames,
			r.isDefault = params.isDefault,
			r.isNamespace = params.isNamespace
	`
	params := map[string]any{
		"module":        imp.Module,
		"file":          imp.FilePath,
		"importedNames": imp.ImportedNames,
		"isDefault":     imp.IsDefault,
		"isNamespace":   imp.IsNamespace,
	}
	return c.executeCypher(ctx, cypher, params)
}

// Variable Operations

// UpsertVariable ensures a :Variable node exists and creates DEFINED_IN→File
func (c *AGEClient) UpsertVariable(ctx context.Context, variable VariableEntity) error {
	cypher := `
		MERGE (v:Variable {name: params.name, file: params.file})
		ON CREATE SET 
			v.type = params.type,
			v.isConst = params.isConst,
			v.isLet = params.isLet,
			v.startLine = params.startLine,
			v.created = localdatetime()
		ON MATCH SET 
			v.type = params.type,
			v.isConst = params.isConst,
			v.isLet = params.isLet,
			v.startLine = params.startLine,
			v.updated = localdatetime()
		WITH v
		MATCH (f:File {path: params.file})
		MERGE (v)-[:DEFINED_IN]->(f)
	`
	params := map[string]any{
		"name":      variable.Name,
		"file":      variable.FilePath,
		"type":      variable.Type,
		"isConst":   variable.IsConst,
		"isLet":     variable.IsLet,
		"startLine": variable.StartLine,
	}
	return c.executeCypher(ctx, cypher, params)
}

// Type Operations

// UpsertType ensures a :Type node exists and creates BELONGS_TO→File
func (c *AGEClient) UpsertType(ctx context.Context, typeEntity TypeEntity) error {
	cypher := `
		MERGE (t:Type {name: params.name, file: params.file})
		ON CREATE SET 
			t.kind = params.kind,
			t.definition = params.definition,
			t.isExport = params.isExport,
			t.created = localdatetime()
		ON MATCH SET 
			t.kind = params.kind,
			t.definition = params.definition,
			t.isExport = params.isExport,
			t.updated = localdatetime()
		WITH t
		MATCH (f:File {path: params.file})
		MERGE (t)-[:BELONGS_TO]->(f)
	`
	params := map[string]any{
		"name":       typeEntity.Name,
		"file":       typeEntity.FilePath,
		"kind":       typeEntity.Kind,
		"definition": typeEntity.Definition,
		"isExport":   typeEntity.IsExport,
	}
	return c.executeCypher(ctx, cypher, params)
}

// Interface Operations

// UpsertInterface ensures an :Interface node exists and creates BELONGS_TO→File
func (c *AGEClient) UpsertInterface(ctx context.Context, iface InterfaceEntity) error {
	cypher := `
		MERGE (i:Interface {name: params.name, file: params.file})
		ON CREATE SET 
			i.isExport = params.isExport,
			i.properties = params.properties,
			i.created = localdatetime()
		ON MATCH SET 
			i.isExport = params.isExport,
			i.properties = params.properties,
			i.updated = localdatetime()
		WITH i
		MATCH (f:File {path: params.file})
		MERGE (i)-[:BELONGS_TO]->(f)
	`
	params := map[string]any{
		"name":       iface.Name,
		"file":       iface.FilePath,
		"isExport":   iface.IsExport,
		"properties": iface.Properties,
	}
	return c.executeCypher(ctx, cypher, params)
}

// Class Operations

// UpsertClass ensures a :Class node exists and creates BELONGS_TO→File
func (c *AGEClient) UpsertClass(ctx context.Context, class ClassEntity) error {
	cypher := `
		MERGE (c:Class {name: params.name, file: params.file})
		ON CREATE SET 
			c.startLine = params.startLine,
			c.endLine = params.endLine,
			c.isExport = params.isExport,
			c.isAbstract = params.isAbstract,
			c.methods = params.methods,
			c.created = localdatetime()
		ON MATCH SET 
			c.startLine = params.startLine,
			c.endLine = params.endLine,
			c.isExport = params.isExport,
			c.isAbstract = params.isAbstract,
			c.methods = params.methods,
			c.updated = localdatetime()
		WITH c
		MATCH (f:File {path: params.file})
		MERGE (c)-[:BELONGS_TO]->(f)
	`
	params := map[string]any{
		"name":       class.Name,
		"file":       class.FilePath,
		"startLine":  class.StartLine,
		"endLine":    class.EndLine,
		"isExport":   class.IsExport,
		"isAbstract": class.IsAbstract,
		"methods":    class.Methods,
	}
	return c.executeCypher(ctx, cypher, params)
}

// Constant Operations

// UpsertConstant ensures a :Constant node exists and creates DEFINED_IN→File
func (c *AGEClient) UpsertConstant(ctx context.Context, constant ConstantEntity) error {
	cypher := `
		MERGE (c:Constant {name: params.name, file: params.file})
		ON CREATE SET 
			c.value = params.value,
			c.created = localdatetime()
		ON MATCH SET 
			c.value = params.value,
			c.updated = localdatetime()
		WITH c
		MATCH (f:File {path: params.file})
		MERGE (c)-[:DEFINED_IN]->(f)
	`
	params := map[string]any{
		"name":  constant.Name,
		"file":  constant.FilePath,
		"value": constant.Value,
	}
	return c.executeCypher(ctx, cypher, params)
}

// JSX Operations

// UpsertJSXElement ensures a :JSXElement node exists and creates relationships
func (c *AGEClient) UpsertJSXElement(ctx context.Context, jsx JSXElementEntity) error {
	// Determine if it's a custom component
	jsx.IsCustomComponent = len(jsx.TagName) > 0 && strings.ToUpper(jsx.TagName[:1]) == jsx.TagName[:1]

	cypher := `
		MERGE (jsx:JSXElement {tagName: params.tagName, file: params.file, line: params.line})
		ON CREATE SET 
			jsx.containingComponent = params.containingComponent,
			jsx.props = params.props,
			jsx.isCustomComponent = params.isCustomComponent,
			jsx.created = localdatetime()
		ON MATCH SET 
			jsx.containingComponent = params.containingComponent,
			jsx.props = params.props,
			jsx.isCustomComponent = params.isCustomComponent,
			jsx.updated = localdatetime()
		WITH jsx
		MATCH (f:File {path: params.file})
		MERGE (jsx)-[:USED_IN]->(f)
		WITH jsx, f
		WHERE jsx.containingComponent IS NOT NULL AND jsx.containingComponent <> ''
		OPTIONAL MATCH (func:Function {name: jsx.containingComponent, file: params.file})
		FOREACH (_ IN CASE WHEN func IS NOT NULL THEN [1] ELSE [] END |
			MERGE (func)-[:RENDERS]->(jsx)
		)
	`
	params := map[string]any{
		"tagName":             jsx.TagName,
		"file":                jsx.FilePath,
		"line":                jsx.Line,
		"containingComponent": jsx.ContainingComponent,
		"props":               jsx.Props,
		"isCustomComponent":   jsx.IsCustomComponent,
	}
	return c.executeCypher(ctx, cypher, params)
}

// CSS Operations

// UpsertCSSRule ensures a :CSSRule node exists and creates relationships
func (c *AGEClient) UpsertCSSRule(ctx context.Context, css CSSRuleEntity) error {
	cypher := `
		MERGE (css:CSSRule {selector: params.selector, file: params.file})
		ON CREATE SET 
			css.ruleType = params.ruleType,
			css.line = params.line,
			css.propertyName = params.propertyName,
			css.value = params.value,
			css.created = localdatetime()
		ON MATCH SET 
			css.ruleType = params.ruleType,
			css.line = params.line,
			css.propertyName = params.propertyName,
			css.value = params.value,
			css.updated = localdatetime()
		WITH css
		MATCH (f:File {path: params.file})
		MERGE (css)-[:DEFINED_IN]->(f)
	`
	params := map[string]any{
		"selector":     css.Selector,
		"file":         css.FilePath,
		"ruleType":     css.RuleType,
		"line":         css.Line,
		"propertyName": css.PropertyName,
		"value":        css.Value,
	}
	return c.executeCypher(ctx, cypher, params)
}

// Relationship Operations

// UpsertFunctionCall creates a CALLS relationship between functions
func (c *AGEClient) UpsertFunctionCall(ctx context.Context, call FunctionCallEntity) error {
	// If we have a resolved target, create a direct function-to-function relationship
	if call.ResolvedTarget != "" && call.TargetFile != "" {
		cypher := `
			MATCH (caller:Function {name: params.callerFunc, file: params.callerFile})
			MATCH (target:Function {name: params.targetFunc, file: params.targetFile})
			MERGE (caller)-[r:CALLS]->(target)
			ON CREATE SET 
				r.callLocation = params.callLocation,
				r.callContext = params.callContext,
				r.created = localdatetime()
			ON MATCH SET 
				r.callLocation = params.callLocation,
				r.callContext = params.callContext,
				r.updated = localdatetime()
		`
		params := map[string]any{
			"callerFunc":   call.CallerFunc,
			"callerFile":   call.CallerFile,
			"targetFunc":   call.ResolvedTarget,
			"targetFile":   call.TargetFile,
			"callLocation": call.CallLocation,
			"callContext":  call.CallContext,
		}
		return c.executeCypher(ctx, cypher, params)
	} else {
		// Create an unresolved call relationship
		cypher := `
			MATCH (f:File {path: params.callerFile})
			MERGE (call:UnresolvedCall {
				calledFunc: params.calledFunc, 
				callerFile: params.callerFile,
				callerFunc: params.callerFunc,
				line: params.callLocation
			})
			ON CREATE SET 
				call.callContext = params.callContext,
				call.created = localdatetime()
			WITH f, call
			MERGE (f)-[:CONTAINS_CALL]->(call)
			WITH call
			WHERE params.callerFunc IS NOT NULL AND params.callerFunc <> ''
			OPTIONAL MATCH (caller:Function {name: params.callerFunc, file: params.callerFile})
			FOREACH (_ IN CASE WHEN caller IS NOT NULL THEN [1] ELSE [] END |
				MERGE (caller)-[:MAKES_CALL]->(call)
			)
		`
		params := map[string]any{
			"callerFile":   call.CallerFile,
			"callerFunc":   call.CallerFunc,
			"calledFunc":   call.CalledFunc,
			"callLocation": call.CallLocation,
			"callContext":  call.CallContext,
		}
		return c.executeCypher(ctx, cypher, params)
	}
}

// UpsertTypeUsage creates a USES_TYPE relationship
func (c *AGEClient) UpsertTypeUsage(ctx context.Context, usage TypeUsageEntity) error {
	// Try to match the type first
	cypher := `
		MATCH (t:Type {name: params.usedType})
		WITH t
		MATCH (f:File {path: params.usingFile})
		MERGE (f)-[r:USES_TYPE]->(t)
		ON CREATE SET 
			r.context = params.context,
			r.location = params.location,
			r.usingEntity = params.usingEntity,
			r.created = localdatetime()
		ON MATCH SET 
			r.context = params.context,
			r.location = params.location,
			r.usingEntity = params.usingEntity,
			r.updated = localdatetime()
	`
	params := map[string]any{
		"usingFile":   usage.UsingFile,
		"usingEntity": usage.UsingEntity,
		"usedType":    usage.UsedType,
		"context":     usage.UsageContext,
		"location":    usage.UsageLocation,
	}

	// First try with Type nodes
	err := c.executeCypher(ctx, cypher, params)
	if err == nil {
		return nil
	}

	// If no type was matched, also check interfaces
	cypher2 := `
		MATCH (i:Interface {name: params.usedType})
		WITH i
		MATCH (f:File {path: params.usingFile})
		MERGE (f)-[r:USES_TYPE]->(i)
		ON CREATE SET 
			r.context = params.context,
			r.location = params.location,
			r.usingEntity = params.usingEntity,
			r.created = localdatetime()
		ON MATCH SET 
			r.context = params.context,
			r.location = params.location,
			r.usingEntity = params.usingEntity,
			r.updated = localdatetime()
	`
	return c.executeCypher(ctx, cypher2, params)
}

// UpsertExtends creates an EXTENDS relationship
func (c *AGEClient) UpsertExtends(ctx context.Context, extends ExtendsEntity) error {
	cypher := `
		MATCH (child {name: params.childName, file: params.file})
		WHERE child:Class OR child:Interface
		WITH child
		OPTIONAL MATCH (parent:Class {name: params.parentName})
		OPTIONAL MATCH (parentInterface:Interface {name: params.parentName})
		WITH child, COALESCE(parent, parentInterface) AS parentNode
		WHERE parentNode IS NOT NULL
		MERGE (child)-[r:EXTENDS]->(parentNode)
		ON CREATE SET r.created = localdatetime()
		ON MATCH SET r.updated = localdatetime()
	`
	params := map[string]any{
		"childName":  extends.ChildName,
		"parentName": extends.ParentName,
		"file":       extends.FilePath,
	}
	return c.executeCypher(ctx, cypher, params)
}

// UpsertImplements creates an IMPLEMENTS relationship
func (c *AGEClient) UpsertImplements(ctx context.Context, implements ImplementsEntity) error {
	cypher := `
		MATCH (class:Class {name: params.className, file: params.file})
		MATCH (interface:Interface {name: params.interfaceName})
		MERGE (class)-[r:IMPLEMENTS]->(interface)
		ON CREATE SET r.created = localdatetime()
		ON MATCH SET r.updated = localdatetime()
	`
	params := map[string]any{
		"className":     implements.ClassName,
		"interfaceName": implements.InterfaceName,
		"file":          implements.FilePath,
	}
	return c.executeCypher(ctx, cypher, params)
}

// UpsertReference creates a generic REFERENCES relationship
func (c *AGEClient) UpsertReference(ctx context.Context, ref ReferenceEntity) error {
	cypher := `
		MATCH (f:File {path: params.sourceFile})
		MERGE (ref:Reference {
			sourceFile: params.sourceFile,
			sourceEntity: params.sourceEntity,
			targetEntity: params.targetEntity,
			refType: params.refType
		})
		ON CREATE SET 
			ref.line = params.line,
			ref.created = localdatetime()
		ON MATCH SET 
			ref.line = params.line,
			ref.updated = localdatetime()
		WITH f, ref
		MERGE (f)-[:CONTAINS]->(ref)
	`
	params := map[string]any{
		"sourceFile":   ref.SourceFile,
		"sourceEntity": ref.SourceEntity,
		"targetEntity": ref.TargetEntity,
		"refType":      ref.RefType,
		"line":         ref.Line,
	}
	return c.executeCypher(ctx, cypher, params)
}

// Utility Operations

// CreateIndexes creates recommended indexes for better query performance
func (c *AGEClient) CreateIndexes(ctx context.Context) error {
	// In Apache AGE, we create indexes on the underlying PostgreSQL tables
	// First, we need to find the table names for our graph

	// Get the graph OID
	var graphOid int
	err := c.db.QueryRow("SELECT oid FROM ag_catalog.ag_graph WHERE name = $1", c.graphName).Scan(&graphOid)
	if err != nil {
		return fmt.Errorf("failed to get graph OID: %w", err)
	}

	// Create indexes for each label
	labels := []string{"File", "Function", "Import", "Type", "Class", "Interface", "JSXElement", "CSSRule", "UnresolvedCall"}
	properties := map[string][]string{
		"File":           {"path"},
		"Function":       {"name", "file"},
		"Import":         {"module"},
		"Type":           {"name"},
		"Class":          {"name"},
		"Interface":      {"name"},
		"JSXElement":     {"tagName"},
		"CSSRule":        {"selector"},
		"UnresolvedCall": {"calledFunc"},
	}

	for _, label := range labels {
		// Get label ID
		var labelId int
		err := c.db.QueryRow("SELECT id FROM ag_catalog.ag_label WHERE graph = $1 AND name = $2",
			graphOid, label).Scan(&labelId)
		if err != nil {
			// Label might not exist yet, skip
			continue
		}

		// Table name format: graph_name.label_name
		tableName := fmt.Sprintf("%s.%s", c.graphName, label)

		// Create indexes for properties
		if props, ok := properties[label]; ok {
			for _, prop := range props {
				indexName := fmt.Sprintf("idx_%s_%s_%s", c.graphName, strings.ToLower(label), prop)
				query := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s ((properties->>'%s'))",
					pq.QuoteIdentifier(indexName), tableName, prop)

				_, err := c.db.Exec(query)
				if err != nil {
					// Log but don't fail - index might already exist or table might not have data yet
					fmt.Printf("Warning: Failed to create index %s: %v\n", indexName, err)
				}
			}
		}
	}

	return nil
}
