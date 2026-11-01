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

import { formatAmount } from '../../sect/parts';
import { getKeyText, getNodeText } from './text';
import {
  getBeastStageNames,
  getBeastText,
  getElixirText,
  getGradeNames,
  getRoomText,
} from '../cave/text';

// 历练之外的事(通过节点、登上问心阶、突破、参悟)写成几句话，显示在台面上方。
export function heroEventLines(events, t, text, sectText) {
  const nodes = getNodeText(t);
  const keys = getKeyText(t);
  const rooms = getRoomText(t);
  const elixirs = getElixirText(t, {}, 0);
  const grades = getGradeNames(t);
  const beasts = getBeastText(t);
  const stages = getBeastStageNames(t);
  const lines = [];
  const gains = [];
  for (const ev of events || []) {
    switch (ev.k) {
      case 'hero_exp':
        gains.push(t('修为 +{{n}}', { n: formatAmount(ev.n) }));
        break;
      case 'res':
        gains.push(
          `${sectText.resources[ev.s]} ${ev.n > 0 ? '+' : '-'}${formatAmount(Math.abs(ev.n))}`,
        );
        break;
      case 'pill':
        gains.push(
          `${sectText.pills[ev.s]?.name} ${ev.n > 0 ? '+' : ''}${ev.n}`,
        );
        break;
      case 'rare':
        gains.push(`${sectText.rares[ev.s]} ${ev.n > 0 ? '+' : ''}${ev.n}`);
        break;
      case 'hero_refine':
        lines.push(
          t('祭炼成功：{{name}} +{{n}}', {
            name: text.relics[ev.s]?.name,
            n: ev.n,
          }),
        );
        break;
      case 'hero_refine_fail':
        lines.push(
          t('祭炼未成：{{name}}，下一次成功率 +{{n}}%', {
            name: text.relics[ev.s]?.name,
            n: ev.n,
          }),
        );
        break;
      case 'hero_brew':
        lines.push(t('{{name}}开炉了', { name: elixirs[ev.s]?.name }));
        break;
      case 'hero_stoke':
        lines.push(
          t('调过了火，{{name}}的品质会更好', { name: elixirs[ev.s]?.name }),
        );
        break;
      case 'hero_elixir':
        lines.push(
          t('丹成：{{grade}}{{name}} × {{n}}', {
            grade: grades[ev.v || 0],
            name: elixirs[ev.s]?.name,
            n: ev.n,
          }),
        );
        break;
      case 'hero_brew_fail':
        lines.push(
          t('武火太旺，{{name}}炸炉了，材料化作一缕青烟', {
            name: elixirs[ev.s]?.name,
          }),
        );
        break;
      case 'hero_take':
        lines.push(
          t('服下{{grade}}{{name}}', {
            grade: grades[ev.v || 0],
            name: elixirs[ev.s]?.name,
          }),
        );
        break;
      case 'hero_temper':
        gains.push(t('最大生命 +{{n}}', { n: ev.n }));
        break;
      case 'hero_buff':
        gains.push(t('药力持续 {{n}} 场', { n: ev.n }));
        break;
      case 'hero_golden':
        lines.push(
          t('金丹九转，开战力量永久 +1（已服 {{n}} 颗）', { n: ev.n }),
        );
        break;
      case 'hero_beast_egg':
        lines.push(t('引来了一颗灵兽蛋，过几个小时孵化'));
        break;
      case 'hero_beast_hatch':
        lines.push(
          t('灵兽蛋孵化了：是一只{{name}}', { name: beasts[ev.s]?.name }),
        );
        break;
      case 'hero_beast_level':
        lines.push(
          t('{{name}}长到了 {{n}} 级', { name: beasts[ev.s]?.name, n: ev.n }),
        );
        break;
      case 'hero_beast_fed':
        gains.push(t('灵兽经验 +{{n}}', { n: ev.n }));
        break;
      case 'hero_beast_play':
        lines.push(
          t('和{{name}}玩了一会儿，亲密 {{n}}', {
            name: beasts[ev.s]?.name,
            n: ev.n,
          }),
        );
        break;
      case 'hero_beast_evolve':
        lines.push(
          t('{{name}}进化成了{{stage}}', {
            name: beasts[ev.s]?.name,
            stage: stages[ev.n],
          }),
        );
        break;
      case 'hero_beast_release':
        lines.push(t('{{name}}回到了山林', { name: beasts[ev.s]?.name }));
        break;
      case 'hero_layer':
        lines.push(t('修为精进，连破 {{n}} 层', { n: ev.n }));
        break;
      case 'hero_bottleneck':
        lines.push(t('已修到第九层圆满，可以尝试突破'));
        break;
      case 'hero_relic':
        lines.push(t('获得法宝：{{name}}', { name: text.relics[ev.s]?.name }));
        break;
      case 'hero_potion':
        lines.push(t('获得丹符：{{name}}', { name: text.potions[ev.s]?.name }));
        break;
      case 'hero_key':
        lines.push(t('获得机缘：{{name}}', { name: keys[ev.s]?.name }));
        break;
      case 'hero_card':
        lines.push(t('收下功法：{{name}}', { name: text.cards[ev.s] }));
        break;
      case 'hero_upgrade':
        lines.push(t('参悟：{{name}}', { name: `${text.cards[ev.s]}+` }));
        break;
      case 'hero_realm':
        lines.push(
          t('突破成功，晋入{{realm}}', { realm: sectText.realms[ev.n] }),
        );
        break;
      case 'hero_break_fail':
        lines.push(t('突破失败，损失了一部分修为，机缘还在'));
        break;
      case 'hero_clear':
        if (nodes[ev.s]) lines.unshift(nodes[ev.s].name);
        break;
      case 'hero_step':
        lines.unshift(t('登上问心阶第 {{n}} 级', { n: ev.n }));
        break;
      case 'hero_room':
        lines.push(
          ev.n === 1
            ? t('洞府开辟了{{name}}', { name: rooms[ev.s]?.name })
            : t('{{name}}扩建到 {{n}} 级', {
                name: rooms[ev.s]?.name,
                n: ev.n,
              }),
        );
        break;
      case 'hero_steps_open':
        lines.push(t('宗主令背面亮起一道新的纹路：山门后的问心阶开放了'));
        break;
      default:
    }
  }
  return { lines, gains };
}
