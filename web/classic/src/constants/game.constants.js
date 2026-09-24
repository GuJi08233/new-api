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

// 游戏币流水类型与提现状态的展示映射。文案写成字面量 t() 调用，i18n 提取才能找到。
export const getGameCoinLogTypeMap = (t) => ({
  admin_adjust: { text: t('管理员调整'), color: 'purple' },
  convert: { text: t('额度兑换'), color: 'blue' },
  withdraw: { text: t('链上提现'), color: 'orange' },
  withdraw_refund: { text: t('提现退回'), color: 'green' },
  game_reward: { text: t('游戏奖励'), color: 'amber' },
});

// 已接入的游戏。键与服务端的游戏标识、游戏中心配置里的字段名一致，路由为 /games/{key}。
export const getGameMetaMap = (t) => ({
  tetris: {
    name: t('俄罗斯方块'),
    description: t('经典消行玩法，消的行越多、等级越高，得分越快'),
  },
});

export const getGameCoinWithdrawalStatusMap = (t) => ({
  pending: { text: t('排队中'), color: 'grey' },
  signed: { text: t('处理中'), color: 'blue' },
  sent: { text: t('已广播'), color: 'cyan' },
  confirmed: { text: t('已到账'), color: 'green' },
  failed: { text: t('失败，已退回'), color: 'red' },
});
