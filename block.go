package main

// BlockKind represents the type of a top-level proto element.
type BlockKind int

const (
	BlockSyntax BlockKind = iota
	BlockPackage
	BlockOption
	BlockImport
	BlockMessage
	BlockEnum
	BlockService
	BlockExtend
	BlockComment // freestanding comment not attached to a declaration
)

func (k BlockKind) String() string {
	switch k {
	case BlockSyntax:
		return "syntax"
	case BlockPackage:
		return "package"
	case BlockOption:
		return "option"
	case BlockImport:
		return "import"
	case BlockMessage:
		return "message"
	case BlockEnum:
		return "enum"
	case BlockService:
		return "service"
	case BlockExtend:
		return "extend"
	case BlockComment:
		return "comment"
	default:
		return "unknown"
	}
}

// Section identifies which output section a block belongs to.
type Section int

const (
	SectionHeader          Section = iota // syntax, package, options, imports
	SectionService                        // service declarations
	SectionRequestResponse                // RPC request/response messages
	SectionCore                           // types referenced by 2+ declarations
	SectionHelper                         // types referenced by exactly 1 declaration
	SectionUnreferenced                   // types referenced by 0 declarations
)

// Block represents a top-level element in a proto file with its raw text.
type Block struct {
	Kind     BlockKind
	Name     string // name of the declaration (for message, enum, service, extend)
	Comments string // leading/detached comments (may include blank lines)
	DeclText string // the declaration text (from keyword to closing ; or })
	Section  Section
	// Extracted from service blocks
	RPCs []RPC
	// For sorting helpers: the single consumer of this type (if Section == SectionHelper)
	Consumer string
}

// RPC represents an RPC method in a service.
type RPC struct {
	Name         string
	RequestType  string
	ResponseType string
}

// Options holds the configuration for sorting.
type Options struct {
	Write            bool
	Check            bool
	Diff             bool
	Verify           bool
	ProtocPath       string
	ProtoPaths       []string
	SharedOrder      string // "alpha" or "dependency"
	SortRPCs         string // "" (disabled), "alpha", or "grouped"
	PreserveDividers bool
	StripCommented   bool
	DryRun           bool
	Verbose          bool
	Quiet            bool
	Recursive        bool
	Annotate         bool
	SectionHeaders   bool
	ConfigFile       string
}
