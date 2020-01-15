package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	if _, err := os.Stat("go.sum"); os.IsNotExist(err) {
		panic(err)
	}

	data, err := ioutil.ReadFile("go.sum")
	if err != nil {
		panic(err)
	}

	modPath := filepath.Join(os.Getenv("GOPATH"), "pkg", "mod")

	for _, line := range bytes.Split(data, []byte("\n")) {
		items := bytes.SplitN(line, []byte(" "), 3)
		if len(items) != 3 {
			continue
		}

		// skip unless has suffix /go.mod
		if !bytes.HasSuffix(items[1], []byte("/go.mod")) {
			continue
		}

		subPath := fmt.Sprintf("%s@%s", items[0], items[1][:len(items[1])-7])

		// DataDog -> !data!dog
		r := regexp.MustCompile(`[A-Z]`)
		subPath = r.ReplaceAllStringFunc(subPath, func(m string) string {
			return "!" + strings.ToLower(m)
		})

		// skip when src folder not exists
		fullPath := filepath.Join(modPath, subPath)
		if _, err := os.Stat(fullPath); err != nil {
			continue
		}

		// skip symlink already exists
		vendorPath := filepath.Join("vendor", string(items[0]))
		if _, err := os.Stat(vendorPath); err == nil {
			continue
		}

		// create parent folder if not exists
		vendorDir := filepath.Dir(vendorPath)

		parentDir := filepath.Dir(vendorDir)
		_ = os.Chmod(parentDir, 0755) // try to 755 parent

		if _, err := os.Stat(vendorDir); os.IsNotExist(err) {
			err := os.MkdirAll(vendorDir, 0755)
			if err != nil {
				panic(err)
			}
		}

		// symlink now
		err := os.Symlink(fullPath, vendorPath)
		if err != nil {
			panic(err)
		}

		// done
		fmt.Println("symlink created", vendorPath)
	}
}
