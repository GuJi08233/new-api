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

// 问道宗门的文案：标识与 pkg/sect 一致。数值(花费、产出、成功率)都由服务端算好，这里只有名字与说明。
// 文案写成字面量 t() 调用，i18n 工具才能找到。

export const RESOURCES = ['stone', 'herb', 'ore', 'rep'];

export const getResourceNames = (t) => ({
  stone: t('灵石'),
  herb: t('灵草'),
  ore: t('玄铁'),
  rep: t('声望'),
});

export const getRealmNames = (t) => [
  t('炼气'),
  t('筑基'),
  t('金丹'),
  t('元婴'),
  t('化神'),
];

export const getTalentNames = (t) => [
  '',
  t('凡品'),
  t('良品'),
  t('上品'),
  t('极品'),
  t('天品'),
];

// 五行灵根：0 金、1 木、2 水、3 火、4 土。
export const getElementNames = (t) => [
  t('金'),
  t('木'),
  t('水'),
  t('火'),
  t('土'),
];

export const getTraitText = (t) => ({
  diligent: { name: t('勤奋'), desc: t('修炼速度 +25%') },
  steady: { name: t('稳重'), desc: t('突破成功率 +10%') },
  brave: { name: t('好战'), desc: t('战力 +10%') },
  clever: { name: t('机敏'), desc: t('帮工效果 +30%，历练收获 +15%') },
  lucky: { name: t('福缘'), desc: t('历练更容易大获全胜') },
});

export const JOBS = ['cultivate', 'field', 'mine', 'array'];

export const getJobNames = (t) => ({
  cultivate: t('闭关修炼'),
  field: t('灵田帮工'),
  mine: t('矿脉帮工'),
  array: t('聚灵阵帮工'),
});

export const getBuildingText = (t) => ({
  hall: {
    name: t('主殿'),
    desc: t(
      '宗门根基：每级每小时产 20 灵石、多收一名弟子，每三级多派一支历练队伍；其他建筑不能高过主殿',
    ),
  },
  field: {
    name: t('灵田'),
    desc: t('每级每小时产 25 灵草，派弟子帮工可以增产'),
  },
  mine: {
    name: t('矿脉'),
    desc: t('每级每小时产 12 玄铁，派弟子帮工可以增产'),
  },
  array: {
    name: t('聚灵阵'),
    desc: t('每级每小时产 30 灵石，并让弟子修炼加快 6%'),
  },
  alchemy: {
    name: t('丹房'),
    desc: t(
      '解锁丹方：1 级回春丹、2 级聚气丹、3 级破境丹、5 级洗髓丹；每级让一炉有 4% 几率多出一颗',
    ),
  },
  library: {
    name: t('藏经阁'),
    desc: t('每级让弟子战力 +6%、历练成功率 +2%'),
  },
});

export const getPillText = (t) => ({
  heal: { name: t('回春丹'), desc: t('治好伤势；大比中服下回复 35% 生命') },
  qi: {
    name: t('聚气丹'),
    desc: t('立刻得到 3 小时闭关的修为，每 6 小时只能服一颗'),
  },
  peiyuan: {
    name: t('培元丹'),
    desc: t('立刻得到 5 小时闭关的修为，与聚气丹共用 6 小时的丹力冷却'),
  },
  break: { name: t('破境丹'), desc: t('突破时服下，成功率 +25%') },
  marrow: { name: t('洗髓丹'), desc: t('资质提升一档') },
});

// 丹药在页面上的顺序，与 pkg/wendao 的 PillKeys 一致。
export const PILLS = ['heal', 'qi', 'peiyuan', 'break', 'marrow'];

// 灵田的灵种。标识与 pkg/wendao 的 Crops 一致。
export const getCropText = (t) => ({
  qingcao: { name: t('青灵草'), desc: t('长得最快的灵草，收成直接进库房') },
  xueshen: { name: t('赤血参'), desc: t('参果能直接入药，一茬两颗回春丹') },
  tieteng: { name: t('玄铁藤'), desc: t('藤蔓里长出玄铁，收成进库房') },
  zhuguo: { name: t('朱果'), desc: t('灵药：丹房用它炼培元丹') },
  lingzhi: { name: t('九叶灵芝'), desc: t('灵药：丹房用它另炼破境丹') },
  lotus: { name: t('七彩莲'), desc: t('灵药：丹房用它另炼洗髓丹') },
});

// 灵药：灵田种出来的稀罕材料。
export const RARES = ['zhuguo', 'lingzhi', 'lotus'];

export const getRareText = (t) => ({
  zhuguo: t('朱果'),
  lingzhi: t('九叶灵芝'),
  lotus: t('七彩莲'),
});

// 商店道具：符箓进背包，丹药买了直接放进丹房。标识与 pkg/sect 的 ShopItems 一致。
export const TALISMANS = ['spirit_talisman', 'recruit_order', 'swift_talisman'];

export const getItemText = (t) => ({
  spirit_talisman: {
    name: t('聚灵符'),
    desc: t('马上得到按现在产量算的 2 小时灵石、灵草与玄铁'),
  },
  recruit_order: {
    name: t('招贤令'),
    desc: t('不花灵石，马上换一批候选弟子'),
  },
  swift_talisman: {
    name: t('神行符'),
    desc: t('在外历练的队伍马上回到宗门，回来后照常领取收获'),
  },
  pill_heal: {
    name: t('回春丹'),
    desc: t('直接放进丹房：治好伤势；大比中服下回复 35% 生命'),
  },
  pill_qi: {
    name: t('聚气丹'),
    desc: t('直接放进丹房：立刻得到 3 小时闭关的修为，每 6 小时只能服一颗'),
  },
  pill_break: {
    name: t('破境丹'),
    desc: t('直接放进丹房：突破时服下，成功率 +25%'),
  },
});

export const getMissionText = (t) => ({
  patrol: { name: t('巡山'), desc: t('在宗门附近巡视，顺手收些灵石灵草') },
  herbs: { name: t('采药'), desc: t('入山采集灵草，木灵根更得心应手') },
  ore: { name: t('探矿'), desc: t('寻找新的矿脉，土灵根更容易发现') },
  bandits: { name: t('剿匪'), desc: t('清剿山下作乱的匪徒，火灵根更占上风') },
  ruins: { name: t('古修洞府'), desc: t('探索前人留下的洞府，或有丹药传承') },
  beasts: { name: t('妖兽山脉'), desc: t('猎杀妖兽取其材料，金灵根更锋利') },
  secret: { name: t('上古秘境'), desc: t('秘境开启，机缘与凶险并存') },
  abyss: { name: t('魔渊'), desc: t('深入魔渊斩妖除魔，收获丰厚') },
  heaven: {
    name: t('天外天'),
    desc: t('传说中的仙境，只有最强的弟子才能活着回来'),
  },
});

// 奇遇：标题、经过与两个选项。insight 的经过里有弟子名字，写成函数。
export const getEventText = (t) => ({
  wanderer: {
    name: t('受伤的散修'),
    desc: t('山门外倒着一名受伤的散修，奄奄一息。'),
    choices: [t('出手救治'), t('婉言拒绝')],
    notes: [t('花一颗回春丹(没有时花 60 灵草)，他可能留下拜师'), ''],
  },
  raid: {
    name: t('魔修来袭'),
    desc: t('一伙魔修趁夜袭扰山门。'),
    choices: [t('派最强的弟子迎战'), t('开启护山大阵')],
    notes: [
      t('赢了才有收获；输了损失一成半灵石，弟子受伤 4 小时'),
      t('花掉一成灵石'),
    ],
  },
  vein: {
    name: t('灵脉异动'),
    desc: t('后山灵脉翻涌，灵气外泄。'),
    choices: [t('加紧开采玄铁'), t('引灵气入聚灵阵')],
    notes: ['', ''],
  },
  treasure: {
    name: t('天材地宝出世'),
    desc: t('百里外宝光冲天，各派都在赶去。'),
    choices: [t('派最强的弟子争夺'), t('不去凑热闹')],
    notes: [
      t('赢了得到破境丹(主殿 5 级起可能是洗髓丹)，输了弟子受伤 4 小时'),
      '',
    ],
  },
  caravan: {
    name: t('商队路过'),
    desc: t('一支行商队伍在山下歇脚。'),
    choices: [t('用灵石换灵草'), t('卖掉一些玄铁')],
    notes: ['', ''],
  },
  insight: {
    name: t('弟子顿悟'),
    desc: (name) => t('{{name}}在修炼中忽有所悟。', { name }),
    choices: [t('让其闭关参悟'), t('请其为同门讲道')],
    notes: [
      t('这名弟子得到 6 小时的修为'),
      t('宗门里每名弟子得到 1.5 小时的修为'),
    ],
  },
  visitor: {
    name: t('名士来访'),
    desc: t('一位云游的名士登门拜访。'),
    choices: [t('盛情款待'), t('以礼相待')],
    notes: ['', ''],
  },
  pest: {
    name: t('灵田虫害'),
    desc: t('灵田里生了噬灵虫，正在啃食灵草。'),
    choices: [t('花灵石请人驱虫'), t('放任不管')],
    notes: ['', t('损失四成灵草')],
  },
  challenge: {
    name: t('他派挑衅'),
    desc: t('邻近门派的弟子上门挑衅，要与本门切磋。'),
    choices: [t('派最强的弟子应战'), t('闭门不出')],
    notes: [t('赢了才有收获；输了声望受损'), t('声望 -3')],
  },
});

export const MOVES = ['strike', 'spell', 'guard', 'meditate', 'pill'];

export const getMoveText = (t) => ({
  strike: { name: t('剑击'), desc: t('造成 1 倍伤害，灵力 +1') },
  spell: {
    name: t('法术'),
    desc: t('消耗 2 灵力，造成 2 倍伤害；打在护体上仍有六成'),
  },
  guard: {
    name: t('护体'),
    desc: t('受到的伤害降到三成；挡住剑击或重击时反震对手，灵力 +1'),
  },
  meditate: { name: t('调息'), desc: t('灵力 +2，回复 6% 生命') },
  pill: { name: t('服丹'), desc: t('服一颗回春丹，回复 35% 生命') },
  charge: { name: t('蓄力'), desc: '' },
  heavy: { name: t('重击'), desc: '' },
});

// 对手亮出来的下一招。
export const getIntentText = (t) => ({
  strike: t('准备剑击：1 倍伤害'),
  spell: t('准备施法：2 倍伤害，护体也只能挡下四成'),
  guard: t('准备护体：伤害降到三成，剑击会被反震'),
  charge: t('正在蓄力：下回合重击，此时受到的伤害 +25%'),
  heavy: t('重击！2.6 倍伤害'),
});

export const getSectNames = (t) => [
  t('青云门'),
  t('天剑宗'),
  t('玄冥谷'),
  t('落霞峰'),
  t('万花谷'),
  t('紫霄宫'),
  t('赤炎派'),
  t('寒冰殿'),
  t('太虚观'),
  t('灵兽山'),
  t('碧水阁'),
  t('金刚寺'),
];

// 弟子名字的字表：[简体, 繁体, 拼音]。下标与 pkg/sect 的 NameSurnames、NameGivens 对应。
const SURNAMES = [
  ['李', '李', 'Li'],
  ['王', '王', 'Wang'],
  ['张', '張', 'Zhang'],
  ['刘', '劉', 'Liu'],
  ['陈', '陳', 'Chen'],
  ['杨', '楊', 'Yang'],
  ['赵', '趙', 'Zhao'],
  ['黄', '黃', 'Huang'],
  ['周', '周', 'Zhou'],
  ['吴', '吳', 'Wu'],
  ['徐', '徐', 'Xu'],
  ['孙', '孫', 'Sun'],
  ['胡', '胡', 'Hu'],
  ['朱', '朱', 'Zhu'],
  ['高', '高', 'Gao'],
  ['林', '林', 'Lin'],
  ['何', '何', 'He'],
  ['郭', '郭', 'Guo'],
  ['马', '馬', 'Ma'],
  ['罗', '羅', 'Luo'],
  ['梁', '梁', 'Liang'],
  ['宋', '宋', 'Song'],
  ['郑', '鄭', 'Zheng'],
  ['谢', '謝', 'Xie'],
  ['韩', '韓', 'Han'],
  ['唐', '唐', 'Tang'],
  ['冯', '馮', 'Feng'],
  ['叶', '葉', 'Ye'],
  ['萧', '蕭', 'Xiao'],
  ['沈', '沈', 'Shen'],
  ['苏', '蘇', 'Su'],
  ['陆', '陸', 'Lu'],
];

const GIVENS = [
  ['云', '雲', 'yun'],
  ['风', '風', 'feng'],
  ['清', '清', 'qing'],
  ['玄', '玄', 'xuan'],
  ['灵', '靈', 'ling'],
  ['青', '青', 'qing'],
  ['墨', '墨', 'mo'],
  ['尘', '塵', 'chen'],
  ['霜', '霜', 'shuang'],
  ['月', '月', 'yue'],
  ['星', '星', 'xing'],
  ['河', '河', 'he'],
  ['岚', '嵐', 'lan'],
  ['羽', '羽', 'yu'],
  ['寒', '寒', 'han'],
  ['若', '若', 'ruo'],
  ['微', '微', 'wei'],
  ['澜', '瀾', 'lan'],
  ['无', '無', 'wu'],
  ['涯', '涯', 'ya'],
  ['长', '長', 'chang'],
  ['歌', '歌', 'ge'],
  ['逸', '逸', 'yi'],
  ['尧', '堯', 'yao'],
  ['舟', '舟', 'zhou'],
  ['远', '遠', 'yuan'],
  ['山', '山', 'shan'],
  ['松', '松', 'song'],
  ['竹', '竹', 'zhu'],
  ['梅', '梅', 'mei'],
  ['雪', '雪', 'xue'],
  ['琴', '琴', 'qin'],
  ['剑', '劍', 'jian'],
  ['心', '心', 'xin'],
  ['明', '明', 'ming'],
  ['华', '華', 'hua'],
  ['宁', '寧', 'ning'],
  ['安', '安', 'an'],
  ['秋', '秋', 'qiu'],
  ['念', '念', 'nian'],
  ['辰', '辰', 'chen'],
  ['瑶', '瑤', 'yao'],
  ['白', '白', 'bai'],
  ['夜', '夜', 'ye'],
  ['离', '離', 'li'],
  ['仙', '仙', 'xian'],
  ['鸿', '鴻', 'hong'],
  ['渊', '淵', 'yuan'],
];

// 按界面语言拼出名字：简体、繁体用汉字，其他语言用拼音(Li Qingyun)。
export function discipleName(name, language) {
  if (!name) return '';
  const [surname, first, second] = name;
  const column = language === 'zh-TW' ? 1 : language?.startsWith('zh') ? 0 : 2;
  const s = SURNAMES[surname]?.[column] || '';
  const g1 = GIVENS[first]?.[column] || '';
  const g2 = second >= 0 ? GIVENS[second]?.[column] || '' : '';
  if (column < 2) return s + g1 + g2;
  return `${s} ${g1.charAt(0).toUpperCase()}${g1.slice(1)}${g2}`;
}
