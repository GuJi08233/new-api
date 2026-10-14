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
import { Button } from '@douyinfe/semi-ui';
import { Crown, Layers, Users } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { isRedSuit, SUIT_ICONS } from '../common/cards';
import PvpGame from '../common/pvp/PvpGame';
import './landlord.css';

const RANKS = [
  '3',
  '4',
  '5',
  '6',
  '7',
  '8',
  '9',
  '10',
  'J',
  'Q',
  'K',
  'A',
  '2',
];
const SUITS = ['♠', '♥', '♦', '♣'];
// 一局最多发几次牌，与服务端 pkg/landlord.MaxDeals 一致：最后一次发牌时前两位都不叫，最后一位必须叫分。
const MAX_DEALS = 3;

// 一张牌。王的牌面像实体牌一样竖写 JOKER，小王黑色、大王红色，读屏与悬停提示用翻译后的名字。
const PlayingCard = ({ card, selected = false, disabled = false, onClick }) => {
  const { t } = useTranslation();
  const joker = card >= 52;
  const suit = card % 4;
  const Suit = SUIT_ICONS[suit];
  const name = joker
    ? card === 52
      ? t('小王')
      : t('大王')
    : `${RANKS[Math.floor(card / 4)]}${SUITS[suit]}`;
  const Element = onClick ? 'button' : 'span';
  return (
    <Element
      className={`landlord-card ${joker ? 'is-joker' : ''} ${card === 53 || (!joker && isRedSuit(suit)) ? 'is-red' : ''} ${selected ? 'is-selected' : ''}`}
      type={onClick ? 'button' : undefined}
      disabled={onClick ? disabled : undefined}
      aria-pressed={onClick ? selected : undefined}
      aria-label={name}
      title={joker ? name : undefined}
      onClick={onClick}
    >
      {joker ? (
        <strong aria-hidden>JOKER</strong>
      ) : (
        <>
          <strong>{RANKS[Math.floor(card / 4)]}</strong>
          <Suit size={18} aria-hidden />
        </>
      )}
    </Element>
  );
};

// 一组牌，从大到小排。
const Cards = ({ cards }) => (
  <div className='landlord-cards'>
    {[...cards].reverse().map((card) => (
      <PlayingCard key={card} card={card} />
    ))}
  </div>
);

const LandlordBoard = ({ state, seat, names, canMove, over, onMove }) => {
  const { t } = useTranslation();
  const [selection, setSelection] = useState([]);
  const hand = [...(state?.hand || [])].sort((left, right) => right - left);
  // 成功出牌后服务端移除手牌，选中牌随新手牌过滤；非法牌型或网络错误保留选择，便于修正重试。
  const selected = selection.filter((card) => hand.includes(card));
  const bidding = state?.phase === 'bidding';
  const lastPlay = state?.last_play;
  const trick = state?.trick || [];
  // 这一轮要压的那一手：最后一次出牌(之后可能还跟着不出)，一轮结束后没有。
  let leadIndex = -1;
  if (lastPlay) {
    trick.forEach((action, index) => {
      if (!action.pass) leadIndex = index;
    });
  }
  const nameOf = (index) =>
    names?.[index] || t('座位 {{seat}}', { seat: index + 1 });
  const combinations = {
    single: t('单张'),
    pair: t('对子'),
    triple: t('三张'),
    triple_single: t('三带一'),
    triple_pair: t('三带二'),
    straight: t('顺子'),
    pair_straight: t('连对'),
    airplane: t('飞机'),
    airplane_single: t('飞机带单牌'),
    airplane_pair: t('飞机带对子'),
    four_two: t('四带二'),
    four_two_pairs: t('四带两对'),
    bomb: t('炸弹'),
    rocket: t('王炸'),
  };
  if (!state) {
    return (
      <div className='landlord-empty'>
        <Users size={40} strokeWidth={1.5} aria-hidden />
        <span>{t('三人到齐后自动发牌')}</span>
      </div>
    );
  }
  return (
    <div className='landlord-board'>
      <div className='landlord-bottom'>
        <span className='landlord-caption'>{t('底牌')}</span>
        {bidding ? (
          <div className='landlord-cards'>
            {[0, 1, 2].map((index) => (
              <span
                key={index}
                className='landlord-card is-back'
                aria-label={t('未公开')}
              />
            ))}
          </div>
        ) : (
          <Cards cards={state.bottom} />
        )}
      </div>
      {bidding && state.deals > 1 && (
        <div className='games-stage-counter'>
          {state.deals >= MAX_DEALS
            ? t(
                '都不叫，已重新发牌（第 {{deal}} 次）；这次前两位都不叫时，最后一位必须叫分',
                { deal: state.deals },
              )
            : t('都不叫，已重新发牌（第 {{deal}} 次）', { deal: state.deals })}
        </div>
      )}
      {bidding ? (
        <>
          <div className='landlord-bids'>
            {state.bids.map((bid, index) => (
              <div
                key={index}
                className={index === state.to_move && !over ? 'is-turn' : ''}
              >
                <span>{nameOf(index)}</span>
                <strong>
                  {bid < 0
                    ? t('尚未叫分')
                    : bid === 0
                      ? t('不叫')
                      : t('{{bid}} 分', { bid })}
                </strong>
              </div>
            ))}
          </div>
          {!over && (
            <div className='pvp-actions'>
              {[0, 1, 2, 3].map((bid) => (
                <Button
                  key={bid}
                  theme={bid > 0 ? 'solid' : 'light'}
                  disabled={
                    !canMove ||
                    (bid > 0 && bid <= state.high_bid) ||
                    (bid === 0 && state.must_bid)
                  }
                  onClick={() => onMove({ bid })}
                >
                  {bid === 0 ? t('不叫') : t('叫 {{bid}} 分', { bid })}
                </Button>
              ))}
            </div>
          )}
        </>
      ) : (
        <div className='landlord-table' aria-live='polite'>
          {trick.length === 0 && (
            <span className='landlord-caption'>
              {t('{{name}} 是地主，先出牌', {
                name: nameOf(state.landlord),
              })}
            </span>
          )}
          {trick.length > 0 && (
            <ol className='landlord-trick'>
              {trick.map((action, index) => (
                <li
                  key={index}
                  className={index === leadIndex ? 'is-lead' : ''}
                >
                  <span className='landlord-caption'>
                    {nameOf(action.seat)}
                    {' · '}
                    {action.pass ? t('不出') : combinations[action.type]}
                  </span>
                  {!action.pass && <Cards cards={action.cards} />}
                </li>
              ))}
            </ol>
          )}
          {trick.length > 0 && !lastPlay && !over && (
            <span className='landlord-caption'>
              {t('两家都不出，{{name}} 领出新一轮', {
                name: nameOf(state.to_move),
              })}
            </span>
          )}
        </div>
      )}
      <div className='landlord-hand'>
        <div className='landlord-caption'>
          {t('我的手牌')}
          {' · '}
          {t('剩余 {{count}} 张', { count: hand.length })}
        </div>
        <div className='landlord-cards'>
          {hand.map((card) => (
            <PlayingCard
              key={card}
              card={card}
              selected={selected.includes(card)}
              disabled={!canMove || bidding || over}
              onClick={() =>
                setSelection(
                  selected.includes(card)
                    ? selected.filter((value) => value !== card)
                    : [...selected, card],
                )
              }
            />
          ))}
        </div>
      </div>
      {!bidding && !over && (
        <div className='pvp-actions'>
          <Button
            theme='solid'
            disabled={!canMove || selected.length === 0}
            onClick={() => onMove({ cards: selected })}
          >
            {t('出牌')}
            {selected.length > 0 && ` (${selected.length})`}
          </Button>
          <Button
            disabled={!canMove || !lastPlay || lastPlay.seat === seat}
            onClick={() => onMove({ pass: true })}
          >
            {t('不出')}
          </Button>
          <Button
            disabled={!canMove || selected.length === 0}
            onClick={() => setSelection([])}
          >
            {t('清空选择')}
          </Button>
        </div>
      )}
      {over && state.hands && (
        <div className='landlord-reveal'>
          {state.hands.map(
            (cards, index) =>
              index !== seat &&
              cards.length > 0 && (
                <div key={index}>
                  <div className='landlord-caption'>
                    {nameOf(index)}
                    {' · '}
                    {index === state.landlord ? t('地主') : t('农民')}
                  </div>
                  <Cards cards={cards} />
                </div>
              ),
          )}
        </div>
      )}
    </div>
  );
};

const Landlord = () => {
  const { t } = useTranslation();
  return (
    <PvpGame
      gameKey='landlord'
      title={t('斗地主')}
      className='landlord-page'
      randomFirst={false}
      seatLabels={(state) =>
        [0, 1, 2].map((index) => (
          <span className='landlord-seat-label' key={index}>
            {state?.landlord === index && <Crown size={13} aria-hidden />}
            {state?.landlord >= 0
              ? state.landlord === index
                ? t('地主')
                : t('农民')
              : t('待定')}
            {state && (
              <span
                className='landlord-seat-count'
                title={t('剩余 {{count}} 张', { count: state.counts[index] })}
              >
                <Layers size={12} aria-hidden />
                {state.counts[index]}
              </span>
            )}
          </span>
        ))
      }
      statusText={({ state, myTurn }) => {
        if (state?.phase === 'bidding') {
          if (!myTurn) return t('等待其他玩家叫分');
          return state.must_bid ? t('轮到你叫分，这次必须叫') : t('轮到你叫分');
        }
        return myTurn ? t('请选择手牌出牌，或选择不出') : t('等待其他玩家出牌');
      }}
      resultNote={({ state }) =>
        state?.winner >= 0
          ? state.winner === state.landlord
            ? t('地主先出完手牌')
            : t('农民先出完手牌，两位农民共同获胜')
          : ''
      }
      rules={[
        t(
          '三人各发 17 张牌，叫分最高者为地主并拿走 3 张底牌；都不叫则重新发牌，第三次发牌时前两位都不叫，最后一位必须叫分',
        ),
        t(
          '地主先出牌，按座位顺序轮流出牌；连续两人不出后，上一位出牌者重新领出',
        ),
        t(
          '押注是底注：地主先出完赢两倍，两位农民各输一倍；任一农民先出完，两位农民各赢一倍，地主输两倍。叫分与炸弹不加倍',
        ),
        t('顺子、连对和飞机不能包含 2 或王；炸弹可压普通牌型，王炸最大'),
      ]}
      renderBoard={({ roomId, state, ...props }) => (
        <LandlordBoard
          key={`${roomId || 'ai'}:${state?.deals || 0}`}
          state={state}
          {...props}
        />
      )}
    />
  );
};

export default Landlord;
