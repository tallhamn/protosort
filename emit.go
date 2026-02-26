package main

import (
	"strings"
)

// Emit produces the final reordered file content from sorted blocks.
func Emit(headerComments string, syntax *Block, pkg *Block, options []*Block, imports []*Block, extends []*Block, body []*Block) string {
	var out strings.Builder

	// File header comments (license, etc.)
	headerComments = cleanComments(headerComments)
	if headerComments != "" {
		out.WriteString(headerComments)
		if !strings.HasSuffix(headerComments, "\n") {
			out.WriteByte('\n')
		}
	}

	// Syntax statement
	if syntax != nil {
		out.WriteString(syntax.DeclText)
		if !strings.HasSuffix(syntax.DeclText, "\n") {
			out.WriteByte('\n')
		}
	}

	// Package statement
	if pkg != nil {
		out.WriteByte('\n')
		writeBlockWithComments(&out, pkg)
	}

	// Options (sorted)
	for _, opt := range options {
		out.WriteByte('\n')
		writeBlockWithComments(&out, opt)
	}

	// Extend blocks (custom options go in header)
	for _, ext := range extends {
		out.WriteByte('\n')
		writeBlockWithComments(&out, ext)
	}

	// Imports (sorted)
	if len(imports) > 0 {
		out.WriteByte('\n')
		for i, imp := range imports {
			if i > 0 {
				out.WriteByte('\n')
			}
			writeBlockWithComments(&out, imp)
		}
	}

	// Body (services, request/response, core, helpers, unreferenced)
	for _, b := range body {
		out.WriteByte('\n')
		writeBlockWithComments(&out, b)
	}

	result := out.String()

	// Ensure file ends with a single newline
	result = strings.TrimRight(result, "\n") + "\n"

	return result
}

// writeBlockWithComments writes a block's comments and declaration text to the builder.
func writeBlockWithComments(out *strings.Builder, b *Block) {
	comments := cleanComments(b.Comments)
	if comments != "" {
		out.WriteString(comments)
		if !strings.HasSuffix(comments, "\n") {
			out.WriteByte('\n')
		}
	}
	out.WriteString(b.DeclText)
	if !strings.HasSuffix(b.DeclText, "\n") {
		out.WriteByte('\n')
	}
}

// cleanComments removes leading/trailing blank lines from a comment block
// while preserving internal blank lines (paragraph separators).
func cleanComments(s string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")

	// Trim leading blank lines
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}

	// Trim trailing blank lines
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}

	if start >= end {
		return ""
	}

	return strings.Join(lines[start:end], "\n")
}
