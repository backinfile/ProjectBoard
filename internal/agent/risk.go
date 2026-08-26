package agent

import (
	"regexp"
	"strings"
)

type Risk struct {
	Dangerous        bool
	Category, Reason string
}

var dangerousPatterns = []struct {
	category, reason string
	pattern          *regexp.Regexp
}{
	{"destructive_files", "删除或大范围覆盖文件", regexp.MustCompile(`(?i)(^|[;&|]\s*)(rm\s+-r|rmdir\s+/s|remove-item\s+.*-recurse|del\s+/[sq])`)},
	{"dangerous_git", "高风险 Git 操作", regexp.MustCompile(`(?i)\bgit\s+(reset\s+--hard|clean\b|push\b|branch\s+-D\b)`)},
	{"system_install", "系统级安装", regexp.MustCompile(`(?i)\b(choco|winget|scoop)\s+install\b|npm\s+install\s+-g\b`)},
	{"secret_access", "可能读取密钥", regexp.MustCompile(`(?i)(\.env\b|id_rsa|credentials|secret|token)`)},
}

func Classify(command string) Risk {
	normalized := strings.TrimSpace(command)
	for _, candidate := range dangerousPatterns {
		if candidate.pattern.MatchString(normalized) {
			return Risk{Dangerous: true, Category: candidate.category, Reason: candidate.reason}
		}
	}
	return Risk{}
}
