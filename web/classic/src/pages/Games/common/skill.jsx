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

import React from 'react';
import { useTranslation } from 'react-i18next';
import { formatNumber } from './api';
import { RuleList, SideBox } from './SideBox';

// 免费游戏(技巧游戏与人机对战)共用的领奖规则展示与结算提示。info 是 GET /api/game/<key> 返回的 skillInfo 部分：
// rule { daily_rewards, tiers, min_seconds_per_coin }、free_daily_limit、progress、best_score。

// 今天还能领几次：账号与同一 IP 各算一次，取剩下较少的那边。
export function rewardsLeft(rule, progress) {
  const used = Math.max(
    progress?.rewards_today || 0,
    progress?.ip_rewards_today || 0,
  );
  return Math.max((rule?.daily_rewards || 0) - used, 0);
}

// 成绩的写法：分数类直接写数字，难度类写难度名称。
export function formatSkillScore(score, levelNames) {
  if (levelNames) return levelNames[score - 1] || String(score);
  return formatNumber(score);
}

// 一局免费游戏结算之后给玩家看的提示：得了多少、没得或少得的原因。result 是结算接口的返回。
export function skillResultMessage(result, t, symbol) {
  const reward = result?.reward || 0;
  const got = t('获得 {{reward}} {{symbol}}', { reward, symbol });
  switch (result?.limited_by) {
    case 'speed':
      return {
        kind: 'error',
        text: t('操作快过人类极限，判定为脚本，不发奖励'),
      };
    case 'daily':
      return {
        kind: '',
        text: t('今天这个游戏的领奖次数已经用完，本局不发奖励'),
      };
    case 'ip':
      return {
        kind: '',
        text: t('当前 IP 今天在这个游戏的领奖次数已经用完'),
      };
    case 'total':
      return reward > 0
        ? {
            kind: 'win',
            text: got,
            note: t('今天的免费奖励已达合计上限，超出的部分不发放'),
          }
        : { kind: '', text: t('今天的免费奖励已达合计上限，明天再来') };
    case 'time':
      return reward > 0
        ? {
            kind: 'win',
            text: got,
            note: t('游戏时长不够更高一档，按时长只发这么多'),
          }
        : { kind: '', text: t('游戏时长太短，这局不够领奖') };
  }
  if (reward > 0) return { kind: 'win', text: got };
  if ((result?.rule?.daily_rewards || 0) === 0) {
    return { kind: '', text: t('这个游戏暂不发放奖励') };
  }
  return { kind: 'loss', text: t('没有达到奖励档位，再接再厉') };
}

// 免费游戏信息栏里的领奖规则：今天还能领几次、奖励档位、时长要求与合计上限、最高成绩。
// levelNames 不为空时成绩按难度显示；scoreName 是成绩一列的标题。
export const SkillSide = ({ info, symbol, scoreName, levelNames, extra }) => {
  const { t } = useTranslation();
  const rule = info?.rule || {};
  const progress = info?.progress || {};
  const tiers = rule.tiers || [];
  const left = rewardsLeft(rule, progress);
  const freeLimit = info?.free_daily_limit || 0;
  const freeEarned = Math.max(
    progress.free_earned_today || 0,
    progress.ip_free_earned_today || 0,
  );

  let rewardNote = t('今天还能领 {{left}} 次', { left });
  if (rule.daily_rewards === 0) {
    rewardNote = t('这个游戏暂不发放奖励');
  } else if (left === 0) {
    rewardNote = t('今天的领奖次数已用完，还可以继续玩');
  }

  return (
    <>
      <SideBox
        label={t('今日奖励')}
        value={progress.earned_today || 0}
        unit={symbol}
        note={rewardNote}
      />
      <SideBox label={t('奖励规则')}>
        {tiers.length > 0 && (
          <table className='game-table'>
            <thead>
              <tr>
                <th>{scoreName || t('成绩')}</th>
                <th>{t('奖励')}</th>
              </tr>
            </thead>
            <tbody>
              {tiers.map((tier) => (
                <tr key={tier.score}>
                  <td>
                    {levelNames
                      ? t('赢下{{level}}', {
                          level: formatSkillScore(tier.score, levelNames),
                        })
                      : t('{{score}} 以上', {
                          score: formatSkillScore(tier.score),
                        })}
                  </td>
                  <td>
                    {tier.coins} {symbol}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <RuleList
          items={[
            rule.daily_rewards > 0 &&
              t(
                '每天最多领 {{count}} 次奖励，同一 IP 下所有账号合计；领完之后照常可玩，不再发奖',
                { count: rule.daily_rewards },
              ),
            t('每局按达到的最高一档发奖，没达到第一档不发'),
            rule.min_seconds_per_coin > 0 &&
              t('每个游戏币至少要玩 {{seconds}} 秒', {
                seconds: rule.min_seconds_per_coin,
              }),
            freeLimit > 0 &&
              t(
                '所有免费游戏每天合计最多奖励 {{limit}} {{symbol}}，今天已领 {{earned}}',
                { limit: freeLimit, symbol, earned: freeEarned },
              ),
          ]}
        />
      </SideBox>
      {extra}
      <SideBox
        label={levelNames ? t('最高难度') : t('最高成绩')}
        value={
          info?.best_score > 0
            ? formatSkillScore(info.best_score, levelNames)
            : '-'
        }
      />
    </>
  );
};

// 一句话的领奖规则，放在开局前的说明里。scoreName 是成绩的叫法(分数、吃到的豆子……)，levelNames 不为空时成绩是难度。
export function skillRuleSummary(info, t, symbol, scoreName, levelNames) {
  const rule = info?.rule;
  if (!rule || rule.daily_rewards === 0 || !rule.tiers?.length) {
    return t('这个游戏暂不发放奖励');
  }
  const tiers = rule.tiers
    .map((tier) =>
      levelNames
        ? t('赢下{{level}}得 {{coins}} {{symbol}}', {
            level: formatSkillScore(tier.score, levelNames),
            coins: tier.coins,
            symbol,
          })
        : t('{{name}}达到 {{score}} 得 {{coins}} {{symbol}}', {
            name: scoreName,
            score: formatSkillScore(tier.score),
            coins: tier.coins,
            symbol,
          }),
    )
    .join(t('，'));
  return t('{{tiers}}；每天最多领 {{count}} 次', {
    tiers,
    count: rule.daily_rewards,
  });
}
