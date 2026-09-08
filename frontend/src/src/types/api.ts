/**
 * 后端 ApiResponse<T> 字段类型契约（接口契约）。
 *
 * <p>与后端 {@code server.Response} 字段一一对应：
 * <ul>
 *   <li>{@code code}：number，0 = OK，非 0 为 AppError 错误码</li>
 *   <li>{@code message}：string，默认 "ok"</li>
 *   <li>{@code data}：泛型 T</li>
 * </ul>
 *
 * <p>字段规范：全部 snake_case（下划线小驼峰），与后端 domain json tag 严格一致。
 */
export interface ApiResponse<T> {
  code: number
  message: string
  data: T
}

/** 后端 /api/v1/meta/health 返回结构。 */
export interface HealthInfo {
  status: string
  phase: string
  uptime_ms: number
  db_enabled: boolean
  providers: number
}

/** 后端 /api/v1/meta/runtime 返回结构（内置运行时状态）。 */
export interface RuntimeStatus {
  home: string
  bundled_dir: string
  ready: boolean
  error?: string
  assets: RuntimeAssetStatus[]
}

/** 单个内置运行时资产（node / python / powershell）状态。 */
export interface RuntimeAssetStatus {
  id: string
  version: string
  ready: boolean
  path: string
  executable: string
  archive: string
  archive_found: boolean
  error?: string
}

/** 后端 /api/v1/kv/{k} 返回结构（V1 meta 表）。 */
export interface MetaEntry {
  k: string
  v: string
  updated_at: number
}

/** 工具：判断业务成功（code === 0）。 */
export function isOk<T>(resp: ApiResponse<T>): boolean {
  return resp.code === 0
}

// ===== 聊天 =====

/** 会话（GET /api/v1/chat/sessions 返回 SessionRESP）。 */
export interface Session {
id: string
name: string
user_id: string
provider_id: string
model: string
/** 逻辑文件夹树关联 id（历史字段，UI 不再直接使用） */
workspace_id: string | null
/** 会话绑定的外部工作目录（绝对路径；空 = 默认工作区）。 */
workspace_path: string | null
active: boolean
message_count: number
last_message_at: number | null
metadata_json: string | null
status: string
/** 会话级工具权限模式（restricted/default/auto_edit/yolo）；空 = 跟随全局设置 */
permission_mode: string | null
/** 分叉来源会话 ID；根会话为空 */
parent_id: string | null
/** 分叉点消息 seq；根会话为 0 */
branch_point: number
created_at: number
updated_at: number
}

/** 分页响应（PageRESP<T>）。 */
export interface Page<T> {
  items: T[]
  total: number
}

/** 参数取值来源层级。 */
export type ParamSource = 'provider' | 'default' | 'builtin' | 'request'

/** 当前会话实际生效的运行参数（GET /api/v1/chat/sessions/:id/params）。 */
export interface EffectiveParams {
  session_id: string
  provider_id: string
  model: string
  temperature: number
  temperature_from: ParamSource
  thinking_effort: string
  thinking_from: ParamSource
  context_window: number
  context_window_from: ParamSource
  compression_ratio: number
  compression_from: ParamSource
  context_budget: number
  max_input_chars: number
}

/** 消息（MessageRESP）。 */
export interface Message {
  id: string
  session_id: string
  seq: number
  run_id: string | null
  role: string
  content: string
  /** 思考过程（thinking 模型的 reasoning 文本；未产生思考时为 null）。 */
  thinking?: string | null
  tool_call_id: string | null
  tool_calls_json: string | null
  status: string
  stop_reason: string | null
  model: string | null
  input_tokens?: number | null
  output_tokens?: number | null
  /** 命中缓存的输入 token（input_tokens 的一个子维度；同一批 token 不叠加）。 */
  cache_read_tokens?: number | null
  total_tokens?: number | null
  latency_ms?: number | null
  cost?: string | null
  created_at: number
  updated_at: number
  /** 持久化过程块：assistant 消息的工具调用/结果/产物，刷新后复现。 */
  blocks?: MessageBlock[] | null
}

/** 消息块类型（与后端 domain.MessageBlockKind 对齐）。 */
export type MessageBlockKind = 'thinking' | 'tool_call' | 'tool_result' | 'artifact' | 'genui'

/** 消息块（MessageBlockRESP）：块即行，payload 为 JSON 字符串。 */
export interface MessageBlock {
  id: string
  message_id: string
  seq: number
  kind: MessageBlockKind
  payload: string
  created_at: number
}

/** 流式聊天请求体（ChatStreamReq）。 */
export interface ChatStreamReq {
  message: string
  session_id?: string
  provider_id?: string
  model_id?: string
  file_ids?: string[]
  /** 采样温度（可选 0.0~2.0；请求级覆盖 provider 级与全局默认 0.2）。 */
  temperature?: number | null
  /** 思考强度（可选 off/low/medium/high；请求级覆盖 provider 级与全局默认 medium）。 */
  thinking_effort?: string | null
  /** 检查点续跑：设置后走 POST /chat/stream/{id}/resume，其余字段忽略。 */
  resume_run_id?: string
}

/** MidTurn Queue 一条消息。 */
export interface QueuedMessage {
  id: string
  text: string
}

/** SSE 事件（AgentEventMapper → FrontendEventFormatter 的内层事件）。 */
export interface ChatStreamEvent {
  type: string
  data: unknown
}

/** 流式 stats 事件 payload（usage/耗时/缓存命中/cost）。 */
export interface ChatStats {
  input_tokens?: number
  output_tokens?: number
  /** 命中缓存的输入 token（输入的子维度，不叠加）。 */
  cache_read_tokens?: number
  /** 写入缓存的输入 token（Anthropic cache_creation；其余模型多为 0）。 */
  cache_creation_tokens?: number
  total_tokens?: number
  latency_ms?: number
  cost?: string
}

/** 交付文件（present_files 工具 → artifact 事件）。 */
export interface ArtifactFile {
  path: string
  name: string
  kind: string
  size?: number
  url?: string
}

/** artifact 事件 payload（ChatEventBus 发布的交付事件）。 */
export interface ArtifactPayload {
  files: ArtifactFile[]
  explanation?: string
}

/** 工作区文件（GET /api/v1/chat/workspace/:id/files）。 */
export interface WorkspaceFile {
  path: string
  name: string
  ext: string
  kind: string
  size: number
  url: string
  modified_at: number
}

// ===== 资源页 =====

/** LLM 模型配置（GET /api/v1/ai-provider 返回 AiProviderRESP，api_key 已脱敏）。 */
export interface AiProvider {
  id: string
  name: string
  kind: string
  api_key_masked: string
  base_url: string | null
  model: string
  alias: string | null
  tier: string
  enabled: boolean
  context_window: number | null
  /** 单次最大输出 token；0/空 = 不限制。 */
  max_output_tokens: number | null
  /** 压缩比例（0.1~1.0，上下文已使用占比超过该值即触发压缩；缺省 0.9）。 */
  compress_ratio: number | null
  /** 默认采样温度（0.0~2.0，可选；null=走全局默认 0.2）。 */
  temperature: number | null
  /** 默认核采样（0~1；0 = 走全局默认）。 */
  top_p: number | null
  /** 默认思考强度（off/low/medium/high，可选；null=走全局默认 medium）。 */
  thinking_effort: string | null
  /** 思维参数协议方言（空 = 自动探测）。 */
  thinking_style: string | null
  /** 实际生效方言（auto 探测结果，展示用）。 */
  thinking_style_resolved: string | null
  supports_tool_call: boolean | null
  supports_vision: boolean | null
  supports_reasoning: boolean | null
  tool_call_effective: boolean
  vision_effective: boolean
  reasoning_effective: boolean
  capabilities_json: string | null
  pricing_json: string | null
  created_at: number
  updated_at: number
}

/** 可切换模型（GET /api/v1/ai-provider/available 返回 AvailableModelRESP）。 */
export interface AvailableModel {
  id: string
  name: string
  kind: string
  model: string
  alias: string | null
  tier: string
  enabled: boolean
  context_window?: number | null
  max_output_tokens?: number | null
  compress_ratio?: number | null
  temperature?: number | null
  thinking_effort?: string | null
  thinking_style?: string | null
  tool_call?: boolean
  vision?: boolean
  reasoning?: boolean
}

/** Provider Kind 元数据（GET /api/v1/ai-provider/kinds 返回 ProviderKindsRESP）。
 *  后端 domain.ProviderKindMeta 字段一一对应；前端不写死 kind 列表，
 *  Settings 下拉直接从后端拉，避免"前端看得见、后端没实现"的契约漂移。 */
export interface AiProviderKind {
  kind: string
  label: string
  base_url_default: string
  model_placeholder: string
}

/** Provider 熔断/就绪状态（GET /api/v1/ai-provider/circuit-status）。 */
export interface CircuitState {
  id: string
  name: string
  enabled: boolean
  state: 'CLOSED' | 'OPEN' | 'HALF_OPEN' | string
  consecutive_failures: number
  retry_in_ms: number
  /** 未就绪原因（apiKey 缺失/解密失败/kind 未知等）；就绪时为空。 */
  reason?: string
}

/** 模型配置创建/更新请求体。 */
export interface AiProviderReq {
  name: string
  kind: string
  api_key?: string
  base_url?: string
  model: string
  alias?: string | null
  tier?: string
  enabled?: boolean
  context_window?: number | null
  max_output_tokens?: number | null
  compress_ratio?: number | null
  capabilities_json?: string | null
  pricing_json?: string | null
  temperature?: number | null
  top_p?: number | null
  thinking_effort?: string | null
  thinking_style?: string | null
  supports_tool_call?: boolean | null
  supports_vision?: boolean | null
  supports_reasoning?: boolean | null
}

/** SMTP配置。 */
export interface SmtpConfig {
  enabled: string
  host: string
  port: string
  username: string
  password: string
  from: string
  ssl?: string
}

/** 联网搜索配置（DuckDuckGo，无需 api_key；仅 enabled 生效）。 */
export interface WebSearchConfig {
  enabled: string
  engine: string
  api_key: string
}

/** 文件（GET /api/v1/files/search 返回 FileRESP）。 */
export interface FileInfo {
  id: string
  name: string
  original_name: string
  file_type: string
  mime_type: string
  size: number
  status: string
  session_id: string | null
  folder_id: string | null
  created_at: number
}

// ===== 本机会话令牌（无账号体系）=====

/**
 * {@code GET /api/v1/auth/session} 返回结构。
 *
 * <p>WorkBaby 是本机单用户桌面助手：没有注册 / 登录 / 刷新令牌。
 */
export interface SessionToken {
  access_token: string
  token_type: string
  /** 0 = 不过期（随后端进程生命周期）。 */
  expires_in: number
  user_id: string
  username: string
}

// ===== 斜杠命令 =====

/** 斜杠命令元数据（GET /api/v1/chat/commands）。 */
export interface SlashCommand {
  name: string
  args: string
  desc: string
  group: 'session' | 'model' | 'agent' | 'system' | string
  client_only: boolean
}

/** 命令列表出参。 */
export interface CommandListRESP {
  items: SlashCommand[]
  total: number
}

/** 会话压缩结果（POST /api/v1/chat/sessions/:id/compact）。 */
export interface CompactResultRESP {
  session_id: string
  compacted: number
  freed_chars: number
  kept_recent: number
  total_before: number
  /** 保留指示是否已钉进上下文（Instructions 非空且落库成功） */
  pinned: boolean
  /** 估算释放的 token（rune/4 近似） */
  freed_tokens: number
  /** 单条更新失败数；>0 表示部分压缩失败 */
  failed: number
}

/** 会话压缩入参。 */
export interface CompactREQ {
  instructions?: string
  keep_recent?: number
}

// ===== 上下文占用 =====

/** 上下文占用分段。 */
export interface ContextSegment {
  key: 'system' | 'memory' | 'tools' | 'history' | string
  title: string
  tokens: number
  /** 占上下文窗口的千分比（避免前端浮点误差） */
  ratio: number
}

/** 会话上下文占用快照。 */
export interface ContextUsageRESP {
  session_id: string
  model: string
  context_window: number
  /** 压缩触发预算（窗口 × 压缩比例；0 = 未启用） */
  context_budget: number
  used_tokens: number
  free_tokens: number
  used_ratio: number
  segments: ContextSegment[]
  message_count: number
  tool_count: number
  /** true 表示历史段为字符近似估算（未拿到 provider 实测 input_tokens） */
  estimated: boolean
}

// ===== 工作区文件树（真实磁盘目录，懒加载） =====

/** 工作区单层目录条目（GET /api/v1/chat/workspace/:id/ls）。 */
export interface WorkspaceEntry {
  /** 相对工作区根，slash 分隔；目录以 / 结尾便于判型 */
  path: string
  name: string
  is_dir: boolean
  size: number
  ext: string
  kind: string
}

/** 单层列目录响应。 */
export interface WorkspaceListRESP {
  items: WorkspaceEntry[]
  truncated: boolean
  /** 工作区根绝对路径（「添加到聊天」拼绝对路径引用用）。 */
  root: string
}

// ===== 运行历史 =====

/** 单次 run 的索引记录（GET /api/v1/chat/runs）。 */
export interface RunRecord {
  run_id: string
  session_id: string
  model: string
  status: 'running' | 'done' | 'error'
  reason: string
  turns: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  total_tokens: number
  started_at: number
  ended_at: number
}

/** 运行历史分页响应。 */
export interface RunRecordList {
  items: RunRecord[]
  total: number
}

// ===== 工作目录信任 =====

export type TrustState = 'allow' | 'ask' | 'deny'

/** 单条信任登记。 */
export interface TrustEntry {
  path: string
  state: TrustState
  updated_at: number
}

/** 单目录信任解析结果。Source 为实际命中的祖先目录（可与入参不同）。 */
export interface TrustResolveRESP {
  path: string
  state: TrustState
  source: string
  matched: boolean
}

// ===== Todo（计划工具共享状态） =====

/** 单条计划项（todo 工具的共享状态；前端进度卡 = state 快照 + chat:todo 事件）。 */
export interface TodoItem {
  id: string
  title: string
  done: boolean
}

/** 会话计划快照。 */
export interface TodoStateRESP {
  session_id?: string
  items: TodoItem[]
  done_count: number
  total: number
}

// ===== 文件变更 =====

/** 文件变更动作（与后端 domain.ChangeAction 对齐）。 */
export type FileChangeAction = 'create' | 'modify' | 'delete'

/** 单条变更（GET /api/v1/chat/sessions/:id/changes 列表项）。 */
export interface FileChange {
  id: string
  session_id: string
  run_id: string
  tool_name: string
  rel_path: string
  action: FileChangeAction
  bytes_before: number
  bytes_after: number
  added_lines: number
  removed_lines: number
  rolled_back: boolean
  created_at: number
}

/** 单条变更详情（GET /api/v1/chat/changes/:cid）。 */
export interface FileChangeDetail extends FileChange {
  diff: string
  before_text: string
  after_text: string
  truncated: boolean
}

/** 变更列表出参。 */
export interface FileChangeListRESP {
  items: FileChange[]
  total: number
}

// ===== 工件 =====

/** 工件类别。 */
export type ArtifactKind = 'image' | 'doc' | 'code' | 'data' | 'file' | string

/** 工件条目（GET /api/v1/chat/sessions/:id/artifacts）。 */
export interface Artifact {
  id: string
  session_id: string
  run_id: string
  kind: ArtifactKind
  name: string
  rel_path: string
  mime_type: string
  size: number
  created_at: number
  updated_at: number
}

/** 工件列表出参。 */
export interface ArtifactListRESP {
  items: Artifact[]
  total: number
}

// ===== 后台任务 =====

/** 任务状态（与后端 domain.TaskState 对齐）。 */
export type TaskState = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'

/** 单条后台任务（GET /api/v1/tasks）。 */
export interface BackgroundTask {
  id: string
  session_id: string
  agent: string
  prompt: string
  state: TaskState
  run_id: string
  result: string
  error: string
  created_at: number
  started_at: number
  finished_at: number
}

/** 后台任务列表出参。 */
export interface TaskListRESP {
  items: BackgroundTask[]
  total: number
}

/** 后台任务提交请求（POST /api/v1/tasks）。 */
export interface TaskSubmitREQ {
  session_id: string
  agent: string
  prompt: string
}

// ===== 审批恢复 =====

/** 未决审批快照（GET /api/v1/chat/approvals/pending；刷新页面后恢复）。 */
export interface ApprovalPending {
  id: string
  run_id: string
  session_id: string
  command: string
  reason: string
  risk: 'needs_approval' | 'irreversible' | string
  expires_at: number
}

// ===== Skills =====

/** Skill（GET /api/v1/skills 返回 SkillRESP）。 */
/** Skill 内置脚本（SKILL.md 包 scripts/ 目录；run_skill_script 工具执行）。 */
export interface SkillScript {
  name: string
  language: string
  code: string
}

export interface Skill {
  id: string
  name: string
  description: string | null
  when_to_use: string | null
  body: string
  allowed_tools: string[] | null
  scripts: SkillScript[] | null
  source_kind: string | null
  enabled: boolean
  created_at: number
  updated_at: number
}

/** Skill 创建/更新请求体。 */
export interface SkillReq {
  name: string
  description?: string
  when_to_use?: string
  body: string
  allowed_tools?: string[]
  scripts?: SkillScript[]
  enabled?: boolean
}

/** 技能包 zip 导入结果（POST /api/v1/skills/import-zip）。 */
export interface SkillZipResult {
  imported: string[]
  skipped: string[]
  failed: Array<{ path: string; error: string }>
}

// ===== MCP Servers =====

/** MCP Server（GET /api/v1/mcp/servers 返回 McpServerRESP）。 */
export interface McpServer {
  id: string
  name: string
  transport: string
  command: string | null
  args: string[] | null
  env: Record<string, string> | null
  base_url: string | null
  enabled: boolean
  tool_count: number
  ready: boolean
  error: string | null
  created_at: number
  updated_at: number
}

/** MCP Server 创建/更新请求体。 */
export interface McpServerReq {
  name: string
  transport?: string
  command?: string
  args?: string[]
  env?: Record<string, string>
  base_url?: string
  enabled?: boolean
}

// ===== Tasks =====

/** 任务项（MVP 仅列近期会话作为"任务快照"）。 */
export interface TaskItem {
  id: string
  name: string
  status: string
  message_count: number
  last_message_at: number | null
  created_at: number
}

// ===== Tools =====

/** 工具描述（仅展示元数据；Agent 调用走 /chat/stream）。 */
export interface ToolInfo {
  name: string
  group: string
  description: string
}

/** 工作流定义。 */
export interface Workflow {
  id: string
  name: string
  description: string | null
  graph: string
  enabled: boolean
  created_at: number
  updated_at: number
}

/** 创建/更新工作流请求。 */
export interface WorkflowReq {
  name: string
  description?: string | null
  graph: string
  enabled?: boolean
}

/** 启动工作流请求。 */
export interface WorkflowRunReq {
  inputs?: Record<string, unknown>
}

/** 工作流图（DAG，编辑器格式；与后端 WorkflowDAGRESP 契约一致）。 */
export interface WorkflowGraph {
  name: string
  nodes: WorkflowGraphNode[]
  edges: WorkflowGraphEdge[]
  outputs?: Record<string, string> // 图级输出：key → "nodeId.field"
}

/** 工作流节点执行记录（GET /api/v1/executions/:id 详情的 nodes）。 */
export interface WorkflowNodeExecution {
  id: string
  execution_id: string
  node_id: string
  node_type: string
  status: 'PENDING' | 'RUNNING' | 'COMPLETED' | 'FAILED' | 'SKIPPED' | string
  error_msg?: string | null
  started_at?: number | null
  finished_at?: number | null
}

/** 执行详情：执行 + 节点执行列表。 */
export interface ExecutionDetail {
  execution: WorkflowExecution
  nodes: WorkflowNodeExecution[]
}

/** 可视化节点：type 是后端节点类型；branch 是 condition 分支归属；pos 为画布坐标。 */
export interface WorkflowGraphNode {
  id: string
  type: string
  params?: Record<string, unknown>
  branch?: string
  pos?: { x: number; y: number }
}

/** 可视化边：source_handle 仅在源是 condition 节点时携带（true/false/自定义分支）。 */
export interface WorkflowGraphEdge {
  from: string
  to: string
  source_handle?: string
}

/** 节点字段描述（schema 驱动属性面板）。 */
export interface WorkflowNodeField {
  name: string
  label: string
  type: 'text' | 'number' | 'textarea' | 'select' | 'json'
  required: boolean
  description?: string
  options?: string[]
  default?: unknown
}

/** 节点类型目录（调色板 + 属性面板数据源）。 */
export interface WorkflowNodeTypeInfo {
  type: string
  label: string
  fields: WorkflowNodeField[]
}

/** 工作流执行记录。 */
export interface WorkflowExecution {
  id: string
  workflow_id: string
  status: 'PENDING' | 'RUNNING' | 'PAUSED' | 'COMPLETED' | 'FAILED' | 'CANCELLED' | string
  inputs: Record<string, unknown> | null
  outputs: Record<string, unknown> | null
  error_msg: string | null
  started_at: number | null
  finished_at: number | null
  created_at: number
}

/** 情景记忆条目。 */
export interface MemoryEpisode {
  id: string
  session_id: string | null
  summary: string
  score: number
  created_at: number
}

/** 写入记忆请求。 */
export interface MemoryEpisodeReq {
  session_id?: string | null
  summary: string
  transcript?: string | null
  tags?: string[]
}

/** 语义记忆（facts）。 */
export interface MemoryFact {
  id: string
  subject: string
  key: string
  value: string
  confidence: number
  source: string | null
  created_at: number
  updated_at: number
}

/** 程序记忆（procedures）。 */
export interface MemoryProcedure {
  id: string
  name: string
  steps: string[]
  success_count: number
  failure_count: number
  last_used_at: number | null
  created_at: number
}

/** 统一召回条目。 */
export interface RecallEntry {
  kind: 'episode' | 'fact' | 'procedure'
  score: number
  source: string
  title: string
  snippet: string
}

/** GenUI 节点类型。 */
export type UiNodeKind =
  | 'stack' | 'grid' | 'row'
  | 'heading' | 'text' | 'divider'
  | 'badge' | 'card' | 'stat' | 'progress' | 'markdown' | 'code-block'
  | 'image'
  | 'button' | 'input'

/** GenUI 节点（递归）。 */
export interface GenUiNode {
  node_id: string
  kind: string
  props?: Record<string, unknown>
  children?: GenUiNode[]
}

/** GenUI 完整树。 */
export interface GenUiTree {
  schema_version: string
  root: GenUiNode
}

/** 文件夹。 */
export interface Folder {
  id: string
  name: string
  parent_id: string | null
  path: string
  description: string | null
  workspace_id: string | null
  child_count: number
  created_at: number
  updated_at: number
}

/** 文件夹树节点。 */
export interface FolderTreeNode {
  folder: Folder
  children: FolderTreeNode[]
}

/** 创建/更新文件夹请求。 */
export interface FolderReq {
  name: string
  parent_id?: string | null
  description?: string | null
  workspace_id?: string | null
}

/** 知识文档。 */
export interface KnowledgeDoc {
  id: string
  folder_id: string | null
  name: string
  source: string
  source_type: string
  mime: string
  size_bytes: number
  chunk_count: number
  status: string
  error_msg: string | null
  created_at: number
  updated_at: number
}

/** 创建/更新文档请求。 */
export interface KnowledgeDocReq {
  folder_id?: string | null
  name: string
  source: string
  source_type: string
}

// ===== Cron =====

/** 定时任务（GET /api/v1/cron/jobs）。 */
export interface CronJob {
  id: string
  name: string
  schedule: string
  workflow_id: string | null
  action: string
  enabled: boolean
  last_run_at: number | null
  last_status: string | null
  next_run_at: number | null
  created_at: number
  updated_at: number
}

/** 定时任务写入请求（后端 CronJobREQ：schedule + workflow_id）。 */
export interface CronJobReq {
  name: string
  schedule: string
  workflow_id?: string | null
  enabled?: boolean
}

// ===== 桌宠 =====

/** 桌宠配置（GET /api/v1/pet/config）。 */
export interface PetConfig {
  user_id: string
  enabled: boolean
  mode: string
  sprite_id: string | null
  position_x: number
  position_y: number
  scale: number
  bubble_enabled: boolean
  bubble_duration_ms: number
  /** 同一 sprite 复用为聊天区背景（与桌宠共享形象来源） */
  chat_background: boolean
  /** 背景不透明度百分比（0~100） */
  background_opacity: number
  /** 背景模糊像素（0 = 不模糊） */
  background_blur_px: number
  updated_at: number | null
}

/** 桌宠配置写入请求。 */
export interface PetConfigReq {
  enabled?: boolean
  mode?: string
  sprite_id?: string | null
  position_x?: number
  position_y?: number
  scale?: number
  bubble_enabled?: boolean
  bubble_duration_ms?: number
  chat_background?: boolean
  background_opacity?: number
  background_blur_px?: number
}

/** 桌宠 sprite（GET /api/v1/pet/sprites）。 */
export interface PetSprite {
  id: string
  name: string
  file_path: string | null
  format: string | null
  frame_count: number | null
  is_builtin: boolean
  created_at: number
}

/** 桌宠 sprite 写入请求。 */
export interface PetSpriteReq {
  name: string
  file_path?: string | null
  format?: string | null
  frame_count?: number | null
  is_builtin?: boolean
}

// ===== Channels =====

/** 通道类型（桌面单机：控制台 / 邮件 / webhook）。 */
export type ChannelType = 'CONSOLE' | 'EMAIL' | 'WEBHOOK'

/** 通道配置（GET /api/v1/channels）。 */
export interface ChannelConfig {
  id: string
  user_id: string | null
  channel_type: string
  enabled: boolean
  config_json: string | null
  webhook_url: string | null
  status: string
  last_error: string | null
  last_active_at: number | null
  created_at: number
  updated_at: number
}

/** 通道配置写入请求。 */
export interface ChannelConfigReq {
  channel_type: string
  enabled?: boolean
  config_json?: string | null
  webhook_url?: string | null
  /** 邮箱通道：收件人地址（结构化字段，最终组装为 config_json）。 */
  email_to?: string
  /** 邮箱通道：发件人显示名（结构化字段）。 */
  email_display_name?: string
}

/** 通道消息日志（GET /api/v1/channels/:id/messages）。 */
export interface ChannelMessageLog {
  id: string
  channel_id: string
  direction: string
  message_type: string | null
  user_external_id: string | null
  session_id: string | null
  content_summary: string | null
  status: string | null
  error_message: string | null
  created_at: number
}

// ===== Admin / Docs =====

/** 内置文档条目（GET /api/v1/docs）。 */
export interface DocItem {
  name: string
  title: string
}

/** 内置文档详情（GET /api/v1/docs/:name）。 */
export interface DocDetail {
  name: string
  title: string
  content: string
}

/** 管理后台概览（GET /api/v1/admin/overview）。 */
export interface AdminOverview {
  version: string
  phase: string
  home: string
  db_enabled: boolean
  tool_count: number
  providers: number
  sessions: number
  messages: number
  memory_episodes: number
  knowledge_docs: number
  workflows: number
  cron_jobs: number
  channels: number
  [key: string]: unknown
}
