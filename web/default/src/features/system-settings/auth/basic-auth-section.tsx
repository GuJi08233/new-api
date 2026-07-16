import { useMemo } from 'react'
import * as z from 'zod'
import { useForm, type Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { quotaUnitsToDollars } from '@/lib/format'
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
import { Textarea } from '@/components/ui/textarea'
import { QuotaAmountFieldPair } from '../components/quota-amount-field-pair'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'

const basicAuthSchema = z.object({
  PasswordLoginEnabled: z.boolean(),
  PasswordRegisterEnabled: z.boolean(),
  EmailVerificationEnabled: z.boolean(),
  RegisterEnabled: z.boolean(),
  InvitationCodeEnabled: z.boolean(),
  InvitationCodePrice: z.coerce.number().min(0),
  InvitationCodePriceAmount: z.coerce.number().min(0),
  InvitationCodeRewardRatio: z.coerce.number().min(0).max(100),
  EmailDomainRestrictionEnabled: z.boolean(),
  EmailAliasRestrictionEnabled: z.boolean(),
  EmailDomainWhitelist: z.string(),
})

type BasicAuthFormValues = {
  PasswordLoginEnabled: boolean
  PasswordRegisterEnabled: boolean
  EmailVerificationEnabled: boolean
  RegisterEnabled: boolean
  InvitationCodeEnabled: boolean
  InvitationCodePrice: number
  InvitationCodePriceAmount: number
  InvitationCodeRewardRatio: number
  EmailDomainRestrictionEnabled: boolean
  EmailAliasRestrictionEnabled: boolean
  EmailDomainWhitelist: string
}

type BasicAuthDefaultValues = Omit<BasicAuthFormValues, 'InvitationCodePriceAmount'>

type BasicAuthSectionProps = {
  defaultValues: BasicAuthDefaultValues
}

export function BasicAuthSection({ defaultValues }: BasicAuthSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const formDefaults = useMemo<BasicAuthFormValues>(
    () => ({
      ...defaultValues,
      InvitationCodePriceAmount: Number(
        quotaUnitsToDollars(defaultValues.InvitationCodePrice).toFixed(6)
      ),
      EmailDomainWhitelist: defaultValues.EmailDomainWhitelist.split(',')
        .map((domain) => domain.trim())
        .filter(Boolean)
        .join('\n'),
    }),
    [defaultValues]
  )

  const form = useForm<BasicAuthFormValues>({
    resolver: zodResolver(basicAuthSchema) as Resolver<
      BasicAuthFormValues,
      unknown,
      BasicAuthFormValues
    >,
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  const onSubmit = async (data: BasicAuthFormValues) => {
    const updates: Array<{ key: string; value: string | boolean | number }> = []

    Object.entries(data).forEach(([key, value]) => {
      if (key === 'InvitationCodePriceAmount') return
      if (key === 'EmailDomainWhitelist') {
        if (typeof value !== 'string') return
        const domains = value
          .split('\n')
          .map((domain) => domain.trim())
          .filter(Boolean)
          .join(',')
        if (domains !== defaultValues.EmailDomainWhitelist) {
          updates.push({ key, value: domains })
        }
      } else if (value !== defaultValues[key as keyof typeof defaultValues]) {
        updates.push({ key, value })
      }
    })

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
  }

  return (
    <SettingsSection title={t('Basic Authentication')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />
          <FormField
            control={form.control}
            name='PasswordLoginEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Password Login')}</FormLabel>
                  <FormDescription>
                    {t('Allow users to log in with password')}
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

          <FormField
            control={form.control}
            name='RegisterEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Registration Enabled')}</FormLabel>
                  <FormDescription>
                    {t('Allow new users to register')}
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

          <FormField
            control={form.control}
            name='InvitationCodeEnabled'
            render={({ field }) => (
              <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                <div className='space-y-0.5'>
                  <FormLabel className='text-base'>
                    {t('Require Invitation Code')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'Require users to provide an invitation code when registering'
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

          <FormField
            control={form.control}
            name='PasswordRegisterEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Password Registration')}</FormLabel>
                  <FormDescription>
                    {t('Allow registration with password')}
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

          <QuotaAmountFieldPair
            form={form}
            amountName='InvitationCodePriceAmount'
            quotaName='InvitationCodePrice'
            amountLabel={t('Invitation Code Price (Amount)')}
            quotaLabel={t('Invitation Code Price (Quota)')}
            amountDescription={t(
              'Displayed balance cost for generating one invitation code'
            )}
            quotaDescription={t(
              'Raw quota cost for generating one invitation code'
            )}
            amountPlaceholder='0'
            quotaPlaceholder='0'
          />

          <FormField
            control={form.control}
            name='InvitationCodeRewardRatio'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Invitation Reward Ratio (%)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={0}
                    max={100}
                    step={1}
                    value={field.value}
                    onChange={(e) =>
                      field.onChange(
                        Number.isFinite(e.target.valueAsNumber)
                          ? e.target.valueAsNumber
                          : 0
                      )
                    }
                    name={field.name}
                    onBlur={field.onBlur}
                    ref={field.ref}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Percentage of the invitation code cost rewarded to the invitee after redemption'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='EmailVerificationEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Email Verification')}</FormLabel>
                  <FormDescription>
                    {t('Require email verification for new accounts')}
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

          <FormField
            control={form.control}
            name='EmailDomainRestrictionEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Email Domain Restriction')}</FormLabel>
                  <FormDescription>
                    {t('Only allow specific email domains')}
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

          <FormField
            control={form.control}
            name='EmailAliasRestrictionEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Email Alias Restriction')}</FormLabel>
                  <FormDescription>
                    {t('Block email aliases (e.g., user+alias@domain.com)')}
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

          <FormField
            control={form.control}
            name='EmailDomainWhitelist'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Email Domain Whitelist')}</FormLabel>
                <FormControl>
                  <Textarea
                    placeholder={t('example.com&#10;company.com')}
                    rows={4}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'One domain per line (only used when domain restriction is enabled)'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
