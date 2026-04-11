package protodef

type Document struct {
	Entries []Entry
}

type Entry struct {
	Key   string
	Value Value
	Line  int
}

type Value interface {
	valueNode()
}

type Scalar struct {
	Text string
}

func (*Scalar) valueNode() {}

type Flow struct {
	Value any
}

func (*Flow) valueNode() {}

type Block struct {
	Entries []Entry
}

func (*Block) valueNode() {}

type Array struct {
	ElemText  string
	CountText string
	Inline    *Block
}

func (*Array) valueNode() {}

type Mapper struct {
	Base  string
	Cases []MapperCase
}

func (*Mapper) valueNode() {}

type MapperCase struct {
	Key     string
	Value   string
	Ordinal int
}

type Switch struct {
	CompareTo string
	Cases     []Entry
}

func (*Switch) valueNode() {}
