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
import { Check, Grab, Hand, Hourglass, Scissors, Swords } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getGameMetaMap } from '../../../constants';
import PvpGame from '../common/pvp/PvpGame';
import './rps.css';

// 一回合公开之后，双方的拳在台面上停留这么久，然后回到这一回合的出拳。
const REVEAL_MS = 2200;

// 三种拳的图标，也是按钮的顺序。
const CHOICE_ICONS = { rock: Grab, scissors: Scissors, paper: Hand };
const CHOICES = Object.keys(CHOICE_ICONS);

// 一个座位这一回合的拳：公开的拳画出图标；对手已经出了但还没公开时只显示"已出拳"，绝不猜它出了什么。
const HandCard = ({ choice, ready, result, reveal, label, names }) => {
  const { t } = useTranslation();
  let body;
  let tone = '';
  if (choice) {
    const Icon = CHOICE_ICONS[choice];
    tone = result ? `is-${result}` : 'is-chosen';
    body = (
      <>
        <Icon size={44} strokeWidth={1.6} />
        <strong>{names[choice]}</strong>
      </>
    );
  } else if (ready) {
    tone = 'is-ready';
    body = (
      <>
        <Check size={36} strokeWidth={1.8} />
        <strong>{t('已出拳')}</strong>
      </>
    );
  } else {
    body = (
      <>
        <Hourglass size={32} strokeWidth={1.6} />
        <strong>{t('还没出拳')}</strong>
      </>
    );
  }
  return (
    <div className={`rps-hand ${tone} ${reveal ? 'is-reveal' : ''}`}>
      <span className='rps-hand-label'>{label}</span>
      {body}
    </div>
  );
};

// 比分、这一回合双方的状态、三个出拳按钮与每回合的记录。刚公开的一回合短暂亮出双方的拳，比赛结束后停在最后一回合。
const RpsBoard = ({ state, seat, canMove, onMove }) => {
  const { t } = useTranslation();
  const names = { rock: t('石头'), scissors: t('剪刀'), paper: t('布') };
  const rounds = state?.rounds || [];
  const count = rounds.length;
  const me = seat;
  const opponent = 1 - seat;
  const wins = state?.wins || [0, 0];

  // 刚公开的回合下标。打开页面时已经打完的回合不再回放。
  const seenRef = useRef(count);
  const [reveal, setReveal] = useState(null);
  useEffect(() => {
    const fresh = count > seenRef.current;
    seenRef.current = count;
    if (!fresh) return undefined;
    setReveal(count - 1);
    const timer = setTimeout(() => setReveal(null), REVEAL_MS);
    return () => clearTimeout(timer);
  }, [count]);

  const shownIndex = state?.over && count > 0 ? count - 1 : reveal;
  const shown = shownIndex !== null ? rounds[shownIndex] : null;
  const outcomeOf = (round) => {
    if (round.winner === -1) return 'draw';
    return round.winner === me ? 'win' : 'loss';
  };
  const outcome = shown && outcomeOf(shown);
  const outcomeText = {
    win: t('你赢了这一回合'),
    loss: t('你输了这一回合'),
    draw: state?.over ? t('这一回合平局') : t('平局，这一回合重来'),
  };
  const badges = { win: t('胜'), loss: t('负'), draw: t('平') };

  return (
    <div className='rps-board'>
      <div className='rps-score'>
        <div className='rps-score-side is-me'>
          <span>{t('我')}</span>
          <strong>{wins[me]}</strong>
        </div>
        <span className='rps-score-colon'>:</span>
        <div className='rps-score-side'>
          <strong>{wins[opponent]}</strong>
          <span>{t('对手')}</span>
        </div>
      </div>
      <div className='games-stage-counter'>
        {state && !state.over
          ? t(
              '第 {{round}} 回合 · 先赢 {{wins}} 回合获胜 · 最多 {{max}} 回合',
              {
                round: state.round,
                wins: state.wins_needed,
                max: state.max_rounds,
              },
            )
          : t('三局两胜，同时出拳')}
      </div>

      <div className='rps-arena' key={shown ? `r${shownIndex}` : 'live'}>
        <HandCard
          label={t('我')}
          names={names}
          choice={shown ? shown.choices[me] : state?.choice}
          result={shown ? outcome : ''}
          reveal={!!shown}
        />
        <Swords className='rps-vs' size={22} aria-hidden />
        <HandCard
          label={t('对手')}
          names={names}
          choice={shown ? shown.choices[opponent] : ''}
          ready={!shown && !!state?.ready?.[opponent]}
          result={
            shown ? { win: 'loss', loss: 'win', draw: 'draw' }[outcome] : ''
          }
          reveal={!!shown}
        />
      </div>
      {shown && (
        <div className={`rps-outcome is-${outcome}`}>
          {t('第 {{round}} 回合', { round: shownIndex + 1 })}
          {' · '}
          {outcomeText[outcome]}
        </div>
      )}

      <div className='rps-choices'>
        {CHOICES.map((choice) => {
          const Icon = CHOICE_ICONS[choice];
          const picked = state?.choice === choice;
          return (
            <button
              key={choice}
              type='button'
              className={picked ? 'rps-choice is-picked' : 'rps-choice'}
              disabled={!canMove || !!state?.choice}
              aria-pressed={picked}
              onClick={() => onMove({ choice, round: state.round })}
            >
              <Icon size={40} strokeWidth={1.6} />
              <span>{names[choice]}</span>
            </button>
          );
        })}
      </div>

      {count > 0 && (
        <ol className='rps-history'>
          {rounds.map((round, index) => {
            const result = outcomeOf(round);
            const Mine = CHOICE_ICONS[round.choices[me]];
            const Theirs = CHOICE_ICONS[round.choices[opponent]];
            return (
              <li key={index} className={`is-${result}`}>
                <span className='rps-history-round'>
                  {t('第 {{round}} 回合', { round: index + 1 })}
                </span>
                <span className='rps-history-hands'>
                  <Mine size={16} aria-label={names[round.choices[me]]} />
                  <span>:</span>
                  <Theirs
                    size={16}
                    aria-label={names[round.choices[opponent]]}
                  />
                </span>
                <span className='rps-history-result'>{badges[result]}</span>
              </li>
            );
          })}
        </ol>
      )}
    </div>
  );
};

const Rps = () => {
  const { t } = useTranslation();
  return (
    <PvpGame
      gameKey='rps'
      title={getGameMetaMap(t).rps.name}
      className='rps-page'
      seatLabels={(state) =>
        [0, 1].map((index) =>
          t('{{wins}} 胜', { wins: state?.wins?.[index] ?? 0 }),
        )
      }
      statusText={({ state, seat }) => {
        if (!state) return undefined;
        if (state.choice) return t('等待对手出拳');
        return state.ready?.[1 - seat]
          ? t('对手已出拳，轮到你了')
          : t('请出拳');
      }}
      rules={[
        t('三局两胜：每回合双方同时出拳，先赢 2 回合的一方获胜'),
        t('石头赢剪刀，剪刀赢布，布赢石头；出得一样是平局，这一回合重来'),
        t('最多打 9 回合（平局也算），打完仍没有人赢满 2 回合判和'),
        t('出拳之前看不到对手的选择；一方出拳后，另一方重新计时'),
        t('这一回合双方都没出拳而超时判和，只有一方没出则判它负'),
      ]}
      renderBoard={({ state, seat, canMove, onMove }) => (
        <RpsBoard state={state} seat={seat} canMove={canMove} onMove={onMove} />
      )}
    />
  );
};

export default Rps;
