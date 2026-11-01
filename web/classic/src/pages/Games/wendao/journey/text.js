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
  taixu_fallen: { name: t('太虚真人'), portrait: 'taixu_fallen' },
  yaohuang: { name: t('玄狐妖皇'), portrait: 'yaohuang' },
  master_soul: { name: t('云渺真人残魂'), portrait: 'master_soul' },
  patriarch: { name: t('幽冥老祖'), portrait: 'patriarch' },
  judge: { name: t('九幽判官'), enemy: 'judge' },
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

// 五章：每个境界一章，第 c 章对应境界 c。
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
  fox_heart: {
    name: t('天狐灵心'),
    desc: t('玄狐妖皇交出的灵心，凝结化神要用'),
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
      line('narrator', t('凝结元婴之后，才能踏入西荒。')),
    ],
  },
  c4_arrive: {
    x: 50,
    y: 88,
    name: t('万妖林口'),
    lines: [
      line(
        'narrator',
        t(
          '西荒，万妖林。林中妖气冲天，林外却扎起了连绵的营帐，旌旗都是正道诸宗的。',
        ),
      ),
      line(
        'taixu',
        t(
          '诸位同道，万妖林妖祸已成大患。贫道太虚，愿与诸宗共讨此獠，还西荒一个太平。',
        ),
      ),
      line('linmu', t('太虚观主，正道盟主……话说得倒是漂亮。')),
      line(
        'taixu',
        t(
          '这位是问道宗的宗主？云渺道友的高徒，贫道早有耳闻。林中凶险，宗主不如在营中歇息，由诸宗代劳。',
        ),
      ),
      line(
        'qinghe',
        t('宗主，我总觉得不对。你闻，林子里的草木气，是被硬生生催出来的。'),
      ),
      line(
        'narrator',
        t('你没有留下。身后，太虚真人望着你的背影，笑意一分未减。'),
      ),
    ],
  },
  c4_trees: {
    x: 34,
    y: 76,
    name: t('古树成精'),
    desc: t('林边的古树被妖气浸透，生出了灵智，挥着藤条拦住去路。'),
  },
  c4_vipers: {
    x: 16,
    y: 62,
    name: t('蛇窟'),
    desc: t('乱石堆里盘着一窝蛇妖，吐信声连成了一片。'),
  },
  c4_foxes: {
    x: 60,
    y: 66,
    name: t('妖狐卫'),
    desc: t('林道上巡逻的妖狐卫双眼血红，像是被什么东西操控着。'),
    intro: [line('qinghe', t('它们眼里的血丝……不像是天生的凶性。'))],
  },
  c4_grove: {
    x: 86,
    y: 54,
    name: t('灵芝谷'),
    desc: t('山谷里长着成片的灵芝与茯苓，隔几个时辰就能再采一轮。'),
  },
  c4_runes: {
    x: 38,
    y: 50,
    name: t('阵眼'),
    lines: [
      line(
        'narrator',
        t(
          '两处战场之间，一块半埋在泥里的黑石引起了你的注意。石面上刻着一道符文，笔画正隐隐发光。',
        ),
      ),
      line(
        'qinghe',
        t(
          '地脉里的灵气全往这块石头里灌……草木妖兽被灵气撑得发了狂，难怪个个红着眼。',
        ),
      ),
      line('linmu', t('这符文的笔画，宗主，你见过。')),
      line(
        'narrator',
        t('雾隐谷镇墓石兽身上那道崭新的符文，与这里的一模一样。'),
      ),
      line(
        'linmu',
        t(
          '不是妖在作乱，是有人用阵法逼它们发疯。唤醒石兽的，和布下这座大阵的，是同一个人。',
        ),
      ),
    ],
  },
  c4_bear: {
    x: 14,
    y: 40,
    name: t('熊罴'),
    desc: t('一头披着铁锈色长毛的熊罴堵在林道上，每踏一步地面都在发颤。'),
    intro: [line('linmu', t('它爪子上的伤痕，是锁链磨出来的。有人拴过它。'))],
  },
  c4_spider: {
    x: 70,
    y: 36,
    name: t('蛛妖'),
    desc: t('古树之间结满了拇指粗的蛛丝，蛛妖盘踞在网心，等着猎物自己撞上去。'),
  },
  c4_ambush: {
    x: 40,
    y: 26,
    name: t('林间伏击'),
    desc: t('被阵法逼疯的妖兽成群扑来，已经分不清敌我。'),
  },
  c4_boss: {
    x: 54,
    y: 13,
    name: t('玄狐妖皇'),
    desc: t(
      '万妖林之主，九尾玄狐。她被阵法逼得发狂，赤红的眼底却还留着一丝清明。',
    ),
    intro: [
      line('yaohuang', t('又是人族……来得好，本座今日要把你们撕个干净！')),
      line(
        'narrator',
        t(
          '九条狐尾铺开，每一条的尾尖都燃着幽蓝的狐火。她的嘶吼里，竟藏着一丝哀求。',
        ),
      ),
    ],
  },
  c4_epilogue: {
    x: 85,
    y: 14,
    name: t('天狐灵心'),
    lines: [
      line(
        'narrator',
        t(
          '狐火熄灭。你一剑劈开阵眼，林中的红光一盏盏暗下去，发狂的妖兽纷纷倒下，又慢慢醒转。',
        ),
      ),
      line('yaohuang', t('……多谢。百年来，没有一个人族肯听本座说话。')),
      line(
        'yaohuang',
        t(
          '三个月前，一个穿太虚观道袍的人踏进万妖林，布下此阵，说要借万妖之力。我族不从，他便催动大阵，逼我们互相厮杀。',
        ),
      ),
      line(
        'linmu',
        t('太虚观……我师门被灭的那一夜，我在门外看见的，也是一件白色的道袍。'),
      ),
      line(
        'yaohuang',
        t(
          '他还说，等九幽的门开了，正道盟主要亲自下去取一样东西。宗主，小心他。',
        ),
      ),
      line(
        'narrator',
        t(
          '妖皇取出一枚通体莹白的狐心，放在你的掌中。化神所需的天狐灵心，就此到手。',
        ),
      ),
      line('narrator', t('凝结化神之后，才能追上太虚真人的脚步。')),
    ],
  },
  c5_arrive: {
    x: 50,
    y: 88,
    name: t('裂隙洞开'),
    lines: [
      line(
        'narrator',
        t(
          '你赶回青岚山时，天已经黑透了。山下的九幽裂隙洞开，黑气冲天，正道诸宗的旗幡围着裂隙猎猎作响。',
        ),
      ),
      line(
        'taixu_fallen',
        t('幽冥教余孽祸乱人间！今日诸宗同心，踏平九幽，永绝后患！'),
      ),
      line('qinghe', t('宗主，他把封印整个撕开了！')),
      line(
        'linmu',
        t(
          '「踏平九幽」？打开锁的人，才是真想进去的那一个。他要的不是幽冥教，是九幽。',
        ),
      ),
      line(
        'narrator',
        t('裂隙里涌出无数幽冥来物。你与林牧、苏青禾对视一眼，纵身跃入黑暗。'),
      ),
    ],
  },
  c5_hounds: {
    x: 26,
    y: 74,
    name: t('冥犬群'),
    desc: t('裂隙边游荡着成群的冥犬，一闻到活人的气息就扑上来。'),
  },
  c5_ghosts: {
    x: 72,
    y: 72,
    name: t('饿鬼道'),
    desc: t('瘦骨嶙峋的饿鬼拖着长长的影子，见什么都想啃一口。'),
  },
  c5_spring: {
    x: 90,
    y: 56,
    name: t('忘川浅滩'),
    desc: t('浅滩上漂着零落的灵物，隔几个时辰就能再拾一批。'),
  },
  c5_legion: {
    x: 14,
    y: 56,
    name: t('幽冥军阵'),
    desc: t('幽冥卫列阵而行，噬魂魔跟在阵后，专吃倒下的魂魄。'),
  },
  c5_judge: {
    x: 30,
    y: 38,
    name: t('九幽判官'),
    desc: t('倒悬之城的城门下坐着一位判官，生死簿上，写满了活人的名字。'),
    intro: [
      line(
        'judge',
        t('阳寿未尽者，擅闯九幽。先让本官看看，你的名字写在第几页。'),
      ),
    ],
  },
  c5_lord: {
    x: 72,
    y: 46,
    name: t('噬魂魔君'),
    desc: t('噬魂魔里最老的一头，吞下的魂魄足够堆成一座山。'),
    intro: [
      line(
        'linmu',
        t('它身上缠着的碎布……是太虚观的云纹。他把自己人也扔进来了。'),
      ),
    ],
  },
  c5_city: {
    x: 50,
    y: 30,
    name: t('倒悬之城'),
    lines: [
      line(
        'narrator',
        t(
          '穿过城门，一座倒悬的城池悬在头顶——楼阁自天上垂下，脚下是望不到边的死气之海。',
        ),
      ),
      line('narrator', t('城心的高台上，立着一道半透明的人影。')),
      line('master_soul', t('……你来了。')),
      line(
        'master_soul',
        t(
          '为师当年封印裂隙，分了一缕魂魄钉在这座城的门上。这一百年，封印靠它撑着。',
        ),
      ),
      line(
        'master_soul',
        t(
          '太虚要的，是九幽里积了千年的死气。他一旦吞下，沧澜界再没有人制得住他。',
        ),
      ),
      line(
        'master_soul',
        t(
          '宗主令还在你手里。去吧，打败他，也打败幽冥老祖。重铸封印，要你亲手来。',
        ),
      ),
      line(
        'narrator',
        t('残魂化作一缕微光，没入你手中的宗主令。令牌微微发烫。'),
      ),
    ],
  },
  c5_taixu: {
    x: 36,
    y: 18,
    name: t('太虚真人'),
    desc: t('高台之上，正道盟主不再掩饰——九幽死气在他的袍袖间翻涌。'),
    intro: [
      line('taixu_fallen', t('云渺的徒弟……果然跟来了。')),
      line(
        'taixu_fallen',
        t('正道盟主？不过是个好听的名号。贫道要的，是越过化神的那一步。'),
      ),
      line('linmu', t('我师门一百三十七口人。你认不认？')),
      line('taixu_fallen', t('……不过是挡路的尘埃。')),
      line('narrator', t('九幽死气灌入他的经脉，白袍一寸寸染黑。')),
    ],
  },
  c5_boss: {
    x: 58,
    y: 9,
    name: t('幽冥老祖'),
    desc: t('太虚真人倒下时，失控的死气唤醒了沉睡在城心的幽冥老祖。'),
    intro: [
      line(
        'narrator',
        t('太虚真人的残躯被一只黑色巨掌捏碎，化作死气，汇入城心。'),
      ),
      line('patriarch', t('百年了……终于有人替本座把门打开了。')),
      line(
        'patriarch',
        t('云渺的徒弟，带着他的令牌来的？来，让本座看看，你比你师父强在哪里。'),
      ),
    ],
  },
  c5_epilogue: {
    x: 84,
    y: 14,
    name: t('重铸封印'),
    lines: [
      line(
        'narrator',
        t(
          '幽冥老祖的身躯在宗主令的光芒里一寸寸崩解，九幽的死气如潮水般退回裂隙深处。',
        ),
      ),
      line('qinghe', t('宗主，裂隙在合拢！')),
      line('master_soul', t('令牌借为师一用。这一次，不会再让你一个人撑了。')),
      line(
        'narrator',
        t(
          '你把宗主令按在裂隙中央，残魂化作封印的最后一道符文。九幽之门，重新合拢。',
        ),
      ),
      line('linmu', t('太虚观……正道盟主的位子，空出来了。')),
      line(
        'narrator',
        t(
          '数月之后，问道宗重回正道五宗之列。山门前新刻的四个字，在晨光里熠熠生辉：问道于心。',
        ),
      ),
      line(
        'narrator',
        t(
          '宗门大比、论剑与仙盟照常进行。九幽秘境留了下来，成了后辈修行的历练之地。',
        ),
      ),
      line(
        'narrator',
        t(
          '宗主令的背面，亮起一道从未见过的纹路。山门后荒废百年的问心阶，重新为宗主敞开。',
        ),
      ),
    ],
  },
});

// 通关(重铸封印)以后主角的称号，随问心阶登到的级数变；还没通关时没有称号。
export const getHeroTitle = (t, hero) => {
  if (!hero.steps_open) return '';
  if (hero.steps >= 50) return t('问心无愧');
  if (hero.steps >= 30) return t('道心通明');
  if (hero.steps >= 10) return t('道心坚定');
  return t('重铸封印');
};

export const getNodeKindNames = (t) => ({
  story: t('剧情'),
  combat: t('遭遇'),
  elite: t('强敌'),
  boss: t('首领'),
  gather: t('采集'),
});
