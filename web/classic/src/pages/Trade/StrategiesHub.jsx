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

import React, { useState } from 'react';
import { Grid3x3, Layers, ShieldCheck } from 'lucide-react';
import BasketsView from './BasketsView';
import FundingArbView from './FundingArbView';
import GridBotView from './GridBotView';

const SUB_TABS = [
  { key: 'baskets', label: '主题组合篮子', Icon: Layers },
  { key: 'arb', label: '资金费率套利', Icon: ShieldCheck },
  { key: 'grid', label: '智能网格挂单', Icon: Grid3x3 },
];

// 策略与稳健收益中心：主题组合一键配置、期现资金费率套利、智能网格挂单三大策略均直接复用真实模拟盘撮合。
const StrategiesHub = ({
  initialTab = 'baskets',
  self,
  perUnit,
  spotMarket,
  futuresMarket,
  spotQuotes,
  futuresQuotes,
  onOpenSpot,
  onOpenFutures,
  onAccountChanged,
  t,
}) => {
  const [tab, setTab] = useState(initialTab);

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-wrap gap-2'>
        {SUB_TABS.map(({ key, label, Icon }) => (
          <button
            key={key}
            type='button'
            className={`trade-hub-tab${tab === key ? ' is-active' : ''}`}
            onClick={() => setTab(key)}
          >
            <Icon size={15} />
            <span>{t(label)}</span>
          </button>
        ))}
      </div>

      {tab === 'baskets' && (
        <BasketsView
          self={self}
          perUnit={perUnit}
          spotMarket={spotMarket}
          futuresMarket={futuresMarket}
          spotQuotes={spotQuotes}
          futuresQuotes={futuresQuotes}
          onOpenSpot={onOpenSpot}
          onOpenFutures={onOpenFutures}
          onAccountChanged={onAccountChanged}
          t={t}
        />
      )}
      {tab === 'arb' && (
        <FundingArbView
          self={self}
          perUnit={perUnit}
          spotMarket={spotMarket}
          futuresMarket={futuresMarket}
          spotQuotes={spotQuotes}
          futuresQuotes={futuresQuotes}
          onAccountChanged={onAccountChanged}
          t={t}
        />
      )}
      {tab === 'grid' && (
        <GridBotView
          self={self}
          perUnit={perUnit}
          spotMarket={spotMarket}
          futuresMarket={futuresMarket}
          spotQuotes={spotQuotes}
          futuresQuotes={futuresQuotes}
          onAccountChanged={onAccountChanged}
          t={t}
        />
      )}
    </div>
  );
};

export default StrategiesHub;
