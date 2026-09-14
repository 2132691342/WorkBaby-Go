<script setup lang="ts">
/**
 * Git 面板（右侧栏）：变更 / 历史 / 图谱 三视图。
 *
 * <p>状态源是会话工作区里的 .git（git CLI，不落库）；工作区未绑定时走默认根。
 * 变更视图按「暂存区 / 工作区」两段拆行，与 git 的 add 语义一致；
 * 历史视图点开某次提交看元信息 + 文件清单 + 单文件 diff；图谱是 git log --graph 的只读原文。
 * diff 渲染复用主链路 DiffView（同一份 parseDiffRows）。
 */
import { computed, ref, watch } from 'vue'
import { GitBranch, RefreshCw, Plus, Trash2 } from '@/components/common/icons'
import { ElMessageBox } from 'element-plus'
import { apiGet, apiPost } from '@/api/client'
import { useChatStore } from '@/stores/chat'
import type { Session } from '@/types/api'
import { t } from '@/i18n'
import { useToast } from '@/composables/useToast'
import DiffView from '@/components/chat/DiffView.vue'

interface GitFile { path: string; orig_path?: string; status: string; staged: boolean }
interface GitCommit { hash: string; author: string; date: string; subject: string; refs?: string }
interface GitCommitFile { path: string; orig_path?: string; status: string; added: number; removed: number; binary?: boolean }
interface GitCommitDetail {
  hash: string
  short_hash: string
  author: string
  author_email?: string
  date: string
  subject: string
  body?: string
  parents?: string[]
  refs?: string
  files: GitCommitFile[]
  total_added: number
  total_removed: number
}
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

/** 视图：变更 / 历史 / 图谱。 */
const tab = ref<'changes' | 'log' | 'graph'>('changes')

// ---- 变更视图 ----
const selected = ref<GitFile | null>(null)
const diff = ref('')
const commitMsg = ref('')
const stageAll = ref(true)

// ---- 历史视图 ----
const commits = ref<GitCommit[]>([])
const logAll = ref(false)
const logLimit = ref(20)
const loadingLog = ref(false)
const detail = ref<GitCommitDetail | null>(null)
const detailFile = ref<GitCommitFile | null>(null)
const detailDiff = ref('')

// ---- 图谱视图 ----
const graph = ref('')
const loadingGraph = ref(false)

const sessionId = computed(() => chat.currentID ?? '')

/** 会话绑定的工作区路径：绑定/换绑后自动重载（面板状态源跟随工作区）。 */
const wsPath = computed(
  () => chat.sessions.find((s: Session) => s.id === chat.currentID)?.workspace_path ?? ''
)

const stagedFiles = computed(() => (overview.value?.files ?? []).filter((f) => f.staged))
const unstagedFiles = computed(() => (overview.value?.files ?? []).filter((f) => !f.staged))
/** 可删除分支：排除当前分支（后端也会拒，前端不给出无效入口）。 */
const deletableBranches = computed(() => (overview.value?.branches ?? []).filter((b) => b !== overview.value?.branch))

/** 状态徽标颜色：M 蓝 / A 绿 / D 红 / U 黄 / ? 灰。 */
function statusClass(s: string): string {
  if (s === 'D') return 'is-del'
  if (s === 'A' || s === '?') return 'is-add'
  if (s === 'U') return 'is-unm'
  if (s === 'R' || s === 'C') return 'is-ren'
  return 'is-mod'
}

function q(): string {
  return `session_id=${encodeURIComponent(sessionId.value)}`
}

async function load(): Promise<void> {
  if (!sessionId.value) return
  loading.value = true
  notRepo.value = false
  try {
    overview.value = await apiGet<GitOverview>(`/api/v1/git/overview?${q()}`)
  } catch {
    overview.value = null
    notRepo.value = true
  } finally {
    loading.value = false
  }
}

async function loadLog(reset = true): Promise<void> {
  if (!sessionId.value) return
  if (reset) logLimit.value = 20
  loadingLog.value = true
  try {
    commits.value = await apiGet<GitCommit[]>(
      `/api/v1/git/log?${q()}&limit=${logLimit.value}&all=${logAll.value}`
    )
  } catch (e) {
    toast.error(t('git.opFailed'), e instanceof Error ? e.message : String(e))
  } finally {
    loadingLog.value = false
  }
}

async function loadGraph(): Promise<void> {
  if (!sessionId.value) return
  loadingGraph.value = true
  try {
    const r = await apiGet<{ graph: string }>(`/api/v1/git/graph?${q()}&limit=120`)
    graph.value = r.graph
  } catch (e) {
    toast.error(t('git.opFailed'), e instanceof Error ? e.message : String(e))
  } finally {
    loadingGraph.value = false
  }
}

/** 切视图时才拉对应数据：面板打开即拉三份是浪费（图谱在大仓库上不便宜）。 */
watch(tab, (v) => {
  if (v === 'log') void loadLog()
  if (v === 'graph' && !graph.value) void loadGraph()
})

async function openDiff(f: GitFile): Promise<void> {
  if (selected.value?.path === f.path && selected.value?.staged === f.staged) {
    selected.value = null
    diff.value = ''
    return
  }
  selected.value = f
  try {
    const r = await apiGet<{ diff: string }>(
      `/api/v1/git/diff?${q()}&path=${encodeURIComponent(f.path)}&staged=${f.staged}`
    )
    diff.value = r.diff
  } catch (e) {
    diff.value = ''
    toast.error(t('git.loadDiffFailed'), String(e))
  }
}

async function openCommit(c: GitCommit): Promise<void> {
  detailFile.value = null
  detailDiff.value = ''
  try {
    detail.value = await apiGet<GitCommitDetail>(`/api/v1/git/commit?${q()}&hash=${c.hash}`)
  } catch (e) {
    toast.error(t('git.opFailed'), e instanceof Error ? e.message : String(e))
  }
}

async function openCommitFile(f: GitCommitFile): Promise<void> {
  if (!detail.value) return
  if (detailFile.value?.path === f.path) {
    detailFile.value = null
    detailDiff.value = ''
    return
  }
  detailFile.value = f
  try {
    const r = await apiGet<{ diff: string }>(
      `/api/v1/git/commit/diff?${q()}&hash=${detail.value.hash}&path=${encodeURIComponent(f.path)}`
    )
    detailDiff.value = r.diff
  } catch (e) {
    detailDiff.value = ''
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

async function createBranch(): Promise<void> {
  let name = ''
  try {
    const r = await ElMessageBox.prompt(t('git.newBranchPrompt'), t('git.newBranch'), {
      inputPlaceholder: 'feature/xxx',
    })
    name = r.value.trim()
  } catch {
    return
  }
  if (!name) return
  await run(() => apiPost('/api/v1/git/switch', { session_id: sessionId.value, branch: name, create: true }))
}

async function deleteBranch(branch: string): Promise<void> {
  try {
    await ElMessageBox.confirm(t('git.deleteBranchConfirm', branch), t('git.deleteBranch'), { type: 'warning' })
  } catch {
    return
  }
  await run(() => apiPost('/api/v1/git/branch/delete', { session_id: sessionId.value, branch }))
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

function resetViews(): void {
  selected.value = null
  diff.value = ''
  detail.value = null
  detailFile.value = null
  detailDiff.value = ''
  graph.value = ''
}

watch(() => chat.currentID, () => {
  resetViews()
  void load()
}, { immediate: true })

watch(wsPath, () => {
  resetViews()
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
      <!-- 分支行：切换 / 新建 / 删除 / ahead-behind / 刷新 -->
      <div class="flex shrink-0 items-center gap-1.5 border-b border-wb-border px-2.5 py-2">
        <el-select
          v-if="overview"
          :model-value="overview.branch"
          size="small"
          filterable
          class="min-w-0 flex-1"
          @change="(v: string) => switchBranch(v)"
        >
          <el-option v-for="b in overview.branches" :key="b" :label="b" :value="b" />
        </el-select>
        <span v-if="overview && (overview.ahead || overview.behind)" class="git-ab">
          ↑{{ overview.ahead }} ↓{{ overview.behind }}
        </span>
        <button type="button" class="btn-icon is-sm" :title="t('git.newBranch')" @click="createBranch">
          <Plus class="ic-xs" />
        </button>
        <el-dropdown v-if="deletableBranches.length" trigger="click">
          <button type="button" class="btn-icon is-sm" :title="t('git.deleteBranch')">
            <Trash2 class="ic-xs" />
          </button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item v-for="b in deletableBranches" :key="b" @click="deleteBranch(b)">
                {{ b }}
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <button type="button" class="btn-icon is-sm" :disabled="loading" :title="t('git.title')" @click="load">
          <RefreshCw class="ic-xs" :class="{ 'animate-spin': loading }" />
        </button>
      </div>

      <!-- 视图切换 -->
      <div class="flex shrink-0 items-center gap-1 px-2.5 py-2">
        <div class="seg sm flex-1">
          <button :class="{ on: tab === 'changes' }" @click="tab = 'changes'">
            {{ t('git.tabChanges') }}
            <span v-if="overview?.files.length" class="git-cnt">{{ overview.files.length }}</span>
          </button>
          <button :class="{ on: tab === 'log' }" @click="tab = 'log'">{{ t('git.tabLog') }}</button>
          <button :class="{ on: tab === 'graph' }" @click="tab = 'graph'">{{ t('git.tabGraph') }}</button>
        </div>
      </div>

      <div class="min-h-0 flex-1 overflow-y-auto">
        <!-- ===== 变更 ===== -->
        <template v-if="tab === 'changes'">
          <div v-if="!overview?.files.length" class="px-2.5 py-3 text-[11px] text-wb-muted">
            {{ t('git.clean') }}
          </div>

          <!-- 暂存区 -->
          <div v-if="stagedFiles.length" class="px-2.5 pb-1.5">
            <div class="mb-1 flex items-center justify-between">
              <span class="git-sect">{{ t('git.staged') }} ({{ stagedFiles.length }})</span>
              <span class="git-sect-btn" @click="stage(null)">{{ t('git.stageAll') }}</span>
            </div>
            <div
              v-for="f in stagedFiles"
              :key="'s' + f.path"
              class="git-file"
              :class="{ 'is-active': selected?.path === f.path && selected?.staged }"
              @click="openDiff(f)"
            >
              <span class="git-badge" :class="statusClass(f.status)">{{ f.status }}</span>
              <span class="min-w-0 flex-1 truncate" :title="f.path">
                <span v-if="f.orig_path" class="git-orig">{{ f.orig_path }} → </span>{{ f.path }}
              </span>
              <span class="git-act" :title="t('git.unstage')" @click.stop="unstage(f)">−</span>
            </div>
          </div>

          <!-- 工作区 -->
          <div class="px-2.5 pb-1.5">
            <div v-if="unstagedFiles.length" class="mb-1 flex items-center justify-between">
              <span class="git-sect">{{ t('git.unstaged') }} ({{ unstagedFiles.length }})</span>
            </div>
            <div
              v-for="f in unstagedFiles"
              :key="'u' + f.path"
              class="git-file"
              :class="{ 'is-active': selected?.path === f.path && !selected?.staged }"
              @click="openDiff(f)"
            >
              <span class="git-badge" :class="statusClass(f.status)">{{ f.status }}</span>
              <span class="min-w-0 flex-1 truncate" :title="f.path">
                <span v-if="f.orig_path" class="git-orig">{{ f.orig_path }} → </span>{{ f.path }}
              </span>
              <span class="git-act" :title="t('git.stage')" @click.stop="stage(f)">+</span>
              <span class="git-act" :title="t('git.discard')" @click.stop="discard(f)">×</span>
            </div>
          </div>

          <!-- 选中文件的 diff -->
          <div v-if="selected" class="border-t border-wb-border px-2.5 py-2">
            <p class="mb-1 truncate text-[11px] font-medium text-wb-ink" :title="selected.path">
              {{ selected.path }}
            </p>
            <DiffView v-if="diff" :diff="diff" :max-height="260" />
            <p v-else class="py-2 text-[11px] text-wb-muted">{{ t('git.noDiff') }}</p>
          </div>

          <!-- 提交 -->
          <div class="border-t border-wb-border px-2.5 py-2">
            <textarea
              v-model="commitMsg"
              rows="2"
              class="w-full resize-none rounded-xl border border-wb-border bg-wb-surface-solid px-2.5 py-2 text-xs text-wb-ink outline-none transition-colors focus:border-wb-primary/50"
              :placeholder="t('git.commitPlaceholder')"
              @keydown.ctrl.enter.prevent="commit"
            />
            <div class="mt-1.5 flex items-center justify-between gap-2">
              <label class="flex cursor-pointer items-center gap-1 text-[11px] text-wb-muted">
                <input v-model="stageAll" type="checkbox" class="accent-[var(--wb-primary)]" />
                {{ t('git.stageAll') }}
              </label>
              <button
                type="button"
                class="btn btn-primary btn-sm btn-pill"
                :disabled="busy || !commitMsg.trim()"
                @click="commit"
              >
                {{ t('git.commit') }}
              </button>
            </div>
          </div>
        </template>

        <!-- ===== 历史 ===== -->
        <template v-else-if="tab === 'log'">
          <div class="flex items-center gap-2 px-2.5 pb-1.5">
            <label class="flex cursor-pointer items-center gap-1 text-[11px] text-wb-muted">
              <input v-model="logAll" type="checkbox" class="accent-[var(--wb-primary)]" @change="loadLog()" />
              {{ t('git.allBranches') }}
            </label>
            <span class="sp flex-1" />
            <span v-if="loadingLog" class="text-[11px] text-wb-muted">{{ t('ui.status.loading') }}</span>
          </div>
          <div
            v-for="c in commits"
            :key="c.hash"
            class="git-commit"
            :class="{ 'is-active': detail?.hash === c.hash }"
            @click="openCommit(c)"
          >
            <div class="flex items-center gap-1.5">
              <span class="git-hash">{{ c.hash }}</span>
              <span v-if="c.refs" class="git-ref">{{ c.refs }}</span>
            </div>
            <span class="min-w-0 flex-1 truncate text-[11.5px] text-wb-ink" :title="c.subject">{{ c.subject }}</span>
            <span class="git-meta">{{ c.author }} · {{ c.date }}</span>
          </div>
          <div v-if="!loadingLog && !commits.length" class="px-2.5 py-3 text-[11px] text-wb-muted">
            {{ t('git.clean') }}
          </div>
          <button v-if="commits.length >= logLimit" type="button" class="git-more" @click="logLimit += 20; loadLog(false)">
            {{ t('git.loadMore') }}
          </button>

          <!-- 提交详情 -->
          <div v-if="detail" class="border-t border-wb-border px-2.5 py-2">
            <p class="text-[11.5px] font-semibold text-wb-ink">{{ detail.subject }}</p>
            <p class="git-meta mt-0.5">{{ detail.short_hash }} · {{ detail.author }} · {{ detail.date }}</p>
            <p v-if="detail.refs" class="git-ref mt-1">{{ detail.refs }}</p>
            <p v-if="detail.body" class="git-body">{{ detail.body }}</p>
            <p class="git-sect mt-2">
              {{ t('git.commitFiles') }} ({{ detail.files.length }})
              <span class="add">+{{ detail.total_added }}</span>
              <span class="del">-{{ detail.total_removed }}</span>
            </p>
            <div
              v-for="f in detail.files"
              :key="f.path"
              class="git-file"
              :class="{ 'is-active': detailFile?.path === f.path }"
              @click="openCommitFile(f)"
            >
              <span class="git-badge" :class="statusClass(f.status)">{{ f.status }}</span>
              <span class="min-w-0 flex-1 truncate" :title="f.path">
                <span v-if="f.orig_path" class="git-orig">{{ f.orig_path }} → </span>{{ f.path }}
              </span>
              <span v-if="f.binary" class="git-meta">{{ t('git.binary') }}</span>
              <template v-else>
                <span class="add">+{{ f.added }}</span>
                <span class="del">-{{ f.removed }}</span>
              </template>
            </div>
            <div v-if="detailFile" class="mt-2">
              <DiffView v-if="detailDiff" :diff="detailDiff" :max-height="260" />
              <p v-else class="py-2 text-[11px] text-wb-muted">{{ t('git.noDiff') }}</p>
            </div>
          </div>
        </template>

        <!-- ===== 图谱 ===== -->
        <template v-else>
          <div class="flex items-center gap-2 px-2.5 pb-1.5">
            <span class="text-[11px] text-wb-muted">{{ t('git.graphHint') }}</span>
            <span class="sp flex-1" />
            <button type="button" class="btn-icon is-sm" :disabled="loadingGraph" @click="loadGraph">
              <RefreshCw class="ic-xs" :class="{ 'animate-spin': loadingGraph }" />
            </button>
          </div>
          <pre v-if="graph" class="git-graph">{{ graph }}</pre>
          <p v-else-if="!loadingGraph" class="px-2.5 py-3 text-[11px] text-wb-muted">{{ t('git.clean') }}</p>
        </template>
      </div>
    </template>
  </div>
</template>

<style scoped>
.git-ab {
  border: 1px solid var(--wb-border);
  border-radius: 999px;
  padding: 1px 6px;
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--wb-muted);
  white-space: nowrap;
  flex: none;
}
.git-cnt {
  margin-left: 3px;
  font-size: 9.5px;
  color: var(--wb-muted);
}
.git-sect {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--wb-muted);
}
.git-sect-btn {
  font-size: 10.5px;
  color: var(--wb-primary-strong);
  cursor: pointer;
}
.git-file {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 5px;
  border-radius: 8px;
  cursor: pointer;
  color: var(--wb-ink);
  transition: background 0.13s ease;
}
.git-file:hover {
  background: color-mix(in srgb, var(--wb-primary) 7%, transparent);
}
.git-file.is-active {
  background: color-mix(in srgb, var(--wb-primary) 12%, transparent);
}
.git-orig {
  color: var(--wb-muted);
  font-size: 10.5px;
}
.git-badge {
  width: 16px;
  text-align: center;
  border-radius: 4px;
  font-size: 10px;
  font-weight: 700;
  flex-shrink: 0;
}
.git-badge.is-mod { background: color-mix(in srgb, var(--wb-primary) 18%, transparent); color: var(--wb-primary-strong); }
.git-badge.is-add { background: color-mix(in srgb, var(--wb-mint) 22%, transparent); color: var(--wb-mint); }
.git-badge.is-del { background: color-mix(in srgb, var(--wb-danger) 18%, transparent); color: var(--wb-danger); }
.git-badge.is-unm { background: color-mix(in srgb, var(--wb-warning) 22%, transparent); color: var(--wb-warning); }
.git-badge.is-ren { background: color-mix(in srgb, var(--wb-lavender) 20%, transparent); color: var(--wb-lavender); }
.git-act {
  color: var(--wb-muted);
  padding: 0 4px;
  border-radius: 4px;
  visibility: hidden;
  flex-shrink: 0;
  font-size: 12px;
  line-height: 1;
}
.git-file:hover .git-act { visibility: visible; }
.git-act:hover { color: var(--wb-primary-strong); background: var(--wb-border); }
.git-commit {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 5px 8px;
  margin: 0 10px 1px;
  border-radius: 8px;
  cursor: pointer;
  transition: background 0.13s ease;
}
.git-commit:hover {
  background: color-mix(in srgb, var(--wb-primary) 7%, transparent);
}
.git-commit.is-active {
  background: color-mix(in srgb, var(--wb-primary) 12%, transparent);
}
.git-hash {
  color: var(--wb-primary-strong);
  font-family: var(--font-mono);
  font-size: 10.5px;
  flex-shrink: 0;
}
.git-ref {
  font-size: 9.5px;
  color: var(--wb-lavender);
  background: color-mix(in srgb, var(--wb-lavender) 14%, transparent);
  border-radius: 999px;
  padding: 0 6px;
  flex-shrink: 0;
  max-width: 12rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.git-meta {
  font-size: 10px;
  color: var(--wb-muted);
  flex-shrink: 0;
  white-space: nowrap;
}
.git-body {
  margin: 6px 0 0;
  font-size: 11px;
  line-height: 1.6;
  color: var(--wb-muted);
  white-space: pre-wrap;
  max-height: 8rem;
  overflow: auto;
}
.git-more {
  display: block;
  width: calc(100% - 20px);
  margin: 4px 10px 2px;
  padding: 5px 0;
  border-radius: 8px;
  border: 1px dashed var(--wb-border-strong);
  background: transparent;
  font-size: 11px;
  color: var(--wb-muted);
  cursor: pointer;
}
.git-more:hover {
  color: var(--wb-primary-strong);
  border-color: var(--wb-primary);
}
.git-graph {
  margin: 0;
  padding: 0 12px 12px;
  font-family: var(--font-mono);
  font-size: 10.5px;
  line-height: 1.55;
  color: var(--wb-ink);
  white-space: pre;
  overflow-x: auto;
}
.add { color: var(--wb-mint); font-family: var(--font-mono); font-size: 10px; }
.del { color: var(--wb-danger); font-family: var(--font-mono); font-size: 10px; }
</style>
