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

import { getKeyText, getNodeText } from './text';

// 历练之外的事(通过节点、突破、参悟)写成几句话，显示在台面上方。
export function heroEventLines(events, t, text, sectText) {
  const nodes = getNodeText(t);
  const keys = getKeyText(t);
  const lines = [];
  const gains = [];
  for (const ev of events || []) {
    switch (ev.k) {
      case 'hero_exp':
        gains.push(t('修为 +{{n}}', { n: ev.n }));
        break;
      case 'res':
        gains.push(`${sectText.resources[ev.s]} ${ev.n > 0 ? '+' : ''}${ev.n}`);
        break;
      case 'pill':
        gains.push(
          `${sectText.pills[ev.s]?.name} ${ev.n > 0 ? '+' : ''}${ev.n}`,
        );
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
      default:
    }
  }
  return { lines, gains };
}
