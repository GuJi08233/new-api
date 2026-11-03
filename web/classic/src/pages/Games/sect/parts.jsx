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

import React, { createContext, useContext, useMemo } from 'react';
import { Tooltip } from '@douyinfe/semi-ui';
import { Gem, Hammer, Leaf, Star } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { formatAmount } from './format';
import { REGION_IDS, getRegionText } from '../wendao/duel/text';
export { formatAmount, formatLeft } from './format';
import {
  getSkillNames,
  getSkillText,
  getWeaponNames,
  getWeaponText,
} from '../wendao/doufa/text';
import {
  RESOURCES,
  discipleName,
  getBuildingText,
  getCropText,
  getDailyTaskText,
  getElementNames,
  getEventText,
  getItemText,
  getJobNames,
  getMissionText,
  getMoveText,
  getPathText,
  getPillText,
  getRareText,
  getRealmNames,
  getResourceNames,
  getSectNames,
  getTalentNames,
  getTraitText,
} from './text';

// 整个页面共用一份文案表与名字的写法，组件里直接取。
const TextContext = createContext(null);

export function SectTextProvider({ children }) {
  const { t, i18n } = useTranslation();
  const language = i18n.language;
  const text = useMemo(
    () => ({
      resources: getResourceNames(t),
      regions: getRegionText(t),
      realms: getRealmNames(t),
      talents: getTalentNames(t),
      elements: getElementNames(t),
      traits: getTraitText(t),
      jobs: getJobNames(t),
      buildings: getBuildingText(t),
      pills: getPillText(t),
      crops: getCropText(t),
      daily: getDailyTaskText(t),
      rares: getRareText(t),
      items: getItemText(t),
      missions: getMissionText(t),
      events: getEventText(t),
      moves: getMoveText(t),
      sects: getSectNames(t),
      path: getPathText(t),
      weapons: getWeaponNames(t),
      weaponDescs: getWeaponText(t),
      skills: getSkillNames(t),
      skillDescs: getSkillText(t),
      name: (name) => discipleName(name, language),
    }),
    [t, language],
  );
  return <TextContext.Provider value={text}>{children}</TextContext.Provider>;
}

export const useSectText = () => useContext(TextContext);

const RESOURCE_ICONS = { stone: Gem, herb: Leaf, ore: Hammer, rep: Star };

// 一笔资源：带图标的数字。have 不为空时，付不起的那一项标红。
export const CostChips = ({ cost, have, sign = '' }) => {
  const text = useSectText();
  const items = RESOURCES.filter((key) => cost?.[key]);
  if (!items.length) return null;
  return (
    <span className='sc-cost'>
      {items.map((key) => {
        const Icon = RESOURCE_ICONS[key];
        const short = have && (have[key] || 0) < cost[key];
        return (
          <Tooltip key={key} content={text.resources[key]}>
            <span className={`sc-chip is-${key}${short ? ' is-short' : ''}`}>
              <Icon size={13} />
              {sign}
              {formatAmount(cost[key])}
            </span>
          </Tooltip>
        );
      })}
    </span>
  );
};

export function canAfford(cost, have) {
  return RESOURCES.every((key) => (have?.[key] || 0) >= (cost?.[key] || 0));
}

// 相对时间：刚才、几分钟前、几小时前、几天前。
export function formatAgo(seconds, t) {
  if (seconds < 60) return t('刚才');
  if (seconds < 3600)
    return t('{{count}} 分钟前', { count: Math.floor(seconds / 60) });
  if (seconds < 86400)
    return t('{{count}} 小时前', { count: Math.floor(seconds / 3600) });
  return t('{{count}} 天前', { count: Math.floor(seconds / 86400) });
}

export const ElementTag = ({ element }) => {
  const text = useSectText();
  return (
    <span className={`sc-element is-e${element}`}>
      {text.elements[element]}
    </span>
  );
};

export const TalentTag = ({ talent }) => {
  const text = useSectText();
  return (
    <span className={`sc-talent is-t${talent}`}>{text.talents[talent]}</span>
  );
};

export const TraitTag = ({ trait }) => {
  const text = useSectText();
  const info = text.traits[trait];
  if (!info) return null;
  return (
    <Tooltip content={info.desc}>
      <span className='sc-trait'>{info.name}</span>
    </Tooltip>
  );
};

export function realmLabel(text, t, realm, layer) {
  return t('{{realm}} · 第 {{layer}} 层', {
    realm: text.realms[realm],
    layer,
  });
}

export const Bar = ({ value, max, kind = '' }) => {
  const pct = max > 0 ? Math.max(0, Math.min(100, (value / max) * 100)) : 0;
  return (
    <span className={`sc-bar ${kind ? `is-${kind}` : ''}`}>
      <span className='sc-bar-fill' style={{ width: `${pct}%` }} />
    </span>
  );
};

// 一件发生了的事写成一句话；资源与丹药的增减不写进句子，由飘字与资源栏表现。
export function eventSentence(ev, text, t) {
  const who = ev.who ? text.name(ev.who) : '';
  switch (ev.k) {
    case 'built':
      return t('{{name}}升到 {{level}} 级', {
        name: text.buildings[ev.s]?.name,
        level: ev.n,
      });
    case 'joined':
      return t('{{who}}拜入宗门', { who });
    case 'left':
      return t('{{who}}下山离去', { who });
    case 'bottleneck':
      return t('{{who}}修到第九层圆满，可以突破了', { who });
    case 'realm':
      return t('{{who}}突破成功，踏入{{realm}}', {
        who,
        realm: text.realms[ev.n],
      });
    case 'break_fail':
      return t('{{who}}突破失败，修为受损', { who });
    case 'injured':
      return t('{{who}}受伤，{{hours}} 小时内修炼变慢、不能出战', {
        who,
        hours: ev.n,
      });
    case 'healed':
      return t('{{who}}伤势痊愈', { who });
    case 'talent':
      return t('{{who}}洗髓伐骨，资质提升为{{talent}}', {
        who,
        talent: text.talents[ev.n],
      });
    case 'layer':
      return t('{{who}}修为精进 {{n}} 层', { who, n: ev.n });
    case 'outpost_build':
      return t('在{{region}}建起下院', {
        region: text.regions[REGION_IDS[ev.n]]?.name,
      });
    case 'outpost_station':
      return t('{{who}}前往{{region}}下院驻守', {
        who,
        region: text.regions[REGION_IDS[ev.n]]?.name,
      });
    case 'outpost_recall':
      return t('{{who}}从{{region}}下院回到本宗', {
        who,
        region: text.regions[REGION_IDS[ev.n]]?.name,
      });
    case 'mission': {
      const name = text.missions[ev.s]?.name;
      if (ev.n === 2) return t('{{name}}大获全胜', { name });
      if (ev.n === 1) return t('{{name}}顺利归来', { name });
      return t('{{name}}失利而归', { name });
    }
    case 'event': {
      const event = text.events[ev.s];
      const choice = event?.choices[ev.n];
      if (ev.win === true)
        return t('{{name}}：{{choice}}，胜！', { name: event?.name, choice });
      if (ev.win === false)
        return t('{{name}}：{{choice}}，败北', { name: event?.name, choice });
      return t('{{name}}：{{choice}}', { name: event?.name, choice });
    }
    case 'tournament':
      return ev.n
        ? t('宗门大比连胜 {{n}} 场', { n: ev.n })
        : t('宗门大比首轮落败');
    case 'arena':
      return ev.n ? t('论剑获胜') : t('论剑落败');
    case 'harvest':
      return ev.n === 2
        ? t('{{name}}丰收，收成翻倍！', { name: text.crops[ev.s]?.name })
        : t('收获{{name}}', { name: text.crops[ev.s]?.name });
    case 'sold':
      if (!ev.v)
        return t('在万宝阁把{{goods}} ×{{n}}赠给了道友', {
          goods: goodsName(text, ev.s),
          n: ev.n,
        });
      return t('万宝阁卖出{{goods}} ×{{n}}，得灵石 {{stone}}', {
        goods: goodsName(text, ev.s),
        n: ev.n,
        stone: ev.v || 0,
      });
    case 'bought':
      if (!ev.v)
        return t('收下道友赠送的{{goods}} ×{{n}}', {
          goods: goodsName(text, ev.s),
          n: ev.n,
        });
      return t('在万宝阁买到{{goods}} ×{{n}}，花灵石 {{stone}}', {
        goods: goodsName(text, ev.s),
        n: ev.n,
        stone: ev.v || 0,
      });
    case 'daily':
      return t('功课完成：{{name}}', {
        name: text.daily[ev.s]?.(ev.n || 1) || ev.s,
      });
    case 'daily_chest':
      return t('打开了今天的功课宝箱（已连续 {{n}} 天）', { n: ev.n });
    case 'garden_water':
      return t('帮道友浇灌了一块灵田');
    case 'garden_pick':
      return t('从道友的灵田里顺手摘了一些');
    case 'path_join':
      return t('拜入{{sect}}，从外门弟子做起', {
        sect: text.path.sects[ev.s]?.name,
      });
    case 'path_leave':
      return t('退出了{{sect}}', { sect: text.path.sects[ev.s]?.name });
    case 'path_found':
      return t('开宗立派，山门落成');
    case 'path_stipend':
      return t('领到今天的俸禄');
    case 'path_merit':
      return t('宗门贡献 +{{n}}', { n: ev.n });
    case 'path_rank':
      return t('晋升为{{rank}}', { rank: text.path.ranks[ev.n] });
    case 'path_exchange':
      return t('用 {{n}} 贡献换到{{goods}}', {
        n: ev.n,
        goods: goodsName(text, ev.s),
      });
    default:
      return '';
  }
}

export function moveName(text, move) {
  return text.moves[move]?.name || move;
}

export function pillName(text, pill) {
  return text.pills[pill]?.name || pill;
}

// 万宝阁与宗门宝库的货物名：灵草、玄铁是资源，pill_ 开头是丹药，rare_ 开头是灵药，arm_ 开头是斗法法宝，skill_ 开头是斗法法术。
export function goodsName(text, goods) {
  if (goods?.startsWith('pill_')) return pillName(text, goods.slice(5));
  if (goods?.startsWith('rare_')) return text.rares[goods.slice(5)] || goods;
  if (goods?.startsWith('arm_')) return text.weapons[goods.slice(4)] || goods;
  if (goods?.startsWith('skill_')) return text.skills[goods.slice(6)] || goods;
  return text.resources[goods] || goods;
}

// 宝库货物的说明。
export function goodsDesc(text, goods) {
  if (goods.startsWith('pill_')) return text.pills[goods.slice(5)]?.desc;
  if (goods.startsWith('arm_')) return text.weaponDescs[goods.slice(4)];
  if (goods.startsWith('skill_')) return text.skillDescs[goods.slice(6)];
  return text.crops[goods.slice(5)]?.desc;
}

// 藏经阁里的货物(法宝与法术)，宝库里的是丹药与灵药。
export const isLibraryGoods = (goods) =>
  goods.startsWith('arm_') || goods.startsWith('skill_');
