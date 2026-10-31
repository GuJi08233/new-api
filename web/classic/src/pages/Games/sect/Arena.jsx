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

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Button } from '@douyinfe/semi-ui';
import { Flag, Shield, Swords, Trophy } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { RuleList } from '../common/SideBox';
import { gameGet } from '../common/api';
import { CostChips, ElementTag, realmLabel, useSectText } from './parts';
import { DuelBoard, useHomeDisciples, useTeamPicker } from './Tournament';

// 守擂弟子的一行：名字、五行、境界与战力。
const DefenderChip = ({ fighter }) => {
  const { t } = useTranslation();
  const text = useSectText();
  return (
    <span className='sc-defender'>
      <strong>{text.name(fighter.name)}</strong>
      <ElementTag element={fighter.element} />
      <span className='sc-muted'>
        {realmLabel(text, t, fighter.realm, fighter.layer)} ·{' '}
        {t('战力 {{power}}', { power: fighter.power })}
      </span>
    </span>
  );
};

const ArenaEnd = ({ round, names, dailyRuns, onBack }) => {
  const { t } = useTranslation();
  const tour = round.tournament;
  const detail = round.result?.detail || {};
  const won = tour.wins === tour.rounds;
  let title = t('论剑落败，击败 {{n}} 名守擂弟子', { n: tour.wins });
  if (won) title = t('论剑获胜！');
  else if (detail.abandoned)
    title = t('已认输，击败 {{n}} 名守擂弟子', { n: tour.wins });
  const delta = round.battle?.attacker_delta;
  return (
    <div className={`sc-end ${won ? 'is-won' : 'is-lost'}`}>
      {won ? <Trophy size={44} /> : <Flag size={44} />}
      <h3>{title}</h3>
      <span className='sc-muted'>
        {t('对手：{{name}}', { name: names[round.defender] || '-' })}
        {delta !== undefined &&
          ` · ${t('积分 {{delta}}', { delta: delta > 0 ? `+${delta}` : delta })}`}
      </span>
      <div className='sc-end-gains'>
        {detail.stone > 0 || detail.rep > 0 ? (
          <>
            <span>{t('宗门所得')}</span>
            <CostChips
              cost={{ stone: detail.stone, rep: detail.rep }}
              sign='+'
            />
          </>
        ) : (
          <span className='sc-muted'>
            {t('今天前 {{n}} 次论剑才有声望与灵石，这次只记积分', {
              n: dailyRuns,
            })}
          </span>
        )}
        {detail.pills > 0 && (
          <span className='sc-muted'>
            {t('{{n}} 颗回春丹已放回丹房', { n: detail.pills })}
          </span>
        )}
      </div>
      <Button theme='solid' onClick={onBack}>
        {t('回到论剑')}
      </Button>
    </div>
  );
};

// 论剑大厅：自己的积分与守擂阵容、出战弟子、积分相近的对手、论剑榜与最近战报。
const ArenaLobby = ({ overview, sect, now, busy, onDefend, onChallenge }) => {
  const { t } = useTranslation();
  const home = useHomeDisciples(sect, now);
  const all = useMemo(
    () => [...sect.disciples].sort((a, b) => b.power - a.power),
    [sect.disciples],
  );
  const lineup = overview.me?.lineup;
  const [defense, defensePicker] = useTeamPicker(all, 3, lineup);
  const [team, teamPicker] = useTeamPicker(home, 3);
  const row = overview.me?.row;
  const names = overview.names || {};
  const userName = (id) => names[id] || `#${id}`;
  const pills = Math.min(sect.pills?.heal || 0, 3);
  return (
    <div className='sc-arena-lobby'>
      <RuleList
        items={[
          t(
            '设好守擂阵容就会登上论剑榜，别的掌门来挑战时由电脑按你宗门现在的实力代打',
          ),
          t(
            '挑战规则与大比相同：依次打败对方全部守擂弟子才算赢，输一场就结束；赢输按积分高低增减积分',
          ),
          t('每天前 {{n}} 次论剑给宗门声望与灵石；论剑不发游戏币', {
            n: overview.daily_runs,
          }),
        ]}
      />
      <div className='sc-arena-me'>
        <span>
          {t('积分')}
          <strong>{row?.rating ?? '-'}</strong>
        </span>
        <span>
          {t('挑战')}
          <strong>
            {row
              ? t('{{wins}} 胜 {{losses}} 负', {
                  wins: row.wins,
                  losses: row.losses,
                })
              : '-'}
          </strong>
        </span>
        <span>
          {t('守擂')}
          <strong>
            {row
              ? t('{{wins}} 胜 {{losses}} 负', {
                  wins: row.defend_wins,
                  losses: row.defend_losses,
                })
              : '-'}
          </strong>
        </span>
        <span>
          {t('守擂战力')}
          <strong>{row?.power || '-'}</strong>
        </span>
      </div>

      <h4 className='sc-heading'>
        <Shield size={15} />
        {t('守擂阵容 {{n}} / 3', { n: defense.length })}
        <span className='sc-muted'>
          {t('按选择的顺序出场；在外历练或受伤也照常守擂')}
        </span>
      </h4>
      {defensePicker}
      <div className='sc-building-foot'>
        <span className='sc-spacer' />
        <Button
          theme='solid'
          disabled={busy || defense.length === 0}
          onClick={() => onDefend(defense)}
        >
          {lineup?.length ? t('更新守擂阵容') : t('设为守擂阵容')}
        </Button>
      </div>

      <h4 className='sc-heading'>
        <Swords size={15} />
        {t('出战弟子 {{n}} / {{size}}', { n: team.length, size: 3 })}
        <span className='sc-muted'>
          {t('随身带 {{n}} 颗回春丹', { n: pills })}
        </span>
      </h4>
      {home.length === 0 ? (
        <p className='sc-hint'>
          {t('宗门里没有能出战的弟子：都在外历练或受了伤')}
        </p>
      ) : (
        teamPicker
      )}

      <h4 className='sc-heading'>{t('对手')}</h4>
      {overview.opponents.length === 0 && (
        <p className='sc-muted'>{t('还没有别的宗门设好守擂阵容')}</p>
      )}
      <div className='sc-mission-list'>
        {overview.opponents.map(({ row: opponent, defenders }) => (
          <div key={opponent.user_id} className='sc-mission sc-opponent'>
            <div className='sc-mission-head'>
              <strong>{userName(opponent.user_id)}</strong>
              <span className='sc-muted'>
                {t('积分 {{rating}}', { rating: opponent.rating })} ·{' '}
                {t('守擂战力 {{power}}', {
                  power: defenders.reduce((sum, f) => sum + f.power, 0),
                })}
              </span>
              <span className='sc-spacer' />
              <Button
                size='small'
                theme='solid'
                disabled={busy || team.length === 0}
                onClick={() => onChallenge(opponent.user_id, team)}
              >
                {t('挑战')}
              </Button>
            </div>
            <div className='sc-defenders'>
              {defenders.map((f, index) => (
                <DefenderChip key={index} fighter={f} />
              ))}
            </div>
          </div>
        ))}
      </div>

      <h4 className='sc-heading'>{t('论剑榜')}</h4>
      <ol className='sc-ladder'>
        {overview.top.map((entry, index) => (
          <li
            key={entry.user_id}
            className={entry.user_id === overview.self ? 'is-me' : ''}
          >
            <span className='sc-ladder-rank'>{index + 1}</span>
            <strong>{userName(entry.user_id)}</strong>
            <span className='sc-spacer' />
            <span className='sc-muted'>
              {t('挑战 {{wins}} 胜', { wins: entry.wins })} ·{' '}
              {t('守擂 {{wins}} 胜', { wins: entry.defend_wins })}
            </span>
            <strong>{entry.rating}</strong>
          </li>
        ))}
      </ol>

      {overview.battles.length > 0 && (
        <>
          <h4 className='sc-heading'>{t('最近战报')}</h4>
          <ul className='sc-log'>
            {overview.battles.map((battle) => {
              const attacking = battle.attacker === overview.self;
              const won = attacking ? battle.win : !battle.win;
              const delta = attacking
                ? battle.attacker_delta
                : battle.defender_delta;
              return (
                <li key={battle.id}>
                  {attacking
                    ? t('挑战 {{name}}：{{result}}', {
                        name: userName(battle.defender),
                        result: won ? t('胜') : t('负'),
                      })
                    : t('{{name}} 来挑战：{{result}}', {
                        name: userName(battle.attacker),
                        result: won ? t('守住了') : t('失守'),
                      })}
                  <span className='sc-muted'>
                    {' '}
                    {t('积分 {{delta}}', {
                      delta: delta > 0 ? `+${delta}` : delta,
                    })}
                  </span>
                </li>
              );
            })}
          </ul>
        </>
      )}
    </div>
  );
};

// 论剑页。round 是进行中或刚结束的一场挑战，由宗门页保管(打开页面时读出进行中的一场)。
const Arena = ({ sect, now, busy, post, round, setRound, onSettled }) => {
  const { t } = useTranslation();
  const [overview, setOverview] = useState(null);

  const load = useCallback(async () => {
    const response = await gameGet('/api/game/sect/arena', t);
    if (response.data) setOverview(response.data);
  }, [t]);
  useEffect(() => {
    load();
  }, [load]);

  const defend = async (team) => {
    const data = await post('/api/game/sect/arena/defense', { team });
    if (data) load();
  };

  const challenge = async (opponent, team) => {
    const data = await post('/api/game/sect/arena/start', { opponent, team });
    if (!data) return;
    setRound(data);
    onSettled();
  };

  const move = async (action, index = 0) => {
    if (!round || round.status !== 'playing') return;
    const data = await post('/api/game/sect/arena/action', {
      session_id: round.session_id,
      version: round.version,
      action,
      index,
    });
    if (!data) {
      const response = await gameGet(
        `/api/game/sect/arena/battle?session_id=${round.session_id}`,
        t,
      );
      if (response.data?.session) setRound(response.data.session);
      return;
    }
    setRound(data);
    if (data.result) {
      onSettled(data);
      load();
    }
  };

  if (!overview) return null;
  const names = overview.names || {};
  if (round?.status === 'finished') {
    return (
      <ArenaEnd
        round={round}
        names={names}
        dailyRuns={overview.daily_runs}
        onBack={() => setRound(null)}
      />
    );
  }
  if (round) {
    const tour = round.tournament;
    return (
      <DuelBoard
        tour={tour}
        busy={busy}
        onMove={move}
        title={t('论剑 · 第 {{round}} / {{rounds}} 名守擂弟子', {
          round: Math.min(tour.round + 1, tour.rounds),
          rounds: tour.rounds,
        })}
        abandonContent={t('认输后这场论剑按落败计算积分。')}
        opponentLabel={names[round.defender] || ''}
      />
    );
  }
  return (
    <ArenaLobby
      key={overview.me?.lineup?.join(',') || 'none'}
      overview={overview}
      sect={sect}
      now={now}
      busy={busy}
      onDefend={defend}
      onChallenge={challenge}
    />
  );
};

export default Arena;
