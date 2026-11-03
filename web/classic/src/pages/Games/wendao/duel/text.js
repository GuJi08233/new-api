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

// 世界地图与斗法契约的文案：标识与 pkg/wendao/duel.go 一致，地域对应剧情的五章(剧情设定见 pkg/wendao/story.md)。
// 文案写成字面量 t() 调用，i18n 工具才能找到。

// 地域的标识，顺序与服务端 wendao.Regions 一致：第 i 个地域在主角到第 i 个境界时开放。
export const REGION_IDS = ['qinglan', 'chixia', 'beiming', 'wanyao', 'jiuyou'];

export const getRegionText = (t) => ({
  qinglan: {
    name: t('青岚山'),
    desc: t('东域，青松叠嶂、山岚终年不散，问道宗山门所在，九幽裂隙就压在山下'),
  },
  chixia: {
    name: t('赤霞原'),
    desc: t('南境，赤土平原、火山连绵，天空常年泛红，狼妖与邪修横行'),
  },
  beiming: {
    name: t('北冥寒渊'),
    desc: t('极北冰原上的万丈深渊，渊底不见天日，守渊人说那里是封印的锁'),
  },
  wanyao: {
    name: t('万妖林'),
    desc: t('西荒，古木参天、妖兽成国，向来是人族禁地'),
  },
  jiuyou: {
    name: t('九幽'),
    desc: t('地脉的尽头，死气凝成的倒悬之城，没有日月'),
  },
});

// 契约的难度档：一清剿(两波)、二讨伐(三波加精英)、三斩首(两波加首领)。
export const getTierNames = (t) => [t('清剿'), t('讨伐'), t('斩首')];

export const getDuelKindNames = (t) => ({
  bounty: t('悬赏'),
  task: t('宗门任务'),
  trial: t('入门试炼'),
});
