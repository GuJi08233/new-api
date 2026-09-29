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

import React, { useCallback, useContext, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Banner,
  Button,
  Empty,
  Input,
  InputNumber,
  Modal,
  Spin,
  Typography,
} from '@douyinfe/semi-ui';
import {
  IllustrationConstruction,
  IllustrationConstructionDark,
} from '@douyinfe/semi-illustrations';
import {
  ArrowRightLeft,
  Coins,
  ExternalLink,
  Gamepad2,
  Send,
  Wallet,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../helpers';
import { renderQuota } from '../../helpers/render';
import {
  discoverInjectedWallets,
  isEthereumUserRejected,
} from '../../helpers/ethereumWallet';
import { UserContext } from '../../context/User';
import {
  GAME_KINDS,
  GAME_ORDER,
  getGameKindMap,
  getGameMetaMap,
} from '../../constants';
import GameCoinRecords from './GameCoinRecords';
import { GAME_ICONS } from './registry';
import { signed } from './common/api';
import './games.css';

const { Text } = Typography;

const ILLUSTRATION_SIZE = { width: 150, height: 150 };
const ADDRESS_PATTERN = /^0x[0-9a-fA-F]{40}$/;

// 游戏卡片上的今日进度，按结算方式各写各的：免费游戏看领奖次数，对战看等待中的房间与人机奖励，
// 下注游戏看今日输赢，奖池看当前奖池。
function gameProgress(game, t, symbol) {
  if (!game.enabled) {
    return [t('游戏已关闭，你还有一局没结束')];
  }
  const lines = [];
  switch (game.kind) {
    case 'skill':
      lines.push(
        game.daily_rewards > 0
          ? t('今日奖励 {{used}} / {{limit}} 次', {
              used: Math.min(game.rewards_today, game.daily_rewards),
              limit: game.daily_rewards,
            })
          : t('暂不发放奖励'),
      );
      if (game.max_reward > 0 && game.daily_rewards > 0) {
        lines.push(
          t('每次最多 {{coins}} {{symbol}}', {
            coins: game.max_reward,
            symbol,
          }),
        );
      }
      break;
    case 'pvp':
      lines.push(
        game.waiting_rooms > 0
          ? t('{{count}} 个房间等待对手', { count: game.waiting_rooms })
          : t('暂无等待中的房间'),
      );
      if (game.ai_enabled && game.ai_daily_rewards > 0) {
        lines.push(
          t('人机奖励 {{used}} / {{limit}} 次', {
            used: Math.min(game.ai_rewards_today, game.ai_daily_rewards),
            limit: game.ai_daily_rewards,
          }),
        );
      }
      break;
    case 'party':
      lines.push(
        game.pot > 0
          ? t('当前奖池 {{pot}} {{symbol}} · {{players}} 人', {
              pot: game.pot,
              players: game.players,
              symbol,
            })
          : t('等待第一位玩家投入'),
      );
      break;
    default:
      if (game.free_spins > 0) {
        lines.push(
          t('免费剩余 {{count}} 次', {
            count: Math.max(game.free_spins - game.free_used_today, 0),
          }),
        );
      }
      lines.push(
        t('今日输赢 {{net}} {{symbol}}', {
          net: signed(game.net_today),
          symbol,
        }),
      );
  }
  return lines;
}

// 取浏览器里排名最靠前的 EVM 钱包(Rabby / MetaMask 等)，没有时提示安装。
async function findInjectedWallet(t) {
  const wallets = await discoverInjectedWallets();
  if (!wallets.length) {
    showError(t('未检测到浏览器钱包，请安装 MetaMask 等 EVM 钱包'));
    return null;
  }
  return wallets[0].provider;
}

const Games = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [, userDispatch] = useContext(UserContext);
  const [info, setInfo] = useState(null);
  const [unavailable, setUnavailable] = useState('');
  const [loading, setLoading] = useState(true);
  const [convertAmount, setConvertAmount] = useState(1);
  const [converting, setConverting] = useState(false);
  const [withdrawAddress, setWithdrawAddress] = useState('');
  const [withdrawAmount, setWithdrawAmount] = useState(1);
  const [withdrawing, setWithdrawing] = useState(false);
  const [recordsVersion, setRecordsVersion] = useState(0);
  const [kindFilter, setKindFilter] = useState('all');

  const loadInfo = useCallback(async () => {
    try {
      const res = await API.get('/api/game/self');
      if (res.data.success) {
        setInfo(res.data.data);
        setConvertAmount((amount) =>
          Math.min(
            Math.max(amount, 1),
            res.data.data.account?.balance || 0,
            res.data.data.convert?.daily_remaining || 0,
          ),
        );
        setUnavailable('');
      } else {
        // 游戏中心关闭等情况下整页不可用，只显示原因，不再加载钱包和记录。
        setUnavailable(res.data.message);
      }
    } catch (error) {
      // 被风控封禁的 IP 收到 403，同样整页只显示原因；其他错误已由全局提示处理。
      if (error?.response?.status === 403) {
        setUnavailable(error.response.data?.message || '');
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadInfo();
  }, [loadInfo]);

  const account = info?.account;
  const balance = account?.balance || 0;
  const token = info?.token;
  const symbol = token?.symbol || '';
  const quotaPerCoin = info?.quota_per_coin || 0;
  const convert = info?.convert;
  const convertRemaining = convert?.daily_remaining || 0;
  const maxConvertAmount = Math.min(balance, convertRemaining);
  const withdraw = info?.withdraw;
  // 游戏中心关闭后，只有还押着游戏币、没结束一局的玩家能打开这里：只列出那几个游戏，兑换、提现与记录都不开放。
  const closed = info?.closed === true;
  const gameMetaMap = getGameMetaMap(t);
  const kindMap = getGameKindMap(t);
  // 只列出前端认识的游戏，按 GAME_ORDER 排列。
  const games = (info?.games || [])
    .filter((game) => gameMetaMap[game.key])
    .sort((a, b) => GAME_ORDER.indexOf(a.key) - GAME_ORDER.indexOf(b.key));
  const kinds = GAME_KINDS.filter((kind) =>
    games.some((game) => game.kind === kind),
  );
  const shownGames =
    kindFilter === 'all'
      ? games
      : games.filter((game) => game.kind === kindFilter);
  // 下注、对战与奖池的累计输赢不计入累计获得，单独列出；没开放这些游戏、也没玩过时不显示。
  const wagerNet = account?.total_wager_net || 0;
  const showWagerNet =
    wagerNet !== 0 || games.some((game) => game.kind !== 'skill');
  const free = info?.free;
  const freeEarned = Math.max(
    free?.earned_today || 0,
    free?.ip_earned_today || 0,
  );

  const refreshAfterChange = () => {
    setRecordsVersion((version) => version + 1);
    return loadInfo();
  };

  const submitConvert = async (amount) => {
    setConverting(true);
    try {
      const res = await API.post('/api/game/convert', { amount });
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      showSuccess(
        t('兑换成功，获得 {{quota}} 额度', {
          quota: renderQuota(res.data.data.quota),
        }),
      );
      const self = await API.get('/api/user/self');
      if (self.data.success) {
        userDispatch({ type: 'login', payload: self.data.data });
      }
    } catch (error) {
      // 请求错误已由全局拦截器提示。
    } finally {
      // 其他标签页可能已兑换，请求失败或结果未知时也重新读取个人今日余量。
      await refreshAfterChange();
      setConverting(false);
    }
  };

  const handleConvert = () => {
    if (!Number.isInteger(convertAmount) || convertAmount < 1) {
      showError(t('请输入正整数数量'));
      return;
    }
    if (convertAmount > balance) {
      showError(t('游戏币余额不足'));
      return;
    }
    if (convertAmount > convertRemaining) {
      showError(t('超过个人今日剩余可兑换额度'));
      loadInfo();
      return;
    }
    const amount = convertAmount;
    Modal.confirm({
      title: t('确认兑换'),
      content: t(
        '将 {{coins}} 个游戏币兑换为 {{quota}} 额度，兑换后不可撤回。',
        { coins: amount, quota: renderQuota(amount * quotaPerCoin) },
      ),
      onOk: () => {
        submitConvert(amount);
      },
    });
  };

  const submitWithdraw = async (address, amount) => {
    setWithdrawing(true);
    try {
      // 首次请求会被要求安全验证，全局拦截器弹出验证后自动带上证明重试。
      const res = await API.post('/api/game/withdraw', { address, amount });
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      showSuccess(t('提现已提交，链上确认后到账'));
      await refreshAfterChange();
    } catch (error) {
      // 取消安全验证不经过全局拦截器，需要在这里提示。
      if (!error?.isAxiosError) {
        showError(error);
      }
    } finally {
      setWithdrawing(false);
    }
  };

  const handleWithdraw = () => {
    const address = withdrawAddress.trim();
    if (!ADDRESS_PATTERN.test(address)) {
      showError(t('请输入 0x 开头的 42 位钱包地址'));
      return;
    }
    if (
      !Number.isInteger(withdrawAmount) ||
      withdrawAmount < withdraw.min_amount ||
      withdrawAmount > withdraw.max_amount
    ) {
      showError(
        t('单次提现数量需在 {{min}} 到 {{max}} 之间', {
          min: withdraw.min_amount,
          max: withdraw.max_amount,
        }),
      );
      return;
    }
    if (withdrawAmount > balance) {
      showError(t('游戏币余额不足'));
      return;
    }
    const amount = withdrawAmount;
    Modal.confirm({
      title: t('确认提现'),
      content: t(
        '将 {{amount}} {{symbol}} 提现到 {{address}}。链上转账无法撤回，请确认地址无误。',
        { amount, symbol, address },
      ),
      onOk: () => {
        submitWithdraw(address, amount);
      },
    });
  };

  const handleUseWalletAddress = async () => {
    const provider = await findInjectedWallet(t);
    if (!provider) return;
    try {
      const accounts = await provider.request({
        method: 'eth_requestAccounts',
      });
      if (accounts?.[0]) {
        setWithdrawAddress(accounts[0]);
      }
    } catch (error) {
      if (!isEthereumUserRejected(error)) {
        showError(error?.message || t('读取钱包地址失败'));
      }
    }
  };

  const handleWatchToken = async () => {
    const provider = await findInjectedWallet(t);
    if (!provider) return;
    try {
      // wallet_watchAsset 把代币加到钱包当前所在的网络，先切到代币所在的链。
      await provider.request({
        method: 'wallet_switchEthereumChain',
        params: [{ chainId: `0x${Number(token.chain_id).toString(16)}` }],
      });
      await provider.request({
        method: 'wallet_watchAsset',
        params: {
          type: 'ERC20',
          options: {
            address: token.address,
            symbol: token.symbol,
            decimals: token.decimals,
          },
        },
      });
    } catch (error) {
      if (isEthereumUserRejected(error)) return;
      if (error?.code === 4902) {
        showError(
          t('钱包中还没有链 ID 为 {{chainId}} 的网络，请先添加该网络', {
            chainId: token.chain_id,
          }),
        );
        return;
      }
      showError(error?.message || t('添加代币失败'));
    }
  };

  if (unavailable) {
    return (
      <div className='games-page'>
        <div className='games-container'>
          <Empty
            image={<IllustrationConstruction style={ILLUSTRATION_SIZE} />}
            darkModeImage={
              <IllustrationConstructionDark style={ILLUSTRATION_SIZE} />
            }
            title={unavailable}
          />
        </div>
      </div>
    );
  }

  return (
    <div className='games-page'>
      <div className='games-glow' aria-hidden />
      <div className='games-container'>
        <div className='games-hero'>
          <div className='games-hero-head'>
            <span className='games-hero-icon'>
              <Gamepad2 size={24} />
            </span>
            <h1 className='games-hero-title'>{t('游戏中心')}</h1>
          </div>
          <p className='games-hero-sub'>
            {t(
              '玩游戏赚取游戏币。游戏币可以兑换为账户额度，也可以提现到链上钱包。',
            )}
          </p>
        </div>

        <Spin spinning={loading}>
          <div className='games-wallet'>
            <div className='games-card games-balance'>
              <div className='games-card-head'>
                <span className='games-card-icon tone-amber'>
                  <Coins size={18} />
                </span>
                <span className='games-card-title'>{t('游戏币余额')}</span>
              </div>
              <div className='games-balance-value'>
                {balance}
                <span className='games-balance-symbol'>{symbol}</span>
              </div>
              <div className='games-balance-stats'>
                <div>
                  <span>{t('累计获得')}</span>
                  <strong>{account?.total_earned || 0}</strong>
                </div>
                <div>
                  <span>{t('已兑换')}</span>
                  <strong>{account?.total_converted || 0}</strong>
                </div>
                <div>
                  <span>{t('已提现')}</span>
                  <strong>{account?.total_withdrawn || 0}</strong>
                </div>
                {showWagerNet && (
                  <div>
                    <span>{t('游戏输赢')}</span>
                    <strong>{signed(wagerNet)}</strong>
                  </div>
                )}
              </div>
              {free?.daily_limit > 0 && (
                <div className='games-free-limit'>
                  {t(
                    '今日技巧与人机奖励 {{earned}} / {{limit}} {{symbol}}，同一 IP 下的账号共用，不含大转盘',
                    { earned: freeEarned, limit: free.daily_limit, symbol },
                  )}
                </div>
              )}
            </div>

            {!closed && (
              <>
                <div className='games-card'>
                  <div className='games-card-head'>
                    <span className='games-card-icon tone-primary'>
                      <ArrowRightLeft size={18} />
                    </span>
                    <span className='games-card-title'>{t('兑换为额度')}</span>
                  </div>
                  <Text type='tertiary' size='small'>
                    {t('1 个游戏币 = {{quota}} 额度，兑换后不可撤回', {
                      quota: renderQuota(quotaPerCoin),
                    })}
                  </Text>
                  <Text type='tertiary' size='small'>
                    {t(
                      '个人今日剩余可兑换 {{remaining}} / {{limit}} 美元额度',
                      {
                        remaining: convertRemaining,
                        limit: convert?.daily_limit || 0,
                      },
                    )}
                  </Text>
                  <InputNumber
                    className='games-field'
                    value={convertAmount}
                    min={maxConvertAmount > 0 ? 1 : 0}
                    max={maxConvertAmount}
                    disabled={maxConvertAmount < 1}
                    precision={0}
                    suffix={symbol}
                    onChange={(value) =>
                      setConvertAmount(typeof value === 'number' ? value : 0)
                    }
                  />
                  <div className='games-preview'>
                    {t('可获得额度')}
                    <strong>
                      {renderQuota((convertAmount || 0) * quotaPerCoin)}
                    </strong>
                  </div>
                  <Button
                    theme='solid'
                    block
                    loading={converting}
                    disabled={maxConvertAmount < 1}
                    onClick={handleConvert}
                  >
                    {t('开始兑换')}
                  </Button>
                </div>

                <div className='games-card'>
                  <div className='games-card-head'>
                    <span className='games-card-icon tone-green'>
                      <Send size={18} />
                    </span>
                    <span className='games-card-title'>{t('提现到链上')}</span>
                  </div>
                  {withdraw?.enabled ? (
                    <>
                      <Input
                        className='games-field'
                        value={withdrawAddress}
                        placeholder={t('收款钱包地址，0x 开头')}
                        onChange={setWithdrawAddress}
                        suffix={
                          <Button
                            theme='borderless'
                            size='small'
                            icon={<Wallet size={14} />}
                            onClick={handleUseWalletAddress}
                          >
                            {t('使用钱包地址')}
                          </Button>
                        }
                      />
                      <InputNumber
                        className='games-field'
                        value={withdrawAmount}
                        min={withdraw.min_amount}
                        max={withdraw.max_amount}
                        precision={0}
                        suffix={symbol}
                        onChange={(value) =>
                          setWithdrawAmount(
                            typeof value === 'number' ? value : 0,
                          )
                        }
                      />
                      <ul className='games-rules'>
                        <li>
                          {t('单次提现 {{min}} – {{max}} {{symbol}}', {
                            min: withdraw.min_amount,
                            max: withdraw.max_amount,
                            symbol,
                          })}
                        </li>
                        <li>
                          {t(
                            '今日全站剩余提现额度 {{remaining}} / {{limit}} {{symbol}}',
                            {
                              remaining: withdraw.daily_remaining,
                              limit: withdraw.daily_limit,
                              symbol,
                            },
                          )}
                        </li>
                        <li>
                          {t(
                            '全站提现额度由所有用户共用，与个人每日兑换额度独立',
                          )}
                        </li>
                        <li>{t('不收取手续费，链上 gas 由平台承担')}</li>
                      </ul>
                      <Button
                        theme='solid'
                        type='primary'
                        block
                        loading={withdrawing}
                        disabled={balance < 1 || withdraw.daily_remaining < 1}
                        onClick={handleWithdraw}
                      >
                        {t('提现')}
                      </Button>
                    </>
                  ) : (
                    <Banner
                      type='warning'
                      closeIcon={null}
                      description={t('链上提现暂未开放')}
                    />
                  )}
                  {token?.address && (
                    <div className='games-token'>
                      <div className='games-token-row'>
                        <span>{t('代币合约')}</span>
                        <Text
                          copyable={{ content: token.address }}
                          ellipsis={{ showTooltip: true }}
                          style={{ maxWidth: 200 }}
                        >
                          {token.address}
                        </Text>
                      </div>
                      <div className='games-token-row'>
                        <span>{t('链 ID')}</span>
                        <span>{token.chain_id}</span>
                      </div>
                      <div className='games-token-row'>
                        <span>{t('精度')}</span>
                        <span>{token.decimals}</span>
                      </div>
                      <div className='games-token-actions'>
                        <Button
                          size='small'
                          icon={<Wallet size={14} />}
                          onClick={handleWatchToken}
                        >
                          {t('添加到钱包')}
                        </Button>
                        {token.explorer_url && (
                          <a
                            href={`${token.explorer_url}/token/${token.address}`}
                            target='_blank'
                            rel='noopener noreferrer'
                          >
                            {t('在区块浏览器查看')}
                            <ExternalLink size={12} />
                          </a>
                        )}
                      </div>
                    </div>
                  )}
                </div>
              </>
            )}
          </div>
        </Spin>

        <section className='games-section'>
          <div className='games-section-header'>
            <span className='games-card-icon tone-violet'>
              <Gamepad2 size={18} />
            </span>
            <div className='games-section-title'>{t('游戏列表')}</div>
          </div>
          <div className='games-section-body'>
            {closed && (
              <Banner
                type='warning'
                closeIcon={null}
                className='games-closed-banner'
                description={t(
                  '游戏中心已关闭，不能再开新局。你还有押着游戏币、没结束的对局，请回去把它玩完',
                )}
              />
            )}
            {games.length > 0 ? (
              <>
                {kinds.length > 1 && (
                  <div className='games-kind-filter'>
                    {['all', ...kinds].map((kind) => (
                      <button
                        key={kind}
                        type='button'
                        className={
                          kind === kindFilter
                            ? 'game-chip is-active'
                            : 'game-chip'
                        }
                        onClick={() => setKindFilter(kind)}
                      >
                        {kind === 'all' ? t('全部') : kindMap[kind].name}
                      </button>
                    ))}
                  </div>
                )}
                {kindFilter !== 'all' && (
                  <div className='games-kind-desc'>
                    {kindMap[kindFilter].description}
                  </div>
                )}
                <div className='games-list'>
                  {shownGames.map((game) => {
                    const meta = gameMetaMap[game.key];
                    const Icon = GAME_ICONS[game.key] || Gamepad2;
                    return (
                      <button
                        type='button'
                        key={game.key}
                        className='games-list-card'
                        onClick={() => navigate(`/games/${game.key}`)}
                      >
                        <span className={`games-list-cover cover-${game.key}`}>
                          <Icon size={28} />
                        </span>
                        <span className='games-list-body'>
                          <span className='games-list-head'>
                            <span className='games-list-title'>
                              {meta.name}
                            </span>
                            <span
                              className={`games-kind-tag kind-${game.kind}`}
                            >
                              {kindMap[game.kind]?.name}
                            </span>
                          </span>
                          <span className='games-list-desc'>
                            {meta.description}
                          </span>
                          <span className='games-list-meta'>
                            {gameProgress(game, t, symbol).map((line) => (
                              <span key={line}>{line}</span>
                            ))}
                          </span>
                        </span>
                      </button>
                    );
                  })}
                </div>
              </>
            ) : (
              <Empty
                image={<IllustrationConstruction style={ILLUSTRATION_SIZE} />}
                darkModeImage={
                  <IllustrationConstructionDark style={ILLUSTRATION_SIZE} />
                }
                title={t('游戏即将上线')}
                description={t('上线后可以在这里玩游戏赚取游戏币，敬请期待')}
              />
            )}
          </div>
        </section>

        {/* 等账户信息加载成功再拉记录：游戏中心关闭时记录接口同样会失败，避免重复报错。 */}
        {info && !closed && (
          <GameCoinRecords
            version={recordsVersion}
            symbol={symbol}
            explorerUrl={token?.explorer_url}
            chainId={token?.chain_id}
          />
        )}
      </div>
    </div>
  );
};

export default Games;
