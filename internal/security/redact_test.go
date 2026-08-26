package security_test

import (
	"github.com/projectboard/projectboard/internal/security"
	"strings"
	"testing"
)

func TestSecretsAreRedactedBeforePersistence(t *testing.T) {
	input := `{"token":"abc123456789secret","line":"Authorization: Bearer abcdefghijklmnop","key":"sk-example123456789"}`
	got := security.Redact(input)
	for _, secret := range []string{"abc123456789secret", "abcdefghijklmnop", "sk-example123456789"} {
		if strings.Contains(got, secret) {
			t.Fatalf("secret remained: %s", got)
		}
	}
}
