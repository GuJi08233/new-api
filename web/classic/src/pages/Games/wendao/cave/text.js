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

// 洞府的文案：屋子的名字与说明。标识与 pkg/wendao/cave.go 一致；文案写成字面量 t() 调用，i18n 工具才能找到。

export const getRoomText = (t) => ({
  quiet: {
    name: t('静室'),
    desc: t('闭关打坐的地方，主角闭关更快，与宗门聚灵阵的加成相加'),
  },
  spring: {
    name: t('灵泉'),
    desc: t('引地脉灵泉淬洗肉身，主角的最大生命更多'),
  },
  furnace: {
    name: t('丹室'),
    desc: t('宗主亲自开炉的地方，炼主角自己服的丹，等级越高丹方越多、品质越好'),
  },
  forge: {
    name: t('器室'),
    desc: t('祭炼法宝的地方，法宝最多祭炼到器室的等级'),
  },
  den: {
    name: t('兽苑'),
    desc: t('养灵兽的地方，等级越高能养的灵兽越多，灵兽也能练得越高'),
  },
});

// roomEffect 是一间屋子在 level 级的效果，写成一句短话；0 级是还没建。
export function roomEffect(t, info, room, level) {
  const cave = info.cave;
  if (level <= 0) return t('尚未开辟');
  switch (room) {
    case 'quiet':
      return t('闭关 +{{n}}%', { n: cave.quiet_pct * level });
    case 'spring':
      return t('最大生命 +{{n}}', { n: cave.spring_hp * level });
    case 'furnace':
      return t('丹炉 {{n}} 座 · 品质 +{{s}}', {
        n: info.elixir.slots[level],
        s: info.elixir.furnace_score * level,
      });
    case 'forge':
      return t('法宝最多祭炼到 +{{n}}', { n: level });
    case 'den':
      return t('灵兽 {{n}} 只 · 等级上限 {{cap}}', {
        n: info.beast.slots[level],
        cap: Math.min(
          info.beast.den_level_cap * level,
          info.beast.stage_levels[info.beast.stage_levels.length - 1],
        ),
      });
    default:
      return '';
  }
}

// 法宝的祭炼路数：命加生命，御加开战护体，锋每几级加一点力量。
export const getRefineKindText = (t, rules) => ({
  vital: { name: t('命'), desc: t('每级最大生命 +{{n}}', { n: rules.hp }) },
  guard: {
    name: t('御'),
    desc: t('每级开战护体 +{{n}}', { n: rules.block }),
  },
  edge: {
    name: t('锋'),
    desc: t('每 {{n}} 级开战力量 +1', { n: rules.edge_step }),
  },
});

// refineEffect 是一件法宝祭炼到 level 级给的加成(戴着才生效)。
export function refineEffect(t, rules, kind, level) {
  if (level <= 0) return t('未祭炼');
  switch (kind) {
    case 'guard':
      return t('开战护体 +{{n}}', { n: rules.block * level });
    case 'edge': {
      const n = Math.floor(level / rules.edge_step);
      const left = rules.edge_step - (level % rules.edge_step);
      if (level >= rules.steps.length) return t('开战力量 +{{n}}', { n });
      return t('开战力量 +{{n}}（再祭炼 {{left}} 级 +1）', { n, left });
    }
    default:
      return t('最大生命 +{{n}}', { n: rules.hp * level });
  }
}

// 丹室的丹：名字与说明(按下品、中品、上品的药力)。标识与 pkg/wendao/elixir.go 一致。
export const getElixirText = (t, hero, golden) => ({
  cuiti: {
    name: t('淬体丹'),
    desc: (e) =>
      t(
        '永久增加最大生命：下品 +{{a}}、中品 +{{b}}、上品 +{{c}}；现在的境界最多加到 {{cap}}',
        { a: e[0], b: e[1], c: e[2], cap: hero.temper_cap },
      ),
  },
  jingang: {
    name: t('金刚丹'),
    desc: (e, battles) =>
      t('接下来 {{n}} 场开战护体：下品 +{{a}}、中品 +{{b}}、上品 +{{c}}', {
        n: battles[0],
        a: e[0],
        b: e[1],
        c: e[2],
      }),
  },
  longhu: {
    name: t('龙虎丹'),
    desc: (e, battles) =>
      t('开战力量 +{{n}}，药力管几场：下品 {{a}}、中品 {{b}}、上品 {{c}}', {
        n: e[0],
        a: battles[0],
        b: battles[1],
        c: battles[2],
      }),
  },
  wudao: {
    name: t('悟道丹'),
    desc: (e) =>
      t(
        '马上得到闭关修为：下品 {{a}}、中品 {{b}}、上品 {{c}} 小时；与聚气丹共用冷却',
        { a: e[0], b: e[1], c: e[2] },
      ),
  },
  jiuzhuan: {
    name: t('九转金丹'),
    desc: () =>
      t('永久开战力量 +1，一共只能服 {{n}} 颗；不分品级，没炸炉就是上品', {
        n: golden,
      }),
  },
});

export const getGradeNames = (t) => [t('下品'), t('中品'), t('上品')];

export const getFireText = (t) => ({
  gentle: { name: t('文火'), desc: t('慢一半，品质更好') },
  steady: { name: t('中火'), desc: t('照常') },
  fierce: { name: t('武火'), desc: t('快一半，品质差些，可能炸炉') },
});

// gradeOdds 是品质分 score(不含随机数)出下品、中品、上品的几率(%)：出炉时加一个 0-99 的随机数。
export function gradeOdds(score, rules) {
  const atLeast = (need) => Math.min(Math.max(score + 100 - need, 0), 100);
  const high = atLeast(rules.grade_high);
  const mid = atLeast(rules.grade_mid) - high;
  return [100 - high - mid, mid, high];
}

// 灵兽：种类的名字与招式说明(n 是这只灵兽现在的效果)，阶数，饵料与喂养。标识与 pkg/wendao/beast.go 一致。
export const getBeastText = (t) => ({
  baihu: { name: t('白虎'), element: 0 },
  lu: { name: t('青鹿'), element: 1 },
  gui: { name: t('玄龟'), element: 2 },
  que: { name: t('赤雀'), element: 3 },
  yuan: { name: t('石猿'), element: 4 },
  hu: { name: t('九尾灵狐'), element: -1 },
});

// beastMove 是灵兽出手的说明，力道 power 换算成这一招实际的数目(与 pkg/dungeon/beast.go 一致)。
export function beastMove(t, kind, power) {
  const half = Math.max(Math.floor(power / 2), 1);
  switch (kind) {
    case 'baihu':
      return t('每回合结束扑击一个敌人，造成 {{n}} 点伤害', { n: power });
    case 'lu':
      return t('每回合结束为主角回复 {{n}} 点生命', { n: half });
    case 'gui':
      return t('每回合结束给主角 {{n}} 点护体', { n: power });
    case 'que':
      return t('每回合结束给所有敌人 {{n}} 层真火', {
        n: Math.max(Math.floor(power / 4), 1),
      });
    case 'yuan':
      return t('每回合结束重击一个敌人 {{n}} 点伤害，并让它虚弱', {
        n: half,
      });
    case 'hu':
      return t('每回合结束以狐火灼伤所有敌人，各 {{n}} 点伤害', { n: half });
    default:
      return '';
  }
}

export const getBeastStageNames = (t) => [t('幼兽'), t('成兽'), t('圣兽')];

export const getBaitText = (t) => ({
  plain: { name: t('寻常饵料'), desc: t('五行灵兽机会均等') },
  fruit: { name: t('朱果饵'), desc: t('资质更好，偶尔引来九尾灵狐') },
  lotus: { name: t('七彩莲饵'), desc: t('资质最好，常引来九尾灵狐') },
});

export const getFeedText = (t) => ({
  herb: t('喂灵草'),
  qi: t('喂聚气丹'),
  peiyuan: t('喂培元丹'),
});
