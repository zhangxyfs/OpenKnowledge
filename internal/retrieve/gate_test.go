package retrieve

import "testing"

func TestGatedBuiltin(t *testing.T) {
	// 精确命中内置表（含归一化：大小写/标点/空白不敏感）
	for _, p := range []string{"继续", "好的", "好的。", "  OK  ", "go, on!", "Thanks", "嗯"} {
		if !Gated(p, nil) {
			t.Errorf("%q 应被门控", p)
		}
	}
}

func TestGatedEmptyTerms(t *testing.T) {
	// 纯标点/emoji/空白/单字符拉丁：Terms 为空，检索必无结果，门控省 embed 调用
	for _, p := range []string{"", "   ", "！！！", "👍", "a b"} {
		if !Gated(p, nil) {
			t.Errorf("%q 应被门控（Terms 为空）", p)
		}
	}
}

func TestGatedNormalPrompts(t *testing.T) {
	for _, p := range []string{"继续推进索引重构", "构建", "ok 的检索怎么配", "git 提交规范", "好的方案有哪些"} {
		if Gated(p, nil) {
			t.Errorf("%q 不应被门控", p)
		}
	}
}

func TestGatedExtra(t *testing.T) {
	if !Gated("走起", []string{"走起"}) {
		t.Error("extra 短语应生效")
	}
	if !Gated("走 起。", []string{"走  起"}) {
		t.Error("extra 判定应走归一化（空白折叠/标点忽略）")
	}
	if Gated("走起吧", []string{"走起"}) {
		t.Error("门控是精确匹配，不做子串")
	}
}

func TestBuiltinPhrasesCopy(t *testing.T) {
	a := BuiltinPhrases()
	if len(a) != 21 {
		t.Fatalf("内置短语表应为 21 条，got %d", len(a))
	}
	a[0] = "篡改"
	if BuiltinPhrases()[0] == "篡改" {
		t.Error("BuiltinPhrases 必须返回副本")
	}
}
