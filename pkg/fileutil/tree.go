package fileutil

import (
	"os"
	"path/filepath"
	"strings"
)

type Node struct {
	Name     string
	Path     string
	IsDir    bool
	Size     int64
	Children []*Node
}

func BuildTree(path string, showAll bool, depth, maxDepth int) (*Node, error) {
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
		return node, nil
	}

	if maxDepth >= 0 && depth >= maxDepth {
		return node, nil
	}

	entries, _ := os.ReadDir(path)

	for _, e := range entries {
		name := e.Name()

		if !showAll && strings.HasPrefix(name, ".") {
			continue
		}

		full := filepath.Join(path, name)

		child, err := BuildTree(full, showAll, depth+1, maxDepth)
		if err != nil {
			continue
		}

		node.Children = append(node.Children, child)
	}

	return node, nil
}
