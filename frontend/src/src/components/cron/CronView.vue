<script setup lang="ts">
/**
 * 定时任务管理视图（无 cron 友好版）。
 *
 * <p>D1 hero 使用场景说明
 * <p>D2 工作流下拉替代裸 input workflowID（el-select）
 * <p>D3 「立即测试」按钮：调 /cron/jobs/trigger/{id} 后立刻 toast 反馈
 * <p>D4 简单 / 高级双模式：默认简单模式（时间 + 频率 + 周几，全部图形化）；
 *     高级模式才露 5 段 cron 输入。简单模式内部仍产 5 段 cron 串后端无感。
 */
import { computed, onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { Clock, Lightbulb, Play, Pencil, Trash2 } from '@/components/common/icons'
import { useCronStore } from '@/stores/cron'
import { useToast } from '@/composables/useToast'
import { t } from '@/i18n'
import type { CronJob } from '@/types/api'

type Mode = 'simple' | 'advanced'
type Frequency = 'every_n_minutes' | 'every_hour' | 'every_day' | 'every_week' | 'every_month'

const cron = useCronStore()
const toast = useToast()
const { jobs, workflows, error, info, loading, editingID, triggeringID, form } = storeToRefs(cron)
const { edit, cancel, submit, remove, triggerNow } = cron
/** 表单动作类型（原型表头列「动作」）。store form 暂未暴露，独立 ref 替代。 */
const actionType = ref<'workflow' | 'chat'>('workflow')
void actionType.value

/** 审计：按 last_run_at 倒序，只显示已运行过的任务。 */
const recentRuns = computed(() =>
  jobs.value
    .filter((j) => j.last_run_at != null)
    .sort((a, b) => (b.last_run_at ?? 0) - (a.last_run_at ?? 0))
    .slice(0, 5)
)

function fmtTime(ts: number | null): string {
  if (!ts) return '—'
  return new Date(ts).toLocaleString('zh-CN', { hour12: false })
}

// ===== 简单模式 =====
const mode = ref<Mode>('simple')
const frequency = ref<Frequency>('every_day')
const minuteStep = ref(5) // 每 N 分钟
const hourOfDay = ref(9)
const minuteOfHour = ref(0)
const weekdays = ref<number[]>([1]) // 1=周一 … 7=周日；默认周一
const dayOfMonth = ref(1)

const WEEKDAY_LABELS = [
  { value: 1, label: '一' },
  { value: 2, label: '二' },
  { value: 3, label: '三' },
  { value: 4, label: '四' },
  { value: 5, label: '五' },
  { value: 6, label: '六' },
  { value: 7, label: '日' }
] as const
void WEEKDAY_LABELS

function toggleWeekday(v: number): void {
  const idx = weekdays.value.indexOf(v)
  if (idx >= 0) weekdays.value.splice(idx, 1)
  else weekdays.value.push(v)
  weekdays.value.sort((a, b) => a - b)
}
void toggleWeekday

/** 简单模式 → 5 段 cron 字符串。 */
const derivedCron = computed<string>(() => {
  const hour = String(hourOfDay.value).padStart(2, '0')
  const min = String(minuteOfHour.value).padStart(2, '0')
  switch (frequency.value) {
    case 'every_n_minutes':
      return `*/${Math.max(1, minuteStep.value)} * * * *`
    case 'every_hour':
      return `${min} * * * *`
    case 'every_day':
      return `${min} ${hour} * * *`
    case 'every_week': {
      const w = weekdays.value.length === 0 ? '*' : weekdays.value.join(',')
      return `${min} ${hour} * * ${w}`
    }
    case 'every_month':
      return `${min} ${hour} ${dayOfMonth.value} * *`
  }
})

/** 派生 cron 变化时回写到 form（仅当当前模式是 simple）。 */
watch(derivedCron, (v) => {
  if (mode.value === 'simple') form.value.schedule = v
})
// 任何简单模式选项变化后立即触发 derivedCron 重算（依赖项已被 computed 覆盖）。

// ===== 高级模式：5 段 cron 输入 =====
const FIELDS = [
  { key: 'minute', labelKey: 'cron.field.minute', placeholder: '0,15,30,45' },
  { key: 'hour', labelKey: 'cron.field.hour', placeholder: '9-18' },
  { key: 'day', labelKey: 'cron.field.day', placeholder: '*' },
  { key: 'month', labelKey: 'cron.field.month', placeholder: '*' },
  { key: 'week', labelKey: 'cron.field.week', placeholder: '1-5' }
] as const
void FIELDS

type ParsedSchedule = { minute: string; hour: string; day: string; month: string; week: string }

const parsedSchedule = computed<ParsedSchedule>(() => {
  const parts = (form.value.schedule ?? '').trim().split(/\s+/)
  const blank = { minute: '*', hour: '*', day: '*', month: '*', week: '*' }
  if (parts.length < 5) return blank
  return {
    minute: parts[0] ?? '*',
    hour: parts[1] ?? '*',
    day: parts[2] ?? '*',
    month: parts[3] ?? '*',
    week: parts[4] ?? '*'
  }
})
void parsedSchedule.value

function setField(key: keyof ParsedSchedule, value: string): void {
  const next = { ...parsedSchedule.value, [key]: value }
  form.value.schedule = `${next.minute} ${next.hour} ${next.day} ${next.month} ${next.week}`.trim()
}
void setField

function validateField(v: string): { ok: boolean; msg?: string } {
  if (!v) return { ok: false, msg: 'empty' }
  if (v === '*') return { ok: true }
  if (/^\*\/(\d+)$/.test(v)) return { ok: true }
  if (/^\d+$/.test(v)) return { ok: true }
  if (/^\d+-\d+$/.test(v)) return { ok: true }
  if (/^[\d,]+$/.test(v)) return { ok: true }
  if (/^[\d,-]+$/.test(v)) return { ok: true }
  return { ok: false, msg: 'invalid' }
}

const fieldValidations = computed(() => ({
  minute: validateField(parsedSchedule.value.minute),
  hour: validateField(parsedSchedule.value.hour),
  day: validateField(parsedSchedule.value.day),
  month: validateField(parsedSchedule.value.month),
  week: validateField(parsedSchedule.value.week)
}))

const scheduleValid = computed(() => Object.values(fieldValidations.value).every((v) => v.ok))
void scheduleValid

function applyPreset(value: string): void {
  form.value.schedule = value
  // 切到 advanced 让用户能看见编辑结果
  mode.value = 'advanced'
}
void applyPreset

/** 进入编辑态时按当前 schedule 反推简单模式（仅在能匹配内置形态时切换）。 */
function syncSimpleFromSchedule(schedule: string): void {
  const parts = schedule.trim().split(/\s+/)
  if (parts.length !== 5) return
  const [m, h, d, , wkn] = parts
  if (m?.startsWith('*/') && h === '*' && d === '*' && parts[3] === '*' && wkn === '*') {
    frequency.value = 'every_n_minutes'
    minuteStep.value = Math.max(1, parseInt(m.slice(2), 10) || 1)
    return
  }
  if (h === '*' && d === '*' && parts[3] === '*' && wkn === '*') {
    frequency.value = 'every_hour'
    minuteOfHour.value = parseInt(m ?? '0', 10) || 0
    return
  }
  if (d === '*' && parts[3] === '*' && wkn === '*') {
    frequency.value = 'every_day'
    hourOfDay.value = parseInt(h ?? '0', 10) || 0
    minuteOfHour.value = parseInt(m ?? '0', 10) || 0
    return
  }
  if (d === '*' && parts[3] === '*' && wkn !== '*') {
    frequency.value = 'every_week'
    hourOfDay.value = parseInt(h ?? '0', 10) || 0
    minuteOfHour.value = parseInt(m ?? '0', 10) || 0
    weekdays.value = wkn.split(',').map((x) => parseInt(x, 10)).filter((x) => x >= 1 && x <= 7)
    return
  }
  if (d !== '*' && parts[3] === '*' && wkn === '*') {
    frequency.value = 'every_month'
    hourOfDay.value = parseInt(h ?? '0', 10) || 0
    minuteOfHour.value = parseInt(m ?? '0', 10) || 0
    dayOfMonth.value = parseInt(d ?? '1', 10) || 1
    return
  }
}

/** 进入编辑态：包装 cron.edit，回推简单模式。 */
function onEdit(j: CronJob): void {
  edit(j)
  syncSimpleFromSchedule(j.schedule)
}
function onCancel(): void {
  cancel()
  mode.value = 'simple'
}

function workflowName(id: string | null): string {
  if (!id) return ''
  return workflows.value.find((w) => w.id === id)?.name ?? id
}
void workflowName

async function handleTriggerNow(id: string): Promise<void> {
  if (await triggerNow(id)) {
    toast.success(t('cron.triggered'))
  } else {
    toast.error(error.value ?? t('cron.triggerFailed'))
  }
}

onMounted(cron.load)

import { watch } from 'vue'
</script>

<template>
  <div class="scroll wb-ui">
    <div class="wrap wrap-md">
      <!-- Hero -->
      <header class="hero">
        <div class="tile"><Clock class="ic" /></div>
        <div>
          <h1>{{ t('cron.title') }}</h1>
          <p>{{ t('cron.subtitle') }}</p>
        </div>
      </header>

      <!-- 调度节奏提示卡 -->
      <div class="card p-sm" style="border-color: rgba(79, 70, 229, 0.3); background: rgba(79, 70, 229, 0.04)">
        <div class="flex-r" style="align-items: flex-start; gap: 9px">
          <Lightbulb class="ic" style="color: var(--wb-warning); margin-top: 2px" />
          <div class="fs12">
            <p style="font-weight: 600; color: var(--wb-primary-strong)">{{ t('cron.heroTitle') }}</p>
            <p class="muted mt4">{{ t('cron.heroBody') }}</p>
            <p class="muted mt4">{{ t('cron.example') }}</p>
          </div>
        </div>
      </div>

      <div v-if="error" class="alert a-danger">{{ error }}</div>
      <div v-if="info" class="alert a-success">{{ info }}</div>

      <!-- 新建表单 -->
      <section class="card">
        <div class="flex-r mb10">
          <h2>{{ editingID ? t('cron.edit') : t('cron.new') }}</h2>
          <span class="sp" />
          <div class="seg">
            <button :class="{ on: mode === 'simple' }" @click="mode = 'simple'">{{ t('cron.mode.simple') }}</button>
            <button :class="{ on: mode === 'advanced' }" @click="mode = 'advanced'">{{ t('cron.mode.advanced') }}</button>
          </div>
        </div>

        <!-- 公共字段：name / actionType / targetWorkflow -->
        <div class="grid2" style="gap: 12px">
          <div class="field">
            <label>{{ t('cron.name') }}</label>
            <input v-model="form.name" class="input" :placeholder="t('cron.namePlaceholder')" />
          </div>
          <div class="field">
            <label>{{ t('cron.actionType') }}</label>
            <select v-model="actionType" class="input">
              <option value="workflow">workflow</option>
              <option value="chat">chat</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('cron.targetWorkflow') }}</label>
            <select v-model="form.workflow_id" class="input">
              <option v-for="w in workflows" :key="w.id" :value="w.id">{{ w.name }}</option>
            </select>
          </div>
        </div>

        <!-- 简单模式：图形化拼 cron；高级模式：5 段 cron 表达式直接输入 -->
        <template v-if="mode === 'simple'">
          <div class="grid2 mt14" style="gap: 12px">
            <div class="field">
              <label>{{ t('cron.frequency') }}</label>
              <select v-model="frequency" class="input">
                <option value="every_day">{{ t('cron.freq.everyDay') }}</option>
                <option value="every_hour">{{ t('cron.freq.everyHour') }}</option>
                <option value="every_week">{{ t('cron.freq.everyWeek') }}</option>
                <option value="every_month">{{ t('cron.freq.everyMonth') }}</option>
                <option value="every_n_minutes">{{ t('cron.freq.everyN') }}</option>
              </select>
            </div>
            <div class="field" v-if="frequency === 'every_n_minutes'">
              <label>{{ t('cron.minuteStep') }}</label>
              <input v-model.number="minuteStep" class="input mono" min="1" max="59" />
            </div>
            <div class="field" v-if="frequency === 'every_day' || frequency === 'every_week' || frequency === 'every_month'">
              <label>{{ t('cron.hour') }}</label>
              <input v-model.number="hourOfDay" class="input mono" min="0" max="23" />
            </div>
            <div class="field" v-if="frequency !== 'every_n_minutes' && frequency !== 'every_hour'">
              <label>{{ t('cron.minute') }}</label>
              <input v-model.number="minuteOfHour" class="input mono" min="0" max="59" />
            </div>
            <div class="field" v-if="frequency === 'every_week'">
              <label>{{ t('cron.weekdays') }}</label>
              <div class="flex flex-wrap gap-1">
                <button
                  v-for="d in WEEKDAY_LABELS"
                  :key="d.value"
                  type="button"
                  class="mini"
                  :class="{ 'mini--active': weekdays.includes(d.value) }"
                  @click="toggleWeekday(d.value)"
                >
                  {{ d.label }}
                </button>
              </div>
            </div>
            <div class="field" v-if="frequency === 'every_month'">
              <label>{{ t('cron.dayOfMonth') }}</label>
              <input v-model.number="dayOfMonth" class="input mono" min="1" max="31" />
            </div>
          </div>
        </template>
        <template v-else>
          <div class="field mt14">
            <label>{{ t('cron.expr') }}</label>
            <input
              v-model="form.schedule"
              class="input mono"
              placeholder="0 9 * * 1"
              style="font-family: var(--font-mono); letter-spacing: 0.5px"
            />
            <p class="fs11 muted mt4">{{ t('cron.exprTip') }}</p>
          </div>
        </template>

        <div class="flex-r mt14">
          <div class="flex-r" style="gap: 7px">
            <span class="switch on" @click="form.enabled = !form.enabled" />
            <span class="fs12">{{ t('common.enabled') }}</span>
          </div>
          <span class="sp" />
          <span class="fs11 muted mono">{{ t('cron.preview') }}: {{ derivedCron }}</span>
          <button class="btn" @click="onCancel">{{ t('ui.btn.cancel') }}</button>
          <button class="btn btn-primary" @click="submit">{{ t('cron.saveBtn') }}</button>
        </div>
      </section>

      <!-- 已配置任务 -->
      <section class="card p-sm">
        <h2 class="mb10">{{ t('cron.list') }} · {{ jobs.length }}</h2>
        <div class="tbl-wrap">
          <table class="tbl">
            <thead>
              <tr>
                <th>{{ t('cron.name') }}</th>
                <th>{{ t('cron.expr') }}</th>
                <th>{{ t('cron.action') }}</th>
                <th>{{ t('cron.lastRun') }}</th>
                <th>{{ t('cron.nextRun') }}</th>
                <th>{{ t('common.status') }}</th>
                <th />
              </tr>
            </thead>
            <tbody>
              <tr v-if="loading"><td colspan="7" class="empty">{{ t('ui.status.loading') }}</td></tr>
              <tr v-else-if="jobs.length === 0"><td colspan="7" class="empty">{{ t('cron.empty') }}</td></tr>
              <tr v-for="j in jobs" v-else :key="j.id">
                <td style="font-weight: 500">{{ j.name }}</td>
                <td class="mono">{{ j.schedule }}</td>
                <td>
                  <span class="badge" :class="(j as Record<string, unknown>).action === 'workflow' ? 'b-primary' : 'b-info'">{{ (j as Record<string, unknown>).action ?? 'workflow' }}</span>
                </td>
                <td class="mono muted">{{ fmtTime(j.last_run_at) }}</td>
                <td class="mono muted">{{ fmtTime(j.next_run_at) }}</td>
                <td>
                  <span class="badge" :class="j.enabled ? 'b-success' : 'b-neutral'">
                    <span class="dot" />{{ j.enabled ? t('cron.enabled') : t('cron.disabled') }}
                  </span>
                </td>
                <td>
                  <div class="tbl-actions">
                    <button class="btn-icon" :title="t('cron.triggerNow')" :disabled="triggeringID === j.id" @click="handleTriggerNow(j.id)"><Play class="ic ic-sm" /></button>
                    <button class="btn-icon" :title="t('ui.btn.edit')" @click="onEdit(j)"><Pencil class="ic ic-sm" /></button>
                    <button class="btn-icon" style="color: var(--wb-danger)" :title="t('ui.btn.delete')" @click="remove(j.id)"><Trash2 class="ic ic-sm" /></button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- 执行审计（真实数据：按 last_run_at 倒序） -->
      <section class="card p-sm">
        <h2 class="mb10">{{ t('cron.audit') }}</h2>
        <div v-if="recentRuns.length === 0" class="empty fs11" style="padding: 12px 0">{{ t('cron.auditEmpty') }}</div>
        <div v-else class="rowlist">
          <div v-for="j in recentRuns" :key="j.id" class="rli">
            <span class="led" :class="j.last_status === 'success' || j.last_status === 'completed' ? 'g' : j.last_status === 'failed' || j.last_status === 'error' ? 'r' : j.last_status ? 'w' : 'n'" />
            <div class="grow">
              <h5 class="mono">{{ j.name }}</h5>
              <p>{{ fmtTime(j.last_run_at) }}</p>
            </div>
            <span class="badge" :class="j.last_status === 'success' || j.last_status === 'completed' ? 'b-success' : j.last_status === 'failed' || j.last_status === 'error' ? 'b-danger' : 'b-neutral'">
              <span class="dot" />{{ j.last_status ?? '—' }}
            </span>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>
