package generator

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/KonjacBot/packetizer/gogen"
	"github.com/KonjacBot/packetizer/protodef"
	"github.com/KonjacBot/packetizer/schemair"
)

type Config struct {
	Sources        []string          `yaml:"sources"`
	Target         TargetConfig      `yaml:"target"`
	LabelOverrides map[string]string `yaml:"label_overrides"`
}

type TargetConfig struct {
	BaseDirectory string `yaml:"base_directory"`
	BasePackage   string `yaml:"base_package"`
}

func Run(cfg Config) error {
	if len(cfg.Sources) == 0 {
		return fmt.Errorf("target config requires at least one source")
	}
	if cfg.Target.BaseDirectory == "" {
		return fmt.Errorf("target.base_directory is required")
	}
	if cfg.Target.BasePackage == "" {
		return fmt.Errorf("target.base_package is required")
	}
	data, err := loadProtoSources(cfg.Sources)
	if err != nil {
		return err
	}
	doc, err := protodef.Parse(data)
	if err != nil {
		return err
	}
	ir, err := protodef.Lower(doc)
	if err != nil {
		return err
	}
	prepared, err := gogen.Prepare(ir, gogen.Options{LabelOverrides: cfg.LabelOverrides})
	if err != nil {
		return err
	}
	return emitGoLayoutPackages(ir, prepared, cfg.Target, cfg.LabelOverrides)
}

func loadProtoSource(inputPath string, downloadURL string) ([]byte, error) {
	switch {
	case inputPath != "":
		return os.ReadFile(inputPath)
	case downloadURL != "":
		resp, err := http.Get(downloadURL)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("download %s failed: %s", downloadURL, resp.Status)
		}
		return io.ReadAll(resp.Body)
	default:
		return nil, fmt.Errorf("missing source")
	}
}

func loadProtoSources(sources []string) ([]byte, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("missing sources")
	}
	var out bytes.Buffer
	for _, source := range sources {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		inputPath, downloadURL := source, ""
		if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
			inputPath, downloadURL = "", source
		}
		data, err := loadProtoSource(inputPath, downloadURL)
		if err != nil {
			return nil, err
		}
		if out.Len() > 0 && !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
			out.WriteByte('\n')
		}
		out.Write(data)
		if !bytes.HasSuffix(data, []byte("\n")) {
			out.WriteByte('\n')
		}
	}
	return out.Bytes(), nil
}

func emitGoLayoutPackages(ir *schemair.File, prepared *gogen.Prepared, target TargetConfig, labelOverrides map[string]string) error {
	if err := clearGeneratedGoFiles(target.BaseDirectory); err != nil {
		return err
	}
	module, err := modulePath(".")
	if err != nil {
		return err
	}
	baseImport := resolveBaseImportPath(module, target.BasePackage)

	original := map[string]struct{}{}
	for _, def := range ir.Definitions {
		original[def.Name] = struct{}{}
	}
	roots := buildRootOwners(ir.Definitions)
	graph := buildDefinitionGraph(prepared.Definitions, prepared.Types)
	assignments := planDefinitionPackages(prepared.Definitions, graph, original, roots)
	packages := bucketDefinitions(prepared.Definitions, assignments)

	for _, pkg := range packages {
		dir := filepath.Join(target.BaseDirectory, filepath.FromSlash(pkg.Key))
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		externalRefs := buildExternalRefs(assignments, pkg.Key, baseImport)
		files := groupPackageFiles(pkg.Definitions, roots)
		for _, file := range files {
			content, err := gogen.Emit(prepared, gogen.EmitOptions{
				PackageName:    pkg.Name,
				Definitions:    file.Definitions,
				ExternalRefs:   externalRefs,
				LabelOverrides: labelOverrides,
			})
			if err != nil {
				return fmt.Errorf("emit %s: %w", filepath.Join(dir, file.Name), err)
			}
			if err := os.WriteFile(filepath.Join(dir, file.Name), content, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

type packageBucket struct {
	Key         string
	Name        string
	Definitions []*schemair.Definition
}

type fileBucket struct {
	Name        string
	Definitions []*schemair.Definition
}

type rootOwner struct {
	Prefix     string
	PackageKey string
	FileBase   string
}

var packetChannelPattern = regexp.MustCompile(`^(Handshaking|Status|Login|Configuration|Play)To(Client|Server)Packet(?:Name)?$`)
var packetTypePattern = regexp.MustCompile(`^(Handshaking|Status|Login|Configuration|Play)To(Client|Server)Packet(.+)$`)
var typeStatePrefixPattern = regexp.MustCompile(`^(Handshaking|Status|Login|Configuration|Play)(.+)$`)

type packetInfo struct {
	State string
	Bound string
	Name  string
}

func modulePath(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	return "", fmt.Errorf("module path not found")
}

func parsePacketInfo(name string) (packetInfo, bool) {
	m := packetTypePattern.FindStringSubmatch(name)
	if m == nil {
		return packetInfo{}, false
	}
	return packetInfo{
		State: strings.ToLower(m[1]),
		Bound: strings.ToLower(m[2]),
		Name:  compactAliasName(m[3]) + "Packet",
	}, true
}

func bucketDefinitions(defs []*schemair.Definition, assignments map[string]string) []packageBucket {
	grouped := map[string][]*schemair.Definition{}
	for _, def := range defs {
		key := assignments[def.Name]
		grouped[key] = append(grouped[key], def)
	}
	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	out := make([]packageBucket, 0, len(keys))
	for _, key := range keys {
		defs := grouped[key]
		slices.SortFunc(defs, func(a, b *schemair.Definition) int {
			return strings.Compare(a.Name, b.Name)
		})
		out = append(out, packageBucket{
			Key:         key,
			Name:        pathBase(key),
			Definitions: defs,
		})
	}
	return out
}

func groupPackageFiles(defs []*schemair.Definition, roots []rootOwner) []fileBucket {
	grouped := map[string][]*schemair.Definition{}
	for _, def := range defs {
		base := fileBaseForDefinition(def.Name, roots)
		grouped[base] = append(grouped[base], def)
	}
	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	out := make([]fileBucket, 0, len(keys))
	for _, key := range keys {
		defs := grouped[key]
		slices.SortFunc(defs, func(a, b *schemair.Definition) int {
			return strings.Compare(a.Name, b.Name)
		})
		out = append(out, fileBucket{Name: key + ".go", Definitions: defs})
	}
	return out
}

func fileBaseForDefinition(name string, roots []rootOwner) string {
	if root, ok := findOwningRoot(name, roots); ok {
		return root.FileBase
	}
	base := strings.TrimSuffix(snakeFileName(trimTypePrefix(name)), ".go")
	parts := strings.Split(base, "_")
	if len(parts) > 3 {
		parts = parts[:3]
	}
	return strings.Join(parts, "_")
}

func buildExternalRefs(assignments map[string]string, currentPackage string, baseImport string) map[string]gogen.ExternalRef {
	out := map[string]gogen.ExternalRef{}
	for name, pkg := range assignments {
		if pkg == currentPackage {
			continue
		}
		out[name] = gogen.ExternalRef{
			ImportPath: baseImport + "/" + pkg,
			Alias:      importAliasForPackage(pkg),
		}
	}
	return out
}

func importAliasForPackage(pkg string) string {
	return strings.ReplaceAll(pkg, "/", "")
}

func resolveBaseImportPath(modulePath string, basePackage string) string {
	basePackage = strings.TrimSpace(strings.TrimPrefix(basePackage, "./"))
	if basePackage == "" {
		return modulePath
	}
	if strings.HasPrefix(basePackage, modulePath) {
		return basePackage
	}
	return modulePath + "/" + filepath.ToSlash(basePackage)
}

func pathBase(path string) string {
	if idx := strings.LastIndexByte(path, '/'); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

func layoutSwitchCaseName(ctx string, kase schemair.SwitchCase, i int) string {
	if i < 0 {
		return layoutSwitchDefaultName(ctx)
	}
	if len(kase.Labels) == 1 {
		label := protodef.SanitizeTypeNameForCLI(kase.Labels[0])
		if label != "" && label != "True" && label != "False" {
			return ctx + label
		}
	}
	return fmt.Sprintf("%sCase%d", ctx, i)
}

func layoutSwitchDefaultName(ctx string) string {
	return ctx + "Default"
}

func clearGeneratedGoFiles(baseDir string) error {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return err
	}
	return filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".go" {
			return os.Remove(path)
		}
		return nil
	})
}

func planDefinitionPackages(defs []*schemair.Definition, graph map[string]map[string]struct{}, original map[string]struct{}, roots []rootOwner) map[string]string {
	directSeeds := map[string]string{}
	for _, def := range defs {
		if _, isOriginal := original[def.Name]; !isOriginal {
			continue
		}
		if root, ok := findOwningRoot(def.Name, roots); ok {
			directSeeds[def.Name] = root.PackageKey
			continue
		}
		if isRootTypeDefinition(def) {
			directSeeds[def.Name] = "types"
			continue
		}
		if m := typeStatePrefixPattern.FindStringSubmatch(def.Name); m != nil && !packetTypePattern.MatchString(def.Name) {
			directSeeds[def.Name] = strings.ToLower(m[1]) + "/types"
		}
	}
	owners := propagateOwners(defs, graph, directSeeds)

	originalAssignments := map[string]string{}
	for _, def := range defs {
		if _, isOriginal := original[def.Name]; !isOriginal {
			continue
		}
		if seed, ok := directSeeds[def.Name]; ok {
			originalAssignments[def.Name] = seed
			continue
		}
		originalAssignments[def.Name] = collapseOwnersToTypes(owners[def.Name])
	}

	owners = propagateOwners(defs, graph, originalAssignments)

	assignments := map[string]string{}
	for _, def := range defs {
		name := def.Name
		if seed, ok := originalAssignments[name]; ok {
			assignments[name] = seed
			continue
		}
		if root, ok := findOwningRoot(name, roots); ok {
			assignments[name] = root.PackageKey
			continue
		}
		if _, isOriginal := original[name]; !isOriginal && len(owners[name]) == 1 {
			for owner := range owners[name] {
				if !strings.HasSuffix(owner, "/types") {
					assignments[name] = owner
					goto next
				}
			}
		}
		assignments[name] = collapseOwnersToTypes(owners[name])
	next:
	}
	changed := true
	for changed {
		changed = false
		for _, def := range defs {
			name := def.Name
			if assignments[name] != "types" {
				continue
			}
			depStates := map[string]struct{}{}
			for dep := range graph[name] {
				pkg := assignments[dep]
				if pkg == "" || pkg == "types" {
					continue
				}
				state := pkg
				if idx := strings.IndexByte(pkg, '/'); idx >= 0 {
					state = pkg[:idx]
				}
				depStates[state] = struct{}{}
			}
			if len(depStates) == 1 {
				for state := range depStates {
					assignments[name] = state + "/types"
					changed = true
				}
			}
		}
	}
	return assignments
}

func isRootTypeDefinition(def *schemair.Definition) bool {
	switch expr := def.Expr.(type) {
	case *schemair.Native:
		return true
	case *schemair.Call:
		switch expr.Name {
		case "Buffer", "Pstring", "Cstring", "Int", "Count":
			return true
		}
	}
	return false
}

func propagateOwners(defs []*schemair.Definition, graph map[string]map[string]struct{}, seeds map[string]string) map[string]map[string]struct{} {
	owners := map[string]map[string]struct{}{}
	for _, def := range defs {
		owners[def.Name] = map[string]struct{}{}
	}
	for name, seed := range seeds {
		if owners[name] == nil {
			owners[name] = map[string]struct{}{}
		}
		owners[name][seed] = struct{}{}
	}
	changed := true
	for changed {
		changed = false
		for from, deps := range graph {
			for dep := range deps {
				if owners[dep] == nil {
					owners[dep] = map[string]struct{}{}
				}
				for owner := range owners[from] {
					if _, ok := owners[dep][owner]; !ok {
						owners[dep][owner] = struct{}{}
						changed = true
					}
				}
			}
		}
	}
	return owners
}

func collapseOwnersToTypes(owners map[string]struct{}) string {
	states := map[string]struct{}{}
	for owner := range owners {
		state := owner
		if idx := strings.IndexByte(owner, '/'); idx >= 0 {
			state = owner[:idx]
		}
		states[state] = struct{}{}
	}
	if len(states) == 1 {
		for state := range states {
			return state + "/types"
		}
	}
	return "types"
}

func buildDefinitionGraph(defs []*schemair.Definition, all map[string]*schemair.Definition) map[string]map[string]struct{} {
	graph := map[string]map[string]struct{}{}
	for _, def := range defs {
		deps := map[string]struct{}{}
		collectDefinitionDeps(def.Name, def.Expr, def.Name, all, deps)
		delete(deps, def.Name)
		graph[def.Name] = deps
	}
	return graph
}

func buildRootOwners(original []*schemair.Definition) []rootOwner {
	var roots []rootOwner
	for _, def := range original {
		if info, ok := parsePacketInfo(def.Name); ok {
			roots = append(roots, rootOwner{
				Prefix:     def.Name,
				PackageKey: info.State + "/" + info.Bound,
				FileBase:   strings.TrimSuffix(snakeFileName(info.Name), ".go"),
			})
			continue
		}
		if m := packetChannelPattern.FindStringSubmatch(def.Name); m != nil {
			roots = append(roots, rootOwner{
				Prefix:     def.Name,
				PackageKey: strings.ToLower(m[1]) + "/" + strings.ToLower(m[2]),
				FileBase:   truncateSnakeBase(strings.TrimSuffix(snakeFileName(trimTypePrefix(def.Name)), ".go")),
			})
		}
	}
	slices.SortFunc(roots, func(a, b rootOwner) int {
		return len(b.Prefix) - len(a.Prefix)
	})
	return roots
}

func findOwningRoot(name string, roots []rootOwner) (rootOwner, bool) {
	for _, root := range roots {
		if strings.HasPrefix(name, root.Prefix) {
			return root, true
		}
	}
	return rootOwner{}, false
}

func truncateSnakeBase(base string) string {
	parts := strings.Split(base, "_")
	if len(parts) > 3 {
		parts = parts[:3]
	}
	return strings.Join(parts, "_")
}

func trimTypePrefix(name string) string {
	if m := packetTypePattern.FindStringSubmatch(name); m != nil {
		return m[3]
	}
	if m := typeStatePrefixPattern.FindStringSubmatch(name); m != nil {
		return m[2]
	}
	return name
}

func compactAliasName(name string) string {
	words := splitCamelWords(name)
	if len(words) == 0 {
		return name
	}
	compacted := words[:0]
	for _, word := range words {
		if len(compacted) > 0 && strings.EqualFold(compacted[len(compacted)-1], word) {
			continue
		}
		compacted = append(compacted, word)
	}
	return strings.Join(compacted, "")
}

func splitCamelWords(name string) []string {
	if name == "" {
		return nil
	}
	var words []string
	start := 0
	for i := 1; i < len(name); i++ {
		if isWordBoundary(name, i) {
			words = append(words, name[start:i])
			start = i
		}
	}
	words = append(words, name[start:])
	return words
}

func isWordBoundary(name string, i int) bool {
	prev := name[i-1]
	curr := name[i]
	if prev >= '0' && prev <= '9' {
		return curr < '0' || curr > '9'
	}
	if curr >= '0' && curr <= '9' {
		return true
	}
	if prev >= 'a' && prev <= 'z' && curr >= 'A' && curr <= 'Z' {
		return true
	}
	if i+1 < len(name) && prev >= 'A' && prev <= 'Z' && curr >= 'A' && curr <= 'Z' && name[i+1] >= 'a' && name[i+1] <= 'z' {
		return true
	}
	return false
}

func snakeFileName(name string) string {
	words := splitCamelWords(name)
	for i, word := range words {
		words[i] = strings.ToLower(word)
	}
	return strings.Join(words, "_") + ".go"
}

func collectDefinitionDeps(root string, expr schemair.Expr, ctx string, all map[string]*schemair.Definition, deps map[string]struct{}) {
	switch v := expr.(type) {
	case *schemair.Option:
		collectDefinitionDeps(root, v.Inner, ctx+"Value", all, deps)
	case *schemair.Array:
		collectDefinitionDeps(root, v.Elem, ctx+"Item", all, deps)
	case *schemair.Ref:
		if _, ok := all[v.Name]; ok {
			deps[v.Name] = struct{}{}
		}
	case *schemair.Container:
		if ctx != root {
			if _, ok := all[ctx]; ok {
				deps[ctx] = struct{}{}
				return
			}
		}
		for _, field := range v.Fields {
			collectDefinitionDeps(root, field.Type, ctx+field.Name, all, deps)
		}
	case *schemair.Mapper:
		if ctx != root {
			if _, ok := all[ctx]; ok {
				deps[ctx] = struct{}{}
				return
			}
		}
		collectDefinitionDeps(root, v.Base, ctx+"Base", all, deps)
	case *schemair.Bitfield, *schemair.Bitflags:
		if ctx != root {
			if _, ok := all[ctx]; ok {
				deps[ctx] = struct{}{}
			}
		}
	case *schemair.Switch:
		for i, kase := range v.Cases {
			caseName := layoutSwitchCaseName(ctx, kase, i)
			if _, ok := all[caseName]; ok {
				deps[caseName] = struct{}{}
				continue
			}
			collectDefinitionDeps(root, kase.Expr, caseName, all, deps)
		}
		defaultName := layoutSwitchDefaultName(ctx)
		if _, ok := all[defaultName]; ok {
			deps[defaultName] = struct{}{}
			return
		}
		if v.Default != nil {
			collectDefinitionDeps(root, v.Default, defaultName, all, deps)
		}
	case *schemair.RegistryHolder:
		collectDefinitionDeps(root, v.OtherwiseType, ctx+v.OtherwiseName, all, deps)
	case *schemair.RegistryHolderSet:
		collectDefinitionDeps(root, v.BaseType, ctx+v.BaseName, all, deps)
		collectDefinitionDeps(root, v.OtherwiseType, ctx+v.OtherwiseName+"Item", all, deps)
	case *schemair.EntityMetadataLoop:
		collectDefinitionDeps(root, v.Elem, ctx+"Item", all, deps)
	case *schemair.TopBitSetTerminatedArray:
		collectDefinitionDeps(root, v.Elem, ctx+"Item", all, deps)
	}
}
