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
import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'

import { api } from '@/lib/api'

// Mirrors the backend model.NameRule* constants
const NAME_RULE_EXACT = 0
const NAME_RULE_PREFIX = 1
const NAME_RULE_CONTAINS = 2
const NAME_RULE_SUFFIX = 3

// Match order for non-exact rules, matching how the backend expands model
// metadata for pricing, so logs and the model plaza resolve the same icon.
const PATTERN_RULE_PRIORITY: Record<number, number> = {
  [NAME_RULE_PREFIX]: 0,
  [NAME_RULE_SUFFIX]: 1,
  [NAME_RULE_CONTAINS]: 2,
}

export interface ModelIconRule {
  model_name: string
  icon: string
  name_rule: number
}

async function getModelIcons(): Promise<ModelIconRule[]> {
  const res = await api.get('/api/model_icons', { skipErrorHandler: true })
  return Array.isArray(res.data?.data) ? res.data.data : []
}

/**
 * Resolve the icon configured in model management for a given model name.
 * Returns an empty string when the model has no configured icon — callers
 * must not fall back to guessing a vendor from the model name.
 */
export function useModelIcons(): (modelName: string) => string {
  const { data } = useQuery({
    queryKey: ['model-icons'],
    queryFn: getModelIcons,
    staleTime: 5 * 60 * 1000,
  })

  return useMemo(() => {
    const exactIcons = new Map<string, string>()
    const patternRules: ModelIconRule[] = []

    for (const rule of data ?? []) {
      if (!rule?.model_name || !rule?.icon) continue
      if (rule.name_rule === NAME_RULE_EXACT || rule.name_rule == null) {
        if (!exactIcons.has(rule.model_name)) {
          exactIcons.set(rule.model_name, rule.icon)
        }
        continue
      }
      patternRules.push(rule)
    }
    patternRules.sort(
      (a, b) =>
        PATTERN_RULE_PRIORITY[a.name_rule] - PATTERN_RULE_PRIORITY[b.name_rule]
    )

    return (modelName: string) => {
      if (!modelName) return ''

      const exact = exactIcons.get(modelName)
      if (exact) return exact

      for (const rule of patternRules) {
        switch (rule.name_rule) {
          case NAME_RULE_PREFIX:
            if (modelName.startsWith(rule.model_name)) return rule.icon
            break
          case NAME_RULE_SUFFIX:
            if (modelName.endsWith(rule.model_name)) return rule.icon
            break
          case NAME_RULE_CONTAINS:
            if (modelName.includes(rule.model_name)) return rule.icon
            break
          default:
            break
        }
      }

      return ''
    }
  }, [data])
}
