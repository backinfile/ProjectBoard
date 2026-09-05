package domain

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/text/unicode/norm"
	"regexp"
	"strings"
	"time"
	"unicode"
)

type State struct {
	Schema   int       `json:"schema"`
	Revision int64     `json:"revision"`
	Theme    string    `json:"theme"`
	Projects []Project `json:"projects"`
	Tasks    []Task    `json:"tasks"`
	Nodes    []Node    `json:"nodes"`
}
type Status struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}
type Project struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Code            string   `json:"code"`
	Description     string   `json:"description"`
	Directory       string   `json:"directory"`
	Statuses        []Status `json:"statuses"`
	InitialStatusID string   `json:"initialStatusId"`
	Archived        bool     `json:"archived,omitempty"`
	Labels          []string `json:"labels,omitempty"`
	NextTaskNum     int      `json:"nextTaskNum,omitempty"`
}
type Attachment struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
	Data   string `json:"data,omitempty"`
}
type Comment struct {
	ID          string       `json:"id"`
	Text        string       `json:"text"`
	At          string       `json:"at"`
	Edited      string       `json:"edited,omitempty"`
	Deleted     string       `json:"deleted,omitempty"`
	ReplyTo     *string      `json:"replyTo,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}
type Check struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}
type Relation struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}
type Task struct {
	ID                string       `json:"id"`
	Project           string       `json:"project"`
	Num               int          `json:"num"`
	Title             string       `json:"title"`
	Description       string       `json:"description"`
	Status            string       `json:"status"`
	Priority          string       `json:"priority"`
	Start             string       `json:"start"`
	Due               string       `json:"due"`
	End               string       `json:"end"`
	Reminder          string       `json:"reminder"`
	Repeat            string       `json:"repeat"`
	Color             string       `json:"color"`
	Progress          *int         `json:"progress"`
	Tags              []string     `json:"tags"`
	Knowledge         []string     `json:"knowledge"`
	Checks            []Check      `json:"checks"`
	Notes             []Comment    `json:"notes"`
	Relations         []Relation   `json:"relations"`
	DetailAttachments []Attachment `json:"detailAttachments"`
	Order             float64      `json:"order"`
	Created           string       `json:"created"`
	Updated           string       `json:"updated"`
	Deleted           string       `json:"deleted,omitempty"`
	RepeatedFrom      string       `json:"repeatedFrom,omitempty"`
	RepeatedTo        string       `json:"repeatedTo,omitempty"`
	ReminderFired     string       `json:"reminderFired,omitempty"`
}
type Revision struct {
	ID      string   `json:"id"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
	At      string   `json:"at"`
	Reason  string   `json:"reason"`
}
type Node struct {
	ID          string     `json:"id"`
	Project     string     `json:"project"`
	Parent      *string    `json:"parent"`
	Name        string     `json:"name"`
	Content     string     `json:"content"`
	Tags        []string   `json:"tags"`
	Links       []string   `json:"links"`
	History     []Revision `json:"history"`
	Order       float64    `json:"order"`
	Pinned      bool       `json:"pinned"`
	Updated     string     `json:"updated"`
	Deleted     string     `json:"deleted,omitempty"`
	DeleteBatch string     `json:"deleteBatch,omitempty"`
}

func ID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func Empty() State {
	return State{Schema: 2, Theme: "light", Projects: []Project{}, Tasks: []Task{}, Nodes: []Node{}}
}
func DefaultStatuses() []Status {
	return []Status{{"created", "创建", "todo"}, {"doing", "进行中", "active"}, {"done", "已完成", "done"}, {"cancelled", "已取消", "cancelled"}}
}
func NewTask(project, id, title string, num int) Task {
	return Task{ID: id, Project: project, Num: num, Title: title, Status: "created", Created: Now(), Updated: Now(), Tags: []string{}, Knowledge: []string{}, Checks: []Check{}, Notes: []Comment{}, Relations: []Relation{}, DetailAttachments: []Attachment{}}
}
func Clone[T any](x T) T { b, _ := json.Marshal(x); var y T; _ = json.Unmarshal(b, &y); return y }
func (s *State) Project(id string) *Project {
	for i := range s.Projects {
		if s.Projects[i].ID == id {
			return &s.Projects[i]
		}
	}
	return nil
}
func (s *State) Task(id string) *Task {
	for i := range s.Tasks {
		if s.Tasks[i].ID == id {
			return &s.Tasks[i]
		}
	}
	return nil
}
func (s *State) Node(id string) *Node {
	for i := range s.Nodes {
		if s.Nodes[i].ID == id {
			return &s.Nodes[i]
		}
	}
	return nil
}
func (s *State) Address(n *Node) string {
	parts := []string{}
	seen := map[string]bool{}
	for n != nil && !seen[n.ID] {
		seen[n.ID] = true
		parts = append([]string{n.Name}, parts...)
		if n.Parent == nil {
			break
		}
		n = s.Node(*n.Parent)
	}
	return strings.Join(parts, ".")
}
func (s *State) Subtree(id string) map[string]bool {
	out := map[string]bool{id: true}
	for changed := true; changed; {
		changed = false
		for _, n := range s.Nodes {
			if n.Parent != nil && out[*n.Parent] && !out[n.ID] {
				out[n.ID] = true
				changed = true
			}
		}
	}
	return out
}
func Clean(s string) string { return norm.NFC.String(strings.TrimSpace(s)) }
func Tags(xs []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range xs {
		v = Clean(v)
		if v != "" && !seen[v] {
			out = append(out, v)
			seen[v] = true
		}
	}
	return out
}

var identifier = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`)
var color = regexp.MustCompile(`^#[a-fA-F0-9]{6}$`)

func ValidID(s string) bool { return identifier.MatchString(s) }
func In(s string, values ...string) bool {
	for _, v := range values {
		if s == v {
			return true
		}
	}
	return false
}
func (s *State) Validate() error {
	if s.Projects == nil {
		s.Projects = []Project{}
	}
	if s.Tasks == nil {
		s.Tasks = []Task{}
	}
	if s.Nodes == nil {
		s.Nodes = []Node{}
	}
	if s.Schema != 2 {
		return fmt.Errorf("请选择版本 2 的工作空间数据")
	}
	if !In(s.Theme, "light", "dark") {
		return fmt.Errorf("请选择浅色或深色主题")
	}
	if len(s.Tasks) > 100000 || len(s.Nodes) > 50000 || len(s.Projects) > 1000 {
		return fmt.Errorf("请减少单个工作空间的数据量")
	}
	ids := map[string]bool{}
	claim := func(id string) error {
		if !ValidID(id) || ids[id] {
			return fmt.Errorf("请使用有效且唯一的 ID")
		}
		ids[id] = true
		return nil
	}
	codes := map[string]bool{}
	for i := range s.Projects {
		p := &s.Projects[i]
		if e := claim(p.ID); e != nil {
			return e
		}
		p.Name = Clean(p.Name)
		if p.Name == "" || len(p.Name) > 300 || !ValidID(p.Code) || codes[strings.ToLower(p.Code)] {
			return fmt.Errorf("请填写项目名称与唯一简称")
		}
		codes[strings.ToLower(p.Code)] = true
		if p.InitialStatusID == "" {
			p.InitialStatusID = "created"
		}
		states := map[string]bool{}
		initial := false
		for _, st := range p.Statuses {
			if !ValidID(st.ID) || states[st.ID] || Clean(st.Name) == "" || st.Name == "待处理" || !In(st.Kind, "todo", "active", "done", "cancelled") {
				return fmt.Errorf("请检查任务状态")
			}
			states[st.ID] = true
			if st.ID == p.InitialStatusID {
				initial = st.Name == "创建" && st.Kind == "todo"
			}
		}
		if !initial {
			return fmt.Errorf("请保留初始状态“创建”")
		}
		p.Labels = Tags(p.Labels)
	}
	siblings := map[string]bool{}
	for i := range s.Nodes {
		n := &s.Nodes[i]
		if e := claim(n.ID); e != nil {
			return e
		}
		n.Name = Clean(n.Name)
		if s.Project(n.Project) == nil || n.Name == "" || len(n.Name) > 320 || strings.Contains(n.Name, ".") || strings.IndexFunc(n.Name, unicode.IsControl) >= 0 {
			return fmt.Errorf("请检查知识节点名称与项目")
		}
		key := n.Project + "/"
		if n.Parent != nil {
			key += *n.Parent
		}
		key += "/" + strings.ToLower(n.Name)
		if n.Deleted == "" && siblings[key] {
			return fmt.Errorf("请为同级节点使用唯一名称")
		}
		if n.Deleted == "" {
			siblings[key] = true
		}
		n.Tags = Tags(n.Tags)
		if n.Links == nil {
			n.Links = []string{}
		}
		if n.History == nil {
			n.History = []Revision{}
		}
		if len(n.Content) > 4<<20 {
			return fmt.Errorf("请将节点内容控制在 4 MB 内")
		}
	}
	for i := range s.Nodes {
		n := &s.Nodes[i]
		seen := map[string]bool{n.ID: true}
		par := n.Parent
		for par != nil {
			p := s.Node(*par)
			if p == nil || p.Project != n.Project || seen[*par] || (n.Deleted == "" && p.Deleted != "") {
				return fmt.Errorf("请检查知识树父子关系")
			}
			seen[*par] = true
			par = p.Parent
		}
		for _, id := range n.Links {
			other := s.Node(id)
			if other == nil || other.Project != n.Project || id == n.ID {
				return fmt.Errorf("请选择当前项目的关联知识")
			}
		}
		for _, h := range n.History {
			if !ValidID(h.ID) {
				return fmt.Errorf("请检查知识历史")
			}
		}
	}
	nums := map[string]bool{}
	for i := range s.Tasks {
		t := &s.Tasks[i]
		if e := claim(t.ID); e != nil {
			return e
		}
		p := s.Project(t.Project)
		if p == nil || strings.TrimSpace(t.Title) == "" || len(t.Title) > 2000 || t.Num < 1 {
			return fmt.Errorf("请检查任务标题、编号与项目")
		}
		nk := fmt.Sprint(t.Project, "/", t.Num)
		if nums[nk] {
			return fmt.Errorf("请使用项目内唯一任务编号")
		}
		nums[nk] = true
		if t.Status == "" {
			t.Status = p.InitialStatusID
		}
		valid := false
		for _, st := range p.Statuses {
			valid = valid || st.ID == t.Status
		}
		if !valid || !In(t.Priority, "", "低", "中", "高", "紧急", "立即处理") {
			return fmt.Errorf("请选择有效的任务状态与优先级")
		}
		for _, v := range []string{t.Start, t.End, t.Due} {
			if v != "" {
				if _, e := time.Parse("2006-01-02", v); e != nil {
					return fmt.Errorf("请选择有效日期")
				}
			}
		}
		if t.Start != "" && t.End != "" && t.Start > t.End {
			return fmt.Errorf("请选择开始日期当天或之后的结束日期")
		}
		if t.Progress != nil && (*t.Progress < 0 || *t.Progress > 100) {
			return fmt.Errorf("请填写 0–100 的进度")
		}
		if !In(t.Repeat, "", "daily", "weekly", "monthly") || (t.Color != "" && !color.MatchString(t.Color)) {
			return fmt.Errorf("请检查重复周期与颜色")
		}
		if t.Reminder != "" {
			if _, e := time.Parse("2006-01-02T15:04", t.Reminder); e != nil {
				return fmt.Errorf("请选择有效提醒时间")
			}
		}
		t.Tags = Tags(t.Tags)
		if t.Knowledge == nil {
			t.Knowledge = []string{}
		}
		if t.Checks == nil {
			t.Checks = []Check{}
		}
		if t.Notes == nil {
			t.Notes = []Comment{}
		}
		if t.Relations == nil {
			t.Relations = []Relation{}
		}
		if t.DetailAttachments == nil {
			t.DetailAttachments = []Attachment{}
		}
		for _, id := range t.Knowledge {
			n := s.Node(id)
			if n == nil || n.Project != t.Project {
				return fmt.Errorf("请选择当前项目知识")
			}
		}
		for _, r := range t.Relations {
			other := s.Task(r.ID)
			if other == nil || other.Project != t.Project || other.ID == t.ID || !In(r.Type, "related", "blocks", "subtask") {
				return fmt.Errorf("请选择当前项目的关联任务")
			}
		}
		comments := map[string]bool{}
		for _, c := range t.Notes {
			if !ValidID(c.ID) || comments[c.ID] || len(c.Text) > 4<<20 {
				return fmt.Errorf("请检查评论内容与 ID")
			}
			comments[c.ID] = true
			if strings.TrimSpace(c.Text) == "" && len(c.Attachments) == 0 {
				return fmt.Errorf("请填写评论或添加附件")
			}
			if e := attachments(c.Attachments); e != nil {
				return e
			}
		}
		for _, c := range t.Notes {
			if c.ReplyTo != nil && (!comments[*c.ReplyTo] || *c.ReplyTo == c.ID) {
				return fmt.Errorf("请选择有效回复对象")
			}
		}
		if e := attachments(t.DetailAttachments); e != nil {
			return e
		}
		for _, c := range t.Checks {
			if !ValidID(c.ID) || strings.TrimSpace(c.Text) == "" {
				return fmt.Errorf("请填写检查项")
			}
		}
	}
	return nil
}
func attachments(xs []Attachment) error {
	if len(xs) > 8 {
		return fmt.Errorf("每个区域最多添加 8 个附件")
	}
	seen := map[string]bool{}
	for _, a := range xs {
		if !ValidID(a.ID) || seen[a.ID] || a.Size < 0 || a.Size > 20<<20 || len(a.Name) == 0 || len(a.Name) > 255 || strings.IndexFunc(a.Name, unicode.IsControl) >= 0 {
			return fmt.Errorf("请选择 20 MB 以内的附件并检查文件名")
		}
		seen[a.ID] = true
	}
	return nil
}
func (s *State) Attachments() map[string]Attachment {
	out := map[string]Attachment{}
	for _, t := range s.Tasks {
		for _, a := range t.DetailAttachments {
			out[a.ID] = a
		}
		for _, c := range t.Notes {
			for _, a := range c.Attachments {
				out[a.ID] = a
			}
		}
	}
	return out
}
