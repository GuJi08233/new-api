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

import React from 'react';
import { Button } from '@douyinfe/semi-ui';
import {
  FlaskConical,
  Footprints,
  Pill,
  ScrollText,
  Sparkles,
  UserPlus,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import GameShop from '../common/GameShop';
import { useSectText } from './parts';
import { TALISMANS } from './text';

const ITEM_ICONS = {
  spirit_talisman: Sparkles,
  recruit_order: UserPlus,
  swift_talisman: Footprints,
  pill_heal: Pill,
  pill_qi: FlaskConical,
  pill_break: ScrollText,
};

// 商店页：上面是背包里的符箓(点一下就用)，下面是用游戏币买道具的商店。丹药买了直接进丹房，在丹房页里看数量。
const Shop = ({ info, sect, busy, act, onBuy }) => {
  const { t } = useTranslation();
  const text = useSectText();
  const items = sect.items || {};
  // 背包里的件数：符箓按背包计，丹药按丹房里的库存计。
  const owned = { ...items };
  for (const pill of ['heal', 'qi', 'break'])
    owned[`pill_${pill}`] = sect.pills?.[pill] || 0;
  const bag = TALISMANS.filter((id) => items[id] > 0);
  return (
    <div className='sc-shop'>
      <h4 className='sc-heading'>{t('背包')}</h4>
      {bag.length === 0 ? (
        <p className='sc-muted'>{t('背包里还没有符箓')}</p>
      ) : (
        <div className='sc-bag'>
          {bag.map((id) => {
            const Icon = ITEM_ICONS[id];
            return (
              <div key={id} className='sc-bag-item'>
                <Icon size={16} />
                <div>
                  <strong>
                    {text.items[id].name} × {items[id]}
                  </strong>
                  <span className='sc-muted'>{text.items[id].desc}</span>
                </div>
                <Button
                  size='small'
                  theme='solid'
                  disabled={busy}
                  onClick={() => act('item', { item: id })}
                >
                  {t('使用')}
                </Button>
              </div>
            );
          })}
        </div>
      )}
      <GameShop
        shop={info?.shop}
        texts={text.items}
        icons={ITEM_ICONS}
        owned={owned}
        maxStack={info?.max_item_stack || 99}
        symbol={info?.symbol}
        busy={busy}
        note={t('符箓放进背包，丹药直接放进丹房；用掉一件少一件')}
        onBuy={onBuy}
      />
    </div>
  );
};

export default Shop;
