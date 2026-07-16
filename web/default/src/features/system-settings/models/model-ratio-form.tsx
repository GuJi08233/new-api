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
import { Code2, Eye, RotateCcw, Save } from 'lucide-react'
import { memo, useCallback, useEffect, useRef, useState } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { JsonCodeEditor } from '@/components/json-code-editor'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'
import { getEnabledModels } from '@/features/channels/api'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import {
  ModelRatioVisualEditor,
  type ModelRatioVisualEditorHandle,
} from './model-ratio-visual-editor'

type ModelFormValues = {
  ModelPrice: string
  ModelRatio: string
  CacheRatio: string
  CreateCacheRatio: string
  CompletionRatio: string
  ImageRatio: string
  AudioRatio: string
  AudioCompletionRatio: string
  ExposeRatioEnabled: boolean
  BillingMode: string
  BillingExpr: string
}

type ModelRatioFormProps = {
  form: UseFormReturn<ModelFormValues>
  savedValues: ModelFormValues
  onSave: (values: ModelFormValues) => Promise<void>
  onReset: () => void
  isSaving: boolean
  isResetting: boolean
  // 分组定价相关 props
  selectedGroup?: string
  onGroupChange?: (group: string) => void
  availableGroups?: string[]
  onSyncComplete?: () => void
}

export const ModelRatioForm = memo(function ModelRatioForm({
  form,
  savedValues,
  onSave,
  onReset,
  isSaving,
  isResetting,
  selectedGroup = 'global',
  onGroupChange,
  availableGroups = [],
  onSyncComplete,
}: ModelRatioFormProps) {
  const { t } = useTranslation()
  const isUnsetVariant = variant === 'unset'
  const [editMode, setEditMode] = useState<'visual' | 'json'>('visual')
  const visualEditorRef = useRef<ModelRatioVisualEditorHandle>(null)

  const enabledModelsQuery = useQuery({
    queryKey: ['enabled-models'],
    queryFn: getEnabledModels,
    enabled: isUnsetVariant,
  })

  const enabledModelsError = isUnsetVariant
    ? enabledModelsQuery.isError ||
      (enabledModelsQuery.data !== undefined &&
        !enabledModelsQuery.data.success)
    : false
  const enabledModelsErrorMessage = enabledModelsQuery.data?.message

  useEffect(() => {
    if (!enabledModelsError) return
    toast.error(enabledModelsErrorMessage || t('Failed to load enabled models'))
  }, [enabledModelsError, enabledModelsErrorMessage, t])

  const handleFieldChange = useCallback(
    (field: keyof ModelFormValues, value: string) => {
      form.setValue(field, value, {
        shouldValidate: true,
        shouldDirty: true,
      })
    },
    [form]
  )

  const toggleEditMode = useCallback(() => {
    setEditMode((prev) => (prev === 'visual' ? 'json' : 'visual'))
  }, [])

  const handleSave = useCallback(async () => {
    if (editMode === 'visual') {
      const committed = await visualEditorRef.current?.commitOpenEditor()
      if (committed === false) return
    }

    await form.handleSubmit(onSave)()
  }, [editMode, form, onSave])

  return (
    <div className='space-y-6'>
      {!isUnsetVariant && (
        <div className='flex flex-wrap justify-end gap-2'>
          <Button
            type='button'
            variant='destructive'
            size='sm'
            onClick={onReset}
            disabled={isResetting}
          >
            <RotateCcw data-icon='inline-start' />
            {t('Reset prices')}
          </Button>
          {editMode === 'json' && (
            <Button
              type='button'
              size='sm'
              onClick={handleSave}
              disabled={isSaving}
            >
              <Save data-icon='inline-start' />
              {isSaving ? t('Saving...') : t('Save model prices')}
            </Button>
          )}
          <Button variant='outline' size='sm' onClick={toggleEditMode}>
            {editMode === 'visual' ? (
              <>
                <Code2 className='mr-2 h-4 w-4' />
                {t('Switch to JSON')}
              </>
            ) : (
              <>
                <Eye className='mr-2 h-4 w-4' />
                {t('Switch to Visual')}
              </>
            )}
          </Button>
        </div>
      )}

      <Form {...form}>
        {editMode === 'visual' ? (
          <div className='space-y-6'>
            <ModelRatioVisualEditor
              ref={visualEditorRef}
              savedModelPrice={savedValues.ModelPrice}
              savedModelRatio={savedValues.ModelRatio}
              savedCacheRatio={savedValues.CacheRatio}
              savedCreateCacheRatio={savedValues.CreateCacheRatio}
              savedCompletionRatio={savedValues.CompletionRatio}
              savedImageRatio={savedValues.ImageRatio}
              savedAudioRatio={savedValues.AudioRatio}
              savedAudioCompletionRatio={savedValues.AudioCompletionRatio}
              savedBillingMode={savedValues.BillingMode}
              savedBillingExpr={savedValues.BillingExpr}
              modelPrice={form.watch('ModelPrice')}
              modelRatio={form.watch('ModelRatio')}
              cacheRatio={form.watch('CacheRatio')}
              createCacheRatio={form.watch('CreateCacheRatio')}
              completionRatio={form.watch('CompletionRatio')}
              imageRatio={form.watch('ImageRatio')}
              audioRatio={form.watch('AudioRatio')}
              audioCompletionRatio={form.watch('AudioCompletionRatio')}
              billingMode={form.watch('BillingMode')}
              billingExpr={form.watch('BillingExpr')}
              candidateModelNames={
                isUnsetVariant ? enabledModelsQuery.data?.data : undefined
              }
              candidateModelsLoading={
                isUnsetVariant && enabledModelsQuery.isLoading
              }
              filterMode={isUnsetVariant ? 'unset' : 'all'}
              onSave={handleSave}
              isSaving={isSaving}
              onChange={(field, value) => {
                const fieldMap: Record<string, keyof ModelFormValues> = {
                  'billing_setting.billing_mode': 'BillingMode',
                  'billing_setting.billing_expr': 'BillingExpr',
                }
                const formField =
                  fieldMap[field] || (field as keyof ModelFormValues)
                handleFieldChange(formField, value)
              }}
              selectedGroup={selectedGroup}
              onGroupChange={onGroupChange}
              availableGroups={availableGroups}
              onSyncComplete={onSyncComplete}
            />

            {!isUnsetVariant && (
              <FormField
                control={form.control}
                name='ExposeRatioEnabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Expose ratio API')}</FormLabel>
                      <FormDescription>
                        {t(
                          'Allow clients to query configured ratios via `/api/ratio`.'
                        )}
                      </FormDescription>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />
            )}
          </div>
        ) : (
          <SettingsForm onSubmit={form.handleSubmit(onSave)}>
            <div className='grid min-w-0 gap-x-5 gap-y-8 lg:grid-cols-2 2xl:grid-cols-3'>
              {modelJsonFields.map((config) => (
                <ModelJsonTextareaField
                  key={config.name}
                  form={form}
                  name={config.name}
                  label={t(config.labelKey)}
                  description={t(config.descriptionKey)}
                />
              ))}
            </div>

            <FormField
              control={form.control}
              name='ExposeRatioEnabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Expose ratio API')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Allow clients to query configured ratios via `/api/ratio`.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
          </SettingsForm>
        )}
      </Form>
    </div>
  )
})
