import { execFile } from "node:child_process";
import path from "node:path";

const OK = "{{EXE}}";

// 与 pi 扩展一致的超时预算
const PROMPT_TIMEOUT_MS = 10000;
const HOOK_TIMEOUT_MS = 5000;

type OkResult = { code: number; stdout: string; stderr: string };

// runOk 调 `ok hook <event>` 子进程：stdin 喂 Claude 风格 snake_case JSON，
// 读 stdout/stderr/exit code。用 node:child_process——Node 与 Bun 双运行时兼容
// （opencode 桌面端服务器跑在 Electron/Node 里，"bun" 模块导入会让整个插件
// 加载失败）；execFile 内建 timeout（超时自动 kill）与 windowsHide（Windows
// 不弹控制台窗口）。全程 fail-open——启动失败、超时、读写异常一律解析为
// 空结果，绝不拖累 opencode 会话。
function runOk(args: string[], payload: unknown, timeoutMs: number): Promise<OkResult> {
  return new Promise((resolve) => {
    try {
      const child = execFile(OK, args, { timeout: timeoutMs, windowsHide: true }, (error, stdout, stderr) => {
        // error.code 为数字即进程退出码（exit 2 = ok 的阻断语义）；超时/启动失败
        // 时 code 非数字，归为空结果
        const code = error && typeof error.code === "number" ? (error.code as number) : 0;
        resolve({ code, stdout: stdout ?? "", stderr: stderr ?? "" });
      });
      child.stdin?.end(JSON.stringify(payload));
    } catch {
      resolve({ code: 0, stdout: "", stderr: "" });
    }
  });
}

let partSeq = 0;

// 旧式命名导出：opencode 把模块每个导出值当作插件函数（getLegacyPlugins）。
export const OpenKnowledgePlugin = async ({ directory, client }: any) => {
  return {
    // ≈ UserPromptSubmit：检索注入。ok 把注入文本写 stdout；
    // 以 synthetic text part 注入（不在 UI 当用户输入渲染，但进会话历史
    // 并参与本次 LLM 请求——output.parts 按引用传入且 hook 后继续使用）。
    "chat.message": async (input: any, output: any) => {
      try {
        const texts: string[] = [];
        for (const p of output.parts) {
          if (p && p.type === "text" && typeof p.text === "string" && p.text.trim()) {
            texts.push(p.text);
          }
        }
        const promptText = texts.join("\n").trim();
        if (!promptText) return;
        const r = await runOk(
          ["hook", "prompt"],
          {
            hook_event_name: "UserPromptSubmit",
            session_id: input.sessionID,
            cwd: directory,
            prompt: promptText,
          },
          PROMPT_TIMEOUT_MS,
        );
        const out = (r.stdout || "").trim();
        if (!out) return;
        output.parts.push({
          id: `part_ok_${Date.now()}_${partSeq++}`,
          messageID: output.message.id,
          sessionID: input.sessionID,
          type: "text",
          text: out,
          synthetic: true,
        });
      } catch { /* fail-open */ }
    },

    // ≈ PostToolUse(matcher Write|Edit)：记录触碰文件。opencode 的 gpt 系新模型
    // 用 apply_patch 替代 write/edit（registry 互斥），patchText 里的
    // *** Add/Update/Delete File: 行逐路径上报；相对路径按 directory 绝对化
    // （ok 侧 TrackTouched 只认项目根内的绝对路径）。
    "tool.execute.after": async (input: any) => {
      try {
        const paths: string[] = [];
        if (input.tool === "write" || input.tool === "edit") {
          const p = input.args?.filePath;
          if (typeof p === "string" && p) paths.push(p);
        } else if (input.tool === "apply_patch") {
          const text = input.args?.patchText;
          if (typeof text === "string") {
            for (const m of text.matchAll(/^\*\*\* (?:Add|Update|Delete) File:\s*(.+)$/gm)) {
              if (m[1]) paths.push(m[1].trim());
            }
          }
        }
        for (const p of paths) {
          await runOk(
            ["hook", "post-tool"],
            {
              hook_event_name: "PostToolUse",
              session_id: input.sessionID,
              cwd: directory,
              tool_name: input.tool,
              tool_input: { path: path.isAbsolute(p) ? p : path.join(directory, p) },
            },
            HOOK_TIMEOUT_MS,
          );
        }
      } catch { /* fail-open */ }
    },

    // ≈ Stop：auto 自省 / enforce。ok 以 exit 2 + stderr 表达"阻断"；opencode 的
    // session.idle 无法阻断已结束的回合，改为把 reason 作为用户消息补发回会话，
    // 驱动 agent 当场完成自省（与 pi 的 sendMessage(triggerTurn) 同构）。
    // 防重依赖 ok 侧 CheckStop 的 LastExtractReminder / MarkBlocked 语义，
    // 插件侧与 pi 一致不计数。promptAsync 立即返回，不等回合结束。
    event: async ({ event }: any) => {
      try {
        if (!event || event.type !== "session.idle") return;
        const sessionID = event.properties?.sessionID;
        if (!sessionID) return;
        const r = await runOk(
          ["hook", "stop"],
          { hook_event_name: "Stop", session_id: sessionID, cwd: directory },
          HOOK_TIMEOUT_MS,
        );
        const reason = (r.stderr || "").trim();
        if (r.code !== 2 || !reason) return;
        await client.session.promptAsync({
          path: { id: sessionID },
          body: { parts: [{ type: "text", text: reason }] },
        });
      } catch { /* fail-open */ }
    },
  };
};
