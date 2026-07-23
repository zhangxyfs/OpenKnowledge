package cli

import (
	"bytes"
	"testing"

	"openknowledge/internal/registry"
)

func TestOffOnToggle(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	var out, errBuf bytes.Buffer
	if registry.HooksDisabled() {
		t.Fatal("default should be enabled")
	}
	if code := Off(nil, &out, &errBuf); code != 0 {
		t.Fatalf("off code=%d err=%q", code, errBuf.String())
	}
	if !registry.HooksDisabled() {
		t.Fatal("expected disabled after ok off")
	}
	if code := On(nil, &out, &errBuf); code != 0 {
		t.Fatalf("on code=%d", code)
	}
	if registry.HooksDisabled() {
		t.Fatal("expected enabled after ok on")
	}
	// On 幂等（无标志文件也成功）
	if code := On(nil, &out, &errBuf); code != 0 {
		t.Fatalf("on idempotent code=%d", code)
	}
}
