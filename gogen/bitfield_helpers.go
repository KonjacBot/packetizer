package gogen

import (
	"fmt"
	"strings"

	"github.com/iancoleman/strcase"

	"github.com/KonjacBot/packetizer/schemair"
)

func bitfieldGoType(field schemair.BitfieldField) string {
	if field.Signed {
		if field.Size <= 32 {
			return "int32"
		}
		return "int64"
	}
	if field.Size <= 32 {
		return "uint32"
	}
	return "uint64"
}

func bitfieldCastType(field schemair.BitfieldField) string {
	if field.Signed {
		return "int64"
	}
	return "uint64"
}

func bitfieldTotalBits(expr *schemair.Bitfield) int {
	total := 0
	for _, field := range expr.Fields {
		total += field.Size
	}
	return total
}

func bitfieldByteSize(expr *schemair.Bitfield) int {
	total := bitfieldTotalBits(expr)
	if total%8 == 0 {
		return total / 8
	}
	return total/8 + 1
}

func bitfieldMask(size int) uint64 {
	if size >= 64 {
		return ^uint64(0)
	}
	return (uint64(1) << size) - 1
}

func switchCaseName(ctx string, kase schemair.SwitchCase, i int) string {
	if i < 0 {
		return switchDefaultName(ctx)
	}
	if len(kase.Labels) == 1 {
		label := sanitizeValueName(kase.Labels[0])
		if label != "" && label != "True" && label != "False" {
			return ctx + label
		}
	}
	return ctx + fmt.Sprintf("Case%d", i)
}

func switchDefaultName(ctx string) string {
	return ctx + "Default"
}

func derefExpr(value string) string {
	return "(*" + value + ")"
}

func (g *Generator) exprTypeName(target schemair.Expr) string {
	for name, def := range g.types {
		if def.Expr == target {
			return name
		}
	}
	for _, def := range g.synthetic {
		if def.Expr == target {
			return def.Name
		}
	}
	return ""
}

func (g *Generator) mapperConstName(typeName string, mapper *schemair.Mapper, label string) string {
	label = g.canonicalLabel(label)
	for _, entry := range mapper.Entries {
		if g.canonicalLabel(entry.Value) == label {
			name := fmt.Sprintf("%s%s", typeName, sanitizeValueName(entry.Value))
			if alias, ok := g.addExternalRef(typeName); ok {
				return alias + "." + name
			}
			return name
		}
	}
	name := fmt.Sprintf("%s%s", typeName, sanitizeValueName(label))
	if alias, ok := g.addExternalRef(typeName); ok {
		return alias + "." + name
	}
	return name
}

func (g *Generator) canonicalLabel(label string) string {
	if override, ok := g.labelOverrides[label]; ok && override != "" {
		label = override
	}
	switch label {
	case "byte":
		return "i8"
	case "short":
		return "i16"
	case "long":
		return "i64"
	case "float":
		return "f32"
	case "double":
		return "f64"
	default:
		return label
	}
}

func (g *Generator) bitflagsGoType(call *schemair.Call) (string, error) {
	mapping, err := g.bitflagsMapping(call)
	if err != nil {
		return "", err
	}
	return mapping.GoType, nil
}

func (g *Generator) bitflagsMapping(call *schemair.Call) (NativeMapping, error) {
	rawType, ok := call.Options["type"].(string)
	if !ok || rawType == "" {
		return NativeMapping{}, fmt.Errorf("bitflags missing underlying type")
	}
	name := strings.TrimSpace(rawType)
	name = strcase.ToCamel(name)
	mapping, ok := g.natives[name]
	if !ok {
		return NativeMapping{}, fmt.Errorf("unsupported bitflags underlying type %s", rawType)
	}
	return mapping, nil
}
