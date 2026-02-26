package main

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// rpcEntry represents a single RPC declaration within a service body,
// including its leading comments and the full RPC text (which may span
// multiple lines if it has an option body).
type rpcEntry struct {
	Comments string // leading comment lines
	RPCText  string // the rpc line(s) including option body
	Name     string // extracted RPC method name
}

// SortRPCsInService reorders RPC declarations within a service block's DeclText.
// mode is "alpha" (alphabetical by name) or "grouped" (group by resource, then alpha).
// Non-RPC content (like service-level options) is preserved at the top of the body.
func SortRPCsInService(declText, mode string) string {
	// Find the opening and closing braces
	openIdx := strings.IndexByte(declText, '{')
	closeIdx := strings.LastIndexByte(declText, '}')
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		return declText
	}

	header := declText[:openIdx+1]
	body := declText[openIdx+1 : closeIdx]
	trailer := declText[closeIdx:]

	entries, nonRPCLines := parseRPCEntries(body)
	if len(entries) <= 1 {
		return declText
	}

	// Sort entries
	switch mode {
	case "alpha":
		sort.SliceStable(entries, func(i, j int) bool {
			return entries[i].Name < entries[j].Name
		})
	case "grouped":
		sort.SliceStable(entries, func(i, j int) bool {
			gi, gj := rpcGroupKey(entries[i].Name), rpcGroupKey(entries[j].Name)
			if gi != gj {
				return gi < gj
			}
			return entries[i].Name < entries[j].Name
		})
	default:
		return declText
	}

	// Reconstruct body
	var out strings.Builder
	out.WriteByte('\n') // newline after opening brace
	// Non-RPC lines (service options) first
	for _, line := range nonRPCLines {
		out.WriteString(line)
		out.WriteByte('\n')
	}
	// Then sorted RPCs
	for _, e := range entries {
		if e.Comments != "" {
			out.WriteString(e.Comments)
		}
		out.WriteString(e.RPCText)
	}

	return header + out.String() + trailer
}

// rpcLineRe matches the start of an RPC declaration.
var rpcLineRe = regexp.MustCompile(`^\s*rpc\s+(\w+)\s*\(`)

// parseRPCEntries parses the body of a service block into RPC entries and
// non-RPC lines (such as service-level options).
func parseRPCEntries(body string) ([]rpcEntry, []string) {
	lines := strings.Split(body, "\n")
	var entries []rpcEntry
	var nonRPCLines []string
	var commentBuf strings.Builder
	var rpcBuf strings.Builder
	var currentName string
	inRPC := false
	braceDepth := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if inRPC {
			rpcBuf.WriteString(line)
			rpcBuf.WriteByte('\n')
			braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
			if braceDepth <= 0 {
				// Check if the line ends the RPC (semicolon or closing brace)
				if strings.Contains(trimmed, ";") || strings.Contains(trimmed, "}") {
					entries = append(entries, rpcEntry{
						Comments: commentBuf.String(),
						RPCText:  rpcBuf.String(),
						Name:     currentName,
					})
					commentBuf.Reset()
					rpcBuf.Reset()
					inRPC = false
					braceDepth = 0
				}
			}
			continue
		}

		// Check for RPC start
		if m := rpcLineRe.FindStringSubmatch(line); m != nil {
			currentName = m[1]
			inRPC = true
			braceDepth = strings.Count(line, "{") - strings.Count(line, "}")
			rpcBuf.WriteString(line)
			rpcBuf.WriteByte('\n')

			// Check if the RPC is complete on one line (ends with ; at depth 0)
			if braceDepth <= 0 && strings.Contains(trimmed, ";") {
				entries = append(entries, rpcEntry{
					Comments: commentBuf.String(),
					RPCText:  rpcBuf.String(),
					Name:     currentName,
				})
				commentBuf.Reset()
				rpcBuf.Reset()
				inRPC = false
				braceDepth = 0
			}
			continue
		}

		// Comment line (attach to next RPC)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
			commentBuf.WriteString(line)
			commentBuf.WriteByte('\n')
			continue
		}

		// Blank line
		if trimmed == "" {
			// If we have pending comments, include the blank line in the comment block
			if commentBuf.Len() > 0 {
				commentBuf.WriteString(line)
				commentBuf.WriteByte('\n')
			}
			continue
		}

		// Non-RPC, non-comment line (e.g., service-level option)
		// Flush any pending comments as non-RPC content too
		if commentBuf.Len() > 0 {
			for _, cl := range strings.Split(strings.TrimRight(commentBuf.String(), "\n"), "\n") {
				nonRPCLines = append(nonRPCLines, cl)
			}
			commentBuf.Reset()
		}
		nonRPCLines = append(nonRPCLines, line)
	}

	// If there's a trailing incomplete RPC (shouldn't happen in valid proto), add it
	if inRPC && rpcBuf.Len() > 0 {
		entries = append(entries, rpcEntry{
			Comments: commentBuf.String(),
			RPCText:  rpcBuf.String(),
			Name:     currentName,
		})
	}

	return entries, nonRPCLines
}

// Known verb prefixes for RPC grouping, ordered longest-first to avoid
// false prefix matches (e.g., "BatchCreate" before "Create").
var rpcVerbPrefixes = []string{
	"BatchCreate", "BatchDelete", "BatchGet", "BatchUpdate",
	"Create", "Delete", "Get", "List", "Update",
	"Watch", "Stream", "Search",
	"Set", "Add", "Remove",
	"Start", "Stop", "Run", "Check", "Cancel",
}

// rpcGroupKey strips a known verb prefix from an RPC name to derive the
// resource name for grouping. Only strips if the character after the prefix
// is uppercase (to avoid false matches). Returns the full name if no prefix
// matches or the name equals the prefix exactly.
func rpcGroupKey(name string) string {
	for _, prefix := range rpcVerbPrefixes {
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			next := rune(name[len(prefix)])
			if unicode.IsUpper(next) {
				return name[len(prefix):]
			}
		}
	}
	return name
}
