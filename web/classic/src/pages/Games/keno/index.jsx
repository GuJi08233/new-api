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
import { Shuffle, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getGameMetaMap } from '../../../constants';
import BetInput from '../common/BetInput';
import GameLayout from '../common/GameLayout';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { gamePost, percent } from '../common/api';
import { useGameInfo } from '../common/useGameInfo';
import {
  BIG_WIN_MULTIPLIER,
  WagerStats,
  playWagerFeedback,
  wagerResultMessage,
} from '../common/wager';
import { AutoPlayControls, useAutoPlay } from '../common/autoPlay';
import { playSound } from '../common/sound';
import './keno.css';

// 开出每个号码的间隔。
const DRAW_MS = 160;
const QUICK_PICK = 5;

function delay(ms, page) {
  return new Promise((resolve) => {
    page.timer = setTimeout(resolve, ms);
  });
}

// 倍数以百分之一倍计，显示成 ×3.8。
const formatMultiplier = (value) => `×${Number((value / 100).toFixed(2))}`;

const Keno = () => {
  const { t } = useTranslation();
  const meta = getGameMetaMap(t).keno;
  const { loading, unavailable, info, patch, reload } = useGameInfo('keno');
  const [bet, setBet] = useState(1);
  const [picks, setPicks] = useState([]);
  const [phase, setPhase] = useState('idle');
  // 当前这一局：服务端的结果、已经开出几个号码，以及开奖时选的号码。
  const [shown, setShown] = useState(null);
  const [error, setError] = useState('');
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
  const numbers = info.numbers || 40;
  const draws = info.draws || 10;
  const maxPicks = info.max_picks || 10;
  const maxBet = Math.max(info.max_bet || 1, 1);
  const stake = Math.min(bet, maxBet);
  const busy = phase !== 'idle';
  const canPlay = !busy && picks.length > 0 && stake <= info.balance;
  const row = info.paytable?.[picks.length] || [];
  const chances = info.chances?.[picks.length] || [];

  const toggle = (number) => {
    if (busy) return;
    if (picks.includes(number)) {
      playSound('click');
      setPicks(picks.filter((n) => n !== number));
    } else if (picks.length < maxPicks) {
      playSound('chip');
      setPicks([...picks, number].sort((a, b) => a - b));
    }
    setShown(null);
  };
  const quickPick = () => {
    const count = picks.length || QUICK_PICK;
    const pool = Array.from({ length: numbers }, (_, i) => i + 1);
    for (let i = pool.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1));
      [pool[i], pool[j]] = [pool[j], pool[i]];
    }
    playSound('rotate');
    setPicks(pool.slice(0, count).sort((a, b) => a - b));
    setShown(null);
  };

  const play = async () => {
    const page = pageRef.current;
    if (!canPlay || !page) {
      auto.stop();
      return;
    }
    const chosen = picks;
    setPhase('drawing');
    setError('');
    setShown(null);
    playSound('start');
    const response = await gamePost(
      '/api/game/keno/play',
      { bet: stake, picks: chosen },
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
    // 开奖过程：号码一个个开出，选中的号码亮起；开完才公布结果、更新余额。
    for (let count = 1; count <= data.drawn.length; count++) {
      await delay(DRAW_MS, page);
      setShown({ data, picks: chosen, revealed: count });
      const number = data.drawn[count - 1];
      if (chosen.includes(number))
        playSound('reveal', { pitch: 1 + count / 20 });
      else playSound('tick');
    }
    await delay(DRAW_MS * 2, page);
    setPhase('idle');
    patch({
      balance: data.balance,
      net_today: data.net_today,
      plays_today: data.plays_today,
      best_win: Math.max(info.best_win || 0, data.win),
    });
    playWagerFeedback(data);
    auto.after(true, { bigWin: data.win >= BIG_WIN_MULTIPLIER * data.bet });
  };
  auto.bind(play);

  const drawn = new Set(shown ? shown.data.drawn.slice(0, shown.revealed) : []);
  const hits = picks.filter((n) => drawn.has(n)).length;
  const settled = !busy && shown;

  let message = {
    kind: '',
    text: t('选 1 到 {{max}} 个号码，开出 {{draws}} 个，中得越多倍数越高', {
      max: maxPicks,
      draws,
    }),
  };
  if (busy) {
    message = {
      kind: '',
      text: t('开奖中… 已中 {{hits}} 个', { hits }),
    };
  } else if (error) {
    message = { kind: 'error', text: error };
  } else if (settled) {
    message = {
      ...wagerResultMessage(shown.data, t, symbol),
      note: t('选 {{picks}} 中 {{hits}}，{{multiplier}}', {
        picks: shown.picks.length,
        hits: shown.data.hits,
        multiplier: formatMultiplier(shown.data.multiplier),
      }),
    };
  }

  const side = (
    <>
      <WagerStats info={info} symbol={symbol} />
      <SideBox label={t('赔付表')}>
        {picks.length ? (
          <table className='game-table keno-paytable'>
            <thead>
              <tr>
                <th>{t('中')}</th>
                <th>{t('倍数')}</th>
                <th>{t('概率')}</th>
              </tr>
            </thead>
            <tbody>
              {row
                .map((value, hit) => ({ value, hit }))
                .filter(({ value }) => value > 0)
                .reverse()
                .map(({ value, hit }) => (
                  <tr
                    key={hit}
                    className={
                      settled && shown.data.hits === hit ? 'is-hit' : ''
                    }
                  >
                    <td>{hit}</td>
                    <td>{formatMultiplier(value)}</td>
                    <td>{percent(chances[hit], 3)}</td>
                  </tr>
                ))}
            </tbody>
          </table>
        ) : (
          <div className='game-box-note'>{t('选好号码后显示这一组的赔付')}</div>
        )}
        <RuleList
          items={[
            t(
              '从 1–{{numbers}} 里选 1 到 {{max}} 个号码，每局开出 {{draws}} 个',
              {
                numbers,
                max: maxPicks,
                draws,
              },
            ),
            t('按选了几个、中了几个查赔付表，派彩 = 下注 × 倍数，最高 1000 倍'),
            t('选号保留到下一局，可以一直用同一组号码'),
            picks.length
              ? t(
                  '选 {{picks}} 个号码的理论返还率是 {{rate}}，玩家长期是输的',
                  {
                    picks: picks.length,
                    rate: percent(info.payback?.[picks.length]),
                  },
                )
              : t('每一种选法的返还率都低于 100%，玩家长期是输的'),
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
      className='keno-page'
    >
      <div
        className={
          message.kind === 'big-win' ? 'games-stage is-big-win' : 'games-stage'
        }
      >
        <div className='game-hud keno-hud'>
          <span>
            {t('已选')}
            <strong>{picks.length}</strong>
            {` / ${maxPicks}`}
          </span>
          <span>
            {t('已中')}
            <strong>{shown ? hits : '-'}</strong>
          </span>
          <span>
            {t('开出')}
            <strong>{drawn.size}</strong>
            {` / ${draws}`}
          </span>
        </div>

        <div className='keno-board' role='group' aria-label={t('号码')}>
          {Array.from({ length: numbers }, (_, i) => {
            const number = i + 1;
            const picked = picks.includes(number);
            const out = drawn.has(number);
            const classes = ['keno-number'];
            if (picked) classes.push('is-picked');
            if (out) classes.push(picked ? 'is-hit' : 'is-drawn');
            return (
              <button
                key={number}
                type='button'
                className={classes.join(' ')}
                aria-pressed={picked}
                disabled={busy || (!picked && picks.length >= maxPicks)}
                onClick={() => toggle(number)}
              >
                {number}
              </button>
            );
          })}
        </div>

        <StageMessage kind={message.kind} note={message.note}>
          {message.text}
        </StageMessage>

        <div className='game-controls'>
          <div className='keno-actions'>
            <button
              type='button'
              className='keno-action'
              disabled={busy}
              onClick={quickPick}
            >
              <Shuffle size={14} />
              {t('机选')}
            </button>
            <button
              type='button'
              className='keno-action'
              disabled={busy || !picks.length}
              onClick={() => {
                setPicks([]);
                setShown(null);
              }}
            >
              <Trash2 size={14} />
              {t('清空')}
            </button>
          </div>
          <BetInput
            value={stake}
            min={info.min_bet}
            max={maxBet}
            symbol={symbol}
            disabled={busy}
            onChange={setBet}
          />
          <button
            type='button'
            className='games-play-button'
            disabled={!canPlay || auto.active}
            onClick={play}
          >
            {busy ? t('开奖中…') : t('开奖')}
          </button>
          <AutoPlayControls auto={auto} disabled={!canPlay} />
          {!picks.length && !busy && (
            <div className='games-stage-hint'>{t('先选至少一个号码')}</div>
          )}
          {picks.length > 0 && stake > info.balance && (
            <div className='games-stage-hint'>{t('游戏币余额不足')}</div>
          )}
        </div>
      </div>
    </GameLayout>
  );
};

export default Keno;
