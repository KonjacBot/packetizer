package protodef

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type logicalLine struct {
	indent int
	text   string
	line   int
}

func Parse(data []byte) (*Document, error) {
	if isJSONObject(data) {
		return parseProtocolJSON(data)
	}
	lines, err := lexLogicalLines(data)
	if err != nil {
		return nil, err
	}
	idx := 0
	entries, err := parseEntries(lines, &idx, 0)
	if err != nil {
		return nil, err
	}
	return &Document{Entries: entries}, nil
}

func isJSONObject(data []byte) bool {
	return len(bytes.TrimSpace(data)) > 0 && bytes.TrimSpace(data)[0] == '{'
}

func parseProtocolJSON(data []byte) (*Document, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse json protocol: %w", err)
	}

	var entries []Entry
	if types, ok := root["types"].(map[string]any); ok {
		entries = append(entries, Entry{Key: "^types", Value: jsonTypesBlock(types)})
	}
	for state, rawState := range root {
		if state == "types" {
			continue
		}
		stateMap, ok := rawState.(map[string]any)
		if !ok {
			continue
		}
		for bound, rawBound := range stateMap {
			boundMap, ok := rawBound.(map[string]any)
			if !ok {
				continue
			}
			types, ok := boundMap["types"].(map[string]any)
			if !ok {
				continue
			}
			entries = append(entries, Entry{
				Key:   "^" + state + "." + bound + ".types",
				Value: jsonTypesBlock(types),
			})
		}
	}
	return &Document{Entries: entries}, nil
}

func jsonTypesBlock(types map[string]any) *Block {
	entries := make([]Entry, 0, len(types))
	for key, raw := range types {
		entries = append(entries, Entry{Key: key, Value: jsonValue(raw)})
	}
	return &Block{Entries: entries}
}

func jsonValue(raw any) Value {
	switch v := raw.(type) {
	case string:
		return &Scalar{Text: v}
	default:
		return &Flow{Value: v}
	}
}

func lexLogicalLines(data []byte) ([]logicalLine, error) {
	rawLines := bytes.Split(data, []byte{'\n'})
	var (
		lines      []logicalLine
		buf        strings.Builder
		baseLine   int
		baseIndent int
		balance    int
	)
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		lines = append(lines, logicalLine{indent: baseIndent, text: buf.String(), line: baseLine})
		buf.Reset()
		baseLine = 0
		baseIndent = 0
		balance = 0
	}
	for i, raw := range rawLines {
		line := stripComment(string(raw))
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		content := line[indent:]
		if buf.Len() == 0 {
			baseLine = i + 1
			baseIndent = indent
			buf.WriteString(content)
			balance = flowBalance(content)
			if balance == 0 {
				flush()
			}
			continue
		}
		buf.WriteByte('\n')
		buf.WriteString(strings.TrimSpace(content))
		balance += flowBalance(content)
		if balance == 0 {
			flush()
		}
	}
	if buf.Len() != 0 {
		return nil, fmt.Errorf("unterminated flow expression at line %d", baseLine)
	}
	return lines, nil
}

func parseEntries(lines []logicalLine, idx *int, indent int) ([]Entry, error) {
	var entries []Entry
	for *idx < len(lines) {
		line := lines[*idx]
		if line.indent < indent {
			break
		}
		if line.indent > indent {
			return nil, fmt.Errorf("unexpected indentation at line %d", line.line)
		}
		entry, err := parseEntry(lines, idx)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func parseEntry(lines []logicalLine, idx *int) (Entry, error) {
	line := lines[*idx]
	key, rawValue, ok := splitKeyValue(line.text)
	if !ok {
		return Entry{}, fmt.Errorf("invalid entry at line %d", line.line)
	}
	*idx++
	key = strings.TrimSpace(key)
	rawValue = strings.TrimSpace(rawValue)

	switch {
	case rawValue == "":
		child, err := parseIndentedBlock(lines, idx, line.indent)
		if err != nil {
			return Entry{}, err
		}
		return Entry{Key: key, Value: &Block{Entries: child}, Line: line.line}, nil
	case strings.HasSuffix(rawValue, "=>"):
		cases, err := parseMapperCases(lines, idx, line.indent)
		if err != nil {
			return Entry{}, err
		}
		return Entry{
			Key:   key,
			Value: &Mapper{Base: strings.TrimSpace(strings.TrimSuffix(rawValue, "=>")), Cases: cases},
			Line:  line.line,
		}, nil
	case strings.HasSuffix(rawValue, "?"):
		cases, err := parseSwitchCases(lines, idx, line.indent)
		if err != nil {
			return Entry{}, err
		}
		return Entry{
			Key:   key,
			Value: &Switch{CompareTo: strings.TrimSpace(strings.TrimSuffix(rawValue, "?")), Cases: cases},
			Line:  line.line,
		}, nil
	default:
		value, err := parseValue(rawValue)
		if err != nil {
			return Entry{}, fmt.Errorf("line %d: %w", line.line, err)
		}
		if arr, ok := value.(*Array); ok {
			child, err := parseIndentedBlock(lines, idx, line.indent)
			if err != nil {
				return Entry{}, err
			}
			if len(child) > 0 {
				arr.Inline = &Block{Entries: child}
			}
		}
		return Entry{Key: key, Value: value, Line: line.line}, nil
	}
}

func parseIndentedBlock(lines []logicalLine, idx *int, parentIndent int) ([]Entry, error) {
	if *idx >= len(lines) || lines[*idx].indent <= parentIndent {
		return nil, nil
	}
	return parseEntries(lines, idx, lines[*idx].indent)
}

func parseMapperCases(lines []logicalLine, idx *int, parentIndent int) ([]MapperCase, error) {
	if *idx >= len(lines) {
		return nil, nil
	}
	next := lines[*idx]
	if next.indent > parentIndent {
		startIndent := next.indent
		var cases []MapperCase
		ordinal := 0
		for *idx < len(lines) {
			line := lines[*idx]
			if line.indent < startIndent {
				break
			}
			if line.indent > startIndent {
				return nil, fmt.Errorf("unexpected nested mapper case at line %d", line.line)
			}
			text := strings.TrimSpace(line.text)
			switch {
			case strings.HasPrefix(text, "- "):
				cases = append(cases, MapperCase{Key: strconv.Itoa(ordinal), Value: strings.TrimSpace(strings.TrimPrefix(text, "- ")), Ordinal: ordinal})
				ordinal++
				*idx++
			default:
				key, value, ok := splitKeyValue(text)
				if !ok {
					return nil, fmt.Errorf("invalid mapper case at line %d", line.line)
				}
				cases = append(cases, MapperCase{Key: strings.TrimSpace(key), Value: strings.TrimSpace(value), Ordinal: ordinal})
				ordinal++
				*idx++
			}
		}
		return cases, nil
	}

	var cases []MapperCase
	ordinal := 0
	for *idx < len(lines) {
		line := lines[*idx]
		if line.indent < parentIndent {
			break
		}
		if line.indent > parentIndent {
			return nil, fmt.Errorf("unexpected nested mapper case at line %d", line.line)
		}
		text := strings.TrimSpace(line.text)
		switch {
		case strings.HasPrefix(text, "- "):
			cases = append(cases, MapperCase{Key: strconv.Itoa(ordinal), Value: strings.TrimSpace(strings.TrimPrefix(text, "- ")), Ordinal: ordinal})
			ordinal++
			*idx++
		case isLiteralMapperKey(text):
			key, value, ok := splitKeyValue(text)
			if !ok {
				return nil, fmt.Errorf("invalid mapper case at line %d", line.line)
			}
			cases = append(cases, MapperCase{Key: strings.TrimSpace(key), Value: strings.TrimSpace(value), Ordinal: ordinal})
			ordinal++
			*idx++
		default:
			return cases, nil
		}
	}
	return cases, nil
}

func parseSwitchCases(lines []logicalLine, idx *int, parentIndent int) ([]Entry, error) {
	child, err := parseIndentedBlock(lines, idx, parentIndent)
	if err != nil {
		return nil, err
	}
	return child, nil
}

func parseValue(raw string) (Value, error) {
	raw = strings.TrimSpace(raw)
	if elem, count, ok := parseArrayShorthand(raw); ok {
		return &Array{ElemText: elem, CountText: count}, nil
	}
	if strings.HasPrefix(raw, "[") || strings.HasPrefix(raw, "{") {
		var value any
		if err := yaml.Unmarshal([]byte(raw), &value); err != nil {
			return nil, fmt.Errorf("parse flow value: %w", err)
		}
		return &Flow{Value: value}, nil
	}
	return &Scalar{Text: raw}, nil
}

func parseArrayShorthand(raw string) (elem string, count string, ok bool) {
	index := strings.Index(raw, "[]")
	if index < 0 {
		return "", "", false
	}
	elem = strings.TrimSpace(raw[:index])
	count = strings.TrimSpace(raw[index+2:])
	if count == "" {
		return "", "", false
	}
	return elem, count, true
}

func splitKeyValue(text string) (key string, value string, ok bool) {
	var (
		depth int
		quote rune
	)
	for i, r := range text {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '[' || r == '{' || r == '(':
			depth++
		case r == ']' || r == '}' || r == ')':
			if depth > 0 {
				depth--
			}
		case r == ':' && depth == 0:
			rest := text[i+1:]
			if rest == "" {
				return text[:i], "", true
			}
			if strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\n") {
				return text[:i], strings.TrimLeft(rest, " "), true
			}
		}
	}
	return "", "", false
}

func stripComment(line string) string {
	var (
		depth int
		quote rune
	)
	for i, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '[' || r == '{' || r == '(':
			depth++
		case r == ']' || r == '}' || r == ')':
			if depth > 0 {
				depth--
			}
		case r == '#' && depth == 0:
			return strings.TrimRight(line[:i], " ")
		}
	}
	return strings.TrimRight(line, " ")
}

func flowBalance(text string) int {
	var (
		balance int
		quote   rune
	)
	for _, r := range text {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '[' || r == '{':
			balance++
		case r == ']' || r == '}':
			balance--
		}
	}
	return balance
}

func isLiteralMapperKey(text string) bool {
	key, _, ok := splitKeyValue(text)
	if !ok {
		return false
	}
	key = strings.TrimSpace(key)
	if key == "" || strings.HasPrefix(key, "if ") || key == "default" {
		return false
	}
	if _, err := strconv.Atoi(key); err == nil {
		return true
	}
	switch key {
	case "true", "false", "null":
		return true
	}
	return strings.HasPrefix(key, "\"") || strings.HasPrefix(key, "'")
}
