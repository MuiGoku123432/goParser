// internal/model/graph.go

package model

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Entity Types

// FunctionEntity represents a :Function node in Neo4j.
type FunctionEntity struct {
	Name      string
	FilePath  string
	StartLine int
	EndLine   int
	Signature string // Function signature with parameters
	IsAsync   bool
	IsExport  bool
}

// ImportEntity represents a :Import node in Neo4j.
type ImportEntity struct {
	Module        string
	FilePath      string
	ImportedNames []string // Names of imported items
	IsDefault     bool
	IsNamespace   bool
}

// VariableEntity represents a :Variable node in Neo4j.
type VariableEntity struct {
	Name      string
	FilePath  string
	Type      string
	IsConst   bool
	IsLet     bool
	StartLine int
}

// TypeEntity represents a :Type node in Neo4j (for type aliases, structs, etc.).
type TypeEntity struct {
	Name       string
	FilePath   string
	Kind       string // "type_alias", "enum", etc.
	Definition string // The full type definition
	IsExport   bool
}

// InterfaceEntity represents an :Interface node in Neo4j.
type InterfaceEntity struct {
	Name       string
	FilePath   string
	IsExport   bool
	Properties []string // List of property names
}

// ClassEntity represents a :Class node in Neo4j.
type ClassEntity struct {
	Name       string
	FilePath   string
	StartLine  int
	EndLine    int
	IsExport   bool
	IsAbstract bool
	Methods    []string // List of method names
}

// ConstantEntity represents a :Constant node in Neo4j.
type ConstantEntity struct {
	Name     string
	FilePath string
	Value    string // String representation of the value
}

// JSXElementEntity represents a :JSXElement node in Neo4j.
type JSXElementEntity struct {
	TagName             string
	FilePath            string
	ContainingComponent string   // The component/function containing this JSX
	Props               []string // List of prop names
	Line                int
	IsCustomComponent   bool // true if TagName starts with uppercase
}

// CSSRuleEntity represents a :CSSRule node in Neo4j.
type CSSRuleEntity struct {
	Selector     string
	RuleType     string // "class", "id", "element", "attribute", "pseudo", "variable"
	FilePath     string
	Line         int
	PropertyName string // For CSS variables
	Value        string // For CSS variables
}

// Relationship Types

// FunctionCallEntity represents a CALLS relationship.
type FunctionCallEntity struct {
	CallerFile     string
	CallerFunc     string // The function making the call
	CalledFunc     string // The function being called
	CallLocation   int    // Line number
	CallContext    string // For method calls, the object/class context
	ResolvedTarget string // The resolved function name if found
	TargetFile     string // The file containing the target function
}

// TypeUsageEntity represents a USES_TYPE relationship.
type TypeUsageEntity struct {
	UsingFile     string
	UsingEntity   string // The entity using the type (e.g., "function:getName")
	UsedType      string
	UsageContext  string // "parameter", "return_type", "variable", "property", etc.
	UsageLocation int    // Line number
}

// ExtendsEntity represents an EXTENDS relationship.
type ExtendsEntity struct {
	ChildName  string
	ParentName string
	FilePath   string
}

// ImplementsEntity represents an IMPLEMENTS relationship.
type ImplementsEntity struct {
	ClassName     string
	InterfaceName string
	FilePath      string
}

// ReferenceEntity represents a generic REFERENCES relationship.
type ReferenceEntity struct {
	SourceFile   string
	SourceEntity string
	TargetEntity string
	RefType      string // "uses", "instantiates", "exports", etc.
	Line         int
}

// Neo4jClient wraps a Bolt driver connected to Aura.
type Neo4jClient struct {
	driver neo4j.DriverWithContext
}

// init loads environment variables from .env (if present).
func init() {
	_ = godotenv.Load()
}

// NewNeo4jClient reads NEO4J_URI, NEO4J_USER, NEO4J_PASS from env and connects.
func NewNeo4jClient() (*Neo4jClient, error) {
	uri := os.Getenv("NEO4J_URI")
	user := os.Getenv("NEO4J_USER")
	pass := os.Getenv("NEO4J_PASS")

	if uri == "" || user == "" || pass == "" {
		return nil, fmt.Errorf("NEO4J_URI, NEO4J_USER, and NEO4J_PASS environment variables must be set")
	}

	auth := neo4j.BasicAuth(user, pass, "")
	driver, err := neo4j.NewDriverWithContext(uri, auth, func(cfg *neo4j.Config) {
		cfg.MaxConnectionPoolSize = 50
		cfg.SocketConnectTimeout = 5 * time.Second
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Neo4j driver: %w", err)
	}

	return &Neo4jClient{driver: driver}, nil
}

// Close terminates the Neo4j driver connection.
func (c *Neo4jClient) Close(ctx context.Context) error {
	return c.driver.Close(ctx)
}

// File Operations

// UpsertFile ensures a :File node exists with the given path and language.
func (c *Neo4jClient) UpsertFile(ctx context.Context, path, language string) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
        MERGE (f:File {path: $path})
        ON CREATE SET f.language = $language, f.created = datetime()
        ON MATCH SET f.language = $language, f.updated = datetime()
        `
		params := map[string]any{
			"path":     path,
			"language": language,
		}
		_, err := tx.Run(ctx, cypher, params)
		return nil, err
	})
	return err
}

// Function Operations

// UpsertFunction ensures a :Function node exists and creates BELONGS_TO→File.
func (c *Neo4jClient) UpsertFunction(ctx context.Context, fn FunctionEntity) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
        MERGE (func:Function {name: $name, file: $file})
        ON CREATE SET 
            func.startLine = $startLine, 
            func.endLine = $endLine,
            func.signature = $signature,
            func.isAsync = $isAsync,
            func.isExport = $isExport,
            func.created = datetime()
        ON MATCH SET 
            func.startLine = $startLine, 
            func.endLine = $endLine,
            func.signature = $signature,
            func.isAsync = $isAsync,
            func.isExport = $isExport,
            func.updated = datetime()
        WITH func
        MATCH (f:File {path: $file})
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
		_, err := tx.Run(ctx, cypher, params)
		return nil, err
	})
	return err
}

// Import Operations

// UpsertImport ensures a :Import node exists and creates IMPORTS→File.
func (c *Neo4jClient) UpsertImport(ctx context.Context, imp ImportEntity) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
        MERGE (i:Import {module: $module})
        ON CREATE SET 
            i.created = datetime()
        ON MATCH SET 
            i.updated = datetime()
        WITH i
        MATCH (f:File {path: $file})
        MERGE (f)-[r:IMPORTS]->(i)
        ON CREATE SET 
            r.importedNames = $importedNames,
            r.isDefault = $isDefault,
            r.isNamespace = $isNamespace
        `
		params := map[string]any{
			"module":        imp.Module,
			"file":          imp.FilePath,
			"importedNames": imp.ImportedNames,
			"isDefault":     imp.IsDefault,
			"isNamespace":   imp.IsNamespace,
		}
		_, err := tx.Run(ctx, cypher, params)
		return nil, err
	})
	return err
}

// Variable Operations

// UpsertVariable ensures a :Variable node exists and creates DEFINED_IN→File.
func (c *Neo4jClient) UpsertVariable(ctx context.Context, variable VariableEntity) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
        MERGE (v:Variable {name: $name, file: $file})
        ON CREATE SET 
            v.type = $type,
            v.isConst = $isConst,
            v.isLet = $isLet,
            v.startLine = $startLine,
            v.created = datetime()
        ON MATCH SET 
            v.type = $type,
            v.isConst = $isConst,
            v.isLet = $isLet,
            v.startLine = $startLine,
            v.updated = datetime()
        WITH v
        MATCH (f:File {path: $file})
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
		_, err := tx.Run(ctx, cypher, params)
		return nil, err
	})
	return err
}

// Type Operations

// UpsertType ensures a :Type node exists and creates BELONGS_TO→File.
func (c *Neo4jClient) UpsertType(ctx context.Context, typeEntity TypeEntity) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
        MERGE (t:Type {name: $name, file: $file})
        ON CREATE SET 
            t.kind = $kind,
            t.definition = $definition,
            t.isExport = $isExport,
            t.created = datetime()
        ON MATCH SET 
            t.kind = $kind,
            t.definition = $definition,
            t.isExport = $isExport,
            t.updated = datetime()
        WITH t
        MATCH (f:File {path: $file})
        MERGE (t)-[:BELONGS_TO]->(f)
        `
		params := map[string]any{
			"name":       typeEntity.Name,
			"file":       typeEntity.FilePath,
			"kind":       typeEntity.Kind,
			"definition": typeEntity.Definition,
			"isExport":   typeEntity.IsExport,
		}
		_, err := tx.Run(ctx, cypher, params)
		return nil, err
	})
	return err
}

// Interface Operations

// UpsertInterface ensures an :Interface node exists and creates BELONGS_TO→File.
func (c *Neo4jClient) UpsertInterface(ctx context.Context, iface InterfaceEntity) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
        MERGE (i:Interface {name: $name, file: $file})
        ON CREATE SET 
            i.isExport = $isExport,
            i.properties = $properties,
            i.created = datetime()
        ON MATCH SET 
            i.isExport = $isExport,
            i.properties = $properties,
            i.updated = datetime()
        WITH i
        MATCH (f:File {path: $file})
        MERGE (i)-[:BELONGS_TO]->(f)
        `
		params := map[string]any{
			"name":       iface.Name,
			"file":       iface.FilePath,
			"isExport":   iface.IsExport,
			"properties": iface.Properties,
		}
		_, err := tx.Run(ctx, cypher, params)
		return nil, err
	})
	return err
}

// Class Operations

// UpsertClass ensures a :Class node exists and creates BELONGS_TO→File.
func (c *Neo4jClient) UpsertClass(ctx context.Context, class ClassEntity) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
        MERGE (c:Class {name: $name, file: $file})
        ON CREATE SET 
            c.startLine = $startLine,
            c.endLine = $endLine,
            c.isExport = $isExport,
            c.isAbstract = $isAbstract,
            c.methods = $methods,
            c.created = datetime()
        ON MATCH SET 
            c.startLine = $startLine,
            c.endLine = $endLine,
            c.isExport = $isExport,
            c.isAbstract = $isAbstract,
            c.methods = $methods,
            c.updated = datetime()
        WITH c
        MATCH (f:File {path: $file})
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
		_, err := tx.Run(ctx, cypher, params)
		return nil, err
	})
	return err
}

// Constant Operations

// UpsertConstant ensures a :Constant node exists and creates DEFINED_IN→File.
func (c *Neo4jClient) UpsertConstant(ctx context.Context, constant ConstantEntity) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
        MERGE (c:Constant {name: $name, file: $file})
        ON CREATE SET 
            c.value = $value,
            c.created = datetime()
        ON MATCH SET 
            c.value = $value,
            c.updated = datetime()
        WITH c
        MATCH (f:File {path: $file})
        MERGE (c)-[:DEFINED_IN]->(f)
        `
		params := map[string]any{
			"name":  constant.Name,
			"file":  constant.FilePath,
			"value": constant.Value,
		}
		_, err := tx.Run(ctx, cypher, params)
		return nil, err
	})
	return err
}

// JSX Operations

// UpsertJSXElement ensures a :JSXElement node exists and creates relationships.
func (c *Neo4jClient) UpsertJSXElement(ctx context.Context, jsx JSXElementEntity) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		// Determine if it's a custom component
		jsx.IsCustomComponent = len(jsx.TagName) > 0 && strings.ToUpper(jsx.TagName[:1]) == jsx.TagName[:1]

		cypher := `
        MERGE (jsx:JSXElement {tagName: $tagName, file: $file, line: $line})
        ON CREATE SET 
            jsx.containingComponent = $containingComponent,
            jsx.props = $props,
            jsx.isCustomComponent = $isCustomComponent,
            jsx.created = datetime()
        ON MATCH SET 
            jsx.containingComponent = $containingComponent,
            jsx.props = $props,
            jsx.isCustomComponent = $isCustomComponent,
            jsx.updated = datetime()
        WITH jsx
        MATCH (f:File {path: $file})
        MERGE (jsx)-[:USED_IN]->(f)
        WITH jsx, f
        WHERE jsx.containingComponent IS NOT NULL AND jsx.containingComponent <> ''
        OPTIONAL MATCH (func:Function {name: jsx.containingComponent, file: $file})
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
		_, err := tx.Run(ctx, cypher, params)
		return nil, err
	})
	return err
}

// CSS Operations

// UpsertCSSRule ensures a :CSSRule node exists and creates relationships.
func (c *Neo4jClient) UpsertCSSRule(ctx context.Context, css CSSRuleEntity) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
        MERGE (css:CSSRule {selector: $selector, file: $file})
        ON CREATE SET 
            css.ruleType = $ruleType,
            css.line = $line,
            css.propertyName = $propertyName,
            css.value = $value,
            css.created = datetime()
        ON MATCH SET 
            css.ruleType = $ruleType,
            css.line = $line,
            css.propertyName = $propertyName,
            css.value = $value,
            css.updated = datetime()
        WITH css
        MATCH (f:File {path: $file})
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
		_, err := tx.Run(ctx, cypher, params)
		return nil, err
	})
	return err
}

// Relationship Operations

// UpsertFunctionCall creates a CALLS relationship between functions.
func (c *Neo4jClient) UpsertFunctionCall(ctx context.Context, call FunctionCallEntity) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		// If we have a resolved target, create a direct function-to-function relationship
		if call.ResolvedTarget != "" && call.TargetFile != "" {
			cypher := `
            MATCH (caller:Function {name: $callerFunc, file: $callerFile})
            MATCH (target:Function {name: $targetFunc, file: $targetFile})
            MERGE (caller)-[r:CALLS]->(target)
            ON CREATE SET 
                r.callLocation = $callLocation,
                r.callContext = $callContext,
                r.created = datetime()
            ON MATCH SET 
                r.callLocation = $callLocation,
                r.callContext = $callContext,
                r.updated = datetime()
            `
			params := map[string]any{
				"callerFunc":   call.CallerFunc,
				"callerFile":   call.CallerFile,
				"targetFunc":   call.ResolvedTarget,
				"targetFile":   call.TargetFile,
				"callLocation": call.CallLocation,
				"callContext":  call.CallContext,
			}
			_, err := tx.Run(ctx, cypher, params)
			return nil, err
		} else {
			// Create an unresolved call relationship
			cypher := `
            MATCH (f:File {path: $callerFile})
            MERGE (call:UnresolvedCall {
                calledFunc: $calledFunc, 
                callerFile: $callerFile,
                callerFunc: $callerFunc,
                line: $callLocation
            })
            ON CREATE SET 
                call.callContext = $callContext,
                call.created = datetime()
            WITH f, call
            MERGE (f)-[:CONTAINS_CALL]->(call)
            WITH call
            WHERE $callerFunc IS NOT NULL AND $callerFunc <> ''
            OPTIONAL MATCH (caller:Function {name: $callerFunc, file: $callerFile})
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
			_, err := tx.Run(ctx, cypher, params)
			return nil, err
		}
	})
	return err
}

// UpsertTypeUsage creates a USES_TYPE relationship.
func (c *Neo4jClient) UpsertTypeUsage(ctx context.Context, usage TypeUsageEntity) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		// Try to match the type first
		cypher := `
        MATCH (t:Type {name: $usedType})
        WITH t
        MATCH (f:File {path: $usingFile})
        MERGE (f)-[r:USES_TYPE]->(t)
        ON CREATE SET 
            r.context = $context,
            r.location = $location,
            r.usingEntity = $usingEntity,
            r.created = datetime()
        ON MATCH SET 
            r.context = $context,
            r.location = $location,
            r.usingEntity = $usingEntity,
            r.updated = datetime()
        RETURN count(r) as created
        `
		params := map[string]any{
			"usingFile":   usage.UsingFile,
			"usingEntity": usage.UsingEntity,
			"usedType":    usage.UsedType,
			"context":     usage.UsageContext,
			"location":    usage.UsageLocation,
		}
		result, err := tx.Run(ctx, cypher, params)
		if err != nil {
			return nil, err
		}

		// Check if any rows were created/matched
		var created int64
		if result.Next(ctx) {
			record := result.Record()
			if count, ok := record.Get("created"); ok {
				created = count.(int64)
			}
		}

		// If no type was matched, also check interfaces
		if created == 0 {
			cypher2 := `
            MATCH (i:Interface {name: $usedType})
            WITH i
            MATCH (f:File {path: $usingFile})
            MERGE (f)-[r:USES_TYPE]->(i)
            ON CREATE SET 
                r.context = $context,
                r.location = $location,
                r.usingEntity = $usingEntity,
                r.created = datetime()
            ON MATCH SET 
                r.context = $context,
                r.location = $location,
                r.usingEntity = $usingEntity,
                r.updated = datetime()
            `
			_, err2 := tx.Run(ctx, cypher2, params)
			if err2 != nil {
				return nil, err2
			}
		}

		return nil, nil
	})
	return err
}

// UpsertExtends creates an EXTENDS relationship.
func (c *Neo4jClient) UpsertExtends(ctx context.Context, extends ExtendsEntity) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
        MATCH (child {name: $childName, file: $file})
        WHERE (child:Class OR child:Interface)
        WITH child
        OPTIONAL MATCH (parent:Class {name: $parentName})
        OPTIONAL MATCH (parentInterface:Interface {name: $parentName})
        WITH child, COALESCE(parent, parentInterface) AS parentNode
        WHERE parentNode IS NOT NULL
        MERGE (child)-[r:EXTENDS]->(parentNode)
        ON CREATE SET r.created = datetime()
        ON MATCH SET r.updated = datetime()
        `
		params := map[string]any{
			"childName":  extends.ChildName,
			"parentName": extends.ParentName,
			"file":       extends.FilePath,
		}
		_, err := tx.Run(ctx, cypher, params)
		return nil, err
	})
	return err
}

// UpsertImplements creates an IMPLEMENTS relationship.
func (c *Neo4jClient) UpsertImplements(ctx context.Context, implements ImplementsEntity) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
        MATCH (class:Class {name: $className, file: $file})
        MATCH (interface:Interface {name: $interfaceName})
        MERGE (class)-[r:IMPLEMENTS]->(interface)
        ON CREATE SET r.created = datetime()
        ON MATCH SET r.updated = datetime()
        `
		params := map[string]any{
			"className":     implements.ClassName,
			"interfaceName": implements.InterfaceName,
			"file":          implements.FilePath,
		}
		_, err := tx.Run(ctx, cypher, params)
		return nil, err
	})
	return err
}

// UpsertReference creates a generic REFERENCES relationship.
func (c *Neo4jClient) UpsertReference(ctx context.Context, ref ReferenceEntity) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
		MATCH (f:File {path: $sourceFile})
		MERGE (ref:Reference {
			sourceFile: $sourceFile,
			sourceEntity: $sourceEntity,
			targetEntity: $targetEntity,
			refType: $refType
		})
		ON CREATE SET 
			ref.line = $line,
			ref.created = datetime()
		ON MATCH SET 
			ref.line = $line,
			ref.updated = datetime()
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
		_, err := tx.Run(ctx, cypher, params)
		return nil, err
	})
	return err
}

// Utility Operations

// CreateIndexes creates recommended indexes for better query performance.
func (c *Neo4jClient) CreateIndexes(ctx context.Context) error {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	indexes := []string{
		"CREATE INDEX IF NOT EXISTS FOR (f:File) ON (f.path)",
		"CREATE INDEX IF NOT EXISTS FOR (fn:Function) ON (fn.name)",
		"CREATE INDEX IF NOT EXISTS FOR (fn:Function) ON (fn.file)",
		"CREATE INDEX IF NOT EXISTS FOR (i:Import) ON (i.module)",
		"CREATE INDEX IF NOT EXISTS FOR (t:Type) ON (t.name)",
		"CREATE INDEX IF NOT EXISTS FOR (c:Class) ON (c.name)",
		"CREATE INDEX IF NOT EXISTS FOR (i:Interface) ON (i.name)",
		"CREATE INDEX IF NOT EXISTS FOR (jsx:JSXElement) ON (jsx.tagName)",
		"CREATE INDEX IF NOT EXISTS FOR (css:CSSRule) ON (css.selector)",
		"CREATE INDEX IF NOT EXISTS FOR (uc:UnresolvedCall) ON (uc.calledFunc)",
	}

	for _, index := range indexes {
		_, err := session.Run(ctx, index, nil)
		if err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}
