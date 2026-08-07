package rxext

import (
	"context"
	"testing"

	extension "openknowledge/internal/rxext/sdk"
)

func TestInitializeRecordsSession(t *testing.T) {
	h := &handler{}
	res, err := h.Initialize(context.Background(), extension.InitializeParams{
		Session: extension.SessionContext{SessionID: "sess-1", WorkspaceRoot: `D:\work\demo`, Generation: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.sessionID != "sess-1" || h.cwd != `D:\work\demo` {
		t.Errorf("会话上下文未记录: %+v", h)
	}
	if len(res.Subscriptions) != 2 {
		t.Errorf("应订阅 input.receive 与 tool.after，got: %v", res.Subscriptions)
	}
}

func TestStubInterceptorsContinue(t *testing.T) {
	h := &handler{sessionID: "s", cwd: "."}
	for _, fn := range []extension.InterceptorFunc{h.onInput, h.onToolAfter} {
		res, err := fn(context.Background(), "", []byte(`{}`))
		if err != nil || res == nil {
			t.Fatalf("拦截器错误: %v", err)
		}
		if res.Decision != extension.DecisionContinue {
			t.Errorf("桩拦截器应 Continue，got: %v", res.Decision)
		}
	}
}
