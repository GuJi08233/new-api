import { useQuery } from '@tanstack/react-query'
import { Copy, ExternalLink, Loader2, RefreshCcw } from 'lucide-react'
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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Separator } from '@/components/ui/separator'

import { getDeployment, listDeploymentContainers } from '../../api'

export function ViewDetailsDialog({
  open,
  onOpenChange,
  deploymentId,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  deploymentId: string | number | null
}) {
  const { t } = useTranslation()

  const {
    data: detailsRes,
    isLoading: isLoadingDetails,
    refetch: refetchDetails,
    isFetching: isFetchingDetails,
  } = useQuery({
    queryKey: ['deployment-details', deploymentId],
    queryFn: () => (deploymentId ? getDeployment(deploymentId) : null),
    enabled: open && deploymentId !== null,
  })

  const {
    data: containersRes,
    isLoading: isLoadingContainers,
    refetch: refetchContainers,
    isFetching: isFetchingContainers,
  } = useQuery({
    queryKey: ['deployment-details-containers', deploymentId],
    queryFn: () =>
      deploymentId ? listDeploymentContainers(deploymentId) : null,
    enabled: open && deploymentId !== null,
  })

  const details = detailsRes?.data
  const containers = useMemo(() => {
    const items = containersRes?.data?.containers
    return Array.isArray(items) ? items : []
  }, [containersRes?.data?.containers])

  const locations = useMemo(() => {
    const items = details?.locations
    if (!Array.isArray(items)) {
      return []
    }
    return items
      .map((x) => {
        if (!x || typeof x !== 'object') {
          return null
        }
        const name = (x as Record<string, unknown>)?.name
        const iso2 = (x as Record<string, unknown>)?.iso2
        const id = (x as Record<string, unknown>)?.id
        return `${String(name ?? id ?? '')}${iso2 ? ` (${iso2})` : ''}`.trim()
      })
      .filter(Boolean) as string[]
  }, [details])

  const handleCopyId = async () => {
    if (deploymentId === null || deploymentId === undefined) {
      return
    }
    try {
      await navigator.clipboard.writeText(String(deploymentId))
      toast.success(t('Copied'))
    } catch {
      toast.error(t('Copy failed'))
    }
  }

  const handleRefresh = () => {
    refetchDetails()
    refetchContainers()
  }

  const payloadJson = useMemo(() => {
    if (!details) return ''
    try {
      return JSON.stringify(details, null, 2)
    } catch {
      return ''
    }
  }, [details])
  const isDetailsLoading = isLoadingDetails || isLoadingContainers
  const showDetailsError = !isDetailsLoading && !detailsRes?.success
  const showDetailsContent = !isDetailsLoading && detailsRes?.success

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[calc(100dvh-2rem)] overflow-hidden max-sm:w-screen max-sm:max-w-none max-sm:rounded-none max-sm:p-4 sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>{t('Deployment details')}</DialogTitle>
        </DialogHeader>

        <div className='max-h-[calc(100dvh-8.5rem)] space-y-3 overflow-y-auto py-2 pr-1 sm:max-h-[72vh] sm:space-y-4'>
            <div className='flex flex-wrap items-center justify-between gap-2'>
            <div className='text-muted-foreground text-sm'>
              {t('Deployment ID')}:{' '}
              <span className='font-mono'>{deploymentId}</span>
            </div>
            <div className='grid grid-cols-2 gap-2 sm:flex sm:items-center'>
              <Button variant='outline' size='sm' onClick={handleCopyId}>
                <Copy className='mr-2 h-4 w-4' />
                {t('Copy')}
              </Button>
              <Button
                variant='outline'
                size='sm'
                onClick={handleRefresh}
                disabled={isFetchingDetails || isFetchingContainers}
              >
                {isFetchingDetails || isFetchingContainers ? (
                  <Loader2 className='mr-2 h-4 w-4 animate-spin' />
                ) : (
                  <RefreshCcw className='mr-2 h-4 w-4' />
                )}
                {t('Refresh')}
              </Button>
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)} className='w-full sm:w-auto'>
            {t('Close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
