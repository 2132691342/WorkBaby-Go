<script setup lang="ts">
import { computed, ref } from 'vue'
import { storeToRefs } from 'pinia'
import {
  Pencil, Send, AlertTriangle, Lock, File as FileIcon
} from '@/components/common/icons'
import { t } from '@/i18n'
import { useChatStore } from '@/stores/chat'
import { useToast } from '@/composables/useToast'

/**
 * 审批语义卡：从「命令文本块」升级为按工具分类的卡片。
 *
 * <p>按 tool 名称解析 approval.command（`name(jsonArgs)`）→ 不同工具给不同预览：
 *   - file_write：显示目标路径 + 写入预览（before 长度 + after 内容片段 + append 标记）；
 *   - exec：命令白名单状态说明 + 参数数组；
 *   - 其它：args JSON pretty-print。
 *
 * <p>风险徽标：needs_approval（warning）/ irreversible（error）。
 * 总会话免审的语义由后端 ApprovalService 承担（同命令 needs_approval 批准后免审，
 * irreversible 每次必问），前端只展示约定。
 */
const chat = useChatStore()
const toast = useToast()
const { pendingApproval } = storeToRefs(chat)

/** 补充输入模式（request_input 工具）：卡片变为问答形态而非批准/拒绝。 */
const isInput = computed(() => pendingApproval.value?.risk === 'input_required')
const inputDraft = ref('')

const parsed = computed<ParsedCommand | null>(() => {
  const cmd = pendingApproval.value?.command ?? ''
  return parseCommand(cmd)
})

const isIrreversible = computed(() => pendingApproval.value?.risk === 'irreversible')

const riskLabel = computed(() => {
  if (isIrreversible.value) return t('chat.approvalRiskIrreversible')
  return t('chat.approvalRiskNeeds')
})

const riskType = computed<'error' | 'warning'>(() =>
  isIrreversible.value ? 'error' : 'warning'
)

const reasonText = computed(() => pendingApproval.value?.reason ?? '')

interface ParsedArgs { [k: string]: unknown }

interface ParsedCommand {
  name: string
  args: ParsedArgs
  /** 原 args JSON（pretty 用）。 */
  rawArgs: string
}

function parseCommand(cmd: string): ParsedCommand | null {
  const trimmed = cmd.trim()
  if (!trimmed) return null
  const m = /^(.+?)\((.*)\)$/s.exec(trimmed)
  if (!m) return { name: trimmed, args: {}, rawArgs: '' }
  const name = m[1].trim()
  const rawArgs = m[2].trim()
  if (!rawArgs) return { name, args: {}, rawArgs: '' }
  try {
    return { name, args: JSON.parse(rawArgs), rawArgs }
  } catch {
    return { name, args: {}, rawArgs }
  }
}

const toolKind = computed(() => {
  const n = parsed.value?.name ?? ''
  if (n === 'file_write' || n === 'file_read' || n === 'file_list') return 'file'
  if (n === 'exec' || n === 'run_skill_script') return 'exec'
  if (n === 'webfetch' || n === 'http') return 'http'
  return 'generic'
})

const toolTitle = computed(() => {
  switch (parsed.value?.name) {
    case 'file_write': return t('chat.approvalToolFileWrite')
    case 'file_read': return t('chat.approvalToolFileRead')
    case 'file_list': return t('chat.approvalToolFileList')
    case 'exec': return t('chat.approvalToolExec')
    case 'run_skill_script': return t('chat.approvalToolSkill')
    case 'http': return t('chat.approvalToolHttp')
    case 'webfetch': return t('chat.approvalToolWebfetch')
    default: return parsed.value?.name || t('chat.approvalToolGeneric')
  }
})

const fileWriteArgs = computed(() => {
  if (toolKind.value !== 'file' || parsed.value?.name !== 'file_write') return null
  const a = parsed.value!.args
  return {
    path: typeof a.path === 'string' ? a.path : '',
    content: typeof a.content === 'string' ? a.content : '',
    append: a.append === true
  }
})

const execArgs = computed(() => {
  if (parsed.value?.name !== 'exec') return null
  const a = parsed.value!.args
  return {
    command: typeof a.command === 'string' ? a.command : '',
    args: Array.isArray(a.args) ? a.args.map(String) : [],
    cwd: typeof a.cwd === 'string' ? a.cwd : ''
  }
})

/** 预览字符上限：超过的折叠为占位。 */
const PREVIEW_LIMIT = 600

function previewContent(content: string): { text: string; truncated: boolean } {
  if (content.length <= PREVIEW_LIMIT) return { text: content, truncated: false }
  return {
    text: content.slice(0, PREVIEW_LIMIT) + '\n…',
    truncated: true
  }
}

const fileWritePreview = computed(() => {
  const a = fileWriteArgs.value
  if (!a) return null
  return previewContent(a.content)
})

const argsJson = computed(() => {
  const p = parsed.value
  if (!p || !p.rawArgs) return null
  if (toolKind.value === 'file' && parsed.value?.name === 'file_write') return null
  return previewContent(p.rawArgs)
})

async function approve(): Promise<void> {
  await chat.approveApproval()
  toast.success(t('chat.approvalApproved'))
}

/** 拒绝执行（安全审批语义）。 */
async function deny(): Promise<void> {
  await chat.denyApproval()
}

/** 跳过补充输入：走后端 /skip；补充输入用 /decide 会因通道不匹配报 4003。 */
async function skip(): Promise<void> {
  await chat.skipApproval()
  toast.success(t('chat.approvalSkipped'))
}

async function sendAnswer(): Promise<void> {
  const text = inputDraft.value.trim()
  if (!text) return
  inputDraft.value = ''
  await chat.answerApproval(text)
}
</script>

<template>
  <el-alert
    v-if="pendingApproval"
    :type="isInput ? 'info' : riskType"
    :closable="false"
    show-icon
    class="approval-card"
  >
    <template #title>
      <div class="flex items-center gap-2">
        <span class="text-sm font-semibold">{{ isInput ? t('chat.inputTitle') : toolTitle }}</span>
        <el-tag
          size="small"
          :type="isInput ? 'primary' : isIrreversible ? 'danger' : 'warning'"
          effect="dark"
          round
        >
          <el-icon class="mr-0.5 align-[-2px]">
            <AlertTriangle />
          </el-icon>
          {{ isInput ? t('chat.inputTag') : riskLabel }}
        </el-tag>
      </div>
    </template>

    <!-- 补充输入模式：问题 + 回复框 -->
    <template v-if="isInput">
      <p class="mb-2 whitespace-pre-wrap text-xs text-wb-ink">{{ pendingApproval.command }}</p>
      <el-input
        v-model="inputDraft"
        type="textarea"
        :rows="2"
        :placeholder="t('chat.inputPlaceholder')"
        @keydown.enter.ctrl="sendAnswer"
      />
      <div class="mt-3 flex gap-2">
        <el-button type="primary" size="small" :disabled="!inputDraft.trim()" @click="sendAnswer">
          <el-icon class="mr-1"><Send /></el-icon>
          {{ t('chat.inputSend') }}
        </el-button>
        <el-button size="small" @click="skip">{{ t('chat.inputSkip') }}</el-button>
      </div>
      <p class="mt-1 text-[11px] text-wb-muted">{{ t('chat.inputHint') }}</p>
    </template>

    <!-- file_write：路径 + before/after 预览 -->
    <template v-else-if="fileWriteArgs">
      <div class="mb-2 flex items-center gap-1.5 text-xs text-wb-muted">
        <el-icon class="text-wb-primary"><FileIcon /></el-icon>
        <span class="font-mono break-all">{{ fileWriteArgs.path || '?' }}</span>
        <el-tag v-if="fileWriteArgs.append" size="small" type="info">
          {{ t('chat.approvalAppend') }}
        </el-tag>
      </div>
      <div v-if="fileWriteArgs.content === ''" class="rounded-md border border-dashed border-wb-border bg-wb-surface-2 px-3 py-2 text-xs text-wb-muted">
        {{ t('chat.approvalEmptyWrite') }}
      </div>
      <pre
        v-else
        class="overflow-x-auto rounded-md border border-wb-border bg-wb-surface-2 px-3 py-2 font-mono text-[11px] leading-relaxed text-wb-ink"
      ><span class="block text-wb-muted">/* {{ t('chat.approvalAfterPreview') }} */</span>{{ fileWritePreview?.text }}</pre>
      <p v-if="fileWritePreview?.truncated" class="mt-1 text-[10px] text-wb-muted">
        {{ t('chat.approvalTruncated', PREVIEW_LIMIT) }}
      </p>
    </template>

    <!-- exec：命令 + 参数 + 白名单说明 -->
    <template v-else-if="execArgs">
      <div class="mb-2 flex items-center gap-1.5 text-xs text-wb-muted">
        <el-icon class="text-wb-primary"><Send /></el-icon>
        <span class="font-mono">{{ execArgs.command || '?' }}</span>
      </div>
      <div v-if="execArgs.args.length" class="mb-2 flex flex-wrap gap-1">
        <el-tag
          v-for="(arg, i) in execArgs.args"
          :key="i"
          size="small"
          effect="plain"
          class="font-mono"
        >
          {{ arg }}
        </el-tag>
      </div>
      <div v-if="execArgs.cwd" class="mb-2 flex items-center gap-1.5 text-[11px] text-wb-muted">
        <span class="text-wb-muted">cwd</span>
        <span class="font-mono">{{ execArgs.cwd }}</span>
      </div>
    </template>

    <!-- 其它工具：args JSON pretty-print -->
    <pre
      v-else-if="argsJson"
      class="overflow-x-auto rounded-md border border-wb-border bg-wb-surface-2 px-3 py-2 font-mono text-[11px] leading-relaxed text-wb-ink"
    >{{ argsJson.text }}</pre>

    <!-- 兜底：原始命令 -->
    <pre
      v-else
      class="overflow-x-auto rounded-md bg-wb-surface-2 px-3 py-2 font-mono text-xs text-wb-ink"
    >{{ pendingApproval.command }}</pre>

    <template v-if="!isInput">
      <p class="mt-2 flex items-center gap-1 text-xs text-wb-muted">
        <el-icon><Lock /></el-icon>
        {{ reasonText }}
      </p>

      <p class="mt-1 text-[11px] text-wb-muted">
        <span v-if="isIrreversible">{{ t('chat.approvalIrreversibleHint') }}</span>
        <span v-else>{{ t('chat.approvalSessionHint') }}</span>
      </p>

      <div class="mt-3 flex gap-2">
        <el-button
          :type="isIrreversible ? 'danger' : 'primary'"
          size="small"
          @click="approve"
        >
          <el-icon class="mr-1"><Pencil /></el-icon>
          {{ isIrreversible ? t('chat.approvalApproveIrreversible') : t('chat.approvalApprove') }}
        </el-button>
        <el-button size="small" @click="deny">{{ t('chat.approvalDeny') }}</el-button>
      </div>
    </template>
  </el-alert>
</template>

<style scoped>
.approval-card :deep(.el-alert__content) {
  padding: 4px 0;
}
</style>
