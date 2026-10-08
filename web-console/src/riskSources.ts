export type RiskArea = 'products' | 'benefits' | 'knowledge' | 'miner'
export type RiskSource = { source_key: string; text: string; area?: string; entity_id?: number; field?: string }
export type RiskReview = {
  claim_sha256: string; source_key: string; text: string; rule_id: string; severity: string;
  status: string; evidence: string; reason: string; semantic_decision: string;
  semantic_reason?: string; semantic_suggestion?: string; appeal_status?: string; confirmed_at?: string;
  sources?: RiskSource[]
}
export function riskSources(review: RiskReview): RiskSource[] {
  return review.sources?.length ? review.sources : [{ source_key: review.source_key, text: review.text }]
}
export function sourceArea(source: RiskSource): RiskArea | undefined {
  if (['products', 'benefits', 'knowledge', 'miner'].includes(source.area || '')) return source.area as RiskArea
  if (source.source_key.startsWith('wheel/')) return 'miner'
  // Legacy sources cannot distinguish product cards from true fact rows.
  // Do not guess from model text or route unknown origins to the fact badge.
  return undefined
}
export function reviewsForArea(reviews: RiskReview[], area: RiskArea, entityID?: number): RiskReview[] {
  return reviews.filter(review => riskSources(review).some(source => sourceArea(source) === area &&
    (entityID === undefined || source.entity_id === entityID)))
}
export function pendingForArea(reviews: RiskReview[], area: RiskArea): number {
  return reviewsForArea(reviews, area).filter(review => review.status !== 'confirmed').length
}
