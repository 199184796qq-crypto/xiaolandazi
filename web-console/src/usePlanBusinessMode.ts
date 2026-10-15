import { onBeforeUnmount, ref, watch, type Ref } from 'vue'
import { getPlanBusinessMode, setPlanBusinessMode, setPlanBusinessIndustry } from './api'
import { isPlanBusinessMode, validatePlanBusinessSettings, type PlanBusinessMode, type PlanBusinessSettings, type PlanBusinessIndustryOption } from './planBusinessMode'

export function usePlanBusinessMode(planId: Readonly<Ref<number | null>>, tenantId: Readonly<Ref<number | undefined>>) {
  const value = ref<PlanBusinessMode | null>(null)
  const loading = ref(false)
  const saving = ref(false)
  const error = ref('')
  const industries = ref<Record<PlanBusinessMode, string>>({ ecommerce: '', local_service: '' })
  const industryOptions = ref<Record<PlanBusinessMode, PlanBusinessIndustryOption[]>>({ ecommerce: [], local_service: [] })
  let revision = 0

  function applySettings(settings: PlanBusinessSettings) {
    validatePlanBusinessSettings(settings)
    value.value = settings.biz_domain
    industries.value = { ...settings.industries }
    industryOptions.value = settings.industry_options
  }

  async function load() {
    const id = planId.value, tenant = tenantId.value, token = ++revision
    value.value = null
    industries.value = { ecommerce: '', local_service: '' }
    industryOptions.value = { ecommerce: [], local_service: [] }
    error.value = ''
    saving.value = false
    loading.value = !!id
    if (!id) return
    try {
      const result = await getPlanBusinessMode(id, tenant)
      if (token !== revision) return
      applySettings(result)
    } catch (e) {
      if (token === revision) error.value = e instanceof Error ? e.message : '读取业务模式失败'
    } finally {
      if (token === revision) loading.value = false
    }
  }

  async function selectIndustry(mode: PlanBusinessMode, industry: string) {
    const id = planId.value, tenant = tenantId.value
    if (!id || loading.value || saving.value || error.value || !isPlanBusinessMode(mode) || !industryOptions.value[mode].some(item => item.code === industry) || industries.value[mode] === industry) return
    const previous = { ...industries.value }, token = ++revision
    industries.value = { ...industries.value, [mode]: industry }
    saving.value = true
    try {
      const result = await setPlanBusinessIndustry(id, mode, industry, tenant)
      if (token !== revision) return
      applySettings(result)
    } catch (e) {
      if (token !== revision) return
      industries.value = previous
      error.value = e instanceof Error ? e.message : '保存行业失败'
    } finally {
      if (token === revision) saving.value = false
    }
  }

  async function select(next: PlanBusinessMode) {
    const id = planId.value, tenant = tenantId.value
    if (!id || !isPlanBusinessMode(next) || loading.value || saving.value || error.value || next === value.value) return
    const previous = value.value, token = ++revision
    value.value = next
    saving.value = true
    try {
      const result = await setPlanBusinessMode(id, next, tenant)
      if (token !== revision) return
      if (!isPlanBusinessMode(result.biz_domain)) throw new Error('保存结果异常，请重新读取')
      value.value = result.biz_domain
    } catch (e) {
      if (token !== revision) return
      value.value = previous
      error.value = e instanceof Error ? e.message : '保存业务模式失败'
    } finally {
      if (token === revision) saving.value = false
    }
  }

  watch([planId, tenantId], () => { void load() }, { immediate: true })
  onBeforeUnmount(() => { revision++ })
  return { value, industries, industryOptions, loading, saving, error, load, select, selectIndustry }
}
