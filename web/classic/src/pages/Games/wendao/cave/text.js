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
