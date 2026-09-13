<script setup lang="ts">
/**
 * Git 面板（右侧栏）：分支切换 + 变更文件 + 单文件 diff + 提交 + 最近历史。
 *
 * <p>状态源是会话工作区里的 .git；工作区未绑定时走默认根（可能是空目录 → 引导文案）。
 * diff 渲染复用主链路 DiffView（同一份 parseDiffRows）。
 */
import { computed, ref, watch } from 'vue'
import { GitBranch, RefreshCw } from '@/components/common/icons'
import { ElMessageBox } from 'element-plus'
import { apiGet, apiPost } from '@/api/client'
import { useChatStore } from '@/stores/chat'
import type { Session } from '@/types/api'
import { t } from '@/i18n'
import { useToast } from '@/composables/useToast'
import DiffView from '@/components/chat/DiffView.vue'

interface GitFile { path: string; status: string; staged: boolean }
interface GitCommit { hash: string; author: string; date: string; subject: string }
interface GitOverview {
  root: string
  branch: string
  upstream: string
  ahead: number
  behind: number
  files: GitFile[]
  branches: string[]
  log: GitCommit[]
}

const chat = useChatStore()
const toast = useToast()

const overview = ref<GitOverview | null>(null)
const notRepo = ref(false)
const loading = ref(false)
const busy = ref(false)
const selected = ref<GitFile | null>(null)
const diff = ref('')
const commitMsg = ref('')
const stageAll = ref(true)

const sessionId = computed(() => chat.currentID ?? '')

/** 会话绑定的工作区路径：绑定/换绑后自动重载（面板状态源跟随工作区）。 */
const wsPath = computed(
  () => chat.sessions.find((s: Session) => s.id === chat.currentID)?.workspace_path ?? ''
)

/** 状态徽标颜色：M 蓝 / A 绿 / D 红 / U 黄 / ? 灰。 */
function statusClass(s: string): string {
  if (s === 'D') return 'is-del'
  if (s === 'A' || s === '?') return 'is-add'
  if (s === 'U') return 'is-unm'
  return 'is-mod'
}

async function load(): Promise<void> {
  if (!sessionId.value) return
  loading.value = true
  notRepo.value = false
  try {
    overview.value = await apiGet<GitOverview>(
      `/api/v1/git/overview?session_id=${encodeURIComponent(sessionId.value)}`
    )
  } catch {
    overview.value = null
    notRepo.value = true
  } finally {
    loading.value = false
  }
}

async function openDiff(f: GitFile): Promise<void> {
  if (selected.value?.path === f.path && selected.value?.staged === f.staged) {
    selected.value = null
    diff.value = ''
    return
  }
  selected.value = f
  try {
    const r = await apiGet<{ diff: string }>(
      `/api/v1/git/diff?session_id=${encodeURIComponent(sessionId.value)}&path=${encodeURIComponent(f.path)}&staged=${f.staged}`
    )
    diff.value = r.diff
  } catch (e) {
    diff.value = ''
    toast.error(t('git.loadDiffFailed'), String(e))
  }
}

async function stage(f: GitFile | null): Promise<void> {
  await run(() => apiPost('/api/v1/git/stage', { session_id: sessionId.value, path: f?.path ?? '' }))
}

async function unstage(f: GitFile): Promise<void> {
  await run(() => apiPost('/api/v1/git/unstage', { session_id: sessionId.value, path: f.path }))
}

async function discard(f: GitFile): Promise<void> {
  try {
    await ElMessageBox.confirm(t('git.discardConfirm', f.path), t('git.discardTitle'), { type: 'warning' })
  } catch {
    return
  }
  await run(() => apiPost('/api/v1/git/discard', { session_id: sessionId.value, path: f.path }))
}

async function switchBranch(branch: string): Promise<void> {
  if (!branch || branch === overview.value?.branch) return
  await run(() => apiPost('/api/v1/git/switch', { session_id: sessionId.value, branch, create: false }))
}

async function commit(): Promise<void> {
  if (!commitMsg.value.trim()) return
  await run(
    () =>
      apiPost('/api/v1/git/commit', {
        session_id: sessionId.value,
        message: commitMsg.value.trim(),
        stage_all: stageAll.value,
      }),
    () => {
      commitMsg.value = ''
      selected.value = null
      diff.value = ''
    }
  )
}

/** 统一执行器：转圈 + 失败提示 + 成功后刷新。 */
async function run(fn: () => Promise<unknown>, after?: () => void): Promise<void> {
  if (busy.value) return
  busy.value = true
  try {
    await fn()
    after?.()
  } catch (e) {
    toast.error(t('git.opFailed'), e instanceof Error ? e.message : String(e))
  } finally {
    busy.value = false
    await load()
  }
}

watch(() => chat.currentID, () => {
  selected.value = null
  diff.value = ''
  void load()
}, { immediate: true })

watch(wsPath, () => {
  selected.value = null
  diff.value = ''
  void load()
})
</script>

<template>
  <div class="flex h-full min-h-0 flex-col text-xs">
    <!-- 引导态：工作区不是 git 仓库 -->
    <div v-if="notRepo" class="flex flex-1 flex-col items-center justify-center gap-2 p-4 text-center">
      <GitBranch class="h-6 w-6 text-wb-muted" />
      <p class="text-wb-ink">{{ t('git.notRepo') }}</p>
      <p class="text-[11px] text-wb-muted">{{ t('git.notRepoHint') }}</p>
    </div>

    <template v-else>
      <!-- 分支行 -->
      <div class="flex shrink-0 items-center gap-1.5 border-b border-wb-border px-2.5 py-1.5">
        <GitBranch class="h-3.5 w-3.5 shrink-0 text-wb-primary" />
        <el-select
          v-if="overview"
          :model-value="overview.branch"
          size="small"
          filterable
          class="flex-1"
          @change="(v: string) => switchBranch(v)"
        >
          <el-option v-for="b in overview.branches" :key="b" :label="b" :value="b" />
        </el-select>
        <span v-if="overview && (overview.ahead || overview.behind)" class="git-ab">
          ↑{{ overview.ahead }} ↓{{ overview.behind }}
        </span>
        <button type="button" class="btn btn-sm" :disabled="loading" @click="load">
          <RefreshCw class="ic ic-sm" :class="{ 'animate-spin': loading }" />
        </button>
      </div>

      <div class="min-h-0 flex-1 overflow-y-auto">
        <!-- 变更文件 -->
        <div class="px-2.5 py-1.5">
          <div class="mb-1 flex items-center justify-between">
            <span class="font-medium text-wb-muted">{{ t('git.changes') }} ({{ overview?.files.length ?? 0 }})</span>
          </div>
          <div v-if="!overview?.files.length" class="py-2 text-[11px] text-wb-muted">{{ t('git.clean') }}</div>
          <div
            v-for="f in overview?.files ?? []"
            :key="f.path + f.staged"
            class="git-file"
            :class="{ 'is-active': selected?.path === f.path && selected?.staged === f.staged }"
            @click="openDiff(f)"
          >
            <span class="git-badge" :class="statusClass(f.status)">{{ f.status }}</span>
            <span class="min-w-0 flex-1 truncate" :title="f.path">{{ f.path }}</span>
            <span v-if="!f.staged" class="git-act" :title="t('git.stage')" @click.stop="stage(f)">+</span>
            <span v-if="f.staged" class="git-act" :title="t('git.unstage')" @click.stop="unstage(f)">−</span>
            <span class="git-act" :title="t('git.discard')" @click.stop="discard(f)">×</span>
          </div>
        </div>

        <!-- 选中文件的 diff -->
        <div v-if="selected" class="border-t border-wb-border px-2.5 py-1.5">
          <p class="mb-1 truncate text-[11px] font-medium text-wb-ink" :title="selected.path">
            {{ selected.path }}
          </p>
          <DiffView v-if="diff" :diff="diff" :max-height="260" />
          <p v-else class="py-2 text-[11px] text-wb-muted">{{ t('git.noDiff') }}</p>
        </div>

        <!-- 提交 -->
        <div class="border-t border-wb-border px-2.5 py-1.5">
          <textarea
            v-model="commitMsg"
            rows="2"
            class="w-full resize-none rounded-lg border border-wb-border bg-wb-surface-2 px-2 py-1.5 text-xs text-wb-ink outline-none focus:border-wb-primary/50"
            :placeholder="t('git.commitPlaceholder')"
          />
          <div class="mt-1 flex items-center justify-between gap-2">
            <label class="flex cursor-pointer items-center gap-1 text-[11px] text-wb-muted">
              <input v-model="stageAll" type="checkbox" class="accent-[var(--wb-primary)]" />
              {{ t('git.stageAll') }}
            </label>
            <button
              type="button"
              class="btn btn-primary btn-sm"
              :disabled="busy || !commitMsg.trim()"
              @click="commit"
            >
              {{ t('git.commit') }}
            </button>
          </div>
        </div>

        <!-- 最近提交 -->
        <div class="border-t border-wb-border px-2.5 py-1.5">
          <p class="mb-1 font-medium text-wb-muted">{{ t('git.log') }}</p>
          <div v-for="c in overview?.log ?? []" :key="c.hash" class="git-log-row">
            <span class="git-hash">{{ c.hash }}</span>
            <span class="min-w-0 flex-1 truncate" :title="`${c.author} · ${c.date}`">{{ c.subject }}</span>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.git-ab {
  border: 1px solid var(--wb-border);
  border-radius: 6px;
  padding: 1px 5px;
  font-size: 10px;
  color: var(--wb-muted);
  white-space: nowrap;
}
.git-file {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 3px 4px;
  border-radius: 6px;
  cursor: pointer;
  color: var(--wb-ink);
}
.git-file:hover {
  background: color-mix(in srgb, var(--wb-primary) 6%, transparent);
}
.git-file.is-active {
  background: color-mix(in srgb, var(--wb-primary) 12%, transparent);
}
.git-badge {
  width: 16px;
  text-align: center;
  border-radius: 4px;
  font-size: 10px;
  font-weight: 600;
  flex-shrink: 0;
}
.git-badge.is-mod { background: color-mix(in srgb, var(--wb-primary) 18%, transparent); color: var(--wb-primary-strong); }
.git-badge.is-add { background: color-mix(in srgb, var(--wb-mint) 25%, transparent); color: var(--wb-mint); }
.git-badge.is-del { background: color-mix(in srgb, var(--wb-danger) 18%, transparent); color: var(--wb-danger); }
.git-badge.is-unm { background: color-mix(in srgb, var(--wb-warning) 22%, transparent); color: var(--wb-warning); }
.git-act {
  color: var(--wb-muted);
  padding: 0 3px;
  border-radius: 4px;
  visibility: hidden;
  flex-shrink: 0;
}
.git-file:hover .git-act { visibility: visible; }
.git-act:hover { color: var(--wb-primary-strong); background: var(--wb-border); }
.git-log-row {
  display: flex;
  gap: 6px;
  padding: 2px 0;
  color: var(--wb-ink);
}
.git-hash {
  color: var(--wb-primary-strong);
  font-family: var(--wb-font-mono, monospace);
  flex-shrink: 0;
}
</style>
