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
import { Empty, Tag, Typography } from '@douyinfe/semi-ui';
import { Activity, Flame, Swords, Target, Trophy, Zap } from 'lucide-react';
import PredictionView from './PredictionView';
import ShortEventsView from './ShortEventsView';
import SprintView from './SprintView';

const { Text, Title } = Typography;

const TABS = [
  { key: 'pk', label: 'AI 擂台 PK', Icon: Swords, badge: '奖池/1v1' },
  { key: 'flash', label: '极速涨跌', Icon: Zap, badge: '1分起' },
  { key: 'range', label: '区间 & 触碰', Icon: Target, badge: '高赔率' },
  { key: 'prediction', label: '多币预测', Icon: Activity, badge: 'Polymarket' },
  { key: 'sprint', label: '闪电冲榜赛', Icon: Trophy, badge: '4h / 日赛' },
];

// 短线玩法中心：聚合 AI 资产 PK 擂台、全品种极速涨跌、区间高倍与触碰期权、多币种 Polymarket 预测与闪电冲榜赛。
const ShortPlayHub = ({
  initialTab = 'pk',
  self,
  perUnit,
  spotSymbols = [],
  futuresSymbols = [],
  spotQuotes = {},
  futuresQuotes = {},
  onAccountChanged,
  t,
}) => {
  const [tab, setTab] = useState(initialTab);

  useEffect(() => {
    setTab(initialTab);
  }, [initialTab]);

  const shortEnabled = self?.short_events?.enabled !== false;
  const sprintEnabled = self?.sprint?.enabled !== false;
  const predictionEnabled = self?.prediction !== false;

  return (
    <div className='flex flex-col gap-4'>
      <div className='trade-card trade-hero-banner flex flex-col gap-3'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div className='flex items-center gap-2'>
            <Flame size={20} className='text-[var(--semi-color-primary)]' />
            <Title heading={5} className='!mb-0'>
              {t('短线收益玩法中心')}
            </Title>
            <Tag size='small' color='amber'>
              {t('5 大短线模式')}
            </Tag>
          </div>
          <Text type='secondary' size='small'>
            {t(
              '1 分钟极速结算到 4 小时冲榜竞赛：支持全 47 个品种，收益直接计入模拟盘余额。',
            )}
          </Text>
        </div>

        <div className='flex flex-wrap gap-2'>
          {TABS.map(({ key, label, Icon, badge }) => (
            <button
              key={key}
              type='button'
              className={`trade-hub-tab${tab === key ? ' is-active' : ''}`}
              onClick={() => setTab(key)}
            >
              <Icon size={15} />
              <span>{t(label)}</span>
              <span className='trade-hub-tab-badge'>{t(badge)}</span>
            </button>
          ))}
        </div>
      </div>

      {tab === 'prediction' ? (
        predictionEnabled ? (
          <PredictionView
            self={self}
            perUnit={perUnit}
            onAccountChanged={onAccountChanged}
            t={t}
          />
        ) : (
          <Empty title={t('多币种预测暂未开放')} />
        )
      ) : tab === 'sprint' ? (
        sprintEnabled ? (
          <SprintView
            self={self}
            perUnit={perUnit}
            onAccountChanged={onAccountChanged}
            t={t}
          />
        ) : (
          <Empty title={t('闪电冲榜赛暂未开放')} />
        )
      ) : shortEnabled ? (
        <ShortEventsView
          section={tab}
          self={self}
          perUnit={perUnit}
          spotSymbols={spotSymbols}
          futuresSymbols={futuresSymbols}
          spotQuotes={spotQuotes}
          futuresQuotes={futuresQuotes}
          onAccountChanged={onAccountChanged}
          t={t}
        />
      ) : (
        <Empty title={t('短线事件玩法暂未开放')} />
      )}
    </div>
  );
};

export default ShortPlayHub;
