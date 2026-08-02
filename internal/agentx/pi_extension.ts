import { execFile } from "node:child_process";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

const OK = "{{EXE}}";

function runOk(
  args: string[],
  payload: unknown,
  timeoutMs: number,
): Promise<{ code: number; stdout: string; stderr: string }> {
  return new Promise((resolve) => {
    try {
      const child = execFile(OK, args, { timeout: timeoutMs, windowsHide: true }, (error, stdout, stderr) => {
        const code = error && typeof error.code === "number" ? (error.code as number) : 0;
        resolve({ code, stdout: stdout ?? "", stderr: stderr ?? "" });
      });
      child.stdin?.end(JSON.stringify(payload));
    } catch {
      resolve({ code: 0, stdout: "", stderr: "" });
    }
  });
}

function sessionId(ctx: { sessionManager: { getSessionId(): string } }): string {
  try {
    return ctx.sessionManager.getSessionId();
  } catch {
    return "";
  }
}

export default function (pi: ExtensionAPI) {
  // ≈ kimi UserPromptSubmit：检索注入（ok 把结果写到 stdout）
  pi.on("before_agent_start", async (event, ctx) => {
    const r = await runOk(
      ["hook", "prompt"],
      { prompt: event.prompt, cwd: ctx.cwd, session_id: sessionId(ctx) },
      10000,
    );
    const out = r.stdout.trim();
    if (out) {
      return { message: { customType: "openknowledge", content: out, display: false } };
    }
  });

  // ≈ kimi PostToolUse（matcher Write|Edit）：记录触碰文件
  pi.on("tool_result", async (event, ctx) => {
    if (event.toolName !== "write" && event.toolName !== "edit") return;
    const input = (event as { input?: { path?: string } }).input;
    if (!input || !input.path) return;
    await runOk(
      ["hook", "post-tool"],
      { tool_input: { path: input.path }, cwd: ctx.cwd, session_id: sessionId(ctx) },
      5000,
    );
  });

  // ≈ kimi Stop：auto 自省 / enforce。ok 以 exit 2 + stderr 文本表达"阻断"，
  // pi 无法阻断已结束的回合，改为把提示注入会话驱动 agent 当场完成自省。
  pi.on("agent_settled", async (_event, ctx) => {
    const r = await runOk(
      ["hook", "stop"],
      { cwd: ctx.cwd, session_id: sessionId(ctx) },
      5000,
    );
    const reason = r.stderr.trim();
    if (r.code === 2 && reason) {
      pi.sendMessage(
        { customType: "openknowledge", content: reason, display: true },
        { triggerTurn: true },
      );
    }
  });
}
