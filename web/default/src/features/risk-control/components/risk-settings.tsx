import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Plus, Trash2 } from 'lucide-react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { getOptions, updateOption } from '../api'
import type { RiskAutoBanRule } from '../types'

const KEY_PREFIX = 'risk_control_setting.'
const KEY_ENABLED = `${KEY_PREFIX}enabled`
const KEY_UA_BLACKLIST = `${KEY_PREFIX}ua_blacklist`
const KEY_UA_ACTION = `${KEY_PREFIX}ua_blacklist_action`
const KEY_IP_BLACKLIST = `${KEY_PREFIX}ip_blacklist`
const KEY_SCAN_MINUTES = `${KEY_PREFIX}scan_minutes`
const KEY_WHITELIST = `${KEY_PREFIX}whitelist_user_ids`
const KEY_RULES = `${KEY_PREFIX}auto_ban_rules`

interface EditableRule extends RiskAutoBanRule {
  _id: number
}

function parseJsonArray<T>(value: string | undefined, fallback: T[]): T[] {
  if (!value) return fallback
  try {
    const parsed = JSON.parse(value)
    return Array.isArray(parsed) ? parsed : fallback
  } catch {
    return fallback
  }
}

export function RiskSettings() {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)

  const [enabled, setEnabled] = useState(false)
  const [uaBlacklistText, setUaBlacklistText] = useState('')
  const [uaAction, setUaAction] = useState('block')
  const [ipBlacklistText, setIpBlacklistText] = useState('')
  const [scanMinutes, setScanMinutes] = useState(10)
  const [whitelistText, setWhitelistText] = useState('')
  const [rules, setRules] = useState<EditableRule[]>([])

  const loadSettings = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getOptions()
      if (res.success && res.data) {
        const optionMap: Record<string, string> = {}
        res.data.forEach((item) => {
          optionMap[item.key] = item.value
        })
        setEnabled(optionMap[KEY_ENABLED] === 'true')
        setUaBlacklistText(
          parseJsonArray<string>(optionMap[KEY_UA_BLACKLIST], []).join('\n')
        )
        setUaAction(optionMap[KEY_UA_ACTION] || 'block')
        setIpBlacklistText(
          parseJsonArray<string>(optionMap[KEY_IP_BLACKLIST], []).join('\n')
        )
        const scan = parseInt(optionMap[KEY_SCAN_MINUTES] || '', 10)
        setScanMinutes(Number.isFinite(scan) && scan > 0 ? scan : 10)
        setWhitelistText(
          parseJsonArray<number>(optionMap[KEY_WHITELIST], []).join(',')
        )
        setRules(
          parseJsonArray<RiskAutoBanRule>(optionMap[KEY_RULES], []).map(
            (r, idx) => ({ ...r, _id: idx })
          )
        )
      } else {
        toast.error(res.message || t('Failed to load settings'))
      }
    } catch {
      toast.error(t('Failed to load settings'))
    }
    setLoading(false)
  }, [t])

  useEffect(() => {
    loadSettings()
  }, [loadSettings])

  const saveSettings = async () => {
    const uaBlacklist = uaBlacklistText
      .split('\n')
      .map((s) => s.trim())
      .filter(Boolean)
    const ipBlacklist = ipBlacklistText
      .split('\n')
      .map((s) => s.trim())
      .filter(Boolean)
    const whitelistUserIds = whitelistText
      .split(/[,，\s]+/)
      .map((s) => parseInt(s, 10))
      .filter((n) => Number.isFinite(n) && n > 0)
    const cleanRules = rules.map(({ _id, ...rest }) => ({
      enabled: !!rest.enabled,
      metric: rest.metric || 'ip_multi_user',
      window_hours: rest.window_hours > 0 ? rest.window_hours : 24,
      threshold: rest.threshold > 0 ? rest.threshold : 1,
      action: rest.action || 'alert',
    }))

    const settingUpdates: Array<{ key: string; value: string }> = [
      { key: KEY_UA_BLACKLIST, value: JSON.stringify(uaBlacklist) },
      { key: KEY_UA_ACTION, value: uaAction },
      { key: KEY_IP_BLACKLIST, value: JSON.stringify(ipBlacklist) },
      { key: KEY_SCAN_MINUTES, value: String(scanMinutes) },
      { key: KEY_WHITELIST, value: JSON.stringify(whitelistUserIds) },
      { key: KEY_RULES, value: JSON.stringify(cleanRules) },
    ]
    const enabledUpdate = { key: KEY_ENABLED, value: String(enabled) }
    const updates = enabled
      ? [...settingUpdates, enabledUpdate]
      : [enabledUpdate, ...settingUpdates]

    setSaving(true)
    try {
      let failed = false
      for (const item of updates) {
        const result = await updateOption(item.key, item.value)
        if (!result.success) {
          failed = true
          break
        }
      }
      if (failed) {
        toast.error(t('Some settings failed to save, please retry'))
      } else {
        toast.success(t('Settings saved'))
      }
    } catch {
      toast.error(t('Failed to save settings'))
    }
    setSaving(false)
  }

  const addRule = () => {
    setRules([
      ...rules,
      {
        _id: Date.now(),
        enabled: false,
        metric: 'ip_multi_user',
        window_hours: 24,
        threshold: 3,
        action: 'alert',
      },
    ])
  }

  const updateRule = (id: number, patch: Partial<EditableRule>) => {
    setRules(rules.map((r) => (r._id === id ? { ...r, ...patch } : r)))
  }

  const removeRule = (id: number) => {
    setRules(rules.filter((r) => r._id !== id))
  }

  return (
    <div className="space-y-4" aria-busy={loading}>
      <Alert>
        <AlertDescription>
          {t(
            'Auto-ban rules run via periodic background scanning and only affect regular users (admins and whitelisted users are never auto-processed). The UA blacklist intercepts requests in real time.'
          )}
        </AlertDescription>
      </Alert>

      <Card>
        <CardHeader>
          <CardTitle>{t('Master Switch')}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center gap-3">
            <Switch checked={enabled} onCheckedChange={setEnabled} />
            <Label>
              {t(
                'Enable risk control (master switch for UA blacklist interception and auto-ban scanning)'
              )}
            </Label>
          </div>
          <div className="flex items-center gap-3">
            <Label className="shrink-0">
              {t('Auto-ban scan interval (minutes)')}
            </Label>
            <Input
              type="number"
              min={1}
              max={1440}
              value={scanMinutes}
              onChange={(e) => {
                const v = parseInt(e.target.value, 10)
                setScanMinutes(Number.isFinite(v) && v > 0 ? v : 10)
              }}
              className="w-28"
            />
          </div>
          <div className="space-y-2">
            <Label>
              {t('Whitelist user IDs (comma-separated, never auto-processed)')}
            </Label>
            <Input
              value={whitelistText}
              onChange={(e) => setWhitelistText(e.target.value)}
              placeholder="1,2,3"
              className="max-w-md"
            />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('UA Blacklist')}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-muted-foreground text-sm">
            {t(
              'One entry per line. Plain text matches as case-insensitive substring; entries with regex metacharacters are treated as regular expressions.'
            )}
          </p>
          <Textarea
            value={uaBlacklistText}
            onChange={(e) => setUaBlacklistText(e.target.value)}
            placeholder={'curl\npython-requests\n^Go-http-client'}
            rows={6}
          />
          <div className="flex items-center gap-3">
            <Label className="shrink-0">{t('Action on match')}</Label>
            <Select value={uaAction} onValueChange={setUaAction}>
              <SelectTrigger className="w-[280px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="block">
                  {t('Reject request only (403)')}
                </SelectItem>
                <SelectItem value="disable_user">
                  {t('Reject request and auto-disable user')}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('IP Blacklist')}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-muted-foreground text-sm">
            {t(
              'One entry per line. Supports exact IPs or CIDR ranges (e.g. 1.2.3.4, 10.0.0.0/8). Matching requests are rejected directly.'
            )}
          </p>
          <Textarea
            value={ipBlacklistText}
            onChange={(e) => setIpBlacklistText(e.target.value)}
            placeholder={'1.2.3.4\n10.0.0.0/8\n2001:db8::/32'}
            rows={6}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle>{t('Auto-ban Rules')}</CardTitle>
          <Button variant="outline" size="sm" onClick={addRule}>
            <Plus className="size-4" />
            {t('Add rule')}
          </Button>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Enabled')}</TableHead>
                <TableHead>{t('Metric')}</TableHead>
                <TableHead>{t('Window (hours)')}</TableHead>
                <TableHead>{t('Threshold (triggers when exceeded)')}</TableHead>
                <TableHead>{t('Action')}</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {rules.map((rule) => (
                <TableRow key={rule._id}>
                  <TableCell>
                    <Switch
                      checked={rule.enabled}
                      onCheckedChange={(checked) =>
                        updateRule(rule._id, { enabled: checked })
                      }
                    />
                  </TableCell>
                  <TableCell>
                    <Select
                      value={rule.metric}
                      onValueChange={(v) =>
                        updateRule(rule._id, {
                          metric: v as RiskAutoBanRule['metric'],
                        })
                      }
                    >
                      <SelectTrigger className="w-[170px]">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="ip_multi_user">
                          {t('Users per IP')}
                        </SelectItem>
                        <SelectItem value="user_multi_ip">
                          {t('IPs per user')}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  </TableCell>
                  <TableCell>
                    <Input
                      type="number"
                      min={1}
                      max={168}
                      value={rule.window_hours}
                      onChange={(e) => {
                        const v = parseInt(e.target.value, 10)
                        updateRule(rule._id, {
                          window_hours: Number.isFinite(v) && v > 0 ? v : 24,
                        })
                      }}
                      className="w-24"
                    />
                  </TableCell>
                  <TableCell>
                    <Input
                      type="number"
                      min={1}
                      value={rule.threshold}
                      onChange={(e) => {
                        const v = parseInt(e.target.value, 10)
                        updateRule(rule._id, {
                          threshold: Number.isFinite(v) && v > 0 ? v : 1,
                        })
                      }}
                      className="w-24"
                    />
                  </TableCell>
                  <TableCell>
                    <Select
                      value={rule.action}
                      onValueChange={(v) =>
                        updateRule(rule._id, {
                          action: v as RiskAutoBanRule['action'],
                        })
                      }
                    >
                      <SelectTrigger className="w-[170px]">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="alert">{t('Alert only')}</SelectItem>
                        <SelectItem value="disable_user">
                          {t('Auto-disable user')}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  </TableCell>
                  <TableCell>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => removeRule(rule._id)}
                    >
                      <Trash2 className="size-4 text-destructive" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
              {rules.length === 0 && (
                <TableRow>
                  <TableCell colSpan={6} className="text-center">
                    {t('No rules yet')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Button onClick={saveSettings} disabled={saving}>
        {t('Save settings')}
      </Button>
    </div>
  )
}
