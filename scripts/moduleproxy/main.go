// Command moduleproxy creates an offline local verification archive, not a
// release or published version. All output must be container-owned scratch.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if os.Getenv("OBEY_SDK_TEST_CONTAINER") != "1" {
		panic("module archive fixture requires container marker")
	}
	if len(os.Args) != 3 {
		panic("expected source and output proxy")
	}
	source, destination := os.Args[1], os.Args[2]
	module, version := "github.com/obediencecorp/obey-go-sdk", "v0.0.0-om0002"
	dir := filepath.Join(destination, module, "@v")
	must(os.MkdirAll(dir, 0755))
	f, err := os.Create(filepath.Join(dir, version+".zip"))
	must(err)
	writer := zip.NewWriter(f)
	must(filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		top := strings.Split(filepath.ToSlash(relative), "/")[0]
		allowed := top == "pkg" || top == "internal" || top == "examples" || relative == "go.mod" || relative == "README.md" || relative == "LICENSE"
		if !allowed {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, err := writer.Create(module + "@" + version + "/" + filepath.ToSlash(relative))
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	}))
	must(writer.Close())
	must(f.Close())
	mod, err := os.ReadFile(filepath.Join(source, "go.mod"))
	must(err)
	if strings.Contains(string(mod), "replace ") {
		panic("SDK module has a replace directive")
	}
	must(os.WriteFile(filepath.Join(dir, version+".mod"), mod, 0644))
	info, _ := json.Marshal(map[string]string{"Version": version, "Time": "2000-01-01T00:00:00Z"})
	must(os.WriteFile(filepath.Join(dir, version+".info"), info, 0644))
	must(os.WriteFile(filepath.Join(dir, "list"), []byte(version+"\n"), 0644))
	archive, err := os.ReadFile(filepath.Join(dir, version+".zip"))
	must(err)
	sum := sha256.Sum256(archive)
	fmt.Println("offline verification archive SHA256", hex.EncodeToString(sum[:]))
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
