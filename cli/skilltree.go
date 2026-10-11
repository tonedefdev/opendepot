package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// skillTreeIndent aligns a skill's file tree under its plan row.
const skillTreeIndent = "        "

// fileChange is one file in a skill directory. rel is slash-separated and relative to that directory.
type fileChange struct {
	rel string
	sym string
}

// skillTreeNode is a directory or file in the tree printed for a skill target.
type skillTreeNode struct {
	name     string
	sym      string
	dir      bool
	children []*skillTreeNode
}

// skillFileChanges compares the files a skill would install from root with the files already at dst.
// A missing dst counts as empty, so every planned file is an addition.
func skillFileChanges(root, dst string) ([]fileChange, error) {
	planned, err := listRegularFiles(root)
	if err != nil {
		return nil, err
	}

	installed, err := listRegularFiles(dst)
	if err != nil {
		return nil, err
	}

	wanted := make(map[string]bool, len(planned))
	for _, rel := range planned {
		wanted[rel] = true
	}

	present := make(map[string]bool, len(installed))
	for _, rel := range installed {
		present[rel] = true
	}

	var changes []fileChange
	for _, rel := range planned {
		if !present[rel] {
			changes = append(changes, fileChange{rel: rel, sym: planAdd})
			continue
		}

		same, err := sameFileContent(filepath.Join(dst, filepath.FromSlash(rel)), filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}

		sym := planChange
		if same {
			sym = planSame
		}

		changes = append(changes, fileChange{rel: rel, sym: sym})
	}

	for _, rel := range installed {
		if !wanted[rel] {
			changes = append(changes, fileChange{rel: rel, sym: planRemove})
		}
	}

	return changes, nil
}

// listRegularFiles returns the slash-separated path of every regular file under root. A missing root has none.
func listRegularFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.Type().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}

		files = append(files, filepath.ToSlash(rel))
		return nil
	})

	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	return files, err
}

// hasFileChanges reports whether any file in the skill would be added, changed, or removed.
func hasFileChanges(changes []fileChange) bool {
	for _, c := range changes {
		if c.sym != planSame {
			return true
		}
	}

	return false
}

// skillChanges lists the files a skill target would change, after confirming the target path is safe to read.
func (a *app) skillChanges(s *stagedEntry, t installTarget) ([]fileChange, error) {
	if err := a.ensureNoSymlinks(t.path); err != nil {
		return nil, err
	}

	return skillFileChanges(s.root, a.path(t.path))
}

// printSkillTree prints the skill's directory tree with each file's change.
func (a *app) printSkillTree(changes []fileChange) {
	st := newStyler(a.out)
	for _, line := range skillTreeLines(st, buildSkillTree(changes), skillTreeIndent) {
		fmt.Fprintln(a.out, line)
	}
}

// buildSkillTree nests the changed files by directory. Entries are sorted by name at each level.
func buildSkillTree(changes []fileChange) []*skillTreeNode {
	var roots []*skillTreeNode
	for _, c := range changes {
		parts := strings.Split(c.rel, "/")
		level := &roots
		for i, part := range parts {
			isDir := i < len(parts)-1
			node := findSkillNode(*level, part, isDir)
			if node == nil {
				node = &skillTreeNode{name: part, dir: isDir}
				*level = append(*level, node)
			}

			if !isDir {
				node.sym = c.sym
			}

			level = &node.children
		}
	}

	sortSkillTree(roots)
	return roots
}

func findSkillNode(nodes []*skillTreeNode, name string, dir bool) *skillTreeNode {
	for _, n := range nodes {
		if n.name == name && n.dir == dir {
			return n
		}
	}

	return nil
}

func sortSkillTree(nodes []*skillTreeNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].name < nodes[j].name
	})

	for _, n := range nodes {
		sortSkillTree(n.children)
	}
}

// skillTreeLines renders nodes with box-drawing connectors, the same layout tree uses.
func skillTreeLines(st styler, nodes []*skillTreeNode, prefix string) []string {
	var lines []string
	for i, n := range nodes {
		connector, childPrefix := "├── ", "│   "
		if i == len(nodes)-1 {
			connector, childPrefix = "└── ", "    "
		}

		if n.dir {
			lines = append(lines, prefix+connector+n.name+"/")
			lines = append(lines, skillTreeLines(st, n.children, prefix+childPrefix)...)
			continue
		}

		name := n.name
		if n.sym == planSame {
			name = st.paint(ansiDim, name)
		}

		lines = append(lines, fmt.Sprintf("%s%s%s  %s %s", prefix, connector, name, st.paint(planColor(n.sym), n.sym), st.paint(ansiDim, planNote(n.sym))))
	}

	return lines
}
