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
import { Button, InputNumber, Modal } from '@douyinfe/semi-ui';
import { ShoppingBag } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { StageMessage } from './SideBox';
import { formatCoins, toCoinCents } from './api';
import './game-shop.css';

// 一次最多买几件，与服务端 gameShopMaxCount 一致。
const MAX_COUNT = 20;

// 一件道具：图标、名称、说明、已有几件，以及买几件、花多少游戏币。
const ShopCard = ({
  item,
  text,
  Icon,
  owned,
  maxStack,
  symbol,
  busy,
  enabled,
  onBuy,
}) => {
  const { t } = useTranslation();
  const [count, setCount] = useState(1);
  const room = Math.max(0, Math.min(MAX_COUNT, maxStack - owned));
  const amount = Math.min(count, room);
  // 按分相乘，避免 0.1 × 3 这样的浮点误差。
  const total = (toCoinCents(item.price) * amount) / 100;
  return (
    <div className='game-shop-card'>
      <div className='game-shop-card-head'>
        <span className='game-shop-icon'>{Icon && <Icon size={18} />}</span>
        <strong>{text?.name || item.id}</strong>
        <span className='game-shop-owned'>{t('已有 {{n}}', { n: owned })}</span>
      </div>
      <p>{text?.desc}</p>
      <div className='game-shop-card-foot'>
        <span className='game-shop-price'>
          {formatCoins(item.price)}
          <small>{symbol}</small>
        </span>
        <InputNumber
          size='small'
          min={1}
          max={Math.max(room, 1)}
          value={amount || 1}
          disabled={!enabled || room === 0}
          onChange={(value) => setCount(Math.max(1, Number(value) || 1))}
          style={{ width: 76 }}
        />
        <Button
          size='small'
          theme='solid'
          disabled={!enabled || busy || room === 0}
          onClick={() => onBuy(item.id, amount, total)}
        >
          {room === 0 ? t('已放满') : t('购买')}
        </Button>
      </div>
    </div>
  );
};

// 长期游戏的道具商店(深渊远征、问道宗门共用)。shop 是游戏信息里的商店部分，texts 与 icons 按道具标识给出名称说明与图标，
// owned 是背包里每种道具的件数。买之前确认一次：花掉的游戏币不退回。
const GameShop = ({
  shop,
  texts,
  icons,
  owned,
  maxStack,
  symbol,
  busy,
  note,
  onBuy,
}) => {
  const { t } = useTranslation();
  if (!shop) return null;
  const buy = (id, count, total) =>
    Modal.confirm({
      title: t('购买道具'),
      content: t(
        '花 {{total}} {{symbol}} 购买 {{name}} × {{count}}？花掉的游戏币不退回，道具也不能换回游戏币。',
        {
          total: formatCoins(total),
          symbol,
          name: texts[id]?.name || id,
          count,
        },
      ),
      onOk: () => onBuy(id, count),
    });
  return (
    <div className='game-shop'>
      <div className='game-shop-title'>
        <ShoppingBag size={16} />
        <strong>{t('道具商店')}</strong>
        {note && <span>{note}</span>}
      </div>
      {!shop.enabled && (
        <StageMessage>{t('道具商店已关闭，背包里的道具照常能用')}</StageMessage>
      )}
      <div className='game-shop-grid'>
        {shop.items.map((item) => (
          <ShopCard
            key={item.id}
            item={item}
            text={texts[item.id]}
            Icon={icons[item.id]}
            owned={owned?.[item.id] || 0}
            maxStack={maxStack}
            symbol={symbol}
            busy={busy}
            enabled={shop.enabled}
            onBuy={buy}
          />
        ))}
      </div>
    </div>
  );
};

export default GameShop;
