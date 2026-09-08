<script setup lang="ts">
/**
 * 聊天输入框（composer）。
 *
 * <p>自上而下分三层：顶部 chip 行（工作区 / 权限）→ 中间输入区（队列 / 附件 / textarea /
 * slash 与 mention 浮层 / 底部工具行）→ 底部状态行（上下文用量 / 快捷键提示）。
 *
 * <p>输入框保留原生 `textarea`：中文 IME composition、自增高、`@` mention 与 `/` 命令
 * 的光标解析都依赖原生事件，ElementPlus `el-input` 无法等价承接。
 */
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  Paperclip,
  Square,
  Upload,
  Send,
  Plus,
  Shield,
  Zap,
  Sparkles,
  ChevronDown
} from '@/components/common/icons'
import { apiGet, apiPost } from '@/api/client'
import { UploadFile, OpenFileDialog } from '@/wailsjs/go/main/App'
import { useToast } from '@/composables/useToast'
import { t } from '@/i18n'
import { useChatStore } from '@/stores/chat'
import { ElMessageBox } from 'element-plus'
import type { AvailableModel, CircuitState, EffectiveParams, FileInfo, Message, QueuedMessage, Skill } from '@/types/api'
import ModelSelector from '@/components/chat/ModelSelector.vue'
import AttachmentStrip from '@/components/chat/composer/AttachmentStrip.vue'
import SlashCommandPalette, { type SlashCommand } from '@/components/chat/composer/SlashCommandPalette.vue'
import MentionPicker, { type MentionItem } from '@/components/chat/composer/MentionPicker.vue'
import SkillPicker from '@/components/chat/composer/SkillPicker.vue'
import MidTurnQueue from '@/components/chat/composer/MidTurnQueue.vue'
import ContextUsagePopover from '@/components/chat/composer/ContextUsagePopover.vue'
import ParamsPopover, { type ComposerParams } from '@/components/chat/composer/ParamsPopover.vue'
import { useSkillsStore } from '@/stores/skills'
import { useFoldersStore } from '@/stores/folders'
import { useFilesStore } from '@/stores/files'

type PermissionLevel = 'restricted' | 'confirm' | 'auto' | 'full'

const props = defineProps<{
  streaming: boolean
  disabled: boolean
  models?: AvailableModel[]
  selectedModelID?: string | null
  /** 上下文窗口使用估值（0~max），占位版用 0/128k。 */
  ctxUsed?: number
  ctxMax?: number
  /** 熔断状态 Map（id → CircuitState）。 */
  circuitStates?: Map<string, CircuitState>
  /** 当前会话绑定的工作区绝对路径（null = 默认会话工作区）。 */
  workspacePath?: string | null
  /** 当前会话生效参数快照（后端三层合并；null = 未加载）。 */
  effectiveParams?: EffectiveParams | null
  /** 当前权限级别（默认 auto）。 */
  permission?: PermissionLevel
}>()

const emit = defineEmits<{
  send: [message: string, fileIds: string[], params: ComposerParams]
  'select-model': [id: string | null]
  stop: []
  enqueue: [message: QueuedMessage]
  'remove-queue': [id: string]
  'send-queued': [message: string]
  regenerate: []
  /** 工作区切换（弹 WorkspacePickerDialog）。 */
  'pick-workspace': []
  /** 权限级别切换。 */
  'change-permission': [level: PermissionLevel]
}>()

const toast = useToast()
const router = useRouter()

/** 输入上限：canSend 在超限时返回 false，真正拦截发送（计数同时标红提示）。
 *  默认 32000 与后端 `domain.DefaultChatMaxInputChars` 对齐；启动时异步拉
 *  `chat.maxInputChars` 覆盖（用户在 Settings → Advanced 改 system_settings 后下次启动生效）。
 *  前端 + 后端都用同源默认值，避免前后端不一致导致「前端能发、后端拒收」。 */
const DEFAULT_MAX_INPUT_CHARS = 32000
const maxInputChars = ref(DEFAULT_MAX_INPUT_CHARS)
const draft = ref('')
const attachments = ref<FileInfo[]>([])
const uploading = ref(false)
const textareaRef = ref<HTMLTextAreaElement | null>(null)

const isComposing = ref(false)

/** 根容器：浮层的「点击外部关闭」判定边界。 */
const composerRef = ref<HTMLElement | null>(null)

const slashOpen = ref(false)
const slashQuery = ref('')
const slashPaletteRef = ref<InstanceType<typeof SlashCommandPalette> | null>(null)
/** 触发字符 `/` 在草稿中的下标（替换时按这个位置截断，而非 lastIndexOf）。 */
const slashStart = ref(-1)
const mentionOpen = ref(false)
const mentionQuery = ref('')
const mentionActiveIndex = ref(-1)
/** 触发字符 `@` 在草稿中的下标（同上）。 */
const mentionStart = ref(-1)
/** 工具行「技能」按钮唤起的技能选择器。 */
const skillPickerOpen = ref(false)

const modelSelectorRef = ref<InstanceType<typeof ModelSelector> | null>(null)
/** 采样参数弹层实例：send 时读取当前 temperature / thinking_effort。 */
const paramsRef = ref<InstanceType<typeof ParamsPopover> | null>(null)

function currentParams(): ComposerParams {
  return paramsRef.value?.current() ?? { temperature: null, thinking_effort: 'medium' }
}

const skillsStore = useSkillsStore()
const foldersStore = useFoldersStore()
const filesStore = useFilesStore()
const chat = useChatStore()

const mentionItems = computed<MentionItem[]>(() => {
  const q = mentionQuery.value.trim().toLowerCase()
  const items: MentionItem[] = []
  for (const s of skillsStore.skills) {
    if (s.enabled === false) continue
    if (q && !s.name.toLowerCase().includes(q)) continue
    items.push({
      type: 'skill',
      id: s.id,
      name: s.name,
      insertText: `@${s.name}`,
      description: s.description ?? undefined
    })
  }
  const flat = (nodes: { id: string; name: string; path?: string; children?: unknown[] }[]): void => {
    for (const n of nodes) {
      if (q && !n.name.toLowerCase().includes(q)) continue
      items.push({ type: 'folder', id: n.id, name: n.name, insertText: `@${n.name}`, path: n.path })
      if (Array.isArray(n.children) && n.children.length > 0) {
        flat(n.children as { id: string; name: string; path?: string; children?: unknown[] }[])
      }
    }
  }
  flat(foldersStore.tree as unknown as { id: string; name: string; path?: string; children?: unknown[] }[])
  for (const f of filesStore.files) {
    const name = f.original_name || f.name
    if (q && !name.toLowerCase().includes(q)) continue
    items.push({
      type: 'file',
      id: f.id,
      name,
      insertText: `@${name}`,
      description: `${(f.size / 1024).toFixed(1)} KB`
    })
  }
  return items.slice(0, 24)
})

watch(mentionOpen, (open) => {
  if (!open) return
  if (skillsStore.skills.length === 0) void skillsStore.load().catch(() => undefined)
  if (filesStore.files.length === 0) void filesStore.load().catch(() => undefined)
})

const queue = ref<QueuedMessage[]>([])
const isDragging = ref(false)
const dragCounter = ref(0)

const canSend = computed(() => {
  // 超 maxInputChars 直接禁止发送（计数标红提示），而非仅变色
  if (draft.value.length > maxInputChars.value) return false
  const hasText = draft.value.trim().length > 0
  const hasAttach = attachments.value.length > 0
  return (hasText || hasAttach) && !isComposing.value
})

const isEnqueueMode = computed(() => props.streaming === true)

const permissionLevel = computed<PermissionLevel>(() => props.permission ?? 'auto')

/** 权限档位：值/标签/说明与风险色（下拉菜单消费；full 需要用户看清后果）。 */
const PERMISSION_ITEMS: Array<{ value: PermissionLevel; labelKey: string; descKey: string; danger?: boolean }> = [
  { value: 'restricted', labelKey: 'chat.perm.restricted', descKey: 'chat.perm.desc.restricted' },
  { value: 'confirm', labelKey: 'chat.perm.confirm', descKey: 'chat.perm.desc.confirm' },
  { value: 'auto', labelKey: 'chat.perm.auto', descKey: 'chat.perm.desc.auto' },
  { value: 'full', labelKey: 'chat.perm.full', descKey: 'chat.perm.desc.full', danger: true }
]

const permissionLabel = computed(
  () => PERMISSION_ITEMS.find((p) => p.value === permissionLevel.value)?.labelKey ?? 'chat.perm.confirm'
)

const permissionDesc = computed(
  () => PERMISSION_ITEMS.find((p) => p.value === permissionLevel.value)?.descKey ?? 'chat.perm.desc.confirm'
)

/** 完全访问/受限为非常态，chip 高亮提示；完全访问额外用危险色。 */
const permissionDanger = computed(() => permissionLevel.value === 'full')

// ===== @skill 提及可视化：从草稿解析已激活的技能，渲染成可辨识的 chip（问题：看不出是个 skill）=====
const activeSkillMentions = computed(() => {
  const tokens = draft.value.match(/@[\w\u4e00-\u9fa5.-]+/g) ?? []
  const names = new Set(tokens.map((tk) => tk.slice(1)))
  return [...names]
    .map((name) => skillsStore.skills.find((s) => s.name === name))
    .filter((s): s is NonNullable<typeof s> => Boolean(s))
})

/** 移除一条 @skill 提及（连同尾部空白），草稿其余内容不动。 */
function removeSkillMention(name: string): void {
  const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  draft.value = draft.value.replace(new RegExp(`\\s*@${escaped}`, 'g'), '').trimStart()
}

// ===== 文件上传（Wails 对话框选路径 → UploadFile 绑定） =====
function pickFile(): void {
  if (props.disabled || props.streaming) return
  void uploadFile()
}

/**
 * 技能 mini：直接弹出技能选择器（不依赖输入框里有没有 `@`）。
 * 选中的技能以 `@名称` 插入光标处，仍走后端 Skill 匹配的同一条路径。
 */
function openSkills(): void {
  if (props.disabled || props.streaming) return
  skillPickerOpen.value = !skillPickerOpen.value
  if (skillPickerOpen.value) {
    slashOpen.value = false
    mentionOpen.value = false
  }
}

/** 在光标处插入文本（替换已有选区），插入后光标停在文本末尾。 */
function insertAtCursor(text: string): void {
  const ta = textareaRef.value
  const start = ta?.selectionStart ?? draft.value.length
  const end = ta?.selectionEnd ?? start
  draft.value = draft.value.slice(0, start) + text + draft.value.slice(end)
  void nextTick(() => {
    ta?.focus()
    const pos = start + text.length
    ta?.setSelectionRange(pos, pos)
    onInput()
    autoResize()
  })
}

/** 技能选择器选中：插入 `@技能名 `（末尾空格方便直接继续写指令）。 */
function pickSkill(skill: Skill): void {
  skillPickerOpen.value = false
  insertAtCursor(`@${skill.name} `)
}

/** 点击浮层外部 / 输入框失焦时收起三个浮层，避免遮住消息流。 */
function onDocumentPointerDown(e: PointerEvent): void {
  if (!skillPickerOpen.value && !slashOpen.value && !mentionOpen.value) return
  const root = composerRef.value
  if (root && e.target instanceof Node && root.contains(e.target)) return
  skillPickerOpen.value = false
  slashOpen.value = false
  mentionOpen.value = false
}

async function uploadFile(_file?: File): Promise<void> {
  uploading.value = true
  try {
    const selected = await OpenFileDialog(t('chat.attachment'), '*.*')
    if (!selected) return
    const info = await UploadFile('', selected, '', '')
    attachments.value.push(info)
    toast.success(t('chat.uploadSuccess'), info.original_name || info.name)
  } catch (err) {
    toast.error(t('chat.uploadFailed'), err instanceof Error ? err.message : String(err))
  } finally {
    uploading.value = false
  }
}

function removeAttachment(id: string): void {
  attachments.value = attachments.value.filter((a) => a.id !== id)
}

function selectModel(id: string | null): void {
  emit('select-model', id)
}

function submit(): void {
  if (!canSend.value) return
  const text = draft.value.trim()
  if (!text && attachments.value.length === 0) return
  if (slashOpen.value && /^\/\w*\s*$/.test(draft.value)) {
    return
  }
  if (isEnqueueMode.value) {
    enqueueMessage(text)
    draft.value = ''
    attachments.value = []
    slashOpen.value = false
    slashQuery.value = ''
    mentionOpen.value = false
    resetHeight()
    void nextTick(() => textareaRef.value?.focus())
    return
  }
  emit('send', text, attachments.value.map((a) => a.id), currentParams())
  draft.value = ''
  attachments.value = []
  slashOpen.value = false
  slashQuery.value = ''
  mentionOpen.value = false
  resetHeight()
  void nextTick(() => textareaRef.value?.focus())
}

function enqueueMessage(text: string): void {
  queue.value.push({ id: `q-${Date.now()}`, text })
}

function onKeydown(e: KeyboardEvent): void {
  // 技能选择器打开时，导航键归它自己处理（内部搜索框已聚焦）
  if (skillPickerOpen.value && ['Enter', 'ArrowDown', 'ArrowUp', 'Escape'].includes(e.key)) return
  if (e.key === 'Enter' && !e.shiftKey && !isComposing.value) {
    e.preventDefault()
    if (slashOpen.value) {
      slashPaletteRef.value?.pickAt(0)
      return
    }
    if (mentionOpen.value && mentionItems.value.length > 0 && mentionActiveIndex.value >= 0) {
      pickMention(mentionItems.value[mentionActiveIndex.value])
      return
    }
    submit()
    return
  }
  if (slashOpen.value) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      slashPaletteRef.value?.moveActive(1)
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      slashPaletteRef.value?.moveActive(-1)
      return
    }
    if (e.key === 'Escape') {
      e.preventDefault()
      slashOpen.value = false
      slashQuery.value = ''
      return
    }
  }
  if (mentionOpen.value && mentionItems.value.length > 0) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      mentionActiveIndex.value = (mentionActiveIndex.value + 1) % mentionItems.value.length
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      mentionActiveIndex.value = mentionActiveIndex.value <= 0
        ? mentionItems.value.length - 1
        : mentionActiveIndex.value - 1
      return
    }
  }
  if (mentionOpen.value && e.key === 'Escape') {
    mentionOpen.value = false
  }
}

function pickMention(item: MentionItem): void {
  // 触发点优先用解析时记下的下标；拿不到（如外部调用）才退化到最后一个 @。
  const idx = mentionStart.value >= 0 ? mentionStart.value : draft.value.lastIndexOf('@')
  if (idx < 0) {
    mentionOpen.value = false
    return
  }
  const text = draft.value
  const before = text.slice(0, idx)
  const caret = textareaRef.value?.selectionStart ?? text.length
  const baseAfter = caret > idx ? text.slice(caret) : ''
  if (item.type === 'file') {
    if (!attachments.value.some((a) => a.id === item.id)) {
      attachments.value.push({
        id: item.id,
        name: item.name,
        original_name: item.name,
        file_type: '',
        mime_type: '',
        size: 0,
        status: '',
        session_id: null,
        folder_id: null,
        created_at: 0
      } as FileInfo)
    }
    draft.value = before + baseAfter + item.insertText + ' '
    toast.success(t('mention.fileAttached', item.name))
  } else {
    // 技能/文件夹/知识库：直接落 `@名称` 令牌——输入框干净，后端 Skill 匹配与模型理解都看得到名称
    draft.value = before + baseAfter + item.insertText + ' '
  }
  mentionOpen.value = false
  mentionQuery.value = ''
  mentionActiveIndex.value = -1
  void nextTick(() => textareaRef.value?.focus())
}

/** 触发字符左侧允许的前导符：行首 / 空白 / 常见中文与括号标点。 */
const TRIGGER_PREFIX = /(?:^|[\s\u3000(（【［{「『"'、，。；：！？,.;:!?])/

/**
 * 按光标位置解析触发态，而不是按整段文本的行尾。
 *
 * 行尾判断在「输入 @ 后又移动光标」「中间插入」「中文标点后输入」等场景下都会漏判，
 * 表现就是 `@` / `/` 时灵时不灵；以 caret 之前的片段为判定依据才是稳的。
 */
function onInput(): void {
  const ta = textareaRef.value
  const pos = ta?.selectionStart ?? draft.value.length
  const before = draft.value.slice(0, pos)

  const at = new RegExp(TRIGGER_PREFIX.source + '@([^\\s@]*)$').exec(before)
  if (at) {
    mentionStart.value = pos - at[1].length - 1
    mentionQuery.value = at[1]
    mentionOpen.value = true
    mentionActiveIndex.value = 0
    slashOpen.value = false
    slashStart.value = -1
    return
  }
  mentionOpen.value = false
  mentionStart.value = -1
  mentionActiveIndex.value = -1

  const sl = new RegExp(TRIGGER_PREFIX.source + '/([A-Za-z]*)$').exec(before)
  if (sl) {
    slashStart.value = pos - sl[1].length - 1
    slashQuery.value = sl[1]
    slashOpen.value = true
    return
  }
  slashOpen.value = false
  slashStart.value = -1
}

function pickSlash(cmd: SlashCommand): void {
  slashOpen.value = false
  switch (cmd.id) {
    case 'clear':
      void clearSession()
      break
    case 'new':
      emit('send', '__wb_new_session__', [], currentParams())
      break
    case 'focus':
      textareaRef.value?.focus()
      break
    case 'workspace':
      emit('pick-workspace')
      break
    case 'model':
      modelSelectorRef.value?.open()
      break
    case 'theme':
      void router.push('/settings')
      toast.info(t('slash.picked', cmd.labelKey ? t(cmd.labelKey) : (cmd.label ?? cmd.id)))
      break
    case 'attach':
      pickFile()
      break
    case 'regenerate':
      emit('regenerate')
      break
    case 'compact':
      void runCompact()
      break
    case 'tasks':
      window.dispatchEvent(new Event('workbaby:open-tasks'))
      break
    case 'agent':
      void submitAgentTask()
      break
    case 'trust': {
      // 循环切换权限档位（confirm → auto → full），替代无动作提示
      const cycle: PermissionLevel[] = ['confirm', 'auto', 'full']
      const idx = cycle.indexOf(permissionLevel.value)
      const next = cycle[(idx + 1) % cycle.length]
      const labelKey = PERMISSION_ITEMS.find((p) => p.value === next)?.labelKey ?? 'chat.perm.auto'
      emit('change-permission', next)
      toast.success(t('slash.trustHint', t(labelKey)))
      break
    }
    case 'export':
      void exportSessionMd()
      break
    case 'help':
      void showHelp()
      break
    default:
      // 后端命令：默认清空 + 提示。后端暂无专用接口的命令不会出现在面板
      toast.info(t('slash.picked', `/${cmd.id}`))
  }
}

async function runCompact(): Promise<void> {
  const id = chat.currentID
  if (!id) {
    toast.warning(t('chat.noSession'))
    return
  }
  const res = await chat.compactSession(id)
  if (res) {
    toast.success(t('slash.compactDone', res.compacted, res.freed_chars))
  } else {
    toast.error(t('slash.compactFailed'))
  }
}

/** /clear：真正清空当前会话消息（复用会话删除级联，后端持久化）。 */
async function clearSession(): Promise<void> {
  const id = chat.currentID
  if (!id) {
    toast.warning(t('chat.noSession'))
    return
  }
  try {
    await chat.clearMessages()
    toast.success(t('slash.cleared'))
  } catch (e) {
    toast.error(t('chat.operationFailed'), e instanceof Error ? e.message : String(e))
  }
}

/** /export：把当前会话导出为本地 Markdown 文件（浏览器 Blob 下载）。 */
async function exportSessionMd(): Promise<void> {
  const id = chat.currentID
  if (!id) {
    toast.warning(t('chat.noSession'))
    return
  }
  try {
    const rows = await apiGet<{ items?: Message[] }>(`/api/v1/chat/sessions/${id}/messages?limit=1000`)
    const items = rows?.items ?? []
    if (items.length === 0) {
      toast.info(t('slash.exportEmpty'))
      return
    }
    const lines: string[] = ['# WorkBaby 会话导出', '']
    for (const m of items) {
      const who = m.role === 'user' ? '你' : 'WorkBaby'
      const when = m.created_at ? new Date(m.created_at).toLocaleString() : ''
      lines.push(`## ${who} ${when ? '· ' + when : ''}`, '')
      if (m.thinking) lines.push(`> 思考：\n> ${m.thinking.replace(/\n/g, '\n> ')}`, '')
      lines.push(m.content ?? '', '')
    }
    const blob = new Blob([lines.join('\n')], { type: 'text/markdown;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `workbaby-${new Date().toISOString().slice(0, 10)}.md`
    a.click()
    URL.revokeObjectURL(url)
    toast.success(t('slash.exportDone'))
  } catch (e) {
    toast.error(t('slash.exportFailed'), e instanceof Error ? e.message : String(e))
  }
}

/** /agent：把一段子任务交给后台 Agent 跑（worker 执行，任务中心可看进度与结果）。 */
async function submitAgentTask(): Promise<void> {
  const id = chat.currentID
  if (!id) {
    toast.warning(t('chat.noSession'))
    return
  }
  try {
    const { value } = await ElMessageBox.prompt(
      t('slash.agentPromptTip'),
      t('slash.agentTitle'),
      {
        inputType: 'textarea',
        inputPlaceholder: t('slash.agentPlaceholder'),
        confirmButtonText: t('slash.agentSubmit'),
        cancelButtonText: t('ui.btn.cancel')
      }
    )
    const prompt = String(value ?? '').trim()
    if (!prompt) return
    await apiPost('/api/v1/tasks', { session_id: id, agent: '', prompt })
    toast.success(t('slash.agentSubmitted'))
    // 任务中心是聊天页右侧面板，跳 /tasks 会落到无关的会话列表页
    window.dispatchEvent(new Event('workbaby:open-tasks'))
  } catch (e) {
    if (e === 'cancel') return // 用户主动取消，不算失败
    toast.error(t('slash.agentFailed'), e instanceof Error ? e.message : String(e))
  }
}

/** /help：弹出命令清单（所有可执行命令与去向一目了然）。 */
async function showHelp(): Promise<void> {
  await ElMessageBox.alert(
    `<div style="line-height:1.9;font-size:13px">
      <b>/new</b> 新建会话<br/>
      <b>/clear</b> 清空当前会话<br/>
      <b>/compact</b> 压缩历史（后端摘要）<br/>
      <b>/model</b> 切换模型 · <b>/workspace</b> 绑定目录 · <b>/theme</b> 外观<br/>
      <b>/attach</b> 添加附件 · <b>/regenerate</b> 重新生成 · <b>/focus</b> 聚焦输入<br/>
      <b>/export</b> 导出会话 Markdown · <b>/tasks</b> 后台任务 · <b>/agent</b> 子代理<br/>
      <b>/trust</b> 查看权限 · <b>/help</b> 本帮助<br/>
      <span style="color:#888">输入 <code>/</code> 实时列出，输入 <code>@</code> 可引用技能/文件夹/文件</span>
    </div>`,
    t('slash.helpTitle'),
    { dangerouslyUseHTMLString: true, confirmButtonText: t('ui.btn.ok') }
  )
}

/** 输入框最大高度：视口 55%（可超过原 240px 上限，长文编辑不憋屈）。 */
const taMaxHeight = Math.round(window.innerHeight * 0.55)

/** 打字时只增不减：用户手动拉伸（resize: vertical）设置的高度由用户做主，不覆盖。 */
function autoResize(): void {
  const ta = textareaRef.value
  if (!ta) return
  if (ta.scrollHeight > ta.clientHeight) {
    ta.style.height = `${Math.min(ta.scrollHeight, taMaxHeight)}px`
  }
}

/** 清空草稿后高度回弹到单行。 */
function resetHeight(): void {
  const ta = textareaRef.value
  if (ta) ta.style.height = ''
}

function addToQueue(): void {
  const text = draft.value.trim()
  if (!text) return
  queue.value.push({ id: `q-${Date.now()}`, text })
  draft.value = ''
  resetHeight()
}

function removeQueueItem(id: string): void {
  queue.value = queue.value.filter((m) => m.id !== id)
  emit('remove-queue', id)
}

function flushQueue(): void {
  if (queue.value.length === 0) return
  const next = queue.value.shift()
  if (next) {
    emit('remove-queue', next.id)
    emit('send-queued', next.text)
  }
}

onMounted(() => {
  textareaRef.value?.focus()
  // 全局快捷键聚焦输入框（Ctrl+/ 与 Ctrl+Shift+P，App.vue 注册）
  window.addEventListener('wb:focus-input', focusFromShortcut)
  document.addEventListener('pointerdown', onDocumentPointerDown, true)
  // 拉取输入上限：system_settings.chat.maxInputChars（key 不存在时后端返 404，catcher 兜底默认）
  void loadMaxInputChars()
})

/** 异步拉取用户配置的输入上限；缺失/解析失败 fallback DEFAULT_MAX_INPUT_CHARS。 */
async function loadMaxInputChars(): Promise<void> {
  try {
    const row = await apiGet<{ k: string; v: string }>('/api/v1/kv/chat.maxInputChars')
    const n = parseInt(row.v, 10)
    if (Number.isFinite(n) && n > 0) maxInputChars.value = n
  } catch {
    // 后端无值 / 网络失败 / 用户没改 → 保持 DEFAULT_MAX_INPUT_CHARS
  }
}

onBeforeUnmount(() => {
  window.removeEventListener('wb:focus-input', focusFromShortcut)
  document.removeEventListener('pointerdown', onDocumentPointerDown, true)
})

function focusFromShortcut(): void {
  void nextTick(() => textareaRef.value?.focus())
}

watch(() => props.streaming, (isStreaming, prev) => {
  if (!isStreaming && prev === true && queue.value.length > 0) {
    flushQueue()
  }
})

function onDragEnter(e: DragEvent): void {
  if (props.disabled || props.streaming) return
  e.preventDefault()
  dragCounter.value++
  if (e.dataTransfer?.types.includes('Files')) {
    isDragging.value = true
  }
}

function onDragOver(e: DragEvent): void {
  if (props.disabled || props.streaming) return
  e.preventDefault()
}

function onDragLeave(e: DragEvent): void {
  if (props.disabled || props.streaming) return
  e.preventDefault()
  dragCounter.value--
  if (dragCounter.value <= 0) {
    dragCounter.value = 0
    isDragging.value = false
  }
}

async function onDrop(e: DragEvent): Promise<void> {
  if (props.disabled || props.streaming) return
  e.preventDefault()
  isDragging.value = false
  dragCounter.value = 0
  const files = e.dataTransfer?.files
  if (!files || files.length === 0) return
  for (const file of Array.from(files)) {
    await uploadFile(file)
  }
}

async function onPaste(e: ClipboardEvent): Promise<void> {
  if (props.disabled || props.streaming) return
  const items = e.clipboardData?.items
  if (!items) return
  const files: File[] = []
  for (const item of Array.from(items)) {
    if (item.kind === 'file') {
      const file = item.getAsFile()
      if (file) files.push(file)
    }
  }
  if (files.length > 0) {
    e.preventDefault()
    for (const file of files) {
      await uploadFile(file)
    }
  }
}

function onCompositionStart(): void {
  isComposing.value = true
}
function onCompositionEnd(): void {
  isComposing.value = false
}

function stop(): void {
  emit('stop')
}

/** 供父组件（ChatView）调用：把示例 prompt 灌进输入框并聚焦。
 *  暴露 setDraft / focusInput 让 ChatView 接住 MessageList 的 use-quick-prompt 事件。
 */
function setDraft(text: string): void {
  draft.value = text
  void nextTick(() => {
    textareaRef.value?.focus()
    autoResize()
  })
}

defineExpose({
  setDraft,
  /** 追加文本（工作区文件树「添加到聊天」）：保留用户已输入内容，只追加引用后聚焦。 */
  appendText: (text: string) => {
    draft.value = draft.value.trimEnd() + (draft.value.trim() ? ' ' : '') + text + ' '
    void nextTick(() => textareaRef.value?.focus())
  },
  focusInput: () => textareaRef.value?.focus()
})
</script>

<template>
  <div
    ref="composerRef"
    class="composer relative"
    @dragenter="onDragEnter"
    @dragover="onDragOver"
    @dragleave="onDragLeave"
    @drop="onDrop"
  >
    <Transition
      enter-active-class="transition-opacity duration-200"
      enter-from-class="opacity-0"
      enter-to-class="opacity-100"
      leave-active-class="transition-opacity duration-150"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <div
        v-if="isDragging"
        class="absolute inset-0 z-50 flex items-center justify-center border-2 border-dashed border-wb-primary bg-wb-primary/10"
      >
        <div class="flex flex-col items-center gap-2 text-wb-primary">
          <Upload class="h-8 w-8" />
          <span class="text-sm font-medium">{{ t('chat.dropToUpload') }}</span>
        </div>
      </div>
    </Transition>

    <!-- 输入容器（原型 .composer-in）：聚焦时主色描边 + 光晕。队列 / 技能提及 / 附件
         条带都收进容器内，视觉上属于「这一条消息」的一部分 -->
    <div class="composer-in">
      <MidTurnQueue class="comp-strip" :queue="queue" @remove="removeQueueItem" @flush="flushQueue" />

      <div v-if="activeSkillMentions.length > 0" class="skill-mention-strip">
        <span
          v-for="sk in activeSkillMentions"
          :key="sk.name"
          class="skill-mention-chip"
          :title="sk.description ?? ''"
        >
          <el-icon :size="11"><Sparkles /></el-icon>
          <span class="max-w-[200px] truncate">{{ t('chat.skillMentionLabel', sk.name) }}</span>
          <button
            type="button"
            class="ml-0.5 cursor-pointer opacity-70 hover:opacity-100"
            :aria-label="t('common.delete')"
            @click="removeSkillMention(sk.name)"
          >×</button>
        </span>
      </div>

      <AttachmentStrip class="comp-strip" :attachments="attachments" @remove="removeAttachment" />

      <div class="relative">
        <textarea
          ref="textareaRef"
          v-model="draft"
          rows="1"
          :placeholder="t('chat.placeholder')"
          :disabled="disabled || streaming"
          @input="onInput(); autoResize()"
          @keydown="onKeydown"
          @paste="onPaste"
          @compositionstart="onCompositionStart"
          @compositionend="onCompositionEnd"
        />

        <SlashCommandPalette
          ref="slashPaletteRef"
          :visible="slashOpen"
          :query="slashQuery"
          :backend-commands="chat.commands"
          @pick="pickSlash"
          @close="slashOpen = false"
        />

        <MentionPicker
          :visible="mentionOpen"
          :query="mentionQuery"
          :items="mentionItems"
          :active-index="mentionActiveIndex"
          @pick="pickMention"
          @update-active="mentionActiveIndex = $event"
          @close="mentionOpen = false"
        />
      </div>

      <!-- 工具行（原型 .composer-bar）：胶囊 mini 控件 + 上下文计数 + 发送键 -->
      <div class="composer-bar">
        <ModelSelector
          ref="modelSelectorRef"
          :models="props.models ?? []"
          :selectedModelID="props.selectedModelID"
          :circuit-states="props.circuitStates"
          @select-model="selectModel"
        />

        <!-- 权限 mini：下拉选择权限档位。trigger 必须是单个原生元素，el-dropdown 直接挂 -->
        <el-dropdown trigger="click" @command="(v: PermissionLevel) => emit('change-permission', v)">
          <button
            type="button"
            class="mini"
            :title="t(permissionDesc)"
            :class="{ 'mini--active': permissionLevel !== 'auto', 'mini--danger': permissionDanger }"
          >
            <Shield class="mic" />
            {{ t(permissionLabel) }}
            <ChevronDown class="mic mic-chev" />
          </button>
          <template #dropdown>
            <el-dropdown-menu class="wb-perm-menu">
              <el-dropdown-item
                v-for="p in PERMISSION_ITEMS"
                :key="p.value"
                :command="p.value"
                :class="{ 'is-danger': p.danger, 'is-selected': permissionLevel === p.value }"
              >
                <div class="flex flex-col gap-0.5 py-0.5">
                  <span class="flex items-center gap-1.5 text-xs font-medium">
                    <span
                      v-if="permissionLevel === p.value"
                      class="inline-block h-1.5 w-1.5 rounded-full bg-wb-primary"
                    />
                    {{ t(p.labelKey) }}
                  </span>
                  <span class="max-w-[260px] text-[11px] leading-snug text-wb-muted">{{ t(p.descKey) }}</span>
                </div>
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>

        <!-- 采样参数 mini：展示生效值 + 可覆盖（与设置页同一数据源） -->
        <ParamsPopover ref="paramsRef" :effective="props.effectiveParams" />

        <!-- 技能 mini：直接弹出技能选择器（选中后以 @技能名 插入光标处） -->
        <div class="relative">
          <button
            type="button"
            class="mini"
            :class="{ 'mini--active': skillPickerOpen }"
            :title="t('nav.skills')"
            :disabled="disabled || streaming"
            @click="openSkills"
          >
            <Zap class="mic" />
            {{ t('nav.skills') }}
          </button>
          <SkillPicker :visible="skillPickerOpen" @pick="pickSkill" @close="skillPickerOpen = false" />
        </div>

        <!-- 附件 mini -->
        <button
          type="button"
          class="mini"
          :title="t('chat.attachment')"
          :disabled="disabled || streaming || uploading"
          @click="pickFile"
        >
          <Paperclip class="mic" />
          {{ t('chat.attachment') }}
        </button>

        <!-- 流式中入队：不打断当前回合，排队等待 flush -->
        <button
          v-if="streaming && draft.trim()"
          type="button"
          class="mini"
          :title="t('queue.enqueue')"
          @click="addToQueue"
        >
          <Plus class="mic" />
          {{ t('queue.enqueue') }}
        </button>

        <span class="sp" />
        <span v-if="draft.length > 0" class="ctx-num" :class="{ over: draft.length > maxInputChars }">
          {{ draft.length }} / {{ maxInputChars }}
        </span>
        <ContextUsagePopover :used="ctxUsed ?? 0" :max="ctxMax ?? 128000" />

        <button
          v-if="!streaming"
          type="button"
          class="send"
          :disabled="!canSend"
          :aria-label="t('ui.btn.send')"
          @click="submit"
        >
          <Send />
        </button>
        <button v-else type="button" class="send send--stop" :aria-label="t('chat.stop')" @click="stop">
          <Square class="fill-current" />
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* ===== 原型 composer：全局 wb-ui.css 已提供 .composer / .composer-in / .mini / .send
   基础样式，这里只做组件级微调。chip 触发器的幽灵化在各子组件自己的 scoped
   样式内完成（ParamsPopover / ModelSelector）——父组件 :deep 跨组件覆盖会因
   样式注入顺序而失效，不要再加回来 ===== */

.mini:disabled,
.composer :deep(.chip-btn:disabled) {
  opacity: 0.5;
  cursor: not-allowed;
}

/* mini 内联图标：统一 12.5px，chevron 更小更淡 */
.mini :deep(svg.mic) {
  width: 12.5px;
  height: 12.5px;
  flex: none;
}
.mini :deep(svg.mic-chev) {
  width: 10px;
  height: 10px;
  opacity: 0.7;
}

/* 非默认权限态需要被看见：只用文字色 + 底色提示，不描边（描边是框线噪音的来源之一） */
.mini--active {
  color: var(--wb-warning);
  background: color-mix(in srgb, var(--wb-warning) 10%, transparent);
}
.mini--danger {
  color: var(--wb-danger);
  background: color-mix(in srgb, var(--wb-danger) 12%, transparent);
}

/* 草稿字符计数：mono 数字，超限标红 */
.ctx-num {
  flex: none;
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: 11px;
  color: var(--wb-muted);
  font-variant-numeric: tabular-nums;
}
.ctx-num.over {
  color: var(--wb-danger);
}

/* 发送键：禁用弱化；停止键 danger 底 + 呼吸动画 */
.send:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.send--stop {
  background: var(--wb-danger);
  animation: stop-breathe 1.6s ease-in-out infinite;
}
@keyframes stop-breathe {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.72;
  }
}

/* textarea 占位符：与原型一致的弱化提示 */
.composer-in textarea::placeholder {
  color: color-mix(in srgb, var(--wb-muted) 75%, transparent);
}

/* 队列 / 附件条带：内边距对齐 textarea（13px），与输入文本视觉同列 */
.comp-strip {
  padding: 10px 13px 0;
}

/* 激活的 @skill 提及 chip：与附件条同级，视觉上明确「这条消息带着技能上下文」；
   只用底色 + 文字色，不描边；内边距对齐 textarea（13px） */
.skill-mention-strip {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding: 10px 13px 0;
}
.skill-mention-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  max-width: 240px;
  height: 24px;
  padding: 0 9px;
  border: 1px solid transparent;
  border-radius: 9999px;
  background: color-mix(in srgb, var(--wb-lavender) 14%, transparent);
  color: var(--wb-lavender);
  font-size: 11px;
  line-height: 1;
  white-space: nowrap;
}

/* 权限下拉：危险档提示 */
.wb-perm-menu :deep(.el-dropdown-menu__item.is-danger) {
  color: var(--wb-danger);
}
</style>