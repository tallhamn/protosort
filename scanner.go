package main

import (
	"fmt"
	"strings"
)

// ScanFile parses a proto file into a sequence of Blocks, preserving raw text.
func ScanFile(content string) ([]*Block, error) {
	s := &scanner{content: content}
	return s.scan()
}

type scanner struct {
	content string
	pos     int
}

func (s *scanner) atEnd() bool {
	return s.pos >= len(s.content)
}

func (s *scanner) peek() byte {
	if s.atEnd() {
		return 0
	}
	return s.content[s.pos]
}

func (s *scanner) peekAt(offset int) byte {
	i := s.pos + offset
	if i >= len(s.content) {
		return 0
	}
	return s.content[i]
}

func (s *scanner) scan() ([]*Block, error) {
	var blocks []*Block

	for !s.atEnd() {
		// Collect leading whitespace and comments
		comments := s.collectComments()

		if s.atEnd() {
			// Trailing comments/whitespace
			if strings.TrimSpace(comments) != "" {
				blocks = append(blocks, &Block{
					Kind:     BlockComment,
					Comments: comments,
				})
			}
			break
		}

		// Read a declaration
		block, err := s.readDeclaration()
		if err != nil {
			return nil, err
		}

		block.Comments = comments
		blocks = append(blocks, block)
	}

	return blocks, nil
}

// collectComments collects whitespace and comments until we reach a declaration keyword.
func (s *scanner) collectComments() string {
	var buf strings.Builder

	for !s.atEnd() {
		c := s.peek()

		// Whitespace
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			buf.WriteByte(c)
			s.pos++
			continue
		}

		// Line comment
		if c == '/' && s.peekAt(1) == '/' {
			start := s.pos
			s.skipToEndOfLine()
			buf.WriteString(s.content[start:s.pos])
			continue
		}

		// Block comment
		if c == '/' && s.peekAt(1) == '*' {
			start := s.pos
			s.skipBlockComment()
			buf.WriteString(s.content[start:s.pos])
			continue
		}

		// Not whitespace or comment — must be a declaration keyword
		break
	}

	return buf.String()
}

func (s *scanner) skipToEndOfLine() {
	for !s.atEnd() && s.peek() != '\n' {
		s.pos++
	}
	if !s.atEnd() {
		s.pos++ // consume the newline
	}
}

func (s *scanner) skipBlockComment() {
	s.pos += 2 // skip /*
	for !s.atEnd() {
		if s.peek() == '*' && s.peekAt(1) == '/' {
			s.pos += 2
			return
		}
		s.pos++
	}
}

// readDeclaration reads a top-level declaration starting at the current position.
func (s *scanner) readDeclaration() (*Block, error) {
	keyword := s.matchKeyword()
	if keyword == "" {
		// Show context for debugging
		end := s.pos + 40
		if end > len(s.content) {
			end = len(s.content)
		}
		return nil, fmt.Errorf("expected declaration keyword at position %d: %q", s.pos, s.content[s.pos:end])
	}

	start := s.pos
	var kind BlockKind

	switch keyword {
	case "syntax":
		kind = BlockSyntax
		s.readUntilSemicolon()
	case "package":
		kind = BlockPackage
		s.readUntilSemicolon()
	case "import":
		kind = BlockImport
		s.readUntilSemicolon()
	case "option":
		kind = BlockOption
		s.readUntilSemicolonWithBraces()
	case "message":
		kind = BlockMessage
		s.readBracedBlock()
	case "enum":
		kind = BlockEnum
		s.readBracedBlock()
	case "service":
		kind = BlockService
		s.readBracedBlock()
	case "extend":
		kind = BlockExtend
		s.readBracedBlock()
	default:
		return nil, fmt.Errorf("unknown keyword %q at position %d", keyword, s.pos)
	}

	declText := s.content[start:s.pos]

	// Consume optional trailing inline comment on the closing line
	trailing := s.consumeTrailingComment()
	declText += trailing

	name := extractDeclName(keyword, declText)

	return &Block{
		Kind:     kind,
		Name:     name,
		DeclText: declText,
	}, nil
}

// matchKeyword checks if the current position starts with a known keyword
// followed by a non-identifier character.
func (s *scanner) matchKeyword() string {
	keywords := []string{"syntax", "package", "import", "option", "message", "enum", "service", "extend"}
	rest := s.content[s.pos:]
	for _, kw := range keywords {
		if strings.HasPrefix(rest, kw) && len(rest) > len(kw) && !isIdentChar(rest[len(kw)]) {
			return kw
		}
	}
	return ""
}

func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

// readUntilSemicolon reads until ';' consuming strings and comments.
func (s *scanner) readUntilSemicolon() {
	for !s.atEnd() {
		c := s.peek()
		if c == ';' {
			s.pos++
			return
		}
		s.skipOneToken()
	}
}

// readUntilSemicolonWithBraces reads until ';' at brace depth 0,
// handling option values that contain braces.
func (s *scanner) readUntilSemicolonWithBraces() {
	depth := 0
	for !s.atEnd() {
		c := s.peek()
		if c == '"' || c == '\'' {
			s.skipString(c)
			continue
		}
		if c == '/' && s.peekAt(1) == '/' {
			s.skipToEndOfLine()
			continue
		}
		if c == '/' && s.peekAt(1) == '*' {
			s.skipBlockComment()
			continue
		}
		if c == '{' {
			depth++
			s.pos++
			continue
		}
		if c == '}' {
			depth--
			s.pos++
			continue
		}
		if c == ';' && depth <= 0 {
			s.pos++
			return
		}
		s.pos++
	}
}

// readBracedBlock reads a braced declaration (message, enum, service, extend)
// until the matching closing brace.
func (s *scanner) readBracedBlock() {
	depth := 0
	for !s.atEnd() {
		c := s.peek()
		if c == '"' || c == '\'' {
			s.skipString(c)
			continue
		}
		if c == '/' && s.peekAt(1) == '/' {
			s.skipToEndOfLine()
			continue
		}
		if c == '/' && s.peekAt(1) == '*' {
			s.skipBlockComment()
			continue
		}
		if c == '{' {
			depth++
			s.pos++
			continue
		}
		if c == '}' {
			depth--
			if depth == 0 {
				s.pos++
				return
			}
			s.pos++
			continue
		}
		s.pos++
	}
}

func (s *scanner) skipString(quote byte) {
	s.pos++ // skip opening quote
	for !s.atEnd() {
		c := s.peek()
		if c == '\\' {
			s.pos += 2 // skip escape sequence
			continue
		}
		if c == quote {
			s.pos++
			return
		}
		s.pos++
	}
}

func (s *scanner) skipOneToken() {
	c := s.peek()
	if c == '"' || c == '\'' {
		s.skipString(c)
		return
	}
	if c == '/' && s.peekAt(1) == '/' {
		s.skipToEndOfLine()
		return
	}
	if c == '/' && s.peekAt(1) == '*' {
		s.skipBlockComment()
		return
	}
	s.pos++
}

// consumeTrailingComment consumes an optional inline comment on the same line
// as the closing ; or }. It does NOT consume the newline.
func (s *scanner) consumeTrailingComment() string {
	start := s.pos

	// Skip horizontal whitespace
	for !s.atEnd() && (s.peek() == ' ' || s.peek() == '\t') {
		s.pos++
	}

	// Check for inline comment
	if !s.atEnd() && s.peek() == '/' && s.peekAt(1) == '/' {
		s.skipToEndOfLine()
		return s.content[start:s.pos]
	}

	// No trailing comment — reset position
	s.pos = start
	return ""
}

// extractDeclName extracts the name from a declaration's text.
func extractDeclName(keyword, text string) string {
	// The name follows the keyword, possibly after whitespace
	rest := text[len(keyword):]
	rest = strings.TrimLeft(rest, " \t\r\n")

	// For import: the name is the quoted path
	if keyword == "import" {
		// Handle "public" or "weak" modifiers
		if strings.HasPrefix(rest, "public ") || strings.HasPrefix(rest, "weak ") {
			idx := strings.IndexByte(rest, ' ')
			rest = strings.TrimLeft(rest[idx:], " \t")
		}
		if len(rest) > 0 && (rest[0] == '"' || rest[0] == '\'') {
			end := strings.IndexByte(rest[1:], rest[0])
			if end >= 0 {
				return rest[1 : end+1]
			}
		}
		return rest
	}

	// For option: the name is everything up to '='
	if keyword == "option" {
		eqIdx := strings.IndexByte(rest, '=')
		if eqIdx >= 0 {
			return strings.TrimSpace(rest[:eqIdx])
		}
		return rest
	}

	// For syntax: the value after '='
	if keyword == "syntax" {
		eqIdx := strings.IndexByte(rest, '=')
		if eqIdx >= 0 {
			val := strings.TrimSpace(rest[eqIdx+1:])
			val = strings.TrimRight(val, ";")
			val = strings.Trim(val, "\"' ")
			return val
		}
		return rest
	}

	// For package: everything up to ';'
	if keyword == "package" {
		semiIdx := strings.IndexByte(rest, ';')
		if semiIdx >= 0 {
			return strings.TrimSpace(rest[:semiIdx])
		}
		return strings.TrimSpace(rest)
	}

	// For message, enum, service, extend: first identifier
	var name strings.Builder
	for _, c := range rest {
		if isIdentChar(byte(c)) || c == '.' {
			name.WriteRune(c)
		} else {
			break
		}
	}
	return name.String()
}
