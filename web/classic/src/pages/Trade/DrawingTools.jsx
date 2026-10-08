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

/*
画线工具栏与属性条移植自 WhatIfIBought(https://github.com/mamawai/wtfibought)的
wiib-web/src/components/chart/DrawToolPicker.tsx 与 DrawOverlay.tsx，按 MIT 许可证使用：

MIT License

Copyright (c) 2026 mamawai

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

import React, { useEffect, useId, useRef, useState } from 'react';
import {
  ArrowRightFromLine,
  ArrowUpFromDot,
  ChevronDown,
  Copy,
  Equal,
  Eye,
  EyeOff,
  Magnet,
  Minus,
  MousePointer2,
  MoveUpRight,
  Pencil,
  RectangleHorizontal,
  Ruler,
  Slash,
  Trash2,
  TrendingDown,
  TrendingUp,
  Type,
  Undo2,
} from 'lucide-react';
import { DRAW_PALETTE, LINE_WIDTHS, STYLE_CAPS } from './drawings';

// 画线工具栏(电脑上是图表左侧的竖栏，手机上是工具栏里的一个按钮)、文字输入框与选中图形的属性条。d 是 useDrawings 的返回值。

const ICON = 15;
// 工具清单：名字与提示是中文原文，渲染时翻译；斐波那契没有合适的图标，用缩写当图标。
const TOOLS = [
  {
    kind: null,
    icon: <MousePointer2 size={ICON} />,
    name: '选择',
    tip: '选择或拖动画线(Esc 取消选中，Delete 删除)',
  },
  {
    kind: 'trend',
    icon: <Slash size={ICON} />,
    name: '趋势线',
    tip: '趋势线：点两下定两端',
  },
  {
    kind: 'ray',
    icon: <ArrowUpFromDot size={ICON} className='rotate-45' />,
    name: '射线',
    tip: '射线：先点起点再点方向，沿方向延伸到图边',
  },
  {
    kind: 'hray',
    icon: <ArrowRightFromLine size={ICON} />,
    name: '水平射线',
    tip: '水平射线：点一下，从这根 K 线往右延伸',
  },
  {
    kind: 'arrow',
    icon: <MoveUpRight size={ICON} />,
    name: '箭头',
    tip: '箭头：点两下，从起点指向终点',
  },
  {
    kind: 'hline',
    icon: <Minus size={ICON} />,
    name: '水平线',
    tip: '水平线：点一下',
  },
  {
    kind: 'vline',
    icon: <Minus size={ICON} className='rotate-90' />,
    name: '垂直线',
    tip: '垂直线：点一下',
  },
  {
    kind: 'channel',
    icon: <Equal size={ICON} className='-rotate-45' />,
    name: '平行通道',
    tip: '平行通道：点三下(基线两端与平行线经过的点)',
  },
  {
    kind: 'rect',
    icon: <RectangleHorizontal size={ICON} />,
    name: '矩形',
    tip: '矩形：点两下定对角',
  },
  {
    kind: 'fib',
    icon: <span className='trade-draw-text-icon'>FIB</span>,
    name: '斐波那契回撤',
    tip: '斐波那契回撤：先点波段起点(1)再点终点(0)，各档是从终点回撤的比例',
  },
  {
    kind: 'fibext',
    icon: <span className='trade-draw-text-icon'>EXT</span>,
    name: '斐波那契扩展',
    tip: '斐波那契扩展：点三下(趋势起点、趋势终点、回撤落点)，看 1.618、2.618 等目标位',
  },
  {
    kind: 'long',
    icon: <TrendingUp size={ICON} />,
    name: '多头仓位',
    tip: '多头仓位：先点入场再点止损，止盈按 2:1 生成，可以拖动',
    tone: 'trade-up',
  },
  {
    kind: 'short',
    icon: <TrendingDown size={ICON} />,
    name: '空头仓位',
    tip: '空头仓位：先点入场再点止损，止盈按 2:1 生成，可以拖动',
    tone: 'trade-down',
  },
  {
    kind: 'range',
    icon: <Ruler size={ICON} />,
    name: '价格区间',
    tip: '价格区间：点两下，量出价差、涨跌幅、K 线根数与时长',
  },
  {
    kind: 'text',
    icon: <Type size={ICON} />,
    name: '文字',
    tip: '文字标注：点一下再输入',
  },
];
const toolOf = (kind) => TOOLS.find((item) => item.kind === kind) || TOOLS[0];

// 竖栏上的分组(同 TradingView)：一组一个按钮，显示这组上次用的工具，点了直接选它，右下角的小三角展开整组。
const GROUPS = [
  ['trend', 'ray', 'hray', 'arrow', 'hline', 'vline'],
  ['channel', 'rect'],
  ['fib', 'fibext'],
  ['long', 'short'],
  ['range'],
  ['text'],
];

// 浮层打开时，点外部或按 Esc 收起；Esc 把焦点放回展开按钮。
function useClickOutside(ref, onOutside, active) {
  useEffect(() => {
    if (!active) return undefined;
    const onDown = (e) => {
      if (ref.current && !ref.current.contains(e.target)) onOutside();
    };
    const onKeyDown = (e) => {
      if (e.key !== 'Escape') return;
      e.preventDefault();
      e.stopPropagation();
      ref.current?.querySelector('button[aria-expanded="true"]')?.focus();
      onOutside();
    };
    document.addEventListener('pointerdown', onDown, true);
    document.addEventListener('keydown', onKeyDown, true);
    return () => {
      document.removeEventListener('pointerdown', onDown, true);
      document.removeEventListener('keydown', onKeyDown, true);
    };
  }, [ref, onOutside, active]);
}

const cx = (...names) => names.filter(Boolean).join(' ');

// DrawActions 是磁吸、显示隐藏、撤销与删除四个按钮，竖栏与手机弹层共用。
const DrawActions = ({ d, buttonClass, t }) => {
  const magnetTitle = d.magnet
    ? t('磁吸已开：端点自动贴到最近的开高低收')
    : t('磁吸已关：自由落点');
  const visibilityTitle = d.hiddenAll
    ? t('画线已隐藏，点一下恢复显示')
    : t('隐藏全部画线(不删除)');
  const trashTitle = d.selection
    ? t('删除选中(Delete)')
    : t('清空这个交易对的全部画线');
  return (
    <>
      <button
        type='button'
        className={cx(buttonClass, d.magnet && 'trade-draw-btn-on')}
        title={magnetTitle}
        aria-label={magnetTitle}
        aria-pressed={d.magnet}
        onClick={() => d.setMagnet(!d.magnet)}
      >
        <Magnet size={ICON} />
      </button>
      <button
        type='button'
        className={cx(buttonClass, d.hiddenAll && 'trade-draw-btn-on')}
        title={visibilityTitle}
        aria-label={visibilityTitle}
        aria-pressed={d.hiddenAll}
        disabled={!d.count}
        onClick={() => d.setHiddenAll(!d.hiddenAll)}
      >
        {d.hiddenAll ? <EyeOff size={ICON} /> : <Eye size={ICON} />}
      </button>
      <button
        type='button'
        className={buttonClass}
        title={t('撤销(Ctrl+Z)')}
        aria-label={t('撤销(Ctrl+Z)')}
        disabled={!d.canUndo}
        onClick={d.undo}
      >
        <Undo2 size={ICON} />
      </button>
      <button
        type='button'
        className={buttonClass}
        title={trashTitle}
        aria-label={trashTitle}
        disabled={!d.selection && !d.count}
        onClick={d.trash}
      >
        <Trash2 size={ICON} />
      </button>
    </>
  );
};

// DrawToolRail 是电脑上图表左侧的竖栏：选择、6 组工具(组内折叠)、分隔线、磁吸、隐藏、撤销与删除。
export const DrawToolRail = ({ d, t }) => {
  // 每组上次选的工具，不存盘，进页面时各组回到第一个。
  const [last, setLast] = useState({});
  const [openGroup, setOpenGroup] = useState(null);
  const groupId = useId();
  const flyoutRef = useRef(null);
  const closeFlyout = useRef(() => setOpenGroup(null)).current;
  useClickOutside(flyoutRef, closeFlyout, openGroup !== null);
  const pickIn = (group, kind) => {
    flyoutRef.current?.querySelector('.trade-draw-btn')?.focus();
    setLast((previous) => ({ ...previous, [group]: kind }));
    d.setTool(kind);
    setOpenGroup(null);
  };
  const pick = TOOLS[0];

  return (
    <div className='trade-draw-rail' role='group' aria-label={t('画线工具')}>
      <button
        type='button'
        className={cx('trade-draw-btn', d.tool === null && 'trade-draw-btn-on')}
        title={t(pick.tip)}
        aria-label={t(pick.name)}
        aria-pressed={d.tool === null}
        onClick={() => {
          d.setTool(null);
          setOpenGroup(null);
        }}
      >
        {pick.icon}
      </button>
      {GROUPS.map((group, gi) => {
        const inGroup = d.tool !== null && group.includes(d.tool);
        const current = toolOf(inGroup ? d.tool : (last[gi] ?? group[0]));
        return (
          <div
            key={gi}
            ref={openGroup === gi ? flyoutRef : undefined}
            className='relative'
          >
            <button
              type='button'
              className={cx(
                'trade-draw-btn',
                inGroup ? 'trade-draw-btn-on' : current.tone,
              )}
              title={t(current.tip)}
              aria-label={t(current.name)}
              aria-pressed={inGroup}
              onClick={() => pickIn(gi, current.kind)}
            >
              {current.icon}
            </button>
            {group.length > 1 && (
              <button
                type='button'
                className='trade-draw-more'
                title={t('展开这组工具')}
                aria-label={t('展开这组工具')}
                aria-expanded={openGroup === gi}
                aria-controls={
                  openGroup === gi ? `${groupId}-${gi}` : undefined
                }
                onClick={() => setOpenGroup(openGroup === gi ? null : gi)}
              />
            )}
            {openGroup === gi && (
              <div
                id={`${groupId}-${gi}`}
                className='trade-draw-flyout'
                role='group'
                aria-label={t('画线工具')}
              >
                {group.map((kind) => {
                  const item = toolOf(kind);
                  return (
                    <button
                      key={kind}
                      type='button'
                      className={cx(
                        'trade-draw-item',
                        d.tool === kind ? 'trade-draw-btn-on' : item.tone,
                      )}
                      title={t(item.tip)}
                      aria-label={t(item.name)}
                      aria-pressed={d.tool === kind}
                      onClick={() => pickIn(gi, kind)}
                    >
                      <span className='trade-draw-item-icon'>{item.icon}</span>
                      {t(item.name)}
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        );
      })}
      <hr className='trade-draw-divider' />
      <DrawActions d={d} buttonClass='trade-draw-btn' t={t} />
    </div>
  );
};

// DrawToolPopover 是手机上工具栏里的一个按钮：显示当前工具，点开是三列工具与一排磁吸、隐藏、撤销、删除。一排十几个图标
// 在窄屏上既挤又认不出，所以弹层里带名字。
export const DrawToolPopover = ({ d, t }) => {
  const [open, setOpen] = useState(false);
  const popoverId = useId();
  const wrapRef = useRef(null);
  const close = useRef(() => setOpen(false)).current;
  useClickOutside(wrapRef, close, open);
  const current = toolOf(d.tool);

  return (
    <div ref={wrapRef} className='relative'>
      <button
        type='button'
        className={cx(
          'trade-draw-trigger',
          d.tool !== null && 'trade-draw-btn-on',
        )}
        title={t(current.tip)}
        aria-label={t('画线工具')}
        aria-expanded={open}
        aria-controls={open ? popoverId : undefined}
        onClick={() => setOpen((value) => !value)}
      >
        {current.icon}
        <span>{t(current.name)}</span>
        <ChevronDown size={12} />
      </button>
      {open && (
        <div
          id={popoverId}
          className='trade-draw-sheet'
          role='group'
          aria-label={t('画线工具')}
        >
          <div className='grid grid-cols-3 gap-1'>
            {TOOLS.map((item) => (
              <button
                key={item.kind ?? 'pick'}
                type='button'
                className={cx(
                  'trade-draw-cell',
                  d.tool === item.kind ? 'trade-draw-btn-on' : item.tone,
                )}
                title={t(item.tip)}
                aria-label={t(item.name)}
                aria-pressed={d.tool === item.kind}
                onClick={() => {
                  wrapRef.current
                    ?.querySelector('.trade-draw-trigger')
                    ?.focus();
                  d.setTool(item.kind);
                  setOpen(false);
                }}
              >
                {item.icon}
                <span>{t(item.name)}</span>
              </button>
            ))}
          </div>
          <div className='trade-draw-sheet-actions'>
            <DrawActions d={d} buttonClass='trade-draw-square' t={t} />
          </div>
        </div>
      )}
    </div>
  );
};

// LineSample 是线宽、线型的样例：一截用边框画出粗细与虚实的横线。
const LineSample = ({ width, dash }) => (
  <span
    className='block w-4'
    style={{ borderTop: `${width}px ${dash} currentColor` }}
  />
);

// PropsBar 是选中图形的属性条：颜色、线宽、线型、改字、复制与删除，压在图表底部居中(时间轴上方)。颜色、线宽、线型各自点开
// 一个往上弹的小面板，换选中别的图形时自动收起。
const PropsBar = ({ d, t }) => {
  const selection = d.selection;
  const caps = STYLE_CAPS[selection.kind];
  // 面板记在选中图形的名下：换了选中，id 对不上就等于收起。
  const [open, setOpen] = useState(null);
  const which = open?.id === selection.id ? open.which : null;
  const wrapRef = useRef(null);
  const close = useRef(() => setOpen(null)).current;
  useClickOutside(wrapRef, close, which !== null);
  const toggle = (name) =>
    setOpen(which === name ? null : { id: selection.id, which: name });
  const applyStyle = (patch) => {
    wrapRef.current?.querySelector('button[aria-expanded="true"]')?.focus();
    d.setStyle(patch);
    setOpen(null);
  };

  return (
    <div ref={wrapRef} className='trade-draw-props'>
      {caps.color && (
        <div className='relative'>
          <button
            type='button'
            className={cx(
              'trade-draw-prop',
              which === 'color' && 'trade-draw-btn-on',
            )}
            title={t('颜色')}
            aria-label={t('颜色')}
            aria-expanded={which === 'color'}
            onClick={() => toggle('color')}
          >
            <span
              className='h-3.5 w-3.5 rounded-full'
              style={{ background: selection.color }}
            />
          </button>
          {which === 'color' && (
            <div className='trade-draw-pop grid grid-cols-4 gap-1'>
              {DRAW_PALETTE.map((color) => (
                <button
                  key={color}
                  type='button'
                  aria-label={color}
                  aria-pressed={color === selection.color}
                  className={cx(
                    'trade-draw-swatch',
                    color === selection.color && 'trade-draw-swatch-on',
                  )}
                  onClick={() => applyStyle({ color })}
                >
                  <span style={{ background: color }} />
                </button>
              ))}
            </div>
          )}
        </div>
      )}
      {caps.line && (
        <>
          <div className='relative'>
            <button
              type='button'
              className={cx(
                'trade-draw-prop',
                which === 'width' && 'trade-draw-btn-on',
              )}
              title={t('线宽')}
              aria-label={t('线宽')}
              aria-expanded={which === 'width'}
              onClick={() => toggle('width')}
            >
              <LineSample width={selection.width} dash='solid' />
            </button>
            {which === 'width' && (
              <div className='trade-draw-pop flex flex-col'>
                {LINE_WIDTHS.map((width) => (
                  <button
                    key={width}
                    type='button'
                    aria-pressed={width === selection.width}
                    className={cx(
                      'trade-draw-prop trade-draw-prop-wide',
                      width === selection.width && 'trade-draw-btn-on',
                    )}
                    onClick={() => applyStyle({ width })}
                  >
                    <LineSample width={width} dash='solid' />
                    <span className='trade-num text-[10px] font-semibold'>
                      {width}px
                    </span>
                  </button>
                ))}
              </div>
            )}
          </div>
          <div className='relative'>
            <button
              type='button'
              className={cx(
                'trade-draw-prop',
                which === 'dash' && 'trade-draw-btn-on',
              )}
              title={t('线型')}
              aria-label={t('线型')}
              aria-expanded={which === 'dash'}
              onClick={() => toggle('dash')}
            >
              <LineSample width={2} dash={selection.dash} />
            </button>
            {which === 'dash' && (
              <div className='trade-draw-pop flex flex-col'>
                {[
                  ['solid', t('实线')],
                  ['dashed', t('虚线')],
                  ['dotted', t('点线')],
                ].map(([dash, label]) => (
                  <button
                    key={dash}
                    type='button'
                    className={cx(
                      'trade-draw-prop',
                      dash === selection.dash && 'trade-draw-btn-on',
                    )}
                    title={label}
                    aria-label={label}
                    aria-pressed={dash === selection.dash}
                    onClick={() => applyStyle({ dash })}
                  >
                    <LineSample width={2} dash={dash} />
                  </button>
                ))}
              </div>
            )}
          </div>
        </>
      )}
      {selection.kind === 'text' && (
        <button
          type='button'
          className='trade-draw-prop'
          title={t('改文字(电脑上可以双击)')}
          aria-label={t('改文字(电脑上可以双击)')}
          onClick={d.editText}
        >
          <Pencil size={ICON} />
        </button>
      )}
      <button
        type='button'
        className='trade-draw-prop'
        title={t('复制一份')}
        aria-label={t('复制一份')}
        onClick={d.cloneSelected}
      >
        <Copy size={ICON} />
      </button>
      <button
        type='button'
        className='trade-draw-prop trade-draw-prop-danger'
        title={t('删除(Delete)')}
        aria-label={t('删除(Delete)')}
        onClick={d.deleteSelected}
      >
        <Trash2 size={ICON} />
      </button>
    </div>
  );
};

// DrawOverlay 是盖在图上的文字输入框与选中图形的属性条，放在图表宿主的旁边：指针事件到不了画线的监听，点属性条不会误触图表。
export const DrawOverlay = ({ d, t }) => (
  <>
    {/* 文字直接浮在图上，所见即所得，只留一条虚线下划线；key 带位置，改另一条标注时输入框重新挂载，原文才会换。 */}
    {d.textEdit && (
      <input
        key={`${d.textEdit.x},${d.textEdit.y}`}
        autoFocus
        className='trade-draw-input'
        placeholder={t('标注文字，回车确认')}
        aria-label={t('文字标注')}
        enterKeyHint='done'
        defaultValue={d.textEdit.value}
        style={{
          left: `clamp(0px, ${d.textEdit.x}px, calc(100% - 180px))`,
          top: `clamp(0px, ${d.textEdit.y - 12}px, calc(100% - 28px))`,
        }}
        onKeyDown={(e) => {
          // 输入法组字时的回车是上屏，不是提交。
          if (
            e.key === 'Enter' &&
            !e.nativeEvent.isComposing &&
            e.nativeEvent.keyCode !== 229
          ) {
            e.preventDefault();
            e.stopPropagation();
            d.commitText(e.currentTarget.value);
          } else if (e.key === 'Escape') {
            e.preventDefault();
            e.stopPropagation();
            d.cancelText();
          }
        }}
        onBlur={(e) => d.commitText(e.currentTarget.value)}
      />
    )}
    {d.selection && !d.tool && <PropsBar d={d} t={t} />}
  </>
);
