import { api } from '@/lib/api'
import type {
  ApiResponse,
  RiskMetric,
  RiskRankingsMeta,
  RiskUserDetailItem,
  RiskIpDetailItem,
} from './types'

export interface RankingsResponse {
  metric: RiskMetric
  items: unknown[]
  meta: RiskRankingsMeta
}

export async function getRiskRankings(
  metric: RiskMetric,
  hours: number,
  limit = 100
): Promise<ApiResponse<RankingsResponse>> {
  const res = await api.get(
    `/api/risk/rankings?metric=${metric}&hours=${hours}&limit=${limit}`
  )
  return res.data
}

export async function getRiskDetailByIp(
  ip: string,
  hours: number
): Promise<
  ApiResponse<{ type: string; value: string; items: RiskUserDetailItem[] }>
> {
  const res = await api.get(
    `/api/risk/detail?type=ip&value=${encodeURIComponent(ip)}&hours=${hours}`
  )
  return res.data
}

export async function getRiskDetailByUser(
  userId: number,
  hours: number
): Promise<
  ApiResponse<{ type: string; value: string; items: RiskIpDetailItem[] }>
> {
  const res = await api.get(
    `/api/risk/detail?type=user&value=${userId}&hours=${hours}`
  )
  return res.data
}

export async function disableUser(id: number): Promise<ApiResponse> {
  const res = await api.post('/api/user/manage', { id, action: 'disable' })
  return res.data
}

// 风控设置通过通用 option 接口读写
export async function getOptions(): Promise<
  ApiResponse<Array<{ key: string; value: string }>>
> {
  const res = await api.get('/api/option/')
  return res.data
}

export async function updateOption(
  key: string,
  value: string
): Promise<ApiResponse> {
  const res = await api.put('/api/option/', { key, value })
  return res.data
}
