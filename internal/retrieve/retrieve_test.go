package retrieve

import "testing"

func TestTerms(t *testing.T) {
	got := Terms("Git 提交规范")
	want := map[string]bool{"git": true, "提交": true, "交规": true, "规范": true}
	if len(got) != len(want) {
		t.Fatalf("terms %v", got)
	}
	for _, term := range got {
		if !want[term] {
			t.Fatalf("unexpected term %q in %v", term, got)
		}
	}
}
