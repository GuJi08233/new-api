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
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { toast } from 'sonner'
import { api } from '@/lib/api'

type GroupPricingSelectorProps = {
  selectedGroup: string
  onGroupChange: (group: string) => void
  availableGroups: string[]
  selectedModels: string[]
  onSyncComplete: () => void
}

export function GroupPricingSelector({
  selectedGroup,
  onGroupChange,
  availableGroups,
  selectedModels,
  onSyncComplete,
}: GroupPricingSelectorProps) {
  const { t } = useTranslation()
  const [syncDialogOpen, setSyncDialogOpen] = useState(false)
  const [syncMode, setSyncMode] = useState<'from_global' | 'to_groups'>('from_global')
  const [targetGroups, setTargetGroups] = useState<string[]>([])
  const [isSyncing, setIsSyncing] = useState(false)
  const groupItems = useMemo(
    () =>
      availableGroups.map((group) => ({
        label: group === 'global' ? t('Global') : group,
        value: group,
      })),
    [availableGroups, t]
  )

  const handleSync = useCallback(async () => {
    setIsSyncing(true)
    try {
      const syncData: Record<string, unknown> = {
        source_group: syncMode === 'from_global' ? 'global' : selectedGroup,
        target_groups: syncMode === 'from_global' ? [selectedGroup] : targetGroups,
        from_global: syncMode === 'from_global',
      }

      if (syncMode === 'to_groups' && selectedModels.length > 0) {
        syncData.model_names = selectedModels
      }

      const res = await api.post('/api/option/sync_group_pricing', syncData)
      if (res.data.success) {
        toast.success(t('Sync successful'))
        onSyncComplete()
        setSyncDialogOpen(false)
        setTargetGroups([])
      } else {
        toast.error(res.data.message || t('Sync failed'))
      }
    } catch {
      toast.error(t('Sync failed, please try again'))
    } finally {
      setIsSyncing(false)
    }
  }, [syncMode, selectedGroup, targetGroups, selectedModels, onSyncComplete, t])

  return (
    <div className='flex flex-wrap items-center gap-2'>
      <Select
        items={groupItems}
        value={selectedGroup}
        onValueChange={(group) => {
          if (group) onGroupChange(group)
        }}
      >
        <SelectTrigger className='w-[180px]'>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectGroup>
            {groupItems.map((group) => (
              <SelectItem key={group.value} value={group.value}>
                {group.label}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>

      {selectedGroup === 'global' ? (
        <Button
          variant='outline'
          size='sm'
          disabled={selectedModels.length === 0}
          onClick={() => {
            setSyncMode('to_groups')
            setSyncDialogOpen(true)
          }}
        >
          {t('Sync selected to groups')}
          {selectedModels.length > 0 && ` (${selectedModels.length})`}
        </Button>
      ) : (
        <>
          <Button
            variant='outline'
            size='sm'
            onClick={() => {
              setSyncMode('from_global')
              setSyncDialogOpen(true)
            }}
          >
            {t('Sync from global')}
          </Button>
          <Button
            variant='outline'
            size='sm'
            disabled={selectedModels.length === 0}
            onClick={() => {
              setSyncMode('to_groups')
              setSyncDialogOpen(true)
            }}
          >
            {t('Sync selected to groups')}
            {selectedModels.length > 0 && ` (${selectedModels.length})`}
          </Button>
        </>
      )}

      <Dialog open={syncDialogOpen} onOpenChange={setSyncDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {syncMode === 'from_global'
                ? t('Sync from global to current group')
                : t('Sync selected models to groups')}
            </DialogTitle>
            <DialogDescription>
              {syncMode === 'from_global'
                ? t('This will copy all global configurations to group {{group}}', {
                    group: selectedGroup,
                  })
                : t('This will sync {{count}} selected models to target groups', {
                    count: selectedModels.length,
                  })}
            </DialogDescription>
          </DialogHeader>

          {syncMode === 'from_global' ? (
            <div className='space-y-4'>
              <p className='text-sm text-muted-foreground'>
                {t('After syncing, you can modify specific model prices for this group')}
              </p>
              <p className='text-sm text-orange-500'>
                {t('Warning: This will overwrite existing group configurations')}
              </p>
            </div>
          ) : (
            <div className='space-y-4'>
              <p className='text-sm text-muted-foreground'>
                {t('Selected models')}: {selectedModels.join(', ')}
              </p>
              <div className='space-y-2'>
                <p className='text-sm font-medium'>{t('Target groups')}</p>
                <div className='flex flex-wrap gap-2'>
                  {availableGroups
                    .filter((g) => g !== 'global' && g !== selectedGroup)
                    .map((group) => (
                      <Button
                        key={group}
                        variant={targetGroups.includes(group) ? 'default' : 'outline'}
                        size='sm'
                        onClick={() => {
                          setTargetGroups((prev) =>
                            prev.includes(group)
                              ? prev.filter((g) => g !== group)
                              : [...prev, group]
                          )
                        }}
                      >
                        {group}
                      </Button>
                    ))}
                </div>
              </div>
            </div>
          )}

          <DialogFooter>
            <Button variant='outline' onClick={() => setSyncDialogOpen(false)}>
              {t('Cancel')}
            </Button>
            <Button
              onClick={handleSync}
              disabled={
                isSyncing ||
                (syncMode === 'to_groups' && targetGroups.length === 0)
              }
            >
              {isSyncing ? t('Syncing...') : t('Confirm sync')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
