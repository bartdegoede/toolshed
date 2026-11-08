package registry

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const userFileHeader = `# Your toolshed. Add tools with "toolshed add" or edit by hand.
# Schema: https://github.com/bartdegoede/toolshed/blob/main/docs/registry.md
`

// AddUserEntry validates e and appends it to the user registry, replacing
// any existing user entry with the same ID. Comments elsewhere in the file
// are preserved.
func AddUserEntry(e Entry) error {
	e.Source = ""
	if err := ValidateEntry(&e); err != nil {
		return err
	}
	doc, tools, err := loadUserDoc()
	if err != nil {
		return err
	}
	var node yaml.Node
	if err := node.Encode(e); err != nil {
		return err
	}
	if i := indexOfID(tools, e.ID); i >= 0 {
		tools.Content[i] = &node
	} else {
		tools.Content = append(tools.Content, &node)
	}
	return saveUserDoc(doc)
}

// RemoveUserEntry deletes a tool from the user registry. Built-in examples
// can't be removed (hide them all with `examples: false`).
func RemoveUserEntry(id string) error {
	doc, tools, err := loadUserDoc()
	if err != nil {
		return err
	}
	i := indexOfID(tools, id)
	if i < 0 {
		return fmt.Errorf("%s is not in your registry (%s)", id, UserPath())
	}
	tools.Content = append(tools.Content[:i], tools.Content[i+1:]...)
	return saveUserDoc(doc)
}

// ApproveUserEntry clears the pending flag on a tool an agent added, so it
// can be started and installed.
func ApproveUserEntry(id string) error {
	doc, tools, err := loadUserDoc()
	if err != nil {
		return err
	}
	i := indexOfID(tools, id)
	if i < 0 {
		return fmt.Errorf("%s is not in your registry (%s)", id, UserPath())
	}
	item := tools.Content[i]
	for j := 0; j+1 < len(item.Content); j += 2 {
		if item.Content[j].Value == "pending" {
			item.Content = append(item.Content[:j], item.Content[j+2:]...)
			break
		}
	}
	return saveUserDoc(doc)
}

// EnsureUserFile creates the user registry (with an explanatory header) if
// it doesn't exist yet.
func EnsureUserFile() error {
	if _, err := os.Stat(UserPath()); err == nil {
		return nil
	}
	doc, _, err := loadUserDoc()
	if err != nil {
		return err
	}
	return saveUserDoc(doc)
}

// loadUserDoc returns the user registry as a YAML node tree plus its `tools`
// sequence node, creating an empty document if the file doesn't exist.
func loadUserDoc() (*yaml.Node, *yaml.Node, error) {
	data, err := os.ReadFile(UserPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte(userFileHeader + "tools: []\n")
	}
	if _, err := Parse(data); err != nil {
		return nil, nil, fmt.Errorf("%s is invalid, fix it before adding tools: %w", UserPath(), err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, err
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "tools" {
			seq := root.Content[i+1]
			seq.Style = 0 // render `tools: []` as a block list once it has items
			return &doc, seq, nil
		}
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "tools"}, seq)
	return &doc, seq, nil
}

func indexOfID(tools *yaml.Node, id string) int {
	for i, item := range tools.Content {
		for j := 0; j+1 < len(item.Content); j += 2 {
			if item.Content[j].Value == "id" && item.Content[j+1].Value == id {
				return i
			}
		}
	}
	return -1
}

// saveUserDoc writes atomically (temp file + rename) so an interrupted save
// never leaves a half-written registry.
func saveUserDoc(doc *yaml.Node) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return err
	}
	path := UserPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".registry-*.yaml")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after a successful rename
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
