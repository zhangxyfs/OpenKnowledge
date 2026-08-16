package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// EmbeddingProfile 是一套 embedding 服务配置。Type：openai（OpenAI 兼容线上/自建）、
// ollama（本机或局域网 Ollama，免 key）、builtin（ok 托管 llama.cpp sidecar）。
// openai/ollama 的 Model 是模型名；builtin 的 Model 是 embed.BuiltinModels 清单 id，
// Mirror 是其下载源（hf-mirror|huggingface|自定义 base URL）。
type EmbeddingProfile struct {
	Name      string `toml:"name"`
	Type      string `toml:"type"`
	BaseURL   string `toml:"base_url,omitempty"`
	Model     string `toml:"model,omitempty"`
	APIKey    string `toml:"api_key,omitempty"`
	APIKeyEnv string `toml:"api_key_env,omitempty"`
	Mirror    string `toml:"mirror,omitempty"`
}

// ResolvedAPIKey 返回生效 key：api_key 优先，其次 api_key_env 指向的环境变量。
func (p EmbeddingProfile) ResolvedAPIKey() string {
	if p.APIKey != "" {
		return p.APIKey
	}
	if p.APIKeyEnv != "" {
		return os.Getenv(p.APIKeyEnv)
	}
	return ""
}

// ModelIdentity 返回建索引的模型身份串（写入 kb.db meta，切换检测用）。
func (p EmbeddingProfile) ModelIdentity() string {
	switch p.Type {
	case "builtin":
		return "builtin:" + p.Model
	case "ollama":
		return "ollama:" + p.Model + "@" + p.BaseURL
	default:
		return "openai:" + p.Model + "@" + p.BaseURL
	}
}

type Embedding struct {
	Active     string             `toml:"active,omitempty"` // 使用中 profile 名；空=未配置
	TimeoutSec int                `toml:"timeout_sec"`
	Profiles   []EmbeddingProfile `toml:"profiles,omitempty"`
	// ModelsDir 内置模型下载目录；空=默认 <ok.exe 所在目录>/models（安装版即安装目录下）
	ModelsDir string `toml:"models_dir,omitempty"`
	// 旧版平铺字段（≤v2.13），仅迁移读取；omitempty 保证新写盘不再出现
	BaseURL   string `toml:"base_url,omitempty"`
	APIKey    string `toml:"api_key,omitempty"`
	APIKeyEnv string `toml:"api_key_env,omitempty"`
	Model     string `toml:"model,omitempty"`
}

// ActiveProfile 返回使用中 profile；未配置或 active 悬空返回 nil。
func (e Embedding) ActiveProfile() *EmbeddingProfile {
	if e.Active == "" {
		return nil
	}
	for i := range e.Profiles {
		if e.Profiles[i].Name == e.Active {
			return &e.Profiles[i]
		}
	}
	return nil
}

// migrateLegacy 把 ≤v2.13 的平铺字段迁移为 "默认" openai profile（内存态；
// 下次保存配置时自然落盘）。
func (e *Embedding) migrateLegacy() {
	if e.BaseURL == "" && e.Model == "" && e.APIKey == "" && e.APIKeyEnv == "" {
		return
	}
	if len(e.Profiles) == 0 {
		e.Profiles = []EmbeddingProfile{{
			Name: "默认", Type: "openai",
			BaseURL: e.BaseURL, Model: e.Model, APIKey: e.APIKey, APIKeyEnv: e.APIKeyEnv,
		}}
		if e.Active == "" {
			e.Active = "默认"
		}
	}
	e.BaseURL, e.Model, e.APIKey, e.APIKeyEnv = "", "", "", ""
}

type Inject struct {
	MaxTokens int `toml:"max_tokens"`
	// ReinjectTurns 无显式压缩事件的宿主按轮次重注入 mandatory 全文（0=关闭）。
	// 上下文压缩把首轮全文摘要掉后，模型仍可靠粘性指针重读文件；需要
	// "硬约束始终在场"的宿主可设 >0 周期性重注入全文。
	ReinjectTurns int `toml:"reinject_turns"`
}

// Index 控制 INDEX.md 渲染预算。
type Index struct {
	MaxLines int `toml:"max_lines"` // 主列表最大行数，默认 50；<=0 按 50（渲染处归一）
}

// RetrieveGate 控制泛化 prompt 门控：内置短语表编译进二进制（随版本演进，
// 不物化进 config.toml），用户在 GUI 维护的只是 extra_phrases 追加层。
type RetrieveGate struct {
	Enabled      bool     `toml:"enabled"`       // 默认 true（见 Default）
	ExtraPhrases []string `toml:"extra_phrases"` // 内置表之外的追加层
}

// RetrieveRecency 控制时效信号（[retrieve.recency] 子表）：按条目 mtime 新鲜度
// 给融合分乘系数（不参与准入）——age ≤ fresh → 1.0，age ≥ stale → floor，
// 中间线性。Floor 非法（<=0 或 >1）按 0.85。
type RetrieveRecency struct {
	Enabled bool           `toml:"enabled"` // 默认 true（见 Default）
	Floor   float64        `toml:"floor"`   // 陈旧系数下限，默认 0.85
	Windows RecencyWindows `toml:"windows"`
}

// RecencyWindows 按条目类型的 [fresh_days, stale_days] 窗口（天）。全零、
// 长度不为 2 或 stale<=fresh 均视为该类型不衰减（fail-open）。
type RecencyWindows struct {
	Rule      []int `toml:"rule"`
	Pitfall   []int `toml:"pitfall"`
	Note      []int `toml:"note"`
	Reference []int `toml:"reference"`
}

// RetrieveFeedback 控制注入→采纳反馈闭环（[retrieve.feedback] 子表）：
// 窗口内持续注入但从未被读取的条目降权（v1 只降不升——加分会自我强化造成
// 条目固化，降权只修"持续噪声"这一种确定的问题）。
type RetrieveFeedback struct {
	Enabled       bool    `toml:"enabled"`        // 默认 false（见 Default）——宿主 read 派发未接通前采纳信号恒零，降权默认关闭；事件照常记录，接通后恢复默认 true
	WindowDays    int     `toml:"window_days"`    // 统计窗口（天），默认 30；<=0 按 30
	MinInjections int     `toml:"min_injections"` // 触发降权的最低注入次数，默认 4；<=0 按 4
	Demote        float64 `toml:"demote"`         // 降权系数，默认 0.8；<=0 或 >=1 按 0.8
}

type Retrieve struct {
	Alpha float64 `toml:"alpha"`
	Beta  float64 `toml:"beta"`
	TopN  int     `toml:"top_n"`
	// MinGap 是语义通道的"头部显著性"判定阈值（默认 0.25）：本次查询余弦分布的
	// max 相对中位数的相对 gap 低于该值时，语义通道整体不准入（宁缺毋滥）。
	// 低对比度自定义 embedding 模型（相关与噪声的 gap 都很小）可调低放宽；
	// <=0 关闭 gap 判定（仅绝对下限 min_score 生效，回到模型相关语义）。
	MinGap float64 `toml:"min_gap"`
	// MinScore 是检索注入的最低置信阈值（0~1，默认 0.5）。准入按通道独立判定：
	// 关键词通道需归一 BM25 分（未乘 α）≥ 阈值；语义通道需余弦 ≥ 语义门槛——
	// 该门槛模型无关（见 index.SemanticFloor：以本次查询余弦分布为参照，
	// 头部相对背景有显著分离才启用），min_score 只作绝对下限兜底。
	// 阈值随库规模缩放（index.MinScoreFloor：<10 条关闭、10→30 线性、≥30 全量）。
	// 自定义/低对比度 embedding 模型整体余弦偏低时，可适当调低本值放宽语义准入。
	// 0 或负数表示关闭阈值（旧语义：score>0 即注入）。宁缺毋滥，不强行凑 top_n。
	MinScore float64 `toml:"min_score"`
	// Fusion 是融合方式：rrf（默认，Reciprocal Rank Fusion，只看各准入通道内
	// 名次不看分数——换 embedding 模型后余弦分布漂移不影响平衡）| weighted
	//（旧行为回滚档：score = α·归一BM25 + β·余弦）。准入逻辑两模式完全一致；
	// Alpha/Beta 仅 weighted 生效。缺省/非法值一律按 rrf（fail-open）。
	Fusion string `toml:"fusion"`
	// RrfK 是 RRF 名次平滑常数（默认 60，Zep 同款惯例）；仅 rrf 模式生效，<=0 按 60。
	RrfK int `toml:"rrf_k"`
	// Gate 是泛化 prompt 门控（[retrieve.gate] 子表）：命中内置/extra 短语的
	// prompt 跳过检索注入与 embed 调用。Enabled 默认 true（见 Default）；
	// ExtraPhrases 是内置短语表之外的追加层，两层取并集生效。
	Gate RetrieveGate `toml:"gate"`
	// Recency 是时效信号（[retrieve.recency] 子表）：陈旧条目在近似同分时让位，
	// 不参与准入。编辑条目即刷新 mtime 新鲜度（feature）。
	Recency RetrieveRecency `toml:"recency"`
	// Feedback 是注入→采纳反馈闭环（[retrieve.feedback] 子表）：采纳归因窗口
	// = 本会话（只统计"读本会话注入过的条目"）。
	Feedback RetrieveFeedback `toml:"feedback"`
}

type EnforceRule struct {
	Type          string   `toml:"type"`
	CodeGlobs     []string `toml:"code_globs"`
	ChangelogGlob string   `toml:"changelog_glob"`
	Message       string   `toml:"message"`
}

// Capture 是知识捕获模式配置：propose（AI 提议草稿，人批准）或
// auto（自动捕获）；TurnInterval 为自动捕获的轮次间隔。
type Capture struct {
	Mode         string `toml:"mode"`
	TurnInterval int    `toml:"turn_interval"`
}

// Wiki 控制 wiki 落后提示。
type Wiki struct {
	StaleCommits int `toml:"stale_commits"` // 落后多少 commit 提示；0 = 关闭
}

// Hooks 控制写入 agent 配置的 hook 超时（秒）。2026-08-04 曾出现 Windows 高负载下
// 5s 超时导致 PostToolUse 整个会话静默丢失，故默认 10 且可在 GUI 引导页调整。
type Hooks struct {
	TimeoutSec int `toml:"timeout_sec"`
}

// Reasonix 控制 reasonix sidecar 的强制检查表达方式。
type Reasonix struct {
	EnforceMode string `toml:"enforce_mode"` // soft|hard|mixed；缺省/非法按 mixed
}

// Provenance 控制分支溯源（born 标签的自动记录）。
type Provenance struct {
	AutoBorn bool `toml:"auto_born"` // 默认 true（见 Default）
}

type Config struct {
	Embedding  Embedding     `toml:"embedding"`
	Inject     Inject        `toml:"inject"`
	Retrieve   Retrieve      `toml:"retrieve"`
	Enforce    []EnforceRule `toml:"enforce"`
	Capture    Capture       `toml:"capture"`
	Wiki       Wiki          `toml:"wiki"`
	Hooks      Hooks         `toml:"hooks"`
	Reasonix   Reasonix      `toml:"reasonix"`
	Provenance Provenance    `toml:"provenance"`
	Index      Index         `toml:"index"`
}

func Default() Config {
	return Config{
		Embedding:  Embedding{TimeoutSec: 5},
		Inject:     Inject{MaxTokens: 800},
		Retrieve:   Retrieve{Alpha: 1.0, Beta: 1.0, TopN: 2, MinScore: 0.5, MinGap: 0.25, Fusion: "rrf", RrfK: 60,
			Gate:    RetrieveGate{Enabled: true},
			Recency: RetrieveRecency{Enabled: true, Floor: 0.85, Windows: RecencyWindows{
				Rule: []int{180, 730}, Pitfall: []int{90, 365}, Note: []int{60, 180}, Reference: []int{180, 730},
			}},
			Feedback: RetrieveFeedback{Enabled: false, WindowDays: 30, MinInjections: 4, Demote: 0.8}},
		Capture:    Capture{Mode: "propose", TurnInterval: 5},
		Wiki:       Wiki{StaleCommits: 20},
		Hooks:      Hooks{TimeoutSec: 10},
		Provenance: Provenance{AutoBorn: true},
		Index:      Index{MaxLines: 50},
	}
}

// Load 读取配置；文件不存在返回 Default，缺省字段用默认值填充。
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	cfg.Embedding.migrateLegacy()
	return cfg, nil
}

// LoadMerged 合并配置：内置默认 ← globalPath ← projectPath，后者覆盖前者。
// profiles 数组例外：toml 对数组整体替换，这里按 name 合并（项目级同名覆盖，
// 不删除全局独有项）。两个文件都可以不存在（视为空）。
func LoadMerged(projectPath, globalPath string) (Config, error) {
	cfg := Default()
	for _, path := range []string{globalPath, projectPath} {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return cfg, err
		}
		prev := cfg.Embedding.Profiles
		md, err := toml.Decode(string(data), &cfg)
		if err != nil {
			return cfg, fmt.Errorf("解析 %s: %w", path, err)
		}
		if md.IsDefined("embedding", "profiles") && len(prev) > 0 {
			merged := append([]EmbeddingProfile{}, prev...)
			for _, p := range cfg.Embedding.Profiles {
				found := false
				for i := range merged {
					if merged[i].Name == p.Name {
						merged[i] = p
						found = true
						break
					}
				}
				if !found {
					merged = append(merged, p)
				}
			}
			cfg.Embedding.Profiles = merged
		}
	}
	cfg.Embedding.migrateLegacy()
	return cfg, nil
}
