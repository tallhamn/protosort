package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
)

// Verify checks that the sorted output is semantically identical to the original.
func Verify(original, sorted string, opts Options) error {
	// Content integrity check (always runs)
	if err := verifyContentIntegrity(original, sorted); err != nil {
		return fmt.Errorf("content integrity check failed: %w", err)
	}

	// Descriptor set comparison (requires protoc)
	if !opts.SkipVerify {
		if err := verifyDescriptorSets(original, sorted, opts); err != nil {
			return fmt.Errorf("descriptor verification failed: %w", err)
		}
	}

	return nil
}

// verifyContentIntegrity checks that the set of declarations (by name and body content)
// is identical before and after reordering.
func verifyContentIntegrity(original, sorted string) error {
	origBlocks, err := ScanFile(original)
	if err != nil {
		return fmt.Errorf("scanning original: %w", err)
	}
	sortedBlocks, err := ScanFile(sorted)
	if err != nil {
		return fmt.Errorf("scanning sorted output: %w", err)
	}

	origDecls := extractDeclarations(origBlocks)
	sortedDecls := extractDeclarations(sortedBlocks)

	// Check counts match
	if len(origDecls) != len(sortedDecls) {
		return fmt.Errorf("declaration count mismatch: original has %d, sorted has %d",
			len(origDecls), len(sortedDecls))
	}

	// Check each declaration by name
	for name, origBody := range origDecls {
		sortedBody, ok := sortedDecls[name]
		if !ok {
			return fmt.Errorf("declaration %q missing from sorted output", name)
		}
		if origBody != sortedBody {
			return fmt.Errorf("declaration %q body differs after sorting", name)
		}
	}

	for name := range sortedDecls {
		if _, ok := origDecls[name]; !ok {
			return fmt.Errorf("unexpected declaration %q in sorted output", name)
		}
	}

	return nil
}

// extractDeclarations returns a map from declaration key to body text.
// The key includes the kind to distinguish messages from enums with the same name.
func extractDeclarations(blocks []*Block) map[string]string {
	decls := make(map[string]string)
	for _, b := range blocks {
		switch b.Kind {
		case BlockMessage, BlockEnum, BlockService, BlockExtend:
			key := b.Kind.String() + ":" + b.Name
			body := extractBody(b.DeclText)
			decls[key] = body
		case BlockSyntax, BlockPackage, BlockOption, BlockImport:
			key := b.Kind.String() + ":" + b.Name
			decls[key] = b.DeclText
		}
	}
	return decls
}

// verifyDescriptorSets compiles both versions with protoc and compares descriptors.
func verifyDescriptorSets(original, sorted string, opts Options) error {
	protocPath := opts.ProtocPath
	if protocPath == "" {
		protocPath = "protoc"
	}

	// Check if protoc is available
	if _, err := exec.LookPath(protocPath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: protoc not found, skipping descriptor verification (use --skip-verify to silence)\n")
		return nil
	}

	tmpDir, err := os.MkdirTemp("", "protosort-verify-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Use the same filename for both so the descriptor's name field matches
	protoFile := filepath.Join(tmpDir, "file.proto")
	origDesc := filepath.Join(tmpDir, "original.pb")
	sortedDesc := filepath.Join(tmpDir, "sorted.pb")

	// Build protoc arguments
	baseArgs := []string{"--proto_path=" + tmpDir}
	for _, p := range opts.ProtoPaths {
		baseArgs = append(baseArgs, "--proto_path="+p)
	}

	// Compile original
	if err := os.WriteFile(protoFile, []byte(original), 0644); err != nil {
		return err
	}
	args1 := append(baseArgs[:len(baseArgs):len(baseArgs)], "--descriptor_set_out="+origDesc, protoFile)
	if out, err := exec.Command(protocPath, args1...).CombinedOutput(); err != nil {
		return fmt.Errorf("protoc failed on original: %s: %w", string(out), err)
	}

	// Compile sorted (overwrite same file so descriptor name matches)
	if err := os.WriteFile(protoFile, []byte(sorted), 0644); err != nil {
		return err
	}
	args2 := append(baseArgs[:len(baseArgs):len(baseArgs)], "--descriptor_set_out="+sortedDesc, protoFile)
	if out, err := exec.Command(protocPath, args2...).CombinedOutput(); err != nil {
		return fmt.Errorf("protoc failed on sorted output: %s: %w", string(out), err)
	}

	// Compare descriptor sets (ignoring source_code_info)
	origBytes, err := os.ReadFile(origDesc)
	if err != nil {
		return err
	}
	sortedBytes, err := os.ReadFile(sortedDesc)
	if err != nil {
		return err
	}

	origStripped, err := normalizeDescriptorSet(origBytes)
	if err != nil {
		return fmt.Errorf("parsing original descriptor set: %w", err)
	}
	sortedStripped, err := normalizeDescriptorSet(sortedBytes)
	if err != nil {
		return fmt.Errorf("parsing sorted descriptor set: %w", err)
	}

	if string(origStripped) != string(sortedStripped) {
		return fmt.Errorf("descriptor sets differ after sorting — the reordering changed the compiled schema")
	}

	return nil
}

// normalizeDescriptorSet parses a serialized FileDescriptorSet, clears
// source_code_info, sorts all descriptor lists by name for order-independent
// comparison, and re-serializes.
func normalizeDescriptorSet(data []byte) ([]byte, error) {
	fds := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(data, fds); err != nil {
		return nil, err
	}
	for _, fd := range fds.GetFile() {
		fd.SourceCodeInfo = nil
		normalizeFileDescriptor(fd)
	}
	return proto.Marshal(fds)
}

func normalizeFileDescriptor(fd *descriptorpb.FileDescriptorProto) {
	sort.Slice(fd.MessageType, func(i, j int) bool {
		return fd.MessageType[i].GetName() < fd.MessageType[j].GetName()
	})
	sort.Slice(fd.EnumType, func(i, j int) bool {
		return fd.EnumType[i].GetName() < fd.EnumType[j].GetName()
	})
	sort.Slice(fd.Service, func(i, j int) bool {
		return fd.Service[i].GetName() < fd.Service[j].GetName()
	})
	sort.Slice(fd.Extension, func(i, j int) bool {
		return fd.Extension[i].GetName() < fd.Extension[j].GetName()
	})
	// Recursively normalize nested messages
	for _, mt := range fd.MessageType {
		normalizeMessageDescriptor(mt)
	}
}

func normalizeMessageDescriptor(md *descriptorpb.DescriptorProto) {
	sort.Slice(md.NestedType, func(i, j int) bool {
		return md.NestedType[i].GetName() < md.NestedType[j].GetName()
	})
	sort.Slice(md.EnumType, func(i, j int) bool {
		return md.EnumType[i].GetName() < md.EnumType[j].GetName()
	})
	for _, nt := range md.NestedType {
		normalizeMessageDescriptor(nt)
	}
}

// DiffStrings produces a unified diff between two strings using an LCS-based
// diff algorithm with 3 lines of context and proper hunk headers.
func DiffStrings(a, b, nameA, nameB string) string {
	linesA := strings.Split(a, "\n")
	linesB := strings.Split(b, "\n")

	// Remove trailing empty string from split if input ends with newline
	if len(linesA) > 0 && linesA[len(linesA)-1] == "" {
		linesA = linesA[:len(linesA)-1]
	}
	if len(linesB) > 0 && linesB[len(linesB)-1] == "" {
		linesB = linesB[:len(linesB)-1]
	}

	edits := lcsDiff(linesA, linesB)

	// Check if there are any changes
	hasChanges := false
	for _, e := range edits {
		if e.op != editEqual {
			hasChanges = true
			break
		}
	}
	if !hasChanges {
		return ""
	}

	var diff strings.Builder
	diff.WriteString(fmt.Sprintf("--- %s\n", nameA))
	diff.WriteString(fmt.Sprintf("+++ %s\n", nameB))

	const ctx = 3
	hunks := buildHunks(edits, ctx)

	for _, h := range hunks {
		diff.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n",
			h.origStart+1, h.origCount,
			h.newStart+1, h.newCount))
		for _, line := range h.lines {
			diff.WriteString(line)
			diff.WriteByte('\n')
		}
	}

	return diff.String()
}

type editOp int

const (
	editEqual editOp = iota
	editDelete
	editInsert
)

type edit struct {
	op   editOp
	line string
	idxA int
	idxB int
}

// lcsDiff computes a diff edit script using the LCS (longest common subsequence) algorithm.
func lcsDiff(a, b []string) []edit {
	n := len(a)
	m := len(b)

	// Build LCS table
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack to find edit script
	var edits []edit
	i, j := n, m
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			i--
			j--
			edits = append(edits, edit{editEqual, a[i], i, j})
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			j--
			edits = append(edits, edit{editInsert, b[j], -1, j})
		} else {
			i--
			edits = append(edits, edit{editDelete, a[i], i, -1})
		}
	}

	// Reverse (built backwards)
	for l, r := 0, len(edits)-1; l < r; l, r = l+1, r-1 {
		edits[l], edits[r] = edits[r], edits[l]
	}

	return edits
}

type hunk struct {
	origStart int
	origCount int
	newStart  int
	newCount  int
	lines     []string
}

// buildHunks groups edits into unified diff hunks with context lines.
func buildHunks(edits []edit, ctx int) []hunk {
	// Find indices of non-equal edits
	type span struct{ start, end int }
	var changes []span
	i := 0
	for i < len(edits) {
		if edits[i].op != editEqual {
			start := i
			for i < len(edits) && edits[i].op != editEqual {
				i++
			}
			changes = append(changes, span{start, i})
		} else {
			i++
		}
	}
	if len(changes) == 0 {
		return nil
	}

	// Group nearby changes into hunk groups (merge if gap <= 2*ctx)
	type group struct{ spans []span }
	groups := []group{{spans: []span{changes[0]}}}
	for i := 1; i < len(changes); i++ {
		gap := changes[i].start - changes[i-1].end
		if gap <= 2*ctx {
			groups[len(groups)-1].spans = append(groups[len(groups)-1].spans, changes[i])
		} else {
			groups = append(groups, group{spans: []span{changes[i]}})
		}
	}

	var hunks []hunk
	for _, g := range groups {
		first := g.spans[0].start
		last := g.spans[len(g.spans)-1].end

		lo := first - ctx
		if lo < 0 {
			lo = 0
		}
		hi := last + ctx
		if hi > len(edits) {
			hi = len(edits)
		}

		var h hunk
		// Track line positions in A and B
		aPos := 0
		bPos := 0
		for _, e := range edits[:lo] {
			switch e.op {
			case editEqual:
				aPos++
				bPos++
			case editDelete:
				aPos++
			case editInsert:
				bPos++
			}
		}
		h.origStart = aPos
		h.newStart = bPos

		for idx := lo; idx < hi; idx++ {
			e := edits[idx]
			switch e.op {
			case editEqual:
				h.lines = append(h.lines, " "+e.line)
				h.origCount++
				h.newCount++
			case editDelete:
				h.lines = append(h.lines, "-"+e.line)
				h.origCount++
			case editInsert:
				h.lines = append(h.lines, "+"+e.line)
				h.newCount++
			}
		}
		hunks = append(hunks, h)
	}

	return hunks
}

// VerboseReport generates a report of type classification for --verbose mode.
func VerboseReport(blocks []*Block) string {
	// Ensure RPCs are populated on service blocks (callers may pass
	// freshly-scanned blocks that haven't been through Sort()).
	for _, b := range blocks {
		if b.Kind == BlockService && len(b.RPCs) == 0 {
			b.RPCs = ExtractRPCs(b)
		}
	}

	refCounts := BuildRefCounts(blocks)
	refGraph := BuildRefGraph(blocks)

	// Identify request/response types via classifyServiceAndRPC
	_, rpcMessages, _ := classifyServiceAndRPC(blocks)
	rpcMsgNames := make(map[string]bool)
	for _, b := range rpcMessages {
		rpcMsgNames[b.Name] = true
	}

	var report strings.Builder
	report.WriteString("Type classification:\n")

	var names []string
	for _, b := range blocks {
		if (b.Kind == BlockMessage || b.Kind == BlockEnum) && b.Name != "" {
			names = append(names, b.Name)
		}
	}
	sort.Strings(names)

	for _, name := range names {
		count := refCounts[name]
		refs := refGraph[name]
		sort.Strings(refs)

		var classification string
		switch {
		case rpcMsgNames[name]:
			classification = "request/response"
		case count >= 2:
			classification = "core"
		case count == 1:
			classification = fmt.Sprintf("helper (used by %s)", refs[0])
		default:
			classification = "unreferenced"
		}

		report.WriteString(fmt.Sprintf("  %-30s refs=%-3d %s", name, count, classification))
		if len(refs) > 0 {
			report.WriteString(fmt.Sprintf("  [%s]", strings.Join(refs, ", ")))
		}
		report.WriteByte('\n')
	}

	return report.String()
}
