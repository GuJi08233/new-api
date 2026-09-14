/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { RatioDifference, RatioType } from '../types'
import { RATIO_TYPE_OPTIONS } from './constants'

export type ModelRow = {
  key: string
  model: string
  ratioTypes: Partial<Record<RatioType, RatioDifference>>
  billingConflict: boolean
}

export type ResolutionsMap = Record<string, Record<string, number | string>>

export type ResolutionSelection = {
  model: string
  ratioType: RatioType
  value: number | string
}

export type ResolutionRemoval = {
  model: string
  ratioType: RatioType
}

export type ResolutionRemovalPlan = Map<string, Set<RatioType>>

export const RATIO_SYNC_FIELDS: RatioType[] = [
  'model_ratio',
  'completion_ratio',
  'cache_ratio',
  'create_cache_ratio',
  'image_ratio',
  'audio_ratio',
  'audio_completion_ratio',
]

export const SYNC_FIELD_ORDER: RatioType[] = [
  ...RATIO_SYNC_FIELDS,
  'model_price',
  'billing_mode',
  'billing_expr',
]

export const NUMERIC_SYNC_FIELDS = new Set<string>([
  ...RATIO_SYNC_FIELDS,
  'model_price',
])

export function getSyncFieldLabel(
  ratioType: string,
  t: (key: string) => string
): string {
  const opt = RATIO_TYPE_OPTIONS.find((o) => o.value === ratioType)
  if (opt) return t(opt.label)
  return ratioType
}

export function getOrderedRatioTypes(
  ratioTypes: Partial<Record<RatioType, RatioDifference>>,
  filter?: string
): RatioType[] {
  const keys = Object.keys(ratioTypes) as RatioType[]
  const ordered = [
    ...SYNC_FIELD_ORDER.filter((f) => keys.includes(f)),
    ...keys.filter((f) => !SYNC_FIELD_ORDER.includes(f)),
  ]
  if (!filter || filter === '__all__') return ordered
  return ordered.filter((f) => f === filter)
}

export function getBillingCategory(
  ratioType: string
): 'price' | 'ratio' | 'tiered' {
  if (ratioType === 'model_price') return 'price'
  if (ratioType === 'billing_mode' || ratioType === 'billing_expr') {
    return 'tiered'
  }
  return 'ratio'
}

export function isSelectableUpstreamValue(
  value: number | string | null | undefined
): boolean {
  return value !== null && value !== undefined
}

export function isSelectedResolutionValue(
  resolutions: ResolutionsMap,
  model: string,
  ratioType: RatioType,
  upstreamValue: number | string | null | undefined
): boolean {
  if (!isSelectableUpstreamValue(upstreamValue)) return false

  const selectedValue = resolutions[model]?.[ratioType]
  if (selectedValue === undefined) return false

  if (NUMERIC_SYNC_FIELDS.has(ratioType)) {
    const selectedNumber = Number(selectedValue)
    const upstreamNumber = Number(upstreamValue)
    return (
      Number.isFinite(selectedNumber) &&
      Number.isFinite(upstreamNumber) &&
      selectedNumber === upstreamNumber
    )
  }

  return selectedValue === upstreamValue
}

export function deleteResolutionField(
  resolutions: ResolutionsMap,
  model: string,
  ratioType: RatioType
): ResolutionsMap {
  return applyResolutionRemovals(resolutions, [{ model, ratioType }])
}

function getDraftModelResolution(
  drafts: Map<string, Record<string, number | string>>,
  resolutions: ResolutionsMap,
  model: string
): Record<string, number | string> {
  const existingDraft = drafts.get(model)
  if (existingDraft) return existingDraft

  const draft = resolutions[model] ? { ...resolutions[model] } : {}
  drafts.set(model, draft)
  return draft
}

function applyResolutionSelectionToDraft(
  drafts: Map<string, Record<string, number | string>>,
  resolutions: ResolutionsMap,
  differences: Record<string, Partial<Record<RatioType, RatioDifference>>>,
  selection: ResolutionSelection
) {
  const category = getBillingCategory(selection.ratioType)
  const newModelRes = getDraftModelResolution(
    drafts,
    resolutions,
    selection.model
  )

  // 固定价格与倍率互斥，同一模型只能同步其中一类
  Object.keys(newModelRes).forEach((rt) => {
    if (
      category !== 'tiered' &&
      getBillingCategory(rt) !== 'tiered' &&
      getBillingCategory(rt) !== category
    ) {
      delete newModelRes[rt]
    }
  })

  newModelRes[selection.ratioType] = selection.value

  if (category === 'tiered') {
    const modeVal = differences[selection.model]?.billing_mode?.upstream
    if (modeVal !== undefined && modeVal !== null) {
      newModelRes['billing_mode'] = modeVal
    } else if (selection.ratioType === 'billing_expr') {
      newModelRes['billing_mode'] = 'tiered_expr'
    }
  }
}

export function getEffectiveResolutionSelections(
  selections: ResolutionSelection[]
): ResolutionSelection[] {
  const effectiveByKey = new Map<string, ResolutionSelection>()

  selections.forEach((selection) => {
    const category = getBillingCategory(selection.ratioType)

    if (category !== 'tiered') {
      for (const [key, existing] of effectiveByKey) {
        if (
          existing.model === selection.model &&
          getBillingCategory(existing.ratioType) !== 'tiered' &&
          getBillingCategory(existing.ratioType) !== category
        ) {
          effectiveByKey.delete(key)
        }
      }
    }

    effectiveByKey.set(
      JSON.stringify([selection.model, selection.ratioType]),
      selection
    )
  })

  return [...effectiveByKey.values()]
}

export function applyResolutionSelections(
  resolutions: ResolutionsMap,
  differences: Record<string, Partial<Record<RatioType, RatioDifference>>>,
  selections: ResolutionSelection[]
): ResolutionsMap {
  if (selections.length === 0) return resolutions

  const next = { ...resolutions }
  const drafts = new Map<string, Record<string, number | string>>()

  selections.forEach((selection) => {
    applyResolutionSelectionToDraft(drafts, resolutions, differences, selection)
  })

  drafts.forEach((draft, model) => {
    if (Object.keys(draft).length === 0) {
      delete next[model]
    } else {
      next[model] = draft
    }
  })

  return next
}

export function applyResolutionSelection(
  resolutions: ResolutionsMap,
  differences: Record<string, Partial<Record<RatioType, RatioDifference>>>,
  selection: ResolutionSelection
): ResolutionsMap {
  return applyResolutionSelections(resolutions, differences, [selection])
}

export function applyResolutionRemovals(
  resolutions: ResolutionsMap,
  removals: ResolutionRemoval[]
): ResolutionsMap {
  if (removals.length === 0) return resolutions

  const plan: ResolutionRemovalPlan = new Map()
  removals.forEach((removal) => {
    const ratioTypes = plan.get(removal.model)
    if (ratioTypes) {
      ratioTypes.add(removal.ratioType)
    } else {
      plan.set(removal.model, new Set([removal.ratioType]))
    }
  })

  return applyResolutionRemovalPlan(resolutions, plan)
}

export function applyResolutionRemovalPlan(
  resolutions: ResolutionsMap,
  plan: ResolutionRemovalPlan
): ResolutionsMap {
  if (plan.size === 0) return resolutions

  const next = { ...resolutions }

  plan.forEach((ratioTypes, model) => {
    const current = resolutions[model]
    if (!current) return

    const draft = { ...current }
    ratioTypes.forEach((ratioType) => {
      delete draft[ratioType]
      if (ratioType === 'billing_expr') delete draft['billing_mode']
      if (ratioType === 'billing_mode') delete draft['billing_expr']
    })
    if (Object.keys(draft).length === 0) {
      delete next[model]
    } else {
      next[model] = draft
    }
  })

  return next
}
