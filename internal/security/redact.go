package security

import "regexp"

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password)(\s*[=:]\s*|\"\s*:\s*\")([^\s\"&,}]+)`),
	regexp.MustCompile(`(?i)bearer\s+[a-z0-9._~+/=-]{12,}`),
	regexp.MustCompile(`\bsk-[a-zA-Z0-9_-]{12,}\b`),
}

func Redact(value string) string {
	out := value
	out = secretPatterns[0].ReplaceAllString(out, "$1$2[REDACTED]")
	out = secretPatterns[1].ReplaceAllString(out, "Bearer [REDACTED]")
	out = secretPatterns[2].ReplaceAllString(out, "[REDACTED]")
	return out
}
