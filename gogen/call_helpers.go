package gogen

import (
	"bytes"
	"fmt"
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
		writeFormat(out, "%snn, err = wire.SizeRestBuffer(%s)\n%sn += nn\n%sif err != nil { return 0, err }\n", indent, value, indent, indent)
	case spec.Type != "":
		mapping, ok := g.natives[spec.Type]
		if !ok {
			return fmt.Errorf("unsupported buffer count type %s", spec.Type)
		}
		writeFormat(out, "%snn, err = %s(%s(len(%s)))\n%sn += nn\n%sif err != nil { return 0, err }\n", indent, mapping.SizeFn, mapping.GoType, value, indent, indent)
		writeFormat(out, "%snn, err = wire.SizeRawBytes(%s)\n%sn += nn\n%sif err != nil { return 0, err }\n", indent, value, indent, indent)
	case spec.Field != "":
		writeFormat(out, "%snn, err = wire.SizeRawBytes(%s)\n%sn += nn\n%sif err != nil { return 0, err }\n", indent, value, indent, indent)
	case spec.Fixed != nil:
		g.addImports("fmt")
		writeFormat(out, "%sif len(%s) != %d { return 0, fmt.Errorf(\"buffer length mismatch: expected %d, got %%d\", len(%s)) }\n", indent, value, *spec.Fixed, *spec.Fixed, value)
		writeFormat(out, "%snn, err = wire.SizeRawBytes(%s)\n%sn += nn\n%sif err != nil { return 0, err }\n", indent, value, indent, indent)
	default:
		return fmt.Errorf("buffer requires rest, count, or countType")
	}
	return nil
}

func (g *Generator) writeBufferAppend(out *bytes.Buffer, call *schemair.Call, value string, indent string, owner string, ctx string) error {
	spec := callCountSpec(call)
	switch {
	case spec.Rest:
		writeFormat(out, "%sdst, err = wire.AppendRestBuffer(dst, %s)\n%sif err != nil { return nil, err }\n", indent, value, indent)
	case spec.Type != "":
		mapping, ok := g.natives[spec.Type]
		if !ok {
			return fmt.Errorf("unsupported buffer count type %s", spec.Type)
		}
		writeFormat(out, "%sdst, err = %s(dst, %s(len(%s)))\n%sif err != nil { return nil, err }\n", indent, mapping.AppendFn, mapping.GoType, value, indent)
		writeFormat(out, "%sdst, err = wire.AppendRawBytes(dst, %s)\n%sif err != nil { return nil, err }\n", indent, value, indent)
	case spec.Field != "":
		writeFormat(out, "%sdst, err = wire.AppendRawBytes(dst, %s)\n%sif err != nil { return nil, err }\n", indent, value, indent)
	case spec.Fixed != nil:
		g.addImports("fmt")
		writeFormat(out, "%sif len(%s) != %d { return nil, fmt.Errorf(\"buffer length mismatch: expected %d, got %%d\", len(%s)) }\n", indent, value, *spec.Fixed, *spec.Fixed, value)
		writeFormat(out, "%sdst, err = wire.AppendRawBytes(dst, %s)\n%sif err != nil { return nil, err }\n", indent, value, indent)
	default:
		return fmt.Errorf("buffer requires rest, count, or countType")
	}
	return nil
}

func (g *Generator) writeBufferDecode(out *bytes.Buffer, call *schemair.Call, value string, indent string, owner string, ctx string) error {
	spec := callCountSpec(call)
	switch {
	case spec.Rest:
		writeFormat(out, "%ssrc, err = wire.DecodeRestBuffer(src, &%s)\n%sif err != nil { return nil, err }\n", indent, value, indent)
	case spec.Type != "":
		mapping, ok := g.natives[spec.Type]
		if !ok {
			return fmt.Errorf("unsupported buffer count type %s", spec.Type)
		}
		countVar := strings.ReplaceAll(indent, "\t", "") + strcase.ToLowerCamel(ctx) + "BufferLen"
		writeFormat(out, "%svar %s %s\n", indent, countVar, mapping.GoType)
		writeFormat(out, "%ssrc, err = %s(src, &%s)\n%sif err != nil { return nil, err }\n", indent, mapping.DecodeFn, countVar, indent)
		writeFormat(out, "%ssrc, err = wire.DecodeFixedBytes(src, &%s, int(%s))\n%sif err != nil { return nil, err }\n", indent, value, countVar, indent)
	case spec.Field != "":
		writeFormat(out, "%ssrc, err = wire.DecodeFixedBytes(src, &%s, int(%s))\n%sif err != nil { return nil, err }\n", indent, value, fieldPathExpr(owner, spec.Field), indent)
	case spec.Fixed != nil:
		writeFormat(out, "%ssrc, err = wire.DecodeFixedBytes(src, &%s, %d)\n%sif err != nil { return nil, err }\n", indent, value, *spec.Fixed, indent)
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
		writeFormat(out, "%sstringLen, err := wire.SizeStringEncoded(%s, %q)\n%sif err != nil { return 0, err }\n", indent, value, encoding, indent)
		writeFormat(out, "%snn, err = %s(%s(stringLen))\n%sn += nn\n%sif err != nil { return 0, err }\n", indent, mapping.SizeFn, mapping.GoType, indent, indent)
		writeFormat(out, "%sn += stringLen\n", indent)
	case spec.Field != "":
		g.addImports("fmt")
		writeFormat(out, "%sstringLen, err := wire.SizeStringEncoded(%s, %q)\n%sif err != nil { return 0, err }\n", indent, value, encoding, indent)
		writeFormat(out, "%sif stringLen != int(%s) { return 0, fmt.Errorf(\"string length mismatch: expected %%d, got %%d\", int(%s), stringLen) }\n", indent, fieldPathExpr(owner, spec.Field), fieldPathExpr(owner, spec.Field))
		writeFormat(out, "%sn += stringLen\n", indent)
	case spec.Fixed != nil:
		g.addImports("fmt")
		writeFormat(out, "%sstringLen, err := wire.SizeStringEncoded(%s, %q)\n%sif err != nil { return 0, err }\n", indent, value, encoding, indent)
		writeFormat(out, "%sif stringLen != %d { return 0, fmt.Errorf(\"string length mismatch: expected %d, got %%d\", stringLen) }\n", indent, *spec.Fixed, *spec.Fixed)
		writeFormat(out, "%sn += stringLen\n", indent)
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
		writeFormat(out, "%sstringLen, err := wire.SizeStringEncoded(%s, %q)\n%sif err != nil { return nil, err }\n", indent, value, encoding, indent)
		writeFormat(out, "%sdst, err = %s(dst, %s(stringLen))\n%sif err != nil { return nil, err }\n", indent, mapping.AppendFn, mapping.GoType, indent)
		writeFormat(out, "%sdst, err = wire.AppendStringEncoded(dst, %s, %q)\n%sif err != nil { return nil, err }\n", indent, value, encoding, indent)
	case spec.Field != "":
		g.addImports("fmt")
		writeFormat(out, "%sstringLen, err := wire.SizeStringEncoded(%s, %q)\n%sif err != nil { return nil, err }\n", indent, value, encoding, indent)
		writeFormat(out, "%sif stringLen != int(%s) { return nil, fmt.Errorf(\"string length mismatch: expected %%d, got %%d\", int(%s), stringLen) }\n", indent, fieldPathExpr(owner, spec.Field), fieldPathExpr(owner, spec.Field))
		writeFormat(out, "%sdst, err = wire.AppendStringEncoded(dst, %s, %q)\n%sif err != nil { return nil, err }\n", indent, value, encoding, indent)
	case spec.Fixed != nil:
		g.addImports("fmt")
		writeFormat(out, "%sstringLen, err := wire.SizeStringEncoded(%s, %q)\n%sif err != nil { return nil, err }\n", indent, value, encoding, indent)
		writeFormat(out, "%sif stringLen != %d { return nil, fmt.Errorf(\"string length mismatch: expected %d, got %%d\", stringLen) }\n", indent, *spec.Fixed, *spec.Fixed)
		writeFormat(out, "%sdst, err = wire.AppendStringEncoded(dst, %s, %q)\n%sif err != nil { return nil, err }\n", indent, value, encoding, indent)
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
		writeFormat(out, "%svar %s %s\n", indent, countVar, mapping.GoType)
		writeFormat(out, "%ssrc, err = %s(src, &%s)\n%sif err != nil { return nil, err }\n", indent, mapping.DecodeFn, countVar, indent)
		writeFormat(out, "%ssrc, err = wire.DecodeFixedString(src, &%s, int(%s), %q)\n%sif err != nil { return nil, err }\n", indent, value, countVar, encoding, indent)
	case spec.Field != "":
		writeFormat(out, "%ssrc, err = wire.DecodeFixedString(src, &%s, int(%s), %q)\n%sif err != nil { return nil, err }\n", indent, value, fieldPathExpr(owner, spec.Field), encoding, indent)
	case spec.Fixed != nil:
		writeFormat(out, "%ssrc, err = wire.DecodeFixedString(src, &%s, %d, %q)\n%sif err != nil { return nil, err }\n", indent, value, *spec.Fixed, encoding, indent)
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
