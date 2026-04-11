package gogen

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/iancoleman/strcase"

	"github.com/KonjacBot/packetizer/schemair"
)

type countSpec struct {
	Type  string
	Field string
	Fixed *int
	Rest  bool
}

func stringCallEncoding(call *schemair.Call) string {
	if encoding, ok := call.Options["encoding"].(string); ok && encoding != "" {
		return encoding
	}
	return "utf-8"
}

func callCountSpec(call *schemair.Call) countSpec {
	var spec countSpec
	if raw, ok := call.Options["countType"].(string); ok {
		spec.Type = raw
	}
	if raw, ok := call.Options["countFor"].(string); ok && raw != "" && spec.Field == "" {
		spec.Field = raw
	}
	if raw, ok := call.Options["count"].(string); ok {
		spec.Field = raw
	}
	if raw, ok := call.Options["count"].(int); ok {
		spec.Fixed = &raw
	}
	if raw, ok := call.Options["rest"].(bool); ok {
		spec.Rest = raw
	}
	return spec
}

func intCallSize(call *schemair.Call) (int, error) {
	raw, ok := call.Options["size"]
	if !ok {
		return 0, fmt.Errorf("int missing size")
	}
	switch v := raw.(type) {
	case int:
		return v, nil
	case int64, float64:
		if n, ok := toInt(v); ok {
			return n, nil
		}
	}
	return 0, fmt.Errorf("invalid int size %v", raw)
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func (g *Generator) countCallMapping(call *schemair.Call) (NativeMapping, error) {
	rawType, ok := call.Options["type"].(string)
	if !ok || rawType == "" {
		return NativeMapping{}, fmt.Errorf("count missing type")
	}
	mapping, ok := g.natives[rawType]
	if !ok {
		return NativeMapping{}, fmt.Errorf("unsupported count type %s", rawType)
	}
	return mapping, nil
}

func (g *Generator) writeBufferSize(out *bytes.Buffer, call *schemair.Call, value string, indent string, owner string, ctx string) error {
	spec := callCountSpec(call)
	switch {
	case spec.Rest:
		if err := writeSizeCall(out, indent, "wire.SizeRestBuffer("+value+")"); err != nil {
			return err
		}
	case spec.Type != "":
		mapping, ok := g.natives[spec.Type]
		if !ok {
			return fmt.Errorf("unsupported buffer count type %s", spec.Type)
		}
		if err := writeSizeCall(out, indent, mapping.SizeFn+"("+mapping.GoType+"(len("+value+")))"); err != nil {
			return err
		}
		if err := writeSizeCall(out, indent, "wire.SizeRawBytes("+value+")"); err != nil {
			return err
		}
	case spec.Field != "":
		if err := writeSizeCall(out, indent, "wire.SizeRawBytes("+value+")"); err != nil {
			return err
		}
	case spec.Fixed != nil:
		g.addImports("fmt")
		if err := writeRawLine(out, indent, "if len("+value+") != "+strconv.Itoa(*spec.Fixed)+" { return 0, fmt.Errorf(\"buffer length mismatch: expected "+strconv.Itoa(*spec.Fixed)+", got %d\", len("+value+")) }"); err != nil {
			return err
		}
		if err := writeSizeCall(out, indent, "wire.SizeRawBytes("+value+")"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("buffer requires rest, count, or countType")
	}
	return nil
}

func (g *Generator) writeBufferAppend(out *bytes.Buffer, call *schemair.Call, value string, indent string, owner string, ctx string) error {
	spec := callCountSpec(call)
	switch {
	case spec.Rest:
		if err := writeAppendCall(out, indent, "wire.AppendRestBuffer(dst, "+value+")"); err != nil {
			return err
		}
	case spec.Type != "":
		mapping, ok := g.natives[spec.Type]
		if !ok {
			return fmt.Errorf("unsupported buffer count type %s", spec.Type)
		}
		if err := writeAppendCall(out, indent, mapping.AppendFn+"(dst, "+mapping.GoType+"(len("+value+")))"); err != nil {
			return err
		}
		if err := writeAppendCall(out, indent, "wire.AppendRawBytes(dst, "+value+")"); err != nil {
			return err
		}
	case spec.Field != "":
		if err := writeAppendCall(out, indent, "wire.AppendRawBytes(dst, "+value+")"); err != nil {
			return err
		}
	case spec.Fixed != nil:
		g.addImports("fmt")
		if err := writeRawLine(out, indent, "if len("+value+") != "+strconv.Itoa(*spec.Fixed)+" { return nil, fmt.Errorf(\"buffer length mismatch: expected "+strconv.Itoa(*spec.Fixed)+", got %d\", len("+value+")) }"); err != nil {
			return err
		}
		if err := writeAppendCall(out, indent, "wire.AppendRawBytes(dst, "+value+")"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("buffer requires rest, count, or countType")
	}
	return nil
}

func (g *Generator) writeBufferDecode(out *bytes.Buffer, call *schemair.Call, value string, indent string, owner string, ctx string) error {
	spec := callCountSpec(call)
	switch {
	case spec.Rest:
		if err := writeDecodeCall(out, indent, "wire.DecodeRestBuffer(src, &"+value+")"); err != nil {
			return err
		}
	case spec.Type != "":
		mapping, ok := g.natives[spec.Type]
		if !ok {
			return fmt.Errorf("unsupported buffer count type %s", spec.Type)
		}
		countVar := strings.ReplaceAll(indent, "\t", "") + strcase.ToLowerCamel(ctx) + "BufferLen"
		if err := writeRawLine(out, indent, "var "+countVar+" "+mapping.GoType); err != nil {
			return err
		}
		if err := writeDecodeCall(out, indent, mapping.DecodeFn+"(src, &"+countVar+")"); err != nil {
			return err
		}
		if err := writeDecodeCall(out, indent, "wire.DecodeFixedBytes(src, &"+value+", int("+countVar+"))"); err != nil {
			return err
		}
	case spec.Field != "":
		if err := writeDecodeCall(out, indent, "wire.DecodeFixedBytes(src, &"+value+", int("+fieldPathExpr(owner, spec.Field)+"))"); err != nil {
			return err
		}
	case spec.Fixed != nil:
		if err := writeDecodeCall(out, indent, "wire.DecodeFixedBytes(src, &"+value+", "+strconv.Itoa(*spec.Fixed)+")"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("buffer requires rest, count, or countType")
	}
	return nil
}

func (g *Generator) writePstringSize(out *bytes.Buffer, call *schemair.Call, value string, indent string, owner string) error {
	spec := callCountSpec(call)
	encoding := stringCallEncoding(call)
	switch {
	case spec.Type != "":
		mapping, ok := g.natives[spec.Type]
		if !ok {
			return fmt.Errorf("unsupported pstring count type %s", spec.Type)
		}
		if err := writeRawLine(out, indent, `stringLen, err := wire.SizeStringEncoded(`+value+`, `+strconv.Quote(encoding)+`)`); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "if err != nil { return 0, err }"); err != nil {
			return err
		}
		if err := writeSizeCall(out, indent, mapping.SizeFn+"("+mapping.GoType+"(stringLen))"); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "n += stringLen"); err != nil {
			return err
		}
	case spec.Field != "":
		g.addImports("fmt")
		if err := writeRawLine(out, indent, `stringLen, err := wire.SizeStringEncoded(`+value+`, `+strconv.Quote(encoding)+`)`); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "if err != nil { return 0, err }"); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "if stringLen != int("+fieldPathExpr(owner, spec.Field)+`) { return 0, fmt.Errorf("string length mismatch: expected %d, got %d", int(`+fieldPathExpr(owner, spec.Field)+`), stringLen) }`); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "n += stringLen"); err != nil {
			return err
		}
	case spec.Fixed != nil:
		g.addImports("fmt")
		if err := writeRawLine(out, indent, `stringLen, err := wire.SizeStringEncoded(`+value+`, `+strconv.Quote(encoding)+`)`); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "if err != nil { return 0, err }"); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "if stringLen != "+strconv.Itoa(*spec.Fixed)+` { return 0, fmt.Errorf("string length mismatch: expected `+strconv.Itoa(*spec.Fixed)+`, got %d", stringLen) }`); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "n += stringLen"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("pstring requires count or countType")
	}
	return nil
}

func (g *Generator) writePstringAppend(out *bytes.Buffer, call *schemair.Call, value string, indent string, owner string) error {
	spec := callCountSpec(call)
	encoding := stringCallEncoding(call)
	switch {
	case spec.Type != "":
		mapping, ok := g.natives[spec.Type]
		if !ok {
			return fmt.Errorf("unsupported pstring count type %s", spec.Type)
		}
		if err := writeRawLine(out, indent, `stringLen, err := wire.SizeStringEncoded(`+value+`, `+strconv.Quote(encoding)+`)`); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "if err != nil { return nil, err }"); err != nil {
			return err
		}
		if err := writeAppendCall(out, indent, mapping.AppendFn+"(dst, "+mapping.GoType+"(stringLen))"); err != nil {
			return err
		}
		if err := writeAppendCall(out, indent, `wire.AppendStringEncoded(dst, `+value+`, `+strconv.Quote(encoding)+`)`); err != nil {
			return err
		}
	case spec.Field != "":
		g.addImports("fmt")
		if err := writeRawLine(out, indent, `stringLen, err := wire.SizeStringEncoded(`+value+`, `+strconv.Quote(encoding)+`)`); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "if err != nil { return nil, err }"); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "if stringLen != int("+fieldPathExpr(owner, spec.Field)+`) { return nil, fmt.Errorf("string length mismatch: expected %d, got %d", int(`+fieldPathExpr(owner, spec.Field)+`), stringLen) }`); err != nil {
			return err
		}
		if err := writeAppendCall(out, indent, `wire.AppendStringEncoded(dst, `+value+`, `+strconv.Quote(encoding)+`)`); err != nil {
			return err
		}
	case spec.Fixed != nil:
		g.addImports("fmt")
		if err := writeRawLine(out, indent, `stringLen, err := wire.SizeStringEncoded(`+value+`, `+strconv.Quote(encoding)+`)`); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "if err != nil { return nil, err }"); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "if stringLen != "+strconv.Itoa(*spec.Fixed)+` { return nil, fmt.Errorf("string length mismatch: expected `+strconv.Itoa(*spec.Fixed)+`, got %d", stringLen) }`); err != nil {
			return err
		}
		if err := writeAppendCall(out, indent, `wire.AppendStringEncoded(dst, `+value+`, `+strconv.Quote(encoding)+`)`); err != nil {
			return err
		}
	default:
		return fmt.Errorf("pstring requires count or countType")
	}
	return nil
}

func (g *Generator) writePstringDecode(out *bytes.Buffer, call *schemair.Call, value string, indent string, owner string) error {
	spec := callCountSpec(call)
	encoding := stringCallEncoding(call)
	switch {
	case spec.Type != "":
		mapping, ok := g.natives[spec.Type]
		if !ok {
			return fmt.Errorf("unsupported pstring count type %s", spec.Type)
		}
		countVar := strcase.ToLowerCamel(owner) + "StringLen"
		if err := writeRawLine(out, indent, "var "+countVar+" "+mapping.GoType); err != nil {
			return err
		}
		if err := writeDecodeCall(out, indent, mapping.DecodeFn+"(src, &"+countVar+")"); err != nil {
			return err
		}
		if err := writeDecodeCall(out, indent, `wire.DecodeFixedString(src, &`+value+`, int(`+countVar+`), `+strconv.Quote(encoding)+`)`); err != nil {
			return err
		}
	case spec.Field != "":
		if err := writeDecodeCall(out, indent, `wire.DecodeFixedString(src, &`+value+`, int(`+fieldPathExpr(owner, spec.Field)+`), `+strconv.Quote(encoding)+`)`); err != nil {
			return err
		}
	case spec.Fixed != nil:
		if err := writeDecodeCall(out, indent, `wire.DecodeFixedString(src, &`+value+`, `+strconv.Itoa(*spec.Fixed)+`, `+strconv.Quote(encoding)+`)`); err != nil {
			return err
		}
	default:
		return fmt.Errorf("pstring requires count or countType")
	}
	return nil
}

func exprHasExternalCompare(expr schemair.Expr) bool {
	switch v := expr.(type) {
	case *schemair.Option:
		return exprHasExternalCompare(v.Inner)
	case *schemair.Array:
		return exprHasExternalCompare(v.Elem)
	case *schemair.Container:
		for _, field := range v.Fields {
			if exprHasExternalCompare(field.Type) {
				return true
			}
		}
		return false
	case *schemair.Switch:
		if strings.HasPrefix(v.CompareTo, "../") || strings.HasPrefix(v.CompareTo, "/") {
			return true
		}
		for _, kase := range v.Cases {
			if exprHasExternalCompare(kase.Expr) {
				return true
			}
		}
		return v.Default != nil && exprHasExternalCompare(v.Default)
	case *schemair.RegistryHolder:
		return exprHasExternalCompare(v.OtherwiseType)
	case *schemair.RegistryHolderSet:
		return exprHasExternalCompare(v.BaseType) || exprHasExternalCompare(v.OtherwiseType)
	case *schemair.EntityMetadataLoop:
		return exprHasExternalCompare(v.Elem)
	case *schemair.TopBitSetTerminatedArray:
		return exprHasExternalCompare(v.Elem)
	default:
		return false
	}
}
