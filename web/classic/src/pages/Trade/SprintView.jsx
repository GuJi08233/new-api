/*
Copyright (C) 2025 QuantumNous

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

import React, { useCallback, useEffect, useState } from 'react';
import {
  Banner,
  Button,
  Spin,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { Flame, RefreshCw, Trophy, Users } from 'lucide-react';
import { showError, showSuccess, timestamp2string } from '../../helpers';
import {
  formatDuration,
  formatSignedUsdt,
  formatUsdt,
  tradeGet,
  tradePost,
  trendClass,
} from './api';

const { Text, Title } = Typography;

// 闪电冲榜赛：4 小时闪电赛 & 每日收益率赛。报名费进入奖池，按扣除存取款干扰后的赛段真实收益率排名自动发奖。
const SprintView = ({ perUnit, onAccountChanged, t }) => {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [joining, setJoining] = useState('');
  const [now, setNow] = useState(() => Math.floor(Date.now() / 1000));

  const load = useCallback(async () => {
    setLoading(true);
    const res = await tradeGet('/api/trade/sprints', t);
    setLoading(false);
    if (res.data) setData(res.data);
  }, [t]);

  useEffect(() => {
    load();
    const timer = setInterval(() => {
      if (!document.hidden) load();
    }, 10000);
    return () => clearInterval(timer);
  }, [load]);

  useEffect(() => {
    const timer = setInterval(
      () => setNow(Math.floor(Date.now() / 1000)),
      1000,
    );
    return () => clearInterval(timer);
  }, []);

  const join = async (kind) => {
    setJoining(kind);
    const res = await tradePost('/api/trade/sprints/join', { kind }, t);
    setJoining('');
    if (res.error) {
      showError(res.error);
      return;
    }
    showSuccess(t('已报名参赛！赛段内交易收益率实时参与排名'));
    onAccountChanged?.();
    load();
  };

  if (!data && loading) {
    return (
      <div className='flex justify-center py-10'>
        <Spin size='large' />
      </div>
    );
  }

  const standingColumns = [
    {
      title: t('名次'),
      dataIndex: 'rank',
      width: 68,
      render: (rank) => (
        <span className={`trade-rank trade-rank-${rank}`}>{rank}</span>
      ),
    },
    {
      title: t('选手'),
      dataIndex: 'name',
      render: (name, row) => (
        <div className='flex items-center gap-1.5'>
          <Text strong={row.me}>{name}</Text>
          {row.me && (
            <Tag size='small' color='blue'>
              {t('我')}
            </Tag>
          )}
        </div>
      ),
    },
    {
      title: t('赛段收益率'),
      dataIndex: 'return_rate',
      align: 'right',
      render: (rate) => {
        const pct = Number(rate || 0) * 100;
        return (
          <span className={`trade-num font-semibold ${trendClass(pct)}`}>
            {pct > 0 ? '+' : ''}
            {pct.toFixed(2)}%
          </span>
        );
      },
    },
    {
      title: t('赛段盈亏'),
      dataIndex: 'profit',
      align: 'right',
      render: (val) => (
        <span className={`trade-num ${trendClass(val)}`}>
          {formatSignedUsdt(val, perUnit)} USDT
        </span>
      ),
    },
    {
      title: t('预计奖金'),
      dataIndex: 'prize',
      align: 'right',
      render: (val) => (
        <span className='trade-num font-semibold text-[#f59e0b]'>
          {val > 0 ? `${formatUsdt(val, perUnit)} USDT` : '--'}
        </span>
      ),
    },
  ];

  return (
    <div className='flex flex-col gap-4'>
      <Banner
        type='info'
        closeIcon={null}
        description={t(
          '报名后立即记录当前净值与净转入，赛段内现货、合约、短线交易盈亏均计入收益率（盘中转入资金自动扣减基数，无法靠加金刷榜）。报名费 100% 进入奖池（抽成 {{fee}}%），不足 2 人自动全额退费。',
          { fee: ((data?.fee_bps || 500) / 100).toFixed(0) },
        )}
      />

      <div className='grid grid-cols-1 gap-4 xl:grid-cols-2'>
        {(data?.active || []).map((entry) => {
          const {
            sprint,
            joined,
            can_join: canJoin,
            split_bps: splitBps,
            standings,
            me,
          } = entry;
          const isDaily = sprint.kind === 'daily';
          const endRem = Math.max(0, sprint.end_time - now);
          const regRem = Math.max(0, sprint.register_deadline - now);

          return (
            <div key={sprint.id} className='trade-card flex flex-col gap-3'>
              <div className='flex flex-wrap items-center justify-between gap-2'>
                <div className='flex items-center gap-2'>
                  {isDaily ? (
                    <Trophy size={18} style={{ color: '#f59e0b' }} />
                  ) : (
                    <Flame
                      size={18}
                      className='text-[var(--semi-color-primary)]'
                    />
                  )}
                  <Title heading={5} className='!mb-0'>
                    {isDaily ? t('每日收益率冲榜赛') : t('4 小时闪电冲榜赛')}
                  </Title>
                  <Tag size='small' color={isDaily ? 'amber' : 'blue'}>
                    {sprint.period_key}
                  </Tag>
                </div>
                <Button
                  size='small'
                  theme='borderless'
                  icon={<RefreshCw size={13} />}
                  onClick={load}
                />
              </div>

              <div className='grid grid-cols-3 gap-2.5 trade-num'>
                <div className='trade-stat-chip !p-2.5'>
                  <Text type='tertiary' size='small' className='!text-[11px]'>
                    {t('当前总奖池')}
                  </Text>
                  <div className='text-base font-bold text-[#f59e0b]'>
                    {formatUsdt(sprint.prize_pool, perUnit)} USDT
                  </div>
                </div>
                <div className='trade-stat-chip !p-2.5'>
                  <Text type='tertiary' size='small' className='!text-[11px]'>
                    {t('已报名人数')}
                  </Text>
                  <div className='text-base font-bold inline-flex items-center gap-1'>
                    <Users size={14} />
                    {sprint.participants}
                  </div>
                </div>
                <div className='trade-stat-chip !p-2.5'>
                  <Text type='tertiary' size='small' className='!text-[11px]'>
                    {t('距离结算')}
                  </Text>
                  <div className='text-base font-bold'>
                    {formatDuration(endRem, t)}
                  </div>
                </div>
              </div>

              <div className='flex flex-wrap items-center justify-between gap-2'>
                <div className='flex flex-wrap items-center gap-1.5'>
                  <Text type='tertiary' size='small'>
                    {t('分奖比例')}：
                  </Text>
                  {(splitBps || []).map((bps, idx) => (
                    <Tag key={idx} size='small' color='amber' type='light'>
                      #{idx + 1} {(bps / 100).toFixed(0)}%
                    </Tag>
                  ))}
                </div>
                {joined ? (
                  <Tag color='green' size='large'>
                    {me
                      ? t('已参赛 · 当前第 {{rank}} 名', { rank: me.rank })
                      : t('已参赛')}
                  </Tag>
                ) : (
                  <Button
                    theme='solid'
                    disabled={!canJoin}
                    loading={joining === sprint.kind}
                    onClick={() => join(sprint.kind)}
                  >
                    {canJoin
                      ? t('报名参赛 ({{fee}} USDT)', {
                          fee: formatUsdt(sprint.entry_fee, perUnit, 0),
                        })
                      : regRem === 0
                        ? t('本场报名已截止')
                        : t('暂不可报名')}
                  </Button>
                )}
              </div>

              <Table
                size='small'
                rowKey='user_id'
                columns={standingColumns}
                dataSource={standings || []}
                pagination={false}
                empty={
                  <Text type='tertiary'>
                    {t('虚位以待，成为本场首位报名选手')}
                  </Text>
                }
              />
            </div>
          );
        })}
      </div>

      {(data?.recent || []).filter((s) => s.status === 'settled').length >
        0 && (
        <div className='trade-card flex flex-col gap-2'>
          <Title heading={6} className='!mb-0'>
            {t('往期已结算赛事')}
          </Title>
          <Table
            size='small'
            rowKey='id'
            pagination={false}
            dataSource={(data?.recent || []).filter(
              (s) => s.status === 'settled',
            )}
            columns={[
              { title: t('期次'), dataIndex: 'period_key' },
              {
                title: t('类型'),
                dataIndex: 'kind',
                render: (v) => (v === 'daily' ? t('每日赛') : t('4 小时赛')),
              },
              {
                title: t('参赛人数'),
                dataIndex: 'participants',
                align: 'right',
              },
              {
                title: t('总奖池'),
                dataIndex: 'prize_pool',
                align: 'right',
                render: (v) => `${formatUsdt(v, perUnit)} USDT`,
              },
              {
                title: t('结算时间'),
                dataIndex: 'settled_at',
                render: (v) => (v ? timestamp2string(v) : '--'),
              },
            ]}
          />
        </div>
      )}
    </div>
  );
};

export default SprintView;
