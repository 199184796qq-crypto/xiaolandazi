<script setup lang="ts">
import { computed } from 'vue'
import areaData from 'china-area-data'

interface RegionOption {
  code: string
  name: string
}

const props = withDefaults(
  defineProps<{
    province: string
    city: string
    district: string
    required?: boolean
    disabled?: boolean
  }>(),
  {
    required: true,
    disabled: false,
  },
)

const emit = defineEmits<{
  'update:province': [value: string]
  'update:city': [value: string]
  'update:district': [value: string]
}>()

const DIRECT_MUNICIPALITIES = new Set([
  '110000',
  '120000',
  '310000',
  '500000',
])

const SPECIAL_REGIONS = new Set([
  '810000',
  '820000',
])

const provinces = computed<RegionOption[]>(() =>
  Object.entries(areaData['86'] ?? {}).map(([code, name]) => ({
    code,
    name,
  })),
)

const provinceCode = computed(() => {
  return (
    provinces.value.find((item) => item.name === props.province)?.code ?? ''
  )
})

const isDirectRegion = computed(
  () =>
    DIRECT_MUNICIPALITIES.has(provinceCode.value) ||
    SPECIAL_REGIONS.has(provinceCode.value),
)

const cities = computed<RegionOption[]>(() => {
  if (!provinceCode.value) return []

  if (isDirectRegion.value) {
    return [
      {
        code: provinceCode.value + ':direct',
        name: props.province,
      },
    ]
  }

  return Object.entries(areaData[provinceCode.value] ?? {}).map(
    ([code, name]) => ({
      code,
      name,
    }),
  )
})

const cityCode = computed(() => {
  if (!provinceCode.value) return ''

  if (isDirectRegion.value) {
    return props.city ? provinceCode.value + ':direct' : ''
  }

  return cities.value.find((item) => item.name === props.city)?.code ?? ''
})

const districts = computed<RegionOption[]>(() => {
  if (!provinceCode.value || !cityCode.value) return []

  if (SPECIAL_REGIONS.has(provinceCode.value)) {
    return Object.entries(areaData[provinceCode.value] ?? {}).map(
      ([code, name]) => ({
        code,
        name,
      }),
    )
  }

  if (DIRECT_MUNICIPALITIES.has(provinceCode.value)) {
    const prefix = provinceCode.value.slice(0, 2)

    if (provinceCode.value === '500000') {
      const merged = {
        ...(areaData['500100'] ?? {}),
        ...(areaData['500200'] ?? {}),
      }
      return Object.entries(merged).map(([code, name]) => ({
        code,
        name,
      }))
    }

    const municipalityCityCode = prefix + '0100'
    return Object.entries(areaData[municipalityCityCode] ?? {}).map(
      ([code, name]) => ({
        code,
        name,
      }),
    )
  }

  return Object.entries(areaData[cityCode.value] ?? {}).map(
    ([code, name]) => ({
      code,
      name,
    }),
  )
})

const districtCode = computed(() => {
  return districts.value.find((item) => item.name === props.district)?.code ?? ''
})

function onProvinceChange(event: Event) {
  const code = (event.target as HTMLSelectElement).value
  const selected = provinces.value.find((item) => item.code === code)

  emit('update:province', selected?.name ?? '')
  emit('update:city', '')
  emit('update:district', '')
}

function onCityChange(event: Event) {
  const code = (event.target as HTMLSelectElement).value
  const selected = cities.value.find((item) => item.code === code)

  emit('update:city', selected?.name ?? '')
  emit('update:district', '')
}

function onDistrictChange(event: Event) {
  const code = (event.target as HTMLSelectElement).value
  const selected = districts.value.find((item) => item.code === code)

  emit('update:district', selected?.name ?? '')
}
</script>

<template>
  <div class="region-select-grid">
    <label class="region-select-field">
      <span>省 <em v-if="required">*</em></span>
      <select
        :value="provinceCode"
        :required="required"
        :disabled="disabled"
        @change="onProvinceChange"
      >
        <option value="" disabled>请选择省份</option>
        <option
          v-for="item in provinces"
          :key="item.code"
          :value="item.code"
        >
          {{ item.name }}
        </option>
      </select>
    </label>

    <label class="region-select-field">
      <span>市 <em v-if="required">*</em></span>
      <select
        :value="cityCode"
        :required="required"
        :disabled="disabled || !provinceCode"
        @change="onCityChange"
      >
        <option value="" disabled>请选择城市</option>
        <option
          v-for="item in cities"
          :key="item.code"
          :value="item.code"
        >
          {{ item.name }}
        </option>
      </select>
    </label>

    <label class="region-select-field">
      <span>区 / 县 <em v-if="required">*</em></span>
      <select
        :value="districtCode"
        :required="required"
        :disabled="disabled || !cityCode"
        @change="onDistrictChange"
      >
        <option value="" disabled>请选择区 / 县</option>
        <option
          v-for="item in districts"
          :key="item.code"
          :value="item.code"
        >
          {{ item.name }}
        </option>
      </select>
    </label>
  </div>
</template>

<style scoped>
.region-select-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  width: 100%;
}

.region-select-field {
  display: grid;
  min-width: 0;
  gap: 8px;
}

.region-select-field > span {
  color: #555d6e;
  font-size: calc(10px + var(--ui-font-delta, 0px));
  font-weight: 700;
}

.region-select-field em {
  color: #d34b5a;
  font-style: normal;
}

.region-select-field select {
  width: 100%;
  min-height: 46px;
  border: 1px solid #dfe3eb;
  border-radius: 10px;
  padding: 0 38px 0 12px;
  color: #303747;
  background: #fff;
  outline: none;
  font: inherit;
  cursor: pointer;
  transition:
    border-color 160ms ease,
    box-shadow 160ms ease,
    background 160ms ease;
}

.region-select-field select:focus {
  border-color: #6667dc;
  box-shadow: 0 0 0 4px rgba(88, 89, 211, 0.08);
}

.region-select-field select:disabled {
  color: #a4aab7;
  cursor: not-allowed;
  background: #f6f7fa;
}

@media (max-width: 760px) {
  .region-select-grid {
    grid-template-columns: 1fr;
  }
}
</style>