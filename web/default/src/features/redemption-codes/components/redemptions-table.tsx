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
import { getRouteApi } from '@tanstack/react-router'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  DISABLED_ROW_DESKTOP,
  DISABLED_ROW_MOBILE,
  DataTablePage,
  useDataTable,
} from '@/components/data-table'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'

import { getRedemptions, searchRedemptions } from '../api'
import {
  ERROR_MESSAGES,
  REDEMPTION_STATUS,
  getRedemptionStatusOptions,
} from '../constants'
import { isRedemptionExpired } from '../lib'
import type { Redemption } from '../types'
import { DataTableBulkActions } from './data-table-bulk-actions'
import { useRedemptionsColumns } from './redemptions-columns'
import { RedemptionsMobileList } from './redemptions-mobile-list'
import { useRedemptions } from './redemptions-provider'

const route = getRouteApi('/_authenticated/redemption-codes/')

function isDisabledRedemptionRow(redemption: Redemption) {
  return (
    redemption.status !== REDEMPTION_STATUS.ENABLED ||
    isRedemptionExpired(redemption.expired_time, redemption.status)
  )
}

export function RedemptionsTable() {
  const { t } = useTranslation()
  const columns = useRedemptionsColumns()
  const { refreshTrigger } = useRedemptions()
  const isMobile = useMediaQuery('(max-width: 640px)')

  const {
    globalFilter,
    onGlobalFilterChange,
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: { defaultPage: 1, defaultPageSize: isMobile ? 10 : 20 },
    globalFilter: { enabled: true, key: 'filter' },
    columnFilters: [{ columnId: 'status', searchKey: 'status', type: 'array' }],
  })
  const statusFilter =
    (columnFilters.find((filter) => filter.id === 'status')?.value as
      | string[]
      | undefined) ?? []
  const statusFilterValue = statusFilter[0] ?? ''

  // Fetch data with React Query
  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'redemptions',
      pagination.pageIndex + 1,
      pagination.pageSize,
      globalFilter,
      statusFilterValue,
      refreshTrigger,
    ],
    queryFn: async () => {
      const hasFilter = globalFilter?.trim()
      const hasStatusFilter = statusFilterValue !== ''
      const params = {
        p: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
      }

      const result =
        hasFilter || hasStatusFilter
          ? await searchRedemptions({
              ...params,
              keyword: globalFilter,
              status: statusFilterValue,
            })
          : await getRedemptions(params)

      if (!result.success) {
        toast.error(
          result.message ||
            t(
              hasFilter || hasStatusFilter
                ? ERROR_MESSAGES.SEARCH_FAILED
                : ERROR_MESSAGES.LOAD_FAILED
            )
        )
        return { items: [], total: 0 }
      }

      return {
        items: result.data?.items || [],
        total: result.data?.total || 0,
      }
    },
    placeholderData: (previousData) => previousData,
  })

  const redemptions = data?.items || []

  const { table } = useDataTable({
    data: redemptions,
    columns,
    enableRowSelection: true,
    columnFilters,
    globalFilter,
    pagination,
    globalFilterFn: (row, _columnId, filterValue) => {
      const name = String(row.getValue('name')).toLowerCase()
      const id = String(row.getValue('id'))
      const searchValue = String(filterValue).toLowerCase()

      return name.includes(searchValue) || id.includes(searchValue)
    },
    onPaginationChange,
    onGlobalFilterChange,
    onColumnFiltersChange,
    manualPagination: true,
    manualFiltering: true,
    totalCount: data?.total || 0,
    ensurePageInRange,
  })

  const redemptionStatusOptions = useMemo(
    () => getRedemptionStatusOptions(t),
    [t]
  )

  return (
    <>
      <div className='space-y-3 sm:space-y-4'>
        <DataTableToolbar
          table={table}
          searchPlaceholder={t('Filter by name or ID...')}
          filters={[
            {
              columnId: 'status',
              title: t('Status'),
              options: redemptionStatusOptions,
            },
          ]}
        />
        {isMobile ? (
          <MobileCardList
            table={table}
            isLoading={isLoading}
            emptyTitle={t('No Redemption Codes Found')}
            emptyDescription={t(
              'No redemption codes available. Create your first redemption code to get started.'
            )}
            getRowClassName={(row) =>
              isDisabledRedemptionRow(row.original)
                ? DISABLED_ROW_MOBILE
                : undefined
            }
          />
        ) : (
          <>
            <div
              className={cn(
                'overflow-hidden rounded-md border transition-opacity duration-150',
                isFetching && !isLoading && 'pointer-events-none opacity-50'
              )}
            >
              <Table>
                <TableHeader>
                  {table.getHeaderGroups().map((headerGroup) => (
                    <TableRow key={headerGroup.id}>
                      {headerGroup.headers.map((header) => {
                        return (
                          <TableHead key={header.id} colSpan={header.colSpan}>
                            {header.isPlaceholder
                              ? null
                              : flexRender(
                                  header.column.columnDef.header,
                                  header.getContext()
                                )}
                          </TableHead>
                        )
                      })}
                    </TableRow>
                  ))}
                </TableHeader>
                <TableBody>
                  {isLoading ? (
                    <TableSkeleton
                      table={table}
                      keyPrefix='redemptions-skeleton'
                    />
                  ) : table.getRowModel().rows.length === 0 ? (
                    <TableEmpty
                      colSpan={columns.length}
                      title={t('No Redemption Codes Found')}
                      description={t(
                        'No redemption codes available. Create your first redemption code to get started.'
                      )}
                    />
                  ) : (
                    table.getRowModel().rows.map((row) => {
                      const redemption = row.original

                      return (
                        <TableRow
                          key={row.id}
                          data-state={row.getIsSelected() && 'selected'}
                          className={cn(
                            isDisabledRedemptionRow(redemption) &&
                              DISABLED_ROW_DESKTOP
                          )}
                        >
                          {row.getVisibleCells().map((cell) => (
                            <TableCell key={cell.id}>
                              {flexRender(
                                cell.column.columnDef.cell,
                                cell.getContext()
                              )}
                            </TableCell>
                          ))}
                        </TableRow>
                      )
                    })
                  )}
                </TableBody>
              </Table>
            </div>
            <DataTableBulkActions table={table} />
          </>
        )}
      </div>
      <PageFooterPortal>
        <DataTablePagination table={table} />
      </PageFooterPortal>
    </>
  )
}
