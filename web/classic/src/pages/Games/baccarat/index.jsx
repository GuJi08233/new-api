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

import React, { useEffect, useRef, useState } from 'react';
import { Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getGameMetaMap } from '../../../constants';
import GameLayout from '../common/GameLayout';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { formatCoins, gamePost, percent, toCoinCents } from '../common/api';
import { useGameInfo } from '../common/useGameInfo';
import {
  BIG_WIN_MULTIPLIER,
  WagerStats,
  playWagerFeedback,
  wagerResultMessage,
} from '../common/wager';
import { AutoPlayControls, useAutoPlay } from '../common/autoPlay';
import { playSound } from '../common/sound';
import { RANK_LABELS, SUIT_ICONS, isRedSuit } from '../common/cards';
import './baccarat.css';

const CHIP_VALUES = [0.1, 1, 5, 10, 50, 100, 500];
// 翻开每张牌的间隔，第三张牌前多停一会儿。
const CARD_MS = 420;
const THIRD_MS = 700;
const ROAD_SIZE = 36;

// 一手牌的点数：A 算 1，10 与人头牌算 0，合计取个位。
const handTotal = (cards) =>
  cards.reduce((sum, card) => sum + (card.rank >= 10 ? 0 : card.rank), 0) % 10;

// 翻牌的顺序：闲、庄、闲、庄，再按补牌规则依次是闲的第三张、庄的第三张。
function dealOrder(round) {
  const order = [
    ['player', 0],
    ['banker', 0],
    ['player', 1],
    ['banker', 1],
  ];
  if (round.player.cards.length > 2) order.push(['player', 2]);
  if (round.banker.cards.length > 2) order.push(['banker', 2]);
  return order;
}

function delay(ms, page) {
  return new Promise((resolve) => {
    page.timer = setTimeout(resolve, ms);
  });
}

const BaccaratCard = ({ card }) => {
  const Suit = SUIT_ICONS[card.suit];
  return (
    <div className={`baccarat-card ${isRedSuit(card.suit) ? 'is-red' : ''}`}>
      <span className='baccarat-card-rank'>{RANK_LABELS[card.rank - 1]}</span>
      <Suit className='baccarat-card-suit' />
    </div>
  );
};

const Baccarat = () => {
  const { t } = useTranslation();
  const meta = getGameMetaMap(t).baccarat;
  const { loading, unavailable, info, patch, reload } = useGameInfo('baccarat');
  const [chip, setChip] = useState(1);
  // 桌面上的注：键是押注的位置，值是押的游戏币。发完一局保留，可以直接再发一局。
  const [bets, setBets] = useState({});
  const [phase, setPhase] = useState('idle');
  // 当前桌面上的一局：服务端的结果、已经翻开几张牌，以及当时桌面上的注。
  const [shown, setShown] = useState(null);
  const [error, setError] = useState('');
  const [hint, setHint] = useState('');
  // 本次打开页面以来每一局的赢家(珠盘路)，最早的在前。
  const [road, setRoad] = useState([]);
  const roadIdRef = useRef(0);
  const pageRef = useRef(null);
  const auto = useAutoPlay();

  useEffect(() => {
    const page = { timer: 0 };
    pageRef.current = page;
    return () => {
      clearTimeout(page.timer);
      pageRef.current = null;
    };
  }, []);

  if (!info) {
    return (
      <GameLayout
        title={meta.name}
        loading={loading}
        unavailable={unavailable}
      />
    );
  }

  const symbol = info.symbol || '';
  const maxBet = Math.max(info.max_bet || 1, 1);
  const payouts = info.payouts || {};
  const payback = info.payback || {};
  const spots = info.spots || [];
  const chips = CHIP_VALUES.filter(
    (value) => value <= maxBet && value >= (info.min_bet || 0.01),
  );
  const chipValue = chips.includes(chip) ? chip : chips[0];
  const totalCents = Object.values(bets).reduce(
    (sum, amount) => sum + toCoinCents(amount),
    0,
  );
  const total = totalCents / 100;
  const busy = phase !== 'idle';
  const canDeal =
    !busy && totalCents >= 1 && total <= maxBet && total <= info.balance;
  const names = {
    player: t('闲'),
    banker: t('庄'),
    tie: t('和'),
    player_pair: t('闲对'),
    banker_pair: t('庄对'),
  };
  // 押中时赔多少(不含本金)：派彩以百分之一倍计、含本金。
  const odds = (spot) => {
    const net = ((payouts[spot] || 100) - 100) / 100;
    return `${Number(net.toFixed(2))}:1`;
  };

  const place = (spot) => {
    if (busy) return;
    if (totalCents + toCoinCents(chipValue) > toCoinCents(maxBet)) {
      setHint(
        t('一局合计最多下注 {{max}} {{symbol}}', { max: maxBet, symbol }),
      );
      return;
    }
    playSound('chip');
    setHint('');
    setBets({
      ...bets,
      [spot]: (toCoinCents(bets[spot] || 0) + toCoinCents(chipValue)) / 100,
    });
  };

  const deal = async () => {
    const page = pageRef.current;
    if (!canDeal || !page) {
      auto.stop();
      return;
    }
    const placed = bets;
    setPhase('dealing');
    setError('');
    setHint('');
    setShown(null);
    playSound('start');
    const response = await gamePost(
      '/api/game/baccarat/deal',
      {
        bets: spots
          .filter((spot) => placed[spot])
          .map((spot) => ({ spot, amount: placed[spot] })),
      },
      t,
    );
    if (pageRef.current !== page) return;
    if (response.error) {
      setPhase('idle');
      setError(response.error);
      auto.stop();
      reload(true);
      return;
    }
    const data = response.data;
    const order = dealOrder(data.round);
    // 发牌是开奖过程，按顺序一张张翻开；补牌之前多停一会儿，最后才公布输赢、更新余额。
    for (let count = 1; count <= order.length; count++) {
      await delay(count > 4 ? THIRD_MS : CARD_MS, page);
      setShown({ data, bets: placed, revealed: count });
      playSound('deal');
    }
    await delay(CARD_MS, page);
    setPhase('idle');
    roadIdRef.current += 1;
    setRoad((items) =>
      [...items, { id: roadIdRef.current, winner: data.round.winner }].slice(
        -ROAD_SIZE,
      ),
    );
    patch({
      balance: data.balance,
      net_today: data.net_today,
      plays_today: data.plays_today,
      best_win: Math.max(info.best_win || 0, data.win),
    });
    playWagerFeedback(data);
    auto.after(true, { bigWin: data.win >= BIG_WIN_MULTIPLIER * data.bet });
  };
  auto.bind(deal);

  const round = shown?.data.round;
  const order = round ? dealOrder(round) : [];
  const visible = { player: [], banker: [] };
  order.slice(0, shown?.revealed || 0).forEach(([side, index]) => {
    visible[side].push(round[side].cards[index]);
  });
  const settled = !busy && shown;
  const wins =
    settled && shown.bets === bets
      ? Object.fromEntries(shown.data.bets.map((bet) => [bet.spot, bet.win]))
      : null;

  let message = {
    kind: '',
    text: t('选好筹码点桌面下注，可以同时押好几处，然后发牌'),
  };
  if (busy) {
    message = { kind: '', text: t('发牌中…') };
  } else if (error) {
    message = { kind: 'error', text: error };
  } else if (settled) {
    message = {
      ...wagerResultMessage(shown.data, t, symbol),
      note:
        round.winner === 'tie'
          ? t('闲 {{player}} 点，庄 {{banker}} 点，和局', {
              player: round.player.total,
              banker: round.banker.total,
            })
          : t('闲 {{player}} 点，庄 {{banker}} 点，{{winner}}赢', {
              player: round.player.total,
              banker: round.banker.total,
              winner: names[round.winner],
            }),
    };
  }
  let shownHint = hint;
  if (!shownHint && total > info.balance) shownHint = t('游戏币余额不足');

  const side = (
    <>
      <WagerStats info={info} symbol={symbol} />
      <SideBox label={t('赔率')}>
        <table className='game-table'>
          <thead>
            <tr>
              <th>{t('押法')}</th>
              <th>{t('赔率')}</th>
              <th>{t('返还率')}</th>
            </tr>
          </thead>
          <tbody>
            {spots.map((spot) => (
              <tr key={spot}>
                <td>{names[spot]}</td>
                <td>{odds(spot)}</td>
                <td>{percent(payback[spot])}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <RuleList
          items={[
            t(
              '闲、庄各发两张，点数取个位：A 算 1，10、J、Q、K 算 0，更接近 9 的一方赢',
            ),
            t(
              '任一方头两张是 8 或 9 点即停牌；否则闲 0–5 点补一张，庄按闲的第三张决定是否补牌',
            ),
            t('押庄赢时抽 5%，和局时押闲、押庄退回本金'),
            t('闲对、庄对：这一家头两张点数相同'),
            t(
              '{{decks}} 副牌，每局重新洗牌；所有押法的返还率都低于 100%，玩家长期是输的',
              {
                decks: info.decks || 8,
              },
            ),
          ]}
        />
      </SideBox>
    </>
  );

  return (
    <GameLayout
      title={meta.name}
      balance={info.balance}
      symbol={symbol}
      side={side}
      className='baccarat-page'
    >
      <div
        className={
          message.kind === 'big-win' ? 'games-stage is-big-win' : 'games-stage'
        }
      >
        <div className='baccarat-felt'>
          {['player', 'banker'].map((hand) => (
            <div
              key={hand}
              className={`baccarat-hand is-${hand} ${settled && round.winner === hand ? 'is-winner' : ''}`}
            >
              <div className='baccarat-hand-title'>
                <span>{names[hand]}</span>
                <strong>
                  {visible[hand].length ? handTotal(visible[hand]) : '-'}
                </strong>
              </div>
              <div className='baccarat-cards'>
                {visible[hand].map((card, index) => (
                  <BaccaratCard key={index} card={card} />
                ))}
              </div>
            </div>
          ))}
        </div>

        <StageMessage kind={message.kind} note={message.note}>
          {message.text}
        </StageMessage>

        <div className='baccarat-spots'>
          {spots.map((spot) => {
            const won = wins?.[spot];
            const marked =
              wins && won !== undefined
                ? won > (bets[spot] || 0)
                  ? 'is-win'
                  : won > 0
                    ? 'is-push'
                    : 'is-loss'
                : '';
            return (
              <button
                key={spot}
                type='button'
                className={`baccarat-spot is-${spot} ${marked}`}
                disabled={busy}
                aria-label={t('押{{spot}}', { spot: names[spot] })}
                onClick={() => place(spot)}
              >
                <span className='baccarat-spot-name'>{names[spot]}</span>
                <small>{odds(spot)}</small>
                {bets[spot] > 0 && (
                  <span className='baccarat-spot-bet'>
                    {formatCoins(bets[spot])}
                  </span>
                )}
              </button>
            );
          })}
        </div>

        <div className='baccarat-road' aria-label={t('珠盘路')}>
          {road.map((item) => (
            <span key={item.id} className={`is-${item.winner}`}>
              {names[item.winner]}
            </span>
          ))}
        </div>

        <div className='baccarat-chips' role='group' aria-label={t('筹码')}>
          {chips.map((value) => (
            <button
              key={value}
              type='button'
              className={`baccarat-chip${value === chipValue ? ' is-active' : ''}`}
              aria-pressed={value === chipValue}
              aria-label={t('筹码 {{value}}', { value })}
              onClick={() => setChip(value)}
            >
              {value}
            </button>
          ))}
        </div>

        <div className='game-controls'>
          <span className='baccarat-total'>
            {t('合计下注')}
            <strong>{total}</strong>
            {` / ${maxBet} ${symbol}`}
          </span>
          <button
            type='button'
            className='baccarat-clear'
            disabled={busy || totalCents === 0}
            onClick={() => {
              setBets({});
              setHint('');
            }}
          >
            <Trash2 size={14} />
            {t('清空')}
          </button>
          <button
            type='button'
            className='games-play-button'
            disabled={!canDeal || auto.active}
            onClick={deal}
          >
            {busy ? t('发牌中…') : t('发牌')}
          </button>
          <AutoPlayControls auto={auto} disabled={!canDeal} />
          {shownHint && <div className='games-stage-hint'>{shownHint}</div>}
        </div>
      </div>
    </GameLayout>
  );
};

export default Baccarat;
