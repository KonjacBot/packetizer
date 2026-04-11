package schemair

type File struct {
	Definitions []*Definition
}

type Definition struct {
	Name      string
	Namespace string
	Expr      Expr
}

type Expr interface {
	exprNode()
}

type Ref struct {
	Name string
}

func (*Ref) exprNode() {}

type Native struct {
	Name string
}

func (*Native) exprNode() {}

type Void struct{}

func (*Void) exprNode() {}

type Container struct {
	Fields []Field
}

func (*Container) exprNode() {}

type Field struct {
	Name      string
	Optional  bool
	Anonymous bool
	Type      Expr
}

type Array struct {
	Elem  Expr
	Count Count
}

func (*Array) exprNode() {}

type Count struct {
	Type  string
	Field string
	Fixed *int
}

type Option struct {
	Inner Expr
}

func (*Option) exprNode() {}

type Mapper struct {
	Base    Expr
	Entries []MapperEntry
}

func (*Mapper) exprNode() {}

type MapperEntry struct {
	Key   string
	Value string
}

type Switch struct {
	CompareTo    string
	CompareValue any
	Cases        []SwitchCase
	Default      Expr
}

func (*Switch) exprNode() {}

type SwitchCase struct {
	Labels []string
	Expr   Expr
}

type Call struct {
	Name    string
	Options map[string]any
}

func (*Call) exprNode() {}

type RegistryHolder struct {
	BaseName      string
	OtherwiseName string
	OtherwiseType Expr
}

func (*RegistryHolder) exprNode() {}

type RegistryHolderSet struct {
	BaseName      string
	BaseType      Expr
	OtherwiseName string
	OtherwiseType Expr
}

func (*RegistryHolderSet) exprNode() {}

type EntityMetadataLoop struct {
	Elem   Expr
	EndVal int
}

func (*EntityMetadataLoop) exprNode() {}

type TopBitSetTerminatedArray struct {
	Elem Expr
}

func (*TopBitSetTerminatedArray) exprNode() {}

type Bitfield struct {
	Fields []BitfieldField
}

func (*Bitfield) exprNode() {}

type Bitflags struct {
	Base  string
	Flags []BitflagFlag
}

func (*Bitflags) exprNode() {}

type BitflagFlag struct {
	Name string
	Mask uint64
}

type BitfieldField struct {
	Name   string
	Size   int
	Signed bool
}
