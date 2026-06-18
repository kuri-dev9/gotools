package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gtools/pkg/cli"
	"gtools/pkg/version"

	"github.com/spf13/pflag"
)

type Node struct {
	Name     string  `json:"name"`
	Path     string  `json:"path"`
	IsDir    bool    `json:"is_dir"`
	Size     int64   `json:"size"`
	Children []*Node `json:"children,omitempty"`
}

const version_info = "1.0.0"

var (
	maxDepth  = pflag.IntP("level", "L", -1, "max display depth")
	showAll   = pflag.BoolP("all", "a", false, "show hidden files")
	dirOnly   = pflag.BoolP("dir", "d", false, "directories only")
	fullPath  = pflag.BoolP("full", "f", false, "print full path")
	showSize  = pflag.BoolP("size", "s", false, "show file size")
	humanSize = pflag.Bool("human", false, "human readable size")
	sortBy    = pflag.String("sort", "name", "sort by name|size")
	jsonOut   = pflag.Bool("json", false, "json output")
	showSum   = pflag.Bool("summary", false, "show summary")

	helpFlag    = pflag.BoolP("help", "h", false, "show help")
	versionFlag = pflag.BoolP("version", "v", false, "show version")
)

var totalSize int64
var totalFiles int

func init() {
    runtime.GOMAXPROCS(1)
}

func main() {
	pflag.Usage = func() {
		cli.PrintHelp("gtree", helpText)
	}

	pflag.Parse()

	// =====================
	// help / version
	// =====================
	if *helpFlag {
		cli.PrintHelp("gtree", helpText)
		return
	}

	if *versionFlag {
		version.Version = version_info
		version.Print()
		return
	}

	root := "."
	if pflag.NArg() > 0 {
		root = pflag.Arg(0)
	}

	// =====================
	// build tree
	// =====================
	node, err := buildTree(root, 0)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	if *jsonOut {
		out, _ := json.MarshalIndent(node, "", "  ")
		fmt.Println(string(out))
		return
	}

	fmt.Println(root)
	printTree(node, "", true)

	if *showSum {
		fmt.Printf("\nTotal: %d files, %s\n", totalFiles, formatSize(totalSize))
	}
}

func buildTree(path string, depth int) (*Node, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	node := &Node{
		Name:  info.Name(),
		Path:  path,
		IsDir: info.IsDir(),
		Size:  info.Size(),
	}

	if !info.IsDir() {
		totalFiles++
		totalSize += info.Size()
		return node, nil
	}

	if *maxDepth >= 0 && depth >= *maxDepth {
		return node, nil
	}

	entries, err := ioutil.ReadDir(path)
	if err != nil {
		return node, nil
	}

	var children []*Node

	for _, e := range entries {
		name := e.Name()

		if !*showAll && strings.HasPrefix(name, ".") {
			continue
		}

		full := filepath.Join(path, name)

		child, err := buildTree(full, depth+1)
		if err != nil {
			continue
		}

		if *dirOnly && !child.IsDir {
			continue
		}

		children = append(children, child)
	}

	sortNodes(children)

	node.Children = children
	return node, nil
}

func sortNodes(nodes []*Node) {
	switch *sortBy {
	case "size":
		sort.Slice(nodes, func(i, j int) bool {
			return nodes[i].Size > nodes[j].Size
		})
	default:
		sort.Slice(nodes, func(i, j int) bool {
			return nodes[i].Name < nodes[j].Name
		})
	}
}

func printTree(node *Node, prefix string, isLast bool) {
	for i, child := range node.Children {
		last := i == len(node.Children)-1

		connector := "├── "
		nextPrefix := prefix + "│   "

		if last {
			connector = "└── "
			nextPrefix = prefix + "    "
		}

		name := child.Name
		if *fullPath {
			name = child.Path
		}

		sizeStr := ""
		if *showSize && !child.IsDir {
			sizeStr = " (" + formatSize(child.Size) + ")"
		}

		fmt.Println(prefix + connector + name + sizeStr)

		if child.IsDir {
			printTree(child, nextPrefix, last)
		}
	}
}

func formatSize(size int64) string {
	if !*humanSize {
		return fmt.Sprintf("%dB", size)
	}

	units := []string{"B", "KB", "MB", "GB"}
	i := 0
	f := float64(size)

	for f > 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}

	return fmt.Sprintf("%.1f%s", f, units[i])
}
