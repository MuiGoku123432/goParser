// internal/monitor/diff_analyzer.go

package monitor

import (
	"goParse/internal/driver"
	"goParse/internal/model"
	"reflect"
)

// DiffAnalyzer compares parsed files to determine what actually changed
type DiffAnalyzer struct {
	cache map[string]*CachedParse
}

// CachedParse stores the previous parse result
type CachedParse struct {
	ParsedFile driver.ParsedFile
	Hash       string
}

// EntityChanges represents what changed in a file
type EntityChanges struct {
	FilePath string

	// Entity changes
	AddedFunctions    []model.FunctionEntity
	ModifiedFunctions []model.FunctionEntity
	RemovedFunctions  []model.FunctionEntity

	AddedClasses    []model.ClassEntity
	ModifiedClasses []model.ClassEntity
	RemovedClasses  []model.ClassEntity

	AddedInterfaces    []model.InterfaceEntity
	ModifiedInterfaces []model.InterfaceEntity
	RemovedInterfaces  []model.InterfaceEntity

	AddedTypes    []model.TypeEntity
	ModifiedTypes []model.TypeEntity
	RemovedTypes  []model.TypeEntity

	// Relationship changes
	AddedImports   []model.ImportEntity
	RemovedImports []model.ImportEntity

	AddedFunctionCalls   []model.FunctionCallEntity
	RemovedFunctionCalls []model.FunctionCallEntity
}

// NewDiffAnalyzer creates a new diff analyzer
func NewDiffAnalyzer() *DiffAnalyzer {
	return &DiffAnalyzer{
		cache: make(map[string]*CachedParse),
	}
}

// AnalyzeChanges compares old and new parse results
func (da *DiffAnalyzer) AnalyzeChanges(filePath string, newParse driver.ParsedFile) (*EntityChanges, bool) {
	changes := &EntityChanges{FilePath: filePath}
	hasChanges := false

	// Get cached parse
	cached, exists := da.cache[filePath]
	if !exists {
		// First time seeing this file, everything is new
		changes.AddedFunctions = newParse.Funcs
		changes.AddedClasses = newParse.Classes
		changes.AddedInterfaces = newParse.Interfaces
		changes.AddedTypes = newParse.Types
		changes.AddedImports = newParse.Imports
		changes.AddedFunctionCalls = newParse.FunctionCalls

		// Cache the parse
		da.cache[filePath] = &CachedParse{
			ParsedFile: newParse,
		}

		return changes, true
	}

	oldParse := cached.ParsedFile

	// Analyze function changes
	funcChanges := da.analyzeFunctionChanges(oldParse.Funcs, newParse.Funcs)
	if funcChanges.hasChanges() {
		hasChanges = true
		changes.AddedFunctions = funcChanges.added
		changes.ModifiedFunctions = funcChanges.modified
		changes.RemovedFunctions = funcChanges.removed
	}

	// Analyze class changes
	classChanges := da.analyzeClassChanges(oldParse.Classes, newParse.Classes)
	if classChanges.hasChanges() {
		hasChanges = true
		changes.AddedClasses = classChanges.added
		changes.ModifiedClasses = classChanges.modified
		changes.RemovedClasses = classChanges.removed
	}

	// Analyze other entity types...
	// (Similar analysis for interfaces, types, etc.)

	// Update cache if there were changes
	if hasChanges {
		da.cache[filePath] = &CachedParse{
			ParsedFile: newParse,
		}
	}

	return changes, hasChanges
}

// analyzeFunctionChanges compares function lists
func (da *DiffAnalyzer) analyzeFunctionChanges(oldFuncs, newFuncs []model.FunctionEntity) entityDiff[model.FunctionEntity] {
	diff := entityDiff[model.FunctionEntity]{}

	// Create maps for efficient lookup
	oldMap := make(map[string]model.FunctionEntity)
	for _, f := range oldFuncs {
		oldMap[f.Name] = f
	}

	newMap := make(map[string]model.FunctionEntity)
	for _, f := range newFuncs {
		newMap[f.Name] = f
	}

	// Find added and modified functions
	for name, newFunc := range newMap {
		if oldFunc, exists := oldMap[name]; exists {
			// Check if modified
			if !da.functionsEqual(oldFunc, newFunc) {
				diff.modified = append(diff.modified, newFunc)
			}
		} else {
			// New function
			diff.added = append(diff.added, newFunc)
		}
	}

	// Find removed functions
	for name, oldFunc := range oldMap {
		if _, exists := newMap[name]; !exists {
			diff.removed = append(diff.removed, oldFunc)
		}
	}

	return diff
}

// analyzeClassChanges compares class lists
func (da *DiffAnalyzer) analyzeClassChanges(oldClasses, newClasses []model.ClassEntity) entityDiff[model.ClassEntity] {
	diff := entityDiff[model.ClassEntity]{}

	oldMap := make(map[string]model.ClassEntity)
	for _, c := range oldClasses {
		oldMap[c.Name] = c
	}

	newMap := make(map[string]model.ClassEntity)
	for _, c := range newClasses {
		newMap[c.Name] = c
	}

	for name, newClass := range newMap {
		if oldClass, exists := oldMap[name]; exists {
			if !da.classesEqual(oldClass, newClass) {
				diff.modified = append(diff.modified, newClass)
			}
		} else {
			diff.added = append(diff.added, newClass)
		}
	}

	for name, oldClass := range oldMap {
		if _, exists := newMap[name]; !exists {
			diff.removed = append(diff.removed, oldClass)
		}
	}

	return diff
}

// Helper types and methods
type entityDiff[T any] struct {
	added    []T
	modified []T
	removed  []T
}

func (ed entityDiff[T]) hasChanges() bool {
	return len(ed.added) > 0 || len(ed.modified) > 0 || len(ed.removed) > 0
}

func (da *DiffAnalyzer) functionsEqual(f1, f2 model.FunctionEntity) bool {
	return f1.StartLine == f2.StartLine &&
		f1.EndLine == f2.EndLine &&
		f1.Signature == f2.Signature &&
		f1.IsAsync == f2.IsAsync &&
		f1.IsExport == f2.IsExport
}

func (da *DiffAnalyzer) classesEqual(c1, c2 model.ClassEntity) bool {
	return c1.StartLine == c2.StartLine &&
		c1.EndLine == c2.EndLine &&
		c1.IsExport == c2.IsExport &&
		c1.IsAbstract == c2.IsAbstract &&
		reflect.DeepEqual(c1.Methods, c2.Methods)
}

// RemoveFromCache removes a file from the cache
func (da *DiffAnalyzer) RemoveFromCache(filePath string) {
	delete(da.cache, filePath)
}

// GetCacheSize returns the number of cached files
func (da *DiffAnalyzer) GetCacheSize() int {
	return len(da.cache)
}
