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

import React, { useMemo } from 'react';
import { Progress, Tag, Typography } from '@douyinfe/semi-ui';
import {
  Award,
  Flame,
  Shield,
  Sparkles,
  TrendingUp,
  Trophy,
} from 'lucide-react';
import { toUsdt } from './api';
import { resolveSymbolMeta } from './symbols';

const { Text } = Typography;

const ARENA_STATS_KEY = 'trade-arena-stats:v1';

function readArenaStats() {
  try {
    const raw = window.localStorage.getItem(ARENA_STATS_KEY);
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
}

const TRADER_TIERS = [
  { key: 'rookie', name: '见习交易员', minScore: 0, color: 'grey' },
  { key: 'bronze', name: '青铜散户', minScore: 100, color: 'amber' },
  { key: 'silver', name: '白银游资', minScore: 500, color: 'blue' },
  { key: 'gold', name: '黄金操盘手', minScore: 2000, color: 'orange' },
  { key: 'platinum', name: '铂金基金经理', minScore: 8000, color: 'cyan' },
  { key: 'diamond', name: '钻石对冲大师', minScore: 25000, color: 'violet' },
  { key: 'legend', name: '传奇华尔街之狼', minScore: 80000, color: 'red' },
];

// 交易员段位与成就卡：基于真实模拟盘资产、盈亏、持仓多样性及 K 线盲测战绩展示段位与徽章。
const TraderRankCard = ({ self, perUnit, futuresPositions = [], t }) => {
  const { tier, nextTier, percent, score, badges } = useMemo(() => {
    const equity = toUsdt(self?.valuation?.equity, perUnit);
    const totalPnl = toUsdt(self?.total_pnl, perUnit);
    const holdings = self?.valuation?.holdings || [];
    const arena = readArenaStats();

    // 综合分：总资产 + 累计正盈利加权 + 持仓品种多样性 + 盲测训练加分
    const diversityBonus = (holdings.length + futuresPositions.length) * 40;
    const arenaBonus = (arena?.rounds || 0) * 35;
    const rawScore = Math.max(
      0,
      Math.round(
        equity + Math.max(0, totalPnl) * 4 + diversityBonus + arenaBonus,
      ),
    );

    let currentIdx = 0;
    for (let i = 0; i < TRADER_TIERS.length; i++) {
      if (rawScore >= TRADER_TIERS[i].minScore) currentIdx = i;
    }
    const cur = TRADER_TIERS[currentIdx];
    const next = TRADER_TIERS[currentIdx + 1] || null;
    const pct = next
      ? Math.min(
          100,
          Math.max(
            0,
            Math.round(
              ((rawScore - cur.minScore) / (next.minScore - cur.minScore)) *
                100,
            ),
          ),
        )
      : 100;

    const hasAiHolding = [...holdings, ...futuresPositions].some((pos) => {
      const meta = resolveSymbolMeta(pos.symbol);
      return (
        meta &&
        (meta.sector === 'llm' ||
          meta.sector === 'ai_infra' ||
          meta.sector === 'semi')
      );
    });
    const list = [
      {
        key: 'funded',
        label: '启航入金',
        unlocked: equity > 0 || (self?.account?.total_in || 0) > 0,
        Icon: Sparkles,
        color: 'blue',
      },
      {
        key: 'profit',
        label: '实现盈利',
        unlocked: totalPnl > 0,
        Icon: TrendingUp,
        color: 'green',
      },
      {
        key: 'ai_hunter',
        label: 'AI 赛道猎手',
        unlocked: hasAiHolding,
        Icon: Flame,
        color: 'violet',
      },
      {
        key: 'multi_market',
        label: '现合双修',
        unlocked: holdings.length > 0 && futuresPositions.length > 0,
        Icon: Shield,
        color: 'cyan',
      },
      {
        key: 'arena',
        label: '盲测训练师',
        unlocked: (arena?.rounds || 0) >= 1,
        Icon: Award,
        color: 'amber',
      },
      {
        key: 'master',
        label: '百战高胜率',
        unlocked: (arena?.bestReturn || 0) >= 15 || totalPnl >= 100,
        Icon: Trophy,
        color: 'orange',
      },
    ];

    return {
      tier: cur,
      nextTier: next,
      percent: pct,
      score: rawScore,
      badges: list,
    };
  }, [self, perUnit, futuresPositions]);

  const unlockedCount = badges.filter((b) => b.unlocked).length;

  return (
    <div className='trade-card trade-rank-card flex flex-col gap-3'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div className='flex items-center gap-2.5'>
          <span className='trade-rank-emblem'>
            <Trophy size={18} />
          </span>
          <div className='flex flex-col leading-tight'>
            <div className='flex items-center gap-2'>
              <Text strong className='text-base'>
                {t(tier.name)}
              </Text>
              <Tag size='small' color={tier.color} type='solid'>
                {t('交易积分 {{score}}', { score })}
              </Tag>
            </div>
            <Text type='tertiary' size='small'>
              {nextTier
                ? t('距离「{{next}}」还差 {{gap}} 积分', {
                    next: t(nextTier.name),
                    gap: Math.max(0, nextTier.minScore - score),
                  })
                : t('已达最高交易员段位')}
            </Text>
          </div>
        </div>
        <Text type='tertiary' size='small' className='trade-num'>
          {t('已解锁成就 {{done}} / {{total}}', {
            done: unlockedCount,
            total: badges.length,
          })}
        </Text>
      </div>

      <Progress
        percent={percent}
        size='small'
        stroke='var(--semi-color-primary)'
      />

      <div className='flex flex-wrap gap-2'>
        {badges.map(({ key, label, unlocked, Icon, color }) => (
          <Tag
            key={key}
            size='large'
            color={unlocked ? color : 'grey'}
            type={unlocked ? 'light' : 'ghost'}
            className={unlocked ? '' : 'opacity-50'}
          >
            <span className='inline-flex items-center gap-1'>
              <Icon size={13} />
              {t(label)}
            </span>
          </Tag>
        ))}
      </div>
    </div>
  );
};

export default TraderRankCard;
