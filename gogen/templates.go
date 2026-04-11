package gogen

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"strings"
	"text/template"
)

type templateData map[string]any

//go:embed templates/*.tmpl
var templateFS embed.FS

var templates = template.Must(template.New("gogen").Funcs(template.FuncMap{
	"join": strings.Join,
}).ParseFS(templateFS, "templates/*.tmpl"))

func executeTemplate(out *bytes.Buffer, name string, data any) error {
	if err := templates.ExecuteTemplate(out, name, data); err != nil {
		return fmt.Errorf("execute template %s: %w", name, err)
	}
	return nil
}

func renderFormattedTemplate(name string, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := executeTemplate(&buf, name, data); err != nil {
		return nil, err
	}
	return format.Source(buf.Bytes())
}

func writeSizeCall(out *bytes.Buffer, indent string, call string) error {
	return executeTemplate(out, "sizeCall", templateData{"Indent": indent, "Call": call})
}

func writeAppendCall(out *bytes.Buffer, indent string, call string) error {
	return executeTemplate(out, "appendCall", templateData{"Indent": indent, "Call": call})
}

func writeDecodeCall(out *bytes.Buffer, indent string, call string) error {
	return executeTemplate(out, "decodeCall", templateData{"Indent": indent, "Call": call})
}
