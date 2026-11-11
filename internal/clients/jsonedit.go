package clients

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/tailscale/hujson"
)

// Clients whose config is JSON (Antigravity, and later Gemini CLI, Cursor,
// VS Code) are edited with editJSONServer: it changes only the keys toolshed
// owns in one server's entry and keeps every other byte, comments and
// trailing commas included, as the Codex TOML editor does. New keys and
// servers copy the indentation around them.

// jsonKV is one key toolshed writes in a server's entry, in this order.
type jsonKV struct {
	Key string
	Val any
}

// errJSONLayout means the config isn't shaped the way toolshed edits: the
// servers section or the server isn't a JSON object.
var errJSONLayout = errors.New("unsupported layout")

// editJSONServer sets (set != nil) or removes (set == nil) server name in
// the section object (e.g. "mcpServers") of a JSON or JSONC document. When
// setting, it writes the keys in set, removes the keys in drop, and leaves
// the server's other keys as they are.
func editJSONServer(doc []byte, section, name string, set []jsonKV, drop []string) ([]byte, error) {
	if len(bytes.TrimSpace(doc)) == 0 {
		if set == nil {
			return doc, nil
		}
		doc = []byte("{}\n")
	}
	root, err := hujson.Parse(doc)
	if err != nil {
		return nil, err
	}
	top, ok := root.Value.(*hujson.Object)
	if !ok {
		return nil, fmt.Errorf("%w: the file isn't a JSON object", errJSONLayout)
	}
	unit := indentUnit(top)
	servers := member(top, section)
	if servers == nil {
		if set == nil {
			return doc, nil
		}
		appendMember(top, section, &hujson.Object{}, unit, 1)
		servers = member(top, section)
	}
	sobj, ok := servers.Value.(*hujson.Object)
	if !ok {
		return nil, fmt.Errorf("%w: %s isn't a JSON object", errJSONLayout, section)
	}

	if set == nil {
		for i, m := range sobj.Members {
			if literalName(m.Name) == name {
				removeMember(sobj, i)
				break
			}
		}
		return root.Pack(), nil
	}

	srv := member(sobj, name)
	if srv == nil {
		// A new server: render it whole, in the style around it.
		indent := strings.Repeat(unit, 2)
		if len(sobj.Members) > 0 {
			indent = lineIndent(sobj, "")
		}
		v, err := renderObject(set, indent, unit, multiline(sobj))
		if err != nil {
			return nil, err
		}
		appendMember(sobj, name, v, unit, 2)
		return root.Pack(), nil
	}
	obj, ok := srv.Value.(*hujson.Object)
	if !ok {
		return nil, fmt.Errorf("%w: %s.%s isn't a JSON object", errJSONLayout, section, name)
	}
	for _, k := range drop {
		for i, m := range obj.Members {
			if literalName(m.Name) == k {
				removeMember(obj, i)
				break
			}
		}
	}
	for _, kv := range set {
		if v := member(obj, kv.Key); v != nil {
			nv, err := jsonValue(kv.Val, lineIndent(obj, kv.Key), unit, multiline(obj))
			if err != nil {
				return nil, err
			}
			v.Value = nv.Value
			continue
		}
		appendMember(obj, kv.Key, kv.Val, unit, 3)
	}
	return root.Pack(), nil
}

// member returns the value of obj's member called name, or nil.
func member(obj *hujson.Object, name string) *hujson.Value {
	for i := range obj.Members {
		if literalName(obj.Members[i].Name) == name {
			return &obj.Members[i].Value
		}
	}
	return nil
}

func literalName(v hujson.Value) string {
	if lit, ok := v.Value.(hujson.Literal); ok {
		return lit.String()
	}
	return ""
}

// multiline reports whether obj puts its members on their own lines (an
// empty object gets them that way).
func multiline(obj *hujson.Object) bool {
	return len(obj.Members) == 0 || bytes.Contains(obj.Members[0].Name.BeforeExtra, []byte("\n"))
}

// lineIndent is the indentation of obj's members (of key, when it's there).
func lineIndent(obj *hujson.Object, key string) string {
	for _, m := range obj.Members {
		if key == "" || literalName(m.Name) == key {
			b := m.Name.BeforeExtra
			if i := bytes.LastIndexByte(b, '\n'); i >= 0 && len(bytes.TrimLeft(b[i+1:], " \t")) == 0 {
				return string(b[i+1:])
			}
		}
	}
	return ""
}

// indentUnit is the file's indentation step: what its top-level members
// are indented by, or two spaces.
func indentUnit(top *hujson.Object) string {
	if s := lineIndent(top, ""); s != "" {
		return s
	}
	return "  "
}

// appendMember adds name: val at the end of obj, at nesting depth (1 for
// the top-level object's members), in the style of obj's other members: on
// its own line, indented like them (not copying a comment above the last
// one), or after a space in a one-line object. val is a Go value, or a
// hujson.Value / *hujson.Object already rendered.
func appendMember(obj *hujson.Object, name string, val any, unit string, depth int) {
	indent := strings.Repeat(unit, depth)
	multi := multiline(obj)
	before := []byte("\n" + indent)
	switch {
	case len(obj.Members) == 0:
		if len(bytes.TrimSpace(obj.AfterExtra)) == 0 { // keep a comment in "{ /* … */ }"
			obj.AfterExtra = []byte("\n" + strings.Repeat(unit, depth-1))
		}
	case multi:
		indent = lineIndent(obj, "")
		before = []byte("\n" + indent)
	default:
		before = []byte(" ")
	}
	var v hujson.Value
	switch val := val.(type) {
	case *hujson.Object:
		v = hujson.Value{Value: val}
	case hujson.Value:
		v = val
	default:
		v, _ = jsonValue(val, indent, unit, multi)
	}
	v.BeforeExtra = []byte(" ")
	trailing := false
	if n := len(obj.Members); n > 0 && obj.Members[n-1].Value.AfterExtra != nil {
		trailing = true // keep the file's trailing comma on the new last member
	}
	if trailing {
		v.AfterExtra = []byte{}
	}
	obj.Members = append(obj.Members, hujson.ObjectMember{
		Name:  hujson.Value{BeforeExtra: before, Value: hujson.String(name)},
		Value: v,
	})
}

// removeMember removes obj's i-th member, keeping a trailing comma on the
// new last member if the file had one.
func removeMember(obj *hujson.Object, i int) {
	last := i == len(obj.Members)-1
	after := obj.Members[i].Value.AfterExtra
	obj.Members = append(obj.Members[:i], obj.Members[i+1:]...)
	if n := len(obj.Members); n == 0 {
		obj.AfterExtra = nil
	} else if last {
		obj.Members[n-1].Value.AfterExtra = after
	}
}

// renderObject renders the keys of a new object, in order: one per line
// (indent is the indentation of the line it starts on) when multi, else on
// one line.
func renderObject(kvs []jsonKV, indent, unit string, multi bool) (hujson.Value, error) {
	var b strings.Builder
	b.WriteString("{")
	for i, kv := range kvs {
		key, _ := json.Marshal(kv.Key)
		var val []byte
		var err error
		if multi {
			val, err = json.MarshalIndent(kv.Val, indent+unit, unit)
		} else {
			val, err = json.Marshal(kv.Val)
		}
		if err != nil {
			return hujson.Value{}, err
		}
		switch {
		case multi:
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString("\n" + indent + unit)
		case i > 0:
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s: %s", key, val)
	}
	if multi && len(kvs) > 0 {
		b.WriteString("\n" + indent)
	}
	b.WriteString("}")
	v, err := hujson.Parse([]byte(b.String()))
	if err != nil {
		return hujson.Value{}, err
	}
	v.BeforeExtra = []byte(" ")
	return v, nil
}

// jsonValue renders val as a hujson value: indented like its surroundings
// (indent is the indentation of the line the key is on) when multi, else
// on one line.
func jsonValue(val any, indent, unit string, multi bool) (hujson.Value, error) {
	var data []byte
	var err error
	if multi {
		data, err = json.MarshalIndent(val, indent, unit)
	} else {
		data, err = json.Marshal(val)
	}
	if err != nil {
		return hujson.Value{}, err
	}
	v, err := hujson.Parse(data)
	if err != nil {
		return hujson.Value{}, err
	}
	v.BeforeExtra = []byte(" ")
	return v, nil
}

// readJSONC decodes a JSON or JSONC file into v. A missing file is empty.
func readJSONC(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	std, err := hujson.Standardize(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(std, v)
}

// editJSON is editJSONServer; tests swap it for a faulty one to check that
// writeJSONServer catches a bad edit.
var editJSON = editJSONServer

// writeJSONServer applies editJSONServer to a config file, checks that the
// result parses, that the server says what was intended and that nothing
// else in the file changed, and replaces the file atomically.
func writeJSONServer(path, section, name string, set []jsonKV, drop []string) error {
	doc, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	out, err := editJSON(doc, section, name, set, drop)
	if err != nil {
		return fmt.Errorf("editing %s: %w", path, err)
	}
	if err := checkJSONEdit(doc, out, section, name, set, drop); err != nil {
		return fmt.Errorf("editing %s: %w (nothing was written)", path, err)
	}
	return writeFileAtomic(path, out)
}

// checkJSONEdit compares the documents before and after an edit: the server
// has the keys in set (or is gone, for set == nil) and none in drop, keeps
// its other keys, and the rest of the document is the same.
func checkJSONEdit(before, after []byte, section, name string, set []jsonKV, drop []string) error {
	decode := func(doc []byte) (map[string]any, error) {
		m := map[string]any{}
		if len(bytes.TrimSpace(doc)) == 0 {
			return m, nil
		}
		std, err := hujson.Standardize(doc)
		if err != nil {
			return nil, err
		}
		return m, json.Unmarshal(std, &m)
	}
	old, err := decode(before)
	if err != nil {
		return err
	}
	got, err := decode(after)
	if err != nil {
		return fmt.Errorf("produced invalid JSON: %w", err)
	}
	oldServers, _ := old[section].(map[string]any)
	gotServers, _ := got[section].(map[string]any)
	oldSrv, _ := oldServers[name].(map[string]any)
	gotSrv, hasSrv := gotServers[name].(map[string]any)
	delete(old, section)
	delete(got, section)
	if !reflect.DeepEqual(old, got) {
		return errors.New("other settings changed")
	}
	for k, v := range oldServers {
		if k != name && !reflect.DeepEqual(v, gotServers[k]) {
			return fmt.Errorf("server %s changed", k)
		}
	}
	for k := range gotServers {
		if _, ok := oldServers[k]; !ok && k != name {
			return fmt.Errorf("server %s appeared", k)
		}
	}
	if set == nil {
		if hasSrv {
			return fmt.Errorf("%s is still there", name)
		}
		return nil
	}
	if !hasSrv {
		return fmt.Errorf("%s isn't there", name)
	}
	want := map[string]any{}
	for k, v := range oldSrv {
		want[k] = v
	}
	for _, k := range drop {
		delete(want, k)
	}
	for _, kv := range set {
		data, _ := json.Marshal(kv.Val)
		var v any
		_ = json.Unmarshal(data, &v)
		want[kv.Key] = v
	}
	if !reflect.DeepEqual(want, gotSrv) {
		return fmt.Errorf("%s isn't what was intended", name)
	}
	return nil
}

// writeFileAtomic replaces path with data through a temporary file in the
// same directory, keeping the file's mode (0600 for a new file).
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
