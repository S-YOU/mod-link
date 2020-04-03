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

	if _, err := os.Stat("go.mod"); os.IsNotExist(err) {
		panic(err)
	}
	gomod, err := ioutil.ReadFile("go.mod")
	if err != nil {
		panic(err)
	}

	re := regexp.MustCompile(`replace (\S+) => (\S+)`)
	replaces := re.FindAllSubmatch(gomod, -1)
	replaceMap := make(map[string]string, len(replaces))
	for _, x := range replaces {
		replaceMap[string(x[2])] = string(x[1])
	}

	vendorDir := filepath.Dir("vendor")
	if _, err := os.Stat(vendorDir); os.IsNotExist(err) {
		err := os.Mkdir(vendorDir, 0755)
		if err != nil {
			panic(err)
		}
	}

	modulesRegexp := regexp.MustCompile(`\t(\S+) `)
	matches := modulesRegexp.FindAllSubmatch(gomod, -1)
	var buf bytes.Buffer
	for _, x := range matches {
		buf.Write(x[1])
		buf.WriteString("\n")
	}
	ioutil.WriteFile("vendor/modules.txt", buf.Bytes(), 0644)

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

		multipleMod := make([]byte, 0)
		multipleMod = append(multipleMod, items[0]...)
		multipleMod = append(multipleMod, '/', 'v', '2')
		if bytes.Contains(data, multipleMod) {
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

		links := []string{string(items[0])}
		if val, ok := replaceMap[string(string(items[0]))]; ok {
			links = append(links, val)
		}

		for _, link := range links {
			// skip symlink already exists
			vendorPath := filepath.Join("vendor", link)
			if info, err := os.Lstat(vendorPath); err == nil {
				if info.Mode()&os.ModeSymlink == os.ModeSymlink {
					err := os.Remove(vendorPath)
					if err != nil {
						panic(err)
					}
				}
				continue
			}

			// needed?
			_ = os.Chmod(vendorPath, 0755)

			for parentDir := filepath.Dir(vendorPath); parentDir != "vendor"; parentDir = filepath.Dir(parentDir) {
				_ = os.Chmod(parentDir, 0755)
			}

			// create parent folder if not exists
			vendorDir := filepath.Dir(vendorPath)
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

			fmt.Println("symlink created", vendorPath)
		}
	}
}
