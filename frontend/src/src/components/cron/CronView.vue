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
import { Clock, Lightbulb } from '@/components/common/icons'
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

function toggleWeekday(v: number): void {
  const idx = weekdays.value.indexOf(v)
  if (idx >= 0) weekdays.value.splice(idx, 1)
  else weekdays.value.push(v)
  weekdays.value.sort((a, b) => a - b)
}

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

function setField(key: keyof ParsedSchedule, value: string): void {
  const next = { ...parsedSchedule.value, [key]: value }
  form.value.schedule = `${next.minute} ${next.hour} ${next.day} ${next.month} ${next.week}`.trim()
}

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

function applyPreset(value: string): void {
  form.value.schedule = value
  // 切到 advanced 让用户能看见编辑结果
  mode.value = 'advanced'
}

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
  <div class="flex h-full flex-col overflow-y-auto text-wb-ink">
    <div class="mx-auto w-full max-w-4xl space-y-5 px-6 py-8">
      <header class="flex items-center gap-3">
        <div class="flex h-10 w-10 items-center justify-center rounded-lg bg-wb-primary/10 text-wb-primary">
          <Clock class="h-5 w-5" />
        </div>
        <div>
          <h1 class="font-display text-lg font-semibold text-wb-ink">{{ t('cron.title') }}</h1>
          <p class="text-xs text-wb-muted">{{ t('cron.subtitle') }}</p>
        </div>
      </header>

      <section class="card flex items-start gap-2 border-wb-primary/30 bg-wb-primary/5 p-4">
        <Lightbulb class="h-4 w-4 shrink-0 text-wb-warning" />
        <div class="flex-1 text-xs text-wb-ink">
          <p class="font-medium text-wb-primary-strong">{{ t('cron.heroTitle') }}</p>
          <p class="mt-1 text-wb-muted">{{ t('cron.heroBody') }}</p>
          <p class="mt-1 text-wb-muted">{{ t('cron.example') }}</p>
        </div>
      </section>

      <div v-if="error" class="rounded-lg bg-wb-danger/15 px-3 py-2 text-sm text-wb-danger">{{ error }}</div>
      <div v-if="info" class="rounded-lg bg-wb-success/15 px-3 py-2 text-sm text-wb-success">{{ info }}</div>

      <section class="card p-5">
        <div class="mb-4 flex items-center justify-between">
          <h2 class="font-display text-sm font-semibold text-wb-ink">
            {{ editingID ? t('cron.edit') : t('cron.new') }}
          </h2>
          <el-radio-group v-model="mode" size="small">
            <el-radio-button label="simple">{{ t('cron.mode.simple') }}</el-radio-button>
            <el-radio-button label="advanced">{{ t('cron.mode.advanced') }}</el-radio-button>
          </el-radio-group>
        </div>

        <el-form class="wb-el-form" label-position="top" @submit.prevent="submit">
          <el-form-item :label="t('cron.name')">
            <el-input v-model="form.name" :placeholder="t('cron.name')" />
          </el-form-item>

          <!-- 简单模式：可视化定时配置 -->
          <template v-if="mode === 'simple'">
            <el-form-item :label="t('cron.preset.label')">
              <div class="grid w-full grid-cols-1 gap-4 md:grid-cols-2">
                <div class="flex flex-col gap-1">
                  <span class="text-xs text-wb-muted">频率</span>
                  <el-select v-model="frequency" class="w-full">
                    <el-option value="every_n_minutes" label="每 N 分钟" />
                    <el-option value="every_hour" label="每小时" />
                    <el-option value="every_day" label="每天" />
                    <el-option value="every_week" label="每周（指定周几）" />
                    <el-option value="every_month" label="每月（指定日）" />
                  </el-select>
                </div>

                <div v-if="frequency === 'every_n_minutes'" class="flex flex-col gap-1">
                  <span class="text-xs text-wb-muted">步长（分钟）</span>
                  <el-input-number v-model="minuteStep" :min="1" :max="59" class="!w-full" />
                </div>

                <div v-if="frequency !== 'every_n_minutes'" class="flex flex-col gap-1">
                  <span class="text-xs text-wb-muted">小时</span>
                  <el-input-number v-model="hourOfDay" :min="0" :max="23" class="!w-full" />
                </div>

                <div v-if="frequency !== 'every_n_minutes'" class="flex flex-col gap-1">
                  <span class="text-xs text-wb-muted">分钟</span>
                  <el-input-number v-model="minuteOfHour" :min="0" :max="59" class="!w-full" />
                </div>

                <div v-if="frequency === 'every_week'" class="col-span-full flex flex-col gap-1">
                  <span class="text-xs text-wb-muted">周几（可多选）</span>
                  <div class="flex gap-2">
                    <el-button
                      v-for="w in WEEKDAY_LABELS"
                      :key="w.value"
                      size="small"
                      :type="weekdays.includes(w.value) ? 'primary' : 'default'"
                      @click="toggleWeekday(w.value)"
                    >
                      {{ w.label }}
                    </el-button>
                  </div>
                </div>

                <div v-if="frequency === 'every_month'" class="flex flex-col gap-1">
                  <span class="text-xs text-wb-muted">日（1-31）</span>
                  <el-input-number v-model="dayOfMonth" :min="1" :max="31" class="!w-full" />
                </div>
              </div>
              <p class="mt-2 font-mono text-xs text-wb-muted">
                Cron: <span class="text-wb-ink">{{ derivedCron }}</span>
              </p>
            </el-form-item>
          </template>

          <!-- 高级模式：5 段 cron 输入 -->
          <template v-else>
            <el-form-item :label="t('cron.preset.label')">
              <div class="flex flex-wrap gap-2">
                <el-button v-for="p in [
                  { label: t('cron.preset.everyMinute'), value: '* * * * *' },
                  { label: t('cron.preset.every5Min'), value: '*/5 * * * *' },
                  { label: t('cron.preset.everyHour'), value: '0 * * * *' },
                  { label: t('cron.preset.daily9'), value: '0 9 * * *' },
                  { label: t('cron.preset.weeklyMon9'), value: '0 9 * * 1' },
                  { label: t('cron.preset.monthly1'), value: '0 0 1 * *' }
                ]" :key="p.value" size="small" @click="applyPreset(p.value)">
                  {{ p.label }}
                </el-button>
              </div>
            </el-form-item>

            <el-form-item :label="t('cron.fields.label')">
              <div class="grid w-full grid-cols-5 gap-2">
                <div v-for="f in FIELDS" :key="f.key" class="flex flex-col gap-1">
                  <span class="text-xs text-wb-muted">{{ t(f.labelKey) }}</span>
                  <el-input
                    :model-value="parsedSchedule[f.key]"
                    :placeholder="f.placeholder"
                    class="font-mono"
                    :class="{ 'is-invalid': !fieldValidations[f.key].ok }"
                    @update:model-value="(v: string) => setField(f.key, v)"
                  />
                  <span v-if="!fieldValidations[f.key].ok" class="text-[10px] text-wb-danger">
                    {{ t('cron.fieldInvalid') }} ({{ fieldValidations[f.key].msg }})
                  </span>
                </div>
              </div>
              <p v-if="!scheduleValid" class="mt-2 text-[10px] text-wb-danger">
                {{ t('cron.scheduleInvalidHint') }}
              </p>
            </el-form-item>
          </template>

          <el-form-item :label="t('cron.workflowLabel')">
            <el-select v-model="form.workflow_id" clearable :placeholder="t('cron.noWorkflow')" class="!w-72">
              <el-option v-for="w in workflows" :key="w.id" :value="w.id" :label="w.name" />
            </el-select>
            <p class="mt-1 text-[10px] text-wb-muted">{{ t('cron.workflowHint') }}</p>
          </el-form-item>

          <div class="flex items-center gap-3">
            <el-switch v-model="form.enabled" />
            <span class="text-sm text-wb-ink">{{ t('common.enabled') }}</span>
            <div class="flex-1" />
            <el-button v-if="editingID" @click="onCancel">{{ t('ui.btn.cancel') }}</el-button>
            <el-button
              type="primary"
              native-type="submit"
              :disabled="mode === 'advanced' && !scheduleValid"
            >
              {{ editingID ? t('ui.btn.save') : t('ui.btn.create') }}
            </el-button>
          </div>
        </el-form>
      </section>

      <section class="card p-5">
        <h2 class="mb-3 font-display text-sm font-semibold text-wb-ink">{{ t('cron.list') }}</h2>
        <div v-if="loading" class="text-sm text-wb-muted">{{ t('ui.status.loading') }}</div>
        <el-table v-else-if="jobs.length > 0" :data="jobs" stripe class="wb-el-table">
          <el-table-column :label="t('cron.name')" min-width="180">
            <template #default="{ row }">
              <span class="font-medium text-wb-ink">{{ (row as CronJob).name }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('cron.schedule')" min-width="160">
            <template #default="{ row }">
              <span class="font-mono text-xs text-wb-ink">{{ (row as CronJob).schedule }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('cron.workflowLabel')" min-width="160">
            <template #default="{ row }">
              <span v-if="(row as CronJob).workflow_id" class="text-xs text-wb-muted">
                {{ workflowName((row as CronJob).workflow_id) }}
              </span>
              <span v-else class="text-xs text-wb-muted">-</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.enabled')" width="90">
            <template #default="{ row }">
              <el-tag size="small" :type="(row as CronJob).enabled ? 'success' : 'info'" effect="plain">
                {{ (row as CronJob).enabled ? t('common.enabled') : t('common.disabled') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('memory.center.col.actions')" width="200" fixed="right">
            <template #default="{ row }">
              <el-button
                v-if="(row as CronJob).enabled"
                link
                type="warning"
                size="small"
                :loading="triggeringID === (row as CronJob).id"
                @click="handleTriggerNow((row as CronJob).id)"
              >
                {{ t('cron.triggerNow') }}
              </el-button>
              <el-button link type="primary" size="small" @click="onEdit(row as CronJob)">{{ t('ui.btn.edit') }}</el-button>
              <el-button link type="danger" size="small" @click="remove((row as CronJob).id)">{{ t('ui.btn.delete') }}</el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-else :description="t('cron.empty')" :image-size="80" class="py-6" />
      </section>
    </div>
  </div>
</template>
