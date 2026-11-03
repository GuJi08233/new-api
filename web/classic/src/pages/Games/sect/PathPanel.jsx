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
import { Button, InputNumber, Modal } from '@douyinfe/semi-ui';
import { Check } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { gameGet, gamePostOnce } from '../common/api';
import { getClassText } from '../dungeon/text';
import { useWendaoPart } from '../wendao/shell';
import { ActiveDuelNotice, ContractList } from '../wendao/duel/Contracts';
import DuelRun from '../wendao/duel/DuelRun';
import TravelNotice from '../wendao/world/TravelNotice';
import DailyPanel from './DailyPanel';
import {
  Bar,
  CostChips,
  canAfford,
  formatAmount,
  formatLeft,
  goodsDesc,
  goodsName,
  isLibraryGoods,
  useSectText,
} from './parts';

const NO_GIFT = { stone: 0, herb: 0, ore: 0 };

// 散修：四个正道宗门任选一个，过了入门试炼(一场斗法)就拜入(退出不满七天时不能)，或者境界与灵石够了自己开宗立派。规则
// (rules)来自服务端的 info.path，book 是斗法契约(今天各宗门的试炼用了几次、有没有没打完的一场)，这里只负责写出来。
const LoosePath = ({
  rules,
  sect,
  standing,
  now,
  busy,
  book,
  blocked,
  trialTries,
  actPath,
  onTrial,
  onGiveUp,
}) => {
  const { t } = useTranslation();
  const text = useSectText();
  const classes = getClassText(t);
  const wait = standing.path?.left
    ? standing.path.left + rules.rejoin_wait - now
    : 0;
  const realmReady = standing.realm >= rules.found_realm;
  const foundCost = { stone: rules.found_cost };
  const confirmFound = () =>
    Modal.confirm({
      title: t('开宗立派'),
      content: t(
        '花掉 {{n}} 灵石，建起主殿、灵田与聚灵阵，收下两名弟子。开宗以后由你经营宗门，不能再拜入别的宗门。',
        { n: rules.found_cost },
      ),
      onOk: () => actPath('found'),
    });
  return (
    <>
      <div className='sc-path-me'>
        <strong>{t('散修')}</strong>
        <p>
          {t(
            '无门无派，来去自由。拜入正道宗门可以领俸禄、攒贡献换丹药，还有门派加成；修到{{realm}}、攒够灵石，也可以自己开宗立派。',
            { realm: text.realms[rules.found_realm] },
          )}
        </p>
      </div>
      <h4 className='sc-heading'>{t('拜入宗门')}</h4>
      <p className='sc-hint'>
        {t(
          '拜入宗门要先过入门试炼：一间房、两波敌人，打赢才算入门；每个宗门每天最多试 {{n}} 次，输了或中途离开也用掉一次。',
          { n: trialTries },
        )}
      </p>
      <ActiveDuelNotice active={book?.active} busy={busy} onGiveUp={onGiveUp} />
      {wait > 0 && (
        <p className='sc-hint'>
          {t('退出宗门不满七天，{{left}}后才能再拜入', {
            left: formatLeft(wait, t),
          })}
        </p>
      )}
      <div className='sc-buildings'>
        {rules.sects.map((npc) => {
          const info = text.path.sects[npc.id];
          // 各宗宝库都有的不算特产，只列出这家独有的。
          const specialties = npc.goods.filter(
            (goods) =>
              !rules.sects.every((other) =>
                other.goods.some((item) => item.id === goods.id),
              ),
          );
          return (
            <div key={npc.id} className='sc-building'>
              <div className='sc-building-head'>
                <strong>{info?.name}</strong>
                <span className='sc-level'>
                  {npc.style ? classes[npc.style]?.name : text.path.alchemy}
                </span>
              </div>
              <p className='sc-building-desc'>{info?.desc}</p>
              <p className='sc-path-line'>
                {t('门派加成：{{perk}}', {
                  perk: text.path.perks[npc.perk]?.(rules.perks[npc.perk]),
                })}
              </p>
              <p className='sc-path-line'>
                {t('宝库特产：{{goods}}', {
                  goods: specialties
                    .map((goods) => goodsName(text, goods.id))
                    .join(' · '),
                })}
              </p>
              <div className='sc-building-foot'>
                <span className='sc-muted'>
                  {t('今天已试 {{n}}/{{max}}', {
                    n: book?.trials?.[npc.id] || 0,
                    max: trialTries,
                  })}
                </span>
                <span className='sc-spacer' />
                <Button
                  size='small'
                  theme='solid'
                  disabled={
                    busy ||
                    wait > 0 ||
                    !!book?.active ||
                    blocked ||
                    (book?.trials?.[npc.id] || 0) >= trialTries
                  }
                  onClick={() => onTrial(npc.id)}
                >
                  {t('入门试炼')}
                </Button>
              </div>
            </div>
          );
        })}
      </div>
      <h4 className='sc-heading'>{t('开宗立派')}</h4>
      <div className='sc-building'>
        <p className='sc-building-desc'>
          {t(
            '建起主殿、灵田与聚灵阵，收下两名弟子；之后招收弟子、派遣历练、参加宗门大比与论剑，还能结交仙盟、在万宝阁买卖。',
          )}
        </p>
        <div className='sc-building-foot'>
          <span className={realmReady ? 'sc-muted' : 'sc-path-short'}>
            {t('要{{need}}，现在{{realm}}', {
              need: text.realms[rules.found_realm],
              realm: text.realms[standing.realm],
            })}
          </span>
          <CostChips cost={foundCost} have={sect.res} />
          <span className='sc-spacer' />
          <Button
            theme='solid'
            disabled={busy || !realmReady || !canAfford(foundCost, sect.res)}
            onClick={confirmFound}
          >
            {t('开宗立派')}
          </Button>
        </div>
      </div>
    </>
  );
};

// 宝库或藏经阁里的一排货物：说明、要的身份与贡献，和「换取」；owned 里的(藏经阁里已经换到的)标上已有。
const GoodsList = ({ goods, rank, merit, busy, onExchange, owned }) => {
  const { t } = useTranslation();
  const text = useSectText();
  return (
    <div className='sc-listings'>
      {goods.map((item) => {
        const locked = rank < item.rank;
        const have = owned?.includes(item.id);
        return (
          <div key={item.id} className='sc-listing'>
            <strong>{goodsName(text, item.id)}</strong>
            <span className='sc-muted'>{goodsDesc(text, item.id)}</span>
            <span className='sc-spacer' />
            {locked && (
              <span className='sc-path-short'>
                {t('{{rank}}可换', { rank: text.path.ranks[item.rank] })}
              </span>
            )}
            <span className='sc-chip is-stone'>
              {t('{{n}} 贡献', { n: item.merit })}
            </span>
            <Button
              size='small'
              disabled={busy || have || locked || merit < item.merit}
              onClick={() => onExchange(item.id)}
            >
              {have ? t('已有') : t('换取')}
            </Button>
          </div>
        );
      })}
    </div>
  );
};

// 宗门弟子：身份与贡献、下一级身份的要求、每天的俸禄、宗门任务(斗法)、上交资源换贡献、宝库与藏经阁换东西，以及退出宗门。
const DisciplePath = ({
  rules,
  sect,
  standing,
  busy,
  book,
  blocked,
  taskTries,
  actPath,
  onTask,
  onGiveUp,
  reloadBook,
}) => {
  const { t } = useTranslation();
  const text = useSectText();
  const [gift, setGift] = useState(NO_GIFT);
  // 换到法宝或法术以后重新读一遍契约，藏经阁里已有的标出来。
  const exchange = async (id) => {
    if (await actPath('exchange', { goods: id })) reloadBook();
  };
  const path = standing.path;
  const npc = rules.sects.find((item) => item.id === path.sect);
  const rank = path.rank || 0;
  const merit = path.merit || 0;
  const earned = path.earned || 0;
  // 藏经阁里已经换到的货物标识(法宝 arm_、法术 skill_)，换到的不能再换。
  const owned = [
    ...(book?.arms?.weapons || []).map((w) => `arm_${w}`),
    ...(book?.arms?.skills || []).map((s) => `skill_${s}`),
  ];
  const next = rank + 1 < rules.rank_merit.length ? rank + 1 : 0;
  const rate = rules.donate_rate;
  const giftMerit =
    Math.floor(gift.stone / rate.stone) +
    Math.floor(gift.herb / rate.herb) +
    Math.floor(gift.ore / rate.ore);
  const confirmLeave = () =>
    Modal.confirm({
      title: t('退出宗门'),
      content: t('退出以后身份与贡献清零，七天内不能再拜入任何宗门。'),
      onOk: () => actPath('leave'),
    });
  return (
    <>
      <div className='sc-path-me'>
        <strong>
          {t('{{sect}} · {{rank}}', {
            sect: text.path.sects[path.sect]?.name,
            rank: text.path.ranks[rank],
          })}
        </strong>
        <p>
          {t('门派加成：{{perk}}', {
            perk: text.path.perks[npc?.perk]?.(rules.perks[npc?.perk]),
          })}
        </p>
        <Button
          size='small'
          type='danger'
          theme='borderless'
          disabled={busy}
          onClick={confirmLeave}
        >
          {t('退出宗门')}
        </Button>
      </div>
      <div className='sc-arena-me'>
        <span>
          {t('贡献')}
          <strong>{formatAmount(merit)}</strong>
        </span>
        <span>
          {t('累计贡献')}
          <strong>{formatAmount(earned)}</strong>
        </span>
      </div>
      <div className='sc-path-row'>
        <strong>{t('今日俸禄')}</strong>
        <CostChips cost={rules.rank_stipend[rank]} />
        <span className='sc-spacer' />
        {standing.stipend_ready ? (
          <Button
            size='small'
            theme='solid'
            disabled={busy}
            onClick={() => actPath('stipend')}
          >
            {t('领取')}
          </Button>
        ) : (
          <span className='sc-daily-claimed'>
            <Check size={14} />
            {t('今天已领')}
          </span>
        )}
      </div>
      {next > 0 ? (
        <div className='sc-path-row'>
          <strong>{t('晋升{{rank}}', { rank: text.path.ranks[next] })}</strong>
          <Bar value={earned} max={rules.rank_merit[next]} />
          <span className='sc-muted'>
            {t('累计贡献 {{have}}/{{need}}', {
              have: formatAmount(earned),
              need: formatAmount(rules.rank_merit[next]),
            })}
          </span>
          <span
            className={
              standing.realm >= rules.rank_realm[next]
                ? 'sc-muted'
                : 'sc-path-short'
            }
          >
            {t('境界要{{realm}}', {
              realm: text.realms[rules.rank_realm[next]],
            })}
          </span>
        </div>
      ) : (
        <p className='sc-muted'>{t('已是长老，宗门里最高的身份')}</p>
      )}

      <h4 className='sc-heading'>{t('宗门任务')}</h4>
      <p className='sc-muted'>
        {t(
          '每天换一批，每张最多挑战 {{n}} 次；打赢给贡献与一点灵石，敌人随境界、地域与难度变强。',
          { n: taskTries },
        )}
      </p>
      <ActiveDuelNotice active={book?.active} busy={busy} onGiveUp={onGiveUp} />
      <ContractList
        contracts={book?.tasks}
        open={book?.open}
        active={book?.active}
        blocked={blocked}
        busy={busy}
        maxTries={taskTries}
        onStart={onTask}
      />

      <h4 className='sc-heading'>{t('上交')}</h4>
      <div className='sc-building-foot sc-donate'>
        {['stone', 'herb', 'ore'].map((key) => (
          <InputNumber
            key={key}
            min={0}
            max={sect.res[key] || 0}
            precision={0}
            step={rate[key]}
            value={gift[key]}
            prefix={text.resources[key]}
            onChange={(value) =>
              setGift((previous) => ({
                ...previous,
                [key]: Number(value) || 0,
              }))
            }
            style={{ width: 150 }}
          />
        ))}
        <span className='sc-muted'>
          {t('折合 {{n}} 贡献', { n: giftMerit })}
        </span>
        <span className='sc-spacer' />
        <Button
          theme='solid'
          disabled={busy || giftMerit <= 0}
          onClick={async () => {
            if (await actPath('donate', gift)) setGift(NO_GIFT);
          }}
        >
          {t('上交')}
        </Button>
      </div>
      <p className='sc-muted'>
        {t(
          '{{stone}} 灵石、{{herb}} 灵草或 {{ore}} 玄铁折 1 点贡献，零头留在库房；每领一件每日功课另得 {{daily}} 贡献',
          {
            stone: rate.stone,
            herb: rate.herb,
            ore: rate.ore,
            daily: rules.daily_merit,
          },
        )}
      </p>

      <h4 className='sc-heading'>{t('宗门宝库')}</h4>
      <GoodsList
        goods={(npc?.goods || []).filter((g) => !isLibraryGoods(g.id))}
        rank={rank}
        merit={merit}
        busy={busy}
        onExchange={exchange}
      />
      <h4 className='sc-heading'>{t('藏经阁')}</h4>
      <p className='sc-muted'>
        {t(
          '换来的斗法法宝与法术永久属于你，退出宗门也在；到「世界」里配置出战，只在悬赏、宗门任务与入门试炼里带，九幽秘境里不带。',
        )}
      </p>
      <GoodsList
        goods={(npc?.goods || []).filter((g) => isLibraryGoods(g.id))}
        rank={rank}
        merit={merit}
        busy={busy}
        onExchange={exchange}
        owned={owned}
      />
    </>
  );
};

// 身份页：没开宗时宗门页的第一个页签。散修在这里拜入宗门或开宗立派，宗门弟子领俸禄、上交、换宝库；下面照常是每日功课。
// standing 是服务端给的身份({ identity, path, realm, stipend_ready })。
const PathPanel = ({
  rules,
  sect,
  standing,
  now,
  busy,
  act,
  actPath,
  onDuelDone,
}) => {
  const { t } = useTranslation();
  const { info: world } = useWendaoPart('world');
  const [book, setBook] = useState(null);
  const [travel, setTravel] = useState(null);
  const bookSeq = useRef(0);
  const [duel, setDuel] = useState(null);
  const [duelBusy, setDuelBusy] = useState(false);
  const identity = standing?.identity;

  const loadBook = useCallback(async () => {
    const mine = ++bookSeq.current;
    const res = await gameGet('/api/game/wendao/duel/state', t);
    if (mine !== bookSeq.current) return;
    if (res.data) {
      setBook(res.data.book);
      setTravel(res.data.travel?.active || null);
    }
  }, [t]);
  useEffect(() => {
    if (identity === 'loose' || identity === 'disciple') loadBook();
  }, [identity, loadBook]);

  const giveUp = async () => {
    bookSeq.current += 1;
    setDuelBusy(true);
    const res = await gamePostOnce('/api/game/wendao/duel/giveup', {}, t);
    setDuelBusy(false);
    bookSeq.current += 1;
    if (res.data) {
      setBook(res.data.book);
      setTravel(res.data.travel?.active || null);
    }
  };
  // 打完回来：契约、身份与宗门都按回应里的新样子换上；放弃或出错时重新读一遍契约。
  const finished = (answer) => {
    setDuel(null);
    if (answer?.book) {
      bookSeq.current += 1;
      setBook(answer.book);
      setTravel(answer.travel?.active || null);
      onDuelDone(answer);
    } else {
      loadBook();
    }
  };

  if (!rules || !standing) return null;
  if (duel) {
    return (
      <div className='sc-path'>
        <DuelRun kind={duel.kind} id={duel.id} onDone={finished} />
      </div>
    );
  }
  const taskBusy = busy || duelBusy;
  return (
    <div className='sc-path'>
      <TravelNotice active={travel} />
      {standing.identity === 'disciple' && (
        <DisciplePath
          rules={rules}
          sect={sect}
          standing={standing}
          busy={taskBusy}
          book={book}
          blocked={!book || !!travel}
          taskTries={world?.contract_tries ?? 2}
          actPath={actPath}
          onTask={(c) => setDuel({ kind: c.kind, id: c.id })}
          onGiveUp={giveUp}
          reloadBook={loadBook}
        />
      )}
      {standing.identity === 'loose' && (
        <LoosePath
          rules={rules}
          sect={sect}
          standing={standing}
          now={now}
          busy={taskBusy}
          book={book}
          blocked={!book || !!travel}
          trialTries={world?.trial_tries ?? 5}
          actPath={actPath}
          onTrial={(id) => setDuel({ kind: 'trial', id })}
          onGiveUp={giveUp}
        />
      )}
      {!standing.identity && (
        <p className='sc-hint'>
          {t('先在「历练」里选定道途，再决定做散修、拜入宗门还是开宗立派')}
        </p>
      )}
      <DailyPanel sect={sect} busy={busy} act={act} />
    </div>
  );
};

export default PathPanel;
