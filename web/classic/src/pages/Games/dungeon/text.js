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

// 深渊远征的文案：标识与 pkg/dungeon 一致。卡牌的效果文字由服务端给出的效果列表拼出来，数值与规则引擎永远一致。
// 文案写成字面量 t() 调用，i18n 工具才能找到。

export const CLASSES = ['warrior', 'mage', 'ranger'];

export const getClassText = (t) => ({
  warrior: {
    name: t('剑士'),
    desc: t('生命厚实，擅长护甲与力量，正面硬碰硬'),
  },
  mage: { name: t('术士'), desc: t('用灼烧持续削弱敌人，后期爆发惊人') },
  ranger: {
    name: t('游侠'),
    desc: t('大量零费飞刀与抽牌，闪避攻击灵活作战'),
  },
});

export const getCardNames = (t) => ({
  w_strike: t('斩击'),
  w_defend: t('防御'),
  w_heavy: t('重劈'),
  w_sweep: t('横扫'),
  w_bash: t('盾击'),
  w_warcry: t('战吼'),
  w_twin: t('双斩'),
  w_stance: t('坚守'),
  w_rend: t('破甲'),
  w_fury: t('怒火'),
  w_ironbody: t('钢铁之躯'),
  w_riposte: t('反击架势'),
  w_bloodblade: t('鲜血之刃'),
  w_quake: t('震地'),
  w_execute: t('处决'),
  w_unyielding: t('不屈'),
  w_giant: t('巨人之力'),
  w_whirlwind: t('旋风斩'),
  m_bolt: t('魔弹'),
  m_ward: t('护盾术'),
  m_ignite: t('点燃'),
  m_meditate: t('冥想'),
  m_fireball: t('火球'),
  m_flamewave: t('烈焰波'),
  m_iceshard: t('冰锥'),
  m_arcane: t('奥术护甲'),
  m_kindle: t('引燃'),
  m_surge: t('法力涌动'),
  m_detonate: t('爆燃'),
  m_emberheart: t('余烬之心'),
  m_firewall: t('火墙'),
  m_meteor: t('陨石'),
  m_frostnova: t('寒霜新星'),
  m_siphon: t('法力虹吸'),
  m_wellspring: t('奥术涌泉'),
  m_consume: t('焚尽'),
  m_timewarp: t('时间扭曲'),
  r_shoot: t('射击'),
  r_evade: t('闪身'),
  r_knives: t('投掷飞刀'),
  r_snare: t('绊索'),
  r_volley: t('连射'),
  r_quick: t('速射'),
  r_roll: t('翻滚'),
  r_bladestorm: t('刀雨'),
  r_aimed: t('瞄准射击'),
  r_scatter: t('散射'),
  r_bladedance: t('刀舞'),
  r_rhythm: t('致命节奏'),
  r_smoke: t('烟雾弹'),
  r_prepare: t('准备'),
  r_pierce: t('贯穿箭'),
  r_mark: t('猎人标记'),
  r_keen: t('锋锐'),
  r_phantom: t('幻影'),
  r_arrowrain: t('箭雨'),
  n_firstaid: t('急救'),
  n_allout: t('全力一击'),
  n_brace: t('预备'),
  n_inspire: t('激励'),
  n_tactics: t('战术'),
  n_flash: t('闪光弹'),
  knife: t('飞刀'),
  doubt: t('疑虑'),
  wound: t('伤口'),
});

export const getStatusNames = (t) => ({
  str: t('力量'),
  weak: t('虚弱'),
  vuln: t('易伤'),
  burn: t('灼烧'),
  thorns: t('荆棘'),
  dodge: t('闪避'),
  rage: t('狂怒'),
  plate: t('铸甲'),
  embers: t('余烬'),
  blades: t('刀舞'),
  keen: t('锋锐'),
  fortify: t('铁壁'),
  wellspring: t('涌泉'),
});

// 能力牌的效果与对应状态的说明是同一句话。
const powerText = (status, n, t) => {
  switch (status) {
    case 'str':
      return t('攻击伤害 +{{n}}', { n });
    case 'rage':
      return t('每回合结束获得 {{n}} 点力量', { n });
    case 'plate':
      return t('每回合结束获得 {{n}} 点护甲', { n });
    case 'embers':
      return t('每回合结束给予所有敌人 {{n}} 层灼烧', { n });
    case 'blades':
      return t('每回合开始将 {{n}} 张飞刀加入手牌', { n });
    case 'keen':
      return t('飞刀伤害 +{{n}}', { n });
    case 'fortify':
      return t('护甲不再在回合开始时清除');
    case 'wellspring':
      return t('每回合开始额外获得 {{n}} 点能量', { n });
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
      return t('回合开始时受到 {{n}} 点伤害（无视护甲），然后减少 1 层', {
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
      return t('获得 {{n}} 点护甲', { n });
    case 'draw':
      return t('抽 {{n}} 张牌', { n });
    case 'energy':
      return t('获得 {{n}} 点能量', { n });
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
      return t('造成等同于护甲值的伤害');
    case 'double_burn':
      return t('使目标的灼烧层数翻倍');
    case 'burst':
      return t('造成目标灼烧层数 {{n}} 倍的伤害，并清除灼烧', { n });
    case 'dmg_per_card':
      return t('本回合每打出过一张牌，造成 {{n}} 点伤害', { n });
    default:
      return '';
  }
}

export const getRelicText = (t) => ({
  warrior_heart: {
    name: t('战士之心'),
    desc: t('战斗开始获得 1 点力量，战斗结束回复 6 点生命'),
  },
  ember_staff: {
    name: t('余烬法杖'),
    desc: t('战斗开始给予所有敌人 2 层灼烧'),
  },
  quiver: { name: t('箭袋'), desc: t('战斗开始将 2 张飞刀加入手牌') },
  iron_plate: { name: t('铁甲片'), desc: t('战斗开始获得 8 点护甲') },
  whetstone: { name: t('磨刀石'), desc: t('战斗开始获得 1 点力量') },
  lucky_coin: { name: t('幸运金币'), desc: t('战斗获得的金币增加 25%') },
  herb_pouch: { name: t('草药包'), desc: t('战斗结束回复 3 点生命') },
  spiked_mail: { name: t('刺甲'), desc: t('战斗开始获得 3 层荆棘') },
  potion_belt: { name: t('药剂腰带'), desc: t('药水栏增加 1 格') },
  jade: { name: t('翡翠'), desc: t('拾取时最大生命 +10') },
  scroll: { name: t('先手卷轴'), desc: t('每场战斗第一回合多抽 2 张牌') },
  crystal: {
    name: t('能量水晶'),
    desc: t('每场战斗第一回合额外获得 1 点能量'),
  },
  hawk_eye: { name: t('鹰眼'), desc: t('战斗开始给予所有敌人 1 层易伤') },
  metronome: { name: t('节拍器'), desc: t('每回合打出第 3 张牌时抽 1 张牌') },
  vampire_fang: { name: t('吸血獠牙'), desc: t('每击败一个敌人回复 4 点生命') },
  eternal_flame: {
    name: t('永燃火种'),
    desc: t('每回合开始给予所有敌人 1 层灼烧'),
  },
  guard_rune: {
    name: t('守护符文'),
    desc: t('回合结束时如果没有护甲，获得 6 点护甲'),
  },
  phoenix: {
    name: t('不死鸟之羽'),
    desc: t('第一次倒下时以 30% 最大生命复活'),
  },
  sage_tome: { name: t('贤者之书'), desc: t('每回合多抽 1 张牌') },
  crown: {
    name: t('王冠'),
    desc: t('每回合额外获得 1 点能量，但卡牌奖励只剩 2 张可选'),
  },
});

export const getPotionText = (t) => ({
  heal: {
    name: t('治疗药水'),
    desc: t('回复 25% 最大生命，战斗外也能喝'),
  },
  fire: { name: t('火焰瓶'), desc: t('对一个敌人造成 20 点伤害') },
  iron: { name: t('铁皮药剂'), desc: t('获得 12 点护甲') },
  strength: { name: t('力量药剂'), desc: t('获得 2 点力量') },
  swift: { name: t('迅捷药剂'), desc: t('抽 3 张牌') },
  energy: { name: t('能量药剂'), desc: t('获得 2 点能量') },
  weakness: { name: t('虚弱药剂'), desc: t('给予所有敌人 3 层虚弱') },
});

export const getEnemyNames = (t) => ({
  slime: t('腐化史莱姆'),
  goblin: t('地精斥候'),
  skeleton: t('骷髅兵'),
  moth: t('毒粉蛾'),
  goblin_chief: t('地精头目'),
  gargoyle: t('石像鬼'),
  slime_king: t('史莱姆之王'),
  werewolf: t('狼人'),
  shade_mage: t('暗影法师'),
  lava_elemental: t('熔岩元素'),
  beetle: t('刺甲虫'),
  troll: t('双头巨魔'),
  death_knight: t('死亡骑士'),
  flame_lord: t('炎魔'),
  abyss_guard: t('深渊守卫'),
  void_walker: t('虚空行者'),
  soul_eater: t('噬魂者'),
  crystal_golem: t('水晶傀儡'),
  abyss_drake: t('深渊之龙'),
  lich: t('巫妖'),
  abyss_lord: t('深渊之主'),
  dummy: t('训练木桩'),
});

// 事件的标题、描述与两个选项。选项里的 {{n}} 是服务端给出的数字。
export const getEventText = (t) => ({
  altar: {
    title: t('古老祭坛'),
    text: t('一座布满裂纹的祭坛渴求鲜血，台面上摆着一件发光的器物。'),
    choices: [
      (n) => t('献上鲜血：失去 {{n}} 点生命，获得一件遗物', { n }),
      () => t('离开'),
    ],
  },
  spring: {
    title: t('治愈之泉'),
    text: t('清澈的泉水泛着微光，似乎能洗去伤痛，也能洗去杂念。'),
    choices: [
      (n) => t('饮用：回复 {{n}} 点生命', { n }),
      () => t('净化：从牌组移除一张牌'),
    ],
  },
  merchant: {
    title: t('迷途商人'),
    text: t('一位迷路的商人愿意低价出售一张珍藏的卡牌。'),
    choices: [
      (n) => t('购买：花 {{n}} 金币获得一张随机稀有牌', { n }),
      () => t('离开'),
    ],
  },
  dojo: {
    title: t('训练场'),
    text: t('废弃的训练场里还留着木桩和沙袋。'),
    choices: [
      (n) => t('刻苦训练：随机升级 {{n}} 张牌', { n }),
      (n) => t('锻炼体魄：最大生命 +{{n}}', { n }),
    ],
  },
  chest: {
    title: t('诅咒宝箱'),
    text: t('宝箱上缠着黑色的锁链，打开它必然要付出代价。'),
    choices: [() => t('打开：获得一件遗物和一张疑虑'), () => t('离开')],
  },
  gambler: {
    title: t('赌徒'),
    text: t('一名赌徒向你发起挑战：抛一枚硬币，正面你赢。'),
    choices: [
      (n) => t('下注 {{n}} 金币：一半机会赢回双倍', { n }),
      () => t('离开'),
    ],
  },
  mirror: {
    title: t('古镜'),
    text: t('一面古镜映出你牌组里的卡牌，仿佛可以把其中一张带出来。'),
    choices: [() => t('复制：选择一张牌复制一份'), () => t('离开')],
  },
});

export const getTalentText = (t) => ({
  vitality: { name: t('坚韧体魄'), desc: t('每级最大生命 +4') },
  purse: { name: t('行囊'), desc: t('每级初始金币 +25') },
  drill: { name: t('战斗直觉'), desc: t('每级开局随机升级 1 张初始牌') },
  potion: { name: t('药剂师'), desc: t('开局带一瓶随机药水') },
  medic: { name: t('战地急救'), desc: t('营火休息额外回复 10% 最大生命') },
  haggle: { name: t('讨价还价'), desc: t('商店价格降低 10%') },
  relic: { name: t('遗物猎人'), desc: t('开局获得一件随机普通遗物') },
});

export const getNodeNames = (t) => ({
  combat: t('战斗'),
  elite: t('精英'),
  boss: t('首领'),
  shop: t('商店'),
  rest: t('营火'),
  event: t('事件'),
  treasure: t('宝箱'),
});

// 深渊等级逐级叠加的难度，第 i 项是深渊 i+1 新增的一条。
export const getAbyssRules = (t) => [
  t('精英敌人生命 +20%'),
  t('普通敌人伤害 +10%'),
  t('商店价格 +10%'),
  t('营火休息回复降为 25%'),
  t('初始最大生命 -5'),
  t('开局牌组多一张疑虑'),
  t('首领生命 +15%'),
  t('普通敌人生命 +10%'),
  t('击败首领后只回复四分之一已损失的生命'),
  t('所有敌人伤害再 +10%'),
];
