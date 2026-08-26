package agent

import "testing"

func TestDangerousCommandsAlwaysRequireAUserDecision(t *testing.T) {
	for _, command := range []string{"git reset --hard HEAD~1", "git clean -fd", "Remove-Item .\\dist -Recurse", "winget install Example", "Get-Content .env"} {
		risk := Classify(command)
		if !risk.Dangerous {
			t.Errorf("expected dangerous: %s", command)
		}
	}
}
func TestReadOnlyDevelopmentCommandsStayUnblocked(t *testing.T) {
	for _, command := range []string{"go test ./...", "git status --short", "npm run typecheck"} {
		risk := Classify(command)
		if risk.Dangerous {
			t.Errorf("expected safe: %s (%s)", command, risk.Reason)
		}
	}
}
