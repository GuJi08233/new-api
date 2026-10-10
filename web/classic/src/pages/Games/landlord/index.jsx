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
import { Crown, Users } from 'lucide-react';
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

const PlayingCard = ({ card, selected = false, disabled = false, onClick }) => {
  const { t } = useTranslation();
  const joker = card >= 52;
  const rank = joker
    ? card === 52
      ? t('小王')
      : t('大王')
    : RANKS[Math.floor(card / 4)];
  const suit = card % 4;
  const Suit = SUIT_ICONS[suit];
  const Element = onClick ? 'button' : 'span';
  return (
    <Element
      className={`landlord-card ${joker ? 'is-joker' : ''} ${card === 53 || (!joker && isRedSuit(suit)) ? 'is-red' : ''} ${selected ? 'is-selected' : ''}`}
      type={onClick ? 'button' : undefined}
      disabled={onClick ? disabled : undefined}
      aria-pressed={onClick ? selected : undefined}
      aria-label={joker ? rank : `${rank}${SUITS[suit]}`}
      onClick={onClick}
    >
      <strong>{rank}</strong>
      {!joker && <Suit size={18} aria-hidden />}
    </Element>
  );
};

const LandlordBoard = ({ state, seat, canMove, over, onMove }) => {
  const { t } = useTranslation();
  const [selection, setSelection] = useState([]);
  const hand = [...(state?.hand || [])].sort((left, right) => right - left);
  // 成功出牌后服务端移除手牌，选中牌随新手牌过滤；非法牌型或网络错误保留选择，便于修正重试。
  const selected = selection.filter((card) => hand.includes(card));
  const bidding = state?.phase === 'bidding';
  const lastPlay = state?.last_play;
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
        <div className='landlord-cards'>
          {bidding
            ? [0, 1, 2].map((index) => (
                <span
                  key={index}
                  className='landlord-card is-back'
                  aria-label={t('未公开')}
                />
              ))
            : state.bottom.map((card) => (
                <PlayingCard key={card} card={card} />
              ))}
        </div>
      </div>
      {state.deals > 1 && (
        <div className='games-stage-counter'>
          {t('都不叫，已重新发牌（第 {{deal}} 次）', { deal: state.deals })}
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
                <span>
                  {index === seat
                    ? t('我')
                    : t('座位 {{seat}}', { seat: index + 1 })}
                </span>
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
          <div className='pvp-actions'>
            {[0, 1, 2, 3].map((bid) => (
              <Button
                key={bid}
                theme={bid > 0 ? 'solid' : 'light'}
                disabled={!canMove || (bid > 0 && bid <= state.high_bid)}
                onClick={() => onMove({ bid })}
              >
                {bid === 0 ? t('不叫') : t('叫 {{bid}} 分', { bid })}
              </Button>
            ))}
          </div>
        </>
      ) : (
        <div className='landlord-table' aria-live='polite'>
          {lastPlay ? (
            <>
              <div className='landlord-caption'>
                {lastPlay.seat === seat
                  ? t('我')
                  : t('座位 {{seat}}', { seat: lastPlay.seat + 1 })}
                {' · '}
                {combinations[lastPlay.type]}
              </div>
              <div className='landlord-cards'>
                {[...lastPlay.cards].reverse().map((card) => (
                  <PlayingCard key={card} card={card} />
                ))}
              </div>
              <div className='landlord-passes'>
                {state.passed.map(
                  (passed, index) =>
                    passed && (
                      <span key={index}>
                        {t('座位 {{seat}} 不出', { seat: index + 1 })}
                      </span>
                    ),
                )}
              </div>
            </>
          ) : (
            <span className='landlord-caption'>{t('新一轮，请领出')}</span>
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
              index !== seat && (
                <div key={index}>
                  <div className='landlord-caption'>
                    {t('座位 {{seat}}', { seat: index + 1 })}
                    {' · '}
                    {index === state.landlord ? t('地主') : t('农民')}
                  </div>
                  <div className='landlord-cards'>
                    {[...cards].reverse().map((card) => (
                      <PlayingCard key={card} card={card} />
                    ))}
                  </div>
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
      teamGame
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
              <span>
                {t('剩余 {{count}} 张', { count: state.counts[index] })}
              </span>
            )}
          </span>
        ))
      }
      statusText={({ state, myTurn }) =>
        state?.phase === 'bidding'
          ? myTurn
            ? t('轮到你叫分')
            : t('等待其他玩家叫分')
          : myTurn
            ? t('请选择手牌出牌，或选择不出')
            : t('等待其他玩家出牌')
      }
      resultNote={({ state }) =>
        state?.winner >= 0
          ? state.winner === state.landlord
            ? t('地主先出完手牌')
            : t('农民先出完手牌，两位农民共同获胜')
          : ''
      }
      rules={[
        t(
          '三人各发 17 张牌，叫分最高者为地主并拿走 3 张底牌；都不叫则重新发牌',
        ),
        t(
          '地主先出牌，按座位顺序轮流出牌；连续两人不出后，上一位出牌者重新领出',
        ),
        t('地主先出完则地主获胜，任一农民先出完则两位农民共同获胜'),
        t(
          '固定押注，叫分与炸弹不加注；获胜队伍平分输方押注，平台从净赢额抽成，余数按座位顺序分配',
        ),
        t('顺子、连对和飞机不能包含 2 或王；炸弹可压普通牌型，王炸最大'),
      ]}
      renderBoard={({ roomId, state, ...props }) => (
        <LandlordBoard
          key={`${roomId || 'waiting'}:${state?.deals || 0}`}
          state={state}
          {...props}
        />
      )}
    />
  );
};

export default Landlord;
