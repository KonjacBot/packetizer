package main

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"text/template"

	"github.com/iancoleman/strcase"

	"git.konjactw.dev/falloutBot/go-mc/net/packet"
	"golang.org/x/tools/go/packages"
)

type FieldInfo struct {
	Name            string
	OriginalType    string
	Type            string
	NeedConvert     bool
	ValidField      bool
	IsSlice         bool
	IsFunc          bool
	FixedBitSetSize string
	IsPointer       bool

	OptionInfos []OptionInfo
}

type StructInfo struct {
	Name    string
	Package string
	Fields  []FieldInfo
	Imports []string
}

type SliceFuncInfo struct {
	SizeType   string
	McType     string
	TargetType string
	TypeName   string
}

type OptionInfo struct {
	Optional bool // Optional if true includes data

	RegistryID bool // RegistryID or inline

	EnumSwitch bool
	EnumID     string

	GroupFieldName string
}

type PackageInfo struct {
	Structs      []StructInfo
	SliceFuncMap map[string]SliceFuncInfo
	Imports      []string
	PackageName  string
}

type packageData struct {
	file       *ast.File
	pkg        *packages.Package
	commentMap ast.CommentMap
}

func parseTag(prefix, tag string) string {
	if tag == "" {
		return ""
	}

	tag = strings.Trim(tag, "`")

	parts := strings.Split(tag, " ")
	for _, part := range parts {
		if strings.HasPrefix(part, prefix) {
			mcValue := strings.Trim(part[len(prefix):], `"`)
			return mcValue
		}
	}

	return ""
}

func shouldProcessStruct(commentMap ast.CommentMap, genDecl *ast.GenDecl) bool {
	if groups, ok := commentMap[genDecl]; ok {
		for _, cg := range groups {
			for _, c := range cg.List {
				content := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c.Text), "//"))
				if content == "codec:gen" {
					return true
				}
			}
		}
	}
	return false
}

func getOptionGroup(commentGroup *ast.CommentGroup) []OptionInfo {
	if commentGroup == nil {
		return nil
	}
	var infos []OptionInfo
	for _, c := range commentGroup.List {
		content := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c.Text), "//"))
		content = strings.TrimPrefix(content, "opt:")
		if content == "" {
			continue
		}
		args := strings.Split(content, ":")
		if len(args) == 1 {
			return nil
		}
		if len(args) >= 2 {
			switch strings.ToLower(args[0]) {
			case "optional", "opt", "prefixed", "option":
				infos = append(infos, OptionInfo{
					GroupFieldName: args[1],
					Optional:       true,
				})
			case "registry", "idor", "id", "reg", "inlineid":
				infos = append(infos, OptionInfo{
					GroupFieldName: args[1],
					RegistryID:     true,
				})
			case "enum", "enumset", "enumswitch", "enums":
				infos = append(infos, OptionInfo{
					GroupFieldName: args[1],
					EnumSwitch:     true,
					EnumID:         args[2],
				})
			}
		}
	}

	return infos
}

func (p packageData) analyzeFile() (pkgInfo PackageInfo) {
	var pkField *types.Interface
	typ, ok := packetFieldMap["Field"]
	if ok {
		pkField = typ.Underlying().(*types.Interface)
	}
	pkgInfo.SliceFuncMap = make(map[string]SliceFuncInfo)

	for _, decl := range p.file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		if !shouldProcessStruct(p.commentMap, gen) {
			continue
		}

		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}

			structInfo := StructInfo{
				Name:    ts.Name.Name,
				Package: p.pkg.Name,
				Fields:  []FieldInfo{},
			}

			for _, field := range st.Fields.List {
				if len(field.Names) == 0 {
					continue
				}

				optionInfos := getOptionGroup(field.Doc)

				for _, name := range field.Names {
					fi := FieldInfo{
						Name:        name.Name,
						OptionInfos: optionInfos,
					}
					obj := p.pkg.TypesInfo.Defs[name]
					if obj != nil {
						tagValue := fieldTagValue(field)
						mcTag := parseTag("mc:", tagValue)
						sliceSizeTag := parseTag("mcSlice:", tagValue)
						t := obj.Type()

						if po, ok := t.(*types.Pointer); ok {
							t = po.Elem()
							fi.IsPointer = true
							fi.NeedConvert = true
						}

						if s, ok := t.(*types.Slice); ok {
							fi.IsSlice = true
							t = s.Elem()
							if mcTag != "" {
								if fieldType, ok := packetFieldMap[mcTag]; ok {
									if _, ok := fieldType.Underlying().(*types.Slice); ok {
										fi.IsSlice = false
										fi.NeedConvert = true
										t = fieldType
									}
								}
							}
						}

						origType := types.TypeString(t, func(p1 *types.Package) string {
							if p1.Name() == p.pkg.Name {
								return ""
							}
							return p1.Name()
						})
						fi.OriginalType = origType

						if b, ok := t.(*types.Basic); ok {
							basicType := getBasicType(b.Kind())
							if basicType != nil {
								fi.NeedConvert = true
								t = basicType
							}
						}

						typeString := types.TypeString(t, func(p1 *types.Package) string {
							if p1.Path() == p.pkg.PkgPath {
								return ""
							}
							return p1.Name()
						})

						fi.Type = typeString

						if mcTag != "" {
							if fieldType, ok := packetFieldMap[mcTag]; ok && types.ConvertibleTo(t, fieldType) {
								fi.NeedConvert = true
								fi.Type = "packet." + mcTag
								t = fieldType
							}
							if fieldType, ok := packetFieldMap[mcTag]; ok {
								if sig, ok := fieldType.Underlying().(*types.Signature); ok {
									if sig.Results().Len() == 1 {
										fi.IsFunc = true
										fi.Type = "packet." + mcTag
										t = sig.Results().At(0).Type()
									}
								}
							}
							if mcTag == "FixedBitSet" {
								fi.FixedBitSetSize = parseTag("size:", tagValue)
							}
						}

						{
							if types.Implements(t, pkField) || types.Implements(types.NewPointer(t), pkField) {
								fi.ValidField = true
							}
						}

						if fi.IsSlice && fi.Type != origType {
							if sliceSizeTag == "" {
								sliceSizeTag = "VarInt"
							}
							newTypeName := strcase.ToCamel(origType + " " + mcTag + " " + sliceSizeTag + " Array")
							pkgInfo.SliceFuncMap[newTypeName] = SliceFuncInfo{
								SizeType:   "packet." + sliceSizeTag,
								McType:     fi.Type,
								TargetType: origType,
								TypeName:   newTypeName,
							}
							fi.Type = newTypeName
							fi.IsSlice = false
							fi.NeedConvert = true
							fi.ValidField = true
						}

						if !fi.ValidField {
							fmt.Printf("Warning: field %s (in struct %s) is not a valid field of packet.Field.\n", fi.Name, ts.Name.Name)
						}
					}

					structInfo.Fields = append(structInfo.Fields, fi)
				}
			}

			pkgInfo.Structs = append(pkgInfo.Structs, structInfo)
		}
	}

	if len(pkgInfo.SliceFuncMap) > 0 {
		if !slices.Contains(pkgInfo.Imports, "errors") {
			pkgInfo.Imports = append(pkgInfo.Imports, "errors")
		}
	}

	if !slices.Contains(pkgInfo.Imports, "git.konjactw.dev/falloutBot/go-mc/net/packet") {
		pkgInfo.Imports = append(pkgInfo.Imports, "git.konjactw.dev/falloutBot/go-mc/net/packet")
	}

	if !slices.Contains(pkgInfo.Imports, "io") {
		pkgInfo.Imports = append(pkgInfo.Imports, "io")
	}
	return
}

func (c PackageInfo) SliceFuncs() []SliceFuncInfo {
	var result []SliceFuncInfo
	for _, v := range c.SliceFuncMap {
		result = append(result, v)
	}
	return result
}

func (f FieldInfo) HasOption() bool {
	return len(f.OptionInfos) > 0
}

func (f FieldInfo) IsFixedBitSet() bool {
	return f.FixedBitSetSize != ""
}

func getBasicType(kind types.BasicKind) types.Type {
	switch kind {
	case types.Bool:
		return packetFieldMap["Boolean"]
	case types.Int:
		return packetFieldMap["Int"]
	case types.Int8:
		return packetFieldMap["Byte"]
	case types.Int16:
		return packetFieldMap["Short"]
	case types.Int32:
		return packetFieldMap["Int"]
	case types.Int64:
		return packetFieldMap["Long"]
	case types.Uint8:
		return packetFieldMap["UnsignedByte"]
	case types.Uint16:
		return packetFieldMap["UnsignedShort"]
	case types.Float32:
		return packetFieldMap["Float"]
	case types.Float64:
		return packetFieldMap["Double"]
	case types.String:
		return packetFieldMap["String"]
	case types.UntypedBool:
		return packetFieldMap["Boolean"]
	case types.UntypedInt:
		return packetFieldMap["Int"]
	case types.UntypedFloat:
		return packetFieldMap["Double"]
	case types.UntypedString:
		return packetFieldMap["String"]
	default:
		return nil
	}
}

func fieldTagValue(f *ast.Field) string {
	if f.Tag != nil {
		return f.Tag.Value
	}
	return ""
}

func (f FieldInfo) GenerateFieldTarget() string {
	pattern := fmt.Sprintf("(&c.%s)", f.Name)
	if f.IsPointer {
		pattern = fmt.Sprintf("(c.%s)", f.Name)
	}
	if f.IsSlice {
		pattern = "packet.Array" + pattern
	} else if f.IsFunc {
		pattern = "" + f.Type + pattern
	} else if f.NeedConvert {
		pattern = "(*" + f.Type + ")" + pattern
	}
	return pattern
}

var packetFieldMap = map[string]types.Type{}

//go:embed codec.go.tmpl
var codecTemplate string

var tmpl *template.Template

func init() {
	_ = packet.Field(nil)

	funcMap := template.FuncMap{}

	tmpl = template.Must(template.New("codecs").Funcs(funcMap).Parse(codecTemplate))
}

func main() {
	dir := flag.String("dir", ".", "input directory to search for codec:gen tags")
	noFormat := flag.Bool("noFormat", false, "don't run goimports and go fmt on the generated file")
	flag.Parse()

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedFiles | packages.NeedDeps,
		Fset: token.NewFileSet(),
		Dir:  *dir,
		Env:  os.Environ(),
	}

	pkgs, err := packages.Load(cfg, "git.konjactw.dev/falloutBot/go-mc/net/packet", "./...")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to load packet package: %v\n", err)
		os.Exit(1)
	}
	for _, pkg := range pkgs {
		if pkg.PkgPath == "git.konjactw.dev/falloutBot/go-mc/net/packet" {
			scope := pkg.Types.Scope()
			for _, name := range scope.Names() {
				packetFieldMap[name] = scope.Lookup(name).Type()
			}
			break
		}
	}

	grouped := make(map[string]*PackageInfo)
	for _, pkg := range pkgs {
		if pkg.PkgPath == "git.konjactw.dev/falloutBot/go-mc/net/packet" {
			continue
		}
		for _, file := range pkg.Syntax {
			pf := cfg.Fset.Position(file.Pos()).Filename
			if strings.HasSuffix(pf, "codecs.go") {
				continue
			}

			info := packageData{file: file, pkg: pkg, commentMap: ast.NewCommentMap(cfg.Fset, file, file.Comments)}.analyzeFile()
			if info.Structs == nil || len(info.Structs) == 0 {
				continue
			}
			groupedInfo, ok := grouped[filepath.Dir(pf)]
			if !ok {
				groupedInfo = &PackageInfo{PackageName: pkg.Name, SliceFuncMap: make(map[string]SliceFuncInfo)}
			}
			groupedInfo.Structs = append(groupedInfo.Structs, info.Structs...)
			for _, s := range info.Imports {
				if slices.Contains(groupedInfo.Imports, s) {
					continue
				}
				groupedInfo.Imports = append(groupedInfo.Imports, s)
			}
			for s, funcInfo := range info.SliceFuncMap {
				groupedInfo.SliceFuncMap[s] = funcInfo
			}
			grouped[filepath.Dir(pf)] = groupedInfo
		}
	}

	if len(grouped) == 0 {
		fmt.Println("No structs found with //codec:gen. Nothing to generate.")
		return
	}

	fmt.Printf("Anaylazed %d packages...\n", len(grouped))
	fmt.Println("Generating code...")
	wg := sync.WaitGroup{}
	for dirPath, pkgInfo := range grouped {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, pkgInfo); err != nil {
			fmt.Fprintf(os.Stderr, "Template execution error: %v\n", err)
			continue
		}

		out := filepath.Join(dirPath, "codecs.go")
		if err := os.WriteFile(out, buf.Bytes(), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", out, err)
			continue
		}

		fmt.Printf("Generated %s\n", out)
		if *noFormat {
			continue
		}
		wg.Add(1)
		go func() {
			err = exec.Command("goimports", "-w", out).Run()
			if err != nil {
				fmt.Println(err)
			}
			err = exec.Command("go", "fmt", out).Run()
			if err != nil {
				fmt.Println(err)
			}
			fmt.Printf("Formatted %s\n", out)
			wg.Done()
		}()
	}

	wg.Wait()
	fmt.Println("Code generation complete.")
}
