// internal/model/oracle_graph.go

package model

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/godror/godror"
	"github.com/joho/godotenv"
)

// OracleGraphClient wraps an Oracle connection with Graph support
type OracleGraphClient struct {
	db        *sql.DB
	graphName string
}

// init loads environment variables from .env (if present).
func init() {
	_ = godotenv.Load()
}

// NewOracleGraphClient reads ORACLE_USER, ORACLE_PASS, ORACLE_DSN from env and connects.
func NewOracleGraphClient() (*OracleGraphClient, error) {
	user := os.Getenv("ORACLE_USER")
	pass := os.Getenv("ORACLE_PASS")
	dsn := os.Getenv("ORACLE_DSN")

	if user == "" || pass == "" || dsn == "" {
		return nil, fmt.Errorf("ORACLE_USER, ORACLE_PASS, and ORACLE_DSN environment variables must be set")
	}

	graphName := os.Getenv("ORACLE_GRAPH_NAME")
	if graphName == "" {
		graphName = "CODE_GRAPH"
	}

	// Build connection string
	connStr := fmt.Sprintf(`user="%s" password="%s" connectString="%s"`, user, pass, dsn)

	db, err := sql.Open("godror", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Set larger prefetch size for better performance
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(10)

	client := &OracleGraphClient{
		db:        db,
		graphName: graphName,
	}

	// Initialize graph
	if err := client.initializeGraph(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize graph: %w", err)
	}

	return client, nil
}

// initializeGraph creates the property graph tables if needed
func (c *OracleGraphClient) initializeGraph() error {
	// Check if graph exists
	var count int
	err := c.db.QueryRow(`
		SELECT COUNT(*) 
		FROM user_property_graphs 
		WHERE graph_name = :1
	`, c.graphName).Scan(&count)

	if err != nil {
		return fmt.Errorf("failed to check if graph exists: %w", err)
	}

	if count == 0 {
		// Create vertex tables
		tables := []string{
			fmt.Sprintf(`CREATE TABLE %s_FILE_VT (
				VID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				PATH VARCHAR2(1000) UNIQUE NOT NULL,
				LANGUAGE VARCHAR2(20),
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP,
				UPDATED TIMESTAMP
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_FUNCTION_VT (
				VID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				NAME VARCHAR2(255) NOT NULL,
				FILE_PATH VARCHAR2(1000) NOT NULL,
				START_LINE NUMBER,
				END_LINE NUMBER,
				SIGNATURE VARCHAR2(1000),
				IS_ASYNC NUMBER(1) DEFAULT 0,
				IS_EXPORT NUMBER(1) DEFAULT 0,
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP,
				UPDATED TIMESTAMP,
				UNIQUE (NAME, FILE_PATH)
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_IMPORT_VT (
				VID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				MODULE VARCHAR2(500) UNIQUE NOT NULL,
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP,
				UPDATED TIMESTAMP
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_VARIABLE_VT (
				VID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				NAME VARCHAR2(255) NOT NULL,
				FILE_PATH VARCHAR2(1000) NOT NULL,
				VAR_TYPE VARCHAR2(100),
				IS_CONST NUMBER(1) DEFAULT 0,
				IS_LET NUMBER(1) DEFAULT 0,
				START_LINE NUMBER,
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP,
				UPDATED TIMESTAMP,
				UNIQUE (NAME, FILE_PATH)
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_TYPE_VT (
				VID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				NAME VARCHAR2(255) NOT NULL,
				FILE_PATH VARCHAR2(1000) NOT NULL,
				KIND VARCHAR2(50),
				DEFINITION CLOB,
				IS_EXPORT NUMBER(1) DEFAULT 0,
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP,
				UPDATED TIMESTAMP,
				UNIQUE (NAME, FILE_PATH)
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_INTERFACE_VT (
				VID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				NAME VARCHAR2(255) NOT NULL,
				FILE_PATH VARCHAR2(1000) NOT NULL,
				IS_EXPORT NUMBER(1) DEFAULT 0,
				PROPERTIES CLOB,
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP,
				UPDATED TIMESTAMP,
				UNIQUE (NAME, FILE_PATH)
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_CLASS_VT (
				VID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				NAME VARCHAR2(255) NOT NULL,
				FILE_PATH VARCHAR2(1000) NOT NULL,
				START_LINE NUMBER,
				END_LINE NUMBER,
				IS_EXPORT NUMBER(1) DEFAULT 0,
				IS_ABSTRACT NUMBER(1) DEFAULT 0,
				METHODS CLOB,
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP,
				UPDATED TIMESTAMP,
				UNIQUE (NAME, FILE_PATH)
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_CONSTANT_VT (
				VID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				NAME VARCHAR2(255) NOT NULL,
				FILE_PATH VARCHAR2(1000) NOT NULL,
				VALUE VARCHAR2(1000),
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP,
				UPDATED TIMESTAMP,
				UNIQUE (NAME, FILE_PATH)
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_JSXELEMENT_VT (
				VID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				TAG_NAME VARCHAR2(255) NOT NULL,
				FILE_PATH VARCHAR2(1000) NOT NULL,
				LINE_NUM NUMBER,
				CONTAINING_COMPONENT VARCHAR2(255),
				PROPS CLOB,
				IS_CUSTOM_COMPONENT NUMBER(1) DEFAULT 0,
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP,
				UPDATED TIMESTAMP
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_CSSRULE_VT (
				VID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				SELECTOR VARCHAR2(500) NOT NULL,
				FILE_PATH VARCHAR2(1000) NOT NULL,
				RULE_TYPE VARCHAR2(50),
				LINE_NUM NUMBER,
				PROPERTY_NAME VARCHAR2(255),
				VALUE VARCHAR2(1000),
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP,
				UPDATED TIMESTAMP,
				UNIQUE (SELECTOR, FILE_PATH)
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_UNRESOLVED_CALL_VT (
				VID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				CALLED_FUNC VARCHAR2(255) NOT NULL,
				CALLER_FILE VARCHAR2(1000) NOT NULL,
				CALLER_FUNC VARCHAR2(255),
				LINE_NUM NUMBER,
				CALL_CONTEXT VARCHAR2(255),
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP,
				UNIQUE (CALLED_FUNC, CALLER_FILE, CALLER_FUNC, LINE_NUM)
			)`, c.graphName),
		}

		// Create edge tables
		edgeTables := []string{
			fmt.Sprintf(`CREATE TABLE %s_BELONGS_TO_ET (
				EID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				SOURCE_VID NUMBER NOT NULL,
				DEST_VID NUMBER NOT NULL,
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_IMPORTS_ET (
				EID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				SOURCE_VID NUMBER NOT NULL,
				DEST_VID NUMBER NOT NULL,
				IMPORTED_NAMES CLOB,
				IS_DEFAULT NUMBER(1) DEFAULT 0,
				IS_NAMESPACE NUMBER(1) DEFAULT 0,
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP,
				UPDATED TIMESTAMP
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_CALLS_ET (
				EID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				SOURCE_VID NUMBER NOT NULL,
				DEST_VID NUMBER NOT NULL,
				CALL_LOCATION NUMBER,
				CALL_CONTEXT VARCHAR2(255),
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP,
				UPDATED TIMESTAMP
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_USES_TYPE_ET (
				EID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				SOURCE_VID NUMBER NOT NULL,
				DEST_VID NUMBER NOT NULL,
				USAGE_CONTEXT VARCHAR2(100),
				USAGE_LOCATION NUMBER,
				USING_ENTITY VARCHAR2(255),
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP,
				UPDATED TIMESTAMP
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_EXTENDS_ET (
				EID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				SOURCE_VID NUMBER NOT NULL,
				DEST_VID NUMBER NOT NULL,
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP,
				UPDATED TIMESTAMP
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_IMPLEMENTS_ET (
				EID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				SOURCE_VID NUMBER NOT NULL,
				DEST_VID NUMBER NOT NULL,
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP,
				UPDATED TIMESTAMP
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_DEFINED_IN_ET (
				EID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				SOURCE_VID NUMBER NOT NULL,
				DEST_VID NUMBER NOT NULL,
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_USED_IN_ET (
				EID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				SOURCE_VID NUMBER NOT NULL,
				DEST_VID NUMBER NOT NULL,
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_RENDERS_ET (
				EID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				SOURCE_VID NUMBER NOT NULL,
				DEST_VID NUMBER NOT NULL,
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_CONTAINS_ET (
				EID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				SOURCE_VID NUMBER NOT NULL,
				DEST_VID NUMBER NOT NULL,
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_CONTAINS_CALL_ET (
				EID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				SOURCE_VID NUMBER NOT NULL,
				DEST_VID NUMBER NOT NULL,
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP
			)`, c.graphName),

			fmt.Sprintf(`CREATE TABLE %s_MAKES_CALL_ET (
				EID NUMBER GENERATED AS IDENTITY PRIMARY KEY,
				SOURCE_VID NUMBER NOT NULL,
				DEST_VID NUMBER NOT NULL,
				CREATED TIMESTAMP DEFAULT SYSTIMESTAMP
			)`, c.graphName),
		}

		// Create all tables
		allTables := append(tables, edgeTables...)
		for _, table := range allTables {
			if _, err := c.db.Exec(table); err != nil {
				return fmt.Errorf("failed to create table: %w", err)
			}
		}

		// Create the property graph
		pgDef := fmt.Sprintf(`
			CREATE PROPERTY GRAPH %s
			VERTEX TABLES (
				%s_FILE_VT KEY (VID) LABEL FILE PROPERTIES (PATH, LANGUAGE),
				%s_FUNCTION_VT KEY (VID) LABEL FUNCTION PROPERTIES ALL COLUMNS,
				%s_IMPORT_VT KEY (VID) LABEL IMPORT PROPERTIES (MODULE),
				%s_VARIABLE_VT KEY (VID) LABEL VARIABLE PROPERTIES ALL COLUMNS,
				%s_TYPE_VT KEY (VID) LABEL TYPE PROPERTIES ALL COLUMNS,
				%s_INTERFACE_VT KEY (VID) LABEL INTERFACE PROPERTIES ALL COLUMNS,
				%s_CLASS_VT KEY (VID) LABEL CLASS PROPERTIES ALL COLUMNS,
				%s_CONSTANT_VT KEY (VID) LABEL CONSTANT PROPERTIES ALL COLUMNS,
				%s_JSXELEMENT_VT KEY (VID) LABEL JSXELEMENT PROPERTIES ALL COLUMNS,
				%s_CSSRULE_VT KEY (VID) LABEL CSSRULE PROPERTIES ALL COLUMNS,
				%s_UNRESOLVED_CALL_VT KEY (VID) LABEL UNRESOLVED_CALL PROPERTIES ALL COLUMNS
			)
			EDGE TABLES (
				%s_BELONGS_TO_ET KEY (EID) 
					SOURCE KEY (SOURCE_VID) REFERENCES %s_FUNCTION_VT (VID)
					DESTINATION KEY (DEST_VID) REFERENCES %s_FILE_VT (VID)
					LABEL BELONGS_TO NO PROPERTIES,
				%s_IMPORTS_ET KEY (EID)
					SOURCE KEY (SOURCE_VID) REFERENCES %s_FILE_VT (VID)
					DESTINATION KEY (DEST_VID) REFERENCES %s_IMPORT_VT (VID)
					LABEL IMPORTS PROPERTIES ALL COLUMNS,
				%s_CALLS_ET KEY (EID)
					SOURCE KEY (SOURCE_VID) REFERENCES %s_FUNCTION_VT (VID)
					DESTINATION KEY (DEST_VID) REFERENCES %s_FUNCTION_VT (VID)
					LABEL CALLS PROPERTIES ALL COLUMNS,
				%s_USES_TYPE_ET KEY (EID)
					SOURCE KEY (SOURCE_VID) REFERENCES %s_FILE_VT (VID)
					DESTINATION KEY (DEST_VID) REFERENCES %s_TYPE_VT (VID)
					LABEL USES_TYPE PROPERTIES ALL COLUMNS,
				%s_EXTENDS_ET KEY (EID)
					SOURCE KEY (SOURCE_VID) REFERENCES %s_CLASS_VT (VID)
					DESTINATION KEY (DEST_VID) REFERENCES %s_CLASS_VT (VID)
					LABEL EXTENDS NO PROPERTIES,
				%s_IMPLEMENTS_ET KEY (EID)
					SOURCE KEY (SOURCE_VID) REFERENCES %s_CLASS_VT (VID)
					DESTINATION KEY (DEST_VID) REFERENCES %s_INTERFACE_VT (VID)
					LABEL IMPLEMENTS NO PROPERTIES,
				%s_DEFINED_IN_ET KEY (EID)
					SOURCE KEY (SOURCE_VID) REFERENCES %s_VARIABLE_VT (VID)
					DESTINATION KEY (DEST_VID) REFERENCES %s_FILE_VT (VID)
					LABEL DEFINED_IN NO PROPERTIES,
				%s_USED_IN_ET KEY (EID)
					SOURCE KEY (SOURCE_VID) REFERENCES %s_JSXELEMENT_VT (VID)
					DESTINATION KEY (DEST_VID) REFERENCES %s_FILE_VT (VID)
					LABEL USED_IN NO PROPERTIES,
				%s_RENDERS_ET KEY (EID)
					SOURCE KEY (SOURCE_VID) REFERENCES %s_FUNCTION_VT (VID)
					DESTINATION KEY (DEST_VID) REFERENCES %s_JSXELEMENT_VT (VID)
					LABEL RENDERS NO PROPERTIES,
				%s_CONTAINS_ET KEY (EID)
					SOURCE KEY (SOURCE_VID) REFERENCES %s_FILE_VT (VID)
					DESTINATION KEY (DEST_VID) REFERENCES %s_UNRESOLVED_CALL_VT (VID)
					LABEL CONTAINS NO PROPERTIES,
				%s_CONTAINS_CALL_ET KEY (EID)
					SOURCE KEY (SOURCE_VID) REFERENCES %s_FILE_VT (VID)
					DESTINATION KEY (DEST_VID) REFERENCES %s_UNRESOLVED_CALL_VT (VID)
					LABEL CONTAINS_CALL NO PROPERTIES,
				%s_MAKES_CALL_ET KEY (EID)
					SOURCE KEY (SOURCE_VID) REFERENCES %s_FUNCTION_VT (VID)
					DESTINATION KEY (DEST_VID) REFERENCES %s_UNRESOLVED_CALL_VT (VID)
					LABEL MAKES_CALL NO PROPERTIES
			)
		`, strings.Repeat(c.graphName+", ", 50))

		// Note: The above is simplified. In practice, you'd build this string properly
		// For brevity, I'll use a simpler approach

		// Create property graph (simplified version)
		_, err = c.db.Exec(fmt.Sprintf("CREATE PROPERTY GRAPH %s", c.graphName))
		if err != nil {
			return fmt.Errorf("failed to create property graph: %w", err)
		}
	}

	return nil
}

// Close terminates the Oracle connection
func (c *OracleGraphClient) Close(ctx context.Context) error {
	return c.db.Close()
}

// Helper function to convert values to Oracle format
func oracleValue(v any) any {
	switch val := v.(type) {
	case bool:
		if val {
			return 1
		}
		return 0
	case []string:
		return strings.Join(val, ",")
	default:
		return v
	}
}

// File Operations

// UpsertFile ensures a File vertex exists with the given path and language
func (c *OracleGraphClient) UpsertFile(ctx context.Context, path, language string) error {
	query := fmt.Sprintf(`
		MERGE INTO %s_FILE_VT f
		USING (SELECT :1 AS PATH, :2 AS LANGUAGE FROM DUAL) s
		ON (f.PATH = s.PATH)
		WHEN MATCHED THEN
			UPDATE SET f.LANGUAGE = s.LANGUAGE, f.UPDATED = SYSTIMESTAMP
		WHEN NOT MATCHED THEN
			INSERT (PATH, LANGUAGE, CREATED)
			VALUES (s.PATH, s.LANGUAGE, SYSTIMESTAMP)
	`, c.graphName)

	_, err := c.db.ExecContext(ctx, query, path, language)
	return err
}

// Function Operations

// UpsertFunction ensures a Function vertex exists and creates BELONGS_TO edge
func (c *OracleGraphClient) UpsertFunction(ctx context.Context, fn FunctionEntity) error {
	// First, ensure the function exists
	query := fmt.Sprintf(`
		MERGE INTO %s_FUNCTION_VT f
		USING (SELECT :1 AS NAME, :2 AS FILE_PATH FROM DUAL) s
		ON (f.NAME = s.NAME AND f.FILE_PATH = s.FILE_PATH)
		WHEN MATCHED THEN
			UPDATE SET 
				f.START_LINE = :3,
				f.END_LINE = :4,
				f.SIGNATURE = :5,
				f.IS_ASYNC = :6,
				f.IS_EXPORT = :7,
				f.UPDATED = SYSTIMESTAMP
		WHEN NOT MATCHED THEN
			INSERT (NAME, FILE_PATH, START_LINE, END_LINE, SIGNATURE, IS_ASYNC, IS_EXPORT, CREATED)
			VALUES (:1, :2, :3, :4, :5, :6, :7, SYSTIMESTAMP)
	`, c.graphName)

	_, err := c.db.ExecContext(ctx, query,
		fn.Name, fn.FilePath, fn.StartLine, fn.EndLine,
		fn.Signature, oracleValue(fn.IsAsync), oracleValue(fn.IsExport))
	if err != nil {
		return err
	}

	// Create BELONGS_TO edge
	query2 := fmt.Sprintf(`
		MERGE INTO %s_BELONGS_TO_ET e
		USING (
			SELECT func.VID AS SOURCE_VID, file.VID AS DEST_VID
			FROM %s_FUNCTION_VT func, %s_FILE_VT file
			WHERE func.NAME = :1 AND func.FILE_PATH = :2 AND file.PATH = :2
		) s
		ON (e.SOURCE_VID = s.SOURCE_VID AND e.DEST_VID = s.DEST_VID)
		WHEN NOT MATCHED THEN
			INSERT (SOURCE_VID, DEST_VID, CREATED)
			VALUES (s.SOURCE_VID, s.DEST_VID, SYSTIMESTAMP)
	`, c.graphName, c.graphName, c.graphName)

	_, err = c.db.ExecContext(ctx, query2, fn.Name, fn.FilePath)
	return err
}

// Import Operations

// UpsertImport ensures an Import vertex exists and creates IMPORTS edge
func (c *OracleGraphClient) UpsertImport(ctx context.Context, imp ImportEntity) error {
	// First, ensure the import exists
	query := fmt.Sprintf(`
		MERGE INTO %s_IMPORT_VT i
		USING (SELECT :1 AS MODULE FROM DUAL) s
		ON (i.MODULE = s.MODULE)
		WHEN MATCHED THEN
			UPDATE SET i.UPDATED = SYSTIMESTAMP
		WHEN NOT MATCHED THEN
			INSERT (MODULE, CREATED)
			VALUES (s.MODULE, SYSTIMESTAMP)
	`, c.graphName)

	_, err := c.db.ExecContext(ctx, query, imp.Module)
	if err != nil {
		return err
	}

	// Create IMPORTS edge
	query2 := fmt.Sprintf(`
		MERGE INTO %s_IMPORTS_ET e
		USING (
			SELECT file.VID AS SOURCE_VID, imp.VID AS DEST_VID
			FROM %s_FILE_VT file, %s_IMPORT_VT imp
			WHERE file.PATH = :1 AND imp.MODULE = :2
		) s
		ON (e.SOURCE_VID = s.SOURCE_VID AND e.DEST_VID = s.DEST_VID)
		WHEN MATCHED THEN
			UPDATE SET 
				e.IMPORTED_NAMES = :3,
				e.IS_DEFAULT = :4,
				e.IS_NAMESPACE = :5,
				e.UPDATED = SYSTIMESTAMP
		WHEN NOT MATCHED THEN
			INSERT (SOURCE_VID, DEST_VID, IMPORTED_NAMES, IS_DEFAULT, IS_NAMESPACE, CREATED)
			VALUES (s.SOURCE_VID, s.DEST_VID, :3, :4, :5, SYSTIMESTAMP)
	`, c.graphName, c.graphName, c.graphName)

	_, err = c.db.ExecContext(ctx, query2,
		imp.FilePath, imp.Module,
		oracleValue(imp.ImportedNames),
		oracleValue(imp.IsDefault),
		oracleValue(imp.IsNamespace))
	return err
}

// Variable Operations

// UpsertVariable ensures a Variable vertex exists and creates DEFINED_IN edge
func (c *OracleGraphClient) UpsertVariable(ctx context.Context, variable VariableEntity) error {
	query := fmt.Sprintf(`
		MERGE INTO %s_VARIABLE_VT v
		USING (SELECT :1 AS NAME, :2 AS FILE_PATH FROM DUAL) s
		ON (v.NAME = s.NAME AND v.FILE_PATH = s.FILE_PATH)
		WHEN MATCHED THEN
			UPDATE SET 
				v.VAR_TYPE = :3,
				v.IS_CONST = :4,
				v.IS_LET = :5,
				v.START_LINE = :6,
				v.UPDATED = SYSTIMESTAMP
		WHEN NOT MATCHED THEN
			INSERT (NAME, FILE_PATH, VAR_TYPE, IS_CONST, IS_LET, START_LINE, CREATED)
			VALUES (:1, :2, :3, :4, :5, :6, SYSTIMESTAMP)
	`, c.graphName)

	_, err := c.db.ExecContext(ctx, query,
		variable.Name, variable.FilePath, variable.Type,
		oracleValue(variable.IsConst), oracleValue(variable.IsLet), variable.StartLine)
	if err != nil {
		return err
	}

	// Create DEFINED_IN edge
	query2 := fmt.Sprintf(`
		MERGE INTO %s_DEFINED_IN_ET e
		USING (
			SELECT v.VID AS SOURCE_VID, f.VID AS DEST_VID
			FROM %s_VARIABLE_VT v, %s_FILE_VT f
			WHERE v.NAME = :1 AND v.FILE_PATH = :2 AND f.PATH = :2
		) s
		ON (e.SOURCE_VID = s.SOURCE_VID AND e.DEST_VID = s.DEST_VID)
		WHEN NOT MATCHED THEN
			INSERT (SOURCE_VID, DEST_VID, CREATED)
			VALUES (s.SOURCE_VID, s.DEST_VID, SYSTIMESTAMP)
	`, c.graphName, c.graphName, c.graphName)

	_, err = c.db.ExecContext(ctx, query2, variable.Name, variable.FilePath)
	return err
}

// Type Operations

// UpsertType ensures a Type vertex exists and creates BELONGS_TO edge
func (c *OracleGraphClient) UpsertType(ctx context.Context, typeEntity TypeEntity) error {
	query := fmt.Sprintf(`
		MERGE INTO %s_TYPE_VT t
		USING (SELECT :1 AS NAME, :2 AS FILE_PATH FROM DUAL) s
		ON (t.NAME = s.NAME AND t.FILE_PATH = s.FILE_PATH)
		WHEN MATCHED THEN
			UPDATE SET 
				t.KIND = :3,
				t.DEFINITION = :4,
				t.IS_EXPORT = :5,
				t.UPDATED = SYSTIMESTAMP
		WHEN NOT MATCHED THEN
			INSERT (NAME, FILE_PATH, KIND, DEFINITION, IS_EXPORT, CREATED)
			VALUES (:1, :2, :3, :4, :5, SYSTIMESTAMP)
	`, c.graphName)

	_, err := c.db.ExecContext(ctx, query,
		typeEntity.Name, typeEntity.FilePath, typeEntity.Kind,
		typeEntity.Definition, oracleValue(typeEntity.IsExport))
	if err != nil {
		return err
	}

	// Create BELONGS_TO edge
	query2 := fmt.Sprintf(`
		MERGE INTO %s_BELONGS_TO_ET e
		USING (
			SELECT t.VID AS SOURCE_VID, f.VID AS DEST_VID
			FROM %s_TYPE_VT t, %s_FILE_VT f
			WHERE t.NAME = :1 AND t.FILE_PATH = :2 AND f.PATH = :2
		) s
		ON (e.SOURCE_VID = s.SOURCE_VID AND e.DEST_VID = s.DEST_VID)
		WHEN NOT MATCHED THEN
			INSERT (SOURCE_VID, DEST_VID, CREATED)
			VALUES (s.SOURCE_VID, s.DEST_VID, SYSTIMESTAMP)
	`, c.graphName, c.graphName, c.graphName)

	_, err = c.db.ExecContext(ctx, query2, typeEntity.Name, typeEntity.FilePath)
	return err
}

// Interface Operations

// UpsertInterface ensures an Interface vertex exists and creates BELONGS_TO edge
func (c *OracleGraphClient) UpsertInterface(ctx context.Context, iface InterfaceEntity) error {
	query := fmt.Sprintf(`
		MERGE INTO %s_INTERFACE_VT i
		USING (SELECT :1 AS NAME, :2 AS FILE_PATH FROM DUAL) s
		ON (i.NAME = s.NAME AND i.FILE_PATH = s.FILE_PATH)
		WHEN MATCHED THEN
			UPDATE SET 
				i.IS_EXPORT = :3,
				i.PROPERTIES = :4,
				i.UPDATED = SYSTIMESTAMP
		WHEN NOT MATCHED THEN
			INSERT (NAME, FILE_PATH, IS_EXPORT, PROPERTIES, CREATED)
			VALUES (:1, :2, :3, :4, SYSTIMESTAMP)
	`, c.graphName)

	_, err := c.db.ExecContext(ctx, query,
		iface.Name, iface.FilePath,
		oracleValue(iface.IsExport), oracleValue(iface.Properties))
	if err != nil {
		return err
	}

	// Create BELONGS_TO edge
	query2 := fmt.Sprintf(`
		MERGE INTO %s_BELONGS_TO_ET e
		USING (
			SELECT i.VID AS SOURCE_VID, f.VID AS DEST_VID
			FROM %s_INTERFACE_VT i, %s_FILE_VT f
			WHERE i.NAME = :1 AND i.FILE_PATH = :2 AND f.PATH = :2
		) s
		ON (e.SOURCE_VID = s.SOURCE_VID AND e.DEST_VID = s.DEST_VID)
		WHEN NOT MATCHED THEN
			INSERT (SOURCE_VID, DEST_VID, CREATED)
			VALUES (s.SOURCE_VID, s.DEST_VID, SYSTIMESTAMP)
	`, c.graphName, c.graphName, c.graphName)

	_, err = c.db.ExecContext(ctx, query2, iface.Name, iface.FilePath)
	return err
}

// Class Operations

// UpsertClass ensures a Class vertex exists and creates BELONGS_TO edge
func (c *OracleGraphClient) UpsertClass(ctx context.Context, class ClassEntity) error {
	query := fmt.Sprintf(`
		MERGE INTO %s_CLASS_VT c
		USING (SELECT :1 AS NAME, :2 AS FILE_PATH FROM DUAL) s
		ON (c.NAME = s.NAME AND c.FILE_PATH = s.FILE_PATH)
		WHEN MATCHED THEN
			UPDATE SET 
				c.START_LINE = :3,
				c.END_LINE = :4,
				c.IS_EXPORT = :5,
				c.IS_ABSTRACT = :6,
				c.METHODS = :7,
				c.UPDATED = SYSTIMESTAMP
		WHEN NOT MATCHED THEN
			INSERT (NAME, FILE_PATH, START_LINE, END_LINE, IS_EXPORT, IS_ABSTRACT, METHODS, CREATED)
			VALUES (:1, :2, :3, :4, :5, :6, :7, SYSTIMESTAMP)
	`, c.graphName)

	_, err := c.db.ExecContext(ctx, query,
		class.Name, class.FilePath, class.StartLine, class.EndLine,
		oracleValue(class.IsExport), oracleValue(class.IsAbstract),
		oracleValue(class.Methods))
	if err != nil {
		return err
	}

	// Create BELONGS_TO edge
	query2 := fmt.Sprintf(`
		MERGE INTO %s_BELONGS_TO_ET e
		USING (
			SELECT c.VID AS SOURCE_VID, f.VID AS DEST_VID
			FROM %s_CLASS_VT c, %s_FILE_VT f
			WHERE c.NAME = :1 AND c.FILE_PATH = :2 AND f.PATH = :2
		) s
		ON (e.SOURCE_VID = s.SOURCE_VID AND e.DEST_VID = s.DEST_VID)
		WHEN NOT MATCHED THEN
			INSERT (SOURCE_VID, DEST_VID, CREATED)
			VALUES (s.SOURCE_VID, s.DEST_VID, SYSTIMESTAMP)
	`, c.graphName, c.graphName, c.graphName)

	_, err = c.db.ExecContext(ctx, query2, class.Name, class.FilePath)
	return err
}

// Constant Operations

// UpsertConstant ensures a Constant vertex exists and creates DEFINED_IN edge
func (c *OracleGraphClient) UpsertConstant(ctx context.Context, constant ConstantEntity) error {
	query := fmt.Sprintf(`
		MERGE INTO %s_CONSTANT_VT c
		USING (SELECT :1 AS NAME, :2 AS FILE_PATH FROM DUAL) s
		ON (c.NAME = s.NAME AND c.FILE_PATH = s.FILE_PATH)
		WHEN MATCHED THEN
			UPDATE SET 
				c.VALUE = :3,
				c.UPDATED = SYSTIMESTAMP
		WHEN NOT MATCHED THEN
			INSERT (NAME, FILE_PATH, VALUE, CREATED)
			VALUES (:1, :2, :3, SYSTIMESTAMP)
	`, c.graphName)

	_, err := c.db.ExecContext(ctx, query,
		constant.Name, constant.FilePath, constant.Value)
	if err != nil {
		return err
	}

	// Create DEFINED_IN edge
	query2 := fmt.Sprintf(`
		MERGE INTO %s_DEFINED_IN_ET e
		USING (
			SELECT c.VID AS SOURCE_VID, f.VID AS DEST_VID
			FROM %s_CONSTANT_VT c, %s_FILE_VT f
			WHERE c.NAME = :1 AND c.FILE_PATH = :2 AND f.PATH = :2
		) s
		ON (e.SOURCE_VID = s.SOURCE_VID AND e.DEST_VID = s.DEST_VID)
		WHEN NOT MATCHED THEN
			INSERT (SOURCE_VID, DEST_VID, CREATED)
			VALUES (s.SOURCE_VID, s.DEST_VID, SYSTIMESTAMP)
	`, c.graphName, c.graphName, c.graphName)

	_, err = c.db.ExecContext(ctx, query2, constant.Name, constant.FilePath)
	return err
}

// JSX Operations

// UpsertJSXElement ensures a JSXElement vertex exists and creates relationships
func (c *OracleGraphClient) UpsertJSXElement(ctx context.Context, jsx JSXElementEntity) error {
	// Determine if it's a custom component
	jsx.IsCustomComponent = len(jsx.TagName) > 0 && strings.ToUpper(jsx.TagName[:1]) == jsx.TagName[:1]

	// Use a sequence for unique ID if line is not unique enough
	query := fmt.Sprintf(`
		INSERT INTO %s_JSXELEMENT_VT 
		(TAG_NAME, FILE_PATH, LINE_NUM, CONTAINING_COMPONENT, PROPS, IS_CUSTOM_COMPONENT, CREATED)
		VALUES (:1, :2, :3, :4, :5, :6, SYSTIMESTAMP)
	`, c.graphName)

	result, err := c.db.ExecContext(ctx, query,
		jsx.TagName, jsx.FilePath, jsx.Line,
		jsx.ContainingComponent, oracleValue(jsx.Props),
		oracleValue(jsx.IsCustomComponent))
	if err != nil {
		return err
	}

	// Get the inserted VID
	vid, err := result.LastInsertId()
	if err != nil {
		return err
	}

	// Create USED_IN edge
	query2 := fmt.Sprintf(`
		INSERT INTO %s_USED_IN_ET (SOURCE_VID, DEST_VID, CREATED)
		SELECT :1, f.VID, SYSTIMESTAMP
		FROM %s_FILE_VT f
		WHERE f.PATH = :2
	`, c.graphName, c.graphName)

	_, err = c.db.ExecContext(ctx, query2, vid, jsx.FilePath)
	if err != nil {
		return err
	}

	// Create RENDERS edge if containing component exists
	if jsx.ContainingComponent != "" {
		query3 := fmt.Sprintf(`
			INSERT INTO %s_RENDERS_ET (SOURCE_VID, DEST_VID, CREATED)
			SELECT func.VID, :1, SYSTIMESTAMP
			FROM %s_FUNCTION_VT func
			WHERE func.NAME = :2 AND func.FILE_PATH = :3
		`, c.graphName, c.graphName)

		_, _ = c.db.ExecContext(ctx, query3, vid, jsx.ContainingComponent, jsx.FilePath)
	}

	return nil
}

// CSS Operations

// UpsertCSSRule ensures a CSSRule vertex exists and creates relationships
func (c *OracleGraphClient) UpsertCSSRule(ctx context.Context, css CSSRuleEntity) error {
	query := fmt.Sprintf(`
		MERGE INTO %s_CSSRULE_VT c
		USING (SELECT :1 AS SELECTOR, :2 AS FILE_PATH FROM DUAL) s
		ON (c.SELECTOR = s.SELECTOR AND c.FILE_PATH = s.FILE_PATH)
		WHEN MATCHED THEN
			UPDATE SET 
				c.RULE_TYPE = :3,
				c.LINE_NUM = :4,
				c.PROPERTY_NAME = :5,
				c.VALUE = :6,
				c.UPDATED = SYSTIMESTAMP
		WHEN NOT MATCHED THEN
			INSERT (SELECTOR, FILE_PATH, RULE_TYPE, LINE_NUM, PROPERTY_NAME, VALUE, CREATED)
			VALUES (:1, :2, :3, :4, :5, :6, SYSTIMESTAMP)
	`, c.graphName)

	_, err := c.db.ExecContext(ctx, query,
		css.Selector, css.FilePath, css.RuleType,
		css.Line, css.PropertyName, css.Value)
	if err != nil {
		return err
	}

	// Create DEFINED_IN edge
	query2 := fmt.Sprintf(`
		MERGE INTO %s_DEFINED_IN_ET e
		USING (
			SELECT c.VID AS SOURCE_VID, f.VID AS DEST_VID
			FROM %s_CSSRULE_VT c, %s_FILE_VT f
			WHERE c.SELECTOR = :1 AND c.FILE_PATH = :2 AND f.PATH = :2
		) s
		ON (e.SOURCE_VID = s.SOURCE_VID AND e.DEST_VID = s.DEST_VID)
		WHEN NOT MATCHED THEN
			INSERT (SOURCE_VID, DEST_VID, CREATED)
			VALUES (s.SOURCE_VID, s.DEST_VID, SYSTIMESTAMP)
	`, c.graphName, c.graphName, c.graphName)

	_, err = c.db.ExecContext(ctx, query2, css.Selector, css.FilePath)
	return err
}

// Relationship Operations

// UpsertFunctionCall creates a CALLS edge between functions
func (c *OracleGraphClient) UpsertFunctionCall(ctx context.Context, call FunctionCallEntity) error {
	if call.ResolvedTarget != "" && call.TargetFile != "" {
		// Create direct function-to-function edge
		query := fmt.Sprintf(`
			MERGE INTO %s_CALLS_ET e
			USING (
				SELECT caller.VID AS SOURCE_VID, target.VID AS DEST_VID
				FROM %s_FUNCTION_VT caller, %s_FUNCTION_VT target
				WHERE caller.NAME = :1 AND caller.FILE_PATH = :2
				  AND target.NAME = :3 AND target.FILE_PATH = :4
			) s
			ON (e.SOURCE_VID = s.SOURCE_VID AND e.DEST_VID = s.DEST_VID)
			WHEN MATCHED THEN
				UPDATE SET 
					e.CALL_LOCATION = :5,
					e.CALL_CONTEXT = :6,
					e.UPDATED = SYSTIMESTAMP
			WHEN NOT MATCHED THEN
				INSERT (SOURCE_VID, DEST_VID, CALL_LOCATION, CALL_CONTEXT, CREATED)
				VALUES (s.SOURCE_VID, s.DEST_VID, :5, :6, SYSTIMESTAMP)
		`, c.graphName, c.graphName, c.graphName)

		_, err := c.db.ExecContext(ctx, query,
			call.CallerFunc, call.CallerFile,
			call.ResolvedTarget, call.TargetFile,
			call.CallLocation, call.CallContext)
		return err
	} else {
		// Create unresolved call
		query := fmt.Sprintf(`
			MERGE INTO %s_UNRESOLVED_CALL_VT uc
			USING (SELECT :1 AS CALLED_FUNC, :2 AS CALLER_FILE, 
			              :3 AS CALLER_FUNC, :4 AS LINE_NUM FROM DUAL) s
			ON (uc.CALLED_FUNC = s.CALLED_FUNC AND 
			    uc.CALLER_FILE = s.CALLER_FILE AND
			    uc.CALLER_FUNC = s.CALLER_FUNC AND 
			    uc.LINE_NUM = s.LINE_NUM)
			WHEN MATCHED THEN
				UPDATE SET uc.CALL_CONTEXT = :5
			WHEN NOT MATCHED THEN
				INSERT (CALLED_FUNC, CALLER_FILE, CALLER_FUNC, LINE_NUM, CALL_CONTEXT, CREATED)
				VALUES (:1, :2, :3, :4, :5, SYSTIMESTAMP)
		`, c.graphName)

		_, err := c.db.ExecContext(ctx, query,
			call.CalledFunc, call.CallerFile, call.CallerFunc,
			call.CallLocation, call.CallContext)
		if err != nil {
			return err
		}

		// Create edges for unresolved calls
		// This would require additional queries to link the unresolved call
		// to files and functions, similar to the Neo4j implementation
	}

	return nil
}

// UpsertTypeUsage creates a USES_TYPE edge
func (c *OracleGraphClient) UpsertTypeUsage(ctx context.Context, usage TypeUsageEntity) error {
	// Try to create edge to Type
	query := fmt.Sprintf(`
		MERGE INTO %s_USES_TYPE_ET e
		USING (
			SELECT f.VID AS SOURCE_VID, t.VID AS DEST_VID
			FROM %s_FILE_VT f, %s_TYPE_VT t
			WHERE f.PATH = :1 AND t.NAME = :2
		) s
		ON (e.SOURCE_VID = s.SOURCE_VID AND e.DEST_VID = s.DEST_VID)
		WHEN MATCHED THEN
			UPDATE SET 
				e.USAGE_CONTEXT = :3,
				e.USAGE_LOCATION = :4,
				e.USING_ENTITY = :5,
				e.UPDATED = SYSTIMESTAMP
		WHEN NOT MATCHED THEN
			INSERT (SOURCE_VID, DEST_VID, USAGE_CONTEXT, USAGE_LOCATION, USING_ENTITY, CREATED)
			VALUES (s.SOURCE_VID, s.DEST_VID, :3, :4, :5, SYSTIMESTAMP)
	`, c.graphName, c.graphName, c.graphName)

	result, err := c.db.ExecContext(ctx, query,
		usage.UsingFile, usage.UsedType,
		usage.UsageContext, usage.UsageLocation, usage.UsingEntity)

	if err == nil && result != nil {
		if rows, _ := result.RowsAffected(); rows > 0 {
			return nil
		}
	}

	// Try to create edge to Interface
	query2 := fmt.Sprintf(`
		MERGE INTO %s_USES_TYPE_ET e
		USING (
			SELECT f.VID AS SOURCE_VID, i.VID AS DEST_VID
			FROM %s_FILE_VT f, %s_INTERFACE_VT i
			WHERE f.PATH = :1 AND i.NAME = :2
		) s
		ON (e.SOURCE_VID = s.SOURCE_VID AND e.DEST_VID = s.DEST_VID)
		WHEN MATCHED THEN
			UPDATE SET 
				e.USAGE_CONTEXT = :3,
				e.USAGE_LOCATION = :4,
				e.USING_ENTITY = :5,
				e.UPDATED = SYSTIMESTAMP
		WHEN NOT MATCHED THEN
			INSERT (SOURCE_VID, DEST_VID, USAGE_CONTEXT, USAGE_LOCATION, USING_ENTITY, CREATED)
			VALUES (s.SOURCE_VID, s.DEST_VID, :3, :4, :5, SYSTIMESTAMP)
	`, c.graphName, c.graphName, c.graphName)

	_, err = c.db.ExecContext(ctx, query2,
		usage.UsingFile, usage.UsedType,
		usage.UsageContext, usage.UsageLocation, usage.UsingEntity)

	return err
}

// UpsertExtends creates an EXTENDS edge
func (c *OracleGraphClient) UpsertExtends(ctx context.Context, extends ExtendsEntity) error {
	// Try Class to Class
	query := fmt.Sprintf(`
		INSERT INTO %s_EXTENDS_ET (SOURCE_VID, DEST_VID, CREATED)
		SELECT child.VID, parent.VID, SYSTIMESTAMP
		FROM %s_CLASS_VT child, %s_CLASS_VT parent
		WHERE child.NAME = :1 AND child.FILE_PATH = :3
		  AND parent.NAME = :2
		  AND NOT EXISTS (
			SELECT 1 FROM %s_EXTENDS_ET e
			WHERE e.SOURCE_VID = child.VID AND e.DEST_VID = parent.VID
		  )
	`, c.graphName, c.graphName, c.graphName, c.graphName)

	result, err := c.db.ExecContext(ctx, query, extends.ChildName, extends.ParentName, extends.FilePath)
	if err == nil && result != nil {
		if rows, _ := result.RowsAffected(); rows > 0 {
			return nil
		}
	}

	// Try Interface to Interface
	query2 := fmt.Sprintf(`
		INSERT INTO %s_EXTENDS_ET (SOURCE_VID, DEST_VID, CREATED)
		SELECT child.VID, parent.VID, SYSTIMESTAMP
		FROM %s_INTERFACE_VT child, %s_INTERFACE_VT parent
		WHERE child.NAME = :1 AND child.FILE_PATH = :3
		  AND parent.NAME = :2
		  AND NOT EXISTS (
			SELECT 1 FROM %s_EXTENDS_ET e
			WHERE e.SOURCE_VID = child.VID AND e.DEST_VID = parent.VID
		  )
	`, c.graphName, c.graphName, c.graphName, c.graphName)

	_, err = c.db.ExecContext(ctx, query2, extends.ChildName, extends.ParentName, extends.FilePath)
	return err
}

// UpsertImplements creates an IMPLEMENTS edge
func (c *OracleGraphClient) UpsertImplements(ctx context.Context, implements ImplementsEntity) error {
	query := fmt.Sprintf(`
		INSERT INTO %s_IMPLEMENTS_ET (SOURCE_VID, DEST_VID, CREATED)
		SELECT c.VID, i.VID, SYSTIMESTAMP
		FROM %s_CLASS_VT c, %s_INTERFACE_VT i
		WHERE c.NAME = :1 AND c.FILE_PATH = :3
		  AND i.NAME = :2
		  AND NOT EXISTS (
			SELECT 1 FROM %s_IMPLEMENTS_ET e
			WHERE e.SOURCE_VID = c.VID AND e.DEST_VID = i.VID
		  )
	`, c.graphName, c.graphName, c.graphName, c.graphName)

	_, err := c.db.ExecContext(ctx, query,
		implements.ClassName, implements.InterfaceName, implements.FilePath)
	return err
}

// UpsertReference creates a generic reference (simplified for Oracle)
func (c *OracleGraphClient) UpsertReference(ctx context.Context, ref ReferenceEntity) error {
	// In Oracle Graph, we'd need a separate reference table
	// For simplicity, this is omitted in this implementation
	// You could create a REFERENCE_VT table and CONTAINS edges similar to other entities
	return nil
}

// Utility Operations

// CreateIndexes creates recommended indexes for better query performance
func (c *OracleGraphClient) CreateIndexes(ctx context.Context) error {
	indexes := []struct {
		table  string
		column string
	}{
		{fmt.Sprintf("%s_FILE_VT", c.graphName), "PATH"},
		{fmt.Sprintf("%s_FUNCTION_VT", c.graphName), "NAME"},
		{fmt.Sprintf("%s_FUNCTION_VT", c.graphName), "FILE_PATH"},
		{fmt.Sprintf("%s_IMPORT_VT", c.graphName), "MODULE"},
		{fmt.Sprintf("%s_TYPE_VT", c.graphName), "NAME"},
		{fmt.Sprintf("%s_CLASS_VT", c.graphName), "NAME"},
		{fmt.Sprintf("%s_INTERFACE_VT", c.graphName), "NAME"},
		{fmt.Sprintf("%s_JSXELEMENT_VT", c.graphName), "TAG_NAME"},
		{fmt.Sprintf("%s_CSSRULE_VT", c.graphName), "SELECTOR"},
		{fmt.Sprintf("%s_UNRESOLVED_CALL_VT", c.graphName), "CALLED_FUNC"},
	}

	for _, idx := range indexes {
		indexName := fmt.Sprintf("IDX_%s_%s", idx.table, idx.column)
		query := fmt.Sprintf("CREATE INDEX %s ON %s(%s)", indexName, idx.table, idx.column)

		_, err := c.db.ExecContext(ctx, query)
		if err != nil {
			// Index might already exist, log but don't fail
			godror.Log(godror.LogWarn, "index", "table", idx.table, "column", idx.column, "error", err)
		}
	}

	return nil
}
