package gogen

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/iancoleman/strcase"

	"github.com/KonjacBot/packetizer/schemair"
)

func writeRawLine(out *bytes.Buffer, indent string, code string) error {
	return executeTemplate(out, "rawLine", templateData{
		"Indent": indent,
		"Code":   code,
	})
}

func sanitizeValueName(name string) string {
	if n, err := strconv.Atoi(name); err == nil {
		return "Value" + strconv.Itoa(n)
	}
	return strcase.ToCamel(strings.NewReplacer("/", " ", ":", " ", ".", " ", "-", " ", "?", " ").Replace(name))
}

func sanitizeFieldGoName(name string) string {
	name = strings.TrimPrefix(name, "$")
	if name == "_" {
		return "Variant"
	}
	result := strcase.ToCamel(strings.NewReplacer(":", " ", ".", " ", "-", " ", "?", " ").Replace(name))
	switch result {
	case "Size", "Append", "Decode":
		return result + "Field"
	default:
		return result
	}
}

func (g *Generator) writeSwitchSize(out *bytes.Buffer, container *schemair.Container, parentContainer *schemair.Container, expr *schemair.Switch, value string, indent string, ctx string, owner string, parentOwner string, rootContainer *schemair.Container, rootOwner string) error {
	fieldExpr := g.switchFieldExpr(container, parentContainer, rootContainer, expr)
	compareExpr, err := g.switchCompareExpr(container, parentContainer, rootContainer, expr, owner, parentOwner, rootOwner)
	if err != nil {
		return err
	}
	if _, ok := fieldExpr.(*schemair.Option); ok {
		if err := writeRawLine(out, indent, "if "+compareExpr+" != nil {"); err != nil {
			return err
		}
		if err := writeRawLine(out, indent+"\t", "switch *"+compareExpr+" {"); err != nil {
			return err
		}
	} else {
		if err := writeRawLine(out, indent, "switch "+compareExpr+" {"); err != nil {
			return err
		}
	}
	seenLabels := map[string]struct{}{}
	for i, kase := range expr.Cases {
		if err := g.writeSwitchCaseSize(out, container, parentContainer, expr, kase, value, indent, ctx, owner, parentOwner, rootContainer, rootOwner, seenLabels, i); err != nil {
			return err
		}
	}
	if _, ok := fieldExpr.(*schemair.Option); ok {
		if err := writeRawLine(out, indent+"\t", "default:"); err != nil {
			return err
		}
		if err := g.writeSwitchDefaultSize(out, container, parentContainer, expr, value, indent+"\t\t", ctx, owner, parentOwner, rootContainer, rootOwner); err != nil {
			return err
		}
		if err := writeRawLine(out, indent+"\t", "}"); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "} else {"); err != nil {
			return err
		}
		if err := g.writeSwitchDefaultSize(out, container, parentContainer, expr, value, indent+"\t", ctx, owner, parentOwner, rootContainer, rootOwner); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "}"); err != nil {
			return err
		}
		return nil
	}
	if err := writeRawLine(out, indent, "default:"); err != nil {
		return err
	}
	if err := g.writeSwitchDefaultSize(out, container, parentContainer, expr, value, indent+"\t", ctx, owner, parentOwner, rootContainer, rootOwner); err != nil {
		return err
	}
	if err := writeRawLine(out, indent, "}"); err != nil {
		return err
	}
	return nil
}

func (g *Generator) writeSwitchAppend(out *bytes.Buffer, container *schemair.Container, parentContainer *schemair.Container, expr *schemair.Switch, value string, indent string, ctx string, owner string, parentOwner string, rootContainer *schemair.Container, rootOwner string) error {
	fieldExpr := g.switchFieldExpr(container, parentContainer, rootContainer, expr)
	compareExpr, err := g.switchCompareExpr(container, parentContainer, rootContainer, expr, owner, parentOwner, rootOwner)
	if err != nil {
		return err
	}
	if _, ok := fieldExpr.(*schemair.Option); ok {
		if err := writeRawLine(out, indent, "if "+compareExpr+" != nil {"); err != nil {
			return err
		}
		if err := writeRawLine(out, indent+"\t", "switch *"+compareExpr+" {"); err != nil {
			return err
		}
	} else {
		if err := writeRawLine(out, indent, "switch "+compareExpr+" {"); err != nil {
			return err
		}
	}
	seenLabels := map[string]struct{}{}
	for i, kase := range expr.Cases {
		if err := g.writeSwitchCaseAppend(out, container, parentContainer, expr, kase, value, indent, ctx, owner, parentOwner, rootContainer, rootOwner, seenLabels, i); err != nil {
			return err
		}
	}
	if _, ok := fieldExpr.(*schemair.Option); ok {
		if err := writeRawLine(out, indent+"\t", "default:"); err != nil {
			return err
		}
		if err := g.writeSwitchDefaultAppend(out, container, parentContainer, expr, value, indent+"\t\t", ctx, owner, parentOwner, rootContainer, rootOwner); err != nil {
			return err
		}
		if err := writeRawLine(out, indent+"\t", "}"); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "} else {"); err != nil {
			return err
		}
		if err := g.writeSwitchDefaultAppend(out, container, parentContainer, expr, value, indent+"\t", ctx, owner, parentOwner, rootContainer, rootOwner); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "}"); err != nil {
			return err
		}
		return nil
	}
	if err := writeRawLine(out, indent, "default:"); err != nil {
		return err
	}
	if err := g.writeSwitchDefaultAppend(out, container, parentContainer, expr, value, indent+"\t", ctx, owner, parentOwner, rootContainer, rootOwner); err != nil {
		return err
	}
	if err := writeRawLine(out, indent, "}"); err != nil {
		return err
	}
	return nil
}

func (g *Generator) writeSwitchDecode(out *bytes.Buffer, container *schemair.Container, parentContainer *schemair.Container, expr *schemair.Switch, value string, indent string, ctx string, owner string, parentOwner string, rootContainer *schemair.Container, rootOwner string) error {
	fieldExpr := g.switchFieldExpr(container, parentContainer, rootContainer, expr)
	compareExpr, err := g.switchCompareExpr(container, parentContainer, rootContainer, expr, owner, parentOwner, rootOwner)
	if err != nil {
		return err
	}
	if _, ok := fieldExpr.(*schemair.Option); ok {
		if err := writeRawLine(out, indent, "if "+compareExpr+" != nil {"); err != nil {
			return err
		}
		if err := writeRawLine(out, indent+"\t", "switch *"+compareExpr+" {"); err != nil {
			return err
		}
	} else {
		if err := writeRawLine(out, indent, "switch "+compareExpr+" {"); err != nil {
			return err
		}
	}
	seenLabels := map[string]struct{}{}
	for i, kase := range expr.Cases {
		if err := g.writeSwitchCaseDecode(out, container, parentContainer, expr, kase, value, indent, ctx, owner, parentOwner, rootContainer, rootOwner, seenLabels, i); err != nil {
			return err
		}
	}
	if _, ok := fieldExpr.(*schemair.Option); ok {
		if err := writeRawLine(out, indent+"\t", "default:"); err != nil {
			return err
		}
		if err := g.writeSwitchDefaultDecode(out, container, parentContainer, expr, value, indent+"\t\t", ctx, owner, parentOwner, rootContainer, rootOwner); err != nil {
			return err
		}
		if err := writeRawLine(out, indent+"\t", "}"); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "} else {"); err != nil {
			return err
		}
		if err := g.writeSwitchDefaultDecode(out, container, parentContainer, expr, value, indent+"\t", ctx, owner, parentOwner, rootContainer, rootOwner); err != nil {
			return err
		}
		if err := writeRawLine(out, indent, "}"); err != nil {
			return err
		}
		return nil
	}
	if err := writeRawLine(out, indent, "default:"); err != nil {
		return err
	}
	if err := g.writeSwitchDefaultDecode(out, container, parentContainer, expr, value, indent+"\t", ctx, owner, parentOwner, rootContainer, rootOwner); err != nil {
		return err
	}
	if err := writeRawLine(out, indent, "}"); err != nil {
		return err
	}
	return nil
}

func (g *Generator) writeSwitchCaseSize(out *bytes.Buffer, container *schemair.Container, parentContainer *schemair.Container, expr *schemair.Switch, kase schemair.SwitchCase, value string, indent string, ctx string, owner string, parentOwner string, rootContainer *schemair.Container, rootOwner string, seenLabels map[string]struct{}, i int) error {
	labels, err := g.switchLabels(container, parentContainer, rootContainer, expr, kase)
	if err != nil {
		return err
	}
	labels = filterNewLabels(labels, seenLabels)
	if len(labels) == 0 {
		return nil
	}
	if err := writeRawLine(out, indent, "case "+strings.Join(labels, ", ")+":"); err != nil {
		return err
	}
	return g.writeSwitchCaseBodySize(out, container, parentContainer, kase.Expr, value, indent+"\t", ctx, owner, parentOwner, rootContainer, rootOwner, kase, i)
}

func (g *Generator) writeSwitchCaseAppend(out *bytes.Buffer, container *schemair.Container, parentContainer *schemair.Container, expr *schemair.Switch, kase schemair.SwitchCase, value string, indent string, ctx string, owner string, parentOwner string, rootContainer *schemair.Container, rootOwner string, seenLabels map[string]struct{}, i int) error {
	labels, err := g.switchLabels(container, parentContainer, rootContainer, expr, kase)
	if err != nil {
		return err
	}
	labels = filterNewLabels(labels, seenLabels)
	if len(labels) == 0 {
		return nil
	}
	if err := writeRawLine(out, indent, "case "+strings.Join(labels, ", ")+":"); err != nil {
		return err
	}
	return g.writeSwitchCaseBodyAppend(out, container, parentContainer, kase.Expr, value, indent+"\t", ctx, owner, parentOwner, rootContainer, rootOwner, kase, i)
}

func (g *Generator) writeSwitchCaseDecode(out *bytes.Buffer, container *schemair.Container, parentContainer *schemair.Container, expr *schemair.Switch, kase schemair.SwitchCase, value string, indent string, ctx string, owner string, parentOwner string, rootContainer *schemair.Container, rootOwner string, seenLabels map[string]struct{}, i int) error {
	labels, err := g.switchLabels(container, parentContainer, rootContainer, expr, kase)
	if err != nil {
		return err
	}
	labels = filterNewLabels(labels, seenLabels)
	if len(labels) == 0 {
		return nil
	}
	if err := writeRawLine(out, indent, "case "+strings.Join(labels, ", ")+":"); err != nil {
		return err
	}
	return g.writeSwitchCaseBodyDecode(out, container, parentContainer, kase.Expr, value, indent+"\t", ctx, owner, parentOwner, rootContainer, rootOwner, kase, i)
}

func (g *Generator) writeSwitchDefaultSize(out *bytes.Buffer, container *schemair.Container, parentContainer *schemair.Container, expr *schemair.Switch, value string, indent string, ctx string, owner string, parentOwner string, rootContainer *schemair.Container, rootOwner string) error {
	return g.writeSwitchCaseBodySize(out, container, parentContainer, expr.Default, value, indent, ctx, owner, parentOwner, rootContainer, rootOwner, schemair.SwitchCase{}, -1)
}

func (g *Generator) writeSwitchDefaultAppend(out *bytes.Buffer, container *schemair.Container, parentContainer *schemair.Container, expr *schemair.Switch, value string, indent string, ctx string, owner string, parentOwner string, rootContainer *schemair.Container, rootOwner string) error {
	return g.writeSwitchCaseBodyAppend(out, container, parentContainer, expr.Default, value, indent, ctx, owner, parentOwner, rootContainer, rootOwner, schemair.SwitchCase{}, -1)
}

func (g *Generator) writeSwitchDefaultDecode(out *bytes.Buffer, container *schemair.Container, parentContainer *schemair.Container, expr *schemair.Switch, value string, indent string, ctx string, owner string, parentOwner string, rootContainer *schemair.Container, rootOwner string) error {
	return g.writeSwitchCaseBodyDecode(out, container, parentContainer, expr.Default, value, indent, ctx, owner, parentOwner, rootContainer, rootOwner, schemair.SwitchCase{}, -1)
}

func (g *Generator) writeSwitchCaseBodySize(out *bytes.Buffer, container *schemair.Container, parentContainer *schemair.Container, expr schemair.Expr, value string, indent string, ctx string, owner string, parentOwner string, rootContainer *schemair.Container, rootOwner string, kase schemair.SwitchCase, i int) error {
	if _, ok := expr.(*schemair.Void); ok {
		return nil
	}
	g.addImports("fmt")
	caseName := switchCaseName(ctx, kase, i)
	typeName, err := g.goType(ctx, expr, caseName)
	if err != nil {
		return err
	}
	temp := strcase.ToLowerCamel(ctx) + "CaseValue"
	if err := writeRawLine(out, indent, temp+", ok := "+value+".("+typeName+")"); err != nil {
		return err
	}
	if err := writeRawLine(out, indent, `if !ok { return 0, fmt.Errorf("field %s expected `+typeName+` in switch case", `+strconv.Quote(value)+`) }`); err != nil {
		return err
	}
	return g.writeSizeExpr(out, container, parentContainer, expr, temp, indent, caseName, owner, parentOwner, rootContainer, rootOwner)
}

func (g *Generator) writeSwitchCaseBodyAppend(out *bytes.Buffer, container *schemair.Container, parentContainer *schemair.Container, expr schemair.Expr, value string, indent string, ctx string, owner string, parentOwner string, rootContainer *schemair.Container, rootOwner string, kase schemair.SwitchCase, i int) error {
	if _, ok := expr.(*schemair.Void); ok {
		return nil
	}
	g.addImports("fmt")
	caseName := switchCaseName(ctx, kase, i)
	typeName, err := g.goType(ctx, expr, caseName)
	if err != nil {
		return err
	}
	temp := strcase.ToLowerCamel(ctx) + "CaseValue"
	if err := writeRawLine(out, indent, temp+", ok := "+value+".("+typeName+")"); err != nil {
		return err
	}
	if err := writeRawLine(out, indent, `if !ok { return nil, fmt.Errorf("field %s expected `+typeName+` in switch case", `+strconv.Quote(value)+`) }`); err != nil {
		return err
	}
	return g.writeAppendExpr(out, container, parentContainer, expr, temp, indent, caseName, owner, parentOwner, rootContainer, rootOwner)
}

func (g *Generator) writeSwitchCaseBodyDecode(out *bytes.Buffer, container *schemair.Container, parentContainer *schemair.Container, expr schemair.Expr, value string, indent string, ctx string, owner string, parentOwner string, rootContainer *schemair.Container, rootOwner string, kase schemair.SwitchCase, i int) error {
	if _, ok := expr.(*schemair.Void); ok {
		if err := writeRawLine(out, indent, value+" = nil"); err != nil {
			return err
		}
		return nil
	}
	caseName := switchCaseName(ctx, kase, i)
	typeName, err := g.goType(ctx, expr, caseName)
	if err != nil {
		return err
	}
	temp := strcase.ToLowerCamel(ctx) + "CaseValue"
	if err := writeRawLine(out, indent, "var "+temp+" "+typeName); err != nil {
		return err
	}
	if err := g.writeDecodeExpr(out, container, parentContainer, expr, temp, indent, caseName, owner, parentOwner, rootContainer, rootOwner); err != nil {
		return err
	}
	if err := writeRawLine(out, indent, value+" = "+temp); err != nil {
		return err
	}
	return nil
}

func (g *Generator) switchCompareExpr(container *schemair.Container, parentContainer *schemair.Container, rootContainer *schemair.Container, expr *schemair.Switch, owner string, parentOwner string, rootOwner string) (string, error) {
	if expr.CompareTo == "" {
		return switchValueExpr(expr.CompareValue), nil
	}
	path := expr.CompareTo
	switch {
	case strings.HasPrefix(path, "../"):
		if parentOwner == "" {
			return "", fmt.Errorf("relative compareTo without parent scope: %s", expr.CompareTo)
		}
		return fieldPathExpr(parentOwner, strings.TrimPrefix(path, "../")), nil
	case strings.HasPrefix(path, "/"):
		if rootOwner == "" {
			return "", fmt.Errorf("root compareTo without root scope: %s", expr.CompareTo)
		}
		return fieldPathExpr(rootOwner, strings.TrimPrefix(path, "/")), nil
	default:
		switch {
		case g.findFieldExpr(container, path) != nil:
			return fieldPathExpr(owner, path), nil
		case parentContainer != nil && g.findFieldExpr(parentContainer, path) != nil && parentOwner != "":
			return fieldPathExpr(parentOwner, path), nil
		case rootContainer != nil && g.findFieldExpr(rootContainer, path) != nil && rootOwner != "":
			return fieldPathExpr(rootOwner, path), nil
		default:
			return fieldPathExpr(owner, path), nil
		}
	}
}

func (g *Generator) switchLabels(container *schemair.Container, parentContainer *schemair.Container, rootContainer *schemair.Container, expr *schemair.Switch, kase schemair.SwitchCase) ([]string, error) {
	fieldExpr := g.switchFieldExpr(container, parentContainer, rootContainer, expr)
	labels := make([]string, 0, len(kase.Labels))
	seen := make(map[string]struct{}, len(kase.Labels))
	for _, label := range kase.Labels {
		converted, err := g.switchLabelExpr(fieldExpr, label)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[converted]; ok {
			continue
		}
		seen[converted] = struct{}{}
		labels = append(labels, converted)
	}
	return labels, nil
}

func (g *Generator) switchFieldExpr(container *schemair.Container, parentContainer *schemair.Container, rootContainer *schemair.Container, expr *schemair.Switch) schemair.Expr {
	if expr.CompareTo == "" {
		return nil
	}
	return g.findFieldExpr(switchCompareContainer(g, container, parentContainer, rootContainer, expr.CompareTo), trimComparePath(expr.CompareTo))
}

func switchCompareContainer(g *Generator, container *schemair.Container, parentContainer *schemair.Container, rootContainer *schemair.Container, path string) *schemair.Container {
	switch {
	case strings.HasPrefix(path, "../"):
		return parentContainer
	case strings.HasPrefix(path, "/"):
		return rootContainer
	default:
		trimmed := trimComparePath(path)
		switch {
		case g.findFieldExpr(container, trimmed) != nil:
			return container
		case parentContainer != nil && g.findFieldExpr(parentContainer, trimmed) != nil:
			return parentContainer
		case rootContainer != nil && g.findFieldExpr(rootContainer, trimmed) != nil:
			return rootContainer
		}
		return container
	}
}

func trimComparePath(path string) string {
	for strings.HasPrefix(path, "../") {
		path = strings.TrimPrefix(path, "../")
	}
	return strings.TrimPrefix(path, "/")
}

func (g *Generator) switchLabelExpr(fieldExpr schemair.Expr, label string) (string, error) {
	switch v := fieldExpr.(type) {
	case *schemair.Ref:
		if def, ok := g.types[v.Name]; ok {
			if mapper, ok := def.Expr.(*schemair.Mapper); ok {
				return g.mapperConstName(def.Name, mapper, label), nil
			}
		}
		if _, ok := g.natives[v.Name]; ok {
			return literalForNative(v.Name, label), nil
		}
		return literalForNative("", label), nil
	case *schemair.Native:
		return literalForNative(v.Name, label), nil
	case *schemair.Mapper:
		typeName := g.exprTypeName(v)
		if typeName == "" {
			typeName = "UnknownEnum"
		}
		return g.mapperConstName(typeName, v, label), nil
	case *schemair.Option:
		return g.switchLabelExpr(v.Inner, label)
	case *schemair.Bitfield:
		return literalForNative("", label), nil
	default:
		return literalForNative("", label), nil
	}
}

func (g *Generator) findFieldExpr(container *schemair.Container, path string) schemair.Expr {
	if container == nil || path == "" {
		return nil
	}
	parts := strings.Split(path, "/")
	var expr schemair.Expr = container
	for _, part := range parts {
		expr = g.findPathExpr(expr, part)
		if expr == nil {
			return nil
		}
	}
	return expr
}

func (g *Generator) findPathExpr(expr schemair.Expr, part string) schemair.Expr {
	switch v := expr.(type) {
	case *schemair.Container:
		for _, field := range v.Fields {
			if field.Name == part {
				return field.Type
			}
			if field.Anonymous {
				if nested := g.findPathExpr(field.Type, part); nested != nil {
					return nested
				}
			}
		}
	case *schemair.Ref:
		if def, ok := g.types[v.Name]; ok {
			return g.findPathExpr(def.Expr, part)
		}
	case *schemair.Option:
		return g.findPathExpr(v.Inner, part)
	case *schemair.Bitfield:
		for _, field := range v.Fields {
			if field.Name == part {
				return nil
			}
		}
	case *schemair.Bitflags:
		for _, flag := range v.Flags {
			if flag.Name == part {
				return &schemair.Native{Name: "Bool"}
			}
		}
	}
	return nil
}

func literalForNative(nativeName string, label string) string {
	switch nativeName {
	case "Bool":
		switch label {
		case "1", "true":
			return "true"
		default:
			return "false"
		}
	}
	if label == "false" {
		return "0"
	}
	if label == "true" {
		return "1"
	}
	if _, err := strconv.Atoi(label); err == nil {
		return label
	}
	if strings.HasPrefix(label, "\"") || strings.HasPrefix(label, "'") {
		return label
	}
	return strconv.Quote(label)
}

func switchValueExpr(v any) string {
	switch raw := v.(type) {
	case nil:
		return "nil"
	case string:
		return strconv.Quote(raw)
	case bool:
		if raw {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(raw)
	default:
		return fmt.Sprint(raw)
	}
}

func fieldPathExpr(owner string, path string) string {
	parts := strings.Split(path, "/")
	var b strings.Builder
	b.WriteString(owner)
	for _, part := range parts {
		if part == "" {
			continue
		}
		b.WriteByte('.')
		b.WriteString(sanitizeFieldGoName(part))
	}
	return b.String()
}

func sliceExpr(value string) string {
	if strings.HasPrefix(value, "*") {
		return "(" + value + ")"
	}
	return value
}

func indexExpr(value string, index string) string {
	base := value
	if strings.HasPrefix(base, "*") {
		base = "(" + base + ")"
	}
	return base + "[" + index + "]"
}

func aliasValueExpr(goType string, value string) string {
	return goType + "(" + value + ")"
}

func filterNewLabels(labels []string, seen map[string]struct{}) []string {
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	return out
}
