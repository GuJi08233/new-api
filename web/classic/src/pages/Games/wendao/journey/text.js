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

// 历练的文案：章节、节点的名字与剧情、机缘。节点标识与 pkg/wendao/journey.go 一致；位置 x、y 是地图上的百分比。
// 世界设定见 pkg/wendao/world.md。文案写成字面量 t() 调用，i18n 工具才能找到。

export const getPrologue = (t) => [
  t(
    '百年前，幽冥教在青岚山下撕开九幽裂隙，正道各派合力将它封印，问道宗也因此元气大伤。',
  ),
  t('如今师尊坐化，宗主令传到了你手里。山门残破，只剩两名弟子。'),
  t('选一条道途，重振问道宗。'),
];

export const getChapterText = (t) => [
  { name: t('第一章 · 青岚初试'), place: t('青岚山') },
  { name: t('第二章 · 赤霞烽火'), place: t('赤霞原') },
  { name: t('第三章 · 寒渊'), place: t('北冥寒渊') },
];

export const getKeyText = (t) => ({
  foundation_dew: {
    name: t('筑基灵液'),
    desc: t('泥沼妖王的内丹所化，突破筑基要用'),
  },
  core_fire: {
    name: t('结丹真火'),
    desc: t('赤炎魔君留下的真火，结成金丹要用'),
  },
  nascent_lotus: {
    name: t('元婴莲'),
    desc: t('幽冥蛟守着的冰莲，凝结元婴要用'),
  },
});

// 节点：name 名字；lines 是剧情节点的几句话，desc 是其他节点的一句说明。
export const getNodeText = (t) => ({
  c1_oath: {
    x: 50,
    y: 90,
    name: t('宗主令'),
    lines: [
      t('师尊临终前说：「封印……又在松动了。问道宗不能断在你手里。」'),
      t(
        '你接过宗主令，对着空荡荡的大殿行了一礼。师尊留下的一点积蓄，就从这里开始。',
      ),
    ],
  },
  c1_backhill: {
    x: 30,
    y: 80,
    name: t('后山异动'),
    desc: t('后山的灵田被一只青泥怪糟蹋了，先拿它练练手。'),
  },
  c1_town: {
    x: 56,
    y: 68,
    name: t('落枫镇'),
    lines: [
      t('镇长迎上来：「仙师来得正好！林子里的山魈越来越多，夜里还下山抢粮。」'),
      t('镇民凑了些灵草相谢，托你进黑松林看看。'),
    ],
  },
  c1_imps: {
    x: 40,
    y: 56,
    name: t('山魈作乱'),
    desc: t('两只山魈拦在林道上，龇着牙不肯让路。'),
  },
  c1_herbs: {
    x: 14,
    y: 50,
    name: t('黑松林药圃'),
    desc: t('林子深处有一片野生药圃，隔几个时辰就能再采一轮灵草。'),
  },
  c1_moths: {
    x: 62,
    y: 46,
    name: t('毒蛾林'),
    desc: t('毒蛾精在松林间散播毒粉，草木都枯黄了。'),
  },
  c1_chief: {
    x: 24,
    y: 35,
    name: t('山魈王'),
    desc: t('山魈的首领占了一处山洞，身边堆着抢来的财物。'),
  },
  c1_bones: {
    x: 78,
    y: 35,
    name: t('乱葬岗'),
    desc: t('林边乱葬岗里的枯骨竟自己爬了起来——有阴气在作祟。'),
  },
  c1_valley: {
    x: 52,
    y: 26,
    name: t('雾隐谷'),
    lines: [
      t('阴气的源头在雾隐谷。谷口的镇墓石兽不知被谁唤醒了。'),
      t('谷底隐约传来泥沼翻涌的声音。'),
    ],
  },
  c1_gargoyle: {
    x: 32,
    y: 16,
    name: t('镇墓石兽'),
    desc: t('石兽守着谷口，一身石甲刀枪难入。'),
  },
  c1_boss: {
    x: 54,
    y: 8,
    name: t('泥沼妖王'),
    desc: t('谷底的泥沼里盘踞着一头妖王，它吞下的阴气就快让它化形了。'),
  },
  c1_rift: {
    x: 80,
    y: 12,
    name: t('九幽裂隙'),
    lines: [
      t('妖王倒下后泥沼退去，露出一道漆黑的裂缝——正是当年封印的九幽裂隙。'),
      t('裂隙里涌出的不是实物，而是一层层幻境，门中典籍称之为「九幽秘境」。'),
      t('妖王的内丹化作一滴筑基灵液。突破筑基之后，才能走出青岚山。'),
    ],
  },
  c2_arrive: {
    x: 50,
    y: 90,
    name: t('赤霞原'),
    lines: [
      t('筑基之后，你循着阴气南下，来到终年赤红的赤霞原。'),
      t(
        '散修们说，一个自称「赤炎魔君」的魔头在火云岭上开了洞府，岭上的妖兽都暴动了。',
      ),
    ],
  },
  c2_wolves: {
    x: 34,
    y: 78,
    name: t('狼妖群'),
    desc: t('原上的狼妖被魔气染红了双眼，见人就扑。'),
  },
  c2_shade: {
    x: 20,
    y: 63,
    name: t('邪修截道'),
    desc: t('一名阴魂修士驱使着铁甲蛊，在路边截杀过往修士。'),
  },
  c2_lava: {
    x: 60,
    y: 66,
    name: t('熔岩火灵'),
    desc: t('火云岭下的熔岩里生出了火灵。'),
  },
  c2_mine: {
    x: 86,
    y: 58,
    name: t('火云岭铁矿'),
    desc: t('岭下有一条露天的玄铁矿脉，可以隔一阵来采一次。'),
  },
  c2_beetles: {
    x: 24,
    y: 47,
    name: t('铁甲蛊巢'),
    desc: t('蛊巢里的铁甲蛊成群结队，甲壳上还带着倒刺。'),
  },
  c2_troll: {
    x: 66,
    y: 49,
    name: t('火云蛮王'),
    desc: t('火云蛮王是魔君麾下的先锋，一拳能把山石砸碎。'),
  },
  c2_ambush: {
    x: 45,
    y: 35,
    name: t('伏击'),
    desc: t('回程路上，狼妖与邪修设下了埋伏。'),
  },
  c2_knight: {
    x: 56,
    y: 21,
    name: t('尸将'),
    desc: t('魔君洞府前立着一具披甲的尸将，生前也是一位修士。'),
  },
  c2_boss: {
    x: 44,
    y: 8,
    name: t('赤炎魔君'),
    desc: t('魔君坐在熔岩之上，身后的火焰隐隐透出九幽的黑色。'),
  },
  c2_epilogue: {
    x: 76,
    y: 10,
    name: t('魔君遗物'),
    lines: [
      t('魔君临死前狂笑：「封印？幽冥老祖早就醒了！」'),
      t('你在他的洞府里找到一缕不灭的结丹真火，还有一张指向北冥寒渊的残图。'),
    ],
  },
  c3_arrive: {
    x: 50,
    y: 90,
    name: t('北冥寒渊'),
    lines: [
      t('北冥寒渊终年冰封，渊底却有阴气源源不断地涌上来。'),
      t('守渊人说，渊底盘着一条幽冥蛟，是从裂隙里爬出来的。'),
    ],
  },
  c3_guard: {
    x: 45,
    y: 76,
    name: t('幽冥卫'),
    desc: t('身披黑甲的幽冥卫在冰面上巡逻，它们本不该出现在人间。'),
  },
  c3_shades: {
    x: 24,
    y: 62,
    name: t('冰雾幽影'),
    desc: t('冰雾里有影子在游走，刀剑砍上去像砍进了水里。'),
  },
  c3_eater: {
    x: 68,
    y: 63,
    name: t('噬魂魔'),
    desc: t('噬魂魔吞了好几位守渊人的魂魄。'),
  },
  c3_spring: {
    x: 88,
    y: 50,
    name: t('寒渊灵泉'),
    desc: t('冰层下有一眼温热的灵泉，泉边长满了灵草。'),
  },
  c3_golem: {
    x: 24,
    y: 44,
    name: t('玄晶傀儡'),
    desc: t('冰窟里立着一具玄晶傀儡，像是上古修士留下的守卫。'),
  },
  c3_lich: {
    x: 66,
    y: 44,
    name: t('白骨尊者'),
    desc: t('一位入了魔的老修士，用白骨炼成了不死之身。'),
  },
  c3_patrol: {
    x: 45,
    y: 29,
    name: t('渊底巡逻'),
    desc: t('越往下走，幽冥的爪牙越多。'),
  },
  c3_drake: {
    x: 50,
    y: 12,
    name: t('幽冥蛟'),
    desc: t('幽冥蛟盘在渊底，每一次吐息都让寒渊更冷一分。'),
  },
  c3_epilogue: {
    x: 78,
    y: 9,
    name: t('元婴之机'),
    lines: [
      t('幽冥蛟沉入深渊，冰里绽开了一朵元婴莲。'),
      t('寒渊之下还有路，通往传说中的万妖林。下一章敬请期待。'),
    ],
  },
});

export const getNodeKindNames = (t) => ({
  story: t('剧情'),
  combat: t('遭遇'),
  elite: t('强敌'),
  boss: t('首领'),
  gather: t('采集'),
});
