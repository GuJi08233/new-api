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

import React, { useContext, useEffect, useState } from 'react';
import {
  Banner,
  Button,
  Card,
  Input,
  InputNumber,
  Modal,
  Spin,
  Switch,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess, showWarning } from '../../helpers';
import { StatusContext } from '../../context/Status';
import { getGameMetaMap } from '../../constants';

const { Text, Title } = Typography;

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

const GameAdminSettings = () => {
  const { t } = useTranslation();
  const [statusState, statusDispatch] = useContext(StatusContext);
  const [setting, setSetting] = useState(null);
  // 最近一次从服务端读到的配置，保存时据此只提交改动过的字段。
  const [savedSetting, setSavedSetting] = useState(null);
  const [hotWallet, setHotWallet] = useState(null);
  const [overview, setOverview] = useState(null);
  const [saving, setSaving] = useState(false);
  const [overviewLoading, setOverviewLoading] = useState(false);
  const [privateKey, setPrivateKey] = useState('');
  const [savingKey, setSavingKey] = useState(false);

  const loadSetting = async () => {
    const res = await API.get('/api/game/admin/setting');
    if (res.data.success) {
      setSetting(res.data.data.setting);
      setSavedSetting(res.data.data.setting);
      setHotWallet(res.data.data.hot_wallet);
    } else {
      showError(res.data.message);
    }
  };

  const loadOverview = async () => {
    setOverviewLoading(true);
    try {
      const res = await API.get('/api/game/admin/overview');
      if (res.data.success) {
        setOverview(res.data.data);
      } else {
        showError(res.data.message);
      }
    } finally {
      setOverviewLoading(false);
    }
  };

  useEffect(() => {
    loadSetting();
    loadOverview();
  }, []);

  const setField = (key, value) => {
    setSetting((previous) => ({ ...previous, [key]: value }));
  };

  const numberField = (key) => (value) => {
    setField(key, typeof value === 'number' ? value : 0);
  };

  // 每个游戏的规则是配置里以游戏标识为键的一个对象。
  const setGameRule = (game, key, value) => {
    setSetting((previous) => ({
      ...previous,
      [game]: { ...previous[game], [key]: value },
    }));
  };

  // 只提交改动过的字段：页面可能是很久以前打开的，整份提交会把别人在这期间改过的字段(例如紧急关闭的提现)改回去。
  const save = async () => {
    const changes = Object.fromEntries(
      Object.entries(setting).filter(
        ([key, value]) =>
          JSON.stringify(value) !== JSON.stringify(savedSetting?.[key]),
      ),
    );
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
      // 顶栏入口依赖 /api/status 的开关，保存后立即同步，不必刷新页面。
      statusDispatch({
        type: 'set',
        payload: {
          ...statusState.status,
          game_center_enabled: res.data.data.enabled,
        },
      });
      loadOverview();
    } finally {
      setSaving(false);
    }
  };

  // 私钥只写不读：接口只返回它对应的地址，输入框保存后即清空。
  const saveHotWallet = async () => {
    const key = privateKey.trim();
    if (!key) {
      showError(t('请填写私钥'));
      return;
    }
    setSavingKey(true);
    try {
      const res = await API.put('/api/game/admin/hot_wallet', {
        private_key: key,
      });
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      setPrivateKey('');
      setHotWallet(res.data.data);
      showSuccess(
        t('私钥已保存，热钱包地址 {{address}}', {
          address: res.data.data.address,
        }),
      );
      loadOverview();
    } finally {
      setSavingKey(false);
    }
  };

  const clearHotWallet = () => {
    Modal.confirm({
      title: t('清除后台私钥'),
      content: t(
        '清除后，如果设置了环境变量 {{env}} 将改用其中的私钥，否则提现会暂停处理，直到重新填写私钥。',
        { env: hotWallet?.env },
      ),
      onOk: async () => {
        const res = await API.delete('/api/game/admin/hot_wallet');
        if (!res.data.success) {
          showError(res.data.message);
          return;
        }
        setHotWallet(res.data.data);
        showSuccess(t('已清除后台私钥'));
        loadOverview();
      },
    });
  };

  if (!setting) {
    return <Spin spinning />;
  }

  let hotWalletSource = t('也可以通过环境变量 {{env}} 提供私钥', {
    env: hotWallet?.env,
  });
  if (hotWallet?.source === 'setting') {
    hotWalletSource = t('私钥来自后台设置');
  } else if (hotWallet?.source === 'env') {
    hotWalletSource = t(
      '私钥来自环境变量 {{env}}，在这里保存私钥后将改用后台私钥',
      { env: hotWallet.env },
    );
  }

  const gameMetaMap = getGameMetaMap(t);

  const unfinished =
    (overview?.status_counts?.pending || 0) +
    (overview?.status_counts?.signed || 0) +
    (overview?.status_counts?.sent || 0);

  return (
    <div className='flex flex-col gap-4'>
      <Card
        title={t('运行概览')}
        headerExtraContent={
          <Button
            icon={<IconRefresh />}
            theme='borderless'
            loading={overviewLoading}
            onClick={loadOverview}
          >
            {t('刷新')}
          </Button>
        }
      >
        {hotWallet?.configured ? (
          <div className='grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4'>
            <div>
              <Text type='tertiary' size='small'>
                {t('热钱包地址')}
              </Text>
              <div>
                <Text copyable ellipsis={{ showTooltip: true }}>
                  {hotWallet.address}
                </Text>
              </div>
            </div>
            <div>
              <Text type='tertiary' size='small'>
                {t('热钱包代币余额')}
              </Text>
              <div className='text-lg font-semibold'>
                {overview?.hot_wallet?.token_balance ?? '-'}{' '}
                {setting.token_symbol}
              </div>
            </div>
            <div>
              <Text type='tertiary' size='small'>
                {t('热钱包 ETH 余额（gas）')}
              </Text>
              <div className='text-lg font-semibold'>
                {overview?.hot_wallet?.eth_balance ?? '-'} ETH
              </div>
            </div>
            <div>
              <Text type='tertiary' size='small'>
                {t('今日已提现 / 全站上限')}
              </Text>
              <div className='text-lg font-semibold'>
                {overview?.withdraw_used ?? 0} / {overview?.withdraw_limit ?? 0}
              </div>
              <Text type='tertiary' size='small'>
                {t('处理中的提现 {{num}} 笔', { num: unfinished })}
              </Text>
            </div>
          </div>
        ) : (
          <Banner
            type='warning'
            closeIcon={null}
            title={t('热钱包未配置，链上提现无法发送')}
            description={t('请在下方「发币热钱包」中填写专用热钱包的私钥。')}
          />
        )}
        {overview?.hot_wallet?.balance_error && (
          <Banner
            className='mt-3'
            type='danger'
            closeIcon={null}
            description={t('读取热钱包余额失败：{{error}}', {
              error: overview.hot_wallet.balance_error,
            })}
          />
        )}
      </Card>

      <Card title={t('发币热钱包')}>
        <SettingRow label={t('当前钱包')} extra={hotWalletSource}>
          {hotWallet?.configured ? (
            <Text copyable ellipsis={{ showTooltip: true }}>
              {hotWallet.address}
            </Text>
          ) : (
            <Text type={hotWallet?.source ? 'danger' : 'tertiary'}>
              {hotWallet?.source
                ? t('私钥无效：{{error}}', { error: hotWallet.error })
                : t('尚未配置')}
            </Text>
          )}
        </SettingRow>
        <SettingRow
          label={t('私钥')}
          extra={t(
            '专用于发放游戏币的钱包，只放当日周转所需的代币和 gas。私钥保存在数据库中，保存后不会再显示',
          )}
        >
          <div className='flex flex-wrap gap-2'>
            <Input
              className='flex-1 min-w-[240px]'
              mode='password'
              autoComplete='new-password'
              value={privateKey}
              placeholder={t('64 位十六进制私钥，可带 0x 前缀')}
              onChange={setPrivateKey}
            />
            <Button theme='solid' loading={savingKey} onClick={saveHotWallet}>
              {t('保存私钥')}
            </Button>
            {hotWallet?.source === 'setting' && (
              <Button type='danger' onClick={clearHotWallet}>
                {t('清除私钥')}
              </Button>
            )}
          </div>
        </SettingRow>
      </Card>

      <Card title={t('游戏中心')}>
        <SettingRow
          label={t('启用游戏中心')}
          extra={t('关闭后顶栏入口隐藏，用户侧的游戏与游戏币接口不可用')}
        >
          <Switch
            checked={setting.enabled}
            onChange={(value) => setField('enabled', value)}
          />
        </SettingRow>
        <SettingRow
          label={t('开局人机验证')}
          extra={t(
            '俄罗斯方块每局开始前需通过 Cloudflare Turnstile 验证，挡住不经浏览器直接调用接口的脚本；老虎机按下注结算，不需要验证。需要先在系统设置中启用 Turnstile',
          )}
        >
          <div className='flex items-center gap-3 flex-wrap'>
            <Switch
              checked={setting.play_turnstile_enabled}
              onChange={(value) => setField('play_turnstile_enabled', value)}
            />
            {setting.play_turnstile_enabled &&
              !statusState?.status?.turnstile_check && (
                <Text type='warning' size='small'>
                  {t('系统设置中未启用 Turnstile，暂不生效')}
                </Text>
              )}
          </div>
        </SettingRow>
      </Card>

      {Object.entries(gameMetaMap).map(([game, meta]) => {
        const rule = setting[game];
        if (!rule) return null;
        const ruleNumber = (key) => (value) =>
          setGameRule(game, key, typeof value === 'number' ? value : 0);
        // 下注游戏(老虎机)的每日上限是净赢，没有分数与时长规则，改为限制单次下注。
        if (meta.wager) {
          return (
            <Card key={game} title={meta.name}>
              <SettingRow
                label={t('开放游戏')}
                extra={t('关闭后用户不能再下注')}
              >
                <Switch
                  checked={rule.enabled}
                  onChange={(value) => setGameRule(game, 'enabled', value)}
                />
              </SettingRow>
              <SettingRow
                label={t('均衡阶段下注额')}
                extra={t(
                  '每个玩家累计下注满这么多游戏币之前按均衡赔付表（长期不赚不赔）；之后单个樱桃不再退回下注，玩家长期是输的。0 表示从一开始就按后者',
                )}
              >
                <InputNumber
                  value={rule.even_stage_wager}
                  min={0}
                  precision={0}
                  suffix={setting.token_symbol}
                  onChange={ruleNumber('even_stage_wager')}
                />
              </SettingRow>
              <SettingRow
                label={t('每人每日净赢上限')}
                extra={t(
                  '0 表示不限制，玩家可以一直下注，输赢都照常结算。设了上限时，每个用户每天在这个游戏里最多净赢（派彩减去下注）这么多游戏币，同一 IP 下所有账号合计也不超过它；到上限后当天不能再下注，单次派彩超出的部分不发放',
                )}
              >
                <InputNumber
                  value={rule.daily_reward_limit}
                  min={0}
                  precision={0}
                  suffix={setting.token_symbol}
                  onChange={ruleNumber('daily_reward_limit')}
                />
              </SettingRow>
              <SettingRow
                label={t('单次最多下注')}
                extra={t(
                  '每次旋转最多下注的游戏币，派彩按下注的倍数计算，最高 50 倍',
                )}
              >
                <InputNumber
                  value={rule.max_bet}
                  min={1}
                  max={1000}
                  precision={0}
                  suffix={setting.token_symbol}
                  onChange={ruleNumber('max_bet')}
                />
              </SettingRow>
              <SettingRow
                label={t('今日净发放')}
                extra={t(
                  '所有用户今天在这个游戏里的净赢合计，负数表示玩家合计输掉的数量',
                )}
              >
                <Text strong>
                  {overview?.game_rewards?.[game] ?? 0} {setting.token_symbol}
                </Text>
              </SettingRow>
            </Card>
          );
        }
        return (
          <Card key={game} title={meta.name}>
            <SettingRow
              label={t('开放游戏')}
              extra={t('关闭后用户不能开始新的对局，进行中的对局也不能结算')}
            >
              <Switch
                checked={rule.enabled}
                onChange={(value) => setGameRule(game, 'enabled', value)}
              />
            </SettingRow>
            <SettingRow
              label={t('每人每日奖励上限')}
              extra={t(
                '每个用户每天最多从这个游戏获得的游戏币，同一 IP 下所有账号合计也不超过这个数；各游戏分别计算，0 表示照常可玩但不发奖励',
              )}
            >
              <InputNumber
                value={rule.daily_reward_limit}
                min={0}
                precision={0}
                suffix={setting.token_symbol}
                onChange={ruleNumber('daily_reward_limit')}
              />
            </SettingRow>
            <SettingRow
              label={t('每个游戏币所需分数')}
              extra={t('单局每满这么多分奖励 1 个游戏币，再受每日上限约束')}
            >
              <InputNumber
                value={rule.score_per_coin}
                min={1}
                precision={0}
                onChange={ruleNumber('score_per_coin')}
              />
            </SettingRow>
            <SettingRow
              label={t('每个游戏币所需时长（秒）')}
              extra={t(
                '单局每玩满这么多秒才能多得 1 个游戏币，挡住几秒就刷够分数的脚本；0 表示不限制',
              )}
            >
              <InputNumber
                value={rule.min_seconds_per_coin}
                min={0}
                max={3600}
                precision={0}
                onChange={ruleNumber('min_seconds_per_coin')}
              />
            </SettingRow>
            <SettingRow
              label={t('今日已发放')}
              extra={t('所有用户今天从这个游戏获得的游戏币总数')}
            >
              <Text strong>
                {overview?.game_rewards?.[game] ?? 0} {setting.token_symbol}
              </Text>
            </SettingRow>
          </Card>
        );
      })}

      <Card title={t('链上提现')}>
        <SettingRow
          label={t('开放链上提现')}
          extra={t('关闭后用户不能发起新的提现，已提交的提现仍会继续处理')}
        >
          <Switch
            checked={setting.withdraw_enabled}
            onChange={(value) => setField('withdraw_enabled', value)}
          />
        </SettingRow>
        <SettingRow
          label={t('全站每日提现上限')}
          extra={t('所有用户当天提现总量的上限，失败退回的提现不占用额度')}
        >
          <InputNumber
            value={setting.withdraw_daily_limit}
            min={1}
            precision={0}
            suffix={setting.token_symbol}
            onChange={numberField('withdraw_daily_limit')}
          />
        </SettingRow>
        <SettingRow label={t('单次提现最少')}>
          <InputNumber
            value={setting.withdraw_min_amount}
            min={1}
            precision={0}
            suffix={setting.token_symbol}
            onChange={numberField('withdraw_min_amount')}
          />
        </SettingRow>
        <SettingRow
          label={t('单次提现最多')}
          extra={t('防止单个用户一次用完当日额度')}
        >
          <InputNumber
            value={setting.withdraw_max_amount}
            min={1}
            precision={0}
            suffix={setting.token_symbol}
            onChange={numberField('withdraw_max_amount')}
          />
        </SettingRow>
      </Card>

      <Card title={t('代币与网络')}>
        <SettingRow
          label={t('代币合约地址')}
          extra={t('ERC-20 合约地址，热钱包只做 transfer，不需要是合约 owner')}
        >
          <Input
            value={setting.token_address}
            onChange={(value) => setField('token_address', value)}
          />
        </SettingRow>
        <SettingRow label={t('代币符号')}>
          <Input
            value={setting.token_symbol}
            maxLength={11}
            onChange={(value) => setField('token_symbol', value)}
            style={{ maxWidth: 200 }}
          />
        </SettingRow>
        <SettingRow
          label={t('代币精度 (decimals)')}
          extra={t('必须与合约的真实值一致，填错会让转账数量相差若干个数量级')}
        >
          <InputNumber
            value={setting.token_decimals}
            min={0}
            max={36}
            precision={0}
            onChange={numberField('token_decimals')}
          />
        </SettingRow>
        <SettingRow
          label={t('链 ID')}
          extra={t('Sepolia 为 11155111，必须与 RPC 节点所在的网络一致')}
        >
          <InputNumber
            value={setting.chain_id}
            min={1}
            precision={0}
            onChange={numberField('chain_id')}
          />
        </SettingRow>
        <SettingRow
          label={t('RPC 节点地址')}
          extra={t('公共节点限流较严，建议使用 Alchemy / Infura 的专属节点')}
        >
          <Input
            value={setting.rpc_url}
            onChange={(value) => setField('rpc_url', value)}
          />
        </SettingRow>
        <SettingRow
          label={t('区块浏览器地址')}
          extra={t('用于生成交易与代币链接，可留空')}
        >
          <Input
            value={setting.explorer_url}
            onChange={(value) => setField('explorer_url', value)}
          />
        </SettingRow>
        <SettingRow
          label={t('确认数')}
          extra={t(
            '从交易所在区块起累计达到这么多个区块才判定到账或退款，1 表示打包即下结论；区块重组可能推翻太浅的结论，建议至少 3',
          )}
        >
          <InputNumber
            value={setting.confirmations}
            min={1}
            max={64}
            precision={0}
            onChange={numberField('confirmations')}
          />
        </SettingRow>
      </Card>

      <div className='flex items-center justify-between gap-4 flex-wrap'>
        <Title heading={6} type='tertiary'>
          {t('游戏币与额度按 1 个游戏币 = 1 美元额度兑换')}
        </Title>
        <Button theme='solid' loading={saving} onClick={save}>
          {t('保存设置')}
        </Button>
      </div>
    </div>
  );
};

export default GameAdminSettings;
