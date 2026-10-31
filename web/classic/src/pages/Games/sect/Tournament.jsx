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
import { Button, Checkbox, Modal } from '@douyinfe/semi-ui';
import { Flag, Shield, Swords, Trophy } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { RuleList, StageMessage } from '../common/SideBox';
import { PlayTurnstile } from '../common/usePlayTurnstile';
import { skillResultMessage } from '../common/skill';
import { MOVES, getIntentText } from './text';
import {
  Bar,
  CostChips,
  ElementTag,
  moveName,
  realmLabel,
  useSectText,
} from './parts';

const EdgeTag = ({ edge }) => {
  const { t } = useTranslation();
  if (edge === 1)
    return <span className='sc-edge is-good'>{t('五行相克')}</span>;
  if (edge === -1) return <span className='sc-edge is-bad'>{t('被克制')}</span>;
  return null;
};

const QiPips = ({ qi }) => (
  <span className='sc-qi' aria-label={`qi ${qi}`}>
    {Array.from({ length: 5 }, (_, i) => (
      <span key={i} className={i < qi ? 'is-on' : ''} />
    ))}
  </span>
);

// 斗法双方的一侧。hit 是这一回合受到的伤害，heal 是回复的生命：每回合换一个 key，飘字与受击抖动都重新播放。
const FighterPanel = ({ fighter, side, sectName, hit, heal, turnKey }) => {
  const { t } = useTranslation();
  const text = useSectText();
  return (
    <div
      key={`${side}-${turnKey}`}
      className={`sc-fighter is-${side}${hit > 0 ? ' is-hit' : ''}`}
    >
      {sectName && <span className='sc-muted'>{sectName}</span>}
      <div className='sc-disciple-head'>
        <strong>{text.name(fighter.name)}</strong>
        <ElementTag element={fighter.element} />
      </div>
      <span className='sc-muted'>
        {realmLabel(text, t, fighter.realm, fighter.layer)} ·{' '}
        {t('战力 {{power}}', { power: fighter.power })}
      </span>
      <div className='sc-fighter-hp'>
        <Bar value={fighter.hp} max={fighter.max_hp} kind='hp' />
        <span>
          {fighter.hp} / {fighter.max_hp}
        </span>
      </div>
      <QiPips qi={fighter.qi} />
      {hit > 0 && <span className='sc-pop is-hit'>-{hit}</span>}
      {heal > 0 && <span className='sc-pop is-heal'>+{heal}</span>}
    </div>
  );
};

// 上一回合的经过写成一两句话。
function turnSummary(last, text, t) {
  if (!last) return '';
  const parts = [
    t('你使出{{mine}}，对手使出{{theirs}}', {
      mine: moveName(text, last.mine),
      theirs: moveName(text, last.theirs),
    }),
  ];
  if (last.dealt + last.counter > 0)
    parts.push(t('造成 {{n}} 伤害', { n: last.dealt + last.counter }));
  if (last.taken + last.struck > 0)
    parts.push(t('受到 {{n}} 伤害', { n: last.taken + last.struck }));
  if (last.healed > 0) parts.push(t('回复 {{n}} 生命', { n: last.healed }));
  if (last.counter > 0) parts.push(t('护体反震 {{n}}', { n: last.counter }));
  return parts.join(t('，'));
}

const Duel = ({ tour, busy, onMove }) => {
  const { t } = useTranslation();
  const text = useSectText();
  const intents = getIntentText(t);
  const me = tour.fighters[tour.current];
  const opponent = tour.opponent;
  const last = tour.last;
  const turnKey = tour.turn;
  const disabled = (move) => {
    if (busy) return true;
    if (move === 'spell') return me.qi < 2;
    if (move === 'pill') return tour.pills <= 0 || me.hp >= me.max_hp;
    return false;
  };
  return (
    <div className='sc-duel'>
      <div className='sc-arena'>
        <FighterPanel
          fighter={me}
          side='me'
          hit={last ? last.taken + last.struck : 0}
          heal={last?.healed || 0}
          turnKey={turnKey}
        />
        <div className='sc-versus'>
          <Swords size={22} />
          <EdgeTag edge={tour.edges[tour.current]} />
          <span className='sc-muted'>
            {t('第 {{turn}} / {{turns}} 回合', {
              turn: tour.turn + 1,
              turns: tour.turns,
            })}
          </span>
        </div>
        <FighterPanel
          fighter={opponent}
          side='them'
          sectName={text.sects[opponent.sect]}
          hit={last ? last.dealt + last.counter : 0}
          turnKey={turnKey}
        />
      </div>
      <div className={`sc-intent is-${tour.intent}`}>
        <Shield size={16} />
        <span>
          {t('对手下一招')}：<strong>{moveName(text, tour.intent)}</strong> ·{' '}
          {intents[tour.intent]}
        </span>
      </div>
      {last && <p className='sc-turn-log'>{turnSummary(last, text, t)}</p>}
      <div className='sc-moves'>
        {MOVES.map((move) => (
          <button
            type='button'
            key={move}
            className={`sc-move is-${move}`}
            disabled={disabled(move)}
            onClick={() => onMove(move)}
          >
            <strong>
              {moveName(text, move)}
              {move === 'pill' && ` × ${tour.pills}`}
            </strong>
            <span>{text.moves[move]?.desc}</span>
          </button>
        ))}
      </div>
    </div>
  );
};

const Pick = ({ tour, busy, onMove }) => {
  const { t } = useTranslation();
  const text = useSectText();
  const opponent = tour.opponent;
  return (
    <div className='sc-pick'>
      {tour.wins > 0 && (
        <StageMessage kind='win'>
          {t('已连胜 {{n}} 场，休整一下，派人迎战下一位对手', { n: tour.wins })}
        </StageMessage>
      )}
      <div className='sc-fighter is-them is-preview'>
        <span className='sc-muted'>
          {t('第 {{round}} 轮对手', { round: tour.round + 1 })} ·{' '}
          {text.sects[opponent.sect]}
        </span>
        <div className='sc-disciple-head'>
          <strong>{text.name(opponent.name)}</strong>
          <ElementTag element={opponent.element} />
        </div>
        <span className='sc-muted'>
          {realmLabel(text, t, opponent.realm, opponent.layer)} ·{' '}
          {t('战力 {{power}}', { power: opponent.power })}
        </span>
      </div>
      <h4 className='sc-heading'>{t('派谁上场')}</h4>
      <div className='sc-mission-list'>
        {tour.fighters.map((f, index) => (
          <button
            type='button'
            key={f.id}
            className='sc-pick-fighter'
            disabled={busy || f.hp <= 0}
            onClick={() => onMove('pick', index)}
          >
            <span className='sc-disciple-head'>
              <strong>{text.name(f.name)}</strong>
              <ElementTag element={f.element} />
              <EdgeTag edge={tour.edges[index]} />
              <span className='sc-power'>
                {t('战力 {{power}}', { power: f.power })}
              </span>
            </span>
            <span className='sc-fighter-hp'>
              <Bar value={f.hp} max={f.max_hp} kind='hp' />
              <span>
                {f.hp > 0 ? `${f.hp} / ${f.max_hp}` : t('已无力再战')}
              </span>
            </span>
          </button>
        ))}
      </div>
    </div>
  );
};

const EndScreen = ({ round, symbol, onBack }) => {
  const { t } = useTranslation();
  const tour = round.tournament;
  const detail = round.result?.detail || {};
  const message = round.result && skillResultMessage(round.result, t, symbol);
  let title = t('连胜 {{n}} 场', { n: tour.wins });
  if (tour.wins === tour.rounds) title = t('五战全胜，大比夺魁！');
  else if (detail.abandoned)
    title = t('已认输，连胜 {{n}} 场', { n: tour.wins });
  else if (tour.wins === 0) title = t('首轮落败');
  return (
    <div className={`sc-end ${tour.wins >= 3 ? 'is-won' : 'is-lost'}`}>
      {tour.wins >= 3 ? <Trophy size={44} /> : <Flag size={44} />}
      <h3>{title}</h3>
      {message && (
        <StageMessage kind={message.kind} note={message.note}>
          {message.text}
        </StageMessage>
      )}
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
            {t('今天前三次大比才有声望与灵石，这次只记战绩')}
          </span>
        )}
        {detail.pills > 0 && (
          <span className='sc-muted'>
            {t('{{n}} 颗回春丹已放回丹房', { n: detail.pills })}
          </span>
        )}
      </div>
      <Button theme='solid' onClick={onBack}>
        {t('回到宗门')}
      </Button>
    </div>
  );
};

// 报名：选出战的弟子(默认战力最高的几名)，带上回春丹。
const Signup = ({ info, sect, now, busy, turnstile, onStart }) => {
  const { t } = useTranslation();
  const text = useSectText();
  const rules = info?.tournament || {};
  const size = rules.team_size || 3;
  const home = useMemo(
    () =>
      sect.disciples
        .filter((d) => !d.mission && d.injured <= now)
        .sort((a, b) => b.power - a.power),
    // 只在弟子名单变了时重新排序，倒计时每秒刷新不影响勾选。
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [sect.disciples],
  );
  const [team, setTeam] = useState(() => home.slice(0, size).map((d) => d.id));
  const chosen = team.filter((id) => home.some((d) => d.id === id));
  const toggle = (id) =>
    setTeam((previous) =>
      previous.includes(id)
        ? previous.filter((x) => x !== id)
        : previous.length < size
          ? [...previous, id]
          : previous,
    );
  const pills = Math.min(sect.pills?.heal || 0, rules.pouch_pills || 3);
  return (
    <div className='sc-signup'>
      <RuleList
        items={[
          t(
            '带最多 {{size}} 名弟子打 {{rounds}} 轮擂台，每轮派其中一名上场，输一场就结束；成绩是连胜场数',
            {
              size,
              rounds: rules.rounds || 5,
            },
          ),
          t(
            '对手的下一招会提前亮出来：蓄力之后必是重击，护体能挡；对手护体时用法术或调息',
          ),
          t(
            '五行相克伤害 ×1.3，被克 ×0.8；弟子的伤带到下一轮，每赢一轮全员回复 5% 生命',
          ),
          t('每天前 {{n}} 次大比按连胜场数给宗门声望与灵石', {
            n: rules.daily_reward_runs || 3,
          }),
        ]}
      />
      <h4 className='sc-heading'>
        {t('出战弟子 {{n}} / {{size}}', { n: chosen.length, size })}
      </h4>
      {home.length === 0 && (
        <p className='sc-hint'>
          {t('宗门里没有能出战的弟子：都在外历练或受了伤')}
        </p>
      )}
      <div className='sc-picker'>
        {home.map((d) => (
          <Checkbox
            key={d.id}
            checked={team.includes(d.id)}
            onChange={() => toggle(d.id)}
          >
            <span className='sc-pick-name'>
              {text.name(d.name)}
              <ElementTag element={d.element} />
              <span className='sc-muted'>
                {realmLabel(text, t, d.realm, d.layer)} ·{' '}
                {t('战力 {{power}}', { power: d.power })}
              </span>
            </span>
          </Checkbox>
        ))}
      </div>
      <div className='sc-building-foot'>
        <span className='sc-muted'>
          {t('随身带 {{n}} 颗回春丹', { n: pills })}
        </span>
        <span className='sc-spacer' />
        <Button
          theme='solid'
          size='large'
          loading={busy}
          disabled={chosen.length === 0 || !turnstile.ready}
          onClick={() => onStart(chosen)}
        >
          {t('报名大比')}
        </Button>
      </div>
      <PlayTurnstile turnstile={turnstile} />
    </div>
  );
};

const Tournament = ({
  info,
  sect,
  now,
  round,
  busy,
  turnstile,
  onStart,
  onMove,
  onBack,
}) => {
  const { t } = useTranslation();
  if (round?.status === 'finished') {
    return <EndScreen round={round} symbol={info?.symbol} onBack={onBack} />;
  }
  if (!round) {
    return (
      <Signup
        info={info}
        sect={sect}
        now={now}
        busy={busy}
        turnstile={turnstile}
        onStart={onStart}
      />
    );
  }
  const tour = round.tournament;
  const confirmAbandon = () =>
    Modal.confirm({
      title: t('认输'),
      content: t('认输后按已经连胜的场数结算。'),
      onOk: () => onMove('abandon'),
    });
  return (
    <div className='sc-tournament'>
      <div className='sc-tour-head'>
        <strong>
          {t('宗门大比 · 第 {{round}} / {{rounds}} 轮', {
            round: Math.min(tour.round + 1, tour.rounds),
            rounds: tour.rounds,
          })}
        </strong>
        <span className='sc-rounds'>
          {Array.from({ length: tour.rounds }, (_, i) => (
            <span
              key={i}
              className={
                i < tour.wins ? 'is-won' : i === tour.round ? 'is-now' : ''
              }
            />
          ))}
        </span>
        <span className='sc-spacer' />
        <Button
          size='small'
          type='danger'
          disabled={busy}
          onClick={confirmAbandon}
        >
          {t('认输')}
        </Button>
      </div>
      {tour.phase === 'pick' ? (
        <Pick tour={tour} busy={busy} onMove={onMove} />
      ) : (
        <Duel tour={tour} busy={busy} onMove={onMove} />
      )}
    </div>
  );
};

export default Tournament;
