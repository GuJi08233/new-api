import { SettingsPage } from '../components/settings-page'
import type { IntegrationSettings as IntegrationSettingsType } from '../types'
import {
  INTEGRATIONS_DEFAULT_SECTION,
  getIntegrationsSectionContent,
  getIntegrationsSectionMeta,
} from './section-registry.tsx'

const defaultIntegrationSettings: IntegrationSettingsType = {
  SMTPServer: '',
  SMTPPort: '',
  SMTPAccount: '',
  SMTPFrom: '',
  SMTPToken: '',
  SMTPSSLEnabled: false,
  SMTPStartTLSEnabled: false,
  SMTPInsecureSkipVerify: false,
  SMTPForceAuthLogin: false,
  WorkerUrl: '',
  WorkerValidKey: '',
  WorkerAllowHttpImageRequestEnabled: false,
  ChannelDisableThreshold: '',
  QuotaRemindThreshold: '',
  AutomaticDisableChannelEnabled: false,
  AutomaticEnableChannelEnabled: false,
  AutomaticDisableKeywords: '',
  AutomaticDisableStatusCodes: '401',
  AutomaticRetryStatusCodes:
    '100-199,300-399,401-407,409-499,500-503,505-523,525-599',
  'monitor_setting.auto_test_channel_enabled': false,
  'monitor_setting.auto_test_channel_minutes': 10,
  'perf_metrics_setting.enabled': true,
  'perf_metrics_setting.flush_interval': 5,
  'perf_metrics_setting.bucket_time': 'hour',
  'perf_metrics_setting.retention_days': 0,
  'model_deployment.ionet.api_key': '',
  'model_deployment.ionet.enabled': false,
  PayAddress: '',
  EpayId: '',
  EpayKey: '',
  Price: 7.3,
  MinTopUp: 1,
  CustomCallbackAddress: '',
  PayMethods: '',
  'payment_setting.amount_options': '',
  'payment_setting.amount_discount': '',
  StripeApiSecret: '',
  StripeWebhookSecret: '',
  StripePriceId: '',
  StripeUnitPrice: 8.0,
  StripeMinTopUp: 1,
  StripePromotionCodesEnabled: false,
  CreemApiKey: '',
  CreemWebhookSecret: '',
  CreemTestMode: false,
  CreemProducts: '[]',
  EthereumEnabled: false,
  EthereumChainId: 11155111,
  EthereumContractAddress: '',
  EthereumAlchemyWebhookSigningKey: '',
  EthereumMinTopUp: 1,
  EthereumSupportedTokens:
    '[{"symbol":"ETH","address":"0x0000000000000000000000000000000000000000","decimals":18,"price":"0.001"}]',
  WaffoEnabled: false,
  WaffoApiKey: '',
  WaffoPrivateKey: '',
  WaffoPublicCert: '',
  WaffoSandboxPublicCert: '',
  WaffoSandboxApiKey: '',
  WaffoSandboxPrivateKey: '',
  WaffoSandbox: false,
  WaffoMerchantId: '',
  WaffoCurrency: 'USD',
  WaffoUnitPrice: 1,
  WaffoMinTopUp: 1,
  WaffoNotifyUrl: '',
  WaffoReturnUrl: '',
  WaffoPayMethods: '[]',
  WaffoPancakeEnabled: false,
  WaffoPancakeSandbox: false,
  WaffoPancakeMerchantID: '',
  WaffoPancakePrivateKey: '',
  WaffoPancakeWebhookPublicKey: '',
  WaffoPancakeWebhookTestKey: '',
  WaffoPancakeStoreID: '',
  WaffoPancakeProductID: '',
  WaffoPancakeReturnURL: '',
  WaffoPancakeCurrency: 'USD',
  WaffoPancakeUnitPrice: 1,
  WaffoPancakeMinTopUp: 1,
}

export function IntegrationSettings() {
  return (
    <SettingsPage
      routePath='/_authenticated/system-settings/integrations/$section'
      defaultSettings={defaultIntegrationSettings}
      defaultSection={INTEGRATIONS_DEFAULT_SECTION}
      getSectionContent={getIntegrationsSectionContent}
      getSectionMeta={getIntegrationsSectionMeta}
    />
  )
}
