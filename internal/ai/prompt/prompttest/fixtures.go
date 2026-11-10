// Package prompttest holds recorded AI answers for the tasks in package
// prompt, for pipeline tests with a fake endpoint (J6.3–J6.6, J7.2–J7.5).
//
// Layout: testdata/<task>/<name>.json is an answer that validates;
// invalid_<name>.json is one the validator must reject; <name>.input.json,
// when present, is the request the answer belongs to (the task's input type
// as JSON). Task names are the AI feature names (item_draft, label, …).
package prompttest

import (
	"embed"
	"encoding/json"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed testdata
var files embed.FS

// Fixture is one recorded answer.
type Fixture struct {
	Task   string
	Name   string          // file name without .json
	Valid  bool            // false for invalid_* answers
	Answer json.RawMessage // the model's JSON answer
	Input  json.RawMessage // the request, or nil
}

// All returns the fixtures of task ("" for all tasks), sorted by task and
// name.
func All(task string) []Fixture {
	var out []Fixture
	_ = fs.WalkDir(files, "testdata", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".json") || strings.HasSuffix(p, ".input.json") {
			return err
		}
		t := path.Base(path.Dir(p))
		if task != "" && t != task {
			return nil
		}
		name := strings.TrimSuffix(path.Base(p), ".json")
		ans, _ := files.ReadFile(p)
		in, err := files.ReadFile(strings.TrimSuffix(p, ".json") + ".input.json")
		if err != nil {
			in = nil
		}
		out = append(out, Fixture{Task: t, Name: name, Valid: !strings.HasPrefix(name, "invalid_"), Answer: ans, Input: in})
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Task != out[j].Task {
			return out[i].Task < out[j].Task
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Answer returns the recorded answer task/name, or nil.
func Answer(task, name string) json.RawMessage {
	b, err := files.ReadFile("testdata/" + task + "/" + name + ".json")
	if err != nil {
		return nil
	}
	return b
}

// Input returns the request recorded with task/name, or nil.
func Input(task, name string) json.RawMessage {
	b, err := files.ReadFile("testdata/" + task + "/" + name + ".input.json")
	if err != nil {
		return nil
	}
	return b
}
