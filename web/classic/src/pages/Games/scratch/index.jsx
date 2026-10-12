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

import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getGameMetaMap } from '../../../constants';
import GameLayout from '../common/GameLayout';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { gamePost, percent } from '../common/api';
import { useGameInfo } from '../common/useGameInfo';
import { WagerStats, wagerResultMessage } from '../common/wager';
import ScratchCard, { CELL_COUNT } from './ScratchCard';
import './scratch.css';

// 每格都没揭开(也用作"没有中奖的格子")与全部揭开。
const NO_CELLS = new Array(CELL_COUNT).fill(false);
const ALL_CELLS = new Array(CELL_COUNT).fill(true);

const Scratch = () => {
  const { t } = useTranslation();
  const meta = getGameMetaMap(t).scratch;
  const { loading, unavailable, info, patch, reload } = useGameInfo('scratch');
  const [price, setPrice] = useState(0);
  const [buying, setBuying] = useState(false);
  // 买到的一张票：票面金额、票价与服务端的结算结果。结果在购买时就已确定，刮开只是揭晓。
  const [ticket, setTicket] = useState(null);
  const [revealed, setRevealed] = useState(NO_CELLS);
  const [error, setError] = useState('');
  const ticketIdRef = useRef(0);
  // 已经公布过结果的票，每张只公布一次。
  const settledRef = useRef(0);
  const aliveRef = useRef(true);

  useEffect(() => {
    aliveRef.current = true;
    return () => {
      aliveRef.current = false;
    };
  }, []);

  const revealCell = useCallback((cell) => {
    setRevealed((previous) =>
      previous[cell]
        ? previous
        : previous.map((value, index) => value || index === cell),
    );
  }, []);

  const finished = !!ticket && revealed.every(Boolean);
  const bestWin = info?.best_win || 0;

  // 整张票都刮开后才公布结果、更新余额与今日输赢，刮的过程中余额不会提前剧透。
  useEffect(() => {
    if (!finished || settledRef.current === ticket.id) return;
    settledRef.current = ticket.id;
    const result = ticket.result;
    patch({
      balance: result.balance,
      net_today: result.net_today,
      plays_today: result.plays_today,
      best_win: Math.max(bestWin, result.win),
    });
  }, [finished, ticket, bestWin, patch]);

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
  const prices = info.prices || [];
  const openPrices = prices
    .filter((item) => item.open)
    .map((item) => item.price);
  // 默认选开放的最低一档；管理员关掉了选中的一档时也退回这一档。
  const shownPrice = openPrices.includes(price) ? price : openPrices[0] || 0;
  const scratching = !!ticket && !finished;
  const canBuy =
    !buying && !scratching && shownPrice > 0 && shownPrice <= info.balance;

  const buy = async () => {
    if (!canBuy) return;
    setBuying(true);
    setError('');
    const response = await gamePost(
      '/api/game/scratch/buy',
      { price: shownPrice },
      t,
    );
    if (!aliveRef.current) return;
    setBuying(false);
    if (response.error) {
      setError(response.error);
      // 余额或规则可能已经变了(没有得到答复时这张票也可能已经买下)，重新读取。
      reload(true);
      return;
    }
    const data = response.data;
    ticketIdRef.current += 1;
    setTicket({
      id: ticketIdRef.current,
      price: data.price,
      cells: data.cells,
      result: data,
    });
    setRevealed(NO_CELLS);
  };

  const result = finished ? ticket.result : null;
  const matches =
    result && result.prize > 0
      ? ticket.cells.map((amount) => amount === result.prize)
      : NO_CELLS;
  const wonMultiple =
    result && result.prize > 0 ? result.prize / result.price : 0;

  let message = { kind: '', text: t('选好票价，买一张刮开看看') };
  if (buying) {
    message = { kind: '', text: t('出票中…') };
  } else if (error) {
    message = { kind: 'error', text: error };
  } else if (result) {
    message = {
      ...wagerResultMessage(result, t, symbol),
      note:
        result.prize > 0
          ? t('刮出 3 个 {{amount}} {{symbol}}', {
              amount: result.prize,
              symbol,
            })
          : undefined,
    };
  } else if (ticket) {
    message = {
      kind: '',
      text: t('按住涂层拖动刮开，也可以一键全部刮开'),
      note: t('已刮开 {{count}} / {{total}} 格', {
        count: revealed.filter(Boolean).length,
        total: CELL_COUNT,
      }),
    };
  }
  const hint =
    !scratching && shownPrice > info.balance ? t('游戏币余额不足') : '';

  const prizes = [...(info.prizes || [])].reverse();
  const winChance = prizes.reduce((sum, prize) => sum + prize.chance, 0);
  const side = (
    <>
      <WagerStats info={info} symbol={symbol} />
      <SideBox
        label={t('奖品表 · 票价 {{price}} {{symbol}}', {
          price: shownPrice,
          symbol,
        })}
      >
        <table className='game-table'>
          <thead>
            <tr>
              <th>{t('倍数')}</th>
              <th>{t('奖金')}</th>
              <th>{t('概率')}</th>
            </tr>
          </thead>
          <tbody>
            {prizes.map((prize) => (
              <tr
                key={prize.multiple}
                className={
                  prize.multiple === wonMultiple ? 'is-active' : undefined
                }
              >
                <td>{`×${prize.multiple}`}</td>
                <td>{`${shownPrice * prize.multiple} ${symbol}`}</td>
                <td>{percent(prize.chance)}</td>
              </tr>
            ))}
            <tr>
              <td colSpan={2}>{t('不中奖')}</td>
              <td>{percent(1 - winChance)}</td>
            </tr>
          </tbody>
        </table>
        <RuleList
          items={[
            t(
              '每张票 9 格，刮出 3 格相同的金额就中这个金额，一张票最多中一个金额',
            ),
            t('奖金是票价的倍数，三档票价共用这张奖品表，1 倍是退回票价'),
            t('中奖概率合计 {{chance}}，理论返还率 {{rate}}，玩家长期是输的', {
              chance: percent(winChance),
              rate: percent(info.payback),
            }),
            t('结果在购买时就已由服务端确定，刮开只是揭晓，不限次数'),
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
      className='scratch-page'
    >
      <div
        className={
          message.kind === 'big-win' ? 'games-stage is-big-win' : 'games-stage'
        }
      >
        <ScratchCard
          ticket={ticket}
          title={meta.name}
          price={shownPrice}
          symbol={symbol}
          revealed={revealed}
          matches={matches}
          onReveal={revealCell}
        />

        <StageMessage kind={message.kind} note={message.note}>
          {message.text}
        </StageMessage>

        <div className='game-controls'>
          <div className='scratch-prices'>
            <span className='scratch-prices-label'>{t('票价')}</span>
            <div className='game-chips'>
              {prices.map((item) => (
                <button
                  key={item.price}
                  type='button'
                  className={
                    item.price === shownPrice
                      ? 'game-chip is-active'
                      : 'game-chip'
                  }
                  aria-pressed={item.price === shownPrice}
                  disabled={!item.open || buying || scratching}
                  title={item.open ? undefined : t('这一档还没开放')}
                  onClick={() => setPrice(item.price)}
                >
                  {`${item.price} ${symbol}`}
                </button>
              ))}
            </div>
          </div>
          {scratching ? (
            <button
              type='button'
              className='games-play-button'
              onClick={() => setRevealed(ALL_CELLS)}
            >
              {t('全部刮开')}
            </button>
          ) : (
            <button
              type='button'
              className='games-play-button'
              disabled={!canBuy}
              onClick={buy}
            >
              {buying
                ? t('出票中…')
                : t('买一张 {{price}} {{symbol}}', {
                    price: shownPrice,
                    symbol,
                  })}
            </button>
          )}
          {hint && <div className='games-stage-hint'>{hint}</div>}
        </div>
      </div>
    </GameLayout>
  );
};

export default Scratch;
