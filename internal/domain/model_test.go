package domain

import (
	"reflect"
	"strings"
	"testing"
)

func fixture() State {
	s := Empty()
	s.Projects = []Project{{ID: "p", Name: "项目", Code: "PB", Statuses: DefaultStatuses(), InitialStatusID: "created"}, {ID: "q", Name: "另一项目", Code: "QQ", Statuses: DefaultStatuses(), InitialStatusID: "created"}}
	parent := "root"
	s.Nodes = []Node{{ID: "root", Project: "p", Name: "技术"}, {ID: "child", Project: "p", Name: "开发", Parent: &parent}, {ID: "foreign", Project: "q", Name: "技术"}}
	s.Tasks = []Task{NewTask("p", "task", "标题", 1), NewTask("p", "other", "另一任务", 2), NewTask("q", "cross", "异项目", 1)}
	return s
}
func ptr[T any](v T) *T { return &v }

// Each row is a separate white-box branch/boundary test, and appears in go test -json.
func TestValidationMatrix(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(*State)
		valid bool
	}{
		{"W001_有效完整结构", func(s *State) {}, true},
		{"W002_结构版本拒绝", func(s *State) { s.Schema = 3 }, false},
		{"W003_主题枚举拒绝", func(s *State) { s.Theme = "system" }, false},
		{"W004_项目上限", func(s *State) { s.Projects = make([]Project, 1001) }, false},
		{"W005_任务上限", func(s *State) { s.Tasks = make([]Task, 100001) }, false},
		{"W006_节点上限", func(s *State) { s.Nodes = make([]Node, 50001) }, false},
		{"W007_跨实体重复ID", func(s *State) { s.Nodes[0].ID = "p" }, false},
		{"W008_非法ID", func(s *State) { s.Projects[0].ID = "../path" }, false},
		{"W009_空项目名", func(s *State) { s.Projects[0].Name = "  " }, false},
		{"W010_项目名长度", func(s *State) { s.Projects[0].Name = strings.Repeat("a", 301) }, false},
		{"W011_简称忽略大小写重复", func(s *State) { s.Projects[1].Code = "pb" }, false},
		{"W012_状态ID重复", func(s *State) { s.Projects[0].Statuses[1].ID = "created" }, false},
		{"W013_状态种类非法", func(s *State) { s.Projects[0].Statuses[1].Kind = "invalid" }, false},
		{"W014_待处理状态拒绝", func(s *State) { s.Projects[0].Statuses[1].Name = "待处理" }, false},
		{"W015_创建状态名称保护", func(s *State) { s.Projects[0].Statuses[0].Name = "别名" }, false},
		{"W016_创建状态种类保护", func(s *State) { s.Projects[0].Statuses[0].Kind = "active" }, false},
		{"W017_节点名称点号", func(s *State) { s.Nodes[0].Name = "a.b" }, false},
		{"W018_节点控制字符", func(s *State) { s.Nodes[0].Name = "a\nb" }, false},
		{"W019_同级大小写碰撞", func(s *State) { s.Nodes = append(s.Nodes, Node{ID: "duplicate", Project: "p", Name: "技术"}) }, false},
		{"W020_回收站同名允许", func(s *State) {
			s.Nodes = append(s.Nodes, Node{ID: "duplicate", Project: "p", Name: "技术", Deleted: Now()})
		}, true},
		{"W021_跨项目父节点", func(s *State) { s.Nodes[1].Parent = ptr("foreign") }, false},
		{"W022_缺失父节点", func(s *State) { s.Nodes[1].Parent = ptr("absent") }, false},
		{"W023_循环父节点", func(s *State) { s.Nodes[0].Parent = ptr("child") }, false},
		{"W024_活动子节点已删父", func(s *State) { s.Nodes[0].Deleted = Now() }, false},
		{"W025_节点自关联", func(s *State) { s.Nodes[0].Links = []string{"root"} }, false},
		{"W026_节点跨项目关联", func(s *State) { s.Nodes[0].Links = []string{"foreign"} }, false},
		{"W027_节点内容4MB边界", func(s *State) { s.Nodes[0].Content = strings.Repeat("a", 4<<20) }, true},
		{"W028_节点内容超4MB", func(s *State) { s.Nodes[0].Content = strings.Repeat("a", (4<<20)+1) }, false},
		{"W029_历史ID非法", func(s *State) { s.Nodes[0].History = []Revision{{ID: "a.b"}} }, false},
		{"W030_空任务标题", func(s *State) { s.Tasks[0].Title = " " }, false},
		{"W031_任务标题长度", func(s *State) { s.Tasks[0].Title = strings.Repeat("a", 2001) }, false},
		{"W032_任务编号零", func(s *State) { s.Tasks[0].Num = 0 }, false},
		{"W033_项目内编号重复", func(s *State) { s.Tasks[1].Num = 1 }, false},
		{"W034_空状态默认创建", func(s *State) { s.Tasks[0].Status = "" }, true},
		{"W035_无效优先级", func(s *State) { s.Tasks[0].Priority = "最高" }, false},
		{"W036_无效日期", func(s *State) { s.Tasks[0].Due = "2026-02-29" }, false},
		{"W037_闰年日期", func(s *State) { s.Tasks[0].Due = "2028-02-29" }, true},
		{"W038_结束早于开始", func(s *State) { s.Tasks[0].Start = "2026-02-02"; s.Tasks[0].End = "2026-02-01" }, false},
		{"W039_进度负数", func(s *State) { s.Tasks[0].Progress = ptr(-1) }, false},
		{"W040_进度零", func(s *State) { s.Tasks[0].Progress = ptr(0) }, true},
		{"W041_进度100", func(s *State) { s.Tasks[0].Progress = ptr(100) }, true},
		{"W042_进度101", func(s *State) { s.Tasks[0].Progress = ptr(101) }, false},
		{"W043_重复周期非法", func(s *State) { s.Tasks[0].Repeat = "yearly" }, false},
		{"W044_颜色非法", func(s *State) { s.Tasks[0].Color = "#fff" }, false},
		{"W045_提醒格式非法", func(s *State) { s.Tasks[0].Reminder = "2026-01-01" }, false},
		{"W046_任务跨项目知识", func(s *State) { s.Tasks[0].Knowledge = []string{"foreign"} }, false},
		{"W047_任务自关联", func(s *State) { s.Tasks[0].Relations = []Relation{{ID: "task", Type: "related"}} }, false},
		{"W048_任务跨项目关联", func(s *State) { s.Tasks[0].Relations = []Relation{{ID: "cross", Type: "related"}} }, false},
		{"W049_关联类型非法", func(s *State) { s.Tasks[0].Relations = []Relation{{ID: "other", Type: "invalid"}} }, false},
		{"W050_空评论", func(s *State) { s.Tasks[0].Notes = []Comment{{ID: "c", Text: " "}} }, false},
		{"W051_重复评论ID", func(s *State) { s.Tasks[0].Notes = []Comment{{ID: "c", Text: "x"}, {ID: "c", Text: "y"}} }, false},
		{"W052_评论自回复", func(s *State) { s.Tasks[0].Notes = []Comment{{ID: "c", Text: "x", ReplyTo: ptr("c")}} }, false},
		{"W053_回复目标缺失", func(s *State) { s.Tasks[0].Notes = []Comment{{ID: "c", Text: "x", ReplyTo: ptr("absent")}} }, false},
		{"W054_评论内容超限", func(s *State) { s.Tasks[0].Notes = []Comment{{ID: "c", Text: strings.Repeat("a", (4<<20)+1)}} }, false},
		{"W055_空检查项", func(s *State) { s.Tasks[0].Checks = []Check{{ID: "check", Text: " "}} }, false},
		{"W056_附件负大小", func(s *State) { s.Tasks[0].DetailAttachments = []Attachment{{ID: "a", Name: "x", Size: -1}} }, false},
		{"W057_附件20MB边界", func(s *State) { s.Tasks[0].DetailAttachments = []Attachment{{ID: "a", Name: "x", Size: 20 << 20}} }, true},
		{"W058_附件超20MB", func(s *State) {
			s.Tasks[0].DetailAttachments = []Attachment{{ID: "a", Name: "x", Size: (20 << 20) + 1}}
		}, false},
		{"W059_附件重名ID", func(s *State) {
			s.Tasks[0].DetailAttachments = []Attachment{{ID: "a", Name: "x"}, {ID: "a", Name: "y"}}
		}, false},
		{"W060_区域九附件", func(s *State) { s.Tasks[0].DetailAttachments = make([]Attachment, 9) }, false},
		{"W061_附件文件名控制字符", func(s *State) { s.Tasks[0].DetailAttachments = []Attachment{{ID: "a", Name: "x\ny"}} }, false},
		{"W062_仅附件评论允许", func(s *State) {
			s.Tasks[0].Notes = []Comment{{ID: "c", Attachments: []Attachment{{ID: "a", Name: "x"}}}}
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := fixture()
			c.edit(&s)
			err := s.Validate()
			if (err == nil) != c.valid {
				t.Fatalf("valid=%v, error=%v", c.valid, err)
			}
			if c.name == "W034_空状态默认创建" && s.Tasks[0].Status != "created" {
				t.Fatal("default missing")
			}
		})
	}
}
func TestModelHelpers(t *testing.T) {
	t.Run("W063_NFC标签去重", func(t *testing.T) {
		got := Tags([]string{" é ", "e\u0301", "", "中文", "中文"})
		if !reflect.DeepEqual(got, []string{"é", "中文"}) {
			t.Fatal(got)
		}
	})
	t.Run("W064_地址和子树稳定ID", func(t *testing.T) {
		s := fixture()
		if s.Address(s.Node("child")) != "技术.开发" {
			t.Fatal("address")
		}
		s.Node("root").Name = "架构"
		if s.Address(s.Node("child")) != "架构.开发" {
			t.Fatal("rename")
		}
		if !reflect.DeepEqual(s.Subtree("root"), map[string]bool{"root": true, "child": true}) {
			t.Fatal("subtree")
		}
	})
	t.Run("W065_克隆隔离", func(t *testing.T) {
		s := fixture()
		c := Clone(s)
		c.Tasks[0].Title = "copy"
		c.Projects[0].Statuses[0].Name = "copy"
		if s.Tasks[0].Title == "copy" || s.Projects[0].Statuses[0].Name == "copy" {
			t.Fatal("alias")
		}
	})
	t.Run("W066_默认字段空集合", func(t *testing.T) {
		s := Empty()
		if err := s.Validate(); err != nil {
			t.Fatal(err)
		}
		x := NewTask("p", "t", "标题", 1)
		if x.Status != "created" || x.Progress != nil || x.Priority != "" || x.Notes == nil || x.DetailAttachments == nil {
			t.Fatal(x)
		}
	})
	t.Run("W067_ID生成格式及样本唯一", func(t *testing.T) {
		seen := map[string]bool{}
		for i := 0; i < 1000; i++ {
			id := ID()
			if !ValidID(id) || len(id) != 32 || seen[id] {
				t.Fatal(id)
			}
			seen[id] = true
		}
	})
	t.Run("W068_回收站附件引用收集", func(t *testing.T) {
		s := fixture()
		s.Tasks[0].Deleted = Now()
		s.Tasks[0].DetailAttachments = []Attachment{{ID: "a", Name: "detail"}}
		s.Tasks[0].Notes = []Comment{{ID: "c", Text: "x", Deleted: Now(), Attachments: []Attachment{{ID: "b", Name: "comment"}}}}
		if len(s.Attachments()) != 2 {
			t.Fatal("trash attachments omitted")
		}
	})
}
