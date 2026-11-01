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
export function roomEffect(t, cave, room, level) {
  if (level <= 0) return t('尚未开辟');
  switch (room) {
    case 'quiet':
      return t('闭关 +{{n}}%', { n: cave.quiet_pct * level });
    case 'spring':
      return t('最大生命 +{{n}}', { n: cave.spring_hp * level });
    case 'furnace':
      return t('丹室 {{n}} 级', { n: level });
    case 'forge':
      return t('法宝最多祭炼到 +{{n}}', { n: level });
    default:
      return '';
  }
}
