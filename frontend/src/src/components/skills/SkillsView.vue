<script setup lang="ts">
/**
 * Skill 管理视图（技能包编辑）。
 *
 * <p><b>技能包</b> = SKILL.md 正文(markdown) + 元信息(description / when_to_use / allowed_tools)
 * + scripts（可执行脚本，经 run_skill_script 工具运行）。
 * 来源：内置（只读）/ 手动编辑 / 本地 zip 批量导入。
 */
import { computed, onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { Zap, Upload, FileText, Plus, Trash2 } from '@/components/common/icons'
import { OpenFileDialog } from '@/wailsjs/go/main/App'
import { apiPost } from '@/api/client'
import { useSkillsStore } from '@/stores/skills'
import { t } from '@/i18n'
import { useToast } from '@/composables/useToast'
import type { Skill, SkillScript } from '@/types/api'

const skills = useSkillsStore()
const { skills: list, loading, importingZip, error, info, editingID, editingSource, creating, form } = storeToRefs(skills)
const toast = useToast()

onMounted(() => {
  skills.load()
})

/** 列表行内启停开关。 */
async function toggleEnabled(s: Skill, enabled: boolean): Promise<void> {
  try {
    await apiPost(`/api/v1/skills/${encodeURIComponent(s.name)}/enabled`, { enabled })
    await skills.load()
  } catch (e) {
    toast.error(t('chat.operationFailed'), e instanceof Error ? e.message : String(e))
  }
}

/** 内置技能只读（后端 UpdateCustom 也会拒绝）。 */
const readOnly = computed(() => editingSource.value === 'builtin')

// ===== 本地 zip 批量导入 =====
async function pickZipAndImport(): Promise<void> {
  try {
    const path = await OpenFileDialog(t('skill.zipPickTitle'), '*.zip')
    if (!path) return
    await skills.importZip(path)
  } catch (e) {
    toast.error(t('skill.zipImportFailed'), e instanceof Error ? e.message : String(e))
  }
}

// ===== scripts 包内脚本编辑 =====
const LANG_OPTIONS = [
  { value: 'javascript', label: t('skill.scriptLang.javascript') },
  { value: 'python', label: t('skill.scriptLang.python') },
  { value: 'powershell', label: t('skill.scriptLang.powershell') },
  { value: 'bash', label: t('skill.scriptLang.bash') }
]
const scriptDialogOpen = ref(false)
const scriptIdx = ref(-1)
const scriptForm = ref<SkillScript>({ name: '', language: 'javascript', code: '' })

function openScriptCreate(): void {
  skills.addScript()
  scriptIdx.value = (form.value.scripts ?? []).length - 1
  scriptForm.value = { name: '', language: 'javascript', code: '' }
  scriptDialogOpen.value = true
}
function openScriptEdit(i: number): void {
  const s = (form.value.scripts ?? [])[i]
  if (!s) return
  scriptIdx.value = i
  scriptForm.value = { name: s.name, language: s.language || 'javascript', code: s.code ?? '' }
  scriptDialogOpen.value = true
}
function saveScript(): void {
  const scripts = (form.value.scripts ?? []).slice()
  if (!scriptForm.value.name.trim()) {
    toast.warning(t('skill.scriptNameRequired'))
    return
  }
  scripts[scriptIdx.value] = { name: scriptForm.value.name.trim(), language: scriptForm.value.language, code: scriptForm.value.code }
  form.value.scripts = scripts
  scriptDialogOpen.value = false
}
function removeScript(i: number): void {
  skills.removeScriptAt(i)
}
</script>

<template>
  <div class="flex h-full flex-col overflow-y-auto text-wb-ink">
    <div class="mx-auto w-full max-w-4xl space-y-5 px-6 py-8">
      <!-- Hero header -->
      <header class="flex items-center gap-3">
        <div class="flex h-10 w-10 items-center justify-center rounded-lg bg-wb-primary/10 text-wb-primary">
          <Zap class="h-5 w-5" />
        </div>
        <div>
          <h1 class="font-display text-lg font-semibold text-wb-ink">{{ t('skill.title') }}</h1>
          <p class="text-xs text-wb-muted">{{ t('skill.subtitle') }}</p>
        </div>
        <div class="ml-auto flex items-center gap-2">
          <el-button :loading="importingZip" @click="pickZipAndImport">
            <Upload class="h-3.5 w-3.5" />
            <span class="ml-1">{{ t('skill.importZip') }}</span>
          </el-button>
          <el-button type="primary" @click="skills.startCreate">{{ t('skill.new') }}</el-button>
        </div>
      </header>

      <div v-if="error" class="rounded-lg bg-wb-danger/15 px-3 py-2 text-sm text-wb-danger">{{ error }}</div>
      <div v-if="info" class="rounded-lg bg-wb-success/15 px-3 py-2 text-sm text-wb-success">{{ info }}</div>

      <!-- Skill 编辑（技能包：meta + body + scripts；creating = 列表非空时「新建」也要展示表单） -->
      <section v-if="editingID || editingSource !== null || creating || list.length === 0" class="card p-5">
        <h2 class="mb-3 font-display text-sm font-semibold text-wb-ink">
          {{ editingID ? t('skill.edit') : t('skill.new') }}
        </h2>
        <el-alert
          v-if="readOnly"
          type="info"
          :closable="false"
          class="mb-3"
          :title="t('skill.builtinReadonly')"
        />
        <el-form class="wb-el-form" label-position="top" @submit.prevent="skills.submit">
          <div class="grid grid-cols-2 gap-3">
            <el-form-item :label="t('skill.name')">
              <el-input v-model="form.name" :disabled="readOnly" :placeholder="t('skill.name')" />
            </el-form-item>
            <el-form-item :label="t('skill.enabledLabel')">
              <el-switch v-model="form.enabled" :disabled="readOnly" />
            </el-form-item>
          </div>
          <el-form-item :label="t('skill.description')">
            <el-input v-model="form.description" :disabled="readOnly" :placeholder="t('skill.descriptionHint')" />
          </el-form-item>
          <el-form-item :label="t('skill.whenToUse')">
            <el-input
              v-model="form.when_to_use"
              :disabled="readOnly"
              type="textarea"
              :rows="2"
              resize="none"
              :placeholder="t('skill.whenToUseHint')"
            />
          </el-form-item>
          <el-form-item :label="t('skill.content')">
            <el-input v-model="form.body" :disabled="readOnly" type="textarea" :rows="10" class="font-mono" :placeholder="t('skill.contentHint')" />
          </el-form-item>
          <el-form-item :label="t('skill.allowedTools')">
            <el-select
              v-model="form.allowed_tools"
              :disabled="readOnly"
              multiple
              filterable
              allow-create
              default-first-option
              :reserve-keyword="false"
              class="w-full"
              :placeholder="t('skill.allowedToolsHint')"
            >
              <el-option v-for="tn in form.allowed_tools || []" :key="tn" :value="tn" :label="tn" />
            </el-select>
          </el-form-item>

          <!-- 技能包 scripts（如 SKILL.md 包 scripts/ 目录；run_skill_script 执行） -->
          <div class="mb-4 rounded-xl border border-wb-border bg-wb-surface/60 p-3">
            <div class="mb-2 flex items-center justify-between">
              <div class="flex items-center gap-1.5 text-xs font-medium text-wb-ink">
                <FileText class="h-3.5 w-3.5 text-wb-mint" />
                {{ t('skill.scripts') }}
                <span class="rounded bg-wb-mint/10 px-1.5 text-[10px] text-wb-mint">{{ (form.scripts ?? []).length }}</span>
              </div>
              <el-button size="small" text type="primary" :disabled="readOnly" @click="openScriptCreate">
                <Plus class="h-3.5 w-3.5" />
                {{ t('skill.scriptAdd') }}
              </el-button>
            </div>
            <div v-if="(form.scripts ?? []).length === 0" class="text-xs text-wb-muted">{{ t('skill.scriptsEmpty') }}</div>
            <ul v-else class="space-y-1">
              <li
                v-for="(sc, i) in (form.scripts ?? [])"
                :key="`${sc.name}-${i}`"
                class="flex items-center gap-2 rounded-lg bg-wb-surface px-2 py-1.5 text-xs"
              >
                <span class="min-w-0 flex-1 truncate font-mono text-wb-ink">{{ sc.name || '(unnamed)' }}</span>
                <span class="rounded bg-wb-primary/10 px-1.5 text-[10px] text-wb-primary-strong">{{ sc.language }}</span>
                <span class="text-[10px] tabular-nums text-wb-muted">{{ (sc.code ?? '').length }}B</span>
                <el-button link type="primary" size="small" :disabled="readOnly" @click="openScriptEdit(i)">{{ t('ui.btn.edit') }}</el-button>
                <el-button link type="danger" size="small" :disabled="readOnly" @click="removeScript(i)">
                  <Trash2 class="h-3.5 w-3.5" />
                </el-button>
              </li>
            </ul>
          </div>

          <div class="flex items-center gap-3">
            <span class="text-xs text-wb-muted">{{ t('skill.triggerHint') }}</span>
            <div class="flex-1" />
            <el-button v-if="editingID" @click="skills.cancel">{{ t('ui.btn.cancel') }}</el-button>
            <el-button type="primary" native-type="submit" :disabled="readOnly">
              {{ editingID ? t('ui.btn.save') : t('ui.btn.create') }}
            </el-button>
          </div>
        </el-form>
      </section>

      <!-- Skill 列表 -->
      <section class="card p-5">
        <h2 class="mb-3 font-display text-sm font-semibold text-wb-ink">{{ t('skill.list') }}</h2>
        <div v-if="loading" class="text-sm text-wb-muted">{{ t('ui.status.loading') }}</div>
        <el-table v-else-if="list.length > 0" :data="list" stripe class="wb-el-table">
          <el-table-column :label="t('skill.name')" min-width="200">
            <template #default="{ row }">
              <span class="font-medium text-wb-ink">{{ (row as Skill).name }}</span>
              <el-tag
                v-if="(row as Skill).source_kind"
                size="small"
                effect="plain"
                class="ml-2"
                :type="(row as Skill).source_kind === 'builtin' ? 'info' : 'warning'"
              >
                {{ (row as Skill).source_kind === 'builtin' ? t('pet.builtin') : t('skill.tagImported') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('skill.description')" min-width="240" show-overflow-tooltip>
            <template #default="{ row }">
              <span class="text-xs text-wb-muted">{{ (row as Skill).description || t('skill.noDescription') }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('skill.scripts')" width="90" align="center">
            <template #default="{ row }">
              <span class="text-xs text-wb-muted">{{ (row as Skill).scripts?.length ?? 0 }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.enabled')" width="80">
            <template #default="{ row }">
              <el-switch
                size="small"
                :model-value="(row as Skill).enabled"
                @change="(v: string | number | boolean) => void toggleEnabled(row as Skill, Boolean(v))"
              />
            </template>
          </el-table-column>
          <el-table-column :label="t('memory.center.col.actions')" width="120" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" size="small" @click="skills.startEdit(row as Skill)">{{ t('ui.btn.edit') }}</el-button>
              <el-button
                v-if="(row as Skill).source_kind !== 'builtin'"
                link
                type="danger"
                size="small"
                @click="skills.remove(row as Skill)"
              >
                {{ t('ui.btn.delete') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-else :description="t('skill.empty')" :image-size="80" class="py-6" />
      </section>
    </div>

    <!-- 脚本编辑对话框 -->
    <el-dialog
      :model-value="scriptDialogOpen"
      :title="t('skill.scriptEdit')"
      width="640px"
      append-to-body
      @update:model-value="(v: boolean) => (scriptDialogOpen = v)"
    >
      <div class="space-y-3">
        <div class="grid grid-cols-2 gap-3">
          <el-form-item :label="t('skill.scriptName')" class="mb-0">
            <el-input v-model="scriptForm.name" placeholder="hello" />
          </el-form-item>
          <el-form-item :label="t('skill.scriptLanguage')" class="mb-0">
            <el-select v-model="scriptForm.language" class="w-full">
              <el-option v-for="o in LANG_OPTIONS" :key="o.value" :value="o.value" :label="o.label" />
            </el-select>
          </el-form-item>
        </div>
        <el-form-item :label="t('skill.scriptCode')" class="mb-0">
          <el-input
            v-model="scriptForm.code"
            type="textarea"
            :rows="14"
            class="font-mono"
            :placeholder="t('skill.scriptCodeHint')"
          />
        </el-form-item>
      </div>
      <template #footer>
        <el-button @click="scriptDialogOpen = false">{{ t('ui.btn.cancel') }}</el-button>
        <el-button type="primary" @click="saveScript">{{ t('ui.btn.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>
