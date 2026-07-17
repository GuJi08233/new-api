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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { resetModelRatios } from '../api'
import { SettingsPageTitleStatusPortal } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { GroupRatioForm } from './group-ratio-form'
import { ModelRatioForm } from './model-ratio-form'
import { ToolPriceSettings } from './tool-price-settings'
import { UpstreamRatioSync } from './upstream-ratio-sync'
import {
  formatJsonForTextarea,
  type JsonValidationError,
  normalizeJsonString,
  validateJsonString,
} from './utils'

type Translate = (key: string, options?: Record<string, unknown>) => string

function formatJsonValidationError(
  t: Translate,
  error?: JsonValidationError,
  fallback = 'Invalid JSON'
) {
  if (!error) return t(fallback)

  if (error.type === 'required') return t('Value is required')
  if (error.type === 'structure') {
    return t(
      fallback === 'Invalid JSON' ? 'JSON structure is invalid' : fallback
    )
  }

  let locationMessage: string
  if (error.line && error.column) {
    locationMessage = t(
      'JSON is invalid at line {{line}}, column {{column}}.',
      {
        line: error.line,
        column: error.column,
      }
    )
  } else if (error.position !== undefined) {
    locationMessage = t('JSON is invalid at position {{position}}.', {
      position: error.position,
    })
  } else {
    locationMessage = t('JSON is invalid. Please check the syntax.')
  }

  const parts = [locationMessage]

  if (error.missingCommaLine) {
    parts.push(
      t('Check line {{line}} for a missing comma.', {
        line: error.missingCommaLine,
      })
    )
  }

  return parts.join(' ')
}

function createJsonStringField(
  t: Translate,
  options?: Parameters<typeof validateJsonString>[1]
) {
  return z.string().superRefine((value, ctx) => {
    const result = validateJsonString(value, options)
    if (!result.valid) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: formatJsonValidationError(t, result.error, result.message),
      })
    }
  })
}

const createModelSchema = (t: Translate) =>
  z.object({
    ModelPrice: createJsonStringField(t),
    ModelRatio: createJsonStringField(t),
    CacheRatio: createJsonStringField(t),
    CreateCacheRatio: createJsonStringField(t),
    CompletionRatio: createJsonStringField(t),
    ImageRatio: createJsonStringField(t),
    AudioRatio: createJsonStringField(t),
    AudioCompletionRatio: createJsonStringField(t),
    ExposeRatioEnabled: z.boolean(),
    BillingMode: createJsonStringField(t),
    BillingExpr: createJsonStringField(t),
  })

const createGroupSchema = (t: Translate) =>
  z.object({
    GroupRatio: createJsonStringField(t),
    TopupGroupRatio: createJsonStringField(t),
    UserUsableGroups: createJsonStringField(t),
    GroupGroupRatio: createJsonStringField(t),
    AutoGroups: createJsonStringField(t, {
      predicate: (parsed) =>
        Array.isArray(parsed) &&
        parsed.every((item) => typeof item === 'string'),
      predicateMessage: 'Expected a JSON array of group identifiers',
    }),
    DefaultUseAutoGroup: z.boolean(),
    GroupSpecialUsableGroup: createJsonStringField(t),
  })

type ModelFormValues = z.infer<ReturnType<typeof createModelSchema>>
type GroupFormValues = z.infer<ReturnType<typeof createGroupSchema>>

type GroupModelDefaults = {
  GroupModelPrice: string
  GroupModelRatio: string
  GroupCompletionRatio: string
  GroupCacheRatio: string
  GroupCreateCacheRatio: string
  GroupImageRatio: string
  GroupAudioRatio: string
  GroupAudioCompletionRatio: string
  GroupBillingMode: string
  GroupBillingExpr: string
}

const GROUP_MODEL_FIELD_MAP = {
  ModelPrice: 'GroupModelPrice',
  ModelRatio: 'GroupModelRatio',
  CacheRatio: 'GroupCacheRatio',
  CreateCacheRatio: 'GroupCreateCacheRatio',
  CompletionRatio: 'GroupCompletionRatio',
  ImageRatio: 'GroupImageRatio',
  AudioRatio: 'GroupAudioRatio',
  AudioCompletionRatio: 'GroupAudioCompletionRatio',
  BillingMode: 'GroupBillingMode',
  BillingExpr: 'GroupBillingExpr',
} as const

type GroupModelField = keyof typeof GROUP_MODEL_FIELD_MAP
type GroupModelOptionKey = (typeof GROUP_MODEL_FIELD_MAP)[GroupModelField]

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function parseFlatMap<T = unknown>(value: string): Record<string, T> {
  if (!value?.trim()) return {}
  try {
    const parsed = JSON.parse(value)
    return isPlainObject(parsed) ? (parsed as Record<string, T>) : {}
  } catch {
    return {}
  }
}

function parseNestedMap<T = unknown>(
  value: string
): Record<string, Record<string, T>> {
  const parsed = parseFlatMap<unknown>(value)
  const result: Record<string, Record<string, T>> = {}
  for (const [group, groupValue] of Object.entries(parsed)) {
    if (isPlainObject(groupValue)) {
      result[group] = groupValue as Record<string, T>
    }
  }
  return result
}

function extractGroupModelFormValue(
  groupModelDefaults: GroupModelDefaults,
  optionKey: GroupModelOptionKey,
  selectedGroup: string
): string {
  const groupValues = parseNestedMap(groupModelDefaults[optionKey])
  return formatJsonForTextarea(
    JSON.stringify(groupValues[selectedGroup] ?? {})
  )
}

function buildModelFormDefaults(
  modelDefaults: ModelFormValues,
  groupModelDefaults: GroupModelDefaults,
  selectedGroup: string
): ModelFormValues {
  if (selectedGroup === 'global') {
    return {
      ...modelDefaults,
      ModelPrice: formatJsonForTextarea(modelDefaults.ModelPrice),
      ModelRatio: formatJsonForTextarea(modelDefaults.ModelRatio),
      CacheRatio: formatJsonForTextarea(modelDefaults.CacheRatio),
      CreateCacheRatio: formatJsonForTextarea(modelDefaults.CreateCacheRatio),
      CompletionRatio: formatJsonForTextarea(modelDefaults.CompletionRatio),
      ImageRatio: formatJsonForTextarea(modelDefaults.ImageRatio),
      AudioRatio: formatJsonForTextarea(modelDefaults.AudioRatio),
      AudioCompletionRatio: formatJsonForTextarea(
        modelDefaults.AudioCompletionRatio
      ),
      BillingMode: formatJsonForTextarea(modelDefaults.BillingMode),
      BillingExpr: formatJsonForTextarea(modelDefaults.BillingExpr),
    }
  }

  return {
    ModelPrice: extractGroupModelFormValue(
      groupModelDefaults,
      'GroupModelPrice',
      selectedGroup
    ),
    ModelRatio: extractGroupModelFormValue(
      groupModelDefaults,
      'GroupModelRatio',
      selectedGroup
    ),
    CacheRatio: extractGroupModelFormValue(
      groupModelDefaults,
      'GroupCacheRatio',
      selectedGroup
    ),
    CreateCacheRatio: extractGroupModelFormValue(
      groupModelDefaults,
      'GroupCreateCacheRatio',
      selectedGroup
    ),
    CompletionRatio: extractGroupModelFormValue(
      groupModelDefaults,
      'GroupCompletionRatio',
      selectedGroup
    ),
    ImageRatio: extractGroupModelFormValue(
      groupModelDefaults,
      'GroupImageRatio',
      selectedGroup
    ),
    AudioRatio: extractGroupModelFormValue(
      groupModelDefaults,
      'GroupAudioRatio',
      selectedGroup
    ),
    AudioCompletionRatio: extractGroupModelFormValue(
      groupModelDefaults,
      'GroupAudioCompletionRatio',
      selectedGroup
    ),
    ExposeRatioEnabled: modelDefaults.ExposeRatioEnabled,
    BillingMode: extractGroupModelFormValue(
      groupModelDefaults,
      'GroupBillingMode',
      selectedGroup
    ),
    BillingExpr: extractGroupModelFormValue(
      groupModelDefaults,
      'GroupBillingExpr',
      selectedGroup
    ),
  }
}

function normalizeModelFormValues(values: ModelFormValues): ModelFormValues {
  return {
    ModelPrice: normalizeJsonString(values.ModelPrice),
    ModelRatio: normalizeJsonString(values.ModelRatio),
    CacheRatio: normalizeJsonString(values.CacheRatio),
    CreateCacheRatio: normalizeJsonString(values.CreateCacheRatio),
    CompletionRatio: normalizeJsonString(values.CompletionRatio),
    ImageRatio: normalizeJsonString(values.ImageRatio),
    AudioRatio: normalizeJsonString(values.AudioRatio),
    AudioCompletionRatio: normalizeJsonString(values.AudioCompletionRatio),
    ExposeRatioEnabled: values.ExposeRatioEnabled,
    BillingMode: normalizeJsonString(values.BillingMode),
    BillingExpr: normalizeJsonString(values.BillingExpr),
  }
}

function buildDerivedGroupBillingMode(
  values: ModelFormValues
): Record<string, string> {
  const explicitModeMap = parseFlatMap<string>(values.BillingMode)
  const billingExprMap = parseFlatMap<string>(values.BillingExpr)
  const priceMap = parseFlatMap<number>(values.ModelPrice)
  const ratioMaps = [
    parseFlatMap<number>(values.ModelRatio),
    parseFlatMap<number>(values.CacheRatio),
    parseFlatMap<number>(values.CreateCacheRatio),
    parseFlatMap<number>(values.CompletionRatio),
    parseFlatMap<number>(values.ImageRatio),
    parseFlatMap<number>(values.AudioRatio),
    parseFlatMap<number>(values.AudioCompletionRatio),
  ]
  const modelNames = new Set([
    ...Object.keys(explicitModeMap),
    ...Object.keys(billingExprMap),
    ...Object.keys(priceMap),
    ...ratioMaps.flatMap((ratioMap) => Object.keys(ratioMap)),
  ])

  const result: Record<string, string> = {}
  for (const modelName of modelNames) {
    if (billingExprMap[modelName]) {
      result[modelName] = 'tiered_expr'
    } else if (
      explicitModeMap[modelName] === 'per-request' ||
      explicitModeMap[modelName] === 'per-token'
    ) {
      result[modelName] = explicitModeMap[modelName]
    } else {
      result[modelName] = modelName in priceMap ? 'per-request' : 'per-token'
    }
  }
  return result
}

function mergeGroupModelDefaults(
  values: ModelFormValues,
  groupModelDefaults: GroupModelDefaults,
  selectedGroup: string
): GroupModelDefaults {
  const leafValues: Record<GroupModelOptionKey, Record<string, unknown>> = {
    GroupModelPrice: parseFlatMap(values.ModelPrice),
    GroupModelRatio: parseFlatMap(values.ModelRatio),
    GroupCacheRatio: parseFlatMap(values.CacheRatio),
    GroupCreateCacheRatio: parseFlatMap(values.CreateCacheRatio),
    GroupCompletionRatio: parseFlatMap(values.CompletionRatio),
    GroupImageRatio: parseFlatMap(values.ImageRatio),
    GroupAudioRatio: parseFlatMap(values.AudioRatio),
    GroupAudioCompletionRatio: parseFlatMap(values.AudioCompletionRatio),
    GroupBillingMode: buildDerivedGroupBillingMode(values),
    GroupBillingExpr: parseFlatMap(values.BillingExpr),
  }
  const result = {} as GroupModelDefaults

  for (const optionKey of Object.values(GROUP_MODEL_FIELD_MAP)) {
    const merged = parseNestedMap(groupModelDefaults[optionKey])
    const groupLeaf = leafValues[optionKey]
    if (Object.keys(groupLeaf).length === 0) {
      delete merged[selectedGroup]
    } else {
      merged[selectedGroup] = groupLeaf
    }
    result[optionKey] = JSON.stringify(merged)
  }

  return result
}

type RatioTabId =
  | 'models'
  | 'unset-models'
  | 'groups'
  | 'tool-prices'
  | 'upstream-sync'

type RatioSettingsCardProps = {
  modelDefaults: ModelFormValues
  groupModelDefaults: GroupModelDefaults
  groupDefaults: GroupFormValues
  toolPricesDefault: string
  titleKey?: string
  visibleTabs?: RatioTabId[]
}

export function RatioSettingsCard({
  modelDefaults,
  groupModelDefaults,
  groupDefaults,
  toolPricesDefault,
  titleKey = 'Pricing Ratios',
  visibleTabs = ['models', 'groups', 'tool-prices', 'upstream-sync'],
}: RatioSettingsCardProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const queryClient = useQueryClient()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [selectedGroup, setSelectedGroup] = useState('global')
  const [activeTab, setActiveTab] = useState<RatioTabId>(
    visibleTabs[0] ?? 'models'
  )

  const availableGroups = useMemo(
    () => [
      'global',
      ...Object.keys(parseFlatMap(groupDefaults.GroupRatio)).filter(
        (group) => group !== 'global'
      ),
    ],
    [groupDefaults.GroupRatio]
  )

  const resetMutation = useMutation({
    mutationFn: resetModelRatios,
    onSuccess: (data) => {
      if (data.success) {
        toast.success(t('Model prices reset successfully'))
        queryClient.invalidateQueries({ queryKey: ['system-options'] })
        setConfirmOpen(false)
      } else {
        toast.error(data.message || t('Failed to reset model ratios'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to reset model ratios'))
    },
  })

  const modelNormalizedDefaults = useRef(
    normalizeModelFormValues(
      buildModelFormDefaults(modelDefaults, groupModelDefaults, selectedGroup)
    )
  )
  const [savedModelValues, setSavedModelValues] = useState(
    modelNormalizedDefaults.current
  )

  const groupNormalizedDefaults = useRef({
    GroupRatio: normalizeJsonString(groupDefaults.GroupRatio),
    TopupGroupRatio: normalizeJsonString(groupDefaults.TopupGroupRatio),
    UserUsableGroups: normalizeJsonString(groupDefaults.UserUsableGroups),
    GroupGroupRatio: normalizeJsonString(groupDefaults.GroupGroupRatio),
    AutoGroups: normalizeJsonString(groupDefaults.AutoGroups),
    DefaultUseAutoGroup: groupDefaults.DefaultUseAutoGroup,
    GroupSpecialUsableGroup: normalizeJsonString(
      groupDefaults.GroupSpecialUsableGroup
    ),
  })
  const modelSchema = useMemo(() => createModelSchema(t), [t])
  const groupSchema = useMemo(() => createGroupSchema(t), [t])

  const modelForm = useForm<ModelFormValues>({
    resolver: zodResolver(modelSchema),
    mode: 'onChange',
    defaultValues: buildModelFormDefaults(
      modelDefaults,
      groupModelDefaults,
      selectedGroup
    ),
  })

  const groupForm = useForm<GroupFormValues>({
    resolver: zodResolver(groupSchema),
    mode: 'onChange',
    defaultValues: {
      ...groupDefaults,
      GroupRatio: formatJsonForTextarea(groupDefaults.GroupRatio),
      TopupGroupRatio: formatJsonForTextarea(groupDefaults.TopupGroupRatio),
      UserUsableGroups: formatJsonForTextarea(groupDefaults.UserUsableGroups),
      GroupGroupRatio: formatJsonForTextarea(groupDefaults.GroupGroupRatio),
      AutoGroups: formatJsonForTextarea(groupDefaults.AutoGroups),
      GroupSpecialUsableGroup: formatJsonForTextarea(
        groupDefaults.GroupSpecialUsableGroup
      ),
    },
  })

  useEffect(() => {
    const nextDefaults = buildModelFormDefaults(
      modelDefaults,
      groupModelDefaults,
      selectedGroup
    )
    modelNormalizedDefaults.current = normalizeModelFormValues(nextDefaults)
    setSavedModelValues(modelNormalizedDefaults.current)
    modelForm.reset(nextDefaults)
  }, [groupModelDefaults, modelDefaults, modelForm, selectedGroup])

  useEffect(() => {
    groupNormalizedDefaults.current = {
      GroupRatio: normalizeJsonString(groupDefaults.GroupRatio),
      TopupGroupRatio: normalizeJsonString(groupDefaults.TopupGroupRatio),
      UserUsableGroups: normalizeJsonString(groupDefaults.UserUsableGroups),
      GroupGroupRatio: normalizeJsonString(groupDefaults.GroupGroupRatio),
      AutoGroups: normalizeJsonString(groupDefaults.AutoGroups),
      DefaultUseAutoGroup: groupDefaults.DefaultUseAutoGroup,
      GroupSpecialUsableGroup: normalizeJsonString(
        groupDefaults.GroupSpecialUsableGroup
      ),
    }

    groupForm.reset({
      ...groupDefaults,
      GroupRatio: formatJsonForTextarea(groupDefaults.GroupRatio),
      TopupGroupRatio: formatJsonForTextarea(groupDefaults.TopupGroupRatio),
      UserUsableGroups: formatJsonForTextarea(groupDefaults.UserUsableGroups),
      GroupGroupRatio: formatJsonForTextarea(groupDefaults.GroupGroupRatio),
      AutoGroups: formatJsonForTextarea(groupDefaults.AutoGroups),
      GroupSpecialUsableGroup: formatJsonForTextarea(
        groupDefaults.GroupSpecialUsableGroup
      ),
    })
  }, [groupDefaults, groupForm])

  const saveModelRatios = useCallback(
    async (values: ModelFormValues) => {
      const normalized = normalizeModelFormValues(values)

      if (selectedGroup === 'global') {
        const apiKeyMap: Partial<Record<keyof ModelFormValues, string>> = {
          BillingMode: 'billing_setting.billing_mode',
          BillingExpr: 'billing_setting.billing_expr',
        }
        const updates = (
          Object.keys(normalized) as Array<keyof ModelFormValues>
        ).filter(
          (key) => normalized[key] !== modelNormalizedDefaults.current[key]
        )

        if (updates.length === 0) {
          toast.info(t('No model price changes to save'))
          return
        }

        for (const key of updates) {
          await updateOption.mutateAsync({
            key: apiKeyMap[key] ?? key,
            value: normalized[key],
          })
        }
      } else {
        const mergedGroupDefaults = mergeGroupModelDefaults(
          normalized,
          groupModelDefaults,
          selectedGroup
        )
        const updates = (
          Object.keys(mergedGroupDefaults) as GroupModelOptionKey[]
        ).filter(
          (key) =>
            normalizeJsonString(mergedGroupDefaults[key]) !==
            normalizeJsonString(groupModelDefaults[key])
        )

        if (updates.length === 0) {
          toast.info(t('No model price changes to save'))
          return
        }

        for (const key of updates) {
          await updateOption.mutateAsync({
            key,
            value: mergedGroupDefaults[key],
          })
        }
      }

      modelNormalizedDefaults.current = normalized
      setSavedModelValues(normalized)
    },
    [groupModelDefaults, selectedGroup, t, updateOption]
  )

  const saveGroupRatios = useCallback(
    async (values: GroupFormValues) => {
      const normalized = {
        GroupRatio: normalizeJsonString(values.GroupRatio),
        TopupGroupRatio: normalizeJsonString(values.TopupGroupRatio),
        UserUsableGroups: normalizeJsonString(values.UserUsableGroups),
        GroupGroupRatio: normalizeJsonString(values.GroupGroupRatio),
        AutoGroups: normalizeJsonString(values.AutoGroups),
        DefaultUseAutoGroup: values.DefaultUseAutoGroup,
        GroupSpecialUsableGroup: normalizeJsonString(
          values.GroupSpecialUsableGroup
        ),
      }

      // Map form field names to API keys (most are 1:1, except GroupSpecialUsableGroup)
      const apiKeyMap: Record<string, string> = {
        GroupSpecialUsableGroup:
          'group_ratio_setting.group_special_usable_group',
      }

      const updates = (
        Object.keys(normalized) as Array<keyof typeof normalized>
      ).filter(
        (key) => normalized[key] !== groupNormalizedDefaults.current[key]
      )

      for (const key of updates) {
        const apiKey = apiKeyMap[key] || key
        await updateOption.mutateAsync({ key: apiKey, value: normalized[key] })
      }
    },
    [updateOption]
  )

  const handleResetRatios = useCallback(() => {
    setConfirmOpen(true)
  }, [])

  const { mutate: resetMutate } = resetMutation
  const handleConfirmReset = useCallback(() => {
    resetMutate()
  }, [resetMutate])

  const handleGroupSyncComplete = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ['system-options'] })
  }, [queryClient])

  const handleTabChange = useCallback((tab: string | null) => {
    if (!tab) return
    if (tab === 'unset-models') setSelectedGroup('global')
    setActiveTab(tab as RatioTabId)
  }, [])

  const tabLabels: Record<RatioTabId, string> = {
    models: 'Model prices',
    'unset-models': 'Unset price models',
    groups: 'Group ratios',
    'tool-prices': 'Tool prices',
    'upstream-sync': 'Upstream price sync',
  }
  const tabsGridClass =
    {
      1: 'grid-cols-1',
      2: 'grid-cols-2',
      3: 'grid-cols-3',
      4: 'grid-cols-4',
      5: 'grid-cols-5',
    }[visibleTabs.length] ?? 'grid-cols-4'
  const defaultTab = visibleTabs[0] ?? 'models'

  const renderTabContent = (tab: RatioTabId) => {
    if (tab === 'models' || tab === 'unset-models') {
      return (
        <ModelRatioForm
          form={modelForm}
          savedValues={savedModelValues}
          onSave={saveModelRatios}
          onReset={handleResetRatios}
          isSaving={updateOption.isPending}
          isResetting={resetMutation.isPending}
          variant={tab === 'unset-models' ? 'unset' : 'default'}
          selectedGroup={selectedGroup}
          onGroupChange={setSelectedGroup}
          availableGroups={availableGroups}
          onSyncComplete={handleGroupSyncComplete}
        />
      )
    }
    if (tab === 'groups') {
      return (
        <GroupRatioForm
          form={groupForm}
          onSave={saveGroupRatios}
          isSaving={updateOption.isPending}
        />
      )
    }
    if (tab === 'tool-prices') {
      return <ToolPriceSettings defaultValue={toolPricesDefault} />
    }
    return (
      <UpstreamRatioSync
        modelRatios={{
          ModelPrice: modelDefaults.ModelPrice,
          ModelRatio: modelDefaults.ModelRatio,
          CompletionRatio: modelDefaults.CompletionRatio,
          CacheRatio: modelDefaults.CacheRatio,
          CreateCacheRatio: modelDefaults.CreateCacheRatio,
          ImageRatio: modelDefaults.ImageRatio,
          AudioRatio: modelDefaults.AudioRatio,
          AudioCompletionRatio: modelDefaults.AudioCompletionRatio,
          'billing_setting.billing_mode': modelDefaults.BillingMode,
          'billing_setting.billing_expr': modelDefaults.BillingExpr,
        }}
      />
    )
  }

  const renderTabSwitcher = () => (
    <TabsList className={`grid w-fit max-w-full ${tabsGridClass}`}>
      {visibleTabs.map((tab) => (
        <TabsTrigger key={tab} value={tab}>
          {t(tabLabels[tab])}
        </TabsTrigger>
      ))}
    </TabsList>
  )

  return (
    <>
      {visibleTabs.length === 1 ? (
        <SettingsSection title={t(titleKey)}>
          {renderTabContent(defaultTab)}
        </SettingsSection>
      ) : (
        <Tabs
          value={activeTab}
          onValueChange={handleTabChange}
          className='h-full min-h-0 gap-6'
        >
          <SettingsPageTitleStatusPortal>
            {renderTabSwitcher()}
          </SettingsPageTitleStatusPortal>

          <SettingsSection title={t(titleKey)} className='min-h-0 flex-1'>
            {visibleTabs.map((tab) => (
              <TabsContent key={tab} value={tab} className='min-h-0'>
                {renderTabContent(tab)}
              </TabsContent>
            ))}
          </SettingsSection>
        </Tabs>
      )}

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Reset all model prices?')}
        desc={t(
          'This will clear custom pricing ratios and revert to upstream defaults.'
        )}
        destructive
        isLoading={resetMutation.isPending}
        handleConfirm={handleConfirmReset}
        confirmText={t('Reset')}
      />
    </>
  )
}
