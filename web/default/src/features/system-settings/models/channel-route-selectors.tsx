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
import { useEffect, useMemo, useState } from 'react'
import { ChevronDown, Loader2, Plus, Search, Wand2, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { CHANNEL_TYPES } from '@/features/channels/constants'

type BoundChannelData = { id: number; name: string; type: number; models: string }
type ChannelListItem = { id: number; name: string; type: number; status: number }

const ENDPOINT_TEMPLATES = [
  { label: 'OpenAI Chat', path: '/v1/chat/completions' },
  { label: 'OpenAI Responses', path: '/v1/responses' },
  { label: 'OpenAI Responses Compact', path: '/v1/responses/compact' },
  { label: 'Anthropic Messages', path: '/v1/messages' },
  { label: 'Gemini Generate', path: '/v1beta/models/{model}:generateContent' },
  { label: 'Gemini Embed', path: '/v1beta/models/{model}:embedContent' },
  {
    label: 'Gemini Batch Embed',
    path: '/v1beta/models/{model}:batchEmbedContents',
  },
  { label: 'Embeddings', path: '/v1/embeddings' },
  { label: 'Rerank', path: '/v1/rerank' },
  { label: 'Image Generation', path: '/v1/images/generations' },
  { label: 'Audio Transcription', path: '/v1/audio/transcriptions' },
  { label: 'Audio Speech', path: '/v1/audio/speech' },
  { label: 'Moderations', path: '/v1/moderations' },
  { label: 'Files', path: '/v1/files' },
]

const MATCH_MODES = [
  { label: 'Exact Match', value: 0 },
  { label: 'Prefix Match', value: 1 },
  { label: 'Contains Match', value: 2 },
  { label: 'Suffix Match', value: 3 },
]

function nameRuleToRegex(name: string, rule: number): string {
  const escaped = name.replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&')
  switch (rule) {
    case 0: return `^${escaped}$`
    case 1: return `^${escaped}`
    case 2: return escaped
    case 3: return `${escaped}$`
    default: return `^${escaped}$`
  }
}

// ---------------------------------------------------------------------------
// GroupSelector
// ---------------------------------------------------------------------------

export function GroupSelector({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const { t } = useTranslation()
  const [groups, setGroups] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)

  const selectedGroups = useMemo(() => {
    const lines = (value || '').split('\n').filter(Boolean)
    const names: string[] = []
    for (const line of lines) {
      const m = line.match(/^\^?(.+?)\$$/)
      if (m) names.push(m[1])
    }
    return names
  }, [value])

  useEffect(() => {
    if (open && groups.length === 0) {
      setLoading(true)
      fetch('/api/group/')
        .then((r) => r.json())
        .then((data) => { if (data.success && Array.isArray(data.data)) setGroups(data.data) })
        .finally(() => setLoading(false))
        .catch(() => {})
    }
  }, [open, groups.length])

  const handleToggle = (groupName: string) => {
    const current = selectedGroups
    const next = current.includes(groupName) ? current.filter((g) => g !== groupName) : [...current, groupName]
    onChange(next.map((g) => `^${g}$`).join('\n'))
  }

  return (
    <div className='space-y-2'>
      <Label className='text-xs'>{t('Match Groups')}</Label>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <Button
              type='button'
              variant='outline'
              className='w-full justify-between font-normal'
            />
          }
        >
          <span className='truncate'>
            {selectedGroups.length > 0
              ? selectedGroups.join(', ')
              : t('All groups (no filter)')}
          </span>
          <ChevronDown className='ml-2 h-4 w-4 shrink-0 opacity-50' />
        </PopoverTrigger>
        <PopoverContent className='w-64 p-2' align='start'>
          {loading && (
            <div className='flex items-center justify-center py-4'><Loader2 className='h-4 w-4 animate-spin' /></div>
          )}
          {!loading && groups.length === 0 && (
            <p className='text-muted-foreground py-2 text-center text-xs'>{t('No groups available')}</p>
          )}
          {!loading && groups.length > 0 && (
            <div className='space-y-1'>
              {groups.map((group) => (
                <label key={group} className='flex cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 hover:bg-accent'>
                  <Checkbox checked={selectedGroups.includes(group)} onCheckedChange={() => handleToggle(group)} />
                  <span className='text-sm'>{group}</span>
                </label>
              ))}
            </div>
          )}
        </PopoverContent>
      </Popover>
      <p className='text-muted-foreground text-xs'>{t('Leave empty to match all groups.')}</p>
    </div>
  )
}

// ---------------------------------------------------------------------------
// ModelNameMatcher
// ---------------------------------------------------------------------------

export function ModelNameMatcher({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const { t } = useTranslation()
  const [inputValue, setInputValue] = useState('')
  const [matchMode, setMatchMode] = useState<number>(0)

  const entries = useMemo(() => {
    return (value || '').split('\n').filter(Boolean).map((regex) => {
      try {
        if (regex.startsWith('^') && regex.endsWith('$')) return { name: regex.slice(1, -1).replaceAll(/\\./g, '.'), mode: 0 }
        if (regex.startsWith('^')) return { name: regex.slice(1).replaceAll(/\\./g, '.'), mode: 1 }
        if (regex.endsWith('$')) return { name: regex.slice(0, -1).replaceAll(/\\./g, '.'), mode: 3 }
        return { name: regex.replaceAll(/\\./g, '.'), mode: 2 }
      } catch { return { name: regex, mode: 2 } }
    })
  }, [value])

  const handleAdd = () => {
    const name = inputValue.trim()
    if (!name) return
    const newRegex = nameRuleToRegex(name, matchMode)
    const current = (value || '').split('\n').filter(Boolean)
    if (current.includes(newRegex)) { toast.warning(t('Already added')); return }
    onChange([...current, newRegex].join('\n'))
    setInputValue('')
  }

  const handleRemove = (index: number) => {
    onChange((value || '').split('\n').filter(Boolean).filter((_, i) => i !== index).join('\n'))
  }

  return (
    <div className='space-y-2'>
      <Label className='text-xs'>{t('Model Match')} *</Label>
      <div className='flex gap-2'>
        <Select
          items={MATCH_MODES.map((mode) => ({
            label: t(mode.label),
            value: String(mode.value),
          }))}
          value={String(matchMode)}
          onValueChange={(mode) => {
            if (mode !== null) setMatchMode(Number(mode))
          }}
        >
          <SelectTrigger className='w-36 shrink-0'><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {MATCH_MODES.map((mode) => (
                <SelectItem key={mode.value} value={String(mode.value)}>
                  {t(mode.label)}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        <Input value={inputValue} onChange={(e) => setInputValue(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); handleAdd() } }} placeholder={t('Enter model name')} className='flex-1' />
        <Button type='button' variant='outline' size='icon' onClick={handleAdd} disabled={!inputValue.trim()}><Plus className='h-4 w-4' /></Button>
      </div>
      {entries.length > 0 && (
        <div className='flex flex-wrap gap-1.5'>
          {entries.map((entry, i) => (
            <Badge key={`${entry.mode}:${entry.name}`} variant='secondary' className='flex items-center gap-1 pr-1'>
              <span className='text-xs'>{entry.name}<span className='text-muted-foreground ml-1'>({MATCH_MODES[entry.mode]?.label || '?'})</span></span>
              <button type='button' onClick={() => handleRemove(i)} className='hover:bg-destructive/20 ml-0.5 rounded-full p-0.5'><X className='h-3 w-3' /></button>
            </Badge>
          ))}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// PathSelector
// ---------------------------------------------------------------------------

export function PathSelector({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const { t } = useTranslation()
  const [customPath, setCustomPath] = useState('')
  const endpointItems = [
    { label: t('Select an endpoint...'), value: null },
    ...ENDPOINT_TEMPLATES.map((endpoint) => ({
      label: `${endpoint.label} — ${endpoint.path}`,
      value: endpoint.path,
    })),
  ]

  const entries = useMemo(() => {
    return (value || '').split('\n').filter(Boolean).map((regex) => {
      try {
        // Gemini 风格正则：还原为可读的模板路径
        const geminiAction = [
          'batchEmbedContents',
          'embedContent',
          'generateContent',
        ].find((action) => regex.includes(action))
        if (geminiAction) {
          return { path: `/v1beta/models/{model}:${geminiAction}`, gemini: true }
        }
        const m = regex.match(/^\^(.+?)\$$/)
        return { path: m ? m[1].replaceAll('\\.', '.') : regex, gemini: false }
      } catch { return { path: regex, gemini: false } }
    })
  }, [value])

  const addPath = (path: string) => {
    let regex: string
    if (path.includes('{model}')) {
      // Gemini 风格路径：归一化版本前缀和流式动作
      // /v1beta/models/{model}:generateContent → ^\/(v1|v1beta|v1alpha)\/models\/[^\/:]+:(stream)?generateContent(\?.*)?$
      const afterVersion = path.replace(/^\/v1(alpha|beta)?\//, '')
      const versionGroup = '(v1|v1beta|v1alpha)'
      let body = afterVersion.replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&')
      // {model} → 匹配任意模型名
      body = body.replace(/\\\{model\\\}/, '[^/:]+')
      // generateContent → 同时匹配 streamGenerateContent
      body = body.replace(/generateContent/, '(stream)?generateContent')
      regex = `^\\/${versionGroup}\\/${body}(\\?.*)?$`
    } else {
      regex = `^${path.replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`
    }
    const current = (value || '').split('\n').filter(Boolean)
    if (current.includes(regex)) { toast.warning(t('Already added')); return }
    onChange([...current, regex].join('\n'))
  }

  const handleRemove = (index: number) => {
    onChange((value || '').split('\n').filter(Boolean).filter((_, i) => i !== index).join('\n'))
  }

  return (
    <div className='space-y-2'>
      <Label className='text-xs'>{t('Match Paths')}</Label>
      <Select
        items={endpointItems}
        value={null}
        onValueChange={(path) => {
          if (path) addPath(path)
        }}
      >
        <SelectTrigger><SelectValue /></SelectTrigger>
        <SelectContent>
          <SelectGroup>
            <SelectItem value={null}>{t('Select an endpoint...')}</SelectItem>
            {ENDPOINT_TEMPLATES.map((endpoint) => (
              <SelectItem key={endpoint.path} value={endpoint.path}>
                {endpoint.label} — {endpoint.path}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      <div className='flex gap-2'>
        <Input value={customPath} onChange={(e) => setCustomPath(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); if (customPath.trim()) { addPath(customPath.trim()); setCustomPath('') } } }} placeholder={t('Or enter custom path...')} className='flex-1' />
        <Button type='button' variant='outline' size='icon' onClick={() => { if (customPath.trim()) { addPath(customPath.trim()); setCustomPath('') } }} disabled={!customPath.trim()}><Plus className='h-4 w-4' /></Button>
      </div>
      {entries.length > 0 && (
        <div className='flex flex-wrap gap-1.5'>
          {entries.map((entry, i) => (
            <Badge key={entry.path} variant='secondary' className='flex items-center gap-1 pr-1'>
              <span className='font-mono text-xs'>{entry.path}</span>
              <button type='button' onClick={() => handleRemove(i)} className='hover:bg-destructive/20 ml-0.5 rounded-full p-0.5'><X className='h-3 w-3' /></button>
            </Badge>
          ))}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// ChannelSelector
// ---------------------------------------------------------------------------

export function ChannelSelector({ value, onChange, compact = false }: { value: string; onChange: (v: string) => void; compact?: boolean }) {
  const { t } = useTranslation()
  const [channels, setChannels] = useState<ChannelListItem[]>([])
  const [loading, setLoading] = useState(false)
  const [search, setSearch] = useState('')
  const [open, setOpen] = useState(false)
  const [autoFillModel, setAutoFillModel] = useState('')
  const [autoFilling, setAutoFilling] = useState(false)
  const [loaded, setLoaded] = useState(false)

  const selectedIds = useMemo(() => {
    const ids: number[] = []
    for (const tok of (value || '').split(/[\n,]/).map((s) => s.trim()).filter(Boolean)) {
      const n = Number(tok); if (Number.isInteger(n) && n > 0) ids.push(n)
    }
    return [...new Set(ids)]
  }, [value])

  const channelMap = useMemo(() => { const m = new Map<number, ChannelListItem>(); for (const ch of channels) m.set(ch.id, ch); return m }, [channels])

  useEffect(() => {
    if (open && !loaded) {
      setLoading(true)
      fetch('/api/channel/?p=0&page_size=500&id_sort=true')
        .then((r) => r.json())
        .then((data) => { if (data.success && data.data?.items) setChannels(data.data.items.map((ch: Record<string, unknown>) => ({ id: ch.id as number, name: ch.name as string, type: ch.type as number, status: ch.status as number }))) })
        .finally(() => { setLoading(false); setLoaded(true) })
        .catch(() => {})
    }
  }, [open, loaded])

  const filteredChannels = useMemo(() => {
    if (!search) return channels
    const q = search.toLowerCase()
    return channels.filter((ch) => ch.name.toLowerCase().includes(q) || String(ch.id).includes(q) || (CHANNEL_TYPES[ch.type as keyof typeof CHANNEL_TYPES] || '').toLowerCase().includes(q))
  }, [channels, search])

  const handleToggle = (id: number) => {
    const next = selectedIds.includes(id) ? selectedIds.filter((i) => i !== id) : [...selectedIds, id]
    onChange(next.join('\n'))
  }

  const handleAutoFill = async () => {
    const model = autoFillModel.trim()
    if (!model) return
    setAutoFilling(true)
    try {
      const resp = await fetch(`/api/channel/bound?model=${encodeURIComponent(model)}`)
      const data = await resp.json()
      if (data.success && Array.isArray(data.data)) {
        if (data.data.length === 0) { toast.info(t('No channels found for this model')); return }
        const existing = new Set(selectedIds)
        const newIds = data.data.map((ch: BoundChannelData) => ch.id).filter((id: number) => !existing.has(id))
        onChange([...selectedIds, ...newIds].join('\n'))
        setChannels((prev) => { const e = new Set(prev.map((c) => c.id)); return [...prev, ...data.data.filter((ch: BoundChannelData) => !e.has(ch.id)).map((ch: BoundChannelData) => ({ id: ch.id, name: ch.name, type: ch.type, status: 1 }))] })
        toast.success(t('Added {{count}} channels', { count: newIds.length }))
      }
    } catch { toast.error(t('Failed to fetch bound channels')) } finally { setAutoFilling(false) }
  }

  const selectedDisplay = useMemo(() => selectedIds.map((id) => { const ch = channelMap.get(id); return { id, name: ch?.name || `#${id}`, type: ch?.type } }), [selectedIds, channelMap])

  return (
    <div className='space-y-2'>
      <Label className='text-xs'>{compact ? t('Channel Pool') : t('Fallback Channel Pool')}</Label>
      <div className='flex gap-2'>
        <Input value={autoFillModel} onChange={(e) => setAutoFillModel(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); handleAutoFill() } }} placeholder={t('Enter model name to auto-fill channels...')} className='flex-1' />
        <Button type='button' variant='outline' size='sm' onClick={handleAutoFill} disabled={autoFilling || !autoFillModel.trim()} className='shrink-0'>
          {autoFilling ? <Loader2 className='mr-1 h-3 w-3 animate-spin' /> : <Wand2 className='mr-1 h-3 w-3' />}
          {t('Auto-fill')}
        </Button>
      </div>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <Button
              type='button'
              variant='outline'
              className='w-full justify-between font-normal'
            />
          }
        >
          <span className='truncate'>{selectedIds.length > 0 ? t('{{count}} channels selected', { count: selectedIds.length }) : t('Select channels...')}</span>
          <ChevronDown className='ml-2 h-4 w-4 shrink-0 opacity-50' />
        </PopoverTrigger>
        <PopoverContent className='w-80 p-0' align='start'>
          <div className='border-b p-2'>
            <div className='relative'><Search className='text-muted-foreground absolute top-2.5 left-2.5 h-4 w-4' /><Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder={t('Search channels...')} className='pl-8' /></div>
          </div>
          <div className='max-h-60 overflow-y-auto p-1'>
            {loading && <div className='flex items-center justify-center py-8'><Loader2 className='h-4 w-4 animate-spin' /></div>}
            {!loading && filteredChannels.length === 0 && <p className='text-muted-foreground py-4 text-center text-xs'>{t('No channels found')}</p>}
            {!loading && filteredChannels.map((ch) => (
              <label key={ch.id} className='flex cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 hover:bg-accent'>
                <Checkbox checked={selectedIds.includes(ch.id)} onCheckedChange={() => handleToggle(ch.id)} />
                <span className='flex-1 truncate text-sm'>{ch.name}</span>
                <Badge variant='outline' className='shrink-0 text-xs'>{CHANNEL_TYPES[ch.type as keyof typeof CHANNEL_TYPES] || `Type ${ch.type}`}</Badge>
              </label>
            ))}
          </div>
        </PopoverContent>
      </Popover>
      {selectedDisplay.length > 0 && (
        <div className='flex flex-wrap gap-1.5'>
          {selectedDisplay.map((ch) => (
            <Badge key={ch.id} variant='secondary' className='flex items-center gap-1 pr-1'>
              <span className='text-xs'>{ch.name}{ch.type != null && <span className='text-muted-foreground ml-1'>({CHANNEL_TYPES[ch.type as keyof typeof CHANNEL_TYPES] || ch.type})</span>}</span>
              <button type='button' onClick={() => handleToggle(ch.id)} className='hover:bg-destructive/20 ml-0.5 rounded-full p-0.5'><X className='h-3 w-3' /></button>
            </Badge>
          ))}
        </div>
      )}
    </div>
  )
}
