import { request } from './api'

export type LectureAngle = { title: string; goal: string }
export type LectureDetail = { id: number; storage?: string; version?: number; name: string; text: string; quote: string }
export type LectureTask = { id: number; revision: number; title: string; purpose: string; subject?: string; root_fact?: { id: number; name: string; text: string }; angles: LectureAngle[]; facts: LectureDetail[]; links: string[]; enabled: boolean; valid: boolean; source?: string; updated_at?: string }
export type LecturePolicy = { enabled: boolean; frequency: 'less' | 'normal' | 'more' }
export type LectureLibrary = { tasks: LectureTask[]; policy: LecturePolicy; fingerprint: string }
export type LectureIssue = { quote: string; rule: string; reason: string; suggestion: string }
export type LectureIntake = { saved: boolean; task_id?: number; facts_added?: number; details_added?: number; subject?: string; title?: string; angles?: LectureAngle[]; notice?: string; issues?: LectureIssue[] }
export type LecturePreview = { text: string; raw_text?: string; approved: boolean; purpose: string; angle: string; notice: string; review_note?: string; issues: { kind: string; quote: string; reason: string }[] }
export function lectureURL(plan: number, tenant?: number, suffix = '') { return `/api/v1/live-agent-plans/${plan}/lecture-tasks${suffix}${tenant ? `?tenant_id=${tenant}` : ''}` }
export function getLectures(plan: number, tenant?: number) { return request<LectureLibrary>(lectureURL(plan, tenant), { cache: 'no-store', suppressErrorDialog: true }) }
export function ingestLecture(plan: number, tenant: number | undefined, source: string, links: string[]) { return request<LectureIntake>(lectureURL(plan, tenant, '/ingest'), { method: 'POST', body: JSON.stringify({ source, links }), suppressErrorDialog: true }) }
export function updateLecture(plan: number, tenant: number | undefined, task: LectureTask, action: string) { return request<{ ok: boolean }>(lectureURL(plan, tenant, `/${task.id}/action`), { method: 'POST', body: JSON.stringify({ revision: task.revision, action }), suppressErrorDialog: true }) }
export function setLecturePolicy(plan: number, tenant: number | undefined, policy: LecturePolicy) { return request<{ ok: boolean }>(lectureURL(plan, tenant, '/policy'), { method: 'PUT', body: JSON.stringify(policy), suppressErrorDialog: true }) }
export function previewLecture(plan: number, tenant: number | undefined, task: LectureTask, angle: number) { return request<LecturePreview>(lectureURL(plan, tenant, `/${task.id}/preview`), { method: 'POST', body: JSON.stringify({ revision: task.revision, angle }), suppressErrorDialog: true }) }
export function editLectureDetail(plan: number, tenant: number | undefined, task: LectureTask, fact: LectureDetail, name: string, text: string) { return request<{ saved: boolean; name: string; text: string; notice: string }>(lectureURL(plan, tenant, `/${task.id}/detail`), { method: 'PUT', body: JSON.stringify({ revision: task.revision, detail_id: fact.id, storage: fact.storage || '', name, text }), suppressErrorDialog: true }) }
