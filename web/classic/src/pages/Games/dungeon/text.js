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

// 九幽秘境(问道的卡牌肉鸽板块)的文案：标识与 pkg/dungeon 一致，名词对照见 pkg/wendao/world.md。卡牌的效果文字由服务端给出的效果列表拼出来，数值与规则引擎永远一致。
// 文案写成字面量 t() 调用，i18n 工具才能找到。

export const CLASSES = ['warrior', 'mage', 'ranger'];

export const getClassText = (t) => ({
  warrior: {
    name: t('体修'),
    desc: t('横练肉身，擅长护体与力量，正面硬碰硬'),
  },
  mage: { name: t('法修'), desc: t('以真火持续灼烧敌人，后期爆发惊人') },
  ranger: {
    name: t('剑修'),
    desc: t('大量零费飞剑与抽牌，身法灵动，以快打快'),
  },
});

export const getCardNames = (t) => ({
  w_strike: t('崩拳'),
  w_defend: t('金钟罩'),
  w_heavy: t('开山掌'),
  w_sweep: t('横扫千军'),
  w_bash: t('铁山靠'),
  w_warcry: t('狮子吼'),
  w_twin: t('连环腿'),
  w_stance: t('不动如山'),
  w_rend: t('碎骨拳'),
  w_fury: t('血气如虹'),
  w_ironbody: t('铜皮铁骨'),
  w_riposte: t('荆棘护体'),
  w_bloodblade: t('燃血'),
  w_quake: t('震地'),
  w_execute: t('霸王拳'),
  w_unyielding: t('金身不坏'),
  w_giant: t('法天象地'),
  w_whirlwind: t('千手乱拳'),
  m_bolt: t('掌心雷'),
  m_ward: t('护体灵光'),
  m_ignite: t('引火诀'),
  m_meditate: t('吐纳'),
  m_fireball: t('火球术'),
  m_flamewave: t('燎原'),
  m_iceshard: t('玄冰刺'),
  m_arcane: t('水镜术'),
  m_kindle: t('星火'),
  m_surge: t('灵台清明'),
  m_detonate: t('业火'),
  m_emberheart: t('离火心法'),
  m_firewall: t('离火阵'),
  m_meteor: t('天火流星'),
  m_frostnova: t('冰封千里'),
  m_siphon: t('吸灵诀'),
  m_wellspring: t('周天运转'),
  m_consume: t('焚天'),
  m_timewarp: t('光阴诀'),
  r_shoot: t('剑气'),
  r_evade: t('游身'),
  r_knives: t('御剑'),
  r_snare: t('剑网'),
  r_volley: t('三叠剑气'),
  r_quick: t('快剑'),
  r_roll: t('踏雪无痕'),
  r_bladestorm: t('分光化影'),
  r_aimed: t('凝神一剑'),
  r_scatter: t('乱剑'),
  r_bladedance: t('养剑'),
  r_rhythm: t('剑意连绵'),
  r_smoke: t('剑幕'),
  r_prepare: t('凝神'),
  r_pierce: t('剑荡八方'),
  r_mark: t('破绽'),
  r_keen: t('剑心'),
  r_phantom: t('残影'),
  r_arrowrain: t('万剑归宗'),
  n_firstaid: t('疗伤诀'),
  n_allout: t('全力一击'),
  n_brace: t('守一'),
  n_inspire: t('回气'),
  n_tactics: t('灵机'),
  n_flash: t('镇魂符'),
  ally_metal: t('金灵剑'),
  ally_wood: t('回春术'),
  ally_water: t('寒潭护体'),
  ally_fire: t('赤焰符'),
  ally_earth: t('厚土诀'),
  knife: t('飞剑'),
  doubt: t('心魔'),
  wound: t('伤势'),
});

export const getStatusNames = (t) => ({
  str: t('力量'),
  weak: t('虚弱'),
  vuln: t('易伤'),
  burn: t('真火'),
  thorns: t('反震'),
  dodge: t('身法'),
  rage: t('血气'),
  plate: t('铜皮'),
  embers: t('离火'),
  blades: t('养剑'),
  keen: t('剑心'),
  fortify: t('金身'),
  wellspring: t('周天'),
});

// 能力牌的效果与对应状态的说明是同一句话。
const powerText = (status, n, t) => {
  switch (status) {
    case 'str':
      return t('攻击伤害 +{{n}}', { n });
    case 'rage':
      return t('每回合结束获得 {{n}} 点力量', { n });
    case 'plate':
      return t('每回合结束获得 {{n}} 点护体', { n });
    case 'embers':
      return t('每回合结束给予所有敌人 {{n}} 层真火', { n });
    case 'blades':
      return t('每回合开始将 {{n}} 张飞剑加入手牌', { n });
    case 'keen':
      return t('飞剑伤害 +{{n}}', { n });
    case 'fortify':
      return t('护体不再在回合开始时清除');
    case 'wellspring':
      return t('每回合开始额外获得 {{n}} 点灵力', { n });
    default:
      return '';
  }
};

// 状态的说明，悬停在状态标记上时显示。
export function statusDesc(status, n, t) {
  switch (status) {
    case 'weak':
      return t('攻击伤害降低 25%，还剩 {{n}} 回合', { n });
    case 'vuln':
      return t('受到的攻击伤害提高 50%，还剩 {{n}} 回合', { n });
    case 'burn':
      return t('回合开始时受到 {{n}} 点伤害（无视护体），然后减少 1 层', {
        n,
      });
    case 'thorns':
      return t('受到攻击时，对攻击者造成 {{n}} 点伤害', { n });
    case 'dodge':
      return t('完全躲开接下来 {{n}} 次攻击', { n });
    default:
      return powerText(status, n, t);
  }
}

// 一条效果的文字。cards 与 statuses 是牌名、状态名。
export function fxText(fx, t, cards, statuses) {
  const n = fx.n || 0;
  const x = fx.x || 1;
  const status = statuses[fx.s] || fx.s;
  switch (fx.k) {
    case 'dmg':
      return x > 1
        ? t('造成 {{n}} 点伤害 {{x}} 次', { n, x })
        : t('造成 {{n}} 点伤害', { n });
    case 'dmg_all':
      return x > 1
        ? t('对所有敌人造成 {{n}} 点伤害 {{x}} 次', { n, x })
        : t('对所有敌人造成 {{n}} 点伤害', { n });
    case 'dmg_rand':
      return t('随机攻击敌人 {{x}} 次，每次 {{n}} 点伤害', { n, x });
    case 'block':
      return t('获得 {{n}} 点护体', { n });
    case 'draw':
      return t('抽 {{n}} 张牌', { n });
    case 'energy':
      return t('获得 {{n}} 点灵力', { n });
    case 'apply':
      return t('给予 {{n}} 层{{status}}', { n, status });
    case 'apply_all':
      return t('给予所有敌人 {{n}} 层{{status}}', { n, status });
    case 'apply_self':
      return t('获得 {{n}} 层{{status}}', { n, status });
    case 'power':
      return powerText(fx.s, n, t);
    case 'add':
      return t('将 {{n}} 张{{card}}加入手牌', { n, card: cards[fx.s] });
    case 'heal':
      return t('回复 {{n}} 点生命', { n });
    case 'lose_hp':
      return t('失去 {{n}} 点生命', { n });
    case 'dmg_block':
      return t('造成等同于护体值的伤害');
    case 'double_burn':
      return t('使目标的真火层数翻倍');
    case 'burst':
      return t('造成目标真火层数 {{n}} 倍的伤害，并清除真火', { n });
    case 'dmg_per_card':
      return t('本回合每打出过一张牌，造成 {{n}} 点伤害', { n });
    default:
      return '';
  }
}

export const getRelicText = (t) => ({
  warrior_heart: {
    name: t('金刚舍利'),
    desc: t('战斗开始获得 1 点力量，战斗结束回复 6 点生命'),
  },
  ember_staff: {
    name: t('离火葫芦'),
    desc: t('战斗开始给予所有敌人 2 层真火'),
  },
  quiver: { name: t('剑匣'), desc: t('战斗开始将 2 张飞剑加入手牌') },
  iron_plate: { name: t('玄铁护心镜'), desc: t('战斗开始获得 8 点护体') },
  whetstone: { name: t('磨剑石'), desc: t('战斗开始获得 1 点力量') },
  lucky_coin: { name: t('聚宝钱'), desc: t('战斗获得的灵珠增加 25%') },
  herb_pouch: { name: t('药囊'), desc: t('战斗结束回复 3 点生命') },
  spiked_mail: { name: t('荆棘软甲'), desc: t('战斗开始获得 3 层反震') },
  potion_belt: { name: t('百宝囊'), desc: t('丹符栏增加 1 格') },
  jade: { name: t('暖玉'), desc: t('拾取时最大生命 +10') },
  scroll: { name: t('先天卦简'), desc: t('每场战斗第一回合多抽 2 张牌') },
  crystal: {
    name: t('聚灵玉'),
    desc: t('每场战斗第一回合额外获得 1 点灵力'),
  },
  hawk_eye: { name: t('照妖镜'), desc: t('战斗开始给予所有敌人 1 层易伤') },
  metronome: { name: t('清心铃'), desc: t('每回合打出第 3 张牌时抽 1 张牌') },
  vampire_fang: { name: t('噬血珠'), desc: t('每击败一个敌人回复 4 点生命') },
  eternal_flame: {
    name: t('长明灯'),
    desc: t('每回合开始给予所有敌人 1 层真火'),
  },
  guard_rune: {
    name: t('玄龟甲'),
    desc: t('回合结束时如果没有护体，获得 6 点护体'),
  },
  phoenix: {
    name: t('涅槃羽'),
    desc: t('第一次倒下时以 30% 最大生命复活'),
  },
  sage_tome: { name: t('无字天书'), desc: t('每回合多抽 1 张牌') },
  crown: {
    name: t('紫金冠'),
    desc: t('每回合额外获得 1 点灵力，但功法奖励只剩 2 张可选'),
  },
});

export const getPotionText = (t) => ({
  heal: {
    name: t('疗伤丹'),
    desc: t('回复 25% 最大生命，战斗外也能服用'),
  },
  fire: { name: t('火符'), desc: t('对一个敌人造成 20 点伤害') },
  iron: { name: t('金刚符'), desc: t('获得 12 点护体') },
  strength: { name: t('大力丸'), desc: t('获得 2 点力量') },
  swift: { name: t('神行符'), desc: t('抽 3 张牌') },
  energy: { name: t('聚灵丹'), desc: t('获得 2 点灵力') },
  weakness: { name: t('散功符'), desc: t('给予所有敌人 3 层虚弱') },
});

// 商店道具：用游戏币买，在秘境里用掉一件少一件。标识与 pkg/dungeon 的 Item* 一致。
export const getItemText = (t) => ({
  elixir: {
    name: t('续命丹'),
    desc: t('回复 40% 最大生命，战斗内外都能用'),
  },
  potion_crate: {
    name: t('丹符补给'),
    desc: t('随机得到一份丹符，丹符栏要有空位'),
  },
  gold_pouch: { name: t('灵珠袋'), desc: t('获得 100 灵珠') },
  relic_box: { name: t('法宝匣'), desc: t('随机得到一件还没有的法宝') },
  ward: {
    name: t('护命符'),
    desc: t('本轮第一次倒下时以 50% 生命复活，在涅槃羽之后生效；每轮最多一张'),
  },
});

export const getEnemyNames = (t) => ({
  slime: t('青泥怪'),
  goblin: t('山魈'),
  skeleton: t('枯骨'),
  moth: t('毒蛾精'),
  goblin_chief: t('山魈王'),
  gargoyle: t('镇墓石兽'),
  slime_king: t('泥沼妖王'),
  werewolf: t('狼妖'),
  shade_mage: t('阴魂修士'),
  lava_elemental: t('火灵'),
  beetle: t('铁甲蛊'),
  troll: t('火云蛮王'),
  death_knight: t('尸将'),
  flame_lord: t('赤炎魔君'),
  abyss_guard: t('幽冥卫'),
  void_walker: t('幽影'),
  soul_eater: t('噬魂魔'),
  crystal_golem: t('玄晶傀儡'),
  abyss_drake: t('幽冥蛟'),
  lich: t('白骨尊者'),
  abyss_lord: t('幽冥老祖'),
  dummy: t('木人桩'),
});

// 事件的标题、描述与两个选项。选项里的 {{n}} 是服务端给出的数字。
export const getEventText = (t) => ({
  altar: {
    title: t('血祭坛'),
    text: t('一座布满裂纹的祭坛渴求鲜血，台上供着一件流光溢彩的法宝。'),
    choices: [
      (n) => t('献上精血：失去 {{n}} 点生命，获得一件法宝', { n }),
      () => t('离开'),
    ],
  },
  spring: {
    title: t('灵泉'),
    text: t('泉水灵气氤氲，似乎能洗去伤痛，也能涤荡杂念。'),
    choices: [
      (n) => t('饮下灵泉：回复 {{n}} 点生命', { n }),
      () => t('洗心：从牌组移除一张牌'),
    ],
  },
  merchant: {
    title: t('游方散修'),
    text: t('一位迷路的散修愿意低价换出一卷压箱底的功法。'),
    choices: [
      (n) => t('交换：花 {{n}} 灵珠获得一张随机稀有功法', { n }),
      () => t('离开'),
    ],
  },
  dojo: {
    title: t('前辈洞府'),
    text: t('一处荒废的洞府，石壁上还刻着前辈修士的心得。'),
    choices: [
      (n) => t('参悟石刻：随机升级 {{n}} 张牌', { n }),
      (n) => t('淬炼肉身：最大生命 +{{n}}', { n }),
    ],
  },
  chest: {
    title: t('封印宝匣'),
    text: t('宝匣上贴满了褪色的封条，打开它必然要付出代价。'),
    choices: [() => t('揭开封条：获得一件法宝和一张心魔'), () => t('离开')],
  },
  gambler: {
    title: t('赌坊老道'),
    text: t('一个邋遢老道拦住去路：掷一枚铜钱，字面朝上算你赢。'),
    choices: [
      (n) => t('押 {{n}} 灵珠：一半机会赢回双倍', { n }),
      () => t('离开'),
    ],
  },
  mirror: {
    title: t('古镜'),
    text: t('一面古镜映出你的功法，仿佛能把其中一张从镜中取出。'),
    choices: [() => t('临摹：选择一张牌复制一份'), () => t('离开')],
  },
});

export const getTalentText = (t) => ({
  vitality: { name: t('筑体'), desc: t('每级最大生命 +4') },
  purse: { name: t('储物袋'), desc: t('每级初始灵珠 +25') },
  drill: { name: t('温故'), desc: t('每级开局随机升级 1 张初始牌') },
  potion: { name: t('丹符随身'), desc: t('开局带一份随机丹符') },
  medic: { name: t('调息'), desc: t('打坐额外回复 10% 最大生命') },
  haggle: { name: t('人情练达'), desc: t('坊市价格降低 10%') },
  relic: { name: t('福缘'), desc: t('开局获得一件随机普通法宝') },
});

export const getNodeNames = (t) => ({
  combat: t('遭遇'),
  elite: t('强敌'),
  boss: t('首领'),
  shop: t('坊市'),
  rest: t('打坐'),
  event: t('奇遇'),
  treasure: t('宝藏'),
});

// 劫数逐级叠加的难度，第 i 项是第 i+1 劫新增的一条(规则包里叫深渊等级)。
export const getAbyssRules = (t) => [
  t('强敌生命 +20%'),
  t('普通敌人伤害 +10%'),
  t('坊市价格 +10%'),
  t('打坐回复降为 25%'),
  t('初始最大生命 -5'),
  t('开局牌组多一张心魔'),
  t('首领生命 +15%'),
  t('普通敌人生命 +10%'),
  t('击败首领后只回复四分之一已损失的生命'),
  t('所有敌人伤害再 +10%'),
];
