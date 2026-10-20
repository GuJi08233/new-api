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

import React, { useRef, useState } from 'react';
import { Button } from '@douyinfe/semi-ui';
import { Crown, Layers, Lightbulb, Users } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { isRedSuit, SUIT_ICONS } from '../common/cards';
import PvpGame from '../common/pvp/PvpGame';
import { groupPlayedCards } from './cards';
import { selectedHintIndex, selectionFeedback } from './selection';
import useCardSelection from './useCardSelection';
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
const PlayingCard = ({
  card,
  selected = false,
  disabled = false,
  onClick,
  ...cardProps
}) => {
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
      {...cardProps}
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
          <Suit className='landlord-card-pip' size={26} aria-hidden />
        </>
      )}
    </Element>
  );
};

// 出牌按主牌与带牌分组，手牌和底牌只按点数从大到小排。
const Cards = ({ cards, type }) => (
  <div className='landlord-cards'>
    {groupPlayedCards(cards, type).map((group, index) => (
      <div key={index} className='landlord-card-group'>
        {group.map((card) => (
          <span key={card} className='landlord-card-slot'>
            <PlayingCard card={card} />
          </span>
        ))}
      </div>
    ))}
  </div>
);

// 一次刷新里同时出现的几手牌(自己出牌后 AI 或托管接连跟上)依次错开这么久出现，看得出谁先出、出了什么。
const PLAY_STAGGER_MS = 400;

const LandlordBoard = ({ state, seat, names, canMove, over, onMove }) => {
  const { t } = useTranslation();
  const [hintChoice, setHintChoice] = useState({ key: '', index: -1 });
  // 出牌区里每一手第一次出现时排定的入场延迟，之后重新渲染不再改动，免得动画重来。
  const playDelaysRef = useRef(new Map());
  const hand = [...(state?.hand || [])].sort((left, right) => right - left);
  const bidding = state?.phase === 'bidding';
  const lastPlay = state?.last_play;
  const trick = state?.trick || [];
  const hints = state?.hints || [];
  const selectionKey = `${state?.phase}/${state?.to_move}/${lastPlay?.seat ?? ''}/${lastPlay?.cards?.join('.') || ''}/${hand.join('.')}`;
  const { selected, selectCards, clearSelection, handProps, getCardProps } =
    useCardSelection({
      hand,
      enabled: canMove && !bidding && !over,
      resetKey: selectionKey,
    });
  const hintIndex = selectedHintIndex(
    selected,
    hints,
    hintChoice,
    selectionKey,
  );
  const feedback = selectionFeedback(selected, lastPlay);
  const noHint =
    hintChoice.key === selectionKey &&
    hintChoice.index === -2 &&
    selected.length === 0;
  // 这一轮要压的那一手：最后一次出牌(之后可能还跟着不出)，一轮结束后没有。
  let leadIndex = -1;
  if (lastPlay) {
    trick.forEach((action, index) => {
      if (!action.pass) leadIndex = index;
    });
  }
  // 每一手的 key 带上这一轮领出的牌、座位和牌面：新一轮领出时换成新的节点，入场动画才会再播一次(牌在一局里不会重复，
  // 不出没有牌面，靠领出的那手区分是哪一轮)。
  const trickKey = trick[0]?.cards?.join('.') || '';
  const playKeys = trick.map(
    (action, index) =>
      `${trickKey}/${action.seat}-${action.pass ? 'pass' : action.cards.join('.')}-${index}`,
  );
  let freshPlays = 0;
  for (const key of playKeys) {
    if (!playDelaysRef.current.has(key)) {
      playDelaysRef.current.set(key, freshPlays * PLAY_STAGGER_MS);
      freshPlays += 1;
    }
  }
  const nameOf = (index) =>
    names?.[index] || t('座位 {{seat}}', { seat: index + 1 });
  const tableSeats = [
    { seat: (seat + 2) % 3, position: 'left', label: t('上家') },
    { seat: (seat + 1) % 3, position: 'right', label: t('下家') },
    { seat, position: 'self', label: t('我') },
  ];
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
  let selectionNote = t('按住并横向拖动可连续选牌');
  if (selected.length > 0) {
    if (feedback.reason === 'invalid') {
      selectionNote = t('这些牌还未组成合法牌型');
    } else if (feedback.reason === 'shape') {
      selectionNote = t('所选牌型或长度不匹配，请选同类同长度的牌或炸弹');
    } else if (feedback.reason === 'rank') {
      selectionNote = t('所选牌还不能压过上一手');
    } else {
      selectionNote = t('已选 {{type}}，可以出牌', {
        type: combinations[feedback.combination.type],
      });
    }
  } else if (noHint) {
    selectionNote = t('没有能压过的牌，可以选择不出');
  }
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
      ) : (
        <div
          className='landlord-table'
          aria-label={t('出牌区')}
          aria-live='polite'
        >
          <div className='landlord-table-note'>
            {trick.length === 0
              ? t('{{name}} 是地主，先出牌', { name: nameOf(state.landlord) })
              : !lastPlay && !over
                ? t('两家都不出，{{name}} 领出新一轮', {
                    name: nameOf(state.to_move),
                  })
                : null}
          </div>
          {tableSeats.map((player) => {
            let actionIndex = -1;
            trick.forEach((action, index) => {
              if (action.seat === player.seat) actionIndex = index;
            });
            const action = trick[actionIndex];
            return (
              <div
                key={player.seat}
                className={`landlord-table-seat is-${player.position} ${!over && state.to_move === player.seat ? 'is-turn' : ''}`}
              >
                <div className='landlord-table-player'>
                  {player.seat === state.landlord && (
                    <Crown size={14} aria-hidden />
                  )}
                  <span>
                    {player.position === 'self'
                      ? nameOf(player.seat)
                      : `${player.label} · ${nameOf(player.seat)}`}
                  </span>
                  <span
                    className={`landlord-table-count ${!over && state.counts[player.seat] > 0 && state.counts[player.seat] <= 2 ? 'is-low' : ''}`}
                    title={t('剩余 {{count}} 张', {
                      count: state.counts[player.seat],
                    })}
                  >
                    <Layers size={12} aria-hidden />
                    {state.counts[player.seat]}
                  </span>
                </div>
                {action ? (
                  <div
                    key={playKeys[actionIndex]}
                    className={`landlord-seat-play ${actionIndex === leadIndex ? 'is-lead' : ''}`}
                    style={{
                      animationDelay: `${playDelaysRef.current.get(playKeys[actionIndex])}ms`,
                    }}
                  >
                    <span className='landlord-caption'>
                      {action.pass ? t('不出') : combinations[action.type]}
                    </span>
                    {!action.pass && (
                      <Cards cards={action.cards} type={action.type} />
                    )}
                  </div>
                ) : (
                  <span className='landlord-table-wait'>{t('等待出牌')}</span>
                )}
              </div>
            );
          })}
          {trick.length > 0 && (
            <details className='landlord-trick-history'>
              <summary>{t('本轮记录')}</summary>
              <ol className='landlord-trick'>
                {trick.map((action, index) => (
                  <li
                    key={playKeys[index]}
                    className={index === leadIndex ? 'is-lead' : ''}
                  >
                    <span className='landlord-caption'>
                      {nameOf(action.seat)} ·{' '}
                      {action.pass ? t('不出') : combinations[action.type]}
                    </span>
                    {!action.pass && (
                      <Cards cards={action.cards} type={action.type} />
                    )}
                  </li>
                ))}
              </ol>
            </details>
          )}
        </div>
      )}
      {!over && (
        <div className='pvp-actions landlord-actions'>
          {bidding ? (
            [0, 1, 2, 3].map((bid) => (
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
            ))
          ) : (
            <>
              <Button
                disabled={!canMove || !lastPlay || lastPlay.seat === seat}
                onClick={() => onMove({ pass: true })}
              >
                {t('不出')}
              </Button>
              <Button
                icon={<Lightbulb size={16} />}
                title={t('提示只选牌，不会自动出牌')}
                disabled={!canMove}
                onClick={() => {
                  const next =
                    hints.length > 0 ? (hintIndex + 1) % hints.length : -2;
                  selectCards(next >= 0 ? hints[next] : []);
                  setHintChoice({ key: selectionKey, index: next });
                }}
              >
                {t('提示')}
                {hintIndex >= 0 && ` ${hintIndex + 1}/${hints.length}`}
              </Button>
              <Button
                theme='solid'
                disabled={!canMove || feedback.reason !== null}
                onClick={() => onMove({ cards: selected })}
              >
                {t('出牌')}
                {selected.length > 0 && ` (${selected.length})`}
              </Button>
              <Button
                disabled={!canMove || selected.length === 0}
                onClick={() => {
                  clearSelection();
                  setHintChoice({ key: selectionKey, index: -1 });
                }}
              >
                {t('清空选择')}
              </Button>
            </>
          )}
        </div>
      )}
      {!bidding && !over && (
        <div
          className={`landlord-selection-note ${selected.length > 0 && feedback.reason ? 'is-warning' : ''}`}
          role='status'
        >
          <span>{selectionNote}</span>
          {hintIndex >= 0 && (
            <span className='landlord-selection-detail'>
              {t('再次点击提示切换组合')}
            </span>
          )}
        </div>
      )}
      <div className='landlord-hand'>
        <div className='landlord-caption'>
          {t('我的手牌')}
          {' · '}
          {t('剩余 {{count}} 张', { count: hand.length })}
          {selected.length > 0 && (
            <span className='landlord-selected-count'>
              {t('已选 {{count}} 张', { count: selected.length })}
            </span>
          )}
        </div>
        <div
          {...handProps}
          className='landlord-hand-row'
          aria-label={t('我的手牌')}
          title={t('手牌按点数从大到小排列')}
        >
          {hand.map((card) => (
            <div key={card} className='landlord-hand-slot'>
              <PlayingCard
                {...getCardProps(card)}
                card={card}
                selected={selected.includes(card)}
                disabled={!canMove || bidding || over}
              />
            </div>
          ))}
        </div>
      </div>
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
      renderBoard={({ roomId, sessionId, state, ...props }) => (
        <LandlordBoard
          key={`${roomId || `ai:${sessionId || ''}`}:${state?.deals || 0}`}
          state={state}
          {...props}
        />
      )}
    />
  );
};

export default Landlord;
