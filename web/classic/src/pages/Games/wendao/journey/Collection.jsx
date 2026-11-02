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

import React, { useMemo, useState } from 'react';
import { Button, Input, Modal, Radio, RadioGroup } from '@douyinfe/semi-ui';
import { ArrowRight, Bookmark, Search } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { CardFace, cardName, useDungeonText } from '../../dungeon/parts';
import { CostChips } from '../../sect/parts';

// 两套牌组是不是同一组功法(不看顺序)。
const sameCards = (a, b) => {
  if (a.length !== b.length) return false;
  const sorted = [...b].sort((x, y) => x - y);
  return [...a].sort((x, y) => x - y).every((v, i) => v === sorted[i]);
};

const NO_FILTER = {
  query: '',
  type: 'all',
  cost: 'all',
  up: 'all',
  scope: 'all',
};

// 牌组预设：几格存起来的牌组，换上、存入现在的牌组、清掉。预设记的是收藏里的下标，丢掉功法时服务端一起改。
const Presets = ({ info, hero, busy, act }) => {
  const { t } = useTranslation();
  // naming 是正在存的那一格与名字。
  const [naming, setNaming] = useState(null);
  const slots = Array.from(
    { length: info.max_presets },
    (_, slot) => hero.presets[slot] || null,
  );
  const save = async () => {
    const data = await act('preset_save', {
      index: naming.slot,
      name: naming.name,
    });
    if (data) setNaming(null);
  };
  const clear = (slot) =>
    Modal.confirm({
      title: t('清掉这套预设？'),
      content: t('只清掉预设，收藏里的功法不受影响。'),
      onOk: () => act('preset_delete', { index: slot }),
    });
  return (
    <div className='jn-presets'>
      {slots.map((preset, slot) => {
        const current = preset && sameCards(preset.deck, hero.deck);
        const short = preset && preset.deck.length < info.deck_min;
        return (
          <div
            key={slot}
            className={`jn-preset${current ? ' is-current' : ''}${preset ? '' : ' is-empty'}`}
          >
            <div className='jn-preset-head'>
              <Bookmark size={14} aria-hidden />
              <strong>
                {preset?.name || t('预设 {{n}}', { n: slot + 1 })}
              </strong>
              <span className='jn-muted'>
                {preset ? t('{{n}} 张', { n: preset.deck.length }) : t('空着')}
              </span>
            </div>
            {short && (
              <span className='jn-preset-note'>
                {t('功法丢掉了几张，不足 {{n}} 张，要重新存', {
                  n: info.deck_min,
                })}
              </span>
            )}
            <div className='jn-preset-actions'>
              {preset && (
                <Button
                  size='small'
                  theme='solid'
                  disabled={busy || current || short}
                  onClick={() => act('preset_load', { index: slot })}
                >
                  {current ? t('正在用') : t('换上')}
                </Button>
              )}
              <Button
                size='small'
                disabled={busy}
                onClick={() => setNaming({ slot, name: preset?.name || '' })}
              >
                {preset ? t('改存现在的牌组') : t('存入现在的牌组')}
              </Button>
              {preset && (
                <Button
                  size='small'
                  type='danger'
                  theme='borderless'
                  disabled={busy}
                  onClick={() => clear(slot)}
                >
                  {t('清掉')}
                </Button>
              )}
            </div>
          </div>
        );
      })}
      <Modal
        visible={!!naming}
        title={t('存入牌组预设')}
        onOk={save}
        onCancel={() => setNaming(null)}
        okButtonProps={{ disabled: busy }}
      >
        <p className='jn-muted'>
          {t('把现在的 {{n}} 张牌组存进这一格，原来存的会被盖掉。', {
            n: hero.deck.length,
          })}
        </p>
        <Input
          value={naming?.name || ''}
          maxLength={info.max_preset_name}
          placeholder={t('起个名字（可以不填）')}
          onChange={(value) =>
            setNaming((previous) => ({ ...previous, name: value }))
          }
        />
      </Modal>
    </div>
  );
};

// 功法：收藏里的所有功法，牌组里的高亮。上面是牌组预设与筛选；选中一张后可以放进或拿出牌组、花灵石参悟，或者丢掉
// 不在牌组里的。
const Collection = ({ info, hero, res, busy, act }) => {
  const { t } = useTranslation();
  const text = useDungeonText();
  const [picked, setPicked] = useState(-1);
  const [filter, setFilter] = useState(NO_FILTER);
  const card = hero.cards[picked];
  const inDeck = hero.deck.includes(picked);
  const deckFull = hero.deck.length >= info.deck_max;
  const deckMin = hero.deck.length <= info.deck_min;
  const cost = card ? info.upgrade_cost[card.rarity] : 0;
  const full = hero.cards.length >= info.max_cards;
  const setField = (key, value) =>
    setFilter((previous) => ({ ...previous, [key]: value }));

  // 同一种功法(同样参悟没参悟)在收藏里有几张。
  const copies = useMemo(() => {
    const counts = {};
    for (const c of hero.cards) {
      const key = `${c.id}${c.up ? '+' : ''}`;
      counts[key] = (counts[key] || 0) + 1;
    }
    return counts;
  }, [hero.cards]);
  const copiesOf = (c) => copies[`${c.id}${c.up ? '+' : ''}`] || 0;

  // 牌组的构成：各类功法几张、平均几点灵力。
  const makeup = useMemo(() => {
    const types = { attack: 0, skill: 0, power: 0 };
    let energy = 0;
    for (const index of hero.deck) {
      const c = hero.cards[index];
      types[c.type] = (types[c.type] || 0) + 1;
      energy += Math.max(c.cost, 0);
    }
    return {
      ...types,
      average: hero.deck.length ? energy / hero.deck.length : 0,
    };
  }, [hero.deck, hero.cards]);

  const presetsWith = (index) =>
    hero.presets
      .map((preset, slot) =>
        preset?.deck.includes(index)
          ? preset.name || t('预设 {{n}}', { n: slot + 1 })
          : null,
      )
      .filter(Boolean);

  const toggle = () => {
    const deck = inDeck
      ? hero.deck.filter((index) => index !== picked)
      : [...hero.deck, picked];
    act('deck', { deck });
  };

  const forget = () => {
    const presets = presetsWith(picked);
    Modal.confirm({
      title: t('丢掉这张功法？'),
      content: [
        t('丢掉的功法找不回来，收藏满了以后赢下战斗也不能再收下新的。'),
        presets.length > 0 &&
          t('牌组预设「{{names}}」里也有这张，会一起拿掉。', {
            names: presets.join('、'),
          }),
      ]
        .filter(Boolean)
        .join(' '),
      onOk: async () => {
        const data = await act('forget', { card: picked });
        if (data) setPicked(-1);
      },
    });
  };

  const query = filter.query.trim().toLowerCase();
  const matches = (index) => {
    const c = hero.cards[index];
    const deck = hero.deck.includes(index);
    if (filter.scope === 'deck' && !deck) return false;
    if (filter.scope === 'rest' && deck) return false;
    if (filter.type !== 'all' && c.type !== filter.type) return false;
    if (filter.cost === '3' && c.cost < 3) return false;
    if (!['all', '3'].includes(filter.cost) && c.cost !== Number(filter.cost))
      return false;
    if (filter.up === 'up' && !c.up) return false;
    if (filter.up === 'can' && !c.upgrade) return false;
    if (filter.up === 'dup' && copiesOf(c) < 2) return false;
    return !query || cardName(c, text).toLowerCase().includes(query);
  };
  // 牌组里的排在前面，各自按收下的先后。
  const order = [
    ...hero.deck,
    ...hero.cards
      .map((_, index) => index)
      .filter((i) => !hero.deck.includes(i)),
  ].filter(matches);
  const filtered = Object.keys(NO_FILTER).some(
    (key) => filter[key] !== NO_FILTER[key],
  );

  return (
    <div className='jn-collection'>
      <p className='jn-muted'>
        {t(
          '牌组 {{deck}} 张（{{min}}–{{max}}），收藏 {{cards}} / {{limit}}。点一张功法放进或拿出牌组、参悟。',
          {
            deck: hero.deck.length,
            min: info.deck_min,
            max: info.deck_max,
            cards: hero.cards.length,
            limit: info.max_cards,
          },
        )}
        <br />
        {t(
          '牌组里攻伐 {{attack}} · 秘术 {{skill}} · 心法 {{power}}，平均 {{energy}} 点灵力',
          {
            attack: makeup.attack,
            skill: makeup.skill,
            power: makeup.power,
            energy: makeup.average.toFixed(1),
          },
        )}
      </p>
      {full && (
        <div className='jn-full-note' role='status'>
          <span>
            {t(
              '收藏满了：赢下战斗不能再收下新的功法。丢掉几张不在牌组里的，给新的功法腾地方。',
            )}
          </span>
          <Button
            size='small'
            onClick={() =>
              setFilter({ ...NO_FILTER, scope: 'rest', up: 'dup' })
            }
          >
            {t('看牌组外的重复功法')}
          </Button>
        </div>
      )}
      <Presets info={info} hero={hero} busy={busy} act={act} />
      <div className='jn-filters'>
        <Input
          size='small'
          prefix={<Search size={14} />}
          value={filter.query}
          placeholder={t('搜索功法')}
          showClear
          onChange={(value) => setField('query', value)}
          className='jn-filter-search'
        />
        <RadioGroup
          type='button'
          buttonSize='small'
          value={filter.scope}
          onChange={(event) => setField('scope', event.target.value)}
          aria-label={t('范围')}
        >
          <Radio value='all'>{t('全部')}</Radio>
          <Radio value='deck'>{t('牌组里')}</Radio>
          <Radio value='rest'>{t('牌组外')}</Radio>
        </RadioGroup>
        <RadioGroup
          type='button'
          buttonSize='small'
          value={filter.type}
          onChange={(event) => setField('type', event.target.value)}
          aria-label={t('类型')}
        >
          <Radio value='all'>{t('全部')}</Radio>
          <Radio value='attack'>{t('攻伐')}</Radio>
          <Radio value='skill'>{t('秘术')}</Radio>
          <Radio value='power'>{t('心法')}</Radio>
        </RadioGroup>
        <RadioGroup
          type='button'
          buttonSize='small'
          value={filter.cost}
          onChange={(event) => setField('cost', event.target.value)}
          aria-label={t('灵力')}
        >
          <Radio value='all'>{t('任意灵力')}</Radio>
          <Radio value='0'>0</Radio>
          <Radio value='1'>1</Radio>
          <Radio value='2'>2</Radio>
          <Radio value='3'>3+</Radio>
        </RadioGroup>
        <RadioGroup
          type='button'
          buttonSize='small'
          value={filter.up}
          onChange={(event) => setField('up', event.target.value)}
          aria-label={t('参悟')}
        >
          <Radio value='all'>{t('全部')}</Radio>
          <Radio value='up'>{t('已参悟')}</Radio>
          <Radio value='can'>{t('可参悟')}</Radio>
          <Radio value='dup'>{t('重复的')}</Radio>
        </RadioGroup>
        {filtered && (
          <Button
            size='small'
            theme='borderless'
            onClick={() => setFilter(NO_FILTER)}
          >
            {t('清除筛选')}
          </Button>
        )}
      </div>
      {card && (
        <div className='jn-card-actions'>
          <strong>{cardName(card, text)}</strong>
          {copiesOf(card) > 1 && (
            <span className='jn-muted'>
              {t('收藏里有 {{n}} 张', { n: copiesOf(card) })}
            </span>
          )}
          <Button
            size='small'
            disabled={busy || (inDeck ? deckMin : deckFull)}
            onClick={toggle}
          >
            {inDeck ? t('拿出牌组') : t('放进牌组')}
          </Button>
          {card.upgrade && (
            <>
              <Button
                size='small'
                theme='solid'
                disabled={busy || (res?.stone || 0) < cost}
                onClick={() => act('upgrade', { card: picked })}
              >
                {t('参悟')}
              </Button>
              <CostChips cost={{ stone: cost }} have={res} />
            </>
          )}
          {!inDeck && (
            <Button size='small' type='danger' disabled={busy} onClick={forget}>
              {t('丢掉')}
            </Button>
          )}
        </div>
      )}
      {card?.upgrade && (
        <div className='dg-upgrade-preview jn-upgrade-preview'>
          <CardFace card={card} />
          <ArrowRight size={22} />
          <CardFace card={card.upgrade} />
        </div>
      )}
      {filtered && (
        <p className='jn-muted'>{t('筛出 {{n}} 张', { n: order.length })}</p>
      )}
      <div className='dg-deck-grid'>
        {order.map((index) => {
          const c = hero.cards[index];
          return (
            <div key={index} className='jn-card-cell'>
              <CardFace
                card={c}
                small
                dim={!hero.deck.includes(index)}
                selected={picked === index}
                onClick={() => setPicked(picked === index ? -1 : index)}
              />
              {copiesOf(c) > 1 && (
                <span className='jn-card-copies'>×{copiesOf(c)}</span>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default Collection;
