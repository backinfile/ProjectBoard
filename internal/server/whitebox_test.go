package server

import (
	"projectboard/internal/domain"
	"reflect"
	"testing"
)

func TestPatchBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		fields map[string]any
		valid  bool
	}{
		{"W069_补丁省略保留", map[string]any{}, true},
		{"W070_补丁null清空", map[string]any{"priority": nil, "progress": nil}, true},
		{"W071_补丁零值保留", map[string]any{"progress": float64(0)}, true},
		{"W072_补丁未知字段拒绝", map[string]any{"project": "other"}, false},
		{"W073_补丁错误类型拒绝", map[string]any{"priority": true}, false},
		{"W074_补丁小数进度拒绝", map[string]any{"progress": 2.5}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := 5
			task := domain.NewTask("p", "t", "标题", 1)
			task.Priority = "高"
			task.Progress = &n
			before := domain.Clone(task)
			err := patch(&task, c.fields, "priority", "progress")
			if (err == nil) != c.valid {
				t.Fatalf("valid=%v err=%v", c.valid, err)
			}
			if !c.valid && !reflect.DeepEqual(task, before) {
				t.Fatal("failed patch changed target")
			}
			switch c.name {
			case "W069_补丁省略保留":
				if !reflect.DeepEqual(task, before) {
					t.Fatal("omitted field changed")
				}
			case "W070_补丁null清空":
				if task.Priority != "" || task.Progress != nil {
					t.Fatal("null not cleared")
				}
			case "W071_补丁零值保留":
				if task.Progress == nil || *task.Progress != 0 {
					t.Fatal("zero lost")
				}
			}
		})
	}
}
func TestToolArgumentMatrix(t *testing.T) {
	for _, c := range []struct {
		name string
		args map[string]any
	}{
		{"W075_工具未知参数", map[string]any{"unknown": true}},
		{"W076_工具标签类型", map[string]any{"tags": []any{1}}},
		{"W077_工具字段对象类型", map[string]any{"fields": "text"}},
		{"W078_工具版本类型", map[string]any{"expected_revision": "1"}},
		{"W079_工具合并布尔类型", map[string]any{"merge": "yes"}},
		{"W080_工具父节点类型", map[string]any{"parent": true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if validateToolArgs(c.args) == nil {
				t.Fatal("invalid argument accepted")
			}
		})
	}
}
