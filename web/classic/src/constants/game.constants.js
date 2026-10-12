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
  game_wager: { text: t('游戏输赢'), color: 'pink' },
  game_pvp: { text: t('对战'), color: 'cyan' },
});

// 游戏的四种结算方式，与服务端 operation_setting.GameKind* 一致，也是游戏中心列表的分类顺序。
export const GAME_KINDS = ['skill', 'pvp', 'wager', 'party'];

export const getGameKindMap = (t) => ({
  skill: {
    name: t('免费赚币'),
    description: t('不花游戏币，成绩达标就有奖励，每个游戏每天有领奖次数'),
  },
  pvp: {
    name: t('对战'),
    description: t('和其他玩家押注对战，也可以和 AI 下棋领奖'),
  },
  wager: {
    name: t('运气'),
    description: t('用游戏币下注，不限次数，返还率公示'),
  },
  party: {
    name: t('多人'),
    description: t('和所有玩家一起玩的奖池'),
  },
});

// 按难度计成绩的游戏，成绩 1、2、3 对应的难度名称；人机对战也用这一套(简单、普通、困难)。扫雷的第三档用「高级难度」这个键
// (中文显示为「高级」)，和渠道设置里表示高级设置的「高级」分开翻译。
export const getGameLevelNames = (t) => ({
  minesweeper: [t('初级'), t('中级'), t('高级难度')],
  sudoku: [t('简单'), t('普通'), t('困难')],
  memory: ['4×4', '4×6', '6×6'],
  puzzle15: ['3×3', '4×4', '5×5'],
  pairlink: ['6×6', '6×8', '8×8'],
  ai: [t('简单'), t('普通'), t('困难')],
});

// 游戏在游戏中心里的排列顺序。不能靠 getGameMetaMap 的键序：像 2048 这样的数字键总会排在最前面。
export const GAME_ORDER = [
  'tetris',
  '2048',
  'snake',
  'flappy',
  'minesweeper',
  'sudoku',
  'memory',
  'puzzle15',
  'pairlink',
  'breakout',
  'sokoban',
  'gomoku',
  'connect4',
  'reversi',
  'xiangqi',
  'rps',
  'landlord',
  'slots',
  'wheel',
  'plinko',
  'dice',
  'roulette',
  'blackjack',
  'mines',
  'hilo',
  'crash',
  'scratch',
  'jackpot',
];

// 玩法分区与结算类型独立：卡牌区也可以包含下注游戏，每个游戏只属于一个分区。
export const GAME_SECTIONS = [
  { key: 'arcade', games: ['tetris', 'snake', 'flappy', 'breakout'] },
  {
    key: 'puzzles',
    games: [
      '2048',
      'minesweeper',
      'sudoku',
      'memory',
      'puzzle15',
      'pairlink',
      'sokoban',
    ],
  },
  { key: 'board', games: ['gomoku', 'connect4', 'reversi', 'xiangqi'] },
  { key: 'cards', games: ['landlord', 'rps', 'blackjack', 'hilo'] },
  {
    key: 'chance',
    games: [
      'slots',
      'wheel',
      'plinko',
      'dice',
      'roulette',
      'mines',
      'crash',
      'scratch',
    ],
  },
  { key: 'jackpot', games: ['jackpot'] },
];

export const getGameSectionMap = (t) => ({
  arcade: {
    name: t('街机挑战'),
    description: t('反应、操作与节奏，挑战自己的最高成绩'),
  },
  puzzles: {
    name: t('益智解谜'),
    description: t('数字、逻辑与记忆，慢慢想出解法'),
  },
  board: { name: t('棋盘对战'), description: t('落子布局，与玩家或 AI 对弈') },
  cards: {
    name: t('卡牌与手势'),
    description: t('叫分、出牌与出拳，和其他玩家或庄家较量'),
  },
  chance: {
    name: t('运气游戏'),
    description: t('选择押注，查看规则与返还率后再开始'),
  },
  jackpot: { name: t('多人奖池'), description: t('一起投入奖池，等待开奖') },
});

// 所有游戏。键与服务端的游戏标识、游戏中心配置里的字段名一致，路由为 /games/{key}，顺序见 GAME_ORDER。
// kind 是结算方式；score 是免费游戏的成绩怎么计：points 为分数类，level 为赢下的难度(名称见 getGameLevelNames)。
export const getGameMetaMap = (t) => ({
  tetris: {
    kind: 'skill',
    name: t('俄罗斯方块'),
    description: t('经典消行玩法，消的行越多、等级越高，得分越快'),
    score: 'points',
    scoreName: t('分数'),
  },
  2048: {
    kind: 'skill',
    name: '2048',
    description: t('滑动合并相同的数字，合出越大的方块得分越高'),
    score: 'points',
    scoreName: t('分数'),
  },
  snake: {
    kind: 'skill',
    name: t('贪吃蛇'),
    description: t('控制小蛇吃豆变长，越吃越快，别撞墙也别咬到自己'),
    score: 'points',
    scoreName: t('吃到的豆子'),
  },
  flappy: {
    kind: 'skill',
    name: t('像素鸟'),
    description: t('点一下扑一次翅膀，穿过一道道管道缝隙'),
    score: 'points',
    scoreName: t('穿过的管道'),
  },
  minesweeper: {
    kind: 'skill',
    name: t('扫雷'),
    description: t('根据数字推理出雷的位置，翻开所有安全格获胜'),
    score: 'level',
    scoreName: t('难度'),
  },
  sudoku: {
    kind: 'skill',
    name: t('数独'),
    description: t('填满九宫格，每行每列每宫 1 到 9 各出现一次'),
    score: 'level',
    scoreName: t('难度'),
  },
  memory: {
    kind: 'skill',
    name: t('记忆翻牌'),
    description: t('翻开两张相同的图案配对，在限定步数内全部配完'),
    score: 'level',
    scoreName: t('难度'),
  },
  puzzle15: {
    kind: 'skill',
    name: t('数字华容道'),
    description: t('滑动方块，把打乱的数字按顺序排好'),
    score: 'level',
    scoreName: t('难度'),
  },
  pairlink: {
    kind: 'skill',
    name: t('连连看'),
    description: t('限时连接相同图案，最多两次转弯，清空棋盘获胜'),
    score: 'level',
    scoreName: t('难度'),
  },
  breakout: {
    kind: 'skill',
    name: t('打砖块'),
    description: t('反弹小球击碎砖块，收集多球与加宽挡板奖励'),
    score: 'points',
    scoreName: t('分数'),
  },
  sokoban: {
    kind: 'skill',
    name: t('每日推箱子'),
    description: t('每天一张可解地图，把所有箱子推到目标位置'),
    score: 'points',
    scoreName: t('完成关卡'),
  },
  gomoku: {
    kind: 'pvp',
    name: t('五子棋'),
    description: t('横竖斜先连成五子者胜，可押注对战或挑战 AI'),
  },
  connect4: {
    kind: 'pvp',
    name: t('四子棋'),
    description: t('棋子落到每列底部，先连成四子者胜'),
  },
  reversi: {
    kind: 'pvp',
    name: t('黑白棋'),
    description: t('夹住对方的棋子翻成自己的颜色，终局子多者胜'),
  },
  xiangqi: {
    kind: 'pvp',
    name: t('中国象棋'),
    description: t('楚河汉界，将死对方的将帅获胜'),
  },
  rps: {
    kind: 'pvp',
    name: t('石头剪刀布'),
    description: t('和其他玩家三局两胜，同时出拳，押注或友谊赛'),
  },
  landlord: {
    kind: 'pvp',
    name: t('斗地主'),
    description: t('三人叫分抢地主，两位农民联手，先出完手牌的一方获胜'),
  },
  slots: {
    kind: 'wager',
    name: t('老虎机'),
    description: t(
      '用游戏币下注转动三个转轮，中线从左起连出相同的 AI 图标就能赢',
    ),
  },
  wheel: {
    kind: 'wager',
    name: t('大转盘'),
    description: t(
      '每天可以免费转，之后花游戏币再转，指针停在哪一格就按那一格派奖',
    ),
  },
  plinko: {
    kind: 'wager',
    name: t('Plinko'),
    description: t(
      '投 1、10 或 100 个游戏币，小球穿过层层节点，落进哪一格就按那一格结算',
    ),
  },
  dice: {
    kind: 'wager',
    name: t('骰子'),
    description: t('选一个倍数，掷出的点数落在区间里就按倍数派彩'),
  },
  roulette: {
    kind: 'wager',
    name: t('轮盘'),
    description: t('欧式单零轮盘，押号码、红黑、单双或区间'),
  },
  blackjack: {
    kind: 'wager',
    name: t('21 点'),
    description: t('要牌、停牌、加倍、分牌，点数比庄家更接近 21 就赢'),
  },
  mines: {
    kind: 'wager',
    name: t('挖宝'),
    description: t('在 25 格里挖宝石，避开地雷，随时收手兑现'),
  },
  hilo: {
    kind: 'wager',
    name: t('高低牌'),
    description: t('猜下一张牌比当前大还是小，连续猜对倍数越滚越高'),
  },
  crash: {
    kind: 'wager',
    name: t('火箭'),
    description: t('倍数随火箭一路上升，在它爆炸之前收手'),
  },
  scratch: {
    kind: 'wager',
    name: t('刮刮乐'),
    description: t('买一张刮开，三个相同的金额就中这个金额'),
  },
  jackpot: {
    kind: 'party',
    name: t('夺宝奖池'),
    description: t('大家往奖池里投币，开奖时按投入比例抽出一位赢家拿走奖池'),
  },
});

// 提现失败的原因：服务端写进提现单与退回流水的固定中文句子(service/game_coin_withdraw.go 的 failGameCoinWithdrawal)，
// 在这里逐条翻译；不认识的原因原样显示。
export const getGameCoinWithdrawFailReasonMap = (t) => ({
  '链上转账执行失败，游戏币已退回': t('链上转账执行失败，游戏币已退回'),
  '代币合约没有完成转账，游戏币已退回': t('代币合约没有完成转账，游戏币已退回'),
  '交易未能上链，游戏币已退回': t('交易未能上链，游戏币已退回'),
  '提现的网络或代币已变更，游戏币已退回':
    t('提现的网络或代币已变更，游戏币已退回'),
  '收款地址无效，游戏币已退回': t('收款地址无效，游戏币已退回'),
  '热钱包代币不足或合约拒绝转账，游戏币已退回': t(
    '热钱包代币不足或合约拒绝转账，游戏币已退回',
  ),
});

export const getGameCoinWithdrawalStatusMap = (t) => ({
  pending: { text: t('排队中'), color: 'grey' },
  signed: { text: t('处理中'), color: 'blue' },
  sent: { text: t('已广播'), color: 'cyan' },
  confirmed: { text: t('已到账'), color: 'green' },
  failed: { text: t('失败，已退回'), color: 'red' },
});
