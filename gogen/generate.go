package gogen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/iancoleman/strcase"

	"github.com/KonjacBot/packetizer/schemair"
)

const wireImportPath = "github.com/KonjacBot/packetizer/wire"

type Options struct {
	PackageName    string
	Types          []string
	LabelOverrides map[string]string
}

type EmitOptions struct {
	PackageName    string
	Definitions    []*schemair.Definition
	ExternalRefs   map[string]ExternalRef
	LabelOverrides map[string]string
}

type Prepared struct {
	File        *schemair.File
	Definitions []*schemair.Definition
	Types       map[string]*schemair.Definition
	Natives     map[string]NativeMapping
	Original    map[string]struct{}
}

type ExternalRef struct {
	ImportPath string
	Alias      string
}

type importSpec struct {
	Path  string
	Alias string
}

type NativeMapping struct {
	GoType   string
	SizeFn   string
	AppendFn string
	DecodeFn string
	Imports  []string
}

type Generator struct {
	file           *schemair.File
	types          map[string]*schemair.Definition
	selected       map[string]struct{}
	local          map[string]struct{}
	natives        map[string]NativeMapping
	imports        []importSpec
	synthetic      []*schemair.Definition
	externalRefs   map[string]ExternalRef
	labelOverrides map[string]string
	original       map[string]struct{}
}

func Generate(file *schemair.File, opts Options) ([]byte, error) {
	prepared, err := Prepare(file, opts)
	if err != nil {
		return nil, err
	}
	return Emit(prepared, EmitOptions{
		PackageName:    opts.PackageName,
		Definitions:    prepared.Definitions,
		LabelOverrides: opts.LabelOverrides,
	})
}

func Prepare(file *schemair.File, opts Options) (*Prepared, error) {
	gen := &Generator{
		file:           file,
		types:          make(map[string]*schemair.Definition, len(file.Definitions)),
		selected:       make(map[string]struct{}),
		natives:        defaultNatives(),
		imports:        nil,
		labelOverrides: opts.LabelOverrides,
	}
	for _, def := range file.Definitions {
		gen.types[def.Name] = def
	}
	if len(opts.Types) == 0 {
		for _, def := range file.Definitions {
			gen.selected[def.Name] = struct{}{}
		}
	} else {
		for _, name := range opts.Types {
			gen.selected[name] = struct{}{}
		}
	}
	defs, err := gen.collectDefinitions()
	if err != nil {
		return nil, err
	}
	types := make(map[string]*schemair.Definition, len(gen.types)+len(gen.synthetic))
	for name, def := range gen.types {
		types[name] = def
	}
	for _, def := range gen.synthetic {
		types[def.Name] = def
	}
	original := make(map[string]struct{}, len(file.Definitions))
	for _, def := range file.Definitions {
		original[def.Name] = struct{}{}
	}
	return &Prepared{
		File:        file,
		Definitions: defs,
		Types:       types,
		Natives:     defaultNatives(),
		Original:    original,
	}, nil
}

func Emit(prepared *Prepared, opts EmitOptions) ([]byte, error) {
	local := make(map[string]struct{}, len(opts.Definitions))
	for _, def := range opts.Definitions {
		local[def.Name] = struct{}{}
	}
	gen := &Generator{
		file:           prepared.File,
		types:          prepared.Types,
		local:          local,
		natives:        prepared.Natives,
		imports:        []importSpec{{Path: wireImportPath}},
		externalRefs:   opts.ExternalRefs,
		labelOverrides: opts.LabelOverrides,
		original:       prepared.Original,
	}
	for _, ref := range opts.ExternalRefs {
		if ref.ImportPath == "" {
			continue
		}
		alias := ref.Alias
		if alias == "" {
			alias = defaultImportAlias(ref.ImportPath)
		}
		gen.addImportSpec(importSpec{Path: ref.ImportPath, Alias: alias})
	}

	var out bytes.Buffer
	if err := executeTemplate(&out, "fileHeader", templateData{"PackageName": opts.PackageName}); err != nil {
		return nil, err
	}
	if err := gen.collectImports(opts.Definitions); err != nil {
		return nil, err
	}
	if err := gen.emitImports(&out); err != nil {
		return nil, err
	}
	for _, def := range opts.Definitions {
		if err := gen.emitDefinition(&out, def); err != nil {
			return nil, err
		}
	}
	formatted, err := format.Source(out.Bytes())
	if err != nil {
		return out.Bytes(), err
	}
	return pruneUnusedImports(formatted)
}

func defaultNatives() map[string]NativeMapping {
	return map[string]NativeMapping{
		"Bool":            {GoType: "bool", SizeFn: "wire.SizeBool", AppendFn: "wire.AppendBool", DecodeFn: "wire.DecodeBool"},
		"VarInt":          {GoType: "int32", SizeFn: "wire.SizeVarInt", AppendFn: "wire.AppendVarInt", DecodeFn: "wire.DecodeVarInt"},
		"VarInt64":        {GoType: "int64", SizeFn: "wire.SizeVarInt64", AppendFn: "wire.AppendVarInt64", DecodeFn: "wire.DecodeVarInt64"},
		"VarInt128":       {GoType: "*wire.BigInt", SizeFn: "wire.SizeVarInt128", AppendFn: "wire.AppendVarInt128", DecodeFn: "wire.DecodeVarInt128"},
		"VarLong":         {GoType: "int64", SizeFn: "wire.SizeVarLong", AppendFn: "wire.AppendVarLong", DecodeFn: "wire.DecodeVarLong"},
		"Zigzag32":        {GoType: "int32", SizeFn: "wire.SizeZigzag32", AppendFn: "wire.AppendZigzag32", DecodeFn: "wire.DecodeZigzag32"},
		"Zigzag64":        {GoType: "int64", SizeFn: "wire.SizeZigzag64", AppendFn: "wire.AppendZigzag64", DecodeFn: "wire.DecodeZigzag64"},
		"I8":              {GoType: "int8", SizeFn: "wire.SizeInt8", AppendFn: "wire.AppendInt8", DecodeFn: "wire.DecodeInt8"},
		"U8":              {GoType: "uint8", SizeFn: "wire.SizeUint8", AppendFn: "wire.AppendUint8", DecodeFn: "wire.DecodeUint8"},
		"I16":             {GoType: "int16", SizeFn: "wire.SizeInt16", AppendFn: "wire.AppendInt16", DecodeFn: "wire.DecodeInt16"},
		"U16":             {GoType: "uint16", SizeFn: "wire.SizeUint16", AppendFn: "wire.AppendUint16", DecodeFn: "wire.DecodeUint16"},
		"Li16":            {GoType: "int16", SizeFn: "wire.SizeInt16", AppendFn: "wire.AppendInt16LE", DecodeFn: "wire.DecodeInt16LE"},
		"Lu16":            {GoType: "uint16", SizeFn: "wire.SizeUint16", AppendFn: "wire.AppendUint16LE", DecodeFn: "wire.DecodeUint16LE"},
		"I32":             {GoType: "int32", SizeFn: "wire.SizeInt32", AppendFn: "wire.AppendInt32", DecodeFn: "wire.DecodeInt32"},
		"U32":             {GoType: "uint32", SizeFn: "wire.SizeUint32", AppendFn: "wire.AppendUint32", DecodeFn: "wire.DecodeUint32"},
		"Li32":            {GoType: "int32", SizeFn: "wire.SizeInt32", AppendFn: "wire.AppendInt32LE", DecodeFn: "wire.DecodeInt32LE"},
		"Lu32":            {GoType: "uint32", SizeFn: "wire.SizeUint32", AppendFn: "wire.AppendUint32LE", DecodeFn: "wire.DecodeUint32LE"},
		"I64":             {GoType: "int64", SizeFn: "wire.SizeInt64", AppendFn: "wire.AppendInt64", DecodeFn: "wire.DecodeInt64"},
		"U64":             {GoType: "uint64", SizeFn: "wire.SizeUint64", AppendFn: "wire.AppendUint64", DecodeFn: "wire.DecodeUint64"},
		"Li64":            {GoType: "int64", SizeFn: "wire.SizeInt64", AppendFn: "wire.AppendInt64LE", DecodeFn: "wire.DecodeInt64LE"},
		"Lu64":            {GoType: "uint64", SizeFn: "wire.SizeUint64", AppendFn: "wire.AppendUint64LE", DecodeFn: "wire.DecodeUint64LE"},
		"F32":             {GoType: "float32", SizeFn: "wire.SizeFloat32", AppendFn: "wire.AppendFloat32", DecodeFn: "wire.DecodeFloat32"},
		"F64":             {GoType: "float64", SizeFn: "wire.SizeFloat64", AppendFn: "wire.AppendFloat64", DecodeFn: "wire.DecodeFloat64"},
		"Lf32":            {GoType: "float32", SizeFn: "wire.SizeFloat32", AppendFn: "wire.AppendFloat32LE", DecodeFn: "wire.DecodeFloat32LE"},
		"Lf64":            {GoType: "float64", SizeFn: "wire.SizeFloat64", AppendFn: "wire.AppendFloat64LE", DecodeFn: "wire.DecodeFloat64LE"},
		"UUID":            {GoType: "uuid.UUID", SizeFn: "wire.SizeUUID", AppendFn: "wire.AppendUUID", DecodeFn: "wire.DecodeUUID", Imports: []string{"github.com/google/uuid"}},
		"Cstring":         {GoType: "string", SizeFn: "wire.SizeCStringUTF8", AppendFn: "wire.AppendCStringUTF8", DecodeFn: "wire.DecodeCStringUTF8"},
		"String":          {GoType: "string", SizeFn: "wire.SizeString", AppendFn: "wire.AppendString", DecodeFn: "wire.DecodeString"},
		"ByteArray":       {GoType: "[]byte", SizeFn: "wire.SizeByteArray", AppendFn: "wire.AppendByteArray", DecodeFn: "wire.DecodeByteArray"},
		"RestBuffer":      {GoType: "[]byte", SizeFn: "wire.SizeRestBuffer", AppendFn: "wire.AppendRestBuffer", DecodeFn: "wire.DecodeRestBuffer"},
		"LpVec3":          {GoType: "wire.LPVec3", SizeFn: "wire.SizeLPVec3", AppendFn: "wire.AppendLPVec3", DecodeFn: "wire.DecodeLPVec3"},
		"AnonymousNbt":    {GoType: "nbt.RawMessage", SizeFn: "wire.SizeNBT", AppendFn: "wire.AppendNBT", DecodeFn: "wire.DecodeNBT", Imports: []string{"github.com/KonjacBot/go-mc/nbt"}},
		"AnonOptionalNbt": {GoType: "*nbt.RawMessage", SizeFn: "wire.SizeOptionalNBT", AppendFn: "wire.AppendOptionalNBT", DecodeFn: "wire.DecodeOptionalNBT", Imports: []string{"github.com/KonjacBot/go-mc/nbt"}},
		"ChatComponent":   {GoType: "chat.Message", SizeFn: "wire.SizeNBT", AppendFn: "wire.AppendNBT", DecodeFn: "wire.DecodeNBT", Imports: []string{"github.com/KonjacBot/go-mc/chat"}},
	}
}

func (g *Generator) collectDefinitions() ([]*schemair.Definition, error) {
	var defs []*schemair.Definition
	seen := make(map[string]struct{})
	for _, def := range g.file.Definitions {
		if _, ok := g.selected[def.Name]; ok {
			if g.skipDefinition(def) {
				continue
			}
			defs = append(defs, def)
			seen[def.Name] = struct{}{}
		}
	}
	for {
		beforeDefs := len(defs)
		beforeSynthetic := len(g.synthetic)
		for _, def := range defs {
			if err := g.collectExpr(def.Expr, def.Name, &defs, seen); err != nil {
				return nil, err
			}
		}
		for _, synthetic := range g.synthetic[beforeSynthetic:] {
			if _, ok := seen[synthetic.Name]; !ok {
				defs = append(defs, synthetic)
				seen[synthetic.Name] = struct{}{}
			}
		}
		if len(defs) == beforeDefs && len(g.synthetic) == beforeSynthetic {
			break
		}
	}
	return defs, nil
}

func (g *Generator) collectExpr(expr schemair.Expr, ctx string, defs *[]*schemair.Definition, seen map[string]struct{}) error {
	switch v := expr.(type) {
	case *schemair.Option:
		return g.collectExpr(v.Inner, ctx+"Value", defs, seen)
	case *schemair.Array:
		return g.collectExpr(v.Elem, ctx+"Item", defs, seen)
	case *schemair.Ref:
		if _, ok := g.natives[v.Name]; ok {
			return nil
		}
		if def, ok := g.types[v.Name]; ok {
			if _, done := seen[def.Name]; !done {
				if g.skipDefinition(def) {
					return nil
				}
				*defs = append(*defs, def)
				seen[def.Name] = struct{}{}
			}
		}
		return nil
	case *schemair.Container:
		if _, ok := g.types[ctx]; !ok {
			g.ensureSynthetic(ctx, v)
		}
		for _, field := range v.Fields {
			if err := g.collectExpr(field.Type, ctx+field.Name, defs, seen); err != nil {
				return err
			}
		}
		return nil
	case *schemair.Bitfield:
		if _, ok := g.types[ctx]; !ok {
			g.ensureSynthetic(ctx, v)
		}
		return nil
	case *schemair.Bitflags:
		if _, ok := g.types[ctx]; !ok {
			g.ensureSynthetic(ctx, v)
		}
		return nil
	case *schemair.Mapper:
		if _, ok := g.types[ctx]; !ok {
			g.ensureSynthetic(ctx, v)
		}
		return g.collectExpr(v.Base, ctx+"Base", defs, seen)
	case *schemair.RegistryHolder:
		if _, ok := g.types[ctx]; !ok {
			g.ensureSynthetic(ctx, v)
		}
		return g.collectExpr(v.OtherwiseType, ctx+v.OtherwiseName, defs, seen)
	case *schemair.RegistryHolderSet:
		if _, ok := g.types[ctx]; !ok {
			g.ensureSynthetic(ctx, v)
		}
		if err := g.collectExpr(v.BaseType, ctx+v.BaseName, defs, seen); err != nil {
			return err
		}
		return g.collectExpr(v.OtherwiseType, ctx+v.OtherwiseName+"Item", defs, seen)
	case *schemair.EntityMetadataLoop:
		if _, ok := g.types[ctx]; !ok {
			g.ensureSynthetic(ctx, v)
		}
		return g.collectExpr(v.Elem, ctx+"Item", defs, seen)
	case *schemair.TopBitSetTerminatedArray:
		if _, ok := g.types[ctx]; !ok {
			g.ensureSynthetic(ctx, v)
		}
		return g.collectExpr(v.Elem, ctx+"Item", defs, seen)
	case *schemair.Switch:
		for i, kase := range v.Cases {
			if err := g.collectExpr(kase.Expr, switchCaseName(ctx, kase, i), defs, seen); err != nil {
				return err
			}
		}
		return g.collectExpr(v.Default, switchDefaultName(ctx), defs, seen)
	case *schemair.Call, *schemair.Native, *schemair.Void:
		return nil
	default:
		return fmt.Errorf("unsupported expr %T", expr)
	}
}

func (g *Generator) skipDefinition(def *schemair.Definition) bool {
	native, ok := def.Expr.(*schemair.Native)
	if ok {
		mapping, supported := g.natives[native.Name]
		if !supported {
			return true
		}
		return strings.HasPrefix(mapping.GoType, "*")
	}
	call, ok := def.Expr.(*schemair.Call)
	if !ok {
		if _, ok := def.Expr.(*schemair.Switch); ok {
			return true
		}
		return false
	}
	goType, err := g.goType(def.Name, call, def.Name)
	if err != nil {
		return false
	}
	return strings.HasPrefix(goType, "*") || goType == "any"
}

func (g *Generator) ensureSynthetic(name string, expr schemair.Expr) {
	if _, ok := g.types[name]; ok {
		return
	}
	for _, synthetic := range g.synthetic {
		if synthetic.Name == name {
			return
		}
	}
	g.synthetic = append(g.synthetic, &schemair.Definition{Name: name, Expr: expr})
}

func (g *Generator) emitImports(out *bytes.Buffer) error {
	imports := append([]importSpec(nil), g.imports...)
	slices.SortFunc(imports, func(a, b importSpec) int {
		if aStd, bStd := isStandardImport(a.Path), isStandardImport(b.Path); aStd != bStd {
			if aStd {
				return -1
			}
			return 1
		}
		if cmp := strings.Compare(a.Path, b.Path); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Alias, b.Alias)
	})
	imports = slices.CompactFunc(imports, func(a, b importSpec) bool {
		return a.Path == b.Path && a.Alias == b.Alias
	})
	return executeTemplate(out, "imports", templateData{"Imports": imports})
}

func isStandardImport(importPath string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}

func (g *Generator) emitDefinition(out *bytes.Buffer, def *schemair.Definition) error {
	switch expr := def.Expr.(type) {
	case *schemair.Mapper:
		return g.emitEnum(out, def.Name, expr)
	case *schemair.Container:
		return g.emitStruct(out, def.Name, expr)
	case *schemair.Array:
		return g.emitArray(out, def.Name, expr)
	case *schemair.Bitflags:
		return g.emitBitflags(out, def.Name, expr)
	case *schemair.Bitfield:
		return g.emitBitfield(out, def.Name, expr)
	case *schemair.RegistryHolder:
		return g.emitRegistryHolder(out, def.Name, expr)
	case *schemair.RegistryHolderSet:
		return g.emitRegistryHolderSet(out, def.Name, expr)
	case *schemair.EntityMetadataLoop:
		return g.emitEntityMetadataLoop(out, def.Name, expr)
	case *schemair.TopBitSetTerminatedArray:
		return g.emitTopBitSetTerminatedArray(out, def.Name, expr)
	default:
		return g.emitAlias(out, def.Name, expr)
	}
}

func (g *Generator) emitEnum(out *bytes.Buffer, name string, expr *schemair.Mapper) error {
	baseType, err := g.goType(name, expr.Base, name)
	if err != nil {
		return err
	}
	baseName, err := g.nativeName(expr.Base)
	if err != nil {
		return err
	}
	mapping := g.natives[baseName]
	entries := make([]templateData, 0, len(expr.Entries))
	for _, entry := range expr.Entries {
		entries = append(entries, templateData{
			"Name": sanitizeValueName(g.canonicalLabel(entry.Value)),
			"Key":  entry.Key,
		})
	}
	return executeTemplate(out, "enum", templateData{
		"Name":     name,
		"BaseType": baseType,
		"Entries":  entries,
		"SizeFn":   mapping.SizeFn,
		"AppendFn": mapping.AppendFn,
		"DecodeFn": mapping.DecodeFn,
	})
}

func (g *Generator) anonymousContainer(expr schemair.Expr) *schemair.Container {
	switch v := expr.(type) {
	case *schemair.Container:
		return v
	case *schemair.Ref:
		if def, ok := g.types[v.Name]; ok {
			return g.anonymousContainer(def.Expr)
		}
	case *schemair.Option:
		return g.anonymousContainer(v.Inner)
	}
	return nil
}

func (g *Generator) emitStructFields(out *bytes.Buffer, name string, fields []schemair.Field) error {
	for _, field := range fields {
		if field.Anonymous {
			if inner := g.anonymousContainer(field.Type); inner != nil {
				if err := g.emitStructFields(out, name, inner.Fields); err != nil {
					return err
				}
				continue
			}
		}
		fieldType, err := g.goType(name+field.Name, field.Type, name+field.Name)
		if err != nil {
			return err
		}
		if err := writeRawLine(out, "\t", field.Name+" "+fieldType); err != nil {
			return err
		}
	}
	return nil
}

func (g *Generator) emitStruct(out *bytes.Buffer, name string, expr *schemair.Container) error {
	var fields bytes.Buffer
	if err := g.emitStructFields(&fields, name, expr.Fields); err != nil {
		return err
	}
	if err := executeTemplate(out, "struct", templateData{"Name": name, "Fields": fields.String()}); err != nil {
		return err
	}

	if !g.isOriginal(name) {
		return nil
	}

	if err := g.emitSize(out, name, expr); err != nil {
		return err
	}
	if err := g.emitAppend(out, name, expr); err != nil {
		return err
	}
	return g.emitDecode(out, name, expr)
}

func (g *Generator) emitArray(out *bytes.Buffer, name string, expr *schemair.Array) error {
	elemType, err := g.goType(name, expr.Elem, name+"Item")
	if err != nil {
		return err
	}
	if err := executeTemplate(out, "arrayType", templateData{"Name": name, "ElemType": elemType}); err != nil {
		return err
	}

	var body bytes.Buffer
	if err := g.writeSizeExpr(&body, nil, nil, expr, "c", "\t", name, "c", "", nil, "c"); err != nil {
		return err
	}
	if err := executeTemplate(out, "sizeFunc", templateData{"Name": name, "Body": body.String()}); err != nil {
		return err
	}

	body.Reset()
	if err := g.writeAppendExpr(&body, nil, nil, expr, "c", "\t", name, "c", "", nil, "c"); err != nil {
		return err
	}
	if err := executeTemplate(out, "appendFunc", templateData{"Name": name, "Body": body.String()}); err != nil {
		return err
	}

	body.Reset()
	if err := g.writeDecodeExpr(&body, nil, nil, expr, "*c", "\t", name, "c", "", nil, "c"); err != nil {
		return err
	}
	return executeTemplate(out, "decodeFunc", templateData{"Name": name, "Body": body.String()})
}

func (g *Generator) emitAlias(out *bytes.Buffer, name string, expr schemair.Expr) error {
	goType, err := g.goType(name, expr, name)
	if err != nil {
		return err
	}
	if err := executeTemplate(out, "aliasType", templateData{"Name": name, "GoType": goType}); err != nil {
		return err
	}

	var body bytes.Buffer
	if err := g.writeSizeExpr(&body, nil, nil, expr, aliasValueExpr(goType, "c"), "\t", name, "c", "", nil, "c"); err != nil {
		return err
	}
	if err := executeTemplate(out, "sizeFunc", templateData{"Name": name, "Body": body.String()}); err != nil {
		return err
	}

	body.Reset()
	if err := g.writeAppendExpr(&body, nil, nil, expr, aliasValueExpr(goType, "c"), "\t", name, "c", "", nil, "c"); err != nil {
		return err
	}
	if err := executeTemplate(out, "appendFunc", templateData{"Name": name, "Body": body.String()}); err != nil {
		return err
	}

	body.Reset()
	if err := writeRawLine(&body, "\t", "var value "+goType); err != nil {
		return err
	}
	if err := g.writeDecodeExpr(&body, nil, nil, expr, "value", "\t", name, "c", "", nil, "c"); err != nil {
		return err
	}
	if err := writeRawLine(&body, "\t", "*c = "+name+"(value)"); err != nil {
		return err
	}
	return executeTemplate(out, "decodeFunc", templateData{"Name": name, "Body": body.String()})
}

func (g *Generator) emitBitfield(out *bytes.Buffer, name string, expr *schemair.Bitfield) error {
	if err := writeRawLine(out, "", "type "+name+" struct {"); err != nil {
		return err
	}
	for _, field := range expr.Fields {
		if err := writeRawLine(out, "\t", field.Name+" "+bitfieldGoType(field)); err != nil {
			return err
		}
	}
	if err := writeRawLine(out, "", "}"); err != nil {
		return err
	}
	if err := writeRawLine(out, "", ""); err != nil {
		return err
	}

	totalBytes := bitfieldByteSize(expr)
	if err := writeRawLine(out, "", "func ("+name+") Size() (int, error) { return "+strconv.Itoa(totalBytes)+", nil }"); err != nil {
		return err
	}
	if err := writeRawLine(out, "", ""); err != nil {
		return err
	}
	if err := writeRawLine(out, "", "func (c "+name+") Append(dst []byte) ([]byte, error) {"); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t", "var raw uint64"); err != nil {
		return err
	}
	remaining := bitfieldTotalBits(expr)
	for _, field := range expr.Fields {
		remaining -= field.Size
		mask := strconv.FormatUint(bitfieldMask(field.Size), 16)
		if err := writeRawLine(out, "\t", "raw |= (uint64("+bitfieldCastType(field)+"(c."+field.Name+")) & 0x"+mask+") << "+strconv.Itoa(remaining)); err != nil {
			return err
		}
	}
	if err := writeRawLine(out, "\t", "for i := "+strconv.Itoa(totalBytes-1)+"; i >= 0; i-- {"); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t\t", "dst = append(dst, byte(raw>>(i*8)))"); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t", "}"); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t", "return dst, nil"); err != nil {
		return err
	}
	if err := writeRawLine(out, "", "}"); err != nil {
		return err
	}
	if err := writeRawLine(out, "", ""); err != nil {
		return err
	}
	if err := writeRawLine(out, "", "func (c *"+name+") Decode(src []byte) ([]byte, error) {"); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t", "if len(src) < "+strconv.Itoa(totalBytes)+" {"); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t\t", "return nil, io.ErrUnexpectedEOF"); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t", "}"); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t", "var raw uint64"); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t", "for i := 0; i < "+strconv.Itoa(totalBytes)+"; i++ {"); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t\t", "raw = (raw << 8) | uint64(src[i])"); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t", "}"); err != nil {
		return err
	}
	remaining = bitfieldTotalBits(expr)
	for _, field := range expr.Fields {
		remaining -= field.Size
		mask := strconv.FormatUint(bitfieldMask(field.Size), 16)
		if err := writeRawLine(out, "\t", "{"); err != nil {
			return err
		}
		if err := writeRawLine(out, "\t\t", "value := (raw >> "+strconv.Itoa(remaining)+") & 0x"+mask); err != nil {
			return err
		}
		if field.Signed {
			if err := writeRawLine(out, "\t\t", "if value&(uint64(1)<<"+strconv.Itoa(field.Size-1)+") != 0 {"); err != nil {
				return err
			}
			if err := writeRawLine(out, "\t\t\t", "value |= ^uint64(0x"+mask+")"); err != nil {
				return err
			}
			if err := writeRawLine(out, "\t\t", "}"); err != nil {
				return err
			}
			if err := writeRawLine(out, "\t\t", "c."+field.Name+" = "+bitfieldGoType(field)+"(int64(value))"); err != nil {
				return err
			}
		} else {
			if err := writeRawLine(out, "\t\t", "c."+field.Name+" = "+bitfieldGoType(field)+"(value)"); err != nil {
				return err
			}
		}
		if err := writeRawLine(out, "\t", "}"); err != nil {
			return err
		}
	}
	if err := writeRawLine(out, "\t", "return src["+strconv.Itoa(totalBytes)+":], nil"); err != nil {
		return err
	}
	if err := writeRawLine(out, "", "}"); err != nil {
		return err
	}
	if err := writeRawLine(out, "", ""); err != nil {
		return err
	}
	g.addImports("io")
	return nil
}

func (g *Generator) emitBitflags(out *bytes.Buffer, name string, expr *schemair.Bitflags) error {
	mapping, ok := g.natives[expr.Base]
	if !ok {
		return fmt.Errorf("unsupported bitflags base %s", expr.Base)
	}
	g.addMappingImports(mapping)

	if err := writeRawLine(out, "", "type "+name+" struct {"); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t", "_Value "+mapping.GoType); err != nil {
		return err
	}
	for _, flag := range expr.Flags {
		if err := writeRawLine(out, "\t", flag.Name+" bool"); err != nil {
			return err
		}
	}
	if err := writeRawLine(out, "", "}"); err != nil {
		return err
	}
	if err := writeRawLine(out, "", ""); err != nil {
		return err
	}

	if err := writeRawLine(out, "", "func ("+name+") Size() (int, error) {"); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t", "return "+mapping.SizeFn+"("+mapping.GoType+"(0))"); err != nil {
		return err
	}
	if err := writeRawLine(out, "", "}"); err != nil {
		return err
	}
	if err := writeRawLine(out, "", ""); err != nil {
		return err
	}
	if err := writeRawLine(out, "", "func (c "+name+") Append(dst []byte) ([]byte, error) {"); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t", "var raw uint64"); err != nil {
		return err
	}
	for _, flag := range expr.Flags {
		if err := writeRawLine(out, "\t", "if c."+flag.Name+" { raw |= 0x"+strconv.FormatUint(flag.Mask, 16)+" }"); err != nil {
			return err
		}
	}
	if err := writeRawLine(out, "\t", "return "+mapping.AppendFn+"(dst, "+mapping.GoType+"(raw))"); err != nil {
		return err
	}
	if err := writeRawLine(out, "", "}"); err != nil {
		return err
	}
	if err := writeRawLine(out, "", ""); err != nil {
		return err
	}
	if err := writeRawLine(out, "", "func (c *"+name+") Decode(src []byte) ([]byte, error) {"); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t", "var raw "+mapping.GoType); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t", "var err error"); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t", "src, err = "+mapping.DecodeFn+"(src, &raw)"); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t", "if err != nil { return nil, err }"); err != nil {
		return err
	}
	if err := writeRawLine(out, "\t", "c._Value = raw"); err != nil {
		return err
	}
	for _, flag := range expr.Flags {
		if err := writeRawLine(out, "\t", "c."+flag.Name+" = (uint64(raw) & 0x"+strconv.FormatUint(flag.Mask, 16)+") != 0"); err != nil {
			return err
		}
	}
	if err := writeRawLine(out, "\t", "return src, nil"); err != nil {
		return err
	}
	if err := writeRawLine(out, "", "}"); err != nil {
		return err
	}
	if err := writeRawLine(out, "", ""); err != nil {
		return err
	}
	return nil
}

func (g *Generator) emitRegistryHolder(out *bytes.Buffer, name string, expr *schemair.RegistryHolder) error {
	dataType, err := g.goType(name+expr.OtherwiseName, expr.OtherwiseType, name+expr.OtherwiseName)
	if err != nil {
		return err
	}
	var sizeBody, appendBody, decodeBody bytes.Buffer
	if err := g.writeSizeExpr(&sizeBody, nil, nil, expr.OtherwiseType, "(*c."+expr.OtherwiseName+")", "\t\t", name+expr.OtherwiseName, "c", "", nil, "c"); err != nil {
		return err
	}
	if err := g.writeAppendExpr(&appendBody, nil, nil, expr.OtherwiseType, "(*c."+expr.OtherwiseName+")", "\t\t", name+expr.OtherwiseName, "c", "", nil, "c"); err != nil {
		return err
	}
	if err := g.writeDecodeExpr(&decodeBody, nil, nil, expr.OtherwiseType, "(*c."+expr.OtherwiseName+")", "\t\t", name+expr.OtherwiseName, "c", "", nil, "c"); err != nil {
		return err
	}
	g.addImports(wireImportPath)
	return executeTemplate(out, "registryHolder", templateData{
		"Name":       name,
		"BaseName":   expr.BaseName,
		"DataName":   expr.OtherwiseName,
		"DataType":   dataType,
		"SizeBody":   sizeBody.String(),
		"AppendBody": appendBody.String(),
		"DecodeBody": decodeBody.String(),
	})
}

func (g *Generator) emitRegistryHolderSet(out *bytes.Buffer, name string, expr *schemair.RegistryHolderSet) error {
	baseType, err := g.goType(name+expr.BaseName, expr.BaseType, name+expr.BaseName)
	if err != nil {
		return err
	}
	elemType, err := g.goType(name+expr.OtherwiseName+"Item", expr.OtherwiseType, name+expr.OtherwiseName+"Item")
	if err != nil {
		return err
	}
	var baseSize, itemSize, baseAppend, itemAppend, baseDecode, itemDecode bytes.Buffer
	if err := g.writeSizeExpr(&baseSize, nil, nil, expr.BaseType, "(*c."+expr.BaseName+")", "\t\t", name+expr.BaseName, "c", "", nil, "c"); err != nil {
		return err
	}
	if err := g.writeSizeExpr(&itemSize, nil, nil, expr.OtherwiseType, "item", "\t\t\t", name+expr.OtherwiseName+"Item", "c", "", nil, "c"); err != nil {
		return err
	}
	if err := g.writeAppendExpr(&baseAppend, nil, nil, expr.BaseType, "(*c."+expr.BaseName+")", "\t\t", name+expr.BaseName, "c", "", nil, "c"); err != nil {
		return err
	}
	if err := g.writeAppendExpr(&itemAppend, nil, nil, expr.OtherwiseType, "item", "\t\t\t", name+expr.OtherwiseName+"Item", "c", "", nil, "c"); err != nil {
		return err
	}
	if err := g.writeDecodeExpr(&baseDecode, nil, nil, expr.BaseType, "(*c."+expr.BaseName+")", "\t\t", name+expr.BaseName, "c", "", nil, "c"); err != nil {
		return err
	}
	if err := g.writeDecodeExpr(&itemDecode, nil, nil, expr.OtherwiseType, "c."+expr.OtherwiseName+"[i]", "\t\t", name+expr.OtherwiseName+"Item", "c", "", nil, "c"); err != nil {
		return err
	}
	g.addImports(wireImportPath)
	return executeTemplate(out, "registryHolderSet", templateData{
		"Name":           name,
		"BaseName":       expr.BaseName,
		"BaseType":       baseType,
		"DataName":       expr.OtherwiseName,
		"DataType":       elemType,
		"BaseSizeBody":   baseSize.String(),
		"ItemSizeBody":   itemSize.String(),
		"BaseAppendBody": baseAppend.String(),
		"ItemAppendBody": itemAppend.String(),
		"BaseDecodeBody": baseDecode.String(),
		"ItemDecodeBody": itemDecode.String(),
	})
}

func (g *Generator) emitEntityMetadataLoop(out *bytes.Buffer, name string, expr *schemair.EntityMetadataLoop) error {
	elemType, err := g.goType(name+"Item", expr.Elem, name+"Item")
	if err != nil {
		return err
	}
	var sizeBody, appendBody, decodeBody bytes.Buffer
	if err := g.writeSizeExpr(&sizeBody, nil, nil, expr.Elem, "item", "\t\t", name+"Item", "c", "", nil, "c"); err != nil {
		return err
	}
	if err := g.writeAppendExpr(&appendBody, nil, nil, expr.Elem, "item", "\t\t", name+"Item", "c", "", nil, "c"); err != nil {
		return err
	}
	if err := g.writeDecodeExpr(&decodeBody, nil, nil, expr.Elem, "item", "\t\t", name+"Item", "c", "", nil, "c"); err != nil {
		return err
	}
	g.addImports(wireImportPath, "io")
	return executeTemplate(out, "entityMetadataLoop", templateData{
		"Name":           name,
		"ElemType":       elemType,
		"EndVal":         expr.EndVal,
		"ItemSizeBody":   sizeBody.String(),
		"ItemAppendBody": appendBody.String(),
		"ItemDecodeBody": decodeBody.String(),
	})
}

func (g *Generator) emitTopBitSetTerminatedArray(out *bytes.Buffer, name string, expr *schemair.TopBitSetTerminatedArray) error {
	elemType, err := g.goType(name+"Item", expr.Elem, name+"Item")
	if err != nil {
		return err
	}
	var sizeBody, appendBody, decodeBody bytes.Buffer
	if err := g.writeSizeExpr(&sizeBody, nil, nil, expr.Elem, "item", "\t\t", name+"Item", "c", "", nil, "c"); err != nil {
		return err
	}
	if err := g.writeAppendExpr(&appendBody, nil, nil, expr.Elem, "item", "\t\t", name+"Item", "c", "", nil, "c"); err != nil {
		return err
	}
	if err := g.writeDecodeExpr(&decodeBody, nil, nil, expr.Elem, "item", "\t\t", name+"Item", "c", "", nil, "c"); err != nil {
		return err
	}
	g.addImports("io")
	return executeTemplate(out, "topBitSetTerminatedArray", templateData{
		"Name":           name,
		"ElemType":       elemType,
		"ItemSizeBody":   sizeBody.String(),
		"ItemAppendBody": appendBody.String(),
		"ItemDecodeBody": decodeBody.String(),
	})
}

func (g *Generator) emitSize(out *bytes.Buffer, name string, expr *schemair.Container) error {
	var body bytes.Buffer
	if err := g.writeContainerFieldsSize(&body, expr, nil, expr.Fields, "c", "\t", name, "c", "", expr, "c"); err != nil {
		return err
	}
	return executeTemplate(out, "sizeFunc", templateData{"Name": name, "Body": body.String()})
}

func (g *Generator) emitAppend(out *bytes.Buffer, name string, expr *schemair.Container) error {
	if len(expr.Fields) == 0 {
		return executeTemplate(out, "emptyAppendFunc", templateData{"Name": name})
	}
	var body bytes.Buffer
	if err := g.writeContainerFieldsAppend(&body, expr, nil, expr.Fields, "c", "\t", name, "c", "", expr, "c"); err != nil {
		return err
	}
	return executeTemplate(out, "appendFunc", templateData{"Name": name, "Body": body.String()})
}

func (g *Generator) emitDecode(out *bytes.Buffer, name string, expr *schemair.Container) error {
	if len(expr.Fields) == 0 {
		return executeTemplate(out, "emptyDecodeFunc", templateData{"Name": name})
	}
	var body bytes.Buffer
	if err := g.writeContainerFieldsDecode(&body, expr, nil, expr.Fields, "c", "\t", name, "c", "", expr, "c"); err != nil {
		return err
	}
	return executeTemplate(out, "decodeFunc", templateData{"Name": name, "Body": body.String()})
}

func (g *Generator) goType(name string, expr schemair.Expr, ctx string) (string, error) {
	switch v := expr.(type) {
	case *schemair.Option:
		inner, err := g.goType(name, v.Inner, ctx+"Value")
		if err != nil {
			return "", err
		}
		return "*" + inner, nil
	case *schemair.Array:
		elem, err := g.goType(name, v.Elem, ctx+"Item")
		if err != nil {
			return "", err
		}
		return "[]" + elem, nil
	case *schemair.Ref:
		if mapping, ok := g.natives[v.Name]; ok {
			g.addMappingImports(mapping)
			return mapping.GoType, nil
		}
		if def, ok := g.types[v.Name]; ok {
			if alias, ok := g.addExternalRef(def.Name); ok {
				return alias + "." + def.Name, nil
			}
			switch def.Expr.(type) {
			case *schemair.Container, *schemair.Mapper, *schemair.Bitfield, *schemair.Bitflags, *schemair.Array, *schemair.RegistryHolder, *schemair.RegistryHolderSet, *schemair.EntityMetadataLoop, *schemair.TopBitSetTerminatedArray:
				return def.Name, nil
			default:
				return g.goType(def.Name, def.Expr, ctx)
			}
		}
		return v.Name, nil
	case *schemair.Native:
		if mapping, ok := g.natives[v.Name]; ok {
			g.addMappingImports(mapping)
			return mapping.GoType, nil
		}
		return "", fmt.Errorf("unsupported native %s", v.Name)
	case *schemair.Call:
		switch v.Name {
		case "Count":
			mapping, err := g.countCallMapping(v)
			if err != nil {
				return "", err
			}
			return mapping.GoType, nil
		case "Buffer":
			return "[]byte", nil
		case "Pstring":
			return "string", nil
		case "Cstring":
			return "string", nil
		case "Int":
			return "*wire.BigInt", nil
		case "Bitflags":
			return "", fmt.Errorf("legacy bitflags call is unsupported")
		default:
			if mapping, ok := g.natives[v.Name]; ok {
				g.addMappingImports(mapping)
				return mapping.GoType, nil
			}
			return "", fmt.Errorf("unsupported call %s", v.Name)
		}
	case *schemair.Container:
		if _, ok := g.types[ctx]; ok {
			if alias, ok := g.addExternalRef(ctx); ok {
				return alias + "." + ctx, nil
			}
			return ctx, nil
		}
		typeName := ctx
		if _, ok := g.types[typeName]; !ok {
			for _, synthetic := range g.synthetic {
				if synthetic.Name == typeName {
					return typeName, nil
				}
			}
			g.synthetic = append(g.synthetic, &schemair.Definition{Name: typeName, Expr: v})
		}
		return typeName, nil
	case *schemair.Mapper:
		if _, ok := g.types[ctx]; ok {
			if alias, ok := g.addExternalRef(ctx); ok {
				return alias + "." + ctx, nil
			}
			return ctx, nil
		}
		typeName := ctx
		for _, synthetic := range g.synthetic {
			if synthetic.Name == typeName {
				return typeName, nil
			}
		}
		g.synthetic = append(g.synthetic, &schemair.Definition{Name: typeName, Expr: v})
		return typeName, nil
	case *schemair.Bitfield:
		if _, ok := g.types[ctx]; ok {
			if alias, ok := g.addExternalRef(ctx); ok {
				return alias + "." + ctx, nil
			}
			return ctx, nil
		}
		typeName := ctx
		for _, synthetic := range g.synthetic {
			if synthetic.Name == typeName {
				return typeName, nil
			}
		}
		g.synthetic = append(g.synthetic, &schemair.Definition{Name: typeName, Expr: v})
		return typeName, nil
	case *schemair.Bitflags:
		if _, ok := g.types[ctx]; ok {
			if alias, ok := g.addExternalRef(ctx); ok {
				return alias + "." + ctx, nil
			}
			return ctx, nil
		}
		typeName := ctx
		for _, synthetic := range g.synthetic {
			if synthetic.Name == typeName {
				return typeName, nil
			}
		}
		g.synthetic = append(g.synthetic, &schemair.Definition{Name: typeName, Expr: v})
		return typeName, nil
	case *schemair.RegistryHolder, *schemair.RegistryHolderSet, *schemair.EntityMetadataLoop, *schemair.TopBitSetTerminatedArray:
		if _, ok := g.types[ctx]; ok {
			if alias, ok := g.addExternalRef(ctx); ok {
				return alias + "." + ctx, nil
			}
			return ctx, nil
		}
		for _, synthetic := range g.synthetic {
			if synthetic.Name == ctx {
				return ctx, nil
			}
		}
		g.synthetic = append(g.synthetic, &schemair.Definition{Name: ctx, Expr: v})
		return ctx, nil
	case *schemair.Void:
		return "struct{}", nil
	case *schemair.Switch:
		return "any", nil
	default:
		return "", fmt.Errorf("unsupported expr %T", expr)
	}
}

func (g *Generator) collectImports(defs []*schemair.Definition) error {
	visited := make(map[string]struct{}, len(defs))
	for _, def := range defs {
		if err := g.walkExpr(def.Name, def.Expr, visited); err != nil {
			return err
		}
	}
	return nil
}

func (g *Generator) walkExpr(name string, expr schemair.Expr, visited map[string]struct{}) error {
	if name != "" {
		if _, ok := visited[name]; ok {
			return nil
		}
		visited[name] = struct{}{}
	}
	switch v := expr.(type) {
	case *schemair.Option:
		return g.walkExpr("", v.Inner, visited)
	case *schemair.Array:
		return g.walkExpr("", v.Elem, visited)
	case *schemair.Ref:
		if mapping, ok := g.natives[v.Name]; ok {
			g.addMappingImports(mapping)
			return nil
		}
		if def, ok := g.types[v.Name]; ok {
			if _, ok := g.addExternalRef(def.Name); ok {
				return nil
			}
			return g.walkExpr(def.Name, def.Expr, visited)
		}
		return nil
	case *schemair.Native:
		if mapping, ok := g.natives[v.Name]; ok {
			g.addMappingImports(mapping)
		}
		return nil
	case *schemair.Call:
		if mapping, ok := g.natives[v.Name]; ok {
			g.addMappingImports(mapping)
		}
		switch v.Name {
		case "Buffer":
			spec := callCountSpec(v)
			if spec.Fixed != nil {
				g.addImports("fmt")
			}
		case "Pstring":
			spec := callCountSpec(v)
			if spec.Field != "" || spec.Fixed != nil {
				g.addImports("fmt")
			}
		}
		return nil
	case *schemair.Container:
		for _, field := range v.Fields {
			if err := g.walkExpr("", field.Type, visited); err != nil {
				return err
			}
		}
		return nil
	case *schemair.Bitfield:
		g.addImports("io")
		return nil
	case *schemair.Bitflags:
		return nil
	case *schemair.Mapper:
		return g.walkExpr("", v.Base, visited)
	case *schemair.RegistryHolder:
		g.addImports(wireImportPath)
		return g.walkExpr("", v.OtherwiseType, visited)
	case *schemair.RegistryHolderSet:
		g.addImports(wireImportPath)
		if err := g.walkExpr("", v.BaseType, visited); err != nil {
			return err
		}
		return g.walkExpr("", v.OtherwiseType, visited)
	case *schemair.EntityMetadataLoop:
		g.addImports("io")
		return g.walkExpr("", v.Elem, visited)
	case *schemair.TopBitSetTerminatedArray:
		g.addImports("io")
		return g.walkExpr("", v.Elem, visited)
	case *schemair.Switch:
		g.addImports("fmt")
		for _, c := range v.Cases {
			if err := g.walkExpr("", c.Expr, visited); err != nil {
				return err
			}
		}
		if v.Default != nil {
			return g.walkExpr("", v.Default, visited)
		}
		return nil
	default:
		return nil
	}
}

func (g *Generator) writeContainerFieldsSize(out *bytes.Buffer, current *schemair.Container, parent *schemair.Container, fields []schemair.Field, value string, indent string, ctx string, owner string, parentOwner string, rootContainer *schemair.Container, rootOwner string) error {
	for _, field := range fields {
		if field.Anonymous {
			if inner := g.anonymousContainer(field.Type); inner != nil {
				if err := g.writeContainerFieldsSize(out, inner, current, inner.Fields, value, indent, ctx+field.Name, value, owner, rootContainer, rootOwner); err != nil {
					return err
				}
				continue
			}
		}
		if err := g.writeSizeExpr(out, current, parent, field.Type, value+"."+field.Name, indent, ctx+field.Name, value, owner, rootContainer, rootOwner); err != nil {
			return err
		}
	}
	return nil
}

func (g *Generator) writeContainerFieldsAppend(out *bytes.Buffer, current *schemair.Container, parent *schemair.Container, fields []schemair.Field, value string, indent string, ctx string, owner string, parentOwner string, rootContainer *schemair.Container, rootOwner string) error {
	for _, field := range fields {
		if field.Anonymous {
			if inner := g.anonymousContainer(field.Type); inner != nil {
				if err := g.writeContainerFieldsAppend(out, inner, current, inner.Fields, value, indent, ctx+field.Name, value, owner, rootContainer, rootOwner); err != nil {
					return err
				}
				continue
			}
		}
		if err := g.writeAppendExpr(out, current, parent, field.Type, value+"."+field.Name, indent, ctx+field.Name, value, owner, rootContainer, rootOwner); err != nil {
			return err
		}
	}
	return nil
}

func (g *Generator) writeContainerFieldsDecode(out *bytes.Buffer, current *schemair.Container, parent *schemair.Container, fields []schemair.Field, value string, indent string, ctx string, owner string, parentOwner string, rootContainer *schemair.Container, rootOwner string) error {
	for _, field := range fields {
		if field.Anonymous {
			if inner := g.anonymousContainer(field.Type); inner != nil {
				if err := g.writeContainerFieldsDecode(out, inner, current, inner.Fields, value, indent, ctx+field.Name, value, owner, rootContainer, rootOwner); err != nil {
					return err
				}
				continue
			}
		}
		if err := g.writeDecodeExpr(out, current, parent, field.Type, value+"."+field.Name, indent, ctx+field.Name, value, owner, rootContainer, rootOwner); err != nil {
			return err
		}
	}
	return nil
}

func (g *Generator) writeSizeExpr(out *bytes.Buffer, container *schemair.Container, parentContainer *schemair.Container, expr schemair.Expr, value string, indent string, ctx string, owner string, parentOwner string, rootContainer *schemair.Container, rootOwner string) error {
	switch v := expr.(type) {
	case *schemair.Option:
		if err := executeTemplate(out, "sizeOptionStart", templateData{"Indent": indent, "Value": value}); err != nil {
			return err
		}
		if err := g.writeSizeExpr(out, container, parentContainer, v.Inner, derefExpr(value), indent+"\t", ctx+"Value", owner, parentOwner, rootContainer, rootOwner); err != nil {
			return err
		}
		return executeTemplate(out, "blockEnd", templateData{"Indent": indent})
	case *schemair.Array:
		countExpr, err := g.countSizeSnippet(v.Count, value, owner)
		if err != nil {
			return err
		}
		if countExpr != "" {
			if err := executeTemplate(out, "rawLine", templateData{"Indent": indent, "Code": countExpr}); err != nil {
				return err
			}
		}
		if err := executeTemplate(out, "rangeStart", templateData{"Indent": indent, "Value": value}); err != nil {
			return err
		}
		if err := g.writeSizeExpr(out, container, parentContainer, v.Elem, "item", indent+"\t", ctx+"Item", owner, parentOwner, rootContainer, rootOwner); err != nil {
			return err
		}
		return executeTemplate(out, "blockEnd", templateData{"Indent": indent})
	case *schemair.Ref:
		if mapping, ok := g.natives[v.Name]; ok {
			g.addMappingImports(mapping)
			return writeSizeCall(out, indent, fmt.Sprintf("%s(%s)", mapping.SizeFn, value))
		}
		if def, ok := g.types[v.Name]; ok {
			if _, external := g.addExternalRef(def.Name); external {
				return writeSizeCall(out, indent, fmt.Sprintf("%s.Size()", value))
			}
			switch inner := def.Expr.(type) {
			case *schemair.Mapper:
				baseName, err := g.nativeName(inner.Base)
				if err != nil {
					return err
				}
				mapping := g.natives[baseName]
				return writeSizeCall(out, indent, fmt.Sprintf("%s(%s(%s))", mapping.SizeFn, mapping.GoType, value))
			case *schemair.Container:
				if exprHasExternalCompare(inner) {
					for _, field := range inner.Fields {
						if err := g.writeSizeExpr(out, inner, container, field.Type, value+"."+field.Name, indent, def.Name+field.Name, value, owner, rootContainer, rootOwner); err != nil {
							return err
						}
					}
					return nil
				}
				return writeSizeCall(out, indent, fmt.Sprintf("%s.Size()", value))
			case *schemair.Array, *schemair.RegistryHolder, *schemair.RegistryHolderSet, *schemair.EntityMetadataLoop, *schemair.TopBitSetTerminatedArray:
				return writeSizeCall(out, indent, fmt.Sprintf("%s.Size()", value))
			case *schemair.Bitfield:
				return writeSizeCall(out, indent, fmt.Sprintf("%s.Size()", value))
			default:
				return g.writeSizeExpr(out, container, parentContainer, inner, value, indent, ctx, owner, parentOwner, rootContainer, rootOwner)
			}
		}
		return fmt.Errorf("unknown ref %s", v.Name)
	case *schemair.Native:
		mapping, ok := g.natives[v.Name]
		if !ok {
			return fmt.Errorf("unsupported native %s", v.Name)
		}
		g.addMappingImports(mapping)
		return writeSizeCall(out, indent, fmt.Sprintf("%s(%s)", mapping.SizeFn, value))
	case *schemair.Call:
		switch v.Name {
		case "Count":
			mapping, err := g.countCallMapping(v)
			if err != nil {
				return err
			}
			return writeSizeCall(out, indent, fmt.Sprintf("%s(%s)", mapping.SizeFn, value))
		case "Buffer":
			return g.writeBufferSize(out, v, value, indent, owner, ctx)
		case "Cstring":
			encoding := stringCallEncoding(v)
			return writeSizeCall(out, indent, fmt.Sprintf("wire.SizeCString(%s, %q)", value, encoding))
		case "Int":
			size, err := intCallSize(v)
			if err != nil {
				return err
			}
			return writeSizeCall(out, indent, fmt.Sprintf("wire.SizeSizedUint(%s, %d)", value, size))
		case "Pstring":
			return g.writePstringSize(out, v, value, indent, owner)
		case "Bitflags":
			return fmt.Errorf("legacy bitflags call is unsupported")
		default:
			return fmt.Errorf("unsupported call %s", v.Name)
		}
	case *schemair.Container:
		return g.writeContainerFieldsSize(out, v, container, v.Fields, value, indent, ctx, owner, parentOwner, rootContainer, rootOwner)
	case *schemair.Bitfield:
		return writeSizeCall(out, indent, fmt.Sprintf("%s.Size()", value))
	case *schemair.Bitflags:
		return writeSizeCall(out, indent, fmt.Sprintf("%s.Size()", value))
	case *schemair.Mapper:
		baseName, err := g.nativeName(v.Base)
		if err != nil {
			return err
		}
		mapping := g.natives[baseName]
		return writeSizeCall(out, indent, fmt.Sprintf("%s(%s(%s))", mapping.SizeFn, mapping.GoType, value))
	case *schemair.RegistryHolder, *schemair.RegistryHolderSet, *schemair.EntityMetadataLoop, *schemair.TopBitSetTerminatedArray:
		return writeSizeCall(out, indent, fmt.Sprintf("%s.Size()", value))
	case *schemair.Switch:
		return g.writeSwitchSize(out, container, parentContainer, v, value, indent, ctx, owner, parentOwner, rootContainer, rootOwner)
	default:
		return fmt.Errorf("unsupported size expr %T", expr)
	}
}

func (g *Generator) writeAppendExpr(out *bytes.Buffer, container *schemair.Container, parentContainer *schemair.Container, expr schemair.Expr, value string, indent string, ctx string, owner string, parentOwner string, rootContainer *schemair.Container, rootOwner string) error {
	switch v := expr.(type) {
	case *schemair.Option:
		if err := writeAppendCall(out, indent, "wire.AppendBool(dst, "+value+" != nil)"); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "if "+value+" != nil {"); err != nil {
			return err
		}
		if err := g.writeAppendExpr(out, container, parentContainer, v.Inner, derefExpr(value), indent+"\t", ctx+"Value", owner, parentOwner, rootContainer, rootOwner); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "}"); err != nil {
			return err
		}
		return nil
	case *schemair.Array:
		countExpr, err := g.countAppendSnippet(v.Count, value, owner)
		if err != nil {
			return err
		}
		if countExpr != "" {
			if err := writeRawLine(out, indent, countExpr); err != nil {
				return err
			}
		}
		if err := writeRawLine(out, indent, "for _, item := range "+value+" {"); err != nil {
			return err
		}
		if err := g.writeAppendExpr(out, container, parentContainer, v.Elem, "item", indent+"\t", ctx+"Item", owner, parentOwner, rootContainer, rootOwner); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "}"); err != nil {
			return err
		}
		return nil
	case *schemair.Ref:
		if mapping, ok := g.natives[v.Name]; ok {
			g.addMappingImports(mapping)
			if err := writeAppendCall(out, indent, mapping.AppendFn+"(dst, "+value+")"); err != nil {
				return err
			}
			return nil
		}
		if def, ok := g.types[v.Name]; ok {
			if _, external := g.addExternalRef(def.Name); external {
				return writeAppendCall(out, indent, fmt.Sprintf("%s.Append(dst)", value))
			}
			switch inner := def.Expr.(type) {
			case *schemair.Mapper:
				baseName, err := g.nativeName(inner.Base)
				if err != nil {
					return err
				}
				mapping := g.natives[baseName]
				if err := writeAppendCall(out, indent, mapping.AppendFn+"(dst, "+mapping.GoType+"("+value+"))"); err != nil {
					return err
				}
				return nil
			case *schemair.Container:
				if exprHasExternalCompare(inner) {
					for _, field := range inner.Fields {
						if err := g.writeAppendExpr(out, inner, container, field.Type, value+"."+field.Name, indent, def.Name+field.Name, value, owner, rootContainer, rootOwner); err != nil {
							return err
						}
					}
					return nil
				}
				if err := writeAppendCall(out, indent, value+".Append(dst)"); err != nil {
					return err
				}
				return nil
			case *schemair.Array, *schemair.RegistryHolder, *schemair.RegistryHolderSet, *schemair.EntityMetadataLoop, *schemair.TopBitSetTerminatedArray:
				if err := writeAppendCall(out, indent, value+".Append(dst)"); err != nil {
					return err
				}
				return nil
			case *schemair.Bitfield:
				if err := writeAppendCall(out, indent, value+".Append(dst)"); err != nil {
					return err
				}
				return nil
			default:
				return g.writeAppendExpr(out, container, parentContainer, inner, value, indent, ctx, owner, parentOwner, rootContainer, rootOwner)
			}
		}
		return fmt.Errorf("unknown ref %s", v.Name)
	case *schemair.Native:
		mapping, ok := g.natives[v.Name]
		if !ok {
			return fmt.Errorf("unsupported native %s", v.Name)
		}
		g.addMappingImports(mapping)
		if err := writeAppendCall(out, indent, mapping.AppendFn+"(dst, "+value+")"); err != nil {
			return err
		}
		return nil
	case *schemair.Call:
		switch v.Name {
		case "Count":
			mapping, err := g.countCallMapping(v)
			if err != nil {
				return err
			}
			if err := writeAppendCall(out, indent, mapping.AppendFn+"(dst, "+value+")"); err != nil {
				return err
			}
			return nil
		case "Buffer":
			return g.writeBufferAppend(out, v, value, indent, owner, ctx)
		case "Cstring":
			encoding := stringCallEncoding(v)
			if err := writeAppendCall(out, indent, `wire.AppendCString(dst, `+value+`, `+strconv.Quote(encoding)+`)`); err != nil {
				return err
			}
			return nil
		case "Int":
			size, err := intCallSize(v)
			if err != nil {
				return err
			}
			if err := writeAppendCall(out, indent, "wire.AppendSizedUint(dst, "+value+", "+strconv.Itoa(size)+")"); err != nil {
				return err
			}
			return nil
		case "Pstring":
			return g.writePstringAppend(out, v, value, indent, owner)
		case "Bitflags":
			return fmt.Errorf("legacy bitflags call is unsupported")
		default:
			return fmt.Errorf("unsupported call %s", v.Name)
		}
	case *schemair.Container:
		return g.writeContainerFieldsAppend(out, v, container, v.Fields, value, indent, ctx, owner, parentOwner, rootContainer, rootOwner)
	case *schemair.Bitfield:
		if err := writeAppendCall(out, indent, value+".Append(dst)"); err != nil {
			return err
		}
		return nil
	case *schemair.Bitflags:
		if err := writeAppendCall(out, indent, value+".Append(dst)"); err != nil {
			return err
		}
		return nil
	case *schemair.Mapper:
		baseName, err := g.nativeName(v.Base)
		if err != nil {
			return err
		}
		mapping := g.natives[baseName]
		if err := writeAppendCall(out, indent, mapping.AppendFn+"(dst, "+mapping.GoType+"("+value+"))"); err != nil {
			return err
		}
		return nil
	case *schemair.RegistryHolder, *schemair.RegistryHolderSet, *schemair.EntityMetadataLoop, *schemair.TopBitSetTerminatedArray:
		if err := writeAppendCall(out, indent, value+".Append(dst)"); err != nil {
			return err
		}
		return nil
	case *schemair.Switch:
		return g.writeSwitchAppend(out, container, parentContainer, v, value, indent, ctx, owner, parentOwner, rootContainer, rootOwner)
	default:
		return fmt.Errorf("unsupported append expr %T", expr)
	}
}

func (g *Generator) writeDecodeExpr(out *bytes.Buffer, container *schemair.Container, parentContainer *schemair.Container, expr schemair.Expr, value string, indent string, ctx string, owner string, parentOwner string, rootContainer *schemair.Container, rootOwner string) error {
	switch v := expr.(type) {
	case *schemair.Option:
		present := strcase.ToLowerCamel(ctx) + "Present"
		if err := writeRawLine(out, indent, "var "+present+" bool"); err != nil {
			return err
		}
		if err := writeDecodeCall(out, indent, "wire.DecodeBool(src, &"+present+")"); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "if "+present+" {"); err != nil {
			return err
		}
		innerType, err := g.goType(ctx, v.Inner, ctx+"Value")
		if err != nil {
			return err
		}
		if err := writeRawLine(out, indent+"\t", value+" = new("+innerType+")"); err != nil {
			return err
		}
		if err := g.writeDecodeExpr(out, container, parentContainer, v.Inner, derefExpr(value), indent+"\t", ctx+"Value", owner, parentOwner, rootContainer, rootOwner); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "} else {"); err != nil {
			return err
		}
		if err := writeRawLine(out, indent+"\t", value+" = nil"); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "}"); err != nil {
			return err
		}
		return nil
	case *schemair.Array:
		lengthVar := strcase.ToLowerCamel(ctx) + "Len"
		switch {
		case v.Count.Fixed != nil:
			if err := writeRawLine(out, indent, lengthVar+" := "+strconv.Itoa(*v.Count.Fixed)); err != nil {
				return err
			}
		case v.Count.Field != "":
			if err := writeRawLine(out, indent, lengthVar+" := int("+fieldPathExpr(owner, v.Count.Field)+")"); err != nil {
				return err
			}
		default:
			nativeName := v.Count.Type
			mapping, ok := g.natives[nativeName]
			if !ok {
				return fmt.Errorf("unsupported array count type %s", v.Count.Type)
			}
			if err := writeRawLine(out, indent, "var "+lengthVar+" "+mapping.GoType); err != nil {
				return err
			}
			if err := writeDecodeCall(out, indent, mapping.DecodeFn+"(src, &"+lengthVar+")"); err != nil {
				return err
			}
			if err := writeRawLine(out, indent, lengthVar+"Count := int("+lengthVar+")"); err != nil {
				return err
			}
			lengthVar += "Count"
		}
		elemType, err := g.goType(ctx, v.Elem, ctx+"Item")
		if err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "if cap("+value+") < "+lengthVar+" { "+value+" = make([]"+elemType+", "+lengthVar+") } else { "+value+" = "+sliceExpr(value)+"[:"+lengthVar+"] }"); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "for i := range "+value+" {"); err != nil {
			return err
		}
		if err := g.writeDecodeExpr(out, container, parentContainer, v.Elem, indexExpr(value, "i"), indent+"\t", ctx+"Item", owner, parentOwner, rootContainer, rootOwner); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "}"); err != nil {
			return err
		}
		return nil
	case *schemair.Ref:
		if mapping, ok := g.natives[v.Name]; ok {
			g.addMappingImports(mapping)
			if err := writeDecodeCall(out, indent, mapping.DecodeFn+"(src, &"+value+")"); err != nil {
				return err
			}
			return nil
		}
		if def, ok := g.types[v.Name]; ok {
			if _, external := g.addExternalRef(def.Name); external {
				return writeDecodeCall(out, indent, fmt.Sprintf("%s.Decode(src)", value))
			}
			switch inner := def.Expr.(type) {
			case *schemair.Mapper:
				baseName, err := g.nativeName(inner.Base)
				if err != nil {
					return err
				}
				mapping := g.natives[baseName]
				temp := strcase.ToLowerCamel(ctx) + "Value"
				if err := writeRawLine(out, indent, "var "+temp+" "+mapping.GoType); err != nil {
					return err
				}
				if err := writeDecodeCall(out, indent, mapping.DecodeFn+"(src, &"+temp+")"); err != nil {
					return err
				}
				typeName := v.Name
				if alias, ok := g.addExternalRef(v.Name); ok {
					typeName = alias + "." + v.Name
				}
				if err := writeRawLine(out, indent, value+" = "+typeName+"("+temp+")"); err != nil {
					return err
				}
				return nil
			case *schemair.Container:
				if exprHasExternalCompare(inner) {
					for _, field := range inner.Fields {
						if err := g.writeDecodeExpr(out, inner, container, field.Type, value+"."+field.Name, indent, def.Name+field.Name, value, owner, rootContainer, rootOwner); err != nil {
							return err
						}
					}
					return nil
				}
				if err := writeDecodeCall(out, indent, value+".Decode(src)"); err != nil {
					return err
				}
				return nil
			case *schemair.Array, *schemair.RegistryHolder, *schemair.RegistryHolderSet, *schemair.EntityMetadataLoop, *schemair.TopBitSetTerminatedArray:
				if err := writeDecodeCall(out, indent, value+".Decode(src)"); err != nil {
					return err
				}
				return nil
			case *schemair.Bitfield:
				if err := writeDecodeCall(out, indent, value+".Decode(src)"); err != nil {
					return err
				}
				return nil
			default:
				return g.writeDecodeExpr(out, container, parentContainer, inner, value, indent, ctx, owner, parentOwner, rootContainer, rootOwner)
			}
		}
		return fmt.Errorf("unknown ref %s", v.Name)
	case *schemair.Native:
		mapping, ok := g.natives[v.Name]
		if !ok {
			return fmt.Errorf("unsupported native %s", v.Name)
		}
		g.addMappingImports(mapping)
		if err := writeDecodeCall(out, indent, mapping.DecodeFn+"(src, &"+value+")"); err != nil {
			return err
		}
		return nil
	case *schemair.Call:
		switch v.Name {
		case "Count":
			mapping, err := g.countCallMapping(v)
			if err != nil {
				return err
			}
			if err := writeDecodeCall(out, indent, mapping.DecodeFn+"(src, &"+value+")"); err != nil {
				return err
			}
			return nil
		case "Buffer":
			return g.writeBufferDecode(out, v, value, indent, owner, ctx)
		case "Cstring":
			encoding := stringCallEncoding(v)
			if err := writeDecodeCall(out, indent, `wire.DecodeCString(src, &`+value+`, `+strconv.Quote(encoding)+`)`); err != nil {
				return err
			}
			return nil
		case "Int":
			size, err := intCallSize(v)
			if err != nil {
				return err
			}
			if err := writeDecodeCall(out, indent, "wire.DecodeSizedUint(src, &"+value+", "+strconv.Itoa(size)+")"); err != nil {
				return err
			}
			return nil
		case "Pstring":
			return g.writePstringDecode(out, v, value, indent, owner)
		case "Bitflags":
			return fmt.Errorf("legacy bitflags call is unsupported")
		default:
			return fmt.Errorf("unsupported call %s", v.Name)
		}
	case *schemair.Container:
		return g.writeContainerFieldsDecode(out, v, container, v.Fields, value, indent, ctx, owner, parentOwner, rootContainer, rootOwner)
	case *schemair.Bitfield:
		if err := writeDecodeCall(out, indent, value+".Decode(src)"); err != nil {
			return err
		}
		return nil
	case *schemair.Bitflags:
		if err := writeDecodeCall(out, indent, value+".Decode(src)"); err != nil {
			return err
		}
		return nil
	case *schemair.Mapper:
		baseName, err := g.nativeName(v.Base)
		if err != nil {
			return err
		}
		mapping := g.natives[baseName]
		temp := strcase.ToLowerCamel(ctx) + "Value"
		if err := writeRawLine(out, indent, "var "+temp+" "+mapping.GoType); err != nil {
			return err
		}
		if err := writeDecodeCall(out, indent, mapping.DecodeFn+"(src, &"+temp+")"); err != nil {
			return err
		}
		typeName := ctx
		if alias, ok := g.addExternalRef(ctx); ok {
			typeName = alias + "." + ctx
		}
		if err := writeRawLine(out, indent, value+" = "+typeName+"("+temp+")"); err != nil {
			return err
		}
		return nil
	case *schemair.RegistryHolder, *schemair.RegistryHolderSet, *schemair.EntityMetadataLoop, *schemair.TopBitSetTerminatedArray:
		if err := writeDecodeCall(out, indent, value+".Decode(src)"); err != nil {
			return err
		}
		return nil
	case *schemair.Switch:
		return g.writeSwitchDecode(out, container, parentContainer, v, value, indent, ctx, owner, parentOwner, rootContainer, rootOwner)
	default:
		return fmt.Errorf("unsupported decode expr %T", expr)
	}
}

func (g *Generator) countSizeSnippet(count schemair.Count, value string, owner string) (string, error) {
	switch {
	case count.Fixed != nil:
		return "", nil
	case count.Field != "":
		return "", nil
	case count.Type != "":
		mapping, ok := g.natives[count.Type]
		if !ok {
			return "", fmt.Errorf("unsupported count type %s", count.Type)
		}
		return fmt.Sprintf("nn, err = %s(%s(len(%s))); n += nn; if err != nil { return 0, err }", mapping.SizeFn, mapping.GoType, value), nil
	default:
		return "", nil
	}
}

func (g *Generator) countAppendSnippet(count schemair.Count, value string, owner string) (string, error) {
	switch {
	case count.Fixed != nil:
		return "", nil
	case count.Field != "":
		return "", nil
	case count.Type != "":
		mapping, ok := g.natives[count.Type]
		if !ok {
			return "", fmt.Errorf("unsupported count type %s", count.Type)
		}
		return fmt.Sprintf("dst, err = %s(dst, %s(len(%s))); if err != nil { return nil, err }", mapping.AppendFn, mapping.GoType, value), nil
	default:
		return "", nil
	}
}

func (g *Generator) nativeName(expr schemair.Expr) (string, error) {
	ref, ok := expr.(*schemair.Ref)
	if !ok {
		return "", fmt.Errorf("expected native ref, got %T", expr)
	}
	return ref.Name, nil
}

func (g *Generator) addImports(imports ...string) {
	for _, imp := range imports {
		g.addImportSpec(importSpec{Path: imp})
	}
}

func (g *Generator) addImportSpec(spec importSpec) {
	if spec.Path == "" {
		return
	}
	for _, existing := range g.imports {
		if existing.Path == spec.Path && existing.Alias == spec.Alias {
			return
		}
	}
	g.imports = append(g.imports, spec)
}

func (g *Generator) addMappingImports(mapping NativeMapping) {
	g.addImports(mapping.Imports...)
	if strings.Contains(mapping.GoType, "wire.") || strings.HasPrefix(mapping.SizeFn, "wire.") || strings.HasPrefix(mapping.AppendFn, "wire.") || strings.HasPrefix(mapping.DecodeFn, "wire.") {
		g.addImports(wireImportPath)
	}
}

func (g *Generator) addExternalRef(name string) (string, bool) {
	if g.isLocal(name) {
		return "", false
	}
	ref, ok := g.externalRefs[name]
	if !ok || ref.ImportPath == "" {
		return "", false
	}
	alias := ref.Alias
	if alias == "" {
		alias = defaultImportAlias(ref.ImportPath)
	}
	g.addImportSpec(importSpec{Path: ref.ImportPath, Alias: alias})
	return alias, true
}

func (g *Generator) isLocal(name string) bool {
	if len(g.local) == 0 {
		return true
	}
	_, ok := g.local[name]
	return ok
}

func (g *Generator) isOriginal(name string) bool {
	if len(g.original) == 0 {
		return true
	}
	_, ok := g.original[name]
	return ok
}

func defaultImportAlias(path string) string {
	base := strcase.ToLowerCamel(strings.ReplaceAll(path, "/", " "))
	base = strings.ReplaceAll(base, ".", "")
	return base
}

func pruneUnusedImports(src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "generated.go", src, parser.ParseComments)
	if err != nil {
		return src, nil
	}
	used := map[string]struct{}{}
	for _, decl := range file.Decls {
		ast.Inspect(decl, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if ident, ok := sel.X.(*ast.Ident); ok {
				used[ident.Name] = struct{}{}
			}
			return true
		})
	}
	var kept []ast.Spec
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}
		for _, spec := range gen.Specs {
			imp := spec.(*ast.ImportSpec)
			name := importName(imp)
			if _, ok := used[name]; ok {
				kept = append(kept, imp)
			}
		}
	}
	var decls []ast.Decl
	if len(kept) > 0 {
		decls = append(decls, &ast.GenDecl{Tok: token.IMPORT, Specs: kept})
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if ok && gen.Tok == token.IMPORT {
			continue
		}
		decls = append(decls, decl)
	}
	file.Decls = decls
	var out bytes.Buffer
	if err := format.Node(&out, fset, file); err != nil {
		return src, nil
	}
	return out.Bytes(), nil
}

func importName(spec *ast.ImportSpec) string {
	if spec.Name != nil {
		return spec.Name.Name
	}
	return path.Base(strings.Trim(spec.Path.Value, `"`))
}
