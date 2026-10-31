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

import React, { useCallback, useEffect, useState } from 'react';
import { Button, Input, InputNumber, Modal } from '@douyinfe/semi-ui';
import { Crown, Flame, Gift, Users } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { RuleList } from '../common/SideBox';
import { gameGet } from '../common/api';
import { playSound } from '../common/sound';
import { celebrate } from '../common/effects';
import { Bar, CostChips, formatAmount, useSectText } from './parts';
import { useHomeDisciples, useTeamPicker } from './Tournament';

// 没有入盟时：建一个仙盟，或者加入现有的仙盟。
const AllianceList = ({ view, sect, busy, act }) => {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const rules = view.rules;
  const hall = sect.buildings.find((b) => b.key === 'hall')?.level || 0;
  return (
    <div className='sc-alliance'>
      <RuleList
        items={[
          t(
            '仙盟每级给全体成员的灵石、灵草与玄铁产出加成 {{pct}}%，人数上限随等级提高',
            { pct: Math.round(rules.bonus_per_level * 100) },
          ),
          t(
            '成员捐献资源充作盟资、记为贡献；盟主用盟资升级仙盟，最高 {{n}} 级',
            {
              n: rules.max_level,
            },
          ),
          t(
            '每周一只妖王，成员每天可派弟子讨伐一次，按伤害领奖；击败妖王后本周出过力的成员都有一份重赏',
          ),
          t('仙盟只发宗门资源与丹药，不发游戏币'),
        ]}
      />
      <h4 className='sc-heading'>{t('建立仙盟')}</h4>
      <div className='sc-building-foot'>
        <Input
          value={name}
          maxLength={rules.name_max}
          placeholder={t('盟名，最多 {{n}} 个字', { n: rules.name_max })}
          onChange={setName}
          style={{ maxWidth: 240 }}
        />
        <CostChips cost={rules.create_cost} have={sect.res} />
        <span className='sc-spacer' />
        <Button
          theme='solid'
          disabled={busy || !name.trim() || hall < rules.min_hall}
          onClick={() => act('create', { name })}
        >
          {t('建立仙盟')}
        </Button>
      </div>
      {hall < rules.min_hall && (
        <p className='sc-hint'>
          {t('主殿升到 {{n}} 级才能建立仙盟', { n: rules.min_hall })}
        </p>
      )}
      <h4 className='sc-heading'>{t('现有仙盟')}</h4>
      {view.list.length === 0 && (
        <p className='sc-muted'>{t('还没有仙盟，来建立第一个吧')}</p>
      )}
      <div className='sc-mission-list'>
        {view.list.map((alliance, index) => {
          const full = alliance.members >= view.caps[index];
          return (
            <div key={alliance.id} className='sc-mission'>
              <div className='sc-mission-head'>
                <strong>{alliance.name}</strong>
                <span className='sc-muted'>
                  {t(
                    '{{level}} 级 · {{members}} / {{cap}} 人 · 盟主 {{leader}}',
                    {
                      level: alliance.level,
                      members: alliance.members,
                      cap: view.caps[index],
                      leader: view.names[alliance.leader_id] || '-',
                    },
                  )}
                </span>
                <span className='sc-spacer' />
                <Button
                  size='small'
                  theme='solid'
                  disabled={busy || full}
                  onClick={() => act('join', { id: alliance.id })}
                >
                  {full ? t('已满员') : t('加入')}
                </Button>
              </div>
              {alliance.notice && (
                <p className='sc-muted sc-alliance-notice'>{alliance.notice}</p>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
};

// 已经入盟：仙盟概况、升级与公告、捐献、本周妖王、成员。
const AllianceHome = ({ view, sect, now, busy, act }) => {
  const { t } = useTranslation();
  const text = useSectText();
  const { alliance, member, boss } = view;
  const leader = alliance.leader_id === view.self;
  const [notice, setNotice] = useState(alliance.notice || '');
  const [gift, setGift] = useState({ stone: 0, herb: 0, ore: 0 });
  const home = useHomeDisciples(sect, now);
  const [team, picker] = useTeamPicker(home, 3);
  const attacked = member.attack_day === view.day && member.week === view.week;
  const giftFunds = (gift.stone || 0) + (gift.herb || 0) + 3 * (gift.ore || 0);
  const pending =
    member.pending_stone > 0 ||
    member.pending_rep > 0 ||
    member.pending_pills > 0;
  const confirmLeave = (target) =>
    Modal.confirm({
      title: target ? t('请出仙盟') : t('退出仙盟'),
      content: target
        ? t('确定把 {{name}} 请出仙盟？没领的奖励会作废。', {
            name: view.names[target] || target,
          })
        : t('退出后贡献清零，没领的奖励作废；盟主退出时由贡献最高的成员接任。'),
      onOk: () => act('leave', target ? { user_id: target } : {}),
    });
  return (
    <div className='sc-alliance'>
      <div className='sc-arena-me'>
        <span>
          {t('仙盟')}
          <strong>{alliance.name}</strong>
        </span>
        <span>
          {t('等级')}
          <strong>
            {alliance.level} / {view.rules.max_level}
          </strong>
        </span>
        <span>
          {t('成员')}
          <strong>
            {alliance.members} / {view.cap}
          </strong>
        </span>
        <span>
          {t('盟资')}
          <strong>{formatAmount(alliance.funds)}</strong>
        </span>
        <span>
          {t('产出加成')}
          <strong>
            +{Math.round(alliance.level * view.rules.bonus_per_level * 100)}%
          </strong>
        </span>
      </div>
      {leader ? (
        <div className='sc-building-foot'>
          <Input
            value={notice}
            maxLength={view.rules.notice_max}
            placeholder={t('仙盟公告')}
            onChange={setNotice}
          />
          <Button
            disabled={busy || notice === (alliance.notice || '')}
            onClick={() => act('notice', { notice })}
          >
            {t('保存公告')}
          </Button>
          <Button
            theme='solid'
            disabled={
              busy ||
              view.upgrade_cost <= 0 ||
              alliance.funds < view.upgrade_cost
            }
            onClick={() => act('upgrade')}
          >
            {view.upgrade_cost > 0
              ? t('升级（{{n}} 盟资）', { n: formatAmount(view.upgrade_cost) })
              : t('已满级')}
          </Button>
        </div>
      ) : (
        alliance.notice && (
          <p className='sc-alliance-notice'>{alliance.notice}</p>
        )
      )}

      <h4 className='sc-heading'>
        <Flame size={15} />
        {t('本周妖王 · {{level}} 级', { level: boss.level })}
        <span className='sc-muted'>
          {t('已击败 {{n}} 次', { n: boss.kills })}
        </span>
      </h4>
      <div className='sc-fighter-hp sc-boss-hp'>
        <Bar value={boss.hp} max={boss.max_hp} kind='hp' />
        <span>
          {boss.hp > 0
            ? `${formatAmount(boss.hp)} / ${formatAmount(boss.max_hp)}`
            : t('本周已击败')}
        </span>
      </div>
      {picker}
      <div className='sc-building-foot'>
        <span className='sc-muted'>
          {t('本周我的伤害 {{n}}', {
            n: formatAmount(member.week === view.week ? member.week_damage : 0),
          })}
        </span>
        <span className='sc-spacer' />
        <Button
          theme='solid'
          type='danger'
          disabled={busy || attacked || boss.hp <= 0 || team.length === 0}
          onClick={() => act('attack', { team })}
        >
          {attacked ? t('今天已讨伐') : t('讨伐妖王')}
        </Button>
      </div>
      {pending && (
        <div className='sc-building-foot sc-alliance-pending'>
          <Gift size={16} />
          <span>{t('待领奖励')}</span>
          <CostChips
            cost={{ stone: member.pending_stone, rep: member.pending_rep }}
            sign='+'
          />
          {member.pending_pills > 0 && (
            <span className='sc-muted'>
              {text.pills.break?.name} × {member.pending_pills}
            </span>
          )}
          <span className='sc-spacer' />
          <Button theme='solid' disabled={busy} onClick={() => act('claim')}>
            {t('领取')}
          </Button>
        </div>
      )}

      <h4 className='sc-heading'>{t('捐献')}</h4>
      <div className='sc-building-foot sc-donate'>
        {['stone', 'herb', 'ore'].map((key) => (
          <InputNumber
            key={key}
            min={0}
            max={sect.res[key] || 0}
            precision={0}
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
          {t('折合 {{n}} 盟资（玄铁一份折 3 点）', { n: giftFunds })}
        </span>
        <span className='sc-spacer' />
        <Button
          theme='solid'
          disabled={busy || giftFunds <= 0}
          onClick={async () => {
            if (await act('donate', gift))
              setGift({ stone: 0, herb: 0, ore: 0 });
          }}
        >
          {t('捐献')}
        </Button>
      </div>

      <h4 className='sc-heading'>
        <Users size={15} />
        {t('成员')}
      </h4>
      <ol className='sc-ladder'>
        {view.members.map((row) => (
          <li
            key={row.user_id}
            className={row.user_id === view.self ? 'is-me' : ''}
          >
            {row.role === 'leader' && <Crown size={14} />}
            <strong>{view.names[row.user_id] || `#${row.user_id}`}</strong>
            <span className='sc-spacer' />
            <span className='sc-muted'>
              {t('贡献 {{n}}', { n: formatAmount(row.contribution) })} ·{' '}
              {t('本周伤害 {{n}}', { n: formatAmount(row.week_damage) })}
            </span>
            {leader && row.user_id !== view.self && (
              <Button
                size='small'
                type='danger'
                theme='borderless'
                disabled={busy}
                onClick={() => confirmLeave(row.user_id)}
              >
                {t('请出')}
              </Button>
            )}
          </li>
        ))}
      </ol>
      <div className='sc-building-foot'>
        <span className='sc-spacer' />
        <Button type='danger' disabled={busy} onClick={() => confirmLeave(0)}>
          {t('退出仙盟')}
        </Button>
      </div>
    </div>
  );
};

// 仙盟页。post 是宗门页的请求(共用忙碌状态与错误提示)，onSect 收下操作之后的最新宗门。
const Alliance = ({ sect, now, busy, post, onSect, onEvents }) => {
  const { t } = useTranslation();
  const [view, setView] = useState(null);

  const load = useCallback(async () => {
    const response = await gameGet('/api/game/wendao/sect/alliance', t);
    if (response.data) setView(response.data);
  }, [t]);
  useEffect(() => {
    load();
  }, [load]);

  const act = async (action, body = {}) => {
    const data = await post(`/api/game/wendao/sect/alliance/${action}`, body);
    if (!data) return false;
    setView(data);
    if (data.sect) onSect(data.sect);
    if (data.events?.length) onEvents(data.events);
    if (action === 'attack') {
      playSound(data.attack?.killed ? 'win' : 'hit');
      if (data.attack?.killed) celebrate(1);
    }
    return true;
  };

  if (!view) return null;
  if (!view.alliance) {
    return <AllianceList view={view} sect={sect} busy={busy} act={act} />;
  }
  return (
    <AllianceHome
      key={view.alliance.id}
      view={view}
      sect={sect}
      now={now}
      busy={busy}
      act={act}
    />
  );
};

export default Alliance;
