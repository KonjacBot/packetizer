package protodef_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/KonjacBot/packetizer/gogen"
	"github.com/KonjacBot/packetizer/protodef"
)

func TestParseLowerGenerateSample(t *testing.T) {
	compileProtoFile(t, "../testdata/sample_proto.yml", "sample", []string{"Mode", "ItemData", "ExamplePacket"})
}

func TestParseLowerGenerateSpecDatatypes(t *testing.T) {
	compileProtoFile(t, "../testdata/spec_proto.yml", "sample", nil)
}

func compileProtoFile(t *testing.T, protoPath string, packageName string, types []string) {
	t.Helper()

	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(protoPath)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := protodef.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	ir, err := protodef.Lower(doc)
	if err != nil {
		t.Fatal(err)
	}
	code, err := gogen.Generate(ir, gogen.Options{
		PackageName: packageName,
		Types:       types,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(code) == 0 {
		t.Fatal("expected generated code")
	}

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "proto_gen.go"), code, 0644); err != nil {
		t.Fatal(err)
	}
	mod := "module proto_compile_test\n\ngo 1.24\n\nrequire github.com/KonjacBot/packetizer v0.0.0\n\nreplace github.com/KonjacBot/packetizer => " + repoRoot
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = tmp
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go mod tidy failed:\n%s", out)
	}

	cmd = exec.Command("go", "test", "./...")
	cmd.Dir = tmp
	cmd.Env = os.Environ()
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test failed:\n%s", out)
	}
}
