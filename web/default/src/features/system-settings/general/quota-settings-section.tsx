import * as z from 'zod'
import type { Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
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
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import { QuotaAmountFieldPair } from '../components/quota-amount-field-pair'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { quotaUnitsToDollars } from '@/lib/format'

const quotaSchema = z.object({
  QuotaForNewUser: z.coerce.number().min(0),
  QuotaForNewUserAmount: z.coerce.number().min(0),
  PreConsumedQuota: z.coerce.number().min(0),
  PreConsumedQuotaAmount: z.coerce.number().min(0),
  QuotaForInvitee: z.coerce.number().min(0),
  QuotaForInviteeAmount: z.coerce.number().min(0),
  TopUpLink: z.string().url().optional().or(z.literal('')),
  'general_setting.docs_link': z.string().url().optional().or(z.literal('')),
  'quota_setting.enable_free_model_pre_consume': z.boolean(),
})

type QuotaFormValues = z.infer<typeof quotaSchema>

type QuotaSettingsDefaults = Omit<
  QuotaFormValues,
  'QuotaForNewUserAmount' | 'PreConsumedQuotaAmount' | 'QuotaForInviteeAmount'
>

type QuotaSettingsSectionProps = {
  defaultValues: QuotaSettingsDefaults
}

export function QuotaSettingsSection({
  defaultValues,
}: QuotaSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const formDefaults: QuotaFormValues = {
    ...defaultValues,
    QuotaForNewUserAmount: Number(
      quotaUnitsToDollars(defaultValues.QuotaForNewUser).toFixed(6)
    ),
    PreConsumedQuotaAmount: Number(
      quotaUnitsToDollars(defaultValues.PreConsumedQuota).toFixed(6)
    ),
    QuotaForInviteeAmount: Number(
      quotaUnitsToDollars(defaultValues.QuotaForInvitee).toFixed(6)
    ),
  }

  const { form, handleSubmit, isDirty, isSubmitting } =
    useSettingsForm<QuotaFormValues>({
      resolver: zodResolver(quotaSchema) as Resolver<
        QuotaFormValues,
        unknown,
        QuotaFormValues
      >,
      defaultValues: formDefaults,
      onSubmit: async (_data, changedFields) => {
        for (const [key, value] of Object.entries(changedFields)) {
          if (key.endsWith('Amount')) {
            continue
          }
          await updateOption.mutateAsync({
            key,
            value: value as string | number | boolean,
          })
        }
      },
    })

  return (
    <SettingsSection
      title={t('Quota Settings')}
      description={t('Configure user quota allocation and rewards')}
    >
      <FormNavigationGuard when={isDirty} />

      <Form {...form}>
        <form onSubmit={handleSubmit} className='space-y-6'>
          <FormDirtyIndicator isDirty={isDirty} />
          <QuotaAmountFieldPair
            form={form}
            amountName='QuotaForNewUserAmount'
            quotaName='QuotaForNewUser'
            amountLabel={t('New User Amount')}
            quotaLabel={t('New User Quota')}
            amountDescription={t('Initial balance shown to new users')}
            quotaDescription={t('Initial raw quota given to new users')}
            amountPlaceholder='0'
            quotaPlaceholder='0'
          />

          <QuotaAmountFieldPair
            form={form}
            amountName='PreConsumedQuotaAmount'
            quotaName='PreConsumedQuota'
            amountLabel={t('Pre-Consumed Amount')}
            quotaLabel={t('Pre-Consumed Quota')}
            amountDescription={t('Pre-consumed balance before final settlement')}
            quotaDescription={t('Raw quota consumed before charging users')}
            amountPlaceholder='0'
            quotaPlaceholder='0'
          />

          <QuotaAmountFieldPair
            form={form}
            amountName='QuotaForInviteeAmount'
            quotaName='QuotaForInvitee'
            amountLabel={t('Invitee Reward Amount')}
            quotaLabel={t('Invitee Reward')}
            amountDescription={t('Balance reward granted to invited users')}
            quotaDescription={t('Quota reward given to invited users')}
            amountPlaceholder='0'
            quotaPlaceholder='0'
          />

          <FormField
            control={form.control}
            name='quota_setting.enable_free_model_pre_consume'
            render={({ field }) => (
              <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                <div className='space-y-0.5'>
                  <FormLabel className='text-base'>
                    {t('Pre-Consume for Free Models')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'When enabled, zero-cost models also pre-consume quota before final settlement.'
                    )}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={updateOption.isPending}
                  />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='TopUpLink'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Top-Up Link')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder={t('https://example.com/topup')}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t('External link for users to purchase quota')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='general_setting.docs_link'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Documentation Link')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder={t('https://docs.example.com')}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t('Link to your documentation site')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <Button
            type='submit'
            disabled={updateOption.isPending || isSubmitting}
          >
            {updateOption.isPending ? t('Saving...') : t('Save Changes')}
          </Button>
        </form>
      </Form>
    </SettingsSection>
  )
}
