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
  FolderOpen,
  Shield,
  Cpu,
  Link2,
  RotateCcw,
  Sparkles,
  ChevronDown
} from '@/components/common/icons'
import { apiGet, apiPost } from '@/api/client'
import { UploadFile, OpenFileDialog } from '@/wailsjs/go/main/App'
import { useToast } from '@/composables/useToast'
import { t } from '@/i18n'
import { useChatStore } from '@/stores/chat'
import { ElMessageBox } from 'element-plus'
import type { AvailableModel, CircuitState, EffectiveParams, FileInfo, Message, QueuedMessage } from '@/types/api'
import ModelSelector from '@/components/chat/ModelSelector.vue'
import AttachmentStrip from '@/components/chat/composer/AttachmentStrip.vue'
import SlashCommandPalette, { type SlashCommand } from '@/components/chat/composer/SlashCommandPalette.vue'
import MentionPicker, { type MentionItem } from '@/components/chat/composer/MentionPicker.vue'
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
  /** A3 痛点：熔断状态 Map（id → CircuitState）。 */
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

const slashOpen = ref(false)
const slashQuery = ref('')
const slashPaletteRef = ref<InstanceType<typeof SlashCommandPalette> | null>(null)
const mentionOpen = ref(false)
const mentionQuery = ref('')
const mentionActiveIndex = ref(-1)

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

// ===== 顶部 chip 显示辅助 =====
const workspaceDisplay = computed(() => {
  const wp = props.workspacePath
  if (!wp) return t('chat.workspace.default')
  // 取最后两级路径（如 D:\projects\my-app → projects/my-app）
  const parts = wp.replace(/\\/g, '/').split('/').filter(Boolean)
  if (parts.length <= 2) return wp
  return parts.slice(-2).join('/')
})

const workspaceFull = computed(() => {
  const wp = props.workspacePath
  return wp ? wp : t('chat.workspace.defaultFull')
})

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
    void nextTick(() => textareaRef.value?.focus())
    return
  }
  emit('send', text, attachments.value.map((a) => a.id), currentParams())
  draft.value = ''
  attachments.value = []
  slashOpen.value = false
  slashQuery.value = ''
  mentionOpen.value = false
  void nextTick(() => textareaRef.value?.focus())
}

function enqueueMessage(text: string): void {
  queue.value.push({ id: `q-${Date.now()}`, text })
}

function onKeydown(e: KeyboardEvent): void {
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
  const text = draft.value
  const idx = text.lastIndexOf('@')
  if (idx < 0) {
    mentionOpen.value = false
    return
  }
  const before = text.slice(0, idx)
  const baseAfter = text.slice(idx).replace(/@\S*$/, '')
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

function onInput(): void {
  const text = draft.value
  const slashMatch = /(?:^|\s)(\/[A-Za-z]*)$/.exec(text)
  if (slashMatch) {
    slashOpen.value = true
    slashQuery.value = slashMatch[1].slice(1)
  } else {
    slashOpen.value = false
    slashQuery.value = ''
  }
  const atMatch = /(?:^|\s)(@\S*)$/.exec(text)
  if (atMatch) {
    mentionOpen.value = true
    mentionQuery.value = atMatch[1].slice(1)
    mentionActiveIndex.value = 0
  } else {
    mentionOpen.value = false
    mentionActiveIndex.value = -1
  }
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
      void router.push('/tasks')
      break
    case 'agent':
      void submitAgentTask()
      break
    case 'trust':
      toast.info(t('slash.trustHint', t(permissionLabel.value)))
      break
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
    void router.push('/tasks')
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

function autoResize(): void {
  const ta = textareaRef.value
  if (!ta) return
  ta.style.height = 'auto'
  ta.style.height = `${Math.min(ta.scrollHeight, 240)}px`
}

function addToQueue(): void {
  const text = draft.value.trim()
  if (!text) return
  queue.value.push({ id: `q-${Date.now()}`, text })
  draft.value = ''
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

defineExpose({ setDraft, focusInput: () => textareaRef.value?.focus() })
</script>

<template>
  <div
    class="composer-card relative border-t border-wb-border bg-wb-surface"
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
        class="absolute inset-0 z-50 flex items-center justify-center rounded-xl border-2 border-dashed border-wb-primary bg-wb-primary/10 pointer-events-none"
      >
        <div class="flex flex-col items-center gap-2 text-wb-primary">
          <Upload class="h-8 w-8" />
          <span class="text-sm font-medium">{{ t('chat.dropToUpload') }}</span>
        </div>
      </div>
    </Transition>

    <div class="mx-auto flex max-w-3xl flex-col gap-3 p-4">
      <!-- 顶部 chip 行：模型 / 工作区 / 权限 / 采样参数。
           视觉层级：次要控制项，统一紧凑 chip 样式（.chip-btn），不抢输入框焦点。 -->
      <div class="flex flex-wrap items-center gap-1.5 px-1">
        <ModelSelector
          ref="modelSelectorRef"
          :models="props.models ?? []"
          :selectedModelID="props.selectedModelID"
          :circuit-states="props.circuitStates"
          @select-model="selectModel"
        />

        <el-tooltip :content="workspaceFull" placement="top">
          <button type="button" class="chip-btn" @click="emit('pick-workspace')">
            <el-icon :size="12" color="var(--wb-primary)"><FolderOpen /></el-icon>
            <span class="max-w-[140px] truncate">{{ workspaceDisplay }}</span>
            <!-- 已绑定态用 Link2 图标内联表达，不再单独占一个文本节点 -->
            <el-icon v-if="props.workspacePath" :size="11" class="text-wb-mint"><Link2 /></el-icon>
            <span v-else class="text-wb-muted/70">{{ t('chat.workspace.chip') }}</span>
          </button>
        </el-tooltip>

        <!-- 权限 chip：下拉选择权限档位。trigger 必须是单个原生元素，el-tooltip 改放内层避免嵌套导致点击失效 -->
        <el-dropdown trigger="click" @command="(v: PermissionLevel) => emit('change-permission', v)">
          <button
            type="button"
            class="chip-btn"
            :title="t(permissionDesc)"
            :class="{ 'chip-btn--active': permissionLevel !== 'auto', 'chip-btn--danger': permissionDanger }"
          >
            <el-icon :size="12"><Shield /></el-icon>
            {{ t(permissionLabel) }}
            <el-icon :size="10" class="ml-0.5 text-wb-muted/60"><ChevronDown /></el-icon>
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

        <!-- 采样参数 chip：展示生效值 + 可覆盖（与设置页同一数据源） -->
        <ParamsPopover ref="paramsRef" :effective="props.effectiveParams" />
      </div>

      <!-- 中间输入区：队列 + 附件 + textarea + popovers -->
      <div class="rounded-lg border border-wb-border bg-wb-surface transition-all focus-within:border-wb-primary">
        <MidTurnQueue :queue="queue" @remove="removeQueueItem" @flush="flushQueue" />
        <div v-if="activeSkillMentions.length > 0" class="skill-mention-strip px-4 pt-2">
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
        <AttachmentStrip :attachments="attachments" @remove="removeAttachment" />

        <div class="relative flex flex-1 flex-col gap-1 px-4 pb-3 pt-2">
          <textarea
            ref="textareaRef"
            v-model="draft"
            rows="2"
            :placeholder="t('chat.placeholder')"
            class="max-h-60 min-h-[3.5rem] w-full resize-y border-0 bg-transparent text-sm leading-relaxed text-wb-ink outline-none placeholder:text-wb-muted/70 disabled:opacity-50"
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

        <!-- 底部行：左 = 附件 / 入队 / 上下文占用 / 字符数（次要信息聚合）
     | 右 = 圆形发送（主操作，唯一强调色） -->
        <div class="flex items-center justify-between gap-2 border-t border-wb-border/50 px-3 py-2">
          <div class="flex min-w-0 items-center gap-2">
            <el-tooltip :content="t('chat.attachment')" placement="top">
              <span class="inline-flex">
                <el-button
                  text
                  circle
                  :loading="uploading"
                  :disabled="disabled || streaming"
                  @click="pickFile"
                >
                  <el-icon><Paperclip /></el-icon>
                </el-button>
              </span>
            </el-tooltip>
            <el-tooltip v-if="streaming && draft.trim()" :content="t('queue.enqueue')" placement="top">
              <el-button text circle @click="addToQueue">
                <el-icon><Plus /></el-icon>
              </el-button>
            </el-tooltip>

            <!-- 上下文占用：与字符数同处一区，用竖线分隔，压缩纵向空间 -->
            <span v-if="draft.length > 0" class="h-3 w-px shrink-0 bg-wb-border" />
            <span v-if="draft.length > 0" class="shrink-0 text-[10px] tabular-nums" :class="draft.length > maxInputChars ? 'text-wb-danger' : 'text-wb-muted'">
              {{ draft.length }} / {{ maxInputChars }}
            </span>
            <span class="h-3 w-px shrink-0 bg-wb-border" />
            <ContextUsagePopover :used="ctxUsed ?? 0" :max="ctxMax ?? 128000" />
          </div>

          <div class="flex shrink-0 items-center gap-2">
            <el-button
              v-if="!streaming"
              type="primary"
              circle
              :disabled="!canSend"
              :aria-label="t('ui.btn.send')"
              @click="submit"
            >
              <el-icon><Send /></el-icon>
            </el-button>
            <el-button v-else type="danger" circle class="wb-stop-breathe" :aria-label="t('chat.stop')" @click="stop">
              <el-icon><Square class="fill-current" /></el-icon>
            </el-button>
          </div>
        </div>
      </div>

      <!-- 底部提示行：仅在有内容可提示时出现，避免空转占高度 -->
      <div v-if="!streaming && !draft.trim()" class="px-1 text-[10px] text-wb-muted/80">
        <span class="flex items-center gap-1">
          <el-icon :size="11"><Cpu /></el-icon>
          {{ t('chat.sendHint') }}
        </span>
      </div>
      <div v-else-if="streaming && queue.length > 0" class="px-1 text-[10px] text-wb-primary">
        <span class="flex items-center gap-1">
          <el-icon :size="11"><RotateCcw /></el-icon>
          {{ t('queue.queued', queue.length) }}
        </span>
      </div>
      <div v-else-if="draft.trim()" class="px-1 text-[10px] text-wb-muted/80">
        <span class="flex items-center gap-1">
          <kbd class="rounded border border-wb-border bg-wb-surface px-1 font-mono text-[9px]">Enter</kbd>
          {{ t('chat.sendHintKbd') }}
        </span>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 输入区在 focus-within 时外层光晕（与 textarea focus 联动） */
.composer-card :focus-within > .rounded-2xl {
  box-shadow:
    0 0 0 1px color-mix(in srgb, var(--wb-primary) 18%, transparent),
    var(--wb-shadow-lg);
}

.composer-card .tabular-nums,
.composer-card .font-mono {
  font-variant-numeric: tabular-nums;
}

/* 顶部次要控制项 chip：统一紧凑外观，视觉权重低于输入框与发送按钮。
   比 el-button small 更矮（h-6）更窄，一行能放下 4 个不换行。 */
.chip-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 24px;
  max-width: 220px;
  padding: 0 8px;
  border: 1px solid var(--wb-border);
  border-radius: 9999px;
  background: var(--wb-surface);
  color: var(--wb-muted);
  font-size: 11px;
  line-height: 1;
  white-space: nowrap;
  cursor: pointer;
  transition:
    border-color 0.15s ease,
    color 0.15s ease,
    background-color 0.15s ease;
}
.chip-btn:hover {
  border-color: color-mix(in srgb, var(--wb-primary) 45%, transparent);
  color: var(--wb-primary-strong);
}
.chip-btn:focus-visible {
  outline: none;
  border-color: var(--wb-primary);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--wb-primary) 14%, transparent);
}
/* 非默认态（如权限切到 confirm/restricted）需要被看见 */
.chip-btn--active {
  border-color: color-mix(in srgb, var(--wb-warning) 55%, transparent);
  color: var(--wb-warning);
  background: color-mix(in srgb, var(--wb-warning) 8%, transparent);
}
/* 完全访问：危险语义色，与 warning 态区分 */
.chip-btn--danger {
  border-color: color-mix(in srgb, var(--wb-danger) 60%, transparent);
  color: var(--wb-danger);
  background: color-mix(in srgb, var(--wb-danger) 10%, transparent);
}
/* 权限下拉：危险档提示 */
.wb-perm-menu :deep(.el-dropdown-menu__item.is-danger) {
  color: var(--wb-danger);
}
/* 激活的 @skill 提及 chip：与附件条同级，视觉上明确「这条消息带着技能上下文」 */
.skill-mention-strip {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.skill-mention-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  max-width: 240px;
  height: 24px;
  padding: 0 9px;
  border: 1px solid color-mix(in srgb, var(--wb-lavender) 45%, transparent);
  border-radius: 9999px;
  background: color-mix(in srgb, var(--wb-lavender) 12%, transparent);
  color: var(--wb-lavender);
  font-size: 11px;
  line-height: 1;
  white-space: nowrap;
}
</style>