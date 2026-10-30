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
import BetInput from '../common/BetInput';
import GameLayout from '../common/GameLayout';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { formatCoins, gamePost, percent, toCoinCents } from '../common/api';
import { useGameInfo } from '../common/useGameInfo';
import {
  WagerStats,
  playWagerFeedback,
  wagerResultMessage,
} from '../common/wager';
import { playSound } from '../common/sound';
import { WINNER_MS, GAP_MS, leaderAt, planRace, progressAt } from './logic';
import './horserace.css';

const HORSE_COLORS = [
  '#ef4444',
  '#f59e0b',
  '#10b981',
  '#3b82f6',
  '#a855f7',
  '#64748b',
];
const KINDS = ['win', 'place'];
const formatOdds = (value) => `×${Number((value / 100).toFixed(2))}`;
const betKey = (kind, horse) => `${kind}-${horse}`;

const Horserace = () => {
  const { t } = useTranslation();
  const meta = getGameMetaMap(t).horserace;
  const horseNames = [
    t('赤兔'),
    t('的卢'),
    t('绝影'),
    t('爪黄'),
    t('乌骓'),
    t('照夜白'),
  ];
  const { loading, unavailable, info, patch, reload } =
    useGameInfo('horserace');
  const [chip, setChip] = useState(1);
  // 押注单：键是 玩法-马，值是金额(分)。
  const [bets, setBets] = useState({});
  const [phase, setPhase] = useState('idle');
  // 当前这一场：服务端的结果与比赛开始的时刻；elapsed 是动画进行到第几毫秒。
  const [race, setRace] = useState(null);
  const [elapsed, setElapsed] = useState(0);
  const [error, setError] = useState('');
  const pageRef = useRef(null);

  useEffect(() => {
    const page = { frame: 0, alive: true };
    pageRef.current = page;
    return () => {
      page.alive = false;
      cancelAnimationFrame(page.frame);
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
  const horses = info.horses || [];
  const maxCents = toCoinCents(Math.max(info.max_bet || 1, 1));
  const totalCents = Object.values(bets).reduce((n, c) => n + c, 0);
  const busy = phase !== 'idle';
  const canRace =
    !busy && totalCents > 0 && totalCents <= toCoinCents(info.balance);

  const addBet = (kind, horse) => {
    if (busy) return;
    const key = betKey(kind, horse);
    const add = Math.min(toCoinCents(chip), maxCents - totalCents);
    if (add <= 0) {
      playSound('error');
      return;
    }
    playSound('chip');
    setBets({ ...bets, [key]: (bets[key] || 0) + add });
    if (phase === 'idle' && race?.done) setRace(null);
  };

  const run = async () => {
    const page = pageRef.current;
    if (!canRace || !page) return;
    const slip = Object.entries(bets).map(([key, cents]) => {
      const [kind, horse] = key.split('-');
      return { kind, horse: Number(horse), amount: cents / 100 };
    });
    setPhase('racing');
    setError('');
    setRace(null);
    setElapsed(0);
    playSound('start');
    const response = await gamePost(
      '/api/game/horserace/race',
      { bets: slip },
      t,
    );
    if (!page.alive) return;
    if (response.error) {
      setPhase('idle');
      setError(response.error);
      reload(true);
      return;
    }
    const data = response.data;
    const plans = planRace(data.order, Date.now());
    const total = WINNER_MS + GAP_MS * (data.order.length - 1) + 400;
    setRace({ data, plans, done: false });
    const started = performance.now();
    let lastGallop = 0;
    const tick = (now) => {
      if (!page.alive) return;
      const time = now - started;
      setElapsed(time);
      if (time - lastGallop > 330 && time < WINNER_MS) {
        lastGallop = time;
        playSound('gallop', { pitch: 0.9 + Math.random() * 0.2 });
      }
      if (time < total) {
        page.frame = requestAnimationFrame(tick);
        return;
      }
      setRace({ data, plans, done: true });
      setPhase('idle');
      patch({
        balance: data.balance,
        net_today: data.net_today,
        plays_today: data.plays_today,
        best_win: Math.max(info.best_win || 0, data.win),
      });
      playWagerFeedback(data);
    };
    page.frame = requestAnimationFrame(tick);
  };

  const order = race?.data.order || [];
  const finishedRank = (horse) =>
    race && elapsed >= race.plans[horse].finish ? order.indexOf(horse) : -1;
  const leader = race && !race.done ? leaderAt(race.plans, elapsed) : -1;

  let message = {
    kind: '',
    text: t('押独赢(跑第一)或位置(跑进前二)，可以同时押好几注'),
  };
  if (busy) {
    message = {
      kind: '',
      text:
        !race || elapsed < 400
          ? t('各就各位…')
          : elapsed < WINNER_MS * 0.85
            ? t('{{horse}} 领先！', { horse: horseNames[leader] })
            : t('冲刺！'),
    };
  } else if (error) {
    message = { kind: 'error', text: error };
  } else if (race?.done) {
    message = {
      ...wagerResultMessage(race.data, t, symbol),
      note: t('冠军 {{first}}，亚军 {{second}}', {
        first: horseNames[order[0]],
        second: horseNames[order[1]],
      }),
    };
  }

  const settled = race?.done ? race.data.bets : null;
  const side = (
    <>
      <WagerStats info={info} symbol={symbol} />
      <SideBox label={t('赔率与概率')}>
        <table className='game-table horserace-odds'>
          <thead>
            <tr>
              <th>{t('马')}</th>
              <th>{t('独赢')}</th>
              <th>{t('位置')}</th>
            </tr>
          </thead>
          <tbody>
            {horses.map((horse, index) => (
              <tr key={index}>
                <td>
                  <span
                    className='horserace-silk'
                    style={{ '--silk': HORSE_COLORS[index] }}
                  >
                    {index + 1}
                  </span>
                  {horseNames[index]}
                </td>
                <td>
                  {formatOdds(horse.win_odds)}
                  <small>{percent(horse.win_chance, 0)}</small>
                </td>
                <td>
                  {formatOdds(horse.place_odds)}
                  <small>{percent(horse.place_chance, 0)}</small>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <RuleList
          items={[
            t('六匹马实力不同，赔率按跑进名次的概率定，括号里是概率'),
            t('独赢押的马跑第一才中，位置押的马跑进前两名就中'),
            t('派彩 = 押注 × 赔率(含本金)，一场可以押多注，合计不超过单次上限'),
            t('每一注的理论返还率在 94% 到 96% 之间，玩家长期是输的'),
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
      className='horserace-page'
    >
      <div
        className={
          message.kind === 'big-win' ? 'games-stage is-big-win' : 'games-stage'
        }
      >
        <div className={`horserace-track ${busy ? 'is-racing' : ''}`}>
          <div className='horserace-finish' aria-hidden />
          {horses.map((_, horse) => {
            const progress = race ? progressAt(race.plans[horse], elapsed) : 0;
            const rank = finishedRank(horse);
            return (
              <div key={horse} className='horserace-lane'>
                <span className='horserace-lane-no'>{horse + 1}</span>
                <span
                  className={`horserace-horse ${horse === leader ? 'is-leader' : ''} ${rank >= 0 ? 'is-finished' : ''}`}
                  style={{
                    '--progress': progress,
                    '--silk': HORSE_COLORS[horse],
                  }}
                  aria-label={horseNames[horse]}
                >
                  <span className='horserace-body' aria-hidden>
                    🐎
                  </span>
                  <span className='horserace-number'>{horse + 1}</span>
                  {busy && progress > 0 && progress < 1 && (
                    <span className='horserace-dust' aria-hidden />
                  )}
                </span>
                {rank >= 0 && (
                  <span className={`horserace-rank is-r${rank}`}>
                    {rank + 1}
                  </span>
                )}
              </div>
            );
          })}
        </div>

        {race?.done && (
          <div className='horserace-podium' role='status'>
            {order.slice(0, 3).map((horse, rank) => (
              <span key={horse} className={`horserace-step is-r${rank}`}>
                <b
                  className='horserace-silk'
                  style={{ '--silk': HORSE_COLORS[horse] }}
                >
                  {horse + 1}
                </b>
                {horseNames[horse]}
              </span>
            ))}
          </div>
        )}

        <StageMessage kind={message.kind} note={message.note}>
          {message.text}
        </StageMessage>

        <div className='horserace-board' role='group' aria-label={t('押注单')}>
          <span className='horserace-board-head'>{t('马')}</span>
          <span className='horserace-board-head'>{t('独赢')}</span>
          <span className='horserace-board-head'>{t('位置')}</span>
          {horses.map((horse, index) => (
            <React.Fragment key={index}>
              <span className='horserace-board-name'>
                <span
                  className='horserace-silk'
                  style={{ '--silk': HORSE_COLORS[index] }}
                >
                  {index + 1}
                </span>
                {horseNames[index]}
              </span>
              {KINDS.map((kind) => {
                const key = betKey(kind, index);
                const cents = bets[key] || 0;
                const won = settled?.find(
                  (bet) =>
                    bet.kind === kind && bet.horse === index && bet.win > 0,
                );
                return (
                  <button
                    key={kind}
                    type='button'
                    className={`horserace-spot ${cents ? 'has-bet' : ''} ${won ? 'is-won' : ''}`}
                    disabled={busy}
                    onClick={() => addBet(kind, index)}
                  >
                    <span>
                      {formatOdds(
                        kind === 'win' ? horse.win_odds : horse.place_odds,
                      )}
                    </span>
                    {cents > 0 && (
                      <b className='horserace-chip'>
                        {formatCoins(cents / 100)}
                      </b>
                    )}
                  </button>
                );
              })}
            </React.Fragment>
          ))}
        </div>

        <div className='game-controls'>
          <BetInput
            value={chip}
            min={info.min_bet}
            max={Math.max(info.max_bet || 1, 1)}
            symbol={symbol}
            disabled={busy}
            onChange={setChip}
            label={t('每次加注')}
          />
          <button
            type='button'
            className='horserace-clear'
            disabled={busy || !totalCents}
            onClick={() => setBets({})}
          >
            <Trash2 size={14} />
            {t('清空')}
          </button>
          <button
            type='button'
            className='games-play-button'
            disabled={!canRace}
            onClick={run}
          >
            {busy
              ? t('比赛中…')
              : t('开跑 · 押 {{total}} {{symbol}}', {
                  total: formatCoins(totalCents / 100),
                  symbol,
                })}
          </button>
          {!totalCents && !busy && (
            <div className='games-stage-hint'>
              {t('点赔率格子下注，再点一次加注')}
            </div>
          )}
          {totalCents > toCoinCents(info.balance) && (
            <div className='games-stage-hint'>{t('游戏币余额不足')}</div>
          )}
        </div>
      </div>
    </GameLayout>
  );
};

export default Horserace;
