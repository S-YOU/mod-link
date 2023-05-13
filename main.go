package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type modItem struct {
	mod      []byte
	ver      []byte
	fullPath string
}

type subPackagePair struct {
	mod  string
	name string
}

var defaultSubPackages = []subPackagePair{
	{"cloud.google.com/go", "civil"},
	{"go.opentelemetry.io/otel", "*"},
	{"github.com/GoogleCloudPlatform/opentelemetry-operations-go", "propagator"},
}

var (
	subPackages = flag.String("sub-packages", "", "sub packages, comma seperated")
)

func main() {
	flag.Parse()
	if _, err := os.Stat("go.sum"); os.IsNotExist(err) {
		panic(err)
	}
	if *subPackages != "" {
		for _, pkg := range strings.Split(*subPackages, ",") {
			pair := strings.LastIndexByte(pkg, '/')
			if pair < 0 {
				panic(fmt.Errorf("sub-packages should contain slash"))
			}
			k, v := pkg[:pair], pkg[pair+1:]
			found := false
			for _, vv := range defaultSubPackages {
				if vv.mod == k && vv.name == v {
					found = true
				}
			}
			if !found {
				fmt.Println("sub-package", pkg)
				defaultSubPackages = append(defaultSubPackages, subPackagePair{k, v})
			}
		}
	}

	data, err := os.ReadFile("go.sum")
	if err != nil {
		panic(err)
	}

	if _, err := os.Stat("go.mod"); os.IsNotExist(err) {
		panic(err)
	}
	gomod, err := os.ReadFile("go.mod")
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
		//fmt.Printf("%s\n", x[1])
		buf.Write(x[1])
		buf.WriteString("\n")
	}
	//if err := os.WriteFile("vendor/modules.txt", buf.Bytes(), 0644); err != nil {
	//	panic(err)
	//}

	modPath := filepath.Join(os.Getenv("GOPATH"), "pkg", "mod")

	modItems := make(map[string]modItem, 0)
	lines := bytes.Split(data, []byte("\n"))
	//first:
	for _, line := range lines {
		items := bytes.SplitN(line, []byte(" "), 3)
		if len(items) != 3 {
			continue
		}

		// skip unless has suffix /go.mod
		if !bytes.HasSuffix(items[1], []byte("/go.mod")) {
			continue
		}

		ver := bytes.SplitN(items[1], []byte("/"), 2)[0]
		if bytes.Contains(gomod, items[0]) {
			pkgVer := make([]byte, 0)
			pkgVer = append(pkgVer, items[0]...)
			pkgVer = append(pkgVer, ' ')
			pkgVer = append(pkgVer, ver...)
			if !bytes.Contains(gomod, pkgVer) {
				continue
			}
		}

		multipleMod := make([]byte, 0)
		multipleMod = append(multipleMod, items[0]...)
		multipleMod = append(multipleMod, '/')
		//for _, l := range lines {
		//	if bytes.HasPrefix(l, multipleMod) {
		//		fmt.Println("line", string(line))
		//		continue first
		//	}
		//}

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

		modItems[string(items[0])] = modItem{mod: items[0], ver: ver, fullPath: fullPath}
	}

	for _, v := range defaultSubPackages {
		appendItems := make([]modItem, 0)
		for modName, x := range modItems {
			if modName == v.mod {
				var names []string
				if v.name == "*" {
					dirs, err := os.ReadDir(x.fullPath)
					if err != nil {
						panic(err)
					}
					for _, d := range dirs {
						name := d.Name()
						if name[0] != '.' && (d.IsDir() || (strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go"))) {
							names = append(names, name)
						}
					}
				} else {
					names = append(names, v.name)
				}
				for _, name := range names {
					subPkgPath := filepath.Join(x.fullPath, name)
					if _, err := os.Lstat(subPkgPath); err == nil {
						relPath := filepath.Join(string(x.mod), name)
						appendItems = append(appendItems, modItem{
							[]byte(relPath), x.ver, subPkgPath,
						})
					}
				}
			}
		}
		for _, x := range appendItems {
			modItems[string(x.mod)] = x
		}
	}

	for modName, modItem := range modItems {
		links := []string{modName}
		if val, ok := replaceMap[modName]; ok {
			links = append(links, val)
		}

		for _, link := range links {
			// skip symlink already exists
			vendorPath := filepath.Join("vendor", link)
			if info, err := os.Lstat(vendorPath); err == nil {
				if info.Mode()&os.ModeSymlink == os.ModeSymlink {
					resolved, err := os.Readlink(vendorPath)
					if err != nil {
						panic(err)
					}
					if modItem.fullPath == resolved {
						continue
					} else {
						err = os.Remove(vendorPath)
						if err != nil {
							panic(err)
						}
					}
				} else {
					continue
				}
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
			err := os.Symlink(modItem.fullPath, vendorPath)
			if err != nil {
				panic(err)
			}

			fmt.Println("symlink created", modItem.fullPath, vendorPath)
		}
	}
}
