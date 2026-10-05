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

import React, { useEffect, useState } from 'react';
import {
  Button,
  Empty,
  Radio,
  RadioGroup,
  Spin,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { RefreshCw } from 'lucide-react';
import { timestamp2string } from '../../helpers';
import { formatSignedUsdt, formatUsdt, tradeGet, trendClass } from './api';

const { Text } = Typography;

// formatReturn 把收益率(小数)显示成带正负号的百分比。
function formatReturn(rate) {
  const percent = Number(rate || 0) * 100;
  return `${percent > 0 ? '+' : ''}${percent.toFixed(2)}%`;
}

// 排行榜：成交过的用户按累计盈亏、收益率或总资产排名，列出前 100 名，上面是自己的名次。名单在服务端隔几分钟才重算一次。
const LeaderboardView = ({ perUnit, t }) => {
  const [sort, setSort] = useState('profit');
  const [board, setBoard] = useState(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [refreshKey, setRefreshKey] = useState(0);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    tradeGet('/api/trade/leaderboard', t, { sort }).then((res) => {
      if (!alive) return;
      setLoading(false);
      if (res.error) {
        setError(res.error);
        return;
      }
      setError('');
      setBoard(res.data);
    });
    return () => {
      alive = false;
    };
  }, [sort, t, refreshKey]);

  if (!board) {
    return error ? <Empty title={error} /> : <Spin size='large' />;
  }

  const me = board.me;
  const minutes = Math.round(board.refresh_seconds / 60);
  const mine = [
    [t('我的排名'), me ? `#${me.rank}` : '--', ''],
    [
      t('累计盈亏'),
      me ? `${formatSignedUsdt(me.profit, perUnit)} USDT` : '--',
      trendClass(me?.profit),
    ],
    [
      t('收益率'),
      me ? formatReturn(me.return_rate) : '--',
      trendClass(me?.return_rate),
    ],
    [t('总资产'), me ? `${formatUsdt(me.equity, perUnit)} USDT` : '--', ''],
  ];

  const columns = [
    {
      title: t('排名'),
      dataIndex: 'rank',
      width: 72,
      render: (rank) => (
        <span className={`trade-rank trade-rank-${rank}`}>{rank}</span>
      ),
    },
    {
      title: t('用户'),
      dataIndex: 'name',
      render: (name, item) => (
        <div className='flex items-center gap-2'>
          <Text ellipsis={{ showTooltip: true }} style={{ maxWidth: 240 }}>
            {name}
          </Text>
          {item.me && (
            <Tag size='small' color='blue' className='shrink-0'>
              {t('我')}
            </Tag>
          )}
        </div>
      ),
    },
    {
      title: t('累计盈亏'),
      dataIndex: 'profit',
      align: 'right',
      render: (profit) => (
        <span className={`trade-num ${trendClass(profit)}`}>
          {formatSignedUsdt(profit, perUnit)} USDT
        </span>
      ),
    },
    {
      title: t('收益率'),
      dataIndex: 'return_rate',
      align: 'right',
      render: (rate) => (
        <span className={`trade-num ${trendClass(rate)}`}>
          {formatReturn(rate)}
        </span>
      ),
    },
    {
      title: t('总资产'),
      dataIndex: 'equity',
      align: 'right',
      render: (equity) => (
        <span className='trade-num'>{formatUsdt(equity, perUnit)} USDT</span>
      ),
    },
  ];

  return (
    <div className='flex flex-col gap-4'>
      <div className='trade-card flex flex-col gap-3'>
        <div className='grid grid-cols-2 gap-4 md:grid-cols-4'>
          {mine.map(([label, value, className]) => (
            <div key={label}>
              <Text type='tertiary' size='small'>
                {label}
              </Text>
              <div className={`trade-num text-lg font-semibold ${className}`}>
                {value}
              </div>
            </div>
          ))}
        </div>
        {!me && (
          <Text type='tertiary' size='small'>
            {t(
              '你还没有上榜：买入过现货或开过合约才会上榜，刚成交的要等名单下次更新。',
            )}
          </Text>
        )}
      </div>
      <div className='trade-card flex flex-col gap-3'>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <RadioGroup
            type='button'
            value={sort}
            onChange={(e) => setSort(e.target.value)}
          >
            <Radio value='profit'>{t('累计盈亏')}</Radio>
            <Radio value='return'>{t('收益率')}</Radio>
            <Radio value='equity'>{t('总资产')}</Radio>
          </RadioGroup>
          <div className='flex items-center gap-1'>
            {error && (
              <Text type='danger' size='small'>
                {error}
              </Text>
            )}
            <Text type='tertiary' size='small'>
              {t('共 {{count}} 人上榜，更新于 {{time}}', {
                count: board.total,
                time: timestamp2string(board.updated_at),
              })}
            </Text>
            <Button
              size='small'
              theme='borderless'
              type='tertiary'
              icon={<RefreshCw size={14} />}
              aria-label={t('刷新')}
              loading={loading}
              onClick={() => setRefreshKey((value) => value + 1)}
            />
          </div>
        </div>
        <Table
          size='small'
          rowKey='rank'
          columns={columns}
          dataSource={board.items}
          pagination={{ pageSize: 20 }}
          loading={loading}
          scroll={{ x: 640 }}
          onRow={(item) =>
            item.me ? { className: 'trade-leaderboard-me' } : {}
          }
          empty={<Text type='tertiary'>{t('还没有人上榜')}</Text>}
        />
        <Text type='tertiary' size='small'>
          {t(
            '累计盈亏是总资产减去净转入(累计转入减累计转出)，收益率是累计盈亏除以累计转入。名单列出前 100 名，每 {{minutes}} 分钟最多更新一次。',
            { minutes },
          )}
        </Text>
      </div>
    </div>
  );
};

export default LeaderboardView;
