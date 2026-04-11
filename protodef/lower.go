package protodef

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/iancoleman/strcase"

	"github.com/KonjacBot/packetizer/schemair"
)

func Lower(doc *Document) (*schemair.File, error) {
	var defs []*schemair.Definition
	for _, entry := range doc.Entries {
		switch {
		case entry.Key == "^types":
			block, ok := entry.Value.(*Block)
			if !ok {
				return nil, fmt.Errorf("^types must be a block")
			}
			more, err := lowerDefinitions("", block.Entries)
			if err != nil {
				return nil, err
			}
			defs = append(defs, more...)
		case strings.HasPrefix(entry.Key, "^") && strings.HasSuffix(entry.Key, ".types"):
			block, ok := entry.Value.(*Block)
			if !ok {
				return nil, fmt.Errorf("%s must be a block", entry.Key)
			}
			namespace := strings.TrimSuffix(strings.TrimPrefix(entry.Key, "^"), ".types")
			more, err := lowerDefinitions(namespace, block.Entries)
			if err != nil {
				return nil, err
			}
			defs = append(defs, more...)
		}
	}
	qualifyDefinitions(defs)
	return &schemair.File{Definitions: defs}, nil
}

func qualifyDefinitions(defs []*schemair.Definition) {
	type defKey struct {
		namespace string
		name      string
	}

	rename := make(map[defKey]string, len(defs))
	nameCounts := make(map[string]int, len(defs))
	for _, def := range defs {
		key := defKey{namespace: def.Namespace, name: def.Name}
		if def.Namespace == "" {
			rename[key] = def.Name
		} else {
			rename[key] = sanitizeTypeName(def.Namespace) + def.Name
		}
		nameCounts[def.Name]++
	}

	resolve := func(namespace, name string) string {
		if name == "" {
			return name
		}
		if renamed, ok := rename[defKey{namespace: namespace, name: name}]; ok {
			return renamed
		}
		if renamed, ok := rename[defKey{namespace: "", name: name}]; ok {
			return renamed
		}
		if nameCounts[name] == 1 {
			for key, renamed := range rename {
				if key.name == name {
					return renamed
				}
			}
		}
		return name
	}

	var rewriteExpr func(namespace string, expr schemair.Expr)
	rewriteExpr = func(namespace string, expr schemair.Expr) {
		switch v := expr.(type) {
		case *schemair.Ref:
			v.Name = resolve(namespace, v.Name)
		case *schemair.Container:
			for i := range v.Fields {
				rewriteExpr(namespace, v.Fields[i].Type)
			}
		case *schemair.Array:
			rewriteExpr(namespace, v.Elem)
			v.Count.Type = resolve(namespace, v.Count.Type)
		case *schemair.Option:
			rewriteExpr(namespace, v.Inner)
		case *schemair.Mapper:
			rewriteExpr(namespace, v.Base)
		case *schemair.Switch:
			for i := range v.Cases {
				rewriteExpr(namespace, v.Cases[i].Expr)
			}
			if v.Default != nil {
				rewriteExpr(namespace, v.Default)
			}
		case *schemair.RegistryHolder:
			rewriteExpr(namespace, v.OtherwiseType)
		case *schemair.RegistryHolderSet:
			rewriteExpr(namespace, v.BaseType)
			rewriteExpr(namespace, v.OtherwiseType)
		case *schemair.EntityMetadataLoop:
			rewriteExpr(namespace, v.Elem)
		case *schemair.TopBitSetTerminatedArray:
			rewriteExpr(namespace, v.Elem)
		}
	}

	for _, def := range defs {
		rewriteExpr(def.Namespace, def.Expr)
		def.Name = rename[defKey{namespace: def.Namespace, name: def.Name}]
	}
}

func lowerDefinitions(namespace string, entries []Entry) ([]*schemair.Definition, error) {
	defs := make([]*schemair.Definition, 0, len(entries))
	for _, entry := range entries {
		expr, err := lowerValue(entry.Key, entry.Value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Key, err)
		}
		defs = append(defs, &schemair.Definition{
			Name:      sanitizeTypeName(entry.Key),
			Namespace: namespace,
			Expr:      expr,
		})
	}
	return defs, nil
}

func lowerValue(name string, value Value) (schemair.Expr, error) {
	switch v := value.(type) {
	case *Scalar:
		switch v.Text {
		case "native":
			return &schemair.Native{Name: sanitizeTypeName(name)}, nil
		case "void":
			return &schemair.Void{}, nil
		default:
			return &schemair.Ref{Name: sanitizeTypeName(v.Text)}, nil
		}
	case *Flow:
		return lowerFlow(v.Value)
	case *Block:
		fields, err := lowerFields(v.Entries)
		if err != nil {
			return nil, err
		}
		return &schemair.Container{Fields: fields}, nil
	case *Array:
		elemExpr, err := lowerArrayElem(v)
		if err != nil {
			return nil, err
		}
		return &schemair.Array{
			Elem:  elemExpr,
			Count: lowerCount(v.CountText),
		}, nil
	case *Mapper:
		base, err := lowerScalarLike(v.Base)
		if err != nil {
			return nil, err
		}
		entries := make([]schemair.MapperEntry, 0, len(v.Cases))
		for _, item := range v.Cases {
			entries = append(entries, schemair.MapperEntry{Key: item.Key, Value: sanitizeTypeName(item.Value)})
		}
		return &schemair.Mapper{Base: base, Entries: entries}, nil
	case *Switch:
		cases := make([]schemair.SwitchCase, 0, len(v.Cases))
		var defaultExpr schemair.Expr = &schemair.Void{}
		for _, item := range v.Cases {
			switch {
			case strings.HasPrefix(item.Key, "if "):
				labels := splitCaseLabels(strings.TrimSpace(strings.TrimPrefix(item.Key, "if ")))
				expr, err := lowerValue(item.Key, item.Value)
				if err != nil {
					return nil, err
				}
				cases = append(cases, schemair.SwitchCase{Labels: labels, Expr: expr})
			case item.Key == "default":
				expr, err := lowerValue(item.Key, item.Value)
				if err != nil {
					return nil, err
				}
				defaultExpr = expr
			default:
				return nil, fmt.Errorf("invalid switch case %q", item.Key)
			}
		}
		return &schemair.Switch{CompareTo: sanitizeFieldPath(v.CompareTo), Cases: cases, Default: defaultExpr}, nil
	default:
		return nil, fmt.Errorf("unsupported value type %T", value)
	}
}

func lowerFields(entries []Entry) ([]schemair.Field, error) {
	fields := make([]schemair.Field, 0, len(entries))
	for _, entry := range entries {
		name := entry.Key
		optional := strings.HasSuffix(name, "?")
		if optional {
			name = strings.TrimSuffix(name, "?")
		}
		expr, err := lowerValue(name, entry.Value)
		if err != nil {
			return nil, err
		}
		if optional {
			expr = &schemair.Option{Inner: expr}
		}
		fields = append(fields, schemair.Field{
			Name:      sanitizeFieldName(name),
			Optional:  optional,
			Anonymous: name == "_",
			Type:      expr,
		})
	}
	return fields, nil
}

func lowerArrayElem(v *Array) (schemair.Expr, error) {
	if v.Inline != nil {
		fields, err := lowerFields(v.Inline.Entries)
		if err != nil {
			return nil, err
		}
		return &schemair.Container{Fields: fields}, nil
	}
	return lowerScalarLike(v.ElemText)
}

func lowerScalarLike(text string) (schemair.Expr, error) {
	text = strings.TrimSpace(text)
	switch text {
	case "":
		return nil, fmt.Errorf("missing type")
	case "void":
		return &schemair.Void{}, nil
	default:
		return &schemair.Ref{Name: sanitizeTypeName(text)}, nil
	}
}

func lowerFlow(raw any) (schemair.Expr, error) {
	switch v := raw.(type) {
	case []any:
		if len(v) == 0 {
			return nil, fmt.Errorf("empty flow expression")
		}
		name, ok := v[0].(string)
		if !ok {
			return nil, fmt.Errorf("flow callee must be string")
		}
		name = sanitizeTypeName(name)
		switch name {
		case "Bitfield":
			if len(v) != 2 {
				return nil, fmt.Errorf("bitfield expects one fields argument")
			}
			rawFields, ok := v[1].([]any)
			if !ok {
				return nil, fmt.Errorf("bitfield fields must be an array")
			}
			fields := make([]schemair.BitfieldField, 0, len(rawFields))
			for _, rawField := range rawFields {
				m, ok := rawField.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("bitfield field must be object")
				}
				size, ok := toInt(m["size"])
				if !ok {
					return nil, fmt.Errorf("bitfield size must be integer")
				}
				signed, _ := m["signed"].(bool)
				name, _ := m["name"].(string)
				fields = append(fields, schemair.BitfieldField{
					Name:   sanitizeFieldName(name),
					Size:   size,
					Signed: signed,
				})
			}
			return &schemair.Bitfield{Fields: fields}, nil
		case "Switch":
			if len(v) != 2 {
				return nil, fmt.Errorf("switch expects options")
			}
			opts, ok := v[1].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("switch options must be object")
			}
			compareTo, _ := opts["compareTo"].(string)
			var compareValue any
			if compareTo == "" {
				if raw, ok := opts["compareToValue"]; ok {
					switch v := raw.(type) {
					case string:
						compareValue = v
					case bool:
						compareValue = v
					case int, int64, float64:
						if n, ok := toInt(v); ok {
							compareValue = n
						}
					}
				}
			}
			fields, _ := opts["fields"].(map[string]any)
			cases := make([]schemair.SwitchCase, 0, len(fields))
			for key, value := range fields {
				expr, err := lowerGenericFlowValue(value)
				if err != nil {
					return nil, err
				}
				cases = append(cases, schemair.SwitchCase{Labels: []string{key}, Expr: expr})
			}
			defaultExpr := schemair.Expr(&schemair.Void{})
			if rawDefault, ok := opts["default"]; ok {
				expr, err := lowerGenericFlowValue(rawDefault)
				if err != nil {
					return nil, err
				}
				defaultExpr = expr
			}
			return &schemair.Switch{CompareTo: sanitizeFieldPath(compareTo), CompareValue: compareValue, Cases: cases, Default: defaultExpr}, nil
		case "Container":
			if len(v) != 2 {
				return nil, fmt.Errorf("container expects fields")
			}
			rawFields, ok := v[1].([]any)
			if !ok {
				return nil, fmt.Errorf("container fields must be an array")
			}
			fields := make([]schemair.Field, 0, len(rawFields))
			for _, rawField := range rawFields {
				m, ok := rawField.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("container field must be object")
				}
				fieldType, err := lowerGenericFlowValue(m["type"])
				if err != nil {
					return nil, err
				}
				name, _ := m["name"].(string)
				anon, _ := m["anon"].(bool)
				if anon && name == "" {
					name = "_"
				}
				fields = append(fields, schemair.Field{
					Name:      sanitizeFieldName(name),
					Anonymous: anon,
					Type:      fieldType,
				})
			}
			return &schemair.Container{Fields: fields}, nil
		case "Mapper":
			if len(v) != 2 {
				return nil, fmt.Errorf("mapper expects options")
			}
			opts, ok := v[1].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("mapper options must be object")
			}
			base, err := lowerGenericFlowValue(opts["type"])
			if err != nil {
				return nil, err
			}
			rawMappings, _ := opts["mappings"].(map[string]any)
			entries := make([]schemair.MapperEntry, 0, len(rawMappings))
			for key, value := range rawMappings {
				entries = append(entries, schemair.MapperEntry{Key: key, Value: fmt.Sprint(value)})
			}
			return &schemair.Mapper{Base: base, Entries: entries}, nil
		case "Array":
			if len(v) != 2 {
				return nil, fmt.Errorf("array expects options")
			}
			opts, ok := v[1].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("array options must be object")
			}
			elem, err := lowerGenericFlowValue(opts["type"])
			if err != nil {
				return nil, err
			}
			count := schemair.Count{}
			if countType, ok := opts["countType"].(string); ok {
				count.Type = sanitizeTypeName(countType)
			}
			if countField, ok := opts["count"].(string); ok {
				if strings.HasPrefix(countField, "$") {
					count.Field = sanitizeFieldPath(strings.TrimPrefix(countField, "$"))
				} else if n, err := strconv.Atoi(countField); err == nil {
					count.Fixed = &n
				} else {
					count.Field = sanitizeFieldPath(countField)
				}
			}
			return &schemair.Array{Elem: elem, Count: count}, nil
		case "Count":
			if len(v) != 2 {
				return nil, fmt.Errorf("count expects options")
			}
			opts, ok := v[1].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("count options must be object")
			}
			call := &schemair.Call{Name: name, Options: map[string]any{}}
			if typ, ok := opts["type"].(string); ok {
				call.Options["type"] = sanitizeTypeName(typ)
			}
			if countFor, ok := opts["countFor"].(string); ok {
				call.Options["countFor"] = sanitizeFieldPath(countFor)
			}
			return call, nil
		case "Option":
			if len(v) != 2 {
				return nil, fmt.Errorf("option expects one argument")
			}
			inner, err := lowerGenericFlowValue(v[1])
			if err != nil {
				return nil, err
			}
			return &schemair.Option{Inner: inner}, nil
		case "Bitflags":
			if len(v) != 2 {
				return nil, fmt.Errorf("bitflags expects options")
			}
			opts, ok := v[1].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("bitflags options must be object")
			}
			base, ok := opts["type"].(string)
			if !ok || strings.TrimSpace(base) == "" {
				return nil, fmt.Errorf("bitflags missing type")
			}
			flags, err := lowerBitflagsFlags(opts)
			if err != nil {
				return nil, err
			}
			return &schemair.Bitflags{
				Base:  sanitizeTypeName(base),
				Flags: flags,
			}, nil
		case "Buffer", "Pstring", "Cstring":
			call := &schemair.Call{Name: name, Options: map[string]any{}}
			if len(v) > 1 {
				opts, ok := v[1].(map[string]any)
				if !ok {
					return nil, fmt.Errorf("%s options must be object", strings.ToLower(name))
				}
				for key, value := range opts {
					switch key {
					case "countType", "type":
						if text, ok := value.(string); ok {
							call.Options[key] = sanitizeTypeName(text)
						}
					case "count", "countFor":
						switch raw := value.(type) {
						case string:
							if n, err := strconv.Atoi(raw); err == nil {
								call.Options[key] = n
							} else {
								call.Options[key] = sanitizeFieldPath(strings.TrimPrefix(raw, "$"))
							}
						case int, int64, float64:
							if n, ok := toInt(raw); ok {
								call.Options[key] = n
							}
						}
					case "rest", "shift", "big":
						if b, ok := value.(bool); ok {
							call.Options[key] = b
						}
					case "encoding":
						if text, ok := value.(string); ok {
							call.Options[key] = strings.ToLower(strings.TrimSpace(text))
						}
					}
				}
			}
			return call, nil
		case "RegistryEntryHolder":
			if len(v) != 2 {
				return nil, fmt.Errorf("registryEntryHolder expects options")
			}
			opts, ok := v[1].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("registryEntryHolder options must be object")
			}
			baseName, _ := opts["baseName"].(string)
			otherwise, err := lowerNamedFieldOption(opts["otherwise"])
			if err != nil {
				return nil, err
			}
			return &schemair.RegistryHolder{
				BaseName:      sanitizeFieldName(baseName),
				OtherwiseName: otherwise.Name,
				OtherwiseType: otherwise.Type,
			}, nil
		case "RegistryEntryHolderSet":
			if len(v) != 2 {
				return nil, fmt.Errorf("registryEntryHolderSet expects options")
			}
			opts, ok := v[1].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("registryEntryHolderSet options must be object")
			}
			base, err := lowerNamedFieldOption(opts["base"])
			if err != nil {
				return nil, err
			}
			otherwise, err := lowerNamedFieldOption(opts["otherwise"])
			if err != nil {
				return nil, err
			}
			return &schemair.RegistryHolderSet{
				BaseName:      base.Name,
				BaseType:      base.Type,
				OtherwiseName: otherwise.Name,
				OtherwiseType: otherwise.Type,
			}, nil
		case "EntityMetadataLoop":
			if len(v) != 2 {
				return nil, fmt.Errorf("entityMetadataLoop expects options")
			}
			opts, ok := v[1].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("entityMetadataLoop options must be object")
			}
			elem, err := lowerGenericFlowValue(opts["type"])
			if err != nil {
				return nil, err
			}
			endVal := 255
			if raw, ok := opts["endVal"]; ok {
				n, ok := toInt(raw)
				if !ok {
					return nil, fmt.Errorf("entityMetadataLoop endVal must be integer")
				}
				endVal = n
			}
			return &schemair.EntityMetadataLoop{Elem: elem, EndVal: endVal}, nil
		case "TopBitSetTerminatedArray":
			if len(v) != 2 {
				return nil, fmt.Errorf("topBitSetTerminatedArray expects options")
			}
			opts, ok := v[1].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("topBitSetTerminatedArray options must be object")
			}
			elem, err := lowerGenericFlowValue(opts["type"])
			if err != nil {
				return nil, err
			}
			return &schemair.TopBitSetTerminatedArray{Elem: elem}, nil
		default:
			call := &schemair.Call{Name: name, Options: map[string]any{}}
			if len(v) > 1 {
				switch opts := v[1].(type) {
				case map[string]any:
					call.Options = opts
				}
			}
			return call, nil
		}
	case map[string]any:
		fields := make([]schemair.Field, 0, len(v))
		for key, child := range v {
			expr, err := lowerGenericFlowValue(child)
			if err != nil {
				return nil, err
			}
			fields = append(fields, schemair.Field{Name: sanitizeFieldName(key), Type: expr})
		}
		return &schemair.Container{Fields: fields}, nil
	default:
		return nil, fmt.Errorf("unsupported flow expression %T", raw)
	}
}

func lowerGenericFlowValue(raw any) (schemair.Expr, error) {
	switch v := raw.(type) {
	case string:
		return lowerScalarLike(v)
	case []any, map[string]any:
		return lowerFlow(v)
	default:
		return nil, fmt.Errorf("unsupported flow value %T", raw)
	}
}

type namedFieldOption struct {
	Name string
	Type schemair.Expr
}

func lowerNamedFieldOption(raw any) (namedFieldOption, error) {
	opts, ok := raw.(map[string]any)
	if !ok {
		return namedFieldOption{}, fmt.Errorf("named field option must be object")
	}
	name, _ := opts["name"].(string)
	typeName, ok := opts["type"].(string)
	if !ok || strings.TrimSpace(typeName) == "" {
		return namedFieldOption{}, fmt.Errorf("named field option missing type")
	}
	expr, err := lowerScalarLike(typeName)
	if err != nil {
		return namedFieldOption{}, err
	}
	return namedFieldOption{Name: sanitizeFieldName(name), Type: expr}, nil
}

func lowerCount(raw string) schemair.Count {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return schemair.Count{Type: "varint"}
	}
	if strings.HasPrefix(raw, "$") {
		return schemair.Count{Field: sanitizeFieldPath(strings.TrimPrefix(raw, "$"))}
	}
	if n, err := strconv.Atoi(raw); err == nil {
		return schemair.Count{Fixed: &n}
	}
	return schemair.Count{Type: sanitizeTypeName(raw)}
}

func splitCaseLabels(raw string) []string {
	parts := strings.Split(raw, " or ")
	labels := make([]string, 0, len(parts))
	for _, part := range parts {
		labels = append(labels, strings.TrimSpace(part))
	}
	return labels
}

func sanitizeTypeName(name string) string {
	switch name {
	case "":
		return ""
	case "varint":
		return "VarInt"
	case "varint64":
		return "VarInt64"
	case "varint128":
		return "VarInt128"
	case "varlong":
		return "VarLong"
	case "zigzag32":
		return "Zigzag32"
	case "zigzag64":
		return "Zigzag64"
	case "bool":
		return "Bool"
	case "void":
		return "Void"
	case "string":
		return "String"
	case "byte":
		return "I8"
	case "short":
		return "I16"
	case "int":
		return "Int"
	case "long":
		return "I64"
	case "float":
		return "F32"
	case "double":
		return "F64"
	case "li16":
		return "Li16"
	case "lu16":
		return "Lu16"
	case "li32":
		return "Li32"
	case "lu32":
		return "Lu32"
	case "li64":
		return "Li64"
	case "lu64":
		return "Lu64"
	case "lf32":
		return "Lf32"
	case "lf64":
		return "Lf64"
	}
	name = strings.TrimPrefix(name, "^")
	name = strings.ReplaceAll(name, "[]", " Array ")
	name = strings.ReplaceAll(name, "/", " ")
	name = strings.ReplaceAll(name, ":", " ")
	name = strings.ReplaceAll(name, ".", " ")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "$", " ")
	name = strings.ReplaceAll(name, "?", " ")
	return strcase.ToCamel(compactTypeWords(strings.Fields(name)))
}

func compactTypeWords(words []string) string {
	if len(words) == 0 {
		return ""
	}
	compacted := make([]string, 0, len(words))
	for _, word := range words {
		if len(compacted) > 0 && strings.EqualFold(compacted[len(compacted)-1], word) {
			continue
		}
		compacted = append(compacted, word)
	}
	packetIndex := -1
	for i, word := range compacted {
		if strings.EqualFold(word, "packet") {
			packetIndex = i
			break
		}
	}
	if packetIndex >= 0 && packetIndex < len(compacted)-1 {
		packet := compacted[packetIndex]
		compacted = append(append(compacted[:packetIndex], compacted[packetIndex+1:]...), packet)
	}
	return strings.Join(compacted, " ")
}

func sanitizeFieldName(name string) string {
	name = strings.TrimPrefix(name, "$")
	if name == "_" {
		return "Variant"
	}
	return sanitizeFieldSegment(name)
}

func sanitizeFieldPath(name string) string {
	if name == "" {
		return ""
	}
	prefix := ""
	for strings.HasPrefix(name, "../") {
		prefix += "../"
		name = strings.TrimPrefix(name, "../")
	}
	if strings.HasPrefix(name, "/") {
		prefix += "/"
		name = strings.TrimPrefix(name, "/")
	}
	parts := strings.Split(name, "/")
	for i, part := range parts {
		parts[i] = sanitizeFieldName(part)
	}
	return prefix + strings.Join(parts, "/")
}

func sanitizeFieldSegment(name string) string {
	result := sanitizeTypeName(name)
	switch result {
	case "Size", "Append", "Decode":
		return result + "Field"
	default:
		return result
	}
}

func lowerBitflagsFlags(opts map[string]any) ([]schemair.BitflagFlag, error) {
	shift := false
	switch raw := opts["shift"].(type) {
	case bool:
		shift = raw
	case int, int64, float64:
		n, ok := toInt(raw)
		if ok {
			shift = n != 0
		}
	}
	switch raw := opts["flags"].(type) {
	case []any:
		flags := make([]schemair.BitflagFlag, 0, len(raw))
		for i, item := range raw {
			name, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("bitflags flag must be string")
			}
			flags = append(flags, schemair.BitflagFlag{
				Name: sanitizeFieldName(name),
				Mask: 1 << i,
			})
		}
		return flags, nil
	case map[string]any:
		flags := make([]schemair.BitflagFlag, 0, len(raw))
		for name, value := range raw {
			n, ok := toInt(value)
			if !ok {
				return nil, fmt.Errorf("bitflags flag value must be integer")
			}
			mask := uint64(n)
			if shift {
				mask = 1 << uint(n)
			}
			flags = append(flags, schemair.BitflagFlag{
				Name: sanitizeFieldName(name),
				Mask: mask,
			})
		}
		slices.SortFunc(flags, func(a, b schemair.BitflagFlag) int {
			if a.Mask < b.Mask {
				return -1
			}
			if a.Mask > b.Mask {
				return 1
			}
			return strings.Compare(a.Name, b.Name)
		})
		return flags, nil
	default:
		return nil, fmt.Errorf("bitflags missing flags")
	}
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
