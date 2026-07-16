import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { RefreshCw } from 'lucide-react'
import { formatTimestampToDate } from '@/lib/format'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  disableUser,
  getRiskDetailByIp,
  getRiskDetailByUser,
  getRiskRankings,
} from '../api'
import type {
  IpRankItem,
  RiskIpDetailItem,
  RiskMetric,
  RiskRankingsMeta,
  RiskUserDetailItem,
  UaRankItem,
  UserRankItem,
} from '../types'

const HOURS_OPTIONS = [
  { value: 24, label: 'Last 1 day' },
  { value: 72, label: 'Last 3 days' },
  { value: 168, label: 'Last 7 days' },
]

type DetailState =
  | { kind: 'ip'; title: string; items: RiskUserDetailItem[] }
  | { kind: 'user'; title: string; items: RiskIpDetailItem[] }
  | null

export function RiskRankings() {
  const { t } = useTranslation()
  const [metric, setMetric] = useState<RiskMetric>('ip_multi_user')
  const [hours, setHours] = useState(24)
  const [loading, setLoading] = useState(false)
  const [items, setItems] = useState<unknown[]>([])
  const [meta, setMeta] = useState<RiskRankingsMeta | null>(null)
  const [detail, setDetail] = useState<DetailState>(null)
  const [detailOpen, setDetailOpen] = useState(false)

  const loadRankings = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getRiskRankings(metric, hours)
      if (res.success && res.data) {
        setItems(res.data.items || [])
        setMeta(res.data.meta)
      } else {
        toast.error(res.message || t('Failed to load rankings'))
      }
    } catch {
      toast.error(t('Failed to load rankings'))
    }
    setLoading(false)
  }, [metric, hours, t])

  useEffect(() => {
    loadRankings()
  }, [loadRankings])

  const handleDisableUser = async (userId: number, username: string) => {
    if (
      !window.confirm(
        t('Disable user {{name}} (#{{id}})?', { name: username, id: userId })
      )
    ) {
      return
    }
    try {
      const res = await disableUser(userId)
      if (res.success) {
        toast.success(t('User disabled'))
      } else {
        toast.error(res.message || t('Operation failed'))
      }
    } catch {
      toast.error(t('Operation failed'))
    }
  }

  const openIpDetail = async (ip: string) => {
    setDetailOpen(true)
    try {
      const res = await getRiskDetailByIp(ip, hours)
      if (res.success && res.data) {
        setDetail({
          kind: 'ip',
          title: `${t('Users associated with IP')}: ${ip}`,
          items: res.data.items || [],
        })
      }
    } catch {
      toast.error(t('Failed to load detail'))
    }
  }

  const openUserDetail = async (userId: number, username: string) => {
    setDetailOpen(true)
    try {
      const res = await getRiskDetailByUser(userId, hours)
      if (res.success && res.data) {
        setDetail({
          kind: 'user',
          title: `${t('IPs used by user')}: ${username || userId}`,
          items: res.data.items || [],
        })
      }
    } catch {
      toast.error(t('Failed to load detail'))
    }
  }

  const showLogWarning =
    meta && (!meta.ip_log_enabled || !meta.ua_log_enabled)

  return (
    <div className='space-y-4'>
      {showLogWarning && (
        <Alert>
          <AlertDescription>
            {t(
              'Risk control relies on IP / User-Agent logging. Some logging switches are off, so ranking data may be incomplete. Enable global IP / UA logging in System Settings.'
            )}
          </AlertDescription>
        </Alert>
      )}

      <div className='flex flex-wrap items-center gap-2'>
        <Select
          value={String(hours)}
          onValueChange={(v) => setHours(Number(v))}
        >
          <SelectTrigger className='w-[140px]'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {HOURS_OPTIONS.map((o) => (
              <SelectItem key={o.value} value={String(o.value)}>
                {t(o.label)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button variant='outline' size='sm' onClick={loadRankings}>
          <RefreshCw className='size-4' />
          {t('Refresh')}
        </Button>
      </div>

      <Tabs value={metric} onValueChange={(v) => setMetric(v as RiskMetric)}>
        <TabsList>
          <TabsTrigger value='ip_multi_user'>
            {t('IP with multiple users')}
          </TabsTrigger>
          <TabsTrigger value='user_multi_ip'>
            {t('User with multiple IPs')}
          </TabsTrigger>
          <TabsTrigger value='ua'>{t('UA Ranking')}</TabsTrigger>
        </TabsList>
      </Tabs>

      <div className='rounded-md border'>
        {metric === 'ip_multi_user' && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>IP</TableHead>
                <TableHead>{t('Associated users')}</TableHead>
                <TableHead>{t('Requests')}</TableHead>
                <TableHead>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(items as IpRankItem[]).map((item) => (
                <TableRow key={item.ip}>
                  <TableCell className='font-mono'>{item.ip}</TableCell>
                  <TableCell>
                    <Badge
                      variant={item.user_count > 5 ? 'destructive' : 'secondary'}
                    >
                      {item.user_count}
                    </Badge>
                  </TableCell>
                  <TableCell>{item.request_count}</TableCell>
                  <TableCell>
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => openIpDetail(item.ip)}
                    >
                      {t('View users')}
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
              {items.length === 0 && !loading && (
                <TableRow>
                  <TableCell colSpan={4} className='text-center'>
                    {t('No data')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        )}

        {metric === 'user_multi_ip' && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('User ID')}</TableHead>
                <TableHead>{t('Username')}</TableHead>
                <TableHead>{t('IP count')}</TableHead>
                <TableHead>{t('Requests')}</TableHead>
                <TableHead>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(items as UserRankItem[]).map((item) => (
                <TableRow key={item.user_id}>
                  <TableCell>{item.user_id}</TableCell>
                  <TableCell>{item.username}</TableCell>
                  <TableCell>
                    <Badge
                      variant={item.ip_count > 5 ? 'destructive' : 'secondary'}
                    >
                      {item.ip_count}
                    </Badge>
                  </TableCell>
                  <TableCell>{item.request_count}</TableCell>
                  <TableCell className='space-x-2'>
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => openUserDetail(item.user_id, item.username)}
                    >
                      {t('View IPs')}
                    </Button>
                    <Button
                      variant='destructive'
                      size='sm'
                      onClick={() =>
                        handleDisableUser(item.user_id, item.username)
                      }
                    >
                      {t('Disable user')}
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
              {items.length === 0 && !loading && (
                <TableRow>
                  <TableCell colSpan={5} className='text-center'>
                    {t('No data')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        )}

        {metric === 'ua' && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>User-Agent</TableHead>
                <TableHead>{t('Users')}</TableHead>
                <TableHead>{t('Requests')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(items as UaRankItem[]).map((item) => (
                <TableRow key={item.ua}>
                  <TableCell className='max-w-[420px] truncate font-mono text-xs'>
                    {item.ua}
                  </TableCell>
                  <TableCell>{item.user_count}</TableCell>
                  <TableCell>{item.request_count}</TableCell>
                </TableRow>
              ))}
              {items.length === 0 && !loading && (
                <TableRow>
                  <TableCell colSpan={3} className='text-center'>
                    {t('No data')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        )}
      </div>

      <Dialog
        open={detailOpen}
        onOpenChange={(open) => {
          setDetailOpen(open)
          if (!open) setDetail(null)
        }}
      >
        <DialogContent className='max-w-3xl'>
          <DialogHeader>
            <DialogTitle>{detail?.title}</DialogTitle>
          </DialogHeader>
          {detail?.kind === 'ip' && (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('User ID')}</TableHead>
                  <TableHead>{t('Username')}</TableHead>
                  <TableHead>{t('Requests')}</TableHead>
                  <TableHead>{t('First seen')}</TableHead>
                  <TableHead>{t('Last seen')}</TableHead>
                  <TableHead>{t('Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {detail.items.map((item) => (
                  <TableRow key={item.user_id}>
                    <TableCell>{item.user_id}</TableCell>
                    <TableCell>{item.username}</TableCell>
                    <TableCell>{item.request_count}</TableCell>
                    <TableCell>{formatTimestampToDate(item.first_seen)}</TableCell>
                    <TableCell>{formatTimestampToDate(item.last_seen)}</TableCell>
                    <TableCell>
                      <Button
                        variant='destructive'
                        size='sm'
                        onClick={() =>
                          handleDisableUser(item.user_id, item.username)
                        }
                      >
                        {t('Disable user')}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
          {detail?.kind === 'user' && (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>IP</TableHead>
                  <TableHead>{t('Requests')}</TableHead>
                  <TableHead>{t('First seen')}</TableHead>
                  <TableHead>{t('Last seen')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {detail.items.map((item) => (
                  <TableRow key={item.ip}>
                    <TableCell className='font-mono'>{item.ip}</TableCell>
                    <TableCell>{item.request_count}</TableCell>
                    <TableCell>{formatTimestampToDate(item.first_seen)}</TableCell>
                    <TableCell>{formatTimestampToDate(item.last_seen)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
