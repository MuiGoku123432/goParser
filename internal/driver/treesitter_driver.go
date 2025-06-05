// internal/driver/treesitter_driver.go

package driver

import (
	"fmt"
	"goParse/internal/model"
	"io/ioutil"
	"log"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	tsCSS "github.com/smacker/go-tree-sitter/css"
	tsJS "github.com/smacker/go-tree-sitter/javascript"
	tsTS "github.com/smacker/go-tree-sitter/typescript/typescript"
)

// ParsedFile holds normalized entities extracted from a single source file.
type ParsedFile struct {
	FilePath string
	Language string

	// Entity collections
	Funcs       []model.FunctionEntity
	Imports     []model.ImportEntity
	Variables   []model.VariableEntity
	Types       []model.TypeEntity
	Interfaces  []model.InterfaceEntity
	Classes     []model.ClassEntity
	Constants   []model.ConstantEntity
	JSXElements []model.JSXElementEntity
	CSSRules    []model.CSSRuleEntity

	// Relationship collections
	FunctionCalls []model.FunctionCallEntity
	TypeUsages    []model.TypeUsageEntity
	Extends       []model.ExtendsEntity
	Implements    []model.ImplementsEntity
	References    []model.ReferenceEntity
}

// TreeSitterDriver knows how to parse .ts/.tsx/.js/.jsx/.css files.
type TreeSitterDriver struct {
	langs map[string]*sitter.Language
}

// NewTreeSitterDriver constructs a driver with grammars for needed file types.
func NewTreeSitterDriver() *TreeSitterDriver {
	return &TreeSitterDriver{
		langs: map[string]*sitter.Language{
			".ts":   tsTS.GetLanguage(),
			".tsx":  tsTS.GetLanguage(),
			".js":   tsJS.GetLanguage(),
			".jsx":  tsJS.GetLanguage(),
			".css":  tsCSS.GetLanguage(),
			".scss": tsCSS.GetLanguage(),
		},
	}
}

// Parse reads the file at 'path', builds an AST with Tree-sitter, then extracts
// comprehensive entities and relationships into a ParsedFile struct.
func (t *TreeSitterDriver) Parse(path string) (ParsedFile, error) {
	ext := filepath.Ext(path)
	lang, ok := t.langs[ext]
	if !ok {
		return ParsedFile{}, fmt.Errorf("unsupported extension: %s", ext)
	}

	src, err := ioutil.ReadFile(path)
	if err != nil {
		return ParsedFile{}, err
	}

	parser := sitter.NewParser()
	parser.SetLanguage(lang)
	tree := parser.Parse(nil, src)
	if tree == nil {
		return ParsedFile{}, fmt.Errorf("tree-sitter failed to parse file: %s", path)
	}
	root := tree.RootNode()

	pf := ParsedFile{
		FilePath: path,
		Language: ext[1:], // e.g. "ts", "tsx", "js", "jsx", "css", "scss"
	}

	switch ext {
	case ".ts", ".tsx":
		t.parseTypeScript(&pf, src, root, lang)
	case ".js", ".jsx":
		t.parseJavaScript(&pf, src, root, lang)
	case ".css", ".scss":
		t.parseCSS(&pf, src, root, lang)
	}

	// Post-processing: resolve function calls
	t.resolveFunctionCalls(&pf)

	return pf, nil
}

// parseTypeScript extracts all entities and relationships from TypeScript/TSX files
func (t *TreeSitterDriver) parseTypeScript(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	// Extract functions
	t.extractTSFunctions(pf, src, root, lang)

	// Extract imports
	t.extractTSImports(pf, src, root, lang)

	// Extract variables
	t.extractTSVariables(pf, src, root, lang)

	// Extract types
	t.extractTSTypes(pf, src, root, lang)

	// Extract interfaces
	t.extractTSInterfaces(pf, src, root, lang)

	// Extract classes
	t.extractTSClasses(pf, src, root, lang)

	// Extract function calls with context
	t.extractTSFunctionCalls(pf, src, root, lang)

	// Extract comprehensive type usages
	t.extractTSTypeUsages(pf, src, root, lang)

	// Extract inheritance relationships
	t.extractTSInheritance(pf, src, root, lang)

	// Extract JSX elements if in .tsx file
	if strings.HasSuffix(pf.FilePath, ".tsx") {
		t.extractJSXElements(pf, src, root, lang)
	}
}

// parseJavaScript extracts all entities and relationships from JavaScript/JSX files
func (t *TreeSitterDriver) parseJavaScript(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	// Extract functions
	t.extractJSFunctions(pf, src, root, lang)

	// Extract imports
	t.extractJSImports(pf, src, root, lang)

	// Extract variables
	t.extractJSVariables(pf, src, root, lang)

	// Extract classes
	t.extractJSClasses(pf, src, root, lang)

	// Extract function calls
	t.extractJSFunctionCalls(pf, src, root, lang)

	// Extract JSX elements if in .jsx file
	if strings.HasSuffix(pf.FilePath, ".jsx") {
		t.extractJSXElements(pf, src, root, lang)
	}
}

// parseCSS extracts entities from CSS/SCSS files
func (t *TreeSitterDriver) parseCSS(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	t.extractCSSRules(pf, src, root, lang)
}

// TypeScript extraction methods

func (t *TreeSitterDriver) extractTSFunctions(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	// Function declarations
	query1 := `(function_declaration name: (identifier) @func.name) @func.def`
	if qs, err := sitter.NewQuery([]byte(query1), lang); err == nil {
		t.runFunctionQuery(pf, src, root, qs)
	}

	// Method definitions
	query2 := `(method_definition key: (property_identifier) @func.name) @func.def`
	if qs, err := sitter.NewQuery([]byte(query2), lang); err == nil {
		t.runFunctionQuery(pf, src, root, qs)
	}

	// Method signatures
	query3 := `(method_signature name: (property_identifier) @func.name) @func.def`
	if qs, err := sitter.NewQuery([]byte(query3), lang); err == nil {
		t.runFunctionQuery(pf, src, root, qs)
	}

	// Arrow functions assigned to variables
	query4 := `(variable_declarator name: (identifier) @func.name value: (arrow_function)) @func.def`
	if qs, err := sitter.NewQuery([]byte(query4), lang); err == nil {
		t.runFunctionQuery(pf, src, root, qs)
	}

	// Function expressions assigned to variables
	query5 := `(variable_declarator name: (identifier) @func.name value: (function_expression)) @func.def`
	if qs, err := sitter.NewQuery([]byte(query5), lang); err == nil {
		t.runFunctionQuery(pf, src, root, qs)
	}
}

func (t *TreeSitterDriver) runFunctionQuery(pf *ParsedFile, src []byte, root *sitter.Node, qs *sitter.Query) {
	qc := sitter.NewQueryCursor()
	qc.Exec(qs, root)
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}

		var nameNode, defNode *sitter.Node
		for _, capture := range match.Captures {
			capName := qs.CaptureNameForId(capture.Index)
			if capName == "func.name" {
				nameNode = capture.Node
			} else if capName == "func.def" {
				defNode = capture.Node
			}
		}

		if nameNode != nil && defNode != nil {
			fnName := string(src[nameNode.StartByte():nameNode.EndByte()])

			// Extract signature and other metadata
			signature := t.extractFunctionSignature(defNode, src)

			pf.Funcs = append(pf.Funcs, model.FunctionEntity{
				Name:      fnName,
				FilePath:  pf.FilePath,
				StartLine: int(defNode.StartPoint().Row) + 1,
				EndLine:   int(defNode.EndPoint().Row) + 1,
				Signature: signature,
			})
		}
	}
}

func (t *TreeSitterDriver) extractFunctionSignature(node *sitter.Node, src []byte) string {
	// Find parameters node
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil && (child.Type() == "formal_parameters" || child.Type() == "parameters") {
			return string(src[child.StartByte():child.EndByte()])
		}
	}
	return ""
}

func (t *TreeSitterDriver) extractTSImports(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	// ES6 imports with named imports
	query1 := `(import_statement source: (string) @imp.module) @imp.stmt`
	if qs, err := sitter.NewQuery([]byte(query1), lang); err == nil {
		t.runImportQuery(pf, src, root, qs, false)
	}

	// CommonJS requires
	query2 := `(call_expression function: (identifier) @req arguments: (arguments (string) @imp.module))`
	if qs, err := sitter.NewQuery([]byte(query2), lang); err == nil {
		t.runImportQuery(pf, src, root, qs, true)
	}
}

func (t *TreeSitterDriver) runImportQuery(pf *ParsedFile, src []byte, root *sitter.Node, qs *sitter.Query, isRequire bool) {
	qc := sitter.NewQueryCursor()
	qc.Exec(qs, root)
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}

		var moduleNode, stmtNode *sitter.Node
		for _, capture := range match.Captures {
			capName := qs.CaptureNameForId(capture.Index)
			if capName == "imp.module" {
				moduleNode = capture.Node
			} else if capName == "imp.stmt" {
				stmtNode = capture.Node
			} else if isRequire && capName == "req" {
				// Check if it's actually "require"
				fnNode := capture.Node
				fnName := string(src[fnNode.StartByte():fnNode.EndByte()])
				if fnName != "require" {
					goto nextMatch
				}
			}
		}

		if moduleNode != nil {
			raw := string(src[moduleNode.StartByte():moduleNode.EndByte()])
			if len(raw) >= 2 && (raw[0] == '"' || raw[0] == '\'') {
				module := raw[1 : len(raw)-1]

				// Extract imported names
				var importedNames []string
				if stmtNode != nil {
					importedNames = t.extractImportedNames(stmtNode, src)
				}

				pf.Imports = append(pf.Imports, model.ImportEntity{
					Module:        module,
					FilePath:      pf.FilePath,
					ImportedNames: importedNames,
				})
			}
		}
	nextMatch:
	}
}

func (t *TreeSitterDriver) extractImportedNames(importNode *sitter.Node, src []byte) []string {
	var names []string

	// Walk the import statement to find imported identifiers
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}

		if node.Type() == "identifier" && node.Parent() != nil {
			parentType := node.Parent().Type()
			if parentType == "import_specifier" || parentType == "namespace_import" || parentType == "import_clause" {
				names = append(names, string(src[node.StartByte():node.EndByte()]))
			}
		}

		for i := 0; i < int(node.ChildCount()); i++ {
			walk(node.Child(i))
		}
	}

	walk(importNode)
	return names
}

func (t *TreeSitterDriver) extractTSClasses(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	query := `(class_declaration) @class`
	qs, err := sitter.NewQuery([]byte(query), lang)
	if err != nil {
		log.Printf("Failed to compile class query: %v", err)
		return
	}

	qc := sitter.NewQueryCursor()
	qc.Exec(qs, root)
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}

		for _, capture := range match.Captures {
			classNode := capture.Node

			// Find the identifier child
			var className string
			var extendsClass string
			var implementsInterfaces []string

			for i := 0; i < int(classNode.ChildCount()); i++ {
				child := classNode.Child(i)
				if child != nil {
					if child.Type() == "identifier" || child.Type() == "type_identifier" {
						if className == "" {
							className = string(src[child.StartByte():child.EndByte()])
						}
					} else if child.Type() == "class_heritage" {
						// Extract extends and implements
						extendsClass, implementsInterfaces = t.extractClassHeritage(child, src)
					}
				}
			}

			if className != "" {
				class := model.ClassEntity{
					Name:      className,
					FilePath:  pf.FilePath,
					StartLine: int(classNode.StartPoint().Row) + 1,
					EndLine:   int(classNode.EndPoint().Row) + 1,
				}
				pf.Classes = append(pf.Classes, class)

				// Add extends relationship
				if extendsClass != "" {
					pf.Extends = append(pf.Extends, model.ExtendsEntity{
						ChildName:  className,
						ParentName: extendsClass,
						FilePath:   pf.FilePath,
					})
				}

				// Add implements relationships
				for _, iface := range implementsInterfaces {
					pf.Implements = append(pf.Implements, model.ImplementsEntity{
						ClassName:     className,
						InterfaceName: iface,
						FilePath:      pf.FilePath,
					})
				}
			}
		}
	}
}

func (t *TreeSitterDriver) extractClassHeritage(heritageNode *sitter.Node, src []byte) (string, []string) {
	var extendsClass string
	var implementsInterfaces []string

	for i := 0; i < int(heritageNode.ChildCount()); i++ {
		child := heritageNode.Child(i)
		if child != nil {
			if child.Type() == "extends_clause" {
				// Find the identifier in extends clause
				for j := 0; j < int(child.ChildCount()); j++ {
					grandchild := child.Child(j)
					if grandchild != nil && (grandchild.Type() == "identifier" || grandchild.Type() == "type_identifier") {
						extendsClass = string(src[grandchild.StartByte():grandchild.EndByte()])
						break
					}
				}
			} else if child.Type() == "implements_clause" {
				// Find all identifiers in implements clause
				var walk func(*sitter.Node)
				walk = func(node *sitter.Node) {
					if node == nil {
						return
					}
					if node.Type() == "type_identifier" || node.Type() == "identifier" {
						implementsInterfaces = append(implementsInterfaces, string(src[node.StartByte():node.EndByte()]))
					}
					for k := 0; k < int(node.ChildCount()); k++ {
						walk(node.Child(k))
					}
				}
				walk(child)
			}
		}
	}

	return extendsClass, implementsInterfaces
}

func (t *TreeSitterDriver) extractTSInterfaces(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	query := `(interface_declaration) @interface`
	qs, err := sitter.NewQuery([]byte(query), lang)
	if err != nil {
		return
	}

	qc := sitter.NewQueryCursor()
	qc.Exec(qs, root)
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}

		for _, capture := range match.Captures {
			interfaceNode := capture.Node

			var interfaceName string
			var extendsInterfaces []string

			for i := 0; i < int(interfaceNode.ChildCount()); i++ {
				child := interfaceNode.Child(i)
				if child != nil {
					if child.Type() == "type_identifier" && interfaceName == "" {
						interfaceName = string(src[child.StartByte():child.EndByte()])
					} else if child.Type() == "extends_type_clause" {
						// Extract extended interfaces
						var walk func(*sitter.Node)
						walk = func(node *sitter.Node) {
							if node == nil {
								return
							}
							if node.Type() == "type_identifier" {
								extendsInterfaces = append(extendsInterfaces, string(src[node.StartByte():node.EndByte()]))
							}
							for j := 0; j < int(node.ChildCount()); j++ {
								walk(node.Child(j))
							}
						}
						walk(child)
					}
				}
			}

			if interfaceName != "" {
				pf.Interfaces = append(pf.Interfaces, model.InterfaceEntity{
					Name:     interfaceName,
					FilePath: pf.FilePath,
				})

				// Add extends relationships for interfaces
				for _, parent := range extendsInterfaces {
					pf.Extends = append(pf.Extends, model.ExtendsEntity{
						ChildName:  interfaceName,
						ParentName: parent,
						FilePath:   pf.FilePath,
					})
				}
			}
		}
	}
}

func (t *TreeSitterDriver) extractTSFunctionCalls(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	// Direct function calls
	query1 := `(call_expression function: (identifier) @call.name) @call.expr`
	if qs, err := sitter.NewQuery([]byte(query1), lang); err == nil {
		t.runFunctionCallQuery(pf, src, root, qs)
	}

	// Method calls
	query2 := `(call_expression function: (member_expression property: (property_identifier) @call.name object: (_) @call.object)) @call.expr`
	if qs, err := sitter.NewQuery([]byte(query2), lang); err == nil {
		t.runMethodCallQuery(pf, src, root, qs)
	}
}

func (t *TreeSitterDriver) runFunctionCallQuery(pf *ParsedFile, src []byte, root *sitter.Node, qs *sitter.Query) {
	qc := sitter.NewQueryCursor()
	qc.Exec(qs, root)
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}

		var nameNode, exprNode *sitter.Node
		for _, capture := range match.Captures {
			capName := qs.CaptureNameForId(capture.Index)
			if capName == "call.name" {
				nameNode = capture.Node
			} else if capName == "call.expr" {
				exprNode = capture.Node
			}
		}

		if nameNode != nil && exprNode != nil {
			funcName := string(src[nameNode.StartByte():nameNode.EndByte()])

			// Find the containing function
			callerFunc := t.findContainingFunction(exprNode, src)

			pf.FunctionCalls = append(pf.FunctionCalls, model.FunctionCallEntity{
				CallerFile:   pf.FilePath,
				CallerFunc:   callerFunc,
				CalledFunc:   funcName,
				CallLocation: int(nameNode.StartPoint().Row) + 1,
			})
		}
	}
}

func (t *TreeSitterDriver) runMethodCallQuery(pf *ParsedFile, src []byte, root *sitter.Node, qs *sitter.Query) {
	qc := sitter.NewQueryCursor()
	qc.Exec(qs, root)
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}

		var nameNode, objectNode, exprNode *sitter.Node
		for _, capture := range match.Captures {
			capName := qs.CaptureNameForId(capture.Index)
			if capName == "call.name" {
				nameNode = capture.Node
			} else if capName == "call.object" {
				objectNode = capture.Node
			} else if capName == "call.expr" {
				exprNode = capture.Node
			}
		}

		if nameNode != nil && exprNode != nil {
			methodName := string(src[nameNode.StartByte():nameNode.EndByte()])

			// Get object type/name if possible
			var objectName string
			if objectNode != nil {
				if objectNode.Type() == "identifier" {
					objectName = string(src[objectNode.StartByte():objectNode.EndByte()])
				} else if objectNode.Type() == "this" {
					objectName = "this"
				}
			}

			// Find the containing function
			callerFunc := t.findContainingFunction(exprNode, src)

			// Create a qualified name for method calls
			qualifiedName := methodName
			if objectName != "" {
				qualifiedName = objectName + "." + methodName
			}

			pf.FunctionCalls = append(pf.FunctionCalls, model.FunctionCallEntity{
				CallerFile:   pf.FilePath,
				CallerFunc:   callerFunc,
				CalledFunc:   qualifiedName,
				CallLocation: int(nameNode.StartPoint().Row) + 1,
				CallContext:  objectName,
			})
		}
	}
}

func (t *TreeSitterDriver) findContainingFunction(node *sitter.Node, src []byte) string {
	current := node.Parent()
	for current != nil {
		nodeType := current.Type()
		if nodeType == "function_declaration" || nodeType == "method_definition" ||
			nodeType == "arrow_function" || nodeType == "function_expression" {
			// Find the function name
			for i := 0; i < int(current.ChildCount()); i++ {
				child := current.Child(i)
				if child != nil && (child.Type() == "identifier" || child.Type() == "property_identifier") {
					return string(src[child.StartByte():child.EndByte()])
				}
			}
			// For arrow functions assigned to variables, check parent
			if nodeType == "arrow_function" && current.Parent() != nil && current.Parent().Type() == "variable_declarator" {
				varDecl := current.Parent()
				for i := 0; i < int(varDecl.ChildCount()); i++ {
					child := varDecl.Child(i)
					if child != nil && child.Type() == "identifier" {
						return string(src[child.StartByte():child.EndByte()])
					}
				}
			}
		}
		current = current.Parent()
	}
	return "" // Top-level call
}

func (t *TreeSitterDriver) extractTSTypeUsages(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	// Type annotations in various contexts
	queries := []struct {
		query   string
		context string
	}{
		{`(type_annotation (type_identifier) @type)`, "annotation"},
		{`(type_arguments (type_identifier) @type)`, "type_argument"},
		{`(return_type (type_identifier) @type)`, "return_type"},
		{`(parameter type: (type_annotation (type_identifier) @type))`, "parameter"},
		{`(property_signature type: (type_annotation (type_identifier) @type))`, "property"},
		{`(type_parameter_declaration (type_parameter (type_identifier) @type))`, "type_parameter"},
	}

	for _, q := range queries {
		if qs, err := sitter.NewQuery([]byte(q.query), lang); err == nil {
			qc := sitter.NewQueryCursor()
			qc.Exec(qs, root)
			for {
				match, ok := qc.NextMatch()
				if !ok {
					break
				}

				for _, capture := range match.Captures {
					if qs.CaptureNameForId(capture.Index) == "type" {
						typeName := string(src[capture.Node.StartByte():capture.Node.EndByte()])

						// Find the containing context (function, class, etc.)
						containingEntity := t.findContainingEntity(capture.Node, src)

						pf.TypeUsages = append(pf.TypeUsages, model.TypeUsageEntity{
							UsingFile:     pf.FilePath,
							UsingEntity:   containingEntity,
							UsedType:      typeName,
							UsageContext:  q.context,
							UsageLocation: int(capture.Node.StartPoint().Row) + 1,
						})
					}
				}
			}
		}
	}
}

func (t *TreeSitterDriver) findContainingEntity(node *sitter.Node, src []byte) string {
	current := node.Parent()
	for current != nil {
		nodeType := current.Type()
		switch nodeType {
		case "function_declaration", "method_definition", "method_signature":
			// Find function name
			for i := 0; i < int(current.ChildCount()); i++ {
				child := current.Child(i)
				if child != nil && (child.Type() == "identifier" || child.Type() == "property_identifier") {
					return "function:" + string(src[child.StartByte():child.EndByte()])
				}
			}
		case "class_declaration":
			// Find class name
			for i := 0; i < int(current.ChildCount()); i++ {
				child := current.Child(i)
				if child != nil && (child.Type() == "identifier" || child.Type() == "type_identifier") {
					return "class:" + string(src[child.StartByte():child.EndByte()])
				}
			}
		case "interface_declaration":
			// Find interface name
			for i := 0; i < int(current.ChildCount()); i++ {
				child := current.Child(i)
				if child != nil && child.Type() == "type_identifier" {
					return "interface:" + string(src[child.StartByte():child.EndByte()])
				}
			}
		case "type_alias_declaration":
			// Find type alias name
			for i := 0; i < int(current.ChildCount()); i++ {
				child := current.Child(i)
				if child != nil && child.Type() == "type_identifier" {
					return "type:" + string(src[child.StartByte():child.EndByte()])
				}
			}
		}
		current = current.Parent()
	}
	return ""
}

func (t *TreeSitterDriver) extractTSInheritance(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	// Already handled in extractTSClasses and extractTSInterfaces
}

// JSX extraction
func (t *TreeSitterDriver) extractJSXElements(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	query := `(jsx_element (jsx_opening_element (identifier) @tag.name)) @jsx.element`
	qs, err := sitter.NewQuery([]byte(query), lang)
	if err != nil {
		// Try alternative query for jsx_self_closing_element
		query2 := `(jsx_self_closing_element (identifier) @tag.name) @jsx.element`
		qs, err = sitter.NewQuery([]byte(query2), lang)
		if err != nil {
			return
		}
	}

	qc := sitter.NewQueryCursor()
	qc.Exec(qs, root)
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}

		var tagNode, elementNode *sitter.Node
		for _, capture := range match.Captures {
			capName := qs.CaptureNameForId(capture.Index)
			if capName == "tag.name" {
				tagNode = capture.Node
			} else if capName == "jsx.element" {
				elementNode = capture.Node
			}
		}

		if tagNode != nil && elementNode != nil {
			tagName := string(src[tagNode.StartByte():tagNode.EndByte()])

			// Extract props
			props := t.extractJSXProps(elementNode, src)

			// Find containing component
			containingComponent := t.findContainingFunction(elementNode, src)

			pf.JSXElements = append(pf.JSXElements, model.JSXElementEntity{
				TagName:             tagName,
				FilePath:            pf.FilePath,
				ContainingComponent: containingComponent,
				Props:               props,
				Line:                int(tagNode.StartPoint().Row) + 1,
			})
		}
	}

	// Also handle jsx_self_closing_element separately
	t.extractJSXSelfClosingElements(pf, src, root, lang)
}

func (t *TreeSitterDriver) extractJSXSelfClosingElements(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	query := `(jsx_self_closing_element) @jsx.element`
	qs, err := sitter.NewQuery([]byte(query), lang)
	if err != nil {
		return
	}

	qc := sitter.NewQueryCursor()
	qc.Exec(qs, root)
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}

		for _, capture := range match.Captures {
			elementNode := capture.Node

			// Find the identifier child
			var tagName string
			for i := 0; i < int(elementNode.ChildCount()); i++ {
				child := elementNode.Child(i)
				if child != nil && child.Type() == "identifier" {
					tagName = string(src[child.StartByte():child.EndByte()])
					break
				}
			}

			if tagName != "" {
				// Extract props
				props := t.extractJSXProps(elementNode, src)

				// Find containing component
				containingComponent := t.findContainingFunction(elementNode, src)

				pf.JSXElements = append(pf.JSXElements, model.JSXElementEntity{
					TagName:             tagName,
					FilePath:            pf.FilePath,
					ContainingComponent: containingComponent,
					Props:               props,
					Line:                int(elementNode.StartPoint().Row) + 1,
				})
			}
		}
	}
}

func (t *TreeSitterDriver) extractJSXProps(elementNode *sitter.Node, src []byte) []string {
	var props []string

	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}

		if node.Type() == "jsx_attribute" {
			// Find the property_identifier child
			for i := 0; i < int(node.ChildCount()); i++ {
				child := node.Child(i)
				if child != nil && child.Type() == "property_identifier" {
					props = append(props, string(src[child.StartByte():child.EndByte()]))
					break
				}
			}
		}

		for i := 0; i < int(node.ChildCount()); i++ {
			walk(node.Child(i))
		}
	}

	walk(elementNode)
	return props
}

// CSS extraction
func (t *TreeSitterDriver) extractCSSRules(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	// Extract CSS class selectors
	query1 := `(class_selector (class_name) @class.name) @selector`
	if qs, err := sitter.NewQuery([]byte(query1), lang); err == nil {
		t.runCSSClassQuery(pf, src, root, qs)
	}

	// Extract CSS ID selectors
	query2 := `(id_selector (id_name) @id.name) @selector`
	if qs, err := sitter.NewQuery([]byte(query2), lang); err == nil {
		t.runCSSIDQuery(pf, src, root, qs)
	}

	// Extract CSS variables (custom properties)
	query3 := `(declaration (property_name) @prop.name (plain_value) @prop.value)`
	if qs, err := sitter.NewQuery([]byte(query3), lang); err == nil {
		t.runCSSVariableQuery(pf, src, root, qs)
	}
}

func (t *TreeSitterDriver) runCSSClassQuery(pf *ParsedFile, src []byte, root *sitter.Node, qs *sitter.Query) {
	qc := sitter.NewQueryCursor()
	qc.Exec(qs, root)
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}

		for _, capture := range match.Captures {
			if qs.CaptureNameForId(capture.Index) == "class.name" {
				className := string(src[capture.Node.StartByte():capture.Node.EndByte()])
				pf.CSSRules = append(pf.CSSRules, model.CSSRuleEntity{
					Selector: "." + className,
					RuleType: "class",
					FilePath: pf.FilePath,
					Line:     int(capture.Node.StartPoint().Row) + 1,
				})
			}
		}
	}
}

func (t *TreeSitterDriver) runCSSIDQuery(pf *ParsedFile, src []byte, root *sitter.Node, qs *sitter.Query) {
	qc := sitter.NewQueryCursor()
	qc.Exec(qs, root)
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}

		for _, capture := range match.Captures {
			if qs.CaptureNameForId(capture.Index) == "id.name" {
				idName := string(src[capture.Node.StartByte():capture.Node.EndByte()])
				pf.CSSRules = append(pf.CSSRules, model.CSSRuleEntity{
					Selector: "#" + idName,
					RuleType: "id",
					FilePath: pf.FilePath,
					Line:     int(capture.Node.StartPoint().Row) + 1,
				})
			}
		}
	}
}

func (t *TreeSitterDriver) runCSSVariableQuery(pf *ParsedFile, src []byte, root *sitter.Node, qs *sitter.Query) {
	qc := sitter.NewQueryCursor()
	qc.Exec(qs, root)
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}

		var propName, propValue string
		for _, capture := range match.Captures {
			capName := qs.CaptureNameForId(capture.Index)
			if capName == "prop.name" {
				propName = string(src[capture.Node.StartByte():capture.Node.EndByte()])
			} else if capName == "prop.value" {
				propValue = string(src[capture.Node.StartByte():capture.Node.EndByte()])
			}
		}

		if strings.HasPrefix(propName, "--") {
			pf.CSSRules = append(pf.CSSRules, model.CSSRuleEntity{
				Selector:     propName,
				RuleType:     "variable",
				FilePath:     pf.FilePath,
				PropertyName: propName,
				Value:        propValue,
			})
		}
	}
}

// JavaScript extraction methods (similar to TypeScript but without type-specific features)
func (t *TreeSitterDriver) extractJSFunctions(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	// Same as TypeScript but without method signatures
	t.extractTSFunctions(pf, src, root, lang)
}

func (t *TreeSitterDriver) extractJSImports(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	t.extractTSImports(pf, src, root, lang)
}

func (t *TreeSitterDriver) extractJSVariables(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	t.extractTSVariables(pf, src, root, lang)
}

func (t *TreeSitterDriver) extractJSClasses(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	query := `(class_declaration) @class`
	qs, err := sitter.NewQuery([]byte(query), lang)
	if err != nil {
		log.Printf("Failed to compile JS class query: %v", err)
		return
	}

	qc := sitter.NewQueryCursor()
	qc.Exec(qs, root)
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}

		for _, capture := range match.Captures {
			classNode := capture.Node

			// Find the identifier child
			var className string
			var extendsClass string

			for i := 0; i < int(classNode.ChildCount()); i++ {
				child := classNode.Child(i)
				if child != nil {
					if child.Type() == "identifier" && className == "" {
						className = string(src[child.StartByte():child.EndByte()])
					} else if child.Type() == "class_heritage" {
						// Extract extends
						for j := 0; j < int(child.ChildCount()); j++ {
							grandchild := child.Child(j)
							if grandchild != nil && grandchild.Type() == "extends_clause" {
								for k := 0; k < int(grandchild.ChildCount()); k++ {
									ggchild := grandchild.Child(k)
									if ggchild != nil && ggchild.Type() == "identifier" {
										extendsClass = string(src[ggchild.StartByte():ggchild.EndByte()])
										break
									}
								}
							}
						}
					}
				}
			}

			if className != "" {
				pf.Classes = append(pf.Classes, model.ClassEntity{
					Name:     className,
					FilePath: pf.FilePath,
				})

				// Add extends relationship
				if extendsClass != "" {
					pf.Extends = append(pf.Extends, model.ExtendsEntity{
						ChildName:  className,
						ParentName: extendsClass,
						FilePath:   pf.FilePath,
					})
				}
			}
		}
	}
}

func (t *TreeSitterDriver) extractJSFunctionCalls(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	t.extractTSFunctionCalls(pf, src, root, lang)
}

func (t *TreeSitterDriver) extractTSVariables(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	query := `(variable_declarator name: (identifier) @var.name) @var.decl`
	qs, err := sitter.NewQuery([]byte(query), lang)
	if err != nil {
		return
	}

	qc := sitter.NewQueryCursor()
	qc.Exec(qs, root)
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}

		var nameNode, declNode *sitter.Node
		for _, capture := range match.Captures {
			capName := qs.CaptureNameForId(capture.Index)
			if capName == "var.name" {
				nameNode = capture.Node
			} else if capName == "var.decl" {
				declNode = capture.Node
			}
		}

		if nameNode != nil && declNode != nil {
			varName := string(src[nameNode.StartByte():nameNode.EndByte()])

			// Check if it has a function value (skip those)
			valueIdx := -1
			for i := 0; i < int(declNode.ChildCount()); i++ {
				child := declNode.Child(i)
				if child.Type() == "=" {
					if i+1 < int(declNode.ChildCount()) {
						valueIdx = i + 1
						break
					}
				}
			}

			if valueIdx != -1 {
				valueNode := declNode.Child(valueIdx)
				if valueNode.Type() == "arrow_function" || valueNode.Type() == "function" || valueNode.Type() == "function_expression" {
					continue // Skip function assignments
				}
			}

			pf.Variables = append(pf.Variables, model.VariableEntity{
				Name:     varName,
				FilePath: pf.FilePath,
				Type:     "variable",
			})
		}
	}
}

func (t *TreeSitterDriver) extractTSTypes(pf *ParsedFile, src []byte, root *sitter.Node, lang *sitter.Language) {
	query := `(type_alias_declaration name: (type_identifier) @type.name)`
	qs, err := sitter.NewQuery([]byte(query), lang)
	if err != nil {
		return
	}

	qc := sitter.NewQueryCursor()
	qc.Exec(qs, root)
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}

		for _, capture := range match.Captures {
			if qs.CaptureNameForId(capture.Index) == "type.name" {
				typeName := string(src[capture.Node.StartByte():capture.Node.EndByte()])
				pf.Types = append(pf.Types, model.TypeEntity{
					Name:     typeName,
					FilePath: pf.FilePath,
					Kind:     "type_alias",
				})
			}
		}
	}
}

// Post-processing: resolve function calls to their definitions
func (t *TreeSitterDriver) resolveFunctionCalls(pf *ParsedFile) {
	// Create a map of function names to their definitions
	funcMap := make(map[string]model.FunctionEntity)
	for _, fn := range pf.Funcs {
		funcMap[fn.Name] = fn
	}

	// Update function calls with resolved targets
	for i := range pf.FunctionCalls {
		call := &pf.FunctionCalls[i]

		// Simple resolution: check if the called function exists in the same file
		if fn, exists := funcMap[call.CalledFunc]; exists {
			call.ResolvedTarget = fn.Name
			call.TargetFile = fn.FilePath
		}

		// For method calls, try to resolve based on context
		if call.CallContext != "" && strings.Contains(call.CalledFunc, ".") {
			parts := strings.Split(call.CalledFunc, ".")
			if len(parts) == 2 {
				methodName := parts[1]
				// Look for the method in classes
				for _, class := range pf.Classes {
					if class.Name == call.CallContext {
						// Method might belong to this class
						for _, fn := range pf.Funcs {
							if fn.Name == methodName {
								call.ResolvedTarget = fn.Name
								call.TargetFile = fn.FilePath
								break
							}
						}
					}
				}
			}
		}
	}
}
