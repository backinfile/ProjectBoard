package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"projectboard/internal/domain"
	"sort"
	"testing"
	"time"
)

func TestTaskFieldCommentAndLabelLifecycle(t *testing.T) {
	f := setup(t)
	f.seed()
	v, e := f.call("create_task", map[string]any{"title": "完整字段", "fields": map[string]any{"description": "详细", "priority": "紧急", "start": "2026-09-05", "end": "2026-09-06", "due": "2026-09-06", "progress": float64(0), "repeat": "daily", "color": "#123456", "tags": []string{"重点"}}})
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(v["data"])
	var task domain.Task
	json.Unmarshal(raw, &task)
	if task.Status != "created" || task.Progress == nil || *task.Progress != 0 {
		t.Fatal("defaults / zero progress")
	}
	_, e = f.call("update_task", map[string]any{"id": task.ID, "fields": map[string]any{"priority": nil, "progress": nil}})
	if e != nil {
		t.Fatal(e)
	}
	st, _ := f.s.Store.Read()
	if st.Task(task.ID).Priority != "" || st.Task(task.ID).Progress != nil {
		t.Fatal("explicit null did not clear")
	}
	a, e := f.call("upload_comment_attachment", map[string]any{"task_id": task.ID, "filename": "sample.md", "mime": "text/markdown", "data": base64.StdEncoding.EncodeToString([]byte("# hello"))})
	if e != nil {
		t.Fatal(e)
	}
	attachment := a["data"].(domain.Attachment)
	v, e = f.call("create_task_comment", map[string]any{"task_id": task.ID, "text": "评论", "attachments": []string{attachment.ID}})
	if e != nil {
		t.Fatal(e)
	}
	c := v["data"].(domain.Comment)
	for _, name := range []string{"update_task_comment", "delete_task_comment", "restore_task_comment"} {
		_, e = f.call(name, map[string]any{"task_id": task.ID, "comment_id": c.ID, "text": "更新内容"})
		if e != nil {
			t.Fatal(name, e)
		}
	}
	_, e = f.call("get_comment_attachment", map[string]any{"task_id": task.ID, "attachment_id": attachment.ID})
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"delete_task", "restore_task"} {
		if _, e = f.call(name, map[string]any{"id": task.ID}); e != nil {
			t.Fatal(e)
		}
	}
	_, e = f.call("create_label", map[string]any{"name": "独立标签"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.call("rename_label", map[string]any{"name": "独立标签", "new_name": "重点"})
	if e == nil {
		t.Fatal("implicit merge")
	}
	_, e = f.call("rename_label", map[string]any{"name": "独立标签", "new_name": "重点", "merge": true})
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.call("delete_label", map[string]any{"name": "重点"})
	if e != nil {
		t.Fatal(e)
	}
	st, _ = f.s.Store.Read()
	if len(st.Task(task.ID).Tags) != 0 || len(st.Task(task.ID).Notes) != 1 {
		t.Fatal("label delete removed content or retained label")
	}
}
func TestDeletePlanExpiryPaginationAndAtomicRestore(t *testing.T) {
	f := setup(t)
	f.seed()
	v, e := f.call("preview_delete", map[string]any{"node_ids": []string{"n1"}})
	if e != nil {
		t.Fatal(e)
	}
	id := v["data"].(map[string]any)["plan_id"].(string)
	plan := f.s.plans[id]
	plan.Expires = time.Now().Add(-time.Second)
	f.s.plans[id] = plan
	if _, e = f.call("commit_delete", map[string]any{"plan_id": id}); e == nil {
		t.Fatal("expired plan committed")
	}
	v, e = f.call("list_knowledge_nodes", map[string]any{"limit": float64(1)})
	if e != nil || v["next_cursor"] == "" {
		t.Fatal("pagination")
	}
	cursor := v["next_cursor"]
	v, e = f.call("list_knowledge_nodes", map[string]any{"limit": float64(1), "cursor": cursor})
	if e != nil || len(v["items"].([]any)) != 1 {
		t.Fatal("next page")
	}
	_, e = f.call("update_project", map[string]any{"fields": map[string]any{"description": "更新"}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.call("list_knowledge_nodes", map[string]any{"cursor": cursor}); e == nil {
		t.Fatal("stale cursor accepted")
	}
	st, _ := f.s.Store.Read()
	bad := domain.Clone(st)
	bad.Tasks[0].Knowledge = []string{"absent"}
	b, _ := json.Marshal(bad)
	if _, e = f.s.Store.Restore(b, st.Revision); e == nil {
		t.Fatal("broken reference restore accepted")
	}
	after, _ := f.s.Store.Read()
	if after.Revision != st.Revision {
		t.Fatal("failed restore mutated state")
	}
	legacy := domain.Clone(st)
	legacy.Tasks[0].DetailAttachments = []domain.Attachment{{ID: "empty", Name: "empty.txt", Type: "text/plain", Size: 0, Data: ""}}
	b, _ = json.Marshal(legacy)
	after, e = f.s.Store.Restore(b, st.Revision)
	if e != nil {
		t.Fatal("empty legacy attachment", e)
	}
	_, content, e := f.s.Store.Attachment("p1", after.Tasks[0].DetailAttachments[0].ID)
	if e != nil || len(content) != 0 {
		t.Fatal("empty attachment content")
	}
}
func TestPerformanceFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("performance fixture")
	}
	f := setup(t)
	st := sample()
	st.Tasks = []domain.Task{}
	st.Nodes = []domain.Node{}
	for i := 0; i < 10000; i++ {
		st.Tasks = append(st.Tasks, domain.NewTask("p1", fmt.Sprintf("task-%d", i), fmt.Sprintf("任务 %d 中文查询", i), i+1))
	}
	for i := 0; i < 2000; i++ {
		st.Nodes = append(st.Nodes, domain.Node{ID: fmt.Sprintf("node-%d", i), Project: "p1", Name: fmt.Sprintf("知识%d", i), Content: "中文知识内容", Tags: []string{}})
	}
	start := time.Now()
	_, e := f.s.Store.Save(st)
	if e != nil {
		t.Fatal(e)
	}
	t.Log("initial commit", time.Since(start))
	times := []time.Duration{}
	for i := 0; i < 20; i++ {
		start = time.Now()
		_, e = f.call("search", map[string]any{"query": "中文", "limit": float64(50)})
		if e != nil {
			t.Fatal(e)
		}
		times = append(times, time.Since(start))
	}
	var total time.Duration
	for _, d := range times {
		total += d
	}
	t.Logf("10000 tasks + 2000 nodes, 20 searches average %s", total/20)
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	t.Logf("search P95 %s", times[18])
	r, b := f.request("GET", "/api/revision", nil)
	if r.StatusCode != 200 || len(b) > 100 || !bytes.Contains(b, []byte("revision")) {
		t.Fatal("revision polling must be compact")
	}
}
