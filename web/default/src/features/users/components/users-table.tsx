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
import { flexRender } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  DISABLED_ROW_DESKTOP,
  DISABLED_ROW_MOBILE,
  DataTablePagination,
  DataTableToolbar,
  MobileCardList,
  TableEmpty,
  TableSkeleton,
  useDataTable,
} from '@/components/data-table'
import { PageFooterPortal } from '@/components/layout'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { cn } from '@/lib/utils'

import { getUsers, searchUsers } from '../api'
import {
  USER_STATUS,
  getUserStatusOptions,
  getUserRoleOptions,
  isUserDeleted,
} from '../constants'
import type { User } from '../types'
import { DataTableBulkActions } from './data-table-bulk-actions'
import { useUsersColumns } from './users-columns'
import { useUsers } from './users-provider'

const route = getRouteApi('/_authenticated/users/')

function isDisabledUserRow(user: User) {
  return isUserDeleted(user) || user.status === USER_STATUS.DISABLED
}

export function UsersTable() {
  const { t } = useTranslation()
  const columns = useUsersColumns()
  const { refreshTrigger } = useUsers()
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
    columnFilters: [
      { columnId: 'status', searchKey: 'status', type: 'array' },
      { columnId: 'role', searchKey: 'role', type: 'array' },
      { columnId: 'group', searchKey: 'group', type: 'string' },
    ],
  })
  const statusFilter =
    (columnFilters.find((filter) => filter.id === 'status')?.value as
      | string[]
      | undefined) ?? []
  const roleFilter =
    (columnFilters.find((filter) => filter.id === 'role')?.value as
      | string[]
      | undefined) ?? []
  const groupFilter =
    (columnFilters.find((filter) => filter.id === 'group')?.value as string) ??
    ''

  // Fetch data with React Query
  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'users',
      pagination.pageIndex + 1,
      pagination.pageSize,
      globalFilter,
      statusFilter,
      roleFilter,
      groupFilter,
      refreshTrigger,
    ],
    queryFn: async () => {
      const hasFilter = globalFilter?.trim()
      const hasColumnFilter =
        statusFilter.length > 0 || roleFilter.length > 0 || Boolean(groupFilter)
      const params = {
        p: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
      }

      const result =
        hasFilter || hasColumnFilter
          ? await searchUsers({
              ...params,
              keyword: globalFilter,
              status: statusFilter[0] ?? '',
              role: roleFilter[0] ?? '',
              group: groupFilter,
            })
          : await getUsers(params)

      if (!result.success) {
        toast.error(
          result.message || `Failed to ${hasFilter ? 'search' : 'load'} users`
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

  const users = data?.items || []

  const { table } = useDataTable({
    data: users,
    columns,
    enableRowSelection: true,
    columnFilters,
    globalFilter,
    pagination,
    globalFilterFn: (row, _columnId, filterValue) => {
      const searchValue = String(filterValue).toLowerCase()
      const fields = [
        row.getValue('username'),
        row.original.display_name,
        row.original.email,
      ]
      return fields.some((field) =>
        String(field || '')
          .toLowerCase()
          .includes(searchValue)
      )
    },
    onPaginationChange,
    onGlobalFilterChange,
    onColumnFiltersChange,
    manualPagination: true,
    manualFiltering: true,
    totalCount: data?.total || 0,
    ensurePageInRange,
  })

  return (
    <>
      <div className='space-y-3 sm:space-y-4'>
        <DataTableToolbar
          table={table}
          searchPlaceholder={t('Filter by username, name or email...')}
          filters={[
            {
              columnId: 'status',
              title: t('Status'),
              options: getUserStatusOptions(t),
            },
            {
              columnId: 'role',
              title: t('Role'),
              options: getUserRoleOptions(t),
            },
          ]}
        />
        {isMobile ? (
          <MobileCardList
            table={table}
            isLoading={isLoading}
            emptyTitle={t('No Users Found')}
            emptyDescription={t(
              'No users available. Try adjusting your search or filters.'
            )}
            getRowClassName={(row) =>
              isDisabledUserRow(row.original) ? DISABLED_ROW_MOBILE : undefined
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
                      {headerGroup.headers.map((header) => (
                        <TableHead key={header.id} colSpan={header.colSpan}>
                          {header.isPlaceholder
                            ? null
                            : flexRender(
                                header.column.columnDef.header,
                                header.getContext()
                              )}
                        </TableHead>
                      ))}
                    </TableRow>
                  ))}
                </TableHeader>
                <TableBody>
                  {isLoading ? (
                    <TableSkeleton table={table} keyPrefix='users-skeleton' />
                  ) : table.getRowModel().rows.length === 0 ? (
                    <TableEmpty
                      colSpan={columns.length}
                      title={t('No Users Found')}
                      description={t(
                        'No users available. Try adjusting your search or filters.'
                      )}
                    />
                  ) : (
                    table.getRowModel().rows.map((row) => {
                      const user = row.original

                      return (
                        <TableRow
                          key={row.id}
                          data-state={row.getIsSelected() && 'selected'}
                          className={cn(
                            isDisabledUserRow(user) && DISABLED_ROW_DESKTOP
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
