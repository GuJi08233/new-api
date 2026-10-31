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

// 历练的文案：人物、章节、节点的名字与剧情、机缘。剧情依据 pkg/wendao/story.md；节点标识与 pkg/wendao/journey.go
// 一致；x、y 是节点在章节底图上的位置(百分比)。文案写成字面量 t() 调用，i18n 工具才能找到。

// 剧情里说话的人：name 是名字，portrait 是 art/Portrait 的人物，enemy 是 art/EnemyArt 的敌人；旁白没有名字。
export const getCast = (t) => ({
  narrator: { name: '' },
  master: { name: t('云渺真人'), portrait: 'master' },
  qinghe: { name: t('苏青禾'), portrait: 'qinghe' },
  linmu: { name: t('林牧'), portrait: 'linmu' },
  laohan: { name: t('老寒'), portrait: 'laohan' },
  mayor: { name: t('镇长'), portrait: 'mayor' },
  chiyan: { name: t('赤炎魔君'), portrait: 'chiyan' },
  taixu: { name: t('太虚真人'), portrait: 'taixu' },
  bone: { name: t('白骨尊者'), enemy: 'lich' },
  slime_king: { name: t('泥沼妖王'), enemy: 'slime_king' },
});

const line = (who, text) => ({ who, text });

// 序章：在选流派之前播放。
export const getPrologue = (t) => [
  line(
    'narrator',
    t(
      '百年前，幽冥教在青岚山下撕开九幽裂隙。正道诸宗合力将它封印，问道宗也因此元气大伤。',
    ),
  ),
  line('master', t('封印……又在松动了。为师撑不了多久了。')),
  line('master', t('宗主令交给你。问道宗不能断在你手里——问道于心，莫忘。')),
  line(
    'narrator',
    t('师尊坐化。山门残破，殿里只剩两名弟子。选一条道途，重振问道宗。'),
  ),
];

// 五章：前三章已开放，后两章在地图上显示为敬请期待。
export const getChapterText = (t) => [
  { name: t('第一章 · 青岚初试'), place: t('青岚山') },
  { name: t('第二章 · 赤霞烽火'), place: t('赤霞原') },
  { name: t('第三章 · 寒渊'), place: t('北冥寒渊') },
  { name: t('第四章 · 万妖'), place: t('万妖林') },
  { name: t('第五章 · 幽冥'), place: t('九幽') },
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

// 节点：name 名字；剧情节点的 lines 是一段对话；其他节点的 desc 是一句说明，intro 是第一次开战前的几句话。
export const getNodeText = (t) => ({
  c1_oath: {
    x: 50,
    y: 88,
    name: t('宗主令'),
    lines: [
      line(
        'narrator',
        t(
          '青岚山，问道宗。百年前这里是东域数一数二的大宗，如今山门只剩半截牌坊。',
        ),
      ),
      line('master', t('封印……又在松动了。为师撑不了多久了。')),
      line('master', t('宗主令交给你。问道宗不能断在你手里——问道于心，莫忘。')),
      line(
        'narrator',
        t(
          '你接过宗主令，对着空荡荡的大殿行了一礼。师尊留下的一点积蓄，就从这里开始。',
        ),
      ),
    ],
  },
  c1_backhill: {
    x: 26,
    y: 78,
    name: t('后山异动'),
    desc: t('后山的灵田被一只青泥怪糟蹋了，先拿它练练手。'),
    intro: [line('narrator', t('泥地里鼓起一个青色的包，冲你吐出一口泥浆。'))],
  },
  c1_town: {
    x: 56,
    y: 68,
    name: t('落枫镇'),
    lines: [
      line('mayor', t('仙师来得正好！林子里的山魈越来越多，夜里还下山抢粮。')),
      line(
        'qinghe',
        t('不止山魈。黑松林的草木一夜之间枯了大片，像是被什么东西吸干了灵气。'),
      ),
      line(
        'qinghe',
        t('我叫苏青禾，在林子里采了十年药，路我熟。宗主要进林，我给你带路。'),
      ),
      line('narrator', t('镇民凑了些灵草相谢。')),
    ],
  },
  c1_imps: {
    x: 40,
    y: 56,
    name: t('山魈作乱'),
    desc: t('两只山魈拦在林道上，龇着牙不肯让路。'),
  },
  c1_herbs: {
    x: 13,
    y: 52,
    name: t('黑松林药圃'),
    desc: t('苏青禾说的野生药圃，隔几个时辰就能再采一轮灵草。'),
  },
  c1_moths: {
    x: 64,
    y: 46,
    name: t('毒蛾林'),
    desc: t('毒蛾精在松林间散播毒粉，草木都枯黄了。'),
  },
  c1_chief: {
    x: 24,
    y: 35,
    name: t('山魈王'),
    desc: t('山魈的首领占了一处山洞，身边堆着抢来的财物。'),
    intro: [line('qinghe', t('小心，它手里那根骨棒……是镇上猎户的。'))],
  },
  c1_bones: {
    x: 80,
    y: 36,
    name: t('乱葬岗'),
    desc: t('林边乱葬岗里的枯骨竟自己爬了起来——有阴气在作祟。'),
  },
  c1_valley: {
    x: 54,
    y: 27,
    name: t('雾隐谷'),
    lines: [
      line('qinghe', t('阴气是从雾隐谷里冒出来的。')),
      line(
        'narrator',
        t(
          '谷口的镇墓石兽睁开了眼。它的石甲上刻着一道崭新的符文，笔画工整，绝不是妖物能刻出来的。',
        ),
      ),
      line('qinghe', t('谁会去唤醒一尊镇墓兽？……谷底有声音，像是泥沼在翻涌。')),
    ],
  },
  c1_gargoyle: {
    x: 32,
    y: 17,
    name: t('镇墓石兽'),
    desc: t('石兽守着谷口，一身石甲刀枪难入。'),
  },
  c1_boss: {
    x: 56,
    y: 9,
    name: t('泥沼妖王'),
    desc: t('谷底的泥沼里盘踞着一头妖王，吞下的阴气就快让它化形了。'),
    intro: [
      line(
        'narrator',
        t('泥沼翻起一张巨口。妖王的眼睛里，有九幽的黑色在流动。'),
      ),
      line('slime_king', t('人……修士……吃了你，本王就能化形！')),
    ],
  },
  c1_rift: {
    x: 82,
    y: 13,
    name: t('九幽裂隙'),
    lines: [
      line(
        'narrator',
        t('妖王倒下，泥沼退去，露出一道漆黑的裂缝——正是百年前封印的九幽裂隙。'),
      ),
      line(
        'narrator',
        t('裂隙里涌出的不是实物，而是一层层幻境。宗门典籍称之为「九幽秘境」。'),
      ),
      line('qinghe', t('宗主，妖王的内丹化成了一滴灵液……这是筑基灵液！')),
      line('qinghe', t('宗门缺个打理药圃的人。若不嫌弃，我跟你上山。')),
      line('narrator', t('突破筑基之后，才能走出青岚山。')),
    ],
  },
  c2_arrive: {
    x: 50,
    y: 88,
    name: t('赤霞原'),
    lines: [
      line('narrator', t('筑基之后，你循着阴气南下，来到终年赤红的赤霞原。')),
      line('linmu', t('喂，那边的！这头狼妖的赏金是我先看上的。')),
      line('linmu', t('……问道宗？没听过。罢了，一起打，赏金对半。我叫林牧。')),
      line(
        'linmu',
        t(
          '听说火云岭上住着一个自称「赤炎魔君」的魔头，岭上的妖兽都是被他逼疯的。',
        ),
      ),
    ],
  },
  c2_wolves: {
    x: 32,
    y: 77,
    name: t('狼妖群'),
    desc: t('原上的狼妖被魔气染红了双眼，见人就扑。'),
  },
  c2_shade: {
    x: 18,
    y: 62,
    name: t('邪修截道'),
    desc: t('一名阴魂修士驱使着铁甲蛊，在路边截杀过往修士。'),
    intro: [line('linmu', t('阴魂修士……幽冥教的人。看来不是巧合。'))],
  },
  c2_lava: {
    x: 60,
    y: 66,
    name: t('熔岩火灵'),
    desc: t('火云岭下的熔岩里生出了火灵。'),
  },
  c2_mine: {
    x: 87,
    y: 60,
    name: t('火云岭铁矿'),
    desc: t('岭下有一条露天的玄铁矿脉，可以隔一阵来采一次。'),
  },
  c2_beetles: {
    x: 22,
    y: 46,
    name: t('铁甲蛊巢'),
    desc: t('蛊巢里的铁甲蛊成群结队，甲壳上还带着倒刺。'),
  },
  c2_troll: {
    x: 46,
    y: 50,
    name: t('火云蛮王'),
    desc: t('火云蛮王是魔君麾下的先锋，一拳能把山石砸碎。'),
  },
  c2_ambush: {
    x: 30,
    y: 32,
    name: t('伏击'),
    desc: t('回程路上，狼妖与邪修设下了埋伏。'),
  },
  c2_knight: {
    x: 54,
    y: 30,
    name: t('尸将'),
    desc: t('魔君洞府前立着一具披甲的尸将，生前也是一位修士。'),
    intro: [line('linmu', t('这身甲……是天剑宗的制式。他们拿正道修士炼尸！'))],
  },
  c2_boss: {
    x: 68,
    y: 18,
    name: t('赤炎魔君'),
    desc: t('魔君坐在熔岩之上，身后的火焰隐隐透出九幽的黑色。'),
    intro: [
      line('chiyan', t('问道宗？哈！云渺那老东西的徒弟？')),
      line('chiyan', t('来得正好，你的魂魄，拿来给本座的炉子添把火！')),
    ],
  },
  c2_epilogue: {
    x: 86,
    y: 30,
    name: t('魔君遗物'),
    lines: [
      line('chiyan', t('咳……封印？可笑……老祖早就醒了！')),
      line(
        'narrator',
        t(
          '魔君化作一地灰烬。洞府里留着一缕不灭的结丹真火，还有一张指向北冥寒渊的残图。',
        ),
      ),
      line(
        'linmu',
        t(
          '残图边上的剑印……是我师门的。十年前师门一夜被灭，凶手到现在都没找到。',
        ),
      ),
      line('linmu', t('北冥寒渊，我跟你走一趟。')),
    ],
  },
  c3_arrive: {
    x: 50,
    y: 88,
    name: t('北冥寒渊'),
    lines: [
      line('narrator', t('北冥寒渊终年冰封，渊底却有阴气源源不断地涌上来。')),
      line('laohan', t('又来送死的？……等等，你腰上挂的是问道宗的宗主令？')),
      line(
        'laohan',
        t('老夫守了这道渊一百年。当年的封印有两处：青岚山是门，北冥是锁。'),
      ),
      line('laohan', t('现在锁上趴着一条幽冥蛟，正一口一口地啃。')),
    ],
  },
  c3_guard: {
    x: 45,
    y: 76,
    name: t('幽冥卫'),
    desc: t('身披黑甲的幽冥卫在冰面上巡逻，它们本不该出现在人间。'),
  },
  c3_shades: {
    x: 22,
    y: 62,
    name: t('冰雾幽影'),
    desc: t('冰雾里有影子在游走，刀剑砍上去像砍进了水里。'),
  },
  c3_eater: {
    x: 74,
    y: 63,
    name: t('噬魂魔'),
    desc: t('噬魂魔吞了好几位守渊人的魂魄。'),
  },
  c3_spring: {
    x: 90,
    y: 46,
    name: t('寒渊灵泉'),
    desc: t('冰层下有一眼温热的灵泉，泉边长满了灵草。'),
  },
  c3_golem: {
    x: 18,
    y: 42,
    name: t('玄晶傀儡'),
    desc: t('冰窟里立着一具玄晶傀儡，像是上古修士留下的守卫。'),
  },
  c3_lich: {
    x: 76,
    y: 34,
    name: t('白骨尊者'),
    desc: t('一位入了魔的老修士，用白骨炼成了不死之身。'),
    intro: [
      line(
        'bone',
        t('太虚观的道法，老夫比你熟。可惜观里容不下一个想要长生的人。'),
      ),
    ],
  },
  c3_patrol: {
    x: 45,
    y: 40,
    name: t('渊底巡逻'),
    desc: t('越往下走，幽冥的爪牙越多。'),
  },
  c3_drake: {
    x: 50,
    y: 16,
    name: t('幽冥蛟'),
    desc: t('幽冥蛟盘在渊底，每一次吐息都让寒渊更冷一分。'),
    intro: [line('laohan', t('就是它！别让它再啃那把锁了！'))],
  },
  c3_epilogue: {
    x: 80,
    y: 12,
    name: t('元婴之机'),
    lines: [
      line('narrator', t('幽冥蛟沉入深渊，冰里绽开了一朵元婴莲。')),
      line(
        'laohan',
        t('锁保住了，可也坏了一半。老夫想不通——当年负责看守北冥的，是太虚观。'),
      ),
      line('linmu', t('太虚观？正道盟主？')),
      line(
        'laohan',
        t(
          '寒渊之下还有路，通往西荒万妖林。那边最近妖祸四起，正道诸宗正在集结……你们自己小心。',
        ),
      ),
      line('narrator', t('第四章 · 万妖，敬请期待。')),
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
