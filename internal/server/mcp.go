package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"projectboard/internal/domain"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

type DeletePlan struct {
	Project  string
	Revision int64
	Expires  time.Time
	Tasks    []string
	Nodes    []string
}

var toolNames = map[string]string{
	"get_project": "读取项目", "update_project": "更新项目", "list_project_statuses": "读取任务状态",
	"list_tasks": "查找任务", "get_task": "读取任务", "create_task": "创建任务", "update_task": "更新任务字段", "move_task": "更新任务状态与排序", "delete_task": "任务移入回收站", "restore_task": "恢复任务",
	"list_task_comments": "读取评论", "create_task_comment": "发表评论", "update_task_comment": "编辑评论", "delete_task_comment": "删除评论", "restore_task_comment": "恢复评论", "upload_comment_attachment": "上传附件（Base64）", "get_comment_attachment": "读取附件（Base64）",
	"list_knowledge_nodes": "查找知识节点", "get_knowledge_node": "按 ID 或点分地址读取知识", "get_knowledge_subtree": "读取知识子树", "create_knowledge_node": "创建知识节点", "update_knowledge_node": "更新知识内容、名称与标签", "move_knowledge_node": "移动知识节点", "restore_knowledge_node": "恢复知识子树",
	"link_task_knowledge": "关联任务与知识", "unlink_task_knowledge": "移除任务知识关联", "link_knowledge_nodes": "关联知识节点", "unlink_knowledge_nodes": "移除知识关联",
	"list_labels": "读取项目标签", "create_label": "创建项目标签", "rename_label": "改名或合并标签", "delete_label": "移除项目标签", "attach_labels": "为选中内容添加标签", "detach_labels": "为选中内容移除标签",
	"search": "搜索任务与知识", "preview_delete": "生成完整删除范围，交由用户确认", "commit_delete": "提交已确认且有效的删除计划",
}

func ToolDefinitions() []*mcp.Tool {
	names := []string{}
	for n := range toolNames {
		names = append(names, n)
	}
	sort.Strings(names)
	out := []*mcp.Tool{}
	for _, name := range names {
		properties := map[string]any{}
		for _, k := range []string{"project_id", "id", "task_id", "node_id", "other_id", "comment_id", "attachment_id", "address", "title", "content", "text", "name", "new_name", "target_type", "type", "query", "match", "cursor", "plan_id", "data", "filename", "mime"} {
			properties[k] = map[string]any{"type": "string"}
		}
		properties["expected_revision"] = map[string]any{"type": "integer", "description": "最新读取结果中的 revision；所有写操作必填"}
		properties["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 200}
		for _, k := range []string{"tags", "target_ids", "task_ids", "node_ids", "attachments"} {
			properties[k] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		}
		properties["parent"] = map[string]any{"type": []string{"string", "null"}}
		properties["fields"] = map[string]any{"type": "object", "description": "任务字段：description,status,priority,start,due,end,progress,reminder,repeat,color,tags,knowledge,relations,checks,detailAttachments；项目：name,code,description,directory,archived；节点：name,content,tags,parent,pinned,order。省略保持，null 清空标量。"}
		properties["merge"] = map[string]any{"type": "boolean"}
		properties["order"] = map[string]any{"type": "number"}
		properties["status"] = map[string]any{"type": []string{"string", "null"}}
		properties["reply_to"] = map[string]any{"type": []string{"string", "null"}}
		required := []string{"project_id"}
		if isWrite(name) {
			required = append(required, "expected_revision")
		}
		out = append(out, &mcp.Tool{Name: name, Description: toolNames[name] + "。所有对象限当前令牌绑定项目。读取返回 revision；列表使用 limit/cursor。", InputSchema: map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}})
	}
	return out
}
func isWrite(name string) bool {
	return !strings.HasPrefix(name, "get_") && !strings.HasPrefix(name, "list_") && name != "search" && name != "preview_delete"
}
func (s *Server) mcpHandler() http.Handler {
	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		project, _ := s.TokenProject(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		return s.MCPServer(project)
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 32 << 20})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if !strings.HasPrefix(token, "Bearer ") {
			fail(w, 401, "请提供项目接入令牌")
			return
		}
		p, e := s.TokenProject(strings.TrimPrefix(token, "Bearer "))
		if e != nil {
			fail(w, 401, "请使用有效项目令牌")
			return
		}
		if header := r.Header.Get("X-Project-ID"); header != "" && header != p {
			fail(w, 403, "PROJECT_SCOPE_MISMATCH")
			return
		}
		handler.ServeHTTP(w, r)
	})
}
func (s *Server) MCPServer(project string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "ProjectBoard", Version: Version}, nil)
	for _, t := range ToolDefinitions() {
		name := t.Name
		server.AddTool(t, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args map[string]any
			if e := json.Unmarshal(req.Params.Arguments, &args); e != nil {
				return nil, e
			}
			v, e := s.Call(project, name, args)
			res := &mcp.CallToolResult{}
			if e != nil {
				res.SetError(e)
				return res, nil
			}
			b, _ := json.Marshal(v)
			res.Content = []mcp.Content{&mcp.TextContent{Text: string(b)}}
			res.StructuredContent = v
			return res, nil
		})
	}
	return server
}
func str(a map[string]any, k string) string { v, _ := a[k].(string); return v }
func list(a map[string]any, k string) []string {
	out := []string{}
	if xs, ok := a[k].([]any); ok {
		for _, v := range xs {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
	}
	if xs, ok := a[k].([]string); ok {
		return xs
	}
	return out
}
func fields(a map[string]any) map[string]any {
	v, _ := a["fields"].(map[string]any)
	if v == nil {
		return map[string]any{}
	}
	return v
}
func patch(target any, values map[string]any, allowed ...string) error {
	raw, _ := json.Marshal(target)
	var obj map[string]any
	_ = json.Unmarshal(raw, &obj)
	for k, v := range values {
		if !domain.In(k, allowed...) {
			return fmt.Errorf("INVALID_ARGUMENT：请选择可编辑字段 %s", k)
		}
		obj[k] = v
	}
	b, e := json.Marshal(obj)
	if e != nil {
		return e
	}
	next := reflect.New(reflect.ValueOf(target).Elem().Type())
	if e = json.Unmarshal(b, next.Interface()); e != nil {
		return fmt.Errorf("INVALID_ARGUMENT：请检查字段类型")
	}
	reflect.ValueOf(target).Elem().Set(next.Elem())
	return nil
}
func remove(xs []string, v string) []string {
	out := []string{}
	for _, x := range xs {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
func (s *Server) Call(project, name string, a map[string]any) (any, error) {
	if e := validateToolArgs(a); e != nil {
		return nil, e
	}
	if project == "" || str(a, "project_id") != project {
		return nil, fmt.Errorf("PROJECT_SCOPE_MISMATCH：请指定当前项目")
	}
	if _, ok := toolNames[name]; !ok {
		return nil, fmt.Errorf("METHOD_NOT_FOUND")
	}
	st, e := s.Store.Read()
	if e != nil {
		return nil, e
	}
	if st.Project(project) == nil {
		return nil, fmt.Errorf("NOT_FOUND：请选择有效项目")
	}
	var result any
	task := func(x *domain.State, id string) (*domain.Task, error) {
		t := x.Task(id)
		if t == nil || t.Project != project {
			return nil, fmt.Errorf("NOT_FOUND：请选择当前项目任务")
		}
		return t, nil
	}
	node := func(x *domain.State, id string) (*domain.Node, error) {
		n := x.Node(id)
		addr := str(a, "address")
		if id == "" && addr != "" {
			for i := range x.Nodes {
				if x.Nodes[i].Project == project && strings.EqualFold(x.Address(&x.Nodes[i]), domain.Clean(addr)) {
					n = &x.Nodes[i]
					break
				}
			}
		}
		if n == nil || n.Project != project {
			return nil, fmt.Errorf("NOT_FOUND：请选择当前项目知识")
		}
		if addr != "" && !strings.EqualFold(x.Address(n), domain.Clean(addr)) {
			return nil, fmt.Errorf("INVALID_ARGUMENT：请使用同一节点的 ID 与地址")
		}
		return n, nil
	}
	matches := func(tags []string, text string) bool {
		if q := str(a, "query"); q != "" && !strings.Contains(strings.ToLower(text), strings.ToLower(q)) {
			return false
		}
		ts := list(a, "tags")
		if len(ts) == 0 {
			return true
		}
		count := 0
		for _, t := range ts {
			if domain.In(t, tags...) {
				count++
			}
		}
		return count == len(ts) || str(a, "match") == "any" && count > 0
	}
	wrapNode := func(x *domain.State, n *domain.Node) map[string]any {
		b, _ := json.Marshal(n)
		var v map[string]any
		json.Unmarshal(b, &v)
		v["address"] = x.Address(n)
		return v
	}
	page := func(items []any) (any, error) {
		limit := 50
		if n, ok := a["limit"].(float64); ok {
			if n < 1 || n > 200 || n != float64(int(n)) {
				return nil, fmt.Errorf("INVALID_ARGUMENT：limit 为 1–200")
			}
			limit = int(n)
		}
		offset := 0
		if cursor := str(a, "cursor"); cursor != "" {
			parts := strings.Split(cursor, ":")
			if len(parts) != 2 || parts[0] != strconv.FormatInt(st.Revision, 10) {
				return nil, fmt.Errorf("REVISION_CONFLICT：请重新开始分页")
			}
			offset, e = strconv.Atoi(parts[1])
			if e != nil || offset < 0 || offset > len(items) {
				return nil, fmt.Errorf("INVALID_ARGUMENT：请检查 cursor")
			}
		}
		end := min(offset+limit, len(items))
		next := ""
		if end < len(items) {
			next = fmt.Sprintf("%d:%d", st.Revision, end)
		}
		return map[string]any{"items": items[offset:end], "next_cursor": next, "revision": st.Revision}, nil
	}
	if match := str(a, "match"); match != "" && !domain.In(match, "all", "any") {
		return nil, fmt.Errorf("INVALID_ARGUMENT：match 为 all 或 any")
	}
	if !isWrite(name) {
		switch name {
		case "get_project":
			result = st.Project(project)
		case "list_project_statuses":
			items := []any{}
			for _, v := range st.Project(project).Statuses {
				items = append(items, v)
			}
			return page(items)
		case "get_task":
			t, e := task(&st, str(a, "id"))
			if e != nil {
				return nil, e
			}
			result = t
		case "list_tasks", "list_knowledge_nodes", "search":
			items := []any{}
			kind := str(a, "type")
			if kind != "" && !domain.In(kind, "all", "task", "node") {
				return nil, fmt.Errorf("INVALID_ARGUMENT：type 为 all、task 或 node")
			}
			if name == "list_tasks" || name == "search" && kind != "node" {
				for _, t := range st.Tasks {
					if t.Project == project && t.Deleted == "" && matches(t.Tags, t.Title+" "+t.Description) && (str(a, "status") == "" || str(a, "status") == t.Status) {
						if name == "search" {
							items = append(items, map[string]any{"type": "task", "value": t})
						} else {
							items = append(items, t)
						}
					}
				}
			}
			if name == "list_knowledge_nodes" || name == "search" && kind != "task" {
				for i := range st.Nodes {
					n := &st.Nodes[i]
					if n.Project == project && n.Deleted == "" && matches(n.Tags, st.Address(n)+" "+n.Content) {
						v := wrapNode(&st, n)
						if name == "search" {
							items = append(items, map[string]any{"type": "node", "value": v})
						} else {
							items = append(items, v)
						}
					}
				}
			}
			return page(items)
		case "get_knowledge_node", "get_knowledge_subtree":
			n, e := node(&st, str(a, "id"))
			if e != nil {
				return nil, e
			}
			if name == "get_knowledge_node" {
				result = wrapNode(&st, n)
			} else {
				ids := st.Subtree(n.ID)
				items := []any{}
				for i := range st.Nodes {
					v := &st.Nodes[i]
					if ids[v.ID] && v.Deleted == "" {
						items = append(items, wrapNode(&st, v))
					}
				}
				return page(items)
			}
		case "list_labels":
			counts := map[string][2]int{}
			for _, t := range st.Project(project).Labels {
				counts[t] = [2]int{}
			}
			for _, t := range st.Tasks {
				if t.Project == project && t.Deleted == "" {
					for _, tag := range t.Tags {
						v := counts[tag]
						v[0]++
						counts[tag] = v
					}
				}
			}
			for _, n := range st.Nodes {
				if n.Project == project && n.Deleted == "" {
					for _, tag := range n.Tags {
						v := counts[tag]
						v[1]++
						counts[tag] = v
					}
				}
			}
			names := []string{}
			for k := range counts {
				names = append(names, k)
			}
			sort.Strings(names)
			items := []any{}
			for _, k := range names {
				items = append(items, map[string]any{"name": k, "project_id": project, "tasks": counts[k][0], "knowledge_nodes": counts[k][1]})
			}
			return page(items)
		case "list_task_comments":
			t, e := task(&st, str(a, "task_id"))
			if e != nil {
				return nil, e
			}
			items := []any{}
			for _, c := range t.Notes {
				if c.Deleted == "" {
					items = append(items, c)
				}
			}
			return page(items)
		case "get_comment_attachment":
			t, e := task(&st, str(a, "task_id"))
			if e != nil {
				return nil, e
			}
			id := str(a, "attachment_id")
			found := false
			for _, c := range t.Notes {
				for _, v := range c.Attachments {
					found = found || v.ID == id
				}
			}
			for _, v := range t.DetailAttachments {
				found = found || v.ID == id
			}
			if !found {
				return nil, fmt.Errorf("NOT_FOUND：请选择任务附件")
			}
			meta, b, e := s.Store.Attachment(project, id)
			if e != nil {
				return nil, e
			}
			result = map[string]any{"attachment": meta, "data": base64.StdEncoding.EncodeToString(b)}
		case "preview_delete":
			plan := DeletePlan{Project: project, Revision: st.Revision, Expires: time.Now().Add(5 * time.Minute), Tasks: []string{}, Nodes: []string{}}
			seen := map[string]bool{}
			for _, id := range list(a, "task_ids") {
				t, e := task(&st, id)
				if e != nil {
					return nil, e
				}
				if t.Deleted == "" && !seen[id] {
					plan.Tasks = append(plan.Tasks, id)
					seen[id] = true
				}
			}
			for _, id := range list(a, "node_ids") {
				n, e := node(&st, id)
				if e != nil {
					return nil, e
				}
				for child := range st.Subtree(n.ID) {
					if !seen[child] && st.Node(child).Deleted == "" {
						seen[child] = true
						plan.Nodes = append(plan.Nodes, child)
					}
				}
			}
			sort.Strings(plan.Nodes)
			id := domain.ID()
			s.mu.Lock()
			for k, p := range s.plans {
				if time.Now().After(p.Expires) {
					delete(s.plans, k)
				}
			}
			s.plans[id] = plan
			s.mu.Unlock()
			ns := []any{}
			for _, id := range plan.Nodes {
				ns = append(ns, wrapNode(&st, st.Node(id)))
			}
			result = map[string]any{"plan_id": id, "expires": plan.Expires, "tasks": plan.Tasks, "nodes": ns, "count": len(plan.Tasks) + len(plan.Nodes)}
		}
		return map[string]any{"data": result, "revision": st.Revision}, nil
	}
	expected, ok := a["expected_revision"].(float64)
	if !ok || expected != float64(int64(expected)) {
		return nil, fmt.Errorf("INVALID_ARGUMENT：请填写 expected_revision")
	}
	if name == "upload_comment_attachment" {
		if int64(expected) != st.Revision {
			return nil, fmt.Errorf("REVISION_CONFLICT")
		}
		if _, e := task(&st, str(a, "task_id")); e != nil {
			return nil, e
		}
		b, e := base64.StdEncoding.DecodeString(str(a, "data"))
		if e != nil {
			return nil, fmt.Errorf("INVALID_ARGUMENT：请提供 Base64 文件内容")
		}
		v, e := s.Store.PutAttachment(project, str(a, "filename"), str(a, "mime"), strings.NewReader(string(b)))
		return map[string]any{"data": v, "revision": st.Revision}, e
	}
	next, e := s.Store.Update(int64(expected), func(x *domain.State) error {
		p := x.Project(project)
		id := str(a, "id")
		switch name {
		case "update_project":
			if e := patch(p, fields(a), "name", "code", "description", "directory", "archived"); e != nil {
				return e
			}
			result = p
		case "create_task":
			num := 1
			for _, t := range x.Tasks {
				if t.Project == project && t.Num >= num {
					num = t.Num + 1
				}
			}
			t := domain.NewTask(project, domain.ID(), str(a, "title"), num)
			t.Status = p.InitialStatusID
			if e := patch(&t, fields(a), "description", "status", "priority", "start", "due", "end", "progress", "reminder", "repeat", "color", "tags", "checks", "knowledge", "relations", "detailAttachments"); e != nil {
				return e
			}
			x.Tasks = append(x.Tasks, t)
			result = t
		case "update_task", "move_task", "delete_task", "restore_task":
			t, e := task(x, id)
			if e != nil {
				return e
			}
			switch name {
			case "update_task":
				if e = patch(t, fields(a), "title", "description", "status", "priority", "start", "due", "end", "progress", "reminder", "repeat", "color", "tags", "checks", "knowledge", "relations", "detailAttachments"); e != nil {
					return e
				}
			case "move_task":
				if v, ok := a["status"]; ok {
					if v == nil {
						t.Status = p.InitialStatusID
					} else {
						v, ok := v.(string)
						if !ok {
							return fmt.Errorf("INVALID_ARGUMENT：请检查状态")
						}
						t.Status = v
					}
				}
				if v, ok := a["order"].(float64); ok {
					t.Order = v
				}
			case "delete_task":
				t.Deleted = domain.Now()
			case "restore_task":
				t.Deleted = ""
			}
			t.Updated = domain.Now()
			result = t
		case "create_knowledge_node":
			n := domain.Node{ID: domain.ID(), Project: project, Name: str(a, "name"), Content: str(a, "content"), Tags: list(a, "tags"), Links: []string{}, History: []domain.Revision{}, Updated: domain.Now()}
			if parent := str(a, "parent"); parent != "" {
				n.Parent = &parent
			}
			x.Nodes = append(x.Nodes, n)
			result = wrapNode(x, &n)
		case "update_knowledge_node", "move_knowledge_node", "restore_knowledge_node":
			n, e := node(x, id)
			if e != nil {
				return e
			}
			if name == "update_knowledge_node" {
				n.History = append(n.History, domain.Revision{ID: domain.ID(), Content: n.Content, Tags: append([]string{}, n.Tags...), At: domain.Now(), Reason: "更新前"})
				if e = patch(n, fields(a), "name", "content", "tags", "pinned", "order"); e != nil {
					return e
				}
			} else {
				if parent := str(a, "parent"); parent != "" {
					n.Parent = &parent
				} else {
					n.Parent = nil
				}
				if name == "restore_knowledge_node" {
					batch := n.DeleteBatch
					for child := range x.Subtree(n.ID) {
						c := x.Node(child)
						if c.DeleteBatch == batch {
							c.Deleted = ""
							c.DeleteBatch = ""
						}
					}
					if name := str(a, "name"); name != "" {
						n.Name = name
					}
				}
			}
			n.Updated = domain.Now()
			result = wrapNode(x, n)
		case "create_task_comment", "update_task_comment", "delete_task_comment", "restore_task_comment":
			t, e := task(x, str(a, "task_id"))
			if e != nil {
				return e
			}
			if name == "create_task_comment" {
				c := domain.Comment{ID: domain.ID(), Text: str(a, "text"), At: domain.Now(), Attachments: []domain.Attachment{}}
				if reply := str(a, "reply_to"); reply != "" {
					c.ReplyTo = &reply
				}
				for _, id := range list(a, "attachments") {
					v, _, e := s.Store.Attachment(project, id)
					if e != nil {
						return e
					}
					c.Attachments = append(c.Attachments, v)
				}
				t.Notes = append(t.Notes, c)
				result = c
			} else {
				found := false
				for i := range t.Notes {
					c := &t.Notes[i]
					if c.ID != str(a, "comment_id") {
						continue
					}
					found = true
					switch name {
					case "update_task_comment":
						c.Text = str(a, "text")
						c.Edited = domain.Now()
					case "delete_task_comment":
						c.Deleted = domain.Now()
					case "restore_task_comment":
						c.Deleted = ""
					}
					result = c
				}
				if !found {
					return fmt.Errorf("NOT_FOUND：请选择任务评论")
				}
			}
			t.Updated = domain.Now()
		case "link_task_knowledge", "unlink_task_knowledge":
			t, e := task(x, str(a, "task_id"))
			if e != nil {
				return e
			}
			n, e := node(x, str(a, "node_id"))
			if e != nil {
				return e
			}
			if name == "link_task_knowledge" {
				t.Knowledge = domain.Tags(append(t.Knowledge, n.ID))
			} else {
				t.Knowledge = remove(t.Knowledge, n.ID)
			}
			result = t
		case "link_knowledge_nodes", "unlink_knowledge_nodes":
			n, e := node(x, str(a, "node_id"))
			if e != nil {
				return e
			}
			other := x.Node(str(a, "other_id"))
			if other == nil || other.Project != project {
				return fmt.Errorf("NOT_FOUND：请选择当前项目知识")
			}
			if name == "link_knowledge_nodes" {
				n.Links = domain.Tags(append(n.Links, other.ID))
			} else {
				n.Links = remove(n.Links, other.ID)
				other.Links = remove(other.Links, n.ID)
			}
			result = wrapNode(x, n)
		case "create_label", "rename_label", "delete_label":
			nameValue := domain.Clean(str(a, "name"))
			if nameValue == "" {
				return fmt.Errorf("INVALID_ARGUMENT：请填写标签")
			}
			if name == "create_label" {
				p.Labels = domain.Tags(append(p.Labels, nameValue))
				result = p.Labels
				break
			}
			replacement := domain.Clean(str(a, "new_name"))
			if name == "rename_label" {
				if replacement == "" {
					return fmt.Errorf("INVALID_ARGUMENT：请填写新标签")
				}
				exists := domain.In(replacement, p.Labels...)
				for _, t := range x.Tasks {
					exists = exists || t.Project == project && domain.In(replacement, t.Tags...)
				}
				for _, n := range x.Nodes {
					exists = exists || n.Project == project && domain.In(replacement, n.Tags...)
				}
				if exists && replacement != nameValue && a["merge"] != true {
					return fmt.Errorf("INVALID_ARGUMENT：同名合并请设置 merge=true")
				}
			}
			change := func(tags []string) []string {
				out := []string{}
				for _, tag := range tags {
					if tag == nameValue {
						if name == "rename_label" {
							out = append(out, replacement)
						}
					} else {
						out = append(out, tag)
					}
				}
				return domain.Tags(out)
			}
			p.Labels = change(p.Labels)
			for i := range x.Tasks {
				if x.Tasks[i].Project == project {
					x.Tasks[i].Tags = change(x.Tasks[i].Tags)
				}
			}
			for i := range x.Nodes {
				if x.Nodes[i].Project == project {
					x.Nodes[i].Tags = change(x.Nodes[i].Tags)
				}
			}
			result = map[string]bool{"updated": true}
		case "attach_labels", "detach_labels":
			tags := domain.Tags(list(a, "tags"))
			ids := list(a, "target_ids")
			if len(tags) == 0 || len(ids) == 0 || !domain.In(str(a, "target_type"), "task", "node") {
				return fmt.Errorf("INVALID_ARGUMENT：请选择标签、目标类型与对象")
			}
			for _, id := range ids {
				var target *[]string
				if str(a, "target_type") == "task" {
					t, e := task(x, id)
					if e != nil {
						return e
					}
					target = &t.Tags
				} else {
					n, e := node(x, id)
					if e != nil {
						return e
					}
					target = &n.Tags
				}
				if name == "attach_labels" {
					*target = domain.Tags(append(*target, tags...))
				} else {
					for _, tag := range tags {
						*target = remove(*target, tag)
					}
				}
			}
			result = map[string]int{"updated": len(ids)}
		case "commit_delete":
			s.mu.Lock()
			plan, ok := s.plans[str(a, "plan_id")]
			s.mu.Unlock()
			if !ok || plan.Project != project || plan.Revision != x.Revision || time.Now().After(plan.Expires) {
				return fmt.Errorf("PLAN_EXPIRED：请重新预览删除范围")
			}
			batch := domain.ID()
			for _, id := range plan.Tasks {
				x.Task(id).Deleted = domain.Now()
			}
			for _, id := range plan.Nodes {
				n := x.Node(id)
				n.Deleted = domain.Now()
				n.DeleteBatch = batch
			}
			result = map[string]int{"deleted": len(plan.Tasks) + len(plan.Nodes)}
		default:
			return fmt.Errorf("METHOD_NOT_FOUND")
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	switch v := result.(type) {
	case domain.Task:
		result = next.Task(v.ID)
	case *domain.Task:
		result = next.Task(v.ID)
	case *domain.Project:
		result = next.Project(v.ID)
	case map[string]any:
		if id, ok := v["id"].(string); ok {
			if n := next.Node(id); n != nil {
				result = wrapNode(&next, n)
			}
		}
	}
	return map[string]any{"data": result, "revision": next.Revision}, nil
}

func validateToolArgs(a map[string]any) error {
	for k, v := range a {
		switch k {
		case "tags", "target_ids", "task_ids", "node_ids", "attachments":
			switch xs := v.(type) {
			case []string:
			case []any:
				for _, x := range xs {
					if _, ok := x.(string); !ok {
						return fmt.Errorf("INVALID_ARGUMENT：%s 请使用字符串数组", k)
					}
				}
			default:
				return fmt.Errorf("INVALID_ARGUMENT：%s 请使用字符串数组", k)
			}
		case "fields":
			if _, ok := v.(map[string]any); !ok {
				return fmt.Errorf("INVALID_ARGUMENT：fields 请使用对象")
			}
		case "expected_revision", "limit", "order":
			if _, ok := v.(float64); !ok {
				return fmt.Errorf("INVALID_ARGUMENT：%s 请使用数值", k)
			}
		case "merge":
			if _, ok := v.(bool); !ok {
				return fmt.Errorf("INVALID_ARGUMENT：merge 请使用布尔值")
			}
		case "parent", "status", "reply_to":
			if v != nil {
				if _, ok := v.(string); !ok {
					return fmt.Errorf("INVALID_ARGUMENT：%s 请使用字符串或 null", k)
				}
			}
		case "project_id", "id", "task_id", "node_id", "other_id", "comment_id", "attachment_id", "address", "title", "content", "text", "name", "new_name", "target_type", "type", "query", "match", "cursor", "plan_id", "data", "filename", "mime":
			if _, ok := v.(string); !ok {
				return fmt.Errorf("INVALID_ARGUMENT：%s 请使用字符串", k)
			}
		default:
			return fmt.Errorf("INVALID_ARGUMENT：请使用已定义参数 %s", k)
		}
	}
	return nil
}
