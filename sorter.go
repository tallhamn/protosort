package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Sort takes proto file content and returns the reordered content.
func Sort(content string, opts Options) (string, []string, error) {
	var warnings []string

	// Check for proto2
	if isProto2(content) {
		return "", nil, &Proto2Error{}
	}

	blocks, err := ScanFile(content)
	if err != nil {
		return "", nil, &ParseError{Err: err}
	}

	if len(blocks) == 0 {
		return content, nil, nil
	}

	// When preserving dividers, attach freestanding divider comments to the
	// following declaration before any other processing.
	if opts.PreserveDividers {
		blocks = attachDividerComments(blocks)
	}

	// Process comments on all blocks
	for _, b := range blocks {
		// Strip section headers first (before divider stripping, since the
		// banner lines would be caught by the divider regex and break the
		// 3-line pattern match).
		b.Comments = stripSectionHeaders(b.Comments)
		processComments(b, opts)
		// If not preserving dividers, strip section divider comments from block comments
		if !opts.PreserveDividers {
			b.Comments = stripDividerComments(b.Comments)
		}
	}

	// Sort RPCs within services if requested (before extracting RPC info)
	if opts.SortRPCs != "" {
		for _, b := range blocks {
			if b.Kind == BlockService {
				b.DeclText = SortRPCsInService(b.DeclText, opts.SortRPCs)
			}
		}
	}

	// Populate RPC info on service blocks
	for _, b := range blocks {
		if b.Kind == BlockService {
			b.RPCs = ExtractRPCs(b)
		}
	}

	// Separate header blocks from body blocks
	var headerComments string
	var syntaxBlock, packageBlock *Block
	var optionBlocks, importBlocks []*Block
	var extendBlocks []*Block
	var bodyBlocks []*Block

	for _, b := range blocks {
		switch b.Kind {
		case BlockSyntax:
			headerComments = b.Comments
			syntaxBlock = b
		case BlockPackage:
			packageBlock = b
		case BlockOption:
			optionBlocks = append(optionBlocks, b)
		case BlockImport:
			importBlocks = append(importBlocks, b)
		case BlockExtend:
			extendBlocks = append(extendBlocks, b)
		case BlockMessage, BlockEnum:
			bodyBlocks = append(bodyBlocks, b)
		case BlockService:
			bodyBlocks = append(bodyBlocks, b)
		case BlockComment:
			// Freestanding comments between declarations are dropped
			// (they become section dividers that don't survive reordering)
		}
	}

	// Sort options alphabetically by name
	sort.Slice(optionBlocks, func(i, j int) bool {
		return optionBlocks[i].Name < optionBlocks[j].Name
	})

	// Sort imports alphabetically by path
	sort.Slice(importBlocks, func(i, j int) bool {
		return importBlocks[i].Name < importBlocks[j].Name
	})

	// Build reference counts and graph
	refCounts := BuildRefCounts(bodyBlocks)
	refGraph := BuildRefGraph(bodyBlocks)

	// Classify body blocks
	serviceBlocks, rpcMessages, remainingBlocks := classifyServiceAndRPC(bodyBlocks)

	// Classify remaining types
	var coreBlocks, helperBlocks, unrefBlocks []*Block

	// Build set of RPC message names for exclusion
	rpcMsgNames := make(map[string]bool)
	for _, b := range rpcMessages {
		rpcMsgNames[b.Name] = true
	}
	svcNames := make(map[string]bool)
	for _, b := range serviceBlocks {
		svcNames[b.Name] = true
	}

	// Build outgoing references map: what local types does each type reference?
	outgoingRefs := make(map[string][]string)
	defined := make(map[string]bool)
	for _, b := range bodyBlocks {
		if b.Kind == BlockMessage || b.Kind == BlockEnum {
			defined[b.Name] = true
		}
	}
	for _, b := range bodyBlocks {
		var refs []string
		switch b.Kind {
		case BlockMessage, BlockExtend:
			refs = ExtractFieldTypes(b)
		}
		// Filter to only local types
		var localRefs []string
		for _, ref := range refs {
			if defined[ref] && ref != b.Name {
				localRefs = append(localRefs, ref)
			}
		}
		if len(localRefs) > 0 {
			outgoingRefs[b.Name] = localRefs
		}
	}

	for _, b := range remainingBlocks {
		if svcNames[b.Name] || rpcMsgNames[b.Name] {
			continue // already placed
		}

		hasOutgoingRefs := len(outgoingRefs[b.Name]) > 0
		incomingRefCount := refCounts[b.Name]

		if hasOutgoingRefs {
			// Composite: references other local types
			b.Section = SectionCore
			coreBlocks = append(coreBlocks, b)
		} else if incomingRefCount > 0 {
			// Helper: doesn't reference local types, but is referenced by others
			b.Section = SectionHelper
			// Store all consumers for potential use
			if refs, ok := refGraph[b.Name]; ok && len(refs) > 0 {
				b.Consumer = refs[0] // primary consumer
			}
			helperBlocks = append(helperBlocks, b)
		} else {
			// Standalone: doesn't reference local types and isn't referenced
			b.Section = SectionUnreferenced
			unrefBlocks = append(unrefBlocks, b)
		}
	}

	// Sort core types
	if opts.SharedOrder == "dependency" {
		coreBlocks = topoSortBlocks(coreBlocks, bodyBlocks)
	} else {
		sort.Slice(coreBlocks, func(i, j int) bool {
			return coreBlocks[i].Name < coreBlocks[j].Name
		})
	}

	// Sort unreferenced types alphabetically
	sort.Slice(unrefBlocks, func(i, j int) bool {
		return unrefBlocks[i].Name < unrefBlocks[j].Name
	})

	// Build helper map: consumer -> [helpers]
	helperMap := make(map[string][]*Block)
	for _, h := range helperBlocks {
		helperMap[h.Consumer] = append(helperMap[h.Consumer], h)
	}
	for consumer := range helperMap {
		sort.Slice(helperMap[consumer], func(i, j int) bool {
			return helperMap[consumer][i].Name < helperMap[consumer][j].Name
		})
	}

	// Build final ordered list
	var ordered []*Block
	emitted := make(map[string]bool)

	var emitWithHelpers func(b *Block)
	emitWithHelpers = func(b *Block) {
		if emitted[b.Name] {
			return
		}
		// Emit helpers for this block first
		if helpers, ok := helperMap[b.Name]; ok {
			for _, h := range helpers {
				emitWithHelpers(h)
			}
		}
		emitted[b.Name] = true
		ordered = append(ordered, b)
	}

	// Section 2: Services and request/response pairs
	for _, svc := range serviceBlocks {
		svc.Section = SectionService
		emitted[svc.Name] = true
		ordered = append(ordered, svc)
	}
	for _, msg := range rpcMessages {
		msg.Section = SectionRequestResponse
		emitWithHelpers(msg)
	}

	// Section 3: Standalone types (unreferenced) - emit without inline helpers
	for _, unref := range unrefBlocks {
		if !emitted[unref.Name] {
			emitted[unref.Name] = true
			ordered = append(ordered, unref)
		}
	}

	// Section 4: Composite types (core) - emit without inline helpers
	for _, core := range coreBlocks {
		if !emitted[core.Name] {
			emitted[core.Name] = true
			ordered = append(ordered, core)
		}
	}

	// Section 5: All helper types
	for _, h := range helperBlocks {
		if !emitted[h.Name] {
			emitted[h.Name] = true
			ordered = append(ordered, h)
		}
	}

	// Inject classification annotations if requested
	if opts.Annotate {
		annotateBlocks(ordered, refGraph)
	}

	// Inject section headers if requested (stripping was done earlier)
	if opts.SectionHeaders {
		injectSectionHeaders(ordered, serviceBlocks)
	}

	// Build the output
	output := Emit(headerComments, syntaxBlock, packageBlock, optionBlocks, importBlocks, extendBlocks, ordered)

	return output, warnings, nil
}

// topoSortBlocks orders core blocks so that referenced types appear before
// referencing types (Kahn's algorithm). Uses alphabetical tie-breaking.
// If cycles exist, falls back to alphabetical order for the cycle members.
func topoSortBlocks(coreBlocks []*Block, allBlocks []*Block) []*Block {
	if len(coreBlocks) <= 1 {
		return coreBlocks
	}

	// Build set of core block names
	coreSet := make(map[string]bool)
	coreMap := make(map[string]*Block)
	for _, b := range coreBlocks {
		coreSet[b.Name] = true
		coreMap[b.Name] = b
	}

	// Build adjacency: edges[A] = {B, C} means A references B and C (among core blocks)
	// We want B, C to appear before A (dependencies first).
	inDegree := make(map[string]int)
	dependents := make(map[string][]string) // dependents[B] = [A] means A depends on B
	for _, b := range coreBlocks {
		inDegree[b.Name] = 0
	}

	for _, b := range allBlocks {
		if !coreSet[b.Name] {
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
		}
		seen := make(map[string]bool)
		for _, ref := range refs {
			if ref == b.Name {
				continue
			}
			if coreSet[ref] && !seen[ref] {
				seen[ref] = true
				inDegree[b.Name]++
				dependents[ref] = append(dependents[ref], b.Name)
			}
		}
	}

	// Kahn's algorithm with alphabetical tie-breaking via sorted queue
	var queue []string
	for _, b := range coreBlocks {
		if inDegree[b.Name] == 0 {
			queue = append(queue, b.Name)
		}
	}
	sort.Strings(queue)

	var result []*Block
	for len(queue) > 0 {
		// Pop first (alphabetically smallest)
		name := queue[0]
		queue = queue[1:]
		result = append(result, coreMap[name])

		for _, dep := range dependents[name] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
				sort.Strings(queue)
			}
		}
	}

	// If cycles prevented some nodes from being emitted, add them alphabetically
	if len(result) < len(coreBlocks) {
		emitted := make(map[string]bool)
		for _, b := range result {
			emitted[b.Name] = true
		}
		var remaining []*Block
		for _, b := range coreBlocks {
			if !emitted[b.Name] {
				remaining = append(remaining, b)
			}
		}
		sort.Slice(remaining, func(i, j int) bool {
			return remaining[i].Name < remaining[j].Name
		})
		result = append(result, remaining...)
	}

	return result
}

// classifyServiceAndRPC separates service blocks and their RPC request/response
// messages from the rest. Messages appear in RPC declaration order.
func classifyServiceAndRPC(blocks []*Block) (services []*Block, rpcMessages []*Block, remaining []*Block) {
	blockMap := make(map[string]*Block)
	for _, b := range blocks {
		if b.Name != "" {
			blockMap[b.Name] = b
		}
	}

	// Find service blocks (preserve original order)
	var svcBlocks []*Block
	for _, b := range blocks {
		if b.Kind == BlockService {
			svcBlocks = append(svcBlocks, b)
		}
	}

	if len(svcBlocks) == 0 {
		return nil, nil, blocks
	}

	// Collect RPC request/response message names in order
	rpcMsgNames := make(map[string]bool)
	var rpcMsgs []*Block
	emitted := make(map[string]bool)

	for _, svc := range svcBlocks {
		for _, rpc := range svc.RPCs {
			for _, typeName := range []string{rpc.RequestType, rpc.ResponseType} {
				if b, ok := blockMap[typeName]; ok && !emitted[typeName] {
					emitted[typeName] = true
					rpcMsgNames[typeName] = true
					rpcMsgs = append(rpcMsgs, b)
				}
			}
		}
	}

	// Remaining blocks: everything not a service and not an RPC message
	var rest []*Block
	for _, b := range blocks {
		if b.Kind != BlockService && !rpcMsgNames[b.Name] {
			rest = append(rest, b)
		}
	}

	return svcBlocks, rpcMsgs, rest
}

// processComments applies --strip-commented-code to block comments.
func processComments(b *Block, opts Options) {
	if b.Comments == "" {
		return
	}
	if opts.StripCommented {
		b.Comments = stripCommentedCode(b.Comments)
	}
}

// stripCommentedCode removes comment blocks that consist entirely of commented-out
// protobuf declarations (e.g., "// rpc Foo(...)" or "// message Bar {}") with no other prose.
// Comment blocks separated by blank lines are evaluated independently.
func stripCommentedCode(comments string) string {
	lines := strings.Split(comments, "\n")
	var result []string

	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])

		// Not a comment line — keep it (blank lines, etc.)
		if !strings.HasPrefix(trimmed, "//") {
			result = append(result, lines[i])
			i++
			continue
		}

		// Collect a contiguous block of // comment lines
		blockStart := i
		for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "//") {
			i++
		}
		block := lines[blockStart:i]

		if isCommentedOutCode(block) {
			// Drop this block
			continue
		}

		result = append(result, block...)
	}

	return strings.Join(result, "\n")
}

// Pre-compiled regex for detecting commented-out proto code.
var codeLineRe = regexp.MustCompile(`^\s*//\s*(` +
	`(message|enum|service|extend)\s+\w+` + // message Foo, enum Bar
	`|rpc\s+\w+\s*\(` + // rpc Method(
	`|(import|option|package|syntax)\s+` + // import "...", option ...
	`|(repeated|optional)\s+\w+\s+\w+\s*=` + // repeated Foo bar = N
	`|\w+\s+\w+\s*=\s*\d+` + // Foo bar = 1 (field declaration)
	`|[{}();]` + // braces, parens, semicolons alone
	`|returns\s*\(` + // returns (
	`)`)

// isCommentedOutCode checks if every line in a comment block looks like
// commented-out proto code rather than prose.
func isCommentedOutCode(lines []string) bool {
	if len(lines) == 0 {
		return false
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "//" {
			continue // empty comment line is neutral
		}
		if !codeLineRe.MatchString(line) {
			return false // this line looks like prose
		}
	}
	return true
}

// Pre-compiled regexes for section divider detection.
var (
	dividerBothSidesRe = regexp.MustCompile(`^//\s*[=\-*#]{3,}\s*(\w+\s*){0,3}[=\-*#]{3,}\s*$`)
	dividerOneSideRe   = regexp.MustCompile(`^//\s*[=\-*#]{3,}\s+(\w+\s*){1,3}$`)
)

// isSectionDivider checks if a comment looks like a section divider.
// Matches patterns like "// === Messages ===" or "// --- Types" but not
// prose comments that happen to contain dashes like "// --- See docs for details ---".
func isSectionDivider(comment string) bool {
	trimmed := strings.TrimSpace(comment)
	return dividerBothSidesRe.MatchString(trimmed) || dividerOneSideRe.MatchString(trimmed)
}

// stripDividerComments removes lines that look like section dividers from a comment block.
func stripDividerComments(comments string) string {
	if comments == "" {
		return ""
	}
	lines := strings.Split(comments, "\n")
	var result []string
	for _, line := range lines {
		if !isSectionDivider(line) {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}

// annotationRe matches annotation comments injected by --annotate so they can
// be stripped before re-injection, ensuring idempotency.
var annotationRe = regexp.MustCompile(`(?m)^//\s*\((core: referenced by |helper: used only by |request/response|unreferenced)\)?[^\n]*$`)

// annotateBlocks injects classification annotations into block Comments.
// Annotations like "// (core: referenced by X, Y)" or "// (helper: used only by Z)".
// Existing annotations are stripped first to ensure idempotency.
func annotateBlocks(blocks []*Block, refGraph map[string][]string) {
	for _, b := range blocks {
		if b.Kind != BlockMessage && b.Kind != BlockEnum {
			continue
		}

		var annotation string
		switch b.Section {
		case SectionRequestResponse:
			annotation = "// (request/response)"
		case SectionCore:
			// Copy slice to avoid mutating the shared refGraph
			refs := make([]string, len(refGraph[b.Name]))
			copy(refs, refGraph[b.Name])
			sort.Strings(refs)
			annotation = fmt.Sprintf("// (core: referenced by %s)", strings.Join(refs, ", "))
		case SectionHelper:
			annotation = fmt.Sprintf("// (helper: used only by %s)", b.Consumer)
		case SectionUnreferenced:
			annotation = "// (unreferenced)"
		default:
			continue
		}

		// Strip any existing annotations first (idempotency)
		comments := stripAnnotations(b.Comments)
		comments = strings.TrimRight(comments, "\n \t")
		if comments != "" {
			b.Comments = comments + "\n" + annotation + "\n"
		} else {
			b.Comments = annotation + "\n"
		}
	}
}

// stripAnnotations removes previously-injected annotation lines from a comment block.
func stripAnnotations(comments string) string {
	if comments == "" {
		return ""
	}
	lines := strings.Split(comments, "\n")
	var result []string
	for _, line := range lines {
		if !annotationRe.MatchString(strings.TrimSpace(line)) {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}

// attachDividerComments scans for freestanding BlockComment blocks that contain
// section divider patterns and prepends their text to the following declaration's
// Comments field. This ensures divider comments travel with the next declaration
// when --preserve-dividers is used.
func attachDividerComments(blocks []*Block) []*Block {
	var result []*Block
	var pending string

	for _, b := range blocks {
		if b.Kind == BlockComment && containsDivider(b.Comments) {
			// Accumulate divider comment text to prepend to next declaration
			if pending != "" {
				pending += "\n"
			}
			pending += strings.TrimSpace(b.Comments)
			continue
		}

		if pending != "" {
			// Prepend pending divider comments to this block's comments
			if b.Comments != "" {
				b.Comments = pending + "\n" + b.Comments
			} else {
				b.Comments = pending + "\n"
			}
			pending = ""
		}
		result = append(result, b)
	}

	// If there's a trailing divider with no following declaration, emit as comment block
	if pending != "" {
		result = append(result, &Block{
			Kind:     BlockComment,
			Comments: pending,
		})
	}

	return result
}

// containsDivider checks if a comment block contains any section divider lines.
func containsDivider(comments string) bool {
	for _, line := range strings.Split(comments, "\n") {
		if isSectionDivider(line) {
			return true
		}
	}
	return false
}

// sectionHeaderBanner is the repeated line used in section headers.
const sectionHeaderBanner = "// ============================================================================"

// sectionHeaderComment returns a 3-line section header comment block for the given label.
func sectionHeaderComment(label string) string {
	return sectionHeaderBanner + "\n// " + label + "\n" + sectionHeaderBanner + "\n"
}

// sectionHeaderRe matches the exact 3-line section header blocks that
// injectSectionHeaders produces. It only strips headers with known labels
// so that human-written decorative banners are never removed.
var sectionHeaderRe = regexp.MustCompile(
	`(?m)^` + regexp.QuoteMeta(sectionHeaderBanner) + `\n// (?:Services|Types for \w+|Shared Types|Core Types|Unreferenced Types|Composite Types(?: (?:\([^)]+\)|--[^\n]+))?|Helper Types(?: (?:\([^)]+\)|--[^\n]+))?|Standalone Types(?: (?:\([^)]+\)|--[^\n]+))?|Types unused by RPCs)\n` + regexp.QuoteMeta(sectionHeaderBanner) + `\n`)

// Note: Old section names (Services, Shared Types, Core Types, Unreferenced Types) are kept
// in the strip regex so that headers from older runs are cleaned up.
// The pattern matches optional descriptions in both parentheses or double-dash format.

// stripSectionHeaders removes injected section header blocks from a comment string.
func stripSectionHeaders(comments string) string {
	if comments == "" {
		return ""
	}
	return sectionHeaderRe.ReplaceAllString(comments, "")
}

// buildMessageToRPCMap builds a map from message name → RPC name using
// service blocks' RPCs. When a message is used by multiple RPCs, the first
// occurrence wins (matching the order-based placement logic).
func buildMessageToRPCMap(serviceBlocks []*Block) map[string]string {
	m := make(map[string]string)
	for _, svc := range serviceBlocks {
		for _, rpc := range svc.RPCs {
			for _, typeName := range []string{rpc.RequestType, rpc.ResponseType} {
				if _, exists := m[typeName]; !exists {
					m[typeName] = rpc.Name
				}
			}
		}
	}
	return m
}

// injectSectionHeaders walks the ordered block list and prepends section
// header comments when the section or RPC owner changes.
func injectSectionHeaders(ordered []*Block, serviceBlocks []*Block) {
	if len(ordered) == 0 {
		return
	}

	hasServices := len(serviceBlocks) > 0
	var msgToRPC map[string]string
	if hasServices {
		msgToRPC = buildMessageToRPCMap(serviceBlocks)
	}

	// Build map of ultimate root consumers for helpers
	// This helps us determine if a helper chain leads to an unreferenced type
	blockMap := make(map[string]*Block)
	for _, b := range ordered {
		blockMap[b.Name] = b
	}

	// Find the ultimate consumer (root of the helper chain)
	var findUltimateConsumer func(string) string
	findUltimateConsumer = func(name string) string {
		if b, ok := blockMap[name]; ok && b.Section == SectionHelper {
			return findUltimateConsumer(b.Consumer)
		}
		return name
	}

	emittedSections := make(map[Section]bool)
	emittedRPCs := make(map[string]bool)

	for _, b := range ordered {
		section := b.Section

		// Reclassify helpers based on their ultimate consumer
		if section == SectionHelper {
			if hasServices {
				// Service files: if primary consumer is RPC message, keep in RPC section
				if b.Consumer != "" {
					if _, isRPC := msgToRPC[b.Consumer]; isRPC {
						section = SectionRequestResponse
					}
					// Otherwise keep as SectionHelper for shared section
				}
			} else {
				// Non-service files: check if ultimate consumer is standalone
				ultimateConsumer := findUltimateConsumer(b.Name)
				if ultimateBlock, ok := blockMap[ultimateConsumer]; ok && ultimateBlock.Section == SectionUnreferenced {
					// Helper chain leads to standalone type
					section = SectionUnreferenced
				}
				// Otherwise keep as SectionHelper for its own section
			}
		}

		var header string

		switch section {
		case SectionService:
			// No header — "service Foo" is self-evident
		case SectionRequestResponse:
			rpcName := msgToRPC[b.Name]
			if rpcName == "" {
				rpcName = msgToRPC[b.Consumer]
			}
			if rpcName != "" && !emittedRPCs[rpcName] {
				header = sectionHeaderComment("Types for " + rpcName)
				emittedRPCs[rpcName] = true
			}
		case SectionCore:
			if !emittedSections[SectionCore] {
				header = sectionHeaderComment("Composite Types -- using other types")
			}
		case SectionHelper:
			if !emittedSections[SectionHelper] {
				header = sectionHeaderComment("Helper Types -- used in other types")
			}
		case SectionUnreferenced:
			if !emittedSections[SectionUnreferenced] {
				if hasServices {
					header = sectionHeaderComment("Types unused by RPCs")
				} else {
					header = sectionHeaderComment("Standalone Types -- not referenced elsewhere in this file")
				}
			}
		}
		emittedSections[section] = true

		if header != "" {
			// Trim leading blank lines from existing comments to avoid
			// double blank lines between the header and the comment.
			c := b.Comments
			for strings.HasPrefix(c, "\n") {
				c = c[1:]
			}
			b.Comments = header + c
		}
	}
}

// isProto2 checks if the file content declares proto2 syntax.
func isProto2(content string) bool {
	// Look for syntax = "proto2"
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "syntax") {
			return strings.Contains(trimmed, `"proto2"`)
		}
		// Skip comments and blank lines
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
			continue
		}
	}
	return false
}
