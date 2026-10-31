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

import React, { createContext, useContext, useMemo, useState } from 'react';
import { Modal, Tooltip } from '@douyinfe/semi-ui';
import {
  ArrowRight,
  Coins,
  FlaskConical,
  Gem,
  HeartPulse,
  Package,
  ShieldPlus,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  fxText,
  getCardNames,
  getItemText,
  getPotionText,
  getRelicText,
  getStatusNames,
  statusDesc,
} from './text';

// 整个页面共用一份文案表，组件里直接取。
const TextContext = createContext(null);

export function DungeonTextProvider({ children }) {
  const { t } = useTranslation();
  const text = useMemo(
    () => ({
      cards: getCardNames(t),
      statuses: getStatusNames(t),
      relics: getRelicText(t),
      potions: getPotionText(t),
      items: getItemText(t),
    }),
    [t],
  );
  return <TextContext.Provider value={text}>{children}</TextContext.Provider>;
}

export const useDungeonText = () => useContext(TextContext);

// 商店道具的图标。
export const ITEM_ICONS = {
  elixir: HeartPulse,
  potion_crate: FlaskConical,
  gold_pouch: Coins,
  relic_box: Package,
  ward: ShieldPlus,
};

export function cardName(card, text) {
  return `${text.cards[card.id] || card.id}${card.up ? '+' : ''}`;
}

// 一张牌的牌面：费用、牌名、类型与效果。手牌里计入力量、虚弱后变大或变小的数值整行标出颜色。
export const CardFace = ({
  card,
  onClick,
  selected = false,
  disabled = false,
  dim = false,
  small = false,
  className = '',
  style,
  onMouseEnter,
}) => {
  const { t } = useTranslation();
  const text = useDungeonText();
  const typeNames = {
    attack: t('攻伐'),
    skill: t('秘术'),
    power: t('心法'),
    curse: t('心魔'),
  };
  const classes = [
    'dg-card',
    `is-${card.type}`,
    `rarity-${card.rarity}`,
    card.up ? 'is-up' : '',
    selected ? 'is-selected' : '',
    dim ? 'is-dim' : '',
    small ? 'is-small' : '',
    onClick ? 'is-clickable' : '',
    className,
  ];
  const lines = card.fx.map((fx, index) => (
    <span
      key={index}
      className={fx.b ? (fx.n > fx.b ? 'is-buffed' : 'is-nerfed') : undefined}
    >
      {fxText(fx, t, text.cards, text.statuses)}
    </span>
  ));
  if (card.type === 'power')
    lines.push(<em key='power'>{t('心法：持续整场战斗')}</em>);
  if (card.exhaust) lines.push(<em key='exhaust'>{t('消耗')}</em>);
  if (card.type === 'curse') lines.push(<em key='curse'>{t('无法打出')}</em>);
  const Tag = onClick ? 'button' : 'div';
  return (
    <Tag
      type={onClick ? 'button' : undefined}
      className={classes.join(' ')}
      style={style}
      disabled={onClick ? disabled : undefined}
      onClick={onClick}
      onMouseEnter={onMouseEnter}
      onFocus={onMouseEnter}
    >
      {card.cost >= 0 && <span className='dg-card-cost'>{card.cost}</span>}
      <span className='dg-card-name'>{cardName(card, text)}</span>
      <span className='dg-card-type'>{typeNames[card.type]}</span>
      <span className='dg-card-text'>{lines}</span>
    </Tag>
  );
};

// 状态标记：状态名与层数，悬停看说明。
export const StatusList = ({ st }) => {
  const { t } = useTranslation();
  const text = useDungeonText();
  const entries = Object.entries(st || {}).filter(([, n]) => n > 0);
  if (!entries.length) return null;
  const debuffs = ['weak', 'vuln', 'burn'];
  return (
    <span className='dg-statuses'>
      {entries.map(([status, n]) => (
        <Tooltip key={status} content={statusDesc(status, n, t)}>
          <span
            className={`dg-status ${debuffs.includes(status) ? 'is-debuff' : ''} is-${status}`}
          >
            {text.statuses[status] || status}
            {status !== 'fortify' && <strong>{n}</strong>}
          </span>
        </Tooltip>
      ))}
    </span>
  );
};

export const HpBar = ({ hp, max, block = 0 }) => (
  <span className='dg-hp'>
    <span
      className='dg-hp-fill'
      style={{ width: `${max > 0 ? (Math.max(hp, 0) / max) * 100 : 0}%` }}
    />
    <span className='dg-hp-text'>
      {Math.max(hp, 0)} / {max}
    </span>
    {block > 0 && <span className='dg-hp-block'>{block}</span>}
  </span>
);

export const RelicChip = ({ id, fresh = false }) => {
  const text = useDungeonText();
  const relic = text.relics[id];
  return (
    <Tooltip content={relic?.desc}>
      <span className={`dg-relic ${fresh ? 'is-fresh' : ''}`}>
        <Gem size={14} />
        {relic?.name || id}
      </span>
    </Tooltip>
  );
};

// 从牌组里选一张牌的弹窗：营火锻造、商店移除、事件。filter 为假的牌不能选；upgrade 为真时同时显示升级后的样子。
export const DeckPicker = ({
  visible,
  title,
  deck,
  filter,
  upgrade,
  onPick,
  onCancel,
}) => {
  const { t } = useTranslation();
  const [hover, setHover] = useState(-1);
  const preview = upgrade && hover >= 0 ? deck[hover]?.upgrade : null;
  return (
    <Modal
      visible={visible}
      title={title}
      footer={null}
      onCancel={onCancel}
      width={760}
      bodyStyle={{ paddingBottom: 20 }}
    >
      {upgrade && (
        <div className='dg-upgrade-preview'>
          {preview ? (
            <>
              <CardFace card={deck[hover]} />
              <ArrowRight size={22} />
              <CardFace card={preview} />
            </>
          ) : (
            <span>{t('把鼠标移到一张牌上，预览升级后的效果')}</span>
          )}
        </div>
      )}
      <div className='dg-deck-grid'>
        {deck.map((card, index) => {
          const allowed = !filter || filter(card);
          return (
            <CardFace
              key={index}
              card={card}
              small
              dim={!allowed}
              disabled={!allowed}
              onClick={allowed ? () => onPick(index) : undefined}
              selected={hover === index}
              onMouseEnter={allowed ? () => setHover(index) : undefined}
            />
          );
        })}
      </div>
    </Modal>
  );
};

// 只看不选的牌堆列表(牌组、抽牌堆、弃牌堆)。
export const PileModal = ({ visible, title, cards, note, onCancel }) => {
  const { t } = useTranslation();
  return (
    <Modal
      visible={visible}
      title={title}
      footer={null}
      onCancel={onCancel}
      width={760}
      bodyStyle={{ paddingBottom: 20 }}
    >
      {note && <p className='dg-modal-note'>{note}</p>}
      {cards.length ? (
        <div className='dg-deck-grid'>
          {cards.map((card, index) => (
            <CardFace key={index} card={card} small />
          ))}
        </div>
      ) : (
        <p className='dg-modal-note'>{t('没有牌')}</p>
      )}
    </Modal>
  );
};
