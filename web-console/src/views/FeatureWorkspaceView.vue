<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { confirmAction } from '../uiFeedback'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  createFeatureRecord,
  deleteFeatureRecord,
  getFeatureRecords,
  updateFeatureRecord,
} from '../api'
import DataListControls from '../components/DataListControls.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import { featureWorkspaceConfigs } from '../featureWorkspaces'
import { session } from '../session'
import type { FeatureField, FeatureWorkspaceConfig } from '../featureWorkspaces'
import type { FeatureRecord, FeatureRecordInput } from '../types'

const props = defineProps<{ featureKey: string }>()

const config = computed<FeatureWorkspaceConfig>(() => {
  const item = featureWorkspaceConfigs[props.featureKey]
  if (!item) throw new Error('未知配置工作台: ' + props.featureKey)
  return item
})

const loading = ref(false)
const saving = ref(false)
const error = useFeedbackErrorRef()
const items = ref<FeatureRecord[]>([])
const viewMode = ref<'card' | 'table'>('table')
const search = ref('')
const statusFilter = ref('all')
const sortMode = ref('sort-asc')
const page = ref(1)
const pageSize = ref(12)
const editorOpen = ref(false)
const editing = ref<FeatureRecord | null>(null)

const form = reactive<Record<string, string | number | boolean>>({
  record_key: '',
  title: '',
  status: 'active',
  sort_order: 0,
})

const statusOptions = [
  { label: '全部状态', value: 'all' },
  { label: '启用', value: 'active' },
  { label: '停用', value: 'inactive' },
  { label: '草稿', value: 'draft' },
  { label: '待处理', value: 'pending' },
  { label: '已完成', value: 'completed' },
  { label: '已取消', value: 'cancelled' },
]

const sortOptions = [
  { label: '默认排序', value: 'sort-asc' },
  { label: '名称 A-Z', value: 'title-asc' },
  { label: '名称 Z-A', value: 'title-desc' },
  { label: '最近更新', value: 'updated-desc' },
]

const canManage = computed(() => {
  const bootstrap = session.bootstrap
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'platform_admin') return true
  const access = bootstrap.staff_access
  if (!access) return false
  return access.is_super_admin || access.permissions.includes(config.value.managePermission)
})

const parsedItems = computed(() =>
  items.value.map((item) => ({
    item,
    payload: parsePayload(item.payload_json),
  })),
)

const filteredItems = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const result = parsedItems.value.filter(({ item, payload }) => {
    const matchesKeyword =
      !keyword ||
      [
        item.record_key,
        item.title,
        ...Object.values(payload).map((value) => String(value ?? '')),
      ].some((value) => value.toLowerCase().includes(keyword))
    const matchesStatus =
      statusFilter.value === 'all' || item.status === statusFilter.value
    return matchesKeyword && matchesStatus
  })

  return [...result].sort((a, b) => {
    if (sortMode.value === 'title-asc') {
      return a.item.title.localeCompare(b.item.title, 'zh-CN')
    }
    if (sortMode.value === 'title-desc') {
      return b.item.title.localeCompare(a.item.title, 'zh-CN')
    }
    if (sortMode.value === 'updated-desc') {
      return new Date(b.item.updated_at).getTime() - new Date(a.item.updated_at).getTime()
    }
    return a.item.sort_order - b.item.sort_order || a.item.id - b.item.id
  })
})

const totalPages = computed(() =>
  Math.max(1, Math.ceil(filteredItems.value.length / pageSize.value)),
)

const pagedItems = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredItems.value.slice(start, start + pageSize.value)
})

watch([search, statusFilter, sortMode, pageSize], () => {
  page.value = 1
})

watch(totalPages, (value) => {
  if (page.value > value) page.value = value
})

watch(
  () => props.featureKey,
  () => load(),
)

function parsePayload(value: string) {
  try {
    return JSON.parse(value || '{}') as Record<string, unknown>
  } catch {
    return {}
  }
}

function fieldDisplay(field: FeatureField, payload: Record<string, unknown>) {
  const value = payload[field.key]
  if (field.type === 'checkbox') return value ? '是' : '否'
  if (field.type === 'select') {
    return field.options?.find((option) => option.value === String(value))?.label || String(value || '—')
  }
  if (value === undefined || value === null || value === '') return '—'
  return String(value) + (field.unit ? ' ' + field.unit : '')
}

function statusLabel(value: string) {
  return statusOptions.find((item) => item.value === value)?.label || value
}

function resetForm() {
  for (const key of Object.keys(form)) {
    delete form[key]
  }
  form.record_key = ''
  form.title = ''
  form.status = 'active'
  form.sort_order = 0
  for (const field of config.value.fields) {
    form[field.key] = field.type === 'checkbox' ? false : ''
  }
}

function openCreate() {
  editing.value = null
  resetForm()
  editorOpen.value = true
  error.value = ''
}

function openEdit(item: FeatureRecord) {
  editing.value = item
  resetForm()
  form.record_key = item.record_key
  form.title = item.title
  form.status = item.status
  form.sort_order = item.sort_order
  const payload = parsePayload(item.payload_json)
  for (const field of config.value.fields) {
    const value = payload[field.key]
    if (field.type === 'checkbox') {
      form[field.key] = Boolean(value)
    } else {
      form[field.key] = value === undefined || value === null ? '' : String(value)
    }
  }
  editorOpen.value = true
  error.value = ''
}

function buildPayload() {
  const payload: Record<string, unknown> = {}
  for (const field of config.value.fields) {
    const value = form[field.key]
    if (field.type === 'checkbox') {
      payload[field.key] = Boolean(value)
    } else if (field.type === 'number') {
      payload[field.key] = value === '' ? 0 : Number(value)
    } else {
      payload[field.key] = String(value ?? '').trim()
    }
  }
  return payload
}

async function save() {
  if (!String(form.record_key || '').trim()) {
    error.value = config.value.codeLabel + '不能为空'
    return
  }
  if (!String(form.title || '').trim()) {
    error.value = config.value.recordLabel + '不能为空'
    return
  }

  for (const field of config.value.fields) {
    if (!field.required) continue
    const value = form[field.key]
    if (field.type === 'checkbox') continue
    if (String(value ?? '').trim() === '') {
      error.value = field.label + '不能为空'
      return
    }
  }

  const input: FeatureRecordInput = {
    record_key: String(form.record_key).trim(),
    title: String(form.title).trim(),
    status: String(form.status || 'active'),
    sort_order: Number(form.sort_order || 0),
    payload_json: JSON.stringify(buildPayload()),
  }

  saving.value = true
  error.value = ''
  try {
    if (editing.value) {
      await updateFeatureRecord(
        config.value.featureKey,
        editing.value.id,
        input,
      )
    } else {
      await createFeatureRecord(config.value.featureKey, input)
    }
    editorOpen.value = false
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存失败'
  } finally {
    saving.value = false
  }
}

async function remove(item: FeatureRecord) {
  if (!(await confirmAction({ title: '确认删除', message: '确定删除“' + item.title + '”吗？删除后不可恢复。', confirmText: '确认删除', danger: true }))) return
  error.value = ''
  try {
    await deleteFeatureRecord(config.value.featureKey, item.id)
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '删除失败'
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await getFeatureRecords(config.value.featureKey)
    items.value = data.items
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取配置失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page feature-workspace-page">
    <ModulePageNav
      :context="config.hub"
      :active-title="config.title"
    />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">{{ config.kicker }}</p>
        <h2>{{ config.title }}</h2>
        <p>{{ config.description }}</p>
      </div>
      <button
        v-if="canManage"
        class="primary-button"
        type="button"
        @click="openCreate"
      >
        ＋ 新增{{ config.recordLabel }}
      </button>
    </section>

    <section class="settings-card feature-workspace-panel">
      <DataListControls
        v-model:view-mode="viewMode"
        v-model:search="search"
        v-model:status="statusFilter"
        v-model:sort="sortMode"
        v-model:page-size="pageSize"
        :search-placeholder="'搜索' + config.recordLabel + ' / 编码 / 配置内容'"
        :status-options="statusOptions"
        :sort-options="sortOptions"
      />

      <p v-if="error" class="inline-error">{{ error }}</p>

      <div v-if="loading" class="panel-loading">正在读取{{ config.title }}...</div>

      <div v-else-if="viewMode === 'table'" class="data-table-wrap">
        <table class="data-table feature-record-table">
          <thead>
            <tr>
              <th>{{ config.recordLabel }}</th>
              <th>状态</th>
              <th
                v-for="field in config.fields.slice(0, 4)"
                :key="field.key"
              >
                {{ field.label }}
              </th>
              <th>更新时间</th>
              <th v-if="canManage">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="entry in pagedItems" :key="entry.item.id">
              <td>
                <strong>{{ entry.item.title }}</strong>
                <small>{{ entry.item.record_key }}</small>
              </td>
              <td>
                <span class="status-pill">{{ statusLabel(entry.item.status) }}</span>
              </td>
              <td
                v-for="field in config.fields.slice(0, 4)"
                :key="field.key"
              >
                {{ fieldDisplay(field, entry.payload) }}
              </td>
              <td>{{ new Date(entry.item.updated_at).toLocaleString('zh-CN') }}</td>
              <td v-if="canManage">
                <div class="table-actions">
                  <button class="text-action" type="button" @click="openEdit(entry.item)">
                    编辑
                  </button>
                  <button class="text-action danger" type="button" @click="remove(entry.item)">
                    删除
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="filteredItems.length === 0" class="empty-state">
          暂无符合条件的{{ config.recordLabel }}。
        </div>
      </div>

      <div v-else class="feature-record-grid">
        <article
          v-for="entry in pagedItems"
          :key="entry.item.id"
          class="feature-record-card"
        >
          <header>
            <div>
              <span>{{ entry.item.record_key }}</span>
              <h3>{{ entry.item.title }}</h3>
            </div>
            <span class="status-pill">{{ statusLabel(entry.item.status) }}</span>
          </header>

          <dl>
            <div
              v-for="field in config.fields"
              :key="field.key"
            >
              <dt>{{ field.label }}</dt>
              <dd>{{ fieldDisplay(field, entry.payload) }}</dd>
            </div>
          </dl>

          <footer v-if="canManage">
            <button class="text-action" type="button" @click="openEdit(entry.item)">编辑</button>
            <button class="text-action danger" type="button" @click="remove(entry.item)">删除</button>
          </footer>
        </article>

        <div v-if="filteredItems.length === 0" class="empty-state">
          暂无符合条件的{{ config.recordLabel }}。
        </div>
      </div>

      <PaginationBar
        v-model:page="page"
        :total-pages="totalPages"
        :total="filteredItems.length"
        :page-size="pageSize"
      />
    </section>

    <div v-if="editorOpen" class="feature-editor-backdrop" @click.self="editorOpen = false">
      <section class="feature-editor-panel">
        <header>
          <div>
            <span class="section-kicker">{{ editing ? 'EDIT' : 'CREATE' }}</span>
            <h3>{{ editing ? '编辑' : '新增' }}{{ config.recordLabel }}</h3>
          </div>
          <button class="icon-button" type="button" @click="editorOpen = false">×</button>
        </header>

        <div class="feature-editor-grid">
          <label>
            <span>{{ config.codeLabel }}</span>
            <input v-model="form.record_key" type="text" />
          </label>
          <label>
            <span>{{ config.recordLabel }}</span>
            <input v-model="form.title" type="text" />
          </label>
          <label>
            <span>状态</span>
            <select v-model="form.status">
              <option v-for="option in statusOptions.slice(1)" :key="option.value" :value="option.value">
                {{ option.label }}
              </option>
            </select>
          </label>
          <label>
            <span>排序</span>
            <input v-model.number="form.sort_order" type="number" />
          </label>

          <label
            v-for="field in config.fields"
            :key="field.key"
            :class="{ 'feature-editor-wide': field.type === 'textarea' }"
          >
            <span>{{ field.label }}<b v-if="field.required">*</b></span>

            <select
              v-if="field.type === 'select'"
              :value="String(form[field.key] ?? '')"
              @change="form[field.key] = ($event.target as HTMLSelectElement).value"
            >
              <option value="">请选择</option>
              <option
                v-for="option in field.options || []"
                :key="option.value"
                :value="option.value"
              >
                {{ option.label }}
              </option>
            </select>

            <textarea
              v-else-if="field.type === 'textarea'"
              :value="String(form[field.key] ?? '')"
              :placeholder="field.placeholder"
              rows="4"
              @input="form[field.key] = ($event.target as HTMLTextAreaElement).value"
            />

            <label v-else-if="field.type === 'checkbox'" class="feature-checkbox">
              <input v-model="form[field.key]" type="checkbox" />
              <span>是</span>
            </label>

            <div v-else class="feature-input-unit">
              <input
                :value="String(form[field.key] ?? '')"
                :type="field.type"
                :placeholder="field.placeholder"
                @input="form[field.key] = ($event.target as HTMLInputElement).value"
              />
              <span v-if="field.unit">{{ field.unit }}</span>
            </div>
          </label>
        </div>

        <p v-if="error" class="inline-error">{{ error }}</p>

        <footer>
          <button class="ghost-button" type="button" @click="editorOpen = false">取消</button>
          <button class="primary-button" type="button" :disabled="saving" @click="save">
            {{ saving ? '保存中...' : '保存' }}
          </button>
        </footer>
      </section>
    </div>
  </div>
</template>
