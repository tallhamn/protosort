package main

import (
	"regexp"
	"strings"
)

// Pre-compiled regexes for declaration parsing.
var (
	rpcRe          = regexp.MustCompile(`rpc\s+(\w+)\s*\(\s*(?:stream\s+)?([\w.]+)\s*\)\s*returns\s*\(\s*(?:stream\s+)?([\w.]+)\s*\)`)
	fieldRe        = regexp.MustCompile(`(?m)^\s*(?:repeated\s+|optional\s+)?([\w.]+)\s+\w+\s*=\s*\d+`)
	mapFieldRe     = regexp.MustCompile(`map\s*<\s*[\w.]+\s*,\s*([\w.]+)\s*>\s*\w+\s*=\s*\d+`)
	oneofRe        = regexp.MustCompile(`(?s)oneof\s+\w+\s*\{([^}]*)\}`)
	oneofVariantRe = regexp.MustCompile(`(?m)^\s*([\w.]+)\s+\w+\s*=\s*\d+`)
)

// ExtractRPCs parses RPC declarations from a service block's DeclText.
func ExtractRPCs(block *Block) []RPC {
	if block.Kind != BlockService {
		return nil
	}
	var rpcs []RPC
	matches := rpcRe.FindAllStringSubmatch(block.DeclText, -1)
	for _, m := range matches {
		rpcs = append(rpcs, RPC{
			Name:         m[1],
			RequestType:  m[2],
			ResponseType: m[3],
		})
	}
	return rpcs
}

// ExtractFieldTypes extracts type names referenced by fields in a message or extend block.
// Each type name is returned at most once per block (per spec: multiple fields referencing
// the same type from one message count as one reference).
func ExtractFieldTypes(block *Block) []string {
	if block.Kind != BlockMessage && block.Kind != BlockExtend {
		return nil
	}

	body := extractBody(block.DeclText)
	seen := make(map[string]bool)
	var types []string

	addType := func(t string) {
		// Package-qualified names (containing dots) are imported types — skip them.
		// Only count references to locally-defined types (simple names).
		if strings.Contains(t, ".") {
			return
		}
		if t != "" && !isScalarType(t) && !seen[t] {
			seen[t] = true
			types = append(types, t)
		}
	}

	// Match regular fields: [repeated|optional] TypeName field_name = N;
	for _, m := range fieldRe.FindAllStringSubmatch(body, -1) {
		addType(m[1])
	}

	// Match map fields: map<KeyType, ValueType> field_name = N;
	for _, m := range mapFieldRe.FindAllStringSubmatch(body, -1) {
		addType(m[1])
	}

	// Match oneof variant types
	for _, m := range oneofRe.FindAllStringSubmatch(body, -1) {
		oneofBody := m[1]
		for _, v := range oneofVariantRe.FindAllStringSubmatch(oneofBody, -1) {
			addType(v[1])
		}
	}

	return types
}

// BuildRefCounts counts how many distinct declarations reference each type name.
// Only types defined in the file are tracked.
// Per spec: circular references between types make both "core" (ref_count >= 2).
func BuildRefCounts(blocks []*Block) map[string]int {
	// Collect names of all types defined in this file.
	// Extend blocks don't define types; they extend external types.
	defined := make(map[string]bool)
	for _, b := range blocks {
		if b.Kind == BlockMessage || b.Kind == BlockEnum {
			if b.Name != "" {
				defined[b.Name] = true
			}
		}
	}

	counts := make(map[string]int)

	// Build a "uses" graph: who references whom
	uses := make(map[string]map[string]bool) // uses[A] = {B, C} means A references B and C

	for _, b := range blocks {
		var refs []string

		switch b.Kind {
		case BlockMessage, BlockExtend:
			refs = ExtractFieldTypes(b)
		case BlockService:
			for _, rpc := range ExtractRPCs(b) {
				refs = append(refs, rpc.RequestType, rpc.ResponseType)
			}
		default:
			continue
		}

		// Deduplicate refs from this declaration
		seen := make(map[string]bool)
		for _, ref := range refs {
			// Skip self-references (e.g., message TreeNode { TreeNode child = 1; })
			if ref == b.Name {
				continue
			}
			if defined[ref] && !seen[ref] {
				seen[ref] = true
				counts[ref]++
				if b.Name != "" {
					if uses[b.Name] == nil {
						uses[b.Name] = make(map[string]bool)
					}
					uses[b.Name][ref] = true
				}
			}
		}
	}

	// Detect circular references and boost counts to >= 2
	for a, aRefs := range uses {
		for b := range aRefs {
			if uses[b] != nil && uses[b][a] {
				// a and b reference each other: ensure both are "core"
				if counts[a] < 2 {
					counts[a] = 2
				}
				if counts[b] < 2 {
					counts[b] = 2
				}
			}
		}
	}

	return counts
}

// BuildRefGraph maps each type name to the set of declarations that reference it.
func BuildRefGraph(blocks []*Block) map[string][]string {
	defined := make(map[string]bool)
	for _, b := range blocks {
		if b.Kind == BlockMessage || b.Kind == BlockEnum {
			if b.Name != "" {
				defined[b.Name] = true
			}
		}
	}

	graph := make(map[string][]string)

	for _, b := range blocks {
		if b.Name == "" {
			continue
		}

		var refs []string
		switch b.Kind {
		case BlockMessage, BlockExtend:
			refs = ExtractFieldTypes(b)
		case BlockService:
			for _, rpc := range ExtractRPCs(b) {
				refs = append(refs, rpc.RequestType, rpc.ResponseType)
			}
		default:
			continue
		}

		seen := make(map[string]bool)
		for _, ref := range refs {
			// Skip self-references
			if ref == b.Name {
				continue
			}
			if defined[ref] && !seen[ref] {
				seen[ref] = true
				graph[ref] = append(graph[ref], b.Name)
			}
		}
	}

	return graph
}

// extractBody returns the text between the first { and last } in a declaration.
func extractBody(declText string) string {
	start := strings.IndexByte(declText, '{')
	end := strings.LastIndexByte(declText, '}')
	if start < 0 || end < 0 || end <= start {
		return ""
	}
	return declText[start+1 : end]
}

// isScalarType returns true for proto3 built-in scalar types.
func isScalarType(name string) bool {
	switch name {
	case "double", "float",
		"int32", "int64", "uint32", "uint64", "sint32", "sint64",
		"fixed32", "fixed64", "sfixed32", "sfixed64",
		"bool", "string", "bytes":
		return true
	}
	return false
}
