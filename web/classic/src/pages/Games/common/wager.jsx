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
import { formatNumber, signed } from './api';
import { SideBox } from './SideBox';

// 下注游戏共用的部分。info 是 GET /api/game/<key> 返回的 wagerInfo 部分：max_bet、net_today、plays_today、best_win。

// 派彩达到下注的这么多倍时按大奖展示，与服务端记审计日志的倍数一致。
export const BIG_WIN_MULTIPLIER = 10;

// 一局下注结算之后的提示。result 是结算接口的返回({ bet, win, net })。
export function wagerResultMessage(result, t, symbol) {
  if (!result) return null;
  if (result.win === 0) {
    return { kind: 'loss', text: t('没有中奖，再试一次') };
  }
  if (result.win === result.bet) {
    return {
      kind: '',
      text: t('退回下注 {{win}} {{symbol}}', { win: result.win, symbol }),
    };
  }
  if (result.win < result.bet) {
    return {
      kind: 'loss',
      text: t('收回 {{win}} {{symbol}}', { win: result.win, symbol }),
    };
  }
  return {
    kind: result.win >= BIG_WIN_MULTIPLIER * result.bet ? 'big-win' : 'win',
    text: t('赢得 {{win}} {{symbol}}', { win: result.win, symbol }),
  };
}

// 下注游戏信息栏里的今日输赢与单局最高派彩。
export const WagerStats = ({ info, symbol }) => {
  const { t } = useTranslation();
  return (
    <SideBox
      label={t('今日输赢')}
      value={signed(info?.net_today)}
      unit={symbol}
      note={t('今天玩了 {{plays}} 局 · 单局最高派彩 {{best}}', {
        plays: info?.plays_today || 0,
        best: formatNumber(info?.best_win),
      })}
    />
  );
};
