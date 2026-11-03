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
import { useTranslation } from 'react-i18next';
import { getRealmNames } from '../../sect/text';

// 每条变强的路点了去哪：历练页的页签，洞府还要打开哪一块。突破、闭关在主角卡上，地图页签上面就是。
const TIP_TARGETS = {
  break: { tab: 'map' },
  cultivate: { tab: 'map' },
  team: { tab: 'map' },
  spring: { tab: 'cave', view: 'rooms' },
  cuiti: { tab: 'cave', view: 'furnace' },
  jingang: { tab: 'cave', view: 'furnace' },
  longhu: { tab: 'cave', view: 'furnace' },
  jiuzhuan: { tab: 'cave', view: 'furnace' },
  refine_vital: { tab: 'cave', view: 'forge' },
  refine_guard: { tab: 'cave', view: 'forge' },
  refine_edge: { tab: 'cave', view: 'forge' },
  beast: { tab: 'cave', view: 'beasts' },
  wear: { tab: 'gear' },
  potions: { tab: 'gear' },
  upgrade: { tab: 'cards' },
};

// 输法与变强建议都由服务端重放结果给出；点建议跳到对应的养成页面。
const DefeatReview = ({ events, onGo }) => {
  const { t } = useTranslation();
  const review = events.find((ev) => ev.k === 'hero_review');
  if (!review) return null;
  const realms = getRealmNames(t);
  const tips = events.filter((ev) => ev.k === 'hero_tip');
  const causes = {
    close: t('差一点就赢了：敌人只剩不到四分之一的生命，补上一点就能翻过来。'),
    burst: t('被打得太快：生命和护体扛不住敌人的攻势，先把这两样补上。'),
    slow: t('输出跟不上：先提升伤害，试着换一套法宝与法术。'),
  };
  const tipText = ({ s: key, n }) => {
    switch (key) {
      case 'break':
        return t('突破到{{realm}}：最大生命、力量与法宝栏都会涨', {
          realm: realms[n],
        });
      case 'cultivate':
        return t('闭关或服聚气丹修到下一层，每层最大生命 +{{n}}', { n });
      case 'spring':
        return t('扩建洞府的灵泉，每级最大生命 +{{n}}', { n });
      case 'cuiti':
        return t('服淬体丹永久加最大生命，这个境界还能加 {{n}} 点', { n });
      case 'jingang':
        return t('服一颗金刚丹，之后几场开战先有护体(下品 {{n}} 点)', { n });
      case 'longhu':
        return t('服一颗龙虎丹，之后几场开战力量 +{{n}}', { n });
      case 'jiuzhuan':
        return t('炼九转金丹，每颗永久力量 +1，还能服 {{n}} 颗', { n });
      case 'refine_vital':
        return t('在器室祭炼戴着的命路法宝，每级最大生命 +{{n}}', { n });
      case 'refine_guard':
        return t('在器室祭炼戴着的御路法宝，每级开战护体 +{{n}}', { n });
      case 'refine_edge':
        return t('在器室祭炼戴着的锋路法宝，每 {{n}} 级力量 +1', { n });
      case 'wear':
        return t('法宝栏还空着 {{n}} 格，把收着的法宝戴上', { n });
      case 'potions':
        return t('行囊里没有丹符：到丹房炼几枚，开战时会自动带上');
      case 'team':
        return t('带上随行弟子(现在有 {{n}} 名能去)，他们会给斗法加成', {
          n,
        });
      case 'beast':
        return t('让一只灵兽出战，为整场斗法提供加成');
      case 'upgrade':
        return t('还有 {{n}} 张功法可以参悟，提高斗法伤害', { n });
      default:
        return null;
    }
  };

  return (
    <div className='jn-review'>
      <p className='jn-review-cause'>{causes[review.s]}</p>
      {tips.length > 0 && (
        <>
          <h4 className='jn-review-head'>{t('可以从这里变强')}</h4>
          <ul className='jn-review-tips'>
            {tips.map((tip) => (
              <li key={tip.s}>
                <span>{tipText(tip)}</span>
                {TIP_TARGETS[tip.s] && (
                  <Button
                    size='small'
                    theme='borderless'
                    onClick={() => onGo(TIP_TARGETS[tip.s])}
                  >
                    {t('去看看')}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
};

export default DefeatReview;
