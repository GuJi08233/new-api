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
import { useEffect, useState } from 'react'
import * as z from 'zod'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { Check, Code, Pencil, Plus, Table2, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { validateJsonString } from './utils'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import {
  GroupSelector,
  ModelNameMatcher,
  PathSelector,
  ChannelSelector,
} from './channel-route-selectors'
import { useChannelNameMap } from './channel-route-hooks'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type RouteTierCondition = {
  var: 'len' | 'p' | 'c' | 'docs'
  op: '<' | '<=' | '>' | '>='
  value: number
}

type RouteTier = {
  conditions: RouteTierCondition[]
  channel_ids: number[]
  reject?: boolean
  reject_message?: string
  label: string
}

type EditableRouteTier = RouteTier & { editorId: number }

let routeTierEditorId = 0

function nextRouteTierEditorId(): number {
  routeTierEditorId += 1
  return routeTierEditorId
}

type ChannelRouteRule = {
  id: number
  name: string
  group_regex: string[]
  model_regex: string[]
  path_regex: string[]
  strict: boolean
  route_tiers?: RouteTier[]
}

type ChannelRouteSettingsCardProps = {
  defaultValues: {
    enabled: boolean
    rules: string
  }
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const VAR_OPTIONS: { value: RouteTierCondition['var']; label: string }[] = [
  { value: 'len', label: 'len (input length)' },
  { value: 'p', label: 'p (prompt tokens)' },
  { value: 'c', label: 'c (completion)' },
  { value: 'docs', label: 'docs (document count)' },
]
const OPS: RouteTierCondition['op'][] = ['<', '<=', '>', '>=']

// ---------------------------------------------------------------------------
// Schemas
// ---------------------------------------------------------------------------

const routeSchema = z.object({
  enabled: z.boolean(),
  rules: z.string().superRefine((value, ctx) => {
    const result = validateJsonString(value, {
      predicate: (parsed) => Array.isArray(parsed),
      predicateMessage: 'Rules JSON must be an array',
    })
    if (!result.valid) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: result.message || 'Invalid JSON',
      })
    }
  }),
})

type ChannelRouteFormValues = z.infer<typeof routeSchema>

// ---------------------------------------------------------------------------
// Examples
// ---------------------------------------------------------------------------

const rulesExample = JSON.stringify(
  [
    {
      name: 'qwen messages native',
      group_regex: ['^default$'],
      model_regex: ['^Qwen3\\.5-35B-A3B$'],
      path_regex: ['^/v1/messages$'],
      strict: true,
      route_tiers: [{ label: 'catch-all', conditions: [], channel_ids: [12] }],
    },
    {
      name: 'GPT-4o tiered routing',
      model_regex: ['^gpt-4o$'],
      route_tiers: [
        {
          label: 'short',
          conditions: [{ var: 'len', op: '<', value: 17000 }],
          channel_ids: [5, 6],
        },
        { label: 'long', conditions: [], channel_ids: [7, 8] },
      ],
      strict: false,
    },
    {
      name: 'Rerank document-count routing',
      path_regex: ['^/v1/rerank$'],
      model_regex: ['^rerank-model$'],
      route_tiers: [
        {
          label: 'small batch',
          conditions: [{ var: 'docs', op: '<=', value: 25 }],
          channel_ids: [1, 2],
        },
        {
          label: 'large batch',
          conditions: [
            { var: 'docs', op: '>', value: 25 },
            { var: 'docs', op: '<=', value: 200 },
          ],
          channel_ids: [3],
        },
        {
          label: 'over limit',
          conditions: [{ var: 'docs', op: '>', value: 200 }],
          channel_ids: [],
          reject: true,
          reject_message: 'Candidate documents cannot exceed 200',
        },
      ],
      strict: true,
    },
  ],
  null,
  2
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function formatTokenHint(
  value: number,
  variable?: RouteTierCondition['var']
): string {
  if (variable === 'docs') return ''
  if (!value || value <= 0) return ''
  if (value >= 1_000_000) return `= ${(value / 1_000_000).toFixed(1)}M tokens`
  if (value >= 1_000) return `= ${(value / 1_000).toFixed(0)}K tokens`
  return `= ${value} tokens`
}

function formatTokenShort(value: number): string {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(0)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(0)}K`
  return String(value)
}

/** Auto-generate a label from tier conditions when user leaves it empty. */
function autoTierLabel(conditions: RouteTierCondition[]): string {
  if (!conditions || conditions.length === 0) return ''
  return conditions
    .map(
      (condition) =>
        `${condition.var} ${condition.op} ${condition.var === 'docs' ? String(condition.value) : formatTokenShort(condition.value)}`
    )
    .join(' AND ')
}

/** Validate tier configuration. Returns an array of warning strings. */
function validateTiers(tiers: RouteTier[]): string[] {
  const warnings: string[] = []
  if (tiers.length <= 1) return warnings

  // Check: catch-all (no conditions) must be the last tier
  for (let i = 0; i < tiers.length - 1; i++) {
    if (tiers[i].conditions.length === 0) {
      warnings.push(
        `Tier ${i + 1} has no conditions (catch-all) but is not the last tier. It will block all subsequent tiers from matching.`
      )
    }
  }

  // Check for dead tiers and overlaps by building effective ranges per variable
  // For each variable (len, p, c), track the constraints imposed by earlier tiers
  for (let i = 1; i < tiers.length; i++) {
    const tier = tiers[i]
    if (tier.conditions.length === 0) continue // catch-all, skip

    // Check if this tier's conditions are unreachable given previous tiers
    for (const cond of tier.conditions) {
      for (let j = 0; j < i; j++) {
        const prevTier = tiers[j]
        if (prevTier.conditions.length === 0) continue

        for (const prevCond of prevTier.conditions) {
          if (prevCond.var !== cond.var) continue

          // Check if prev tier's condition makes this tier's condition impossible
          // prev: x < 100, current: x >= 200 → dead (impossible since prev catches all x < 100)
          // But if there's a gap (100 <= x < 200), the current tier could still match
          // The issue is: if prev tier matches all x < 100, and current needs x >= 200,
          // then values 100-199 fall through to current tier (not dead, just gap)
          // A tier is truly dead only if ALL previous tiers' conditions completely cover it

          // Simple overlap detection:
          // prev: x < A, current: x < B where B <= A → current is dead (subset)
          if (prevCond.op === '<' && cond.op === '<' && cond.value <= prevCond.value) {
            warnings.push(
              `Tier ${i + 1} condition "${cond.var} < ${formatTokenShort(cond.value)}" is already covered by Tier ${j + 1} "${prevCond.var} < ${formatTokenShort(prevCond.value)}".`
            )
          }
          // prev: x <= A, current: x <= B where B <= A → current is dead
          if (prevCond.op === '<=' && cond.op === '<=' && cond.value <= prevCond.value) {
            warnings.push(
              `Tier ${i + 1} condition "${cond.var} <= ${formatTokenShort(cond.value)}" is already covered by Tier ${j + 1} "${prevCond.var} <= ${formatTokenShort(prevCond.value)}".`
            )
          }
          // prev: x >= A, current: x >= B where B <= A → current is dead
          if (prevCond.op === '>=' && cond.op === '>=' && cond.value <= prevCond.value) {
            warnings.push(
              `Tier ${i + 1} condition "${cond.var} >= ${formatTokenShort(cond.value)}" is already covered by Tier ${j + 1} "${prevCond.var} >= ${formatTokenShort(prevCond.value)}".`
            )
          }
          // prev: x > A, current: x > B where B <= A → current is dead
          if (prevCond.op === '>' && cond.op === '>' && cond.value <= prevCond.value) {
            warnings.push(
              `Tier ${i + 1} condition "${cond.var} > ${formatTokenShort(cond.value)}" is already covered by Tier ${j + 1} "${prevCond.var} > ${formatTokenShort(prevCond.value)}".`
            )
          }
          // prev: x < A, current: x >= A → exact boundary, no overlap (valid)
          // prev: x < A, current: x >= B where B < A → overlap on [B, A)
          if (prevCond.op === '<' && cond.op === '>=' && cond.value < prevCond.value) {
            warnings.push(
              `Tier ${i + 1} condition "${cond.var} >= ${formatTokenShort(cond.value)}" overlaps with Tier ${j + 1} "${prevCond.var} < ${formatTokenShort(prevCond.value)}" (range ${formatTokenShort(cond.value)}~${formatTokenShort(prevCond.value)}).`
            )
          }
          // prev: x <= A, current: x > A → exact boundary, no overlap (valid)
          // prev: x <= A, current: x > B where B < A → overlap on (B, A]
          if (prevCond.op === '<=' && cond.op === '>' && cond.value < prevCond.value) {
            warnings.push(
              `Tier ${i + 1} condition "${cond.var} > ${formatTokenShort(cond.value)}" overlaps with Tier ${j + 1} "${prevCond.var} <= ${formatTokenShort(prevCond.value)}" (range ${formatTokenShort(cond.value)}~${formatTokenShort(prevCond.value)}).`
            )
          }
          // prev: x >= A, current: x < A → exact boundary, no overlap (valid)
          // prev: x >= A, current: x < B where B > A → overlap on [A, B)
          if (prevCond.op === '>=' && cond.op === '<' && cond.value > prevCond.value) {
            warnings.push(
              `Tier ${i + 1} condition "${cond.var} < ${formatTokenShort(cond.value)}" overlaps with Tier ${j + 1} "${prevCond.var} >= ${formatTokenShort(prevCond.value)}" (range ${formatTokenShort(prevCond.value)}~${formatTokenShort(cond.value)}).`
            )
          }
          // prev: x > A, current: x <= B where B > A → overlap on (A, B]
          if (prevCond.op === '>' && cond.op === '<=' && cond.value > prevCond.value) {
            warnings.push(
              `Tier ${i + 1} condition "${cond.var} <= ${formatTokenShort(cond.value)}" overlaps with Tier ${j + 1} "${prevCond.var} > ${formatTokenShort(prevCond.value)}" (range ${formatTokenShort(prevCond.value)}~${formatTokenShort(cond.value)}).`
            )
          }
        }
      }
    }
  }

  return warnings
}

function normalizeStringList(text?: string) {
  return (text || '')
    .split('\n')
    .map((item) => item.trim())
    .filter(Boolean)
}

function normalizeChannelIds(text?: string) {
  const tokens = (text || '')
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean)
  const values: number[] = []
  for (const token of tokens) {
    const num = Number(token)
    if (!Number.isInteger(num) || num <= 0) return null
    values.push(num)
  }
  return [...new Set(values)]
}

function formatJson(value: unknown) {
  return JSON.stringify(value, null, 2)
}

function normalizeJson(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return '[]'
  try {
    return JSON.stringify(JSON.parse(trimmed))
  } catch {
    return trimmed
  }
}

function migrateOldFormat(rule: Record<string, unknown>): Record<string, unknown> {
  // Legacy: token_threshold / short_channel_ids / long_channel_ids
  if (!rule.route_tiers) {
    const threshold = rule.token_threshold as number | undefined
    const shortIds = rule.short_channel_ids as number[] | undefined
    const longIds = rule.long_channel_ids as number[] | undefined
    if (threshold && threshold > 0) {
      const tiers: RouteTier[] = []
      if (shortIds && shortIds.length > 0) {
        tiers.push({
          label: 'short',
          conditions: [{ var: 'len', op: '<', value: threshold }],
          channel_ids: shortIds,
        })
      }
      if (longIds && longIds.length > 0) {
        tiers.push({ label: 'long', conditions: [], channel_ids: longIds })
      }
      if (tiers.length > 0) rule.route_tiers = tiers
    }
  }
  delete rule.token_threshold
  delete rule.short_channel_ids
  delete rule.long_channel_ids

  // Legacy: standalone channel_ids (fallback pool). Convert to a catch-all
  // tier appended after any existing tiers, then drop the field.
  const legacyChannelIds = rule.channel_ids as number[] | undefined
  if (Array.isArray(legacyChannelIds) && legacyChannelIds.length > 0) {
    const tiers = Array.isArray(rule.route_tiers)
      ? [...(rule.route_tiers as RouteTier[])]
      : []
    const hasCatchAll = tiers.some(
      (t) => !t.conditions || t.conditions.length === 0,
    )
    if (!hasCatchAll) {
      tiers.push({
        label: 'catch-all',
        conditions: [],
        channel_ids: legacyChannelIds,
      })
    }
    rule.route_tiers = tiers
  }
  delete rule.channel_ids
  return rule
}

function parseRulesJson(value: string) {
  try {
    const parsed = JSON.parse(value || '[]')
    if (!Array.isArray(parsed)) return []
    return parsed.map((rule, index) => {
      const migrated = migrateOldFormat(rule || {})
      return { id: index, ...migrated }
    }) as ChannelRouteRule[]
  } catch {
    return []
  }
}

function rulesToJson(rules: ChannelRouteRule[]) {
  return formatJson(
    rules.map((rule) => {
      const { id, ...rest } = rule
      return rest
    })
  )
}

function emptyTier(): EditableRouteTier {
  return {
    editorId: nextRouteTierEditorId(),
    label: '',
    conditions: [],
    channel_ids: [],
    reject: false,
    reject_message: '',
  }
}

// ---------------------------------------------------------------------------
// ConditionRow — reuses the same pattern as tiered-pricing-editor
// ---------------------------------------------------------------------------

function ConditionRow({
  condition,
  onChange,
  onRemove,
}: {
  condition: RouteTierCondition
  onChange: (next: RouteTierCondition) => void
  onRemove: () => void
}) {
  return (
    <div className='flex items-center gap-2'>
      <Select
        items={VAR_OPTIONS}
        value={condition.var}
        onValueChange={(value) => {
          if (value) onChange({ ...condition, var: value })
        }}
      >
        <SelectTrigger className='w-32' size='sm'>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectGroup>
            {VAR_OPTIONS.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      <Select
        items={OPS.map((op) => ({ label: op, value: op }))}
        value={condition.op}
        onValueChange={(value) => {
          if (value) onChange({ ...condition, op: value })
        }}
      >
        <SelectTrigger className='w-20' size='sm'>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectGroup>
            {OPS.map((op) => (
              <SelectItem key={op} value={op}>
                {op}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      <Input
        type='number'
        min={0}
        step={1}
        value={condition.value || ''}
        onChange={(e) => {
          const value = Number(e.target.value)
          onChange({
            ...condition,
            value: Number.isFinite(value) ? Math.max(0, Math.trunc(value)) : 0,
          })
        }}
        placeholder={condition.var === 'docs' ? 'docs' : 'tokens'}
        className='h-8 w-32'
      />
      <span className='text-muted-foreground text-xs'>
        {formatTokenHint(condition.value, condition.var)}
      </span>
      <Button
        variant='ghost'
        size='icon'
        onClick={onRemove}
        aria-label='remove'
        className='ml-auto'
      >
        <Trash2 className='text-destructive h-4 w-4' />
      </Button>
    </div>
  )
}

// ---------------------------------------------------------------------------
// RouteTierCard — single tier with conditions + channel pool
// ---------------------------------------------------------------------------

function RouteTierCard({
  tier,
  index,
  total,
  onChange,
  onRemove,
  onAddCondition,
}: {
  tier: EditableRouteTier
  index: number
  total: number
  onChange: (next: EditableRouteTier) => void
  onRemove: () => void
  onAddCondition: () => void
}) {
  const { t } = useTranslation()
  const isCatchAll = tier.conditions.length === 0 && index === total - 1

  const handleConditionChange = (ci: number, next: RouteTierCondition) => {
    const conditions = [...tier.conditions]
    conditions[ci] = next
    onChange({ ...tier, conditions })
  }

  const handleConditionRemove = (ci: number) => {
    onChange({ ...tier, conditions: tier.conditions.filter((_, i) => i !== ci) })
  }

  const handleRejectChange = (reject: boolean) => {
    onChange(reject ? { ...tier, reject, channel_ids: [] } : { ...tier, reject })
  }

  return (
    <div className='bg-muted/30 space-y-3 rounded-md border p-3'>
      <div className='flex items-center justify-between gap-2'>
        <div className='flex items-center gap-2'>
          <Badge variant='outline'>
            {t('Tier')} {index + 1} / {total}
          </Badge>
          <Input
            value={tier.label}
            onChange={(e) => onChange({ ...tier, label: e.target.value })}
            placeholder={autoTierLabel(tier.conditions) || t('Tier name')}
            className='h-8 w-40'
          />
          {!tier.label && autoTierLabel(tier.conditions) && (
            <span className='text-muted-foreground text-xs'>
              {autoTierLabel(tier.conditions)}
            </span>
          )}
          {isCatchAll && (
            <Badge variant='secondary' className='text-xs'>
              {t('catch-all')}
            </Badge>
          )}
        </div>
        <Button
          variant='ghost'
          size='icon'
          onClick={onRemove}
          disabled={total <= 1}
          aria-label={t('Remove tier')}
        >
          <Trash2 className='text-destructive h-4 w-4' />
        </Button>
      </div>

      {!isCatchAll && (
        <div className='space-y-2'>
          <div className='flex items-center justify-between'>
            <Label className='text-xs'>{t('Conditions (AND)')}</Label>
            <Button
              variant='ghost'
              size='sm'
              onClick={onAddCondition}
              disabled={tier.conditions.length >= 2}
              className='h-7 px-2 text-xs'
            >
              <Plus className='mr-1 h-3 w-3' />
              {t('Add condition')}
            </Button>
          </div>
          {tier.conditions.length === 0 ? (
            <p className='text-muted-foreground text-xs'>
              {t('Always matches (default tier).')}
            </p>
          ) : (
            tier.conditions.map((cond, ci) => (
              <ConditionRow
                key={`${cond.var}:${cond.op}:${cond.value}`}
                condition={cond}
                onChange={(next) => handleConditionChange(ci, next)}
                onRemove={() => handleConditionRemove(ci)}
              />
            ))
          )}
        </div>
      )}

      <FieldGroup className='gap-3'>
        <Field orientation='horizontal'>
          <FieldContent>
            <FieldLabel htmlFor={`tier-reject-${index}`}>
              {t('Reject requests matching this tier (no routing)')}
            </FieldLabel>
            <FieldDescription>
              {t(
                'Requests matching this tier will be rejected with an error instead of being routed.'
              )}
            </FieldDescription>
          </FieldContent>
          <Switch
            id={`tier-reject-${index}`}
            checked={!!tier.reject}
            onCheckedChange={handleRejectChange}
          />
        </Field>

        {tier.reject ? (
          <Field>
            <FieldLabel htmlFor={`tier-reject-message-${index}`}>
              {t('Custom reject message (optional, default used when empty)')}
            </FieldLabel>
            <Input
              id={`tier-reject-message-${index}`}
              value={tier.reject_message || ''}
              onChange={(event) =>
                onChange({ ...tier, reject_message: event.target.value })
              }
            />
          </Field>
        ) : (
          <ChannelSelector
            value={(tier.channel_ids || []).join('\n')}
            onChange={(text) => {
              const ids = normalizeChannelIds(text)
              onChange({ ...tier, channel_ids: ids || [] })
            }}
            compact
          />
        )}
      </FieldGroup>
    </div>
  )
}

// ---------------------------------------------------------------------------
// RouteTierEditor — manages list of tier cards
// ---------------------------------------------------------------------------

function RouteTierEditor({
  tiers,
  onChange,
}: {
  tiers: EditableRouteTier[]
  onChange: (next: EditableRouteTier[]) => void
}) {
  const { t } = useTranslation()

  const handleTierChange = (index: number, next: EditableRouteTier) => {
    const nextTiers = [...tiers]
    nextTiers[index] = next
    onChange(nextTiers)
  }

  const handleAddTier = () => {
    const nextTiers = [...tiers]
    // Give the previous catch-all a default condition when adding a new tier
    const lastIndex = nextTiers.length - 1
    if (lastIndex >= 0 && nextTiers[lastIndex].conditions.length === 0) {
      nextTiers[lastIndex] = {
        ...nextTiers[lastIndex],
        conditions: [{ var: 'len', op: '<', value: 200000 }],
      }
    }
    nextTiers.push(emptyTier())
    onChange(nextTiers)
  }

  const handleRemoveTier = (index: number) => {
    const nextTiers = tiers.filter((_, i) => i !== index)
    onChange(nextTiers.length > 0 ? nextTiers : [emptyTier()])
  }

  const handleAddCondition = (index: number) => {
    if (tiers[index].conditions.length >= 2) return
    const usedVars = new Set(tiers[index].conditions.map((c) => c.var))
    const nextVar: RouteTierCondition['var'] = usedVars.has('len') ? 'c' : 'len'
    const nextTiers = tiers.map((tier, i) =>
      i === index
        ? {
            ...tier,
            conditions: [
              ...tier.conditions,
              { var: nextVar, op: '<' as const, value: 200000 },
            ],
          }
        : tier
    )
    onChange(nextTiers)
  }

  return (
    <div className='space-y-3'>
      {tiers.map((tier, index) => (
        <RouteTierCard
          key={tier.editorId}
          tier={tier}
          index={index}
          total={tiers.length}
          onChange={(next) => handleTierChange(index, next)}
          onRemove={() => handleRemoveTier(index)}
          onAddCondition={() => handleAddCondition(index)}
        />
      ))}
      <Button variant='outline' size='sm' onClick={handleAddTier}>
        <Plus className='mr-2 h-3 w-3' />
        {t('Add tier')}
      </Button>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export function ChannelRouteSettingsCard({
  defaultValues,
}: ChannelRouteSettingsCardProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [mode, setMode] = useState<'visual' | 'json'>('visual')
  const [rules, setRules] = useState<ChannelRouteRule[]>(() =>
    parseRulesJson(defaultValues.rules)
  )
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<ChannelRouteRule | null>(null)

  // Tier editor state (managed separately from the form)
  const [editingTiers, setEditingTiers] = useState<EditableRouteTier[]>([])

  // Channel name resolver for displaying names in rule list
  const getChannelName = useChannelNameMap()

  const form = useForm<ChannelRouteFormValues>({
    resolver: zodResolver(routeSchema),
    defaultValues: {
      enabled: defaultValues.enabled,
      rules: rulesToJson(parseRulesJson(defaultValues.rules)),
    },
  })

  useEffect(() => {
    const parsed = parseRulesJson(defaultValues.rules)
    setRules(parsed)
    form.reset({
      enabled: defaultValues.enabled,
      rules: rulesToJson(parsed),
    })
  }, [defaultValues, form])

  const updateRulesState = (nextRules: ChannelRouteRule[]) => {
    const normalized = nextRules.map((rule, index) => ({
      ...rule,
      id: index,
    }))
    setRules(normalized)
    form.setValue('rules', rulesToJson(normalized), {
      shouldDirty: true,
      shouldValidate: true,
    })
  }

  const handleSwitchToVisual = () => {
    const validation = validateJsonString(form.getValues('rules'), {
      predicate: (parsed) => Array.isArray(parsed),
      predicateMessage: 'Rules JSON must be an array',
    })
    if (!validation.valid) {
      toast.error(t(validation.message || 'Rules JSON is invalid'))
      return
    }
    setRules(parseRulesJson(form.getValues('rules')))
    setMode('visual')
  }

  const handleSwitchToJson = () => {
    form.setValue('rules', rulesToJson(rules), {
      shouldDirty: true,
      shouldValidate: true,
    })
    setMode('json')
  }

  // Dialog field states (not using react-hook-form to avoid lifecycle issues)
  const [dlgName, setDlgName] = useState('')
  const [dlgGroupRegex, setDlgGroupRegex] = useState('')
  const [dlgModelRegex, setDlgModelRegex] = useState('')
  const [dlgPathRegex, setDlgPathRegex] = useState('')
  const [dlgStrict, setDlgStrict] = useState(true)

  const openCreateDialog = () => {
    setEditingRule(null)
    setDlgName('')
    setDlgGroupRegex('')
    setDlgModelRegex('')
    setDlgPathRegex('')
    setDlgStrict(true)
    setEditingTiers([emptyTier()])
    setDialogOpen(true)
  }

  const openEditDialog = (rule: ChannelRouteRule) => {
    setEditingRule(rule)
    setDlgName(rule.name || '')
    setDlgGroupRegex((rule.group_regex || []).join('\n'))
    setDlgModelRegex((rule.model_regex || []).join('\n'))
    setDlgPathRegex((rule.path_regex || []).join('\n'))
    setDlgStrict(rule.strict ?? true)
    setEditingTiers(
      rule.route_tiers?.length
        ? rule.route_tiers.map((t) => ({
            ...t,
            editorId: nextRouteTierEditorId(),
            conditions: t.conditions || [],
            channel_ids: t.channel_ids || [],
            reject: !!t.reject,
            reject_message: t.reject_message || '',
            label: t.label || '',
          }))
        : [emptyTier()]
    )
    setDialogOpen(true)
  }

  const handleDeleteRule = (id: number) => {
    updateRulesState(rules.filter((rule) => rule.id !== id))
    toast.success(t('Deleted successfully'))
  }

  const handleSaveRule = () => {
    // Validate name
    if (!dlgName.trim()) {
      toast.error(t('Rule name is required'))
      return
    }

    const modelRegex = normalizeStringList(dlgModelRegex)
    if (modelRegex.length === 0) {
      toast.error(t('Model regex is required'))
      return
    }

    const validTiers = editingTiers.filter(
      (tier) =>
        tier.reject ||
        tier.channel_ids.length > 0 ||
        tier.conditions.length > 0 ||
        tier.label.trim() ||
        tier.reject_message?.trim()
    )
    if (validTiers.length === 0) {
      toast.error(t('At least one tier must have channel IDs or enable reject'))
      return
    }
    if (validTiers.some((tier) => !tier.reject && tier.channel_ids.length === 0)) {
      toast.error(t('At least one tier must have channel IDs or enable reject'))
      return
    }

    // Validate tier configuration (overlaps, dead tiers, catch-all position)
    if (validTiers.length > 1) {
      const warnings = validateTiers(validTiers)
      if (warnings.length > 0) {
        toast.warning(warnings[0], { duration: 5000 })
        return
      }
    }

    // Auto-generate labels for tiers without labels
    const tiersWithLabels = validTiers.map((tier) => ({
      label:
        tier.label.trim() ||
        autoTierLabel(tier.conditions) ||
        (tier.conditions.length === 0 ? 'catch-all' : ''),
      conditions: tier.conditions,
      channel_ids: tier.reject ? [] : tier.channel_ids,
      reject: !!tier.reject,
      reject_message: tier.reject ? tier.reject_message?.trim() || '' : '',
    }))

    const nextRule: ChannelRouteRule = {
      id: editingRule?.id ?? rules.length,
      name: dlgName.trim(),
      group_regex: normalizeStringList(dlgGroupRegex),
      model_regex: modelRegex,
      path_regex: normalizeStringList(dlgPathRegex),
      strict: dlgStrict,
      route_tiers: tiersWithLabels,
    }

    if (editingRule) {
      updateRulesState(
        rules.map((rule) => (rule.id === editingRule.id ? nextRule : rule))
      )
    } else {
      updateRulesState([...rules, nextRule])
    }

    setDialogOpen(false)
    setEditingRule(null)
    toast.success(t('Saved successfully'))
  }

  const onSubmit = async (values: ChannelRouteFormValues) => {
    const normalizedRules = normalizeJson(values.rules)
    const normalizedDefaultRules = normalizeJson(defaultValues.rules || '[]')
    const updates: Array<{ key: string; value: string | boolean }> = []

    if (values.enabled !== defaultValues.enabled) {
      updates.push({
        key: 'channel_route_setting.enabled',
        value: values.enabled,
      })
    }

    if (normalizedRules !== normalizedDefaultRules) {
      updates.push({
        key: 'channel_route_setting.rules',
        value: normalizedRules,
      })
    }

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
  }

  const tableRows = rules

  return (
    <>
      <SettingsSection
        title={t('Channel Route')}
        description={t(
          'Route requests by group, model, path, token count, or document count, with optional reject tiers.'
        )}
      >
        <Form {...form}>
          <form
            onSubmit={form.handleSubmit(onSubmit)}
            className='space-y-6'
          >
            <FormField
              control={form.control}
              name='enabled'
              render={({ field }) => (
                <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                  <div className='space-y-0.5'>
                    <FormLabel className='text-base'>
                      {t('Enable Channel Route')}
                    </FormLabel>
                    <FormDescription>
                      {t(
                        'Route to specific channels by group, model, path, token count, or document count.'
                      )}
                    </FormDescription>
                  </div>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </FormItem>
              )}
            />

            <Alert>
              <AlertDescription>
                {t(
                  'Suggested rerank ranges: docs ≤ 25 for small batches, 25 < docs ≤ 200 for large batches, and docs > 200 for an optional reject tier. Routing only selects a channel; pricing remains configured separately.'
                )}
              </AlertDescription>
            </Alert>

            <div className='flex flex-wrap items-center gap-2'>
              <Button
                type='button'
                variant={mode === 'visual' ? 'default' : 'outline'}
                onClick={handleSwitchToVisual}
              >
                <Table2 className='mr-2 h-4 w-4' />
                {t('Visual Editor')}
              </Button>
              <Button
                type='button'
                variant={mode === 'json' ? 'default' : 'outline'}
                onClick={handleSwitchToJson}
              >
                <Code className='mr-2 h-4 w-4' />
                JSON
              </Button>
            </div>

            {mode === 'visual' ? (
              <div className='space-y-4'>
                <div className='flex flex-wrap items-center gap-3'>
                  <Button type='button' onClick={openCreateDialog}>
                    <Plus className='mr-2 h-4 w-4' />
                    {t('Add Rule')}
                  </Button>
                  <p className='text-sm text-muted-foreground'>
                    {t(
                      'Match order: group, model, path. Tiers are evaluated in order; first match wins.'
                    )}
                  </p>
                </div>

                {tableRows.length === 0 ? (
                  <div className='rounded-lg border border-dashed p-6 text-sm text-muted-foreground'>
                    {t('No rules configured')}
                  </div>
                ) : (
                  <div className='space-y-3 rounded-lg border'>
                    {tableRows.map((rule) => (
                      <div
                        key={rule.id}
                        className='border-b p-4 last:border-b-0'
                      >
                        <div className='flex items-start justify-between gap-4'>
                          <div className='space-y-2'>
                            <div className='flex items-center gap-2'>
                              <div className='font-medium'>
                                {rule.name || '-'}
                              </div>
                              <span className='rounded-full border px-2 py-0.5 text-xs text-muted-foreground'>
                                {rule.strict ? t('Strict') : t('Fallback Allowed')}
                              </span>
                            </div>
                            <div className='grid gap-2 text-sm text-muted-foreground md:grid-cols-2'>
                              <div>
                                <span className='font-medium text-foreground'>
                                  {t('Group Regex')}:
                                </span>{' '}
                                {(rule.group_regex || []).join(', ') || '-'}
                              </div>
                              <div>
                                <span className='font-medium text-foreground'>
                                  {t('Model Regex')}:
                                </span>{' '}
                                {(rule.model_regex || []).join(', ') || '-'}
                              </div>
                              <div>
                                <span className='font-medium text-foreground'>
                                  {t('Path Regex')}:
                                </span>{' '}
                                {(rule.path_regex || []).join(', ') || '-'}
                              </div>
                            </div>
                            {rule.route_tiers && rule.route_tiers.length > 0 && (
                              <div className='mt-2 space-y-1'>
                                <div className='text-sm font-medium text-foreground'>
                                  {t('Route Tiers')}:
                                </div>
                                {rule.route_tiers.map((tier, i) => (
                                  <div
                                    key={JSON.stringify(tier)}
                                    className='flex items-center gap-2 text-sm text-muted-foreground'
                                  >
                                    <span className='rounded bg-muted px-1.5 py-0.5 text-xs font-medium'>
                                      {tier.label || autoTierLabel(tier.conditions || []) || `Tier ${i + 1}`}
                                    </span>
                                    {tier.conditions && tier.conditions.length > 0 && (
                                      <span>
                                        {tier.conditions
                                          .map(
                                            (c) =>
                                              `${c.var} ${c.op} ${c.value.toLocaleString()}`
                                          )
                                          .join(' AND ')}
                                      </span>
                                    )}
                                    {!tier.conditions?.length &&
                                      i === (rule.route_tiers?.length ?? 0) - 1 && (
                                        <span className='text-xs italic'>
                                          {t('catch-all')}
                                        </span>
                                      )}
                                    <span className='text-muted-foreground'>
                                      →
                                    </span>
                                    {tier.reject ? (
                                      <span className='flex items-center gap-2'>
                                        <Badge variant='destructive'>
                                          {t('Reject')}
                                        </Badge>
                                        {tier.reject_message && (
                                          <span className='text-muted-foreground text-xs'>
                                            “{tier.reject_message}”
                                          </span>
                                        )}
                                      </span>
                                    ) : (
                                      <span>
                                        {(tier.channel_ids || [])
                                          .map((id) => getChannelName(id))
                                          .join(', ')}
                                      </span>
                                    )}
                                  </div>
                                ))}
                              </div>
                            )}
                          </div>
                          <div className='flex gap-2'>
                            <Button
                              type='button'
                              variant='outline'
                              size='icon'
                              onClick={() => openEditDialog(rule)}
                            >
                              <Pencil className='h-4 w-4' />
                            </Button>
                            <Button
                              type='button'
                              variant='outline'
                              size='icon'
                              onClick={() => handleDeleteRule(rule.id)}
                            >
                              <Trash2 className='h-4 w-4' />
                            </Button>
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            ) : (
              <FormField
                control={form.control}
                name='rules'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Route Rules')}</FormLabel>
                    <FormControl>
                      <Textarea
                        rows={18}
                        placeholder={rulesExample}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Fields: name, group_regex, model_regex, path_regex, route_tiers, strict.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            <Button type='submit' disabled={updateOption.isPending}>
              {updateOption.isPending
                ? t('Saving...')
                : t('Save channel route settings')}
            </Button>
          </form>
        </Form>
      </SettingsSection>

      <Dialog
        open={dialogOpen}
        onOpenChange={(open) => {
          setDialogOpen(open)
          if (!open) setEditingRule(null)
        }}
      >
        <DialogContent className='max-w-3xl'>
          <DialogHeader>
            <DialogTitle>
              {editingRule ? t('Edit Rule') : t('Add Rule')}
            </DialogTitle>
            <DialogDescription>
              {t(
                'Configure ordered routing tiers based on token or document count, including tiers that reject matching requests.'
              )}
            </DialogDescription>
          </DialogHeader>

          <div className='space-y-4'>
            <div className='space-y-2'>
              <Label className='text-sm font-medium'>{t('Rule Name')}</Label>
              <Input
                value={dlgName}
                onChange={(e) => setDlgName(e.target.value)}
                placeholder={t('e.g. qwen messages native')}
              />
            </div>

            <div className='grid gap-4 md:grid-cols-3'>
              <GroupSelector
                value={dlgGroupRegex}
                onChange={setDlgGroupRegex}
              />
              <ModelNameMatcher
                value={dlgModelRegex}
                onChange={setDlgModelRegex}
              />
              <PathSelector
                value={dlgPathRegex}
                onChange={setDlgPathRegex}
              />
            </div>

            {/* Visual tier editor */}
            <div className='space-y-2'>
              <Label className='text-sm font-medium'>
                {t('Route Tiers')}
              </Label>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Each tier supports 0~2 conditions on len/p/c/docs (AND logic). A tier with no conditions is the catch-all; reject tiers return an error without routing.'
                )}
              </p>
              <RouteTierEditor
                tiers={editingTiers}
                onChange={setEditingTiers}
              />
            </div>

            <div className='flex flex-row items-center justify-between rounded-lg border p-4'>
              <div className='space-y-0.5'>
                <Label className='text-base'>{t('Strict Mode')}</Label>
                <p className='text-muted-foreground text-sm'>
                  {t(
                    'When enabled, a matching rule with no available channel fails instead of falling back. Reject tiers always return an error.'
                  )}
                </p>
              </div>
              <Switch
                checked={dlgStrict}
                onCheckedChange={setDlgStrict}
              />
            </div>
          </div>

          <DialogFooter>
            <Button
              type='button'
              variant='outline'
              onClick={() => setDialogOpen(false)}
            >
              {t('Cancel')}
            </Button>
            <Button type='button' onClick={handleSaveRule}>
              <Check className='mr-2 h-4 w-4' />
              {t('Save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
