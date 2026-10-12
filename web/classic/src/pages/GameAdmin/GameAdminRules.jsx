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

import React, { useEffect, useState } from 'react';
import {
  Button,
  Card,
  Collapse,
  InputNumber,
  Select,
  Spin,
  Switch,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconPlus, IconRefresh } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess, showWarning } from '../../helpers';
import {
  GAME_KINDS,
  GAME_ORDER,
  getGameKindMap,
  getGameLevelNames,
  getGameMetaMap,
} from '../../constants';

const { Text } = Typography;

// 与服务端 gameRuleSpecs 一致的取值范围，保存时服务端还会再校验一遍。
const MAX_DAILY_REWARDS = 100;
const MAX_REWARD_TIERS = 6;
const MAX_TIER_COINS = 100;
// 奖励档位最少 0.01 个游戏币，最多两位小数。
const MIN_TIER_COINS = 0.01;
const MAX_SECONDS_PER_COIN = 3600;
const MIN_RTP = 50;
const MAX_RTP = 99;
const MAX_PVP_STAKE = 10000;
const MAX_RAKE = 20;
const MIN_MOVE_SECONDS = 10;
const MAX_MOVE_SECONDS = 600;
const MAX_JACKPOT_BET = 10000;
// 分档投入的游戏只能从这几档里选最高开放到哪一档；其他下注游戏的单次下注上限与下限。
const BET_STEPS = { plinko: [1, 10, 100], scratch: [1, 5, 20] };
const MIN_BETS = { blackjack: 2 };
const MAX_BET = 1000;

// 一行配置：左侧标题与说明，右侧控件。
function SettingRow({ label, extra, children }) {
  return (
    <div className='flex flex-col md:flex-row md:items-center gap-2 md:gap-6 py-3 border-b border-[var(--semi-color-border)] last:border-b-0'>
      <div className='md:w-64 shrink-0'>
        <div className='font-medium'>{label}</div>
        {extra && (
          <Text type='tertiary' size='small'>
            {extra}
          </Text>
        )}
      </div>
      <div className='flex-1 min-w-0'>{children}</div>
    </div>
  );
}

const toNumber = (value) => (typeof value === 'number' ? value : 0);

// 免费游戏的领奖规则：每日领奖次数、奖励档位、每个游戏币所需的当天累计时长。levelNames 不为空时成绩是难度，用下拉选择；
// maxScore 是服务端规定的成绩上限(例如推箱子只有过关一种成绩)，没有上限时为空。
function SkillRuleFields({
  rule,
  onChange,
  levelNames,
  maxScore,
  scoreName,
  symbol,
}) {
  const { t } = useTranslation();
  const tiers = rule.tiers || [];
  const setTier = (index, key, value) =>
    onChange({
      tiers: tiers.map((tier, i) =>
        i === index ? { ...tier, [key]: toNumber(value) } : tier,
      ),
    });
  const scoreLimit = levelNames ? levelNames.length : maxScore;
  const last = tiers[tiers.length - 1];
  // 成绩逐档递增：已经到了上限就加不了下一档。
  const full = !!scoreLimit && !!last && last.score >= scoreLimit;
  const addTier = () => {
    if (full) return;
    const next = last
      ? {
          score: levelNames ? last.score + 1 : last.score * 2,
          coins: Math.min(last.coins + 1, MAX_TIER_COINS),
        }
      : { score: 1, coins: 1 };
    if (scoreLimit) next.score = Math.min(next.score, scoreLimit);
    onChange({ tiers: [...tiers, next] });
  };
  return (
    <>
      <SettingRow
        label={t('每人每日领奖次数')}
        extra={t(
          '每天前几次达标的对局发奖，同一 IP 下所有账号合计也只有这么多次；领完之后照常可玩、不再发奖。0 表示只玩不发奖',
        )}
      >
        <InputNumber
          value={rule.daily_rewards}
          min={0}
          max={MAX_DAILY_REWARDS}
          precision={0}
          onChange={(value) => onChange({ daily_rewards: toNumber(value) })}
        />
      </SettingRow>
      <SettingRow
        label={t('奖励档位')}
        extra={t(
          '单局{{score}}达到某一档就发这一档的奖励，取达到的最高一档；成绩逐档递增、奖励不能比前一档少',
          { score: scoreName },
        )}
      >
        <div className='flex flex-col gap-2'>
          {tiers.map((tier, index) => (
            <div key={index} className='flex flex-wrap items-center gap-2'>
              <Text type='tertiary'>
                {t('第 {{index}} 档', { index: index + 1 })}
              </Text>
              {levelNames ? (
                <Select
                  value={tier.score}
                  style={{ width: 140 }}
                  optionList={levelNames.map((name, level) => ({
                    value: level + 1,
                    label: t('赢下{{level}}', { level: name }),
                  }))}
                  onChange={(value) => setTier(index, 'score', value)}
                />
              ) : (
                <InputNumber
                  value={tier.score}
                  min={1}
                  max={maxScore}
                  precision={0}
                  prefix={t('达到')}
                  style={{ width: 180 }}
                  onChange={(value) => setTier(index, 'score', value)}
                />
              )}
              <InputNumber
                value={tier.coins}
                min={MIN_TIER_COINS}
                max={MAX_TIER_COINS}
                precision={2}
                prefix={t('奖励')}
                suffix={symbol}
                style={{ width: 160 }}
                onChange={(value) => setTier(index, 'coins', value)}
              />
              <Button
                icon={<IconDelete />}
                theme='borderless'
                type='danger'
                disabled={tiers.length <= 1}
                onClick={() =>
                  onChange({ tiers: tiers.filter((_, i) => i !== index) })
                }
              />
            </div>
          ))}
          <div>
            <Button
              icon={<IconPlus />}
              size='small'
              disabled={tiers.length >= MAX_REWARD_TIERS || full}
              onClick={addTier}
            >
              {t('添加一档')}
            </Button>
          </div>
        </div>
      </SettingRow>
      <SettingRow
        label={t('每个游戏币所需累计时长（秒）')}
        extra={t(
          '玩家当天在这个游戏里每累计玩满这么多秒才能多领 1 个游戏币，先达标的对局等时长够了再发；挡住几秒就打出好成绩的脚本。0 表示不限制',
        )}
      >
        <InputNumber
          value={rule.min_seconds_per_coin}
          min={0}
          max={MAX_SECONDS_PER_COIN}
          precision={0}
          onChange={(value) =>
            onChange({ min_seconds_per_coin: toNumber(value) })
          }
        />
      </SettingRow>
    </>
  );
}

// 下注游戏：单次下注上限，按公式计算赔率的游戏还有返还率。
function WagerRuleFields({ game, rule, onChange, symbol }) {
  const { t } = useTranslation();
  const steps = BET_STEPS[game];
  return (
    <>
      <SettingRow
        label={steps ? t('最高开放档位') : t('单次最多下注')}
        extra={
          steps
            ? t('只开放不高于这一档的投入')
            : t('派彩按下注的倍数计算，上限越高，单局可能派出的游戏币越多')
        }
      >
        {steps ? (
          <Select
            value={rule.max_bet}
            style={{ width: 160 }}
            optionList={steps.map((step) => ({
              value: step,
              label: `${step} ${symbol}`,
            }))}
            onChange={(value) => onChange({ max_bet: value })}
          />
        ) : (
          <InputNumber
            value={rule.max_bet}
            min={MIN_BETS[game] || 1}
            max={MAX_BET}
            precision={0}
            suffix={symbol}
            onChange={(value) => onChange({ max_bet: toNumber(value) })}
          />
        )}
      </SettingRow>
      {rule.rtp_percent !== undefined && (
        <SettingRow
          label={t('理论返还率')}
          extra={t(
            '赔率按这个返还率计算，必须低于 100%，玩家长期是输的；派彩向下取整，实际返还率略低',
          )}
        >
          <InputNumber
            value={rule.rtp_percent}
            min={MIN_RTP}
            max={MAX_RTP}
            precision={0}
            suffix='%'
            onChange={(value) => onChange({ rtp_percent: toNumber(value) })}
          />
        </SettingRow>
      )}
    </>
  );
}

function WheelRuleFields({ rule, onChange, symbol }) {
  const { t } = useTranslation();
  return (
    <>
      <SettingRow
        label={t('每人每日免费次数')}
        extra={t(
          '默认每天免费 1 次，同一 IP 下所有账号共用次数；奖品全额发放，不占技巧与人机奖励上限。0 表示没有免费次数',
        )}
      >
        <InputNumber
          value={rule.free_spins}
          min={0}
          max={100}
          precision={0}
          onChange={(value) => onChange({ free_spins: toNumber(value) })}
        />
      </SettingRow>
      <SettingRow
        label={t('每次花费')}
        extra={t(
          '免费次数用完后每次旋转花费的游戏币，不限次数。奖品按它的倍数计算，最高 50 倍',
        )}
      >
        <InputNumber
          value={rule.spin_cost}
          min={1}
          max={100}
          precision={0}
          suffix={symbol}
          onChange={(value) => onChange({ spin_cost: toNumber(value) })}
        />
      </SettingRow>
    </>
  );
}

function PvpRuleFields({ game, rule, onChange, symbol, levelNames }) {
  const { t } = useTranslation();
  const ai = rule.ai || {};
  return (
    <>
      <SettingRow
        label={t('押注上限')}
        extra={
          game === 'landlord'
            ? t(
                '底注上限：地主输赢两倍底注、农民各输赢一倍，入座时每人按两倍预扣；0 表示只能开友谊赛。同一 IP 下的账号不能互相押注对战',
              )
            : t(
                '每位玩家最多押注的游戏币，获胜玩家按游戏规则分配奖池；0 表示只能开友谊赛。同一 IP 下的账号不能互相押注对战',
              )
        }
      >
        <InputNumber
          value={rule.max_stake}
          min={0}
          max={MAX_PVP_STAKE}
          precision={0}
          suffix={symbol}
          onChange={(value) => onChange({ max_stake: toNumber(value) })}
        />
      </SettingRow>
      <SettingRow
        label={t('平台抽成')}
        extra={
          game === 'landlord'
            ? t('从获胜玩家的净赢额抽取的比例，向下取整；平局全额退回')
            : t('从赢家拿走的奖池里抽取的比例，按奖池向下取整；平局全额退回')
        }
      >
        <InputNumber
          value={rule.rake_percent}
          min={0}
          max={MAX_RAKE}
          precision={0}
          suffix='%'
          onChange={(value) => onChange({ rake_percent: toNumber(value) })}
        />
      </SettingRow>
      <SettingRow
        label={t('每步限时（秒）')}
        extra={
          game === 'landlord'
            ? t('轮到的玩家超时没走时转为托管，由 AI 代打')
            : t('轮到的一方超时没走判负')
        }
      >
        <InputNumber
          value={rule.move_seconds}
          min={MIN_MOVE_SECONDS}
          max={MAX_MOVE_SECONDS}
          precision={0}
          onChange={(value) => onChange({ move_seconds: toNumber(value) })}
        />
      </SettingRow>
      {game !== 'rps' && (
        <>
          <SettingRow
            label={t('开放人机对战')}
            extra={t(
              '和服务端 AI 对战，赢了按难度领奖，领奖规则与免费游戏相同',
            )}
          >
            <Switch
              checked={!!ai.enabled}
              onChange={(value) => onChange({ ai: { ...ai, enabled: value } })}
            />
          </SettingRow>
          {ai.enabled && (
            <SkillRuleFields
              rule={ai}
              levelNames={levelNames}
              scoreName={t('AI 难度')}
              symbol={symbol}
              onChange={(changes) => onChange({ ai: { ...ai, ...changes } })}
            />
          )}
        </>
      )}
    </>
  );
}

function JackpotRuleFields({ rule, onChange, symbol }) {
  const { t } = useTranslation();
  return (
    <>
      <SettingRow label={t('每人每轮最多投入')}>
        <InputNumber
          value={rule.max_bet}
          min={1}
          max={MAX_JACKPOT_BET}
          precision={0}
          suffix={symbol}
          onChange={(value) => onChange({ max_bet: toNumber(value) })}
        />
      </SettingRow>
      <SettingRow
        label={t('开奖倒计时（秒）')}
        extra={t('第二位玩家投入后开始倒计时，到点开奖')}
      >
        <InputNumber
          value={rule.round_seconds}
          min={15}
          max={600}
          precision={0}
          onChange={(value) => onChange({ round_seconds: toNumber(value) })}
        />
      </SettingRow>
      <SettingRow
        label={t('平台抽成')}
        extra={t('从奖池里抽取的比例，按奖池向下取整')}
      >
        <InputNumber
          value={rule.rake_percent}
          min={0}
          max={MAX_RAKE}
          precision={0}
          suffix='%'
          onChange={(value) => onChange({ rake_percent: toNumber(value) })}
        />
      </SettingRow>
    </>
  );
}

// 游戏规则页：按结算方式分组，每个游戏一个折叠面板，保存时只提交改动过的游戏与子字段，服务端合并进当前规则。
const GameAdminRules = () => {
  const { t } = useTranslation();
  const [setting, setSetting] = useState(null);
  const [savedSetting, setSavedSetting] = useState(null);
  const [overview, setOverview] = useState(null);
  const [maxTierScores, setMaxTierScores] = useState({});
  const [saving, setSaving] = useState(false);

  // 概览要查热钱包余额，节点慢或出错时不能拖住规则的加载：两者分开请求，概览失败只是不显示概览。
  const load = () => {
    API.get('/api/game/admin/setting')
      .then((res) => {
        if (!res.data.success) {
          showError(res.data.message);
          return;
        }
        setSetting(res.data.data.setting);
        setSavedSetting(res.data.data.setting);
        setMaxTierScores(res.data.data.max_tier_scores || {});
      })
      .catch(() => {});
    API.get('/api/game/admin/overview')
      .then((res) => {
        if (res.data.success) setOverview(res.data.data);
      })
      .catch(() => {});
  };

  useEffect(() => {
    load();
  }, []);

  if (!setting) return <Spin spinning />;

  const metaMap = getGameMetaMap(t);
  const kindMap = getGameKindMap(t);
  const levelNames = getGameLevelNames(t);
  const symbol = setting.token_symbol;
  const rewards = overview?.game_rewards || {};

  const setRule = (game, changes) =>
    setSetting((previous) => ({
      ...previous,
      [game]: { ...previous[game], ...changes },
    }));

  const save = async () => {
    const changes = {};
    for (const game of GAME_ORDER) {
      const rule = setting[game];
      const saved = savedSetting?.[game];
      if (!rule) continue;
      const fields = Object.entries(rule).filter(
        ([field, value]) =>
          JSON.stringify(value) !== JSON.stringify(saved?.[field]),
      );
      if (fields.length > 0) changes[game] = Object.fromEntries(fields);
    }
    if (Object.keys(changes).length === 0) {
      showWarning(t('你似乎并没有修改什么'));
      return;
    }
    setSaving(true);
    try {
      const res = await API.put('/api/game/admin/setting', changes);
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      setSetting(res.data.data);
      setSavedSetting(res.data.data);
      showSuccess(t('保存成功'));
    } finally {
      setSaving(false);
    }
  };

  const renderFields = (game) => {
    const meta = metaMap[game];
    const rule = setting[game];
    const onChange = (changes) => setRule(game, changes);
    if (meta.kind === 'skill') {
      return (
        <SkillRuleFields
          rule={rule}
          onChange={onChange}
          levelNames={meta.score === 'level' ? levelNames[game] : null}
          maxScore={maxTierScores[game]}
          scoreName={meta.scoreName}
          symbol={symbol}
        />
      );
    }
    if (meta.kind === 'pvp') {
      return (
        <PvpRuleFields
          game={game}
          rule={rule}
          onChange={onChange}
          symbol={symbol}
          levelNames={levelNames.ai}
        />
      );
    }
    if (meta.kind === 'party') {
      return (
        <JackpotRuleFields rule={rule} onChange={onChange} symbol={symbol} />
      );
    }
    if (game === 'wheel') {
      return (
        <WheelRuleFields rule={rule} onChange={onChange} symbol={symbol} />
      );
    }
    return (
      <WagerRuleFields
        game={game}
        rule={rule}
        onChange={onChange}
        symbol={symbol}
      />
    );
  };

  // 今日净发放：免费游戏是发出的游戏币；下注、对战与奖池是所有玩家的净赢合计，负数表示玩家合计输掉的数量。
  const todayOf = (game) => {
    const value = (rewards[game] || 0) + (rewards[`${game}_ai`] || 0);
    return value > 0 ? `+${value}` : `${value}`;
  };

  return (
    <div className='flex flex-col gap-4'>
      {GAME_KINDS.map((kind) => {
        const games = GAME_ORDER.filter(
          (game) => metaMap[game]?.kind === kind && setting[game],
        );
        return (
          <Card
            key={kind}
            title={kindMap[kind].name}
            headerExtraContent={
              kind === GAME_KINDS[0] && (
                <Button
                  icon={<IconRefresh />}
                  theme='borderless'
                  onClick={load}
                >
                  {t('刷新')}
                </Button>
              )
            }
          >
            <Text type='tertiary' size='small'>
              {kindMap[kind].description}
            </Text>
            <Collapse className='mt-2'>
              {games.map((game) => {
                const rule = setting[game];
                return (
                  <Collapse.Panel
                    key={game}
                    itemKey={game}
                    header={
                      <div className='flex items-center gap-3 flex-wrap'>
                        <span className='font-medium'>
                          {metaMap[game].name}
                        </span>
                        <Tag
                          color={rule.enabled ? 'green' : 'grey'}
                          size='small'
                        >
                          {rule.enabled ? t('已开放') : t('未开放')}
                        </Tag>
                        <Text type='tertiary' size='small'>
                          {t('今日净发放 {{value}} {{symbol}}', {
                            value: todayOf(game),
                            symbol,
                          })}
                        </Text>
                      </div>
                    }
                  >
                    <SettingRow
                      label={t('开放游戏')}
                      extra={t(
                        '关闭后用户不能再开始新的对局；押着游戏币、没结束的一局仍能玩完',
                      )}
                    >
                      <Switch
                        checked={!!rule.enabled}
                        onChange={(value) => setRule(game, { enabled: value })}
                      />
                    </SettingRow>
                    {renderFields(game)}
                  </Collapse.Panel>
                );
              })}
            </Collapse>
          </Card>
        );
      })}
      <div className='flex items-center justify-end gap-4'>
        <Button theme='solid' loading={saving} onClick={save}>
          {t('保存游戏规则')}
        </Button>
      </div>
    </div>
  );
};

export default GameAdminRules;
