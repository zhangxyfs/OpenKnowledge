import { execFile } from "node:child_process";
import { randomUUID } from "node:crypto";

const OK = "{{EXE}}";

// 与 pi/opencode 一致的超时预算
const PROMPT_TIMEOUT_MS = 10000;
const HOOK_TIMEOUT_MS = 5000;

// runOk 调 `ok hook <event>` 子进程：stdin 喂 Claude 风格 snake_case JSON，读
// stdout/stderr/exit code。execFile 直 exec（无 shell 层，天然免疫 Windows pwsh
// 引号问题，且不经 DSH 的 workspace-write 沙箱执行器），内建 timeout（超时自动
// kill）与 windowsHide。全程 fail-open——启动失败、超时、读写异常一律解析为
// 空结果，绝不拖累 DSH 会话。
function runOk(args, payload, timeoutMs) {
  return new Promise((resolve) => {
    try {
      const child = execFile(OK, args, { timeout: timeoutMs, windowsHide: true }, (error, stdout, stderr) => {
        // error.code 为数字即进程退出码（exit 2 = ok 的阻断语义）；超时/启动失败
        // 时 code 非数字，归为空结果
        const code = error && typeof error.code === "number" ? error.code : 0;
        resolve({ code, stdout: stdout ?? "", stderr: stderr ?? "" });
      });
      child.stdin?.end(JSON.stringify(payload));
    } catch {
      resolve({ code: 0, stdout: "", stderr: "" });
    }
  });
}

// userMessage 构造 DSH 的 UserMessage：纯数据 + uuid + role 'user' + plugin 来源
// 标记，等价 dsh-llm 的 createUserMessage（packages/llm/llm/src/message.ts:192），
// 避免本地绝对路径插件依赖 DSH 内部包（node 解析不到 @deepseek-ai/*）。
function userMessage(text) {
  return {
    id: randomUUID(),
    role: "user",
    content: [{ type: "text", text }],
    source: { kind: "plugin", plugin: "openknowledge" },
  };
}

// sessionFields 从 agent 提取 ok 侧需要的会话字段（与官方桥 base() 同款取法）。
function sessionFields(agent) {
  return {
    session_id: agent?.session?.header?.id ?? "",
    cwd: agent?.session?.header?.cwd ?? process.cwd(),
  };
}

export const name = "openknowledge";

export function apply(ctx) {
  // ≈ UserPromptSubmit：检索注入。waterfall 语义：先跑 ok（fail-open）再 delegate
  // next()，仅当下游为 enter 时把注入消息附加到尾部——与官方桥同构
  // （packages/hooks/hooks-claude-code/src/index.ts:219-235）。插件侧不去重，
  // 是否注入由 ok 的 InjectForPrompt 决定（与其他 agent 一致）。
  ctx.on("agent/pre-step", async ({ agent, messages }, next) => {
    let extra = null;
    try {
      if (messages && messages.length > 0) {
        const promptText = messages
          .flatMap((m) => m.content ?? [])
          .filter((b) => b && b.type === "text" && typeof b.text === "string")
          .map((b) => b.text)
          .join("\n")
          .trim();
        if (promptText) {
          const r = await runOk(
            ["hook", "prompt"],
            { hook_event_name: "UserPromptSubmit", ...sessionFields(agent), prompt: promptText },
            PROMPT_TIMEOUT_MS,
          );
          const out = (r.stdout || "").trim();
          if (out) extra = userMessage(out);
        }
      }
    } catch { /* fail-open */ }
    const downstream = await next();
    if (!extra || downstream.kind !== "enter") return downstream;
    return { kind: "enter", messages: [...downstream.messages, extra] };
  });

  // ≈ PostToolUse(matcher write|edit)：记录触碰文件。DSH 写盘工具为 write/edit，
  // 参数键 file_path（packages/fs/tool-fs/src/write.ts:51），与 Claude 方言一致，
  // tool_input 原样透传。输出忽略，任何失败静默，结果一律 accept 放行。
  ctx.on("tools/post-execute", async (exec, _result, next) => {
    try {
      if (exec && (exec.name === "write" || exec.name === "edit")) {
        await runOk(
          ["hook", "post-tool"],
          {
            hook_event_name: "PostToolUse",
            ...sessionFields(exec.agent),
            tool_name: exec.name,
            tool_input: exec.arguments ?? {},
          },
          HOOK_TIMEOUT_MS,
        );
      }
    } catch { /* fail-open */ }
    return next();
  });

  // ≈ Stop：auto 自省 / enforce。ok 以 exit 2 + stderr 表达"阻断"；
  // agent/turn-stopping 边界上 agent.steer() 强制再来一步（官方桥同款，
  // hooks-claude-code/src/index.ts:270-277）。防重依赖 ok 侧 CheckStop 幂等
  // 语义，插件侧与 pi/opencode 一致不计数。
  ctx.on("agent/turn-stopping", async ({ agent }) => {
    try {
      const r = await runOk(
        ["hook", "stop"],
        { hook_event_name: "Stop", ...sessionFields(agent) },
        HOOK_TIMEOUT_MS,
      );
      const reason = (r.stderr || "").trim();
      if (r.code === 2 && reason) agent.steer(userMessage(reason));
    } catch { /* fail-open */ }
  });
}
