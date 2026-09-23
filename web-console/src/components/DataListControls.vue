<script setup lang="ts">
interface Option {
  label: string
  value: string
}

withDefaults(
  defineProps<{
    viewMode: 'card' | 'table'
    search: string
    searchPlaceholder?: string
    status?: string
    statusOptions?: Option[]
    sort?: string
    sortOptions?: Option[]
    pageSize: number
  }>(),
  {
    searchPlaceholder: '搜索',
    status: 'all',
    statusOptions: () => [],
    sort: '',
    sortOptions: () => [],
  },
)

const emit = defineEmits<{
  'update:viewMode': [value: 'card' | 'table']
  'update:search': [value: string]
  'update:status': [value: string]
  'update:sort': [value: string]
  'update:pageSize': [value: number]
}>()
</script>

<template>
  <div class="data-list-controls">
    <div class="data-list-filters">
      <label class="data-search-box">
        <span>搜索</span>
        <input
          :value="search"
          type="search"
          :placeholder="searchPlaceholder"
          @input="emit('update:search', ($event.target as HTMLInputElement).value)"
        />
      </label>

      <label v-if="statusOptions.length" class="data-filter-select">
        <span>状态</span>
        <select
          :value="status"
          @change="emit('update:status', ($event.target as HTMLSelectElement).value)"
        >
          <option v-for="option in statusOptions" :key="option.value" :value="option.value">
            {{ option.label }}
          </option>
        </select>
      </label>

      <label v-if="sortOptions.length" class="data-filter-select">
        <span>排序</span>
        <select
          :value="sort"
          @change="emit('update:sort', ($event.target as HTMLSelectElement).value)"
        >
          <option v-for="option in sortOptions" :key="option.value" :value="option.value">
            {{ option.label }}
          </option>
        </select>
      </label>

      <label class="data-filter-select page-size-select">
        <span>每页</span>
        <select
          :value="pageSize"
          @change="emit('update:pageSize', Number(($event.target as HTMLSelectElement).value))"
        >
          <option :value="12">12</option>
          <option :value="24">24</option>
          <option :value="48">48</option>
        </select>
      </label>
    </div>

    <div class="data-view-toggle" aria-label="展示模式">
      <button
        type="button"
        :class="{ active: viewMode === 'card' }"
        @click="emit('update:viewMode', 'card')"
      >
        卡片
      </button>
      <button
        type="button"
        :class="{ active: viewMode === 'table' }"
        @click="emit('update:viewMode', 'table')"
      >
        表格
      </button>
    </div>
  </div>
</template>
