package secrets

import (
	"strings"
	"testing"
)

func TestDetectsCommonCredentialFormats(t *testing.T) {
	text := strings.Join([]string{
		"const config = {",
		`  aws_key: "AKIAQWERTYUIOPASDFGH",`,
		`  gh_token: "ghp_Qw7Er2Ty5Ui9Op3As6Df8Gh1Jk4Lz0Xc5Vb9Nm",`,
		`  google: "AIzaSyA1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6Q",`,
		`  slack: "xoxb-1234567890-Kp9Rm2Xw7Vt4Lq6Zn3Bd8Hs5Fj",`,
		`  pem: "-----BEGIN RSA PRIVATE KEY-----",`,
		`  jwt: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N65IhLJTQ",`,
		"}",
	}, "\n")

	findings := Scan(text)
	kinds := map[string]bool{}
	for _, f := range findings {
		kinds[f.Kind] = true
		if f.Line <= 0 {
			t.Errorf("finding has invalid line: %+v", f)
		}
		if f.Preview == "" {
			t.Errorf("finding has empty preview: %+v", f)
		}
	}
	for _, want := range []string{"AWS access key ID", "GitHub token", "Google API key", "Slack token", "PEM private key", "JWT"} {
		if !kinds[want] {
			t.Errorf("expected detection of %q, got kinds %v", want, kinds)
		}
	}
}

func TestDetectsCredentialAssignments(t *testing.T) {
	text := "api_key = \"b3c1f9d2e4a680715c3e\"\npassword: \"hunter2secure99\""
	findings := Scan(text)
	if len(findings) != 2 {
		t.Fatalf("expected 2 assignment findings, got %d: %+v", len(findings), findings)
	}
	if findings[0].Line != 1 || findings[1].Line != 2 {
		t.Errorf("line numbers wrong: %d, %d", findings[0].Line, findings[1].Line)
	}
}

func TestAllowsPlaceholdersAndProse(t *testing.T) {
	cases := []string{
		`api_key = "<YOUR_API_KEY_HERE>"`,
		`token = "{{ .Env.Token }}"`,
		`password = "correct horse battery staple"`,
		`secret_key: "REPLACE_ME_PLEASE"`,
		`client_secret = "xxxxxxxxxxxxxxxx"`,
		`export GEMINI_API_KEY="$GEMINI_API_KEY"`,
		`// TODO: insert placeholder example value`,
		`api_key = "short"`,
	}
	for _, c := range cases {
		if f := Scan(c); len(f) != 0 {
			t.Errorf("false positive for %q: %+v", c, f)
		}
	}
}

func TestMaskNeverRevealsFullSecret(t *testing.T) {
	secret := "AKIAABCDEFGHIJKLMNOP"
	masked := Mask(secret)
	if strings.Contains(masked, secret) {
		t.Fatalf("mask leaked full secret: %s", masked)
	}
	if !strings.HasPrefix(masked, "AKIAAB") {
		t.Fatalf("mask should keep short prefix, got %s", masked)
	}
}

func TestBlockErrorIsActionable(t *testing.T) {
	err := BlockError([]Finding{
		{Kind: "AWS access key ID", Line: 3, Preview: "AKIAAB******CDEF"},
	})
	msg := err.Error()
	for _, want := range []string{"secret scan", "line 3", "AllowSecrets=true", "MG_ALLOW_SECRET_WRITE"} {
		if !strings.Contains(msg, want) {
			t.Errorf("block error missing %q: %s", want, msg)
		}
	}
	if strings.Contains(msg, "AKIAABCDEFGHIJKLMNOP") {
		t.Errorf("block error leaked secret: %s", msg)
	}
}

func TestAllowedEnvSwitch(t *testing.T) {
	t.Setenv("MG_ALLOW_SECRET_WRITE", "")
	if Allowed() {
		t.Error("default should enforce guard")
	}
	t.Setenv("MG_ALLOW_SECRET_WRITE", "0")
	if Allowed() {
		t.Error("'0' should enforce guard")
	}
	t.Setenv("MG_ALLOW_SECRET_WRITE", "1")
	if !Allowed() {
		t.Error("'1' should disable guard")
	}
}

func TestScanHandlesCRLFAndLongText(t *testing.T) {
	text := "line one\r\napi_key = \"b3c1f9d2e4a680715c3e\"\r\nline three"
	findings := Scan(text)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Line != 2 {
		t.Errorf("CRLF line number wrong: %d", findings[0].Line)
	}
}
