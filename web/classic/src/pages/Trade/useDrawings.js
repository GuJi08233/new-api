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
画线交互移植自 WhatIfIBought(https://github.com/mamawai/wtfibought)的 wiib-web/src/components/chart/useDrawings.ts，
按 MIT 许可证使用：

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

import { useCallback, useEffect, useRef, useState } from 'react';
import { Modal } from '@douyinfe/semi-ui';
import { CrosshairMode } from 'lightweight-charts';
import { DrawingLayer } from './DrawingLayer';
import {
  anchorToPoint,
  coordToTime,
  DRAW_COLOR,
  finalizePoints,
  loadDrawings,
  magnetPrice,
  newDrawingId,
  normalizePositionPoints,
  PLACE_POINTS,
  saveDrawings,
} from './drawings';

// 画线的交互：选工具、落点、选中、拖动、删除、撤销与存盘。
//
// 用 pointerdown 而不是 mousedown 接管手势：图表库在自己的画布上监听 mousedown/touchstart，stopPropagation 抢不到；
// 浏览器保证 pointerdown 先于它们触发，在 pointerdown 里关掉图表的平移缩放，图表收到事件时已经不会平移了。手机上再把
// touch-action 设成 none，拖线时页面不跟着滚。
//
// attach 由 CandleChart 在建图的 effect 里调用、在 chart.remove() 之前 detach：另开一个同依赖的 effect 的话，React 会先
// 执行建图 effect 的清理(图表已经删掉)，再执行这里的，那时 detachPrimitive 会出错。
//
// 触屏怎么落点：手指点哪里就挡住哪里，没法精确落点。所以触屏上选了工具后图上出现一个贯穿整图的十字，手指在图上任意位置
// 拖动，十字按相对位移跟着走，原地轻点一下就把交叉点定为当前的点；多点工具定了一个点后十字留在原处接着拖下一个。期间图表
// 自己的平移缩放锁住，图表库的十字线藏起来，免得两个十字打架。

// 按下到抬起移动不超过这些像素算轻点，超过算拖动。
const TAP_SLOP = 6;
// 最多撤销几步。
const UNDO_MAX = 50;
// 复制出来的副本往右下错开多少像素，跟原图叠在一起就看不出复制了。
const CLONE_OFFSET = 24;

// useDrawings 管理一张 K 线图的画线，tool 为 null 是选择模式(可以选中、拖动已有图形，图表照常平移缩放)。
export function useDrawings() {
  const [tool, setTool] = useState(null);
  const [magnet, setMagnet] = useState(true);
  // selection 是选中图形的样式快照，属性条按它显示；图层里的对象是原地改的，直接交给 React 它感知不到变化。
  const [selection, setSelection] = useState(null);
  const [count, setCount] = useState(0);
  // hiddenAll 隐藏全部画线，只切显示不删数据，不存盘。
  const [hiddenAll, setHiddenAll] = useState(false);
  // textEdit 是文字输入框的位置(相对图表宿主)，value 是修改已有标注时的原文；null 表示没在输入。
  const [textEdit, setTextEdit] = useState(null);
  // 撤销栈：每次改动前存一份整套画线的快照，换交易对时清空。
  const historyRef = useRef([]);
  const historyKeyRef = useRef(null);
  // 同一图表因周期/副图重建时保留内存副本，即使浏览器禁用了存储也不会丢线。
  const snapshotRef = useRef(null);
  const [undoCount, setUndoCount] = useState(0);

  const liveRef = useRef(null);
  const detachRef = useRef(null);
  const toolRef = useRef(null);
  const magnetRef = useRef(true);
  const hiddenRef = useRef(false);
  const dragRef = useRef(null);
  // 拖动期间挂在 window 上的监听，要用同一个函数对象才摘得掉。
  const dragHandlersRef = useRef(null);
  // 多点工具已经落定的点(不含跟着指针走的预览点)。
  const placedRef = useRef([]);
  const textAnchorRef = useRef(null);
  // 触屏十字正在被拖的那次手指操作。
  const touchRef = useRef(null);

  useEffect(() => {
    magnetRef.current = magnet;
  }, [magnet]);

  // 下面的回调都只读 ref 与稳定的 setter，一律保持引用不变：指针事件的监听要用同一个函数对象添加和移除。

  // setHiddenAllSync 切换隐藏：画到一半时隐藏就放弃这一笔，已选中的取消选中(看不见的线不该保持选中)。
  const setHiddenAllSync = useCallback((value) => {
    setHiddenAll(value);
    hiddenRef.current = value;
    if (value) {
      toolRef.current = null;
      setTool(null);
    }
    const live = liveRef.current;
    if (!live) return;
    live.layer.hidden = value;
    if (value && live.layer.selectedId) {
      live.layer.selectedId = null;
      setSelection(null);
    }
    live.layer.update();
  }, []);

  // paneEl 是主图(第一个 pane)的画布：鼠标位置换成主图坐标靠它，副图与价格轴自然排除在外。pane 的元素在渲染时才建，用时再取。
  const paneEl = useCallback((live) => {
    if (!live.paneBox?.isConnected) {
      const el = live.chart.panes()[0]?.getHTMLElement() ?? null;
      live.paneBox = el?.querySelector('canvas') ?? el;
    }
    return live.paneBox;
  }, []);

  // localPt 把屏幕坐标换成主图坐标，inside 为假表示落在副图或价格轴上，不归画线管。
  const localPt = useCallback(
    (live, clientX, clientY) => {
      const r = paneEl(live)?.getBoundingClientRect();
      if (!r) return null;
      const x = clientX - r.left;
      const y = clientY - r.top;
      return {
        x,
        y,
        inside: x >= 0 && x <= r.width && y >= 0 && y <= r.height,
      };
    },
    [paneEl],
  );

  // paneToHost 把主图坐标换成图表宿主的坐标，文字输入框按宿主定位。
  const paneToHost = useCallback(
    (live, x, y) => {
      const pane = paneEl(live)?.getBoundingClientRect();
      const host = live.host.getBoundingClientRect();
      if (!pane) return { x, y };
      return { x: x + pane.left - host.left, y: y + pane.top - host.top };
    },
    [paneEl],
  );

  // anchorAt 把像素换成锚点，开着磁吸时吸到最近的开高低收。
  const anchorAt = useCallback((live, x, y) => {
    const t = coordToTime(x, live.ctx);
    if (!Number.isFinite(t)) return null;
    if (!magnetRef.current) {
      const p = live.ctx.series.coordinateToPrice(y);
      return Number.isFinite(p) ? { a: { t, p }, snapped: false } : null;
    }
    const m = magnetPrice(t, y, live.ctx);
    return m && Number.isFinite(m.p)
      ? { a: { t, p: m.p }, snapped: m.snapped }
      : null;
  }, []);

  // lock 锁住图表自己的平移缩放，把手势让给画线；解锁时恢复建图时的配置。触屏绘制模式顺便藏掉图表库的十字线。
  const lock = useCallback((live, on) => {
    live.chart.applyOptions({
      handleScroll: on ? false : live.scrollOpts,
      handleScale: on ? false : live.scaleOpts,
      crosshair: {
        mode:
          on && live.layer.cursor && toolRef.current
            ? CrosshairMode.Hidden
            : live.crosshairMode,
      },
    });
    live.host.style.touchAction = on ? 'none' : live.touchAction;
  }, []);

  const persist = useCallback((live) => {
    snapshotRef.current = { key: live.key, drawings: live.layer.drawings };
    saveDrawings(live.market, live.symbol, live.layer.drawings, live.userId);
    setCount(live.layer.drawings.length);
  }, []);

  // syncSelection 在选中的图形换了、改了样式或删了之后同步给属性条。
  const syncSelection = useCallback((live) => {
    const d = live.layer.drawings.find((x) => x.id === live.layer.selectedId);
    setSelection(
      d
        ? {
            id: d.id,
            kind: d.kind,
            color: d.color,
            width: d.width ?? 1,
            dash: d.dash ?? 'solid',
          }
        : null,
    );
  }, []);

  // remember 在改动之前把整套画线压进撤销栈。
  const remember = useCallback((live) => {
    const history = historyRef.current;
    history.push(structuredClone(live.layer.drawings));
    if (history.length > UNDO_MAX) history.shift();
    setUndoCount(history.length);
  }, []);

  // undo 撤销上一步。正在落点或拖动时不撤：那一步还没落定，撤掉的会是再前一步。
  const undo = useCallback(() => {
    const live = liveRef.current;
    if (
      !live ||
      placedRef.current.length ||
      dragRef.current ||
      textAnchorRef.current ||
      live.layer.editingId
    )
      return;
    const previous = historyRef.current.pop();
    if (!previous) return;
    setUndoCount(historyRef.current.length);
    const L = live.layer;
    L.drawings = previous;
    if (!previous.some((d) => d.id === L.selectedId)) {
      L.selectedId = null;
      // 属性条随选中项一起卸载，先把焦点交还图表，连续撤销仍有作用域。
      live.host.focus({ preventScroll: true });
    }
    syncSelection(live);
    persist(live);
    L.update();
  }, [syncSelection, persist]);

  // endDraw 结束一次绘制(画完或放弃)：清掉预览，回到选择模式。
  const endDraw = useCallback(
    (live) => {
      placedRef.current = [];
      toolRef.current = null;
      touchRef.current = null;
      live.layer.pending = null;
      live.layer.snap = null;
      live.layer.cursor = null;
      live.layer.interactive = true;
      live.host.style.cursor = live.cursor;
      setTool(null);
      lock(live, false);
      live.layer.update();
    },
    [lock],
  );

  const commit = useCallback(
    (live, kind, pts, text) => {
      remember(live);
      const d = {
        id: newDrawingId(),
        kind,
        pts,
        color: DRAW_COLOR,
        ...(text ? { text } : {}),
      };
      live.layer.drawings.push(d);
      live.layer.selectedId = d.id;
      syncSelection(live);
      persist(live);
      live.layer.update();
    },
    [remember, syncSelection, persist],
  );

  const dropSelected = useCallback(
    (live) => {
      const L = live.layer;
      if (!L.drawings.some((d) => d.id === L.selectedId)) return;
      remember(live);
      L.drawings = L.drawings.filter((d) => d.id !== L.selectedId);
      L.selectedId = null;
      setSelection(null);
      live.host.focus({ preventScroll: true });
      persist(live);
      L.update();
    },
    [remember, persist],
  );

  // flashDone 在触屏画完时在图中央闪一下"已画好"，纯装饰，不进 React。
  const flashDone = useCallback((live) => {
    live.tip?.remove();
    cancelAnimationFrame(live.tipFrame);
    clearTimeout(live.tipTimer);
    clearTimeout(live.tipRemoveTimer);
    const tip = document.createElement('div');
    live.tip = tip;
    tip.textContent = live.t('已画好');
    Object.assign(tip.style, {
      position: 'absolute',
      left: '50%',
      top: '50%',
      transform: 'translate(-50%,-50%)',
      zIndex: '8',
      padding: '8px 18px',
      borderRadius: '10px',
      background: 'rgba(23,24,26,.82)',
      color: '#fff',
      font: '700 14px/1 system-ui, sans-serif',
      opacity: '0',
      transition: 'opacity .18s ease',
      pointerEvents: 'none',
    });
    live.host.appendChild(tip);
    live.tipFrame = requestAnimationFrame(() => {
      tip.style.opacity = '1';
    });
    live.tipTimer = setTimeout(() => {
      tip.style.opacity = '0';
      live.tipRemoveTimer = setTimeout(() => tip.remove(), 220);
    }, 900);
  }, []);

  // preview 让多点工具的下一个点跟着指针或十字走。
  const preview = useCallback((live, r) => {
    const L = live.layer;
    const kind = toolRef.current;
    L.snap = r.snapped ? r.a : null;
    if (kind && placedRef.current.length > 0) {
      L.pending = {
        id: '_pending',
        kind,
        pts: finalizePoints(kind, [...placedRef.current, r.a]),
        color: DRAW_COLOR,
      };
    }
    L.update();
  }, []);

  // place 落一个点：单点工具一下就画好；多点工具点够数才画好(仓位工具的止盈由 finalizePoints 生成)，不够时更新预览。
  // 返回这一笔是否画好了。
  const place = useCallback(
    (live, r) => {
      const kind = toolRef.current;
      if (!kind) return false;
      const L = live.layer;
      if (kind === 'text') {
        // 输入框直接摆在文字画好后出现的位置(锚点右侧)，提交时不跳。
        const q = anchorToPoint(r.a, live.ctx);
        if (!q) return false;
        const h = paneToHost(live, q.x + 15, q.y);
        textAnchorRef.current = r.a;
        setTextEdit({ x: h.x, y: h.y });
        endDraw(live);
        return true;
      }
      const need = PLACE_POINTS[kind];
      if (need === 1) {
        commit(live, kind, [r.a]);
        endDraw(live);
        return true;
      }
      placedRef.current.push(r.a);
      if (placedRef.current.length >= need) {
        commit(live, kind, finalizePoints(kind, placedRef.current));
        endDraw(live);
        return true;
      }
      L.pending = {
        id: '_pending',
        kind,
        pts: finalizePoints(kind, [...placedRef.current, r.a]),
        color: DRAW_COLOR,
      };
      L.snap = r.snapped ? r.a : null;
      L.update();
      return false;
    },
    [commit, endDraw, paneToHost],
  );

  // moveCursor 移动触屏的十字，并联动预览与磁吸提示。
  const moveCursor = useCallback(
    (live, x, y) => {
      const L = live.layer;
      L.cursor = { x, y };
      const r = anchorAt(live, x, y);
      if (r) preview(live, r);
      else L.update();
    },
    [anchorAt, preview],
  );

  // showCursor 进入触屏绘制模式，十字摆在主图中央。
  const showCursor = useCallback(
    (live) => {
      const r = paneEl(live)?.getBoundingClientRect();
      moveCursor(
        live,
        (r?.width ?? live.host.clientWidth) / 2,
        (r?.height ?? live.host.clientHeight) / 2,
      );
    },
    [paneEl, moveCursor],
  );

  const hideCursor = useCallback((live) => {
    const pointerId = touchRef.current?.pointerId;
    touchRef.current = null;
    if (pointerId !== undefined && live.host.hasPointerCapture(pointerId))
      live.host.releasePointerCapture(pointerId);
    if (!live.layer.cursor) return;
    live.layer.cursor = null;
    live.layer.update();
  }, []);

  // tapFix 在触屏轻点时把十字交叉点定为当前工具的下一个点。文字工具这一下只是开始输入，不算画完。
  const tapFix = useCallback(
    (live) => {
      const cursor = live.layer.cursor;
      if (!cursor) return;
      const r = anchorAt(live, cursor.x, cursor.y);
      if (!r) return;
      const kind = toolRef.current;
      if (place(live, r) && kind !== 'text') flashDone(live);
    },
    [anchorAt, place, flashDone],
  );

  const stopDrag = useCallback(() => {
    const handlers = dragHandlersRef.current;
    if (!handlers) return;
    window.removeEventListener('pointermove', handlers.move, true);
    window.removeEventListener('pointerup', handlers.up, true);
    window.removeEventListener('pointercancel', handlers.up, true);
    dragHandlersRef.current = null;
  }, []);

  const onWinMove = useCallback(
    (e) => {
      const live = liveRef.current;
      const drag = dragRef.current;
      if (!live || !drag || e.pointerId !== drag.pointerId) return;
      const pt = localPt(live, e.clientX, e.clientY);
      if (!pt) return;
      const L = live.layer;
      const d = L.drawings.find((x) => x.id === drag.id);
      if (!d) return;
      if (!drag.moved) {
        // 没移出轻点的范围算点选：手抖不该挪动图形，也不该记一步撤销。
        if (Math.hypot(e.clientX - drag.cx0, e.clientY - drag.cy0) <= TAP_SLOP)
          return;
        drag.moved = true;
        remember(live);
      }
      if (drag.pt >= 0) {
        const r = anchorAt(live, pt.x, pt.y);
        if (!r) return;
        if (d.kind === 'long' || d.kind === 'short') {
          // 仓位工具的止盈点与止损点同一时刻：拖止盈只改价格，拖止损把止盈的时刻一起带走。
          if (drag.pt === 2) d.pts[2] = { t: d.pts[1].t, p: r.a.p };
          else if (drag.pt === 1) {
            d.pts[1] = r.a;
            d.pts[2] = { t: r.a.t, p: d.pts[2].p };
          } else d.pts[0] = r.a;
          d.pts = normalizePositionPoints(d.kind, d.pts);
        } else {
          d.pts[drag.pt] = r.a;
        }
        L.snap = r.snapped ? r.a : null;
      } else {
        // 整体平移不吸附，否则一跳一跳；横向仍按整根 K 线走。
        const t = coordToTime(pt.x, live.ctx);
        const p = live.ctx.series.coordinateToPrice(pt.y);
        if (!Number.isFinite(t) || !Number.isFinite(p)) return;
        const dt = t - drag.t0;
        const dp = p - drag.p0;
        d.pts = drag.orig.map((a) => ({ t: a.t + dt, p: a.p + dp }));
        L.snap = null;
      }
      L.update();
    },
    [localPt, anchorAt, remember],
  );

  const onWinUp = useCallback(
    (e) => {
      const drag = dragRef.current;
      if (
        !drag ||
        (e?.pointerId !== undefined && e.pointerId !== drag.pointerId)
      )
        return;
      stopDrag();
      const live = liveRef.current;
      dragRef.current = null;
      if (!live) return;
      if (live.host.hasPointerCapture(drag.pointerId))
        live.host.releasePointerCapture(drag.pointerId);
      if (drag.moved) {
        if (e?.type === 'pointercancel' || e?.key === 'Escape') {
          const d = live.layer.drawings.find((x) => x.id === drag.id);
          if (d) d.pts = drag.orig;
          historyRef.current.pop();
          setUndoCount(historyRef.current.length);
        } else persist(live);
      }
      live.layer.snap = null;
      // 还在绘制模式就继续锁着。
      lock(live, toolRef.current !== null);
      live.layer.update();
    },
    [stopDrag, persist, lock],
  );

  // onMove 处理宿主上的指针移动：触屏绘制模式下是拖十字(相对位移)；鼠标只在选了工具或画到一半时接管(预览)，其余交给图表。
  const onMove = useCallback(
    (e) => {
      const live = liveRef.current;
      if (!live) return;
      const touch = touchRef.current;
      if (touch) {
        if (e.pointerId !== touch.pointerId) return;
        const dx = e.clientX - touch.startCX;
        const dy = e.clientY - touch.startCY;
        if (Math.hypot(dx, dy) > TAP_SLOP) touch.moved = true;
        if (!touch.moved) return;
        const r = paneEl(live)?.getBoundingClientRect();
        const w = r?.width ?? live.host.clientWidth;
        const h = r?.height ?? live.host.clientHeight;
        moveCursor(
          live,
          Math.min(Math.max(touch.baseX + dx, 0), w),
          Math.min(Math.max(touch.baseY + dy, 0), h),
        );
        return;
      }
      if (live.layer.cursor) return;
      if (!toolRef.current && placedRef.current.length === 0) return;
      const pt = localPt(live, e.clientX, e.clientY);
      if (!pt) return;
      const L = live.layer;
      if (!pt.inside) {
        if (L.snap) {
          L.snap = null;
          L.update();
        }
        return;
      }
      const r = anchorAt(live, pt.x, pt.y);
      if (r) preview(live, r);
    },
    [paneEl, moveCursor, localPt, anchorAt, preview],
  );

  const onDown = useCallback(
    (e) => {
      const live = liveRef.current;
      if (!live || e.button !== 0 || e.isPrimary === false) return;
      const L = live.layer;
      const kind = toolRef.current;
      live.suppressClick = false;
      const pt = localPt(live, e.clientX, e.clientY);
      if (!pt?.inside || dragRef.current || touchRef.current) return;
      // 点到图上就让输入框失焦(文字标注靠失焦提交)：下面多半会 preventDefault，焦点不会自己离开，Delete 与 Ctrl+Z 会一直被
      // 输入框吃掉。
      const active = document.activeElement;
      if (
        active instanceof HTMLElement &&
        (active.tagName === 'INPUT' || active.tagName === 'TEXTAREA')
      )
        active.blur();
      live.host.focus({ preventScroll: true });
      // 混合输入设备按实际指针类型选择交互，不依赖页面加载时的媒体查询。
      if (kind && e.pointerType === 'touch' && !L.cursor) {
        showCursor(live);
        lock(live, true);
      } else if (kind && e.pointerType !== 'touch' && L.cursor) {
        hideCursor(live);
        lock(live, true);
      }
      // 触屏绘制模式：整块图是十字的触控板，按下只记起点，抬起时按动没动分成拖动与轻点。
      if (L.cursor) {
        e.preventDefault();
        live.suppressClick = true;
        live.onInteract?.();
        live.host.setPointerCapture(e.pointerId);
        touchRef.current = {
          pointerId: e.pointerId,
          startCX: e.clientX,
          startCY: e.clientY,
          baseX: L.cursor.x,
          baseY: L.cursor.y,
          moved: false,
        };
        return;
      }
      if (kind) {
        const r = anchorAt(live, pt.x, pt.y);
        if (!r) return;
        // 压掉随后的兼容鼠标事件，图表收不到 mousedown，也不会触发点击。
        e.preventDefault();
        live.suppressClick = true;
        live.onInteract?.();
        place(live, r);
        return;
      }
      // 选择模式：主动判断点中了什么，触屏没有悬停这一步。
      const hit = L.pick(pt.x, pt.y);
      if (!hit) {
        if (L.selectedId) {
          L.selectedId = null;
          setSelection(null);
          L.update();
        }
        return;
      }
      live.suppressClick = true;
      live.onInteract?.();
      // 手机上按中还没选中的图形只选中、不拖：斐波那契、水平线、大矩形铺满主图，手指一放上去就拖线的话页面就滑不动了。
      if (e.pointerType === 'touch' && L.selectedId !== hit.id) {
        L.selectedId = hit.id;
        syncSelection(live);
        L.update();
        return;
      }
      e.preventDefault();
      lock(live, true);
      L.selectedId = hit.id;
      syncSelection(live);
      const d = L.drawings.find((x) => x.id === hit.id);
      const t0 = coordToTime(pt.x, live.ctx);
      const p0 = live.ctx.series.coordinateToPrice(pt.y);
      dragRef.current =
        d && t0 !== null && p0 !== null
          ? {
              id: hit.id,
              pointerId: e.pointerId,
              pt: hit.pt,
              t0,
              p0,
              orig: d.pts.map((a) => ({ ...a })),
              moved: false,
              cx0: e.clientX,
              cy0: e.clientY,
            }
          : null;
      if (!dragRef.current) {
        lock(live, false);
        L.update();
        return;
      }
      live.host.setPointerCapture(e.pointerId);
      // 挂在 window 上：拖出图表范围也跟得住。
      const handlers = { move: onWinMove, up: onWinUp };
      dragHandlersRef.current = handlers;
      window.addEventListener('pointermove', handlers.move, true);
      window.addEventListener('pointerup', handlers.up, true);
      window.addEventListener('pointercancel', handlers.up, true);
      L.update();
    },
    [
      localPt,
      anchorAt,
      lock,
      place,
      syncSelection,
      onWinMove,
      onWinUp,
      showCursor,
      hideCursor,
    ],
  );

  // onUp 是触屏手指抬起：没动过是轻点落点，动过只是拖了十字，不落点。
  const onUp = useCallback(
    (e) => {
      const live = liveRef.current;
      const touch = touchRef.current;
      if (!live || !touch || e.pointerId !== touch.pointerId) return;
      touchRef.current = null;
      if (live.host.hasPointerCapture(e.pointerId))
        live.host.releasePointerCapture(e.pointerId);
      if (e.type === 'pointerup' && !touch.moved) tapFix(live);
    },
    [tapFix],
  );

  const onKey = useCallback(
    (e) => {
      const live = liveRef.current;
      if (!live || e.defaultPrevented || e.isComposing) return;
      const el = document.activeElement;
      // 快捷键只属于当前有焦点的图表，编辑表单或另一张图时不抢按键。
      if (
        !live.scope.contains(el) ||
        el?.closest(
          'input, textarea, select, [contenteditable]:not([contenteditable="false"]), [role="textbox"]',
        )
      )
        return;
      if (e.key === 'Escape') {
        if (dragRef.current) onWinUp(e);
        else if (placedRef.current.length || toolRef.current) endDraw(live);
        else if (live.layer.selectedId) {
          live.layer.selectedId = null;
          setSelection(null);
          live.host.focus({ preventScroll: true });
          live.layer.update();
        } else return;
        // 这次 Escape 已经用掉了，退出全屏要再按一次。
        e.preventDefault();
        return;
      }
      if (dragRef.current || touchRef.current) return;
      if (
        (e.ctrlKey || e.metaKey) &&
        !e.shiftKey &&
        !e.altKey &&
        historyRef.current.length > 0 &&
        !placedRef.current.length &&
        e.key.toLowerCase() === 'z'
      ) {
        e.preventDefault();
        undo();
        return;
      }
      if (
        (e.key === 'Delete' || e.key === 'Backspace') &&
        !e.ctrlKey &&
        !e.metaKey &&
        !e.altKey &&
        live.layer.selectedId
      ) {
        e.preventDefault();
        dropSelected(live);
      }
    },
    [endDraw, dropSelected, undo, onWinUp],
  );

  // editText 修改选中的文字标注：输入框带着原文盖在原来的位置上。
  const editText = useCallback(() => {
    const live = liveRef.current;
    if (!live) return;
    const L = live.layer;
    const d = L.drawings.find((x) => x.id === L.selectedId);
    if (d?.kind !== 'text') return;
    const q = anchorToPoint(d.pts[0], live.ctx);
    if (!q) return;
    const h = paneToHost(live, q.x + 15, q.y);
    L.editingId = d.id;
    L.update();
    setTextEdit({ x: h.x, y: h.y, value: d.text ?? '' });
  }, [paneToHost]);

  // onDouble 是电脑上双击文字标注改字(手机用属性条上的编辑按钮)。
  const onDouble = useCallback(
    (e) => {
      const live = liveRef.current;
      if (!live || toolRef.current) return;
      const pt = localPt(live, e.clientX, e.clientY);
      if (!pt?.inside) return;
      const hit = live.layer.pick(pt.x, pt.y);
      if (
        !hit ||
        live.layer.drawings.find((x) => x.id === hit.id)?.kind !== 'text'
      )
        return;
      live.layer.selectedId = hit.id;
      live.suppressClick = true;
      live.onInteract?.();
      e.preventDefault();
      e.stopPropagation();
      syncSelection(live);
      editText();
    },
    [localPt, syncSelection, editText],
  );

  // attach 由 CandleChart 在建图的 effect 里调用，返回 detach(必须在 chart.remove() 之前调用)。
  // args 是 { chart, series 蜡烛线, host 图表宿主, scope 含工具栏的容器, market, symbol, userId, ctx, decimals, fmtTime, t, onInteract }。
  const attach = useCallback(
    (args) => {
      detachRef.current?.();
      const key = `${args.userId}:${args.market}:${args.symbol}`;
      const layer = new DrawingLayer(args.ctx, {
        decimals: args.decimals,
        fmtTime: args.fmtTime,
        t: args.t,
      });
      layer.drawings =
        snapshotRef.current?.key === key
          ? snapshotRef.current.drawings
          : loadDrawings(args.market, args.symbol, args.userId);
      layer.hidden = hiddenRef.current;
      args.series.attachPrimitive(layer);
      const live = {
        ...args,
        key,
        layer,
        paneBox: null,
        scope: args.scope ?? args.host,
        scrollOpts: structuredClone(args.chart.options().handleScroll),
        scaleOpts: structuredClone(args.chart.options().handleScale),
        touchAction: args.host.style.touchAction,
        cursor: args.host.style.cursor,
        tabIndex: args.host.getAttribute('tabindex'),
        suppressClick: false,
        crosshairMode:
          args.chart.options().crosshair.mode ?? CrosshairMode.Normal,
      };
      liveRef.current = live;
      // 换交易对、周期时不该还拿着上一次的工具。
      toolRef.current = null;
      setTool(null);
      setSelection(null);
      setCount(layer.drawings.length);
      setTextEdit(null);
      textAnchorRef.current = null;
      placedRef.current = [];
      touchRef.current = null;
      // 撤销记录跟着交易对走：开关副图、换主题、换周期也会重新挂图层，同一个交易对的画线没变，撤销记录留着。
      if (historyKeyRef.current !== key) {
        historyKeyRef.current = key;
        historyRef.current = [];
        setUndoCount(0);
      }
      // 手机上拖已有图形时，在 pointerdown 里才设 touch-action 已经晚了，浏览器照样滚动页面再取消拖动；拖动或拖十字期间直接
      // 拦掉 touchmove 的默认滚动(必须不是 passive 才拦得住)。
      const onTouchMove = (e) => {
        if (dragRef.current || touchRef.current) e.preventDefault();
      };
      const onCaptureLost = (e) => {
        onUp(e);
        onWinUp({ pointerId: e.pointerId, type: 'pointercancel' });
      };
      const host = args.host;
      if (live.tabIndex === null) host.tabIndex = 0;
      host.addEventListener('pointerdown', onDown, true);
      host.addEventListener('pointermove', onMove, true);
      host.addEventListener('pointerup', onUp, true);
      host.addEventListener('pointercancel', onUp, true);
      host.addEventListener('lostpointercapture', onCaptureLost, true);
      host.addEventListener('dblclick', onDouble, true);
      host.addEventListener('touchmove', onTouchMove, { passive: false });
      live.scope.addEventListener('keydown', onKey);
      let detached = false;
      const detach = () => {
        if (detached) return;
        detached = true;
        host.removeEventListener('pointerdown', onDown, true);
        host.removeEventListener('pointermove', onMove, true);
        host.removeEventListener('pointerup', onUp, true);
        host.removeEventListener('pointercancel', onUp, true);
        host.removeEventListener('lostpointercapture', onCaptureLost, true);
        host.removeEventListener('dblclick', onDouble, true);
        host.removeEventListener('touchmove', onTouchMove);
        live.scope.removeEventListener('keydown', onKey);
        // 周期或副图可能在拖动期间重建；保存已移动的图形，再清理监听和捕获。
        if (dragRef.current?.moved) persist(live);
        snapshotRef.current = { key, drawings: layer.drawings };
        stopDrag();
        const pointerId =
          dragRef.current?.pointerId ?? touchRef.current?.pointerId;
        dragRef.current = null;
        touchRef.current = null;
        if (pointerId !== undefined && host.hasPointerCapture(pointerId))
          host.releasePointerCapture(pointerId);
        toolRef.current = null;
        placedRef.current = [];
        textAnchorRef.current = null;
        cancelAnimationFrame(live.tipFrame);
        clearTimeout(live.tipTimer);
        clearTimeout(live.tipRemoveTimer);
        live.tip?.remove();
        live.clearDialog?.destroy();
        lock(live, false);
        host.style.cursor = live.cursor;
        if (live.tabIndex === null) host.removeAttribute('tabindex');
        args.series.detachPrimitive(layer);
        liveRef.current = null;
        detachRef.current = null;
      };
      detachRef.current = detach;
      return detach;
    },
    [onDown, onMove, onUp, onWinUp, onDouble, onKey, stopDrag, persist, lock],
  );

  // 切换工具：锁图表、改光标、关掉命中判断(画新线时不该被旧线抢走光标)，清掉画了一半的点(否则上一个工具的点会带进新工具)。
  useEffect(() => {
    toolRef.current = tool;
    const live = liveRef.current;
    if (!live) return;
    live.layer.interactive = tool === null;
    live.host.style.cursor = tool ? 'crosshair' : live.cursor;
    placedRef.current = [];
    live.layer.pending = null;
    live.layer.snap = null;
    // 触屏：进入绘制模式出十字，退出收起；先处理十字再 lock，lock 按它决定藏不藏图表库的十字线。
    if (tool && window.matchMedia('(pointer: coarse)').matches)
      showCursor(live);
    if (!tool) hideCursor(live);
    lock(live, tool !== null);
    live.layer.update();
  }, [tool, lock, showCursor, hideCursor]);

  // trash 有选中时删除选中的，否则清空这个交易对的全部画线(先确认，一下抹掉几十条太狠)。
  const trash = useCallback(() => {
    const live = liveRef.current;
    if (!live) return;
    const L = live.layer;
    if (L.selectedId) {
      dropSelected(live);
      return;
    }
    if (!L.drawings.length || live.clearDialog) return;
    live.clearDialog = Modal.confirm({
      getPopupContainer: () => {
        const fullscreen = document.fullscreenElement;
        return fullscreen?.contains(live.scope) ? fullscreen : document.body;
      },
      title: live.t('清空 {{symbol}} 的全部 {{count}} 条画线？', {
        symbol: live.symbol,
        count: L.drawings.length,
      }),
      okText: live.t('确认'),
      cancelText: live.t('取消'),
      afterClose: () => {
        live.clearDialog = null;
        if (liveRef.current === live) live.host.focus({ preventScroll: true });
      },
      onOk: () => {
        if (liveRef.current !== live) return;
        remember(live);
        L.drawings = [];
        L.selectedId = null;
        setSelection(null);
        persist(live);
        L.update();
      },
    });
  }, [dropSelected, remember, persist]);

  // commitText 在回车或失焦时收尾文字输入。改已有标注时清空等于不改(删标注用删除)；回车后输入框卸载还会再失焦一次，那时
  // 状态已经清掉，什么都不做。
  const commitText = useCallback(
    (value) => {
      const live = liveRef.current;
      const anchor = textAnchorRef.current;
      setTextEdit(null);
      textAnchorRef.current = null;
      if (!live) return;
      const L = live.layer;
      // 回车提交时输入框仍有焦点；失焦提交时保留用户正在前往的控件。
      const restoreFocus = live.scope.contains(document.activeElement);
      const text = typeof value === 'string' ? value.trim() : '';
      const editing = L.drawings.find((x) => x.id === L.editingId);
      if (editing) {
        L.editingId = null;
        if (text && text !== editing.text) {
          remember(live);
          editing.text = text;
          persist(live);
        }
        L.update();
        if (restoreFocus) live.host.focus({ preventScroll: true });
        return;
      }
      if (anchor && text) commit(live, 'text', [anchor], text);
      if (restoreFocus) live.host.focus({ preventScroll: true });
    },
    [commit, remember, persist],
  );

  const cancelText = useCallback(() => {
    setTextEdit(null);
    textAnchorRef.current = null;
    const live = liveRef.current;
    if (live?.layer.editingId) {
      live.layer.editingId = null;
      live.layer.update();
    }
    if (live?.scope.contains(document.activeElement))
      live.host.focus({ preventScroll: true });
  }, []);

  // setStyle 改选中图形的颜色、线宽或线型；点的就是当前值时什么都不做，免得白记一步撤销。
  const setStyle = useCallback(
    (patch) => {
      const live = liveRef.current;
      if (!live) return;
      const d = live.layer.drawings.find((x) => x.id === live.layer.selectedId);
      if (!d) return;
      if (
        (patch.color === undefined || patch.color === d.color) &&
        (patch.width === undefined || patch.width === (d.width ?? 1)) &&
        (patch.dash === undefined || patch.dash === (d.dash ?? 'solid'))
      )
        return;
      remember(live);
      Object.assign(d, patch);
      persist(live);
      syncSelection(live);
      live.layer.update();
    },
    [remember, persist, syncSelection],
  );

  // cloneSelected 复制选中的图形，副本往右下错开 CLONE_OFFSET 像素(横向按整根 K 线错开)，并选中副本方便接着拖。
  const cloneSelected = useCallback(() => {
    const live = liveRef.current;
    if (!live) return;
    const L = live.layer;
    const source = L.drawings.find((x) => x.id === L.selectedId);
    if (!source) return;
    remember(live);
    const timeScale = live.ctx.timeScale;
    const a0 = timeScale.logicalToCoordinate(0);
    const a1 = timeScale.logicalToCoordinate(1);
    const spacing = a0 !== null && a1 !== null ? a1 - a0 : 0;
    const dt =
      Math.max(1, spacing > 0 ? Math.round(CLONE_OFFSET / spacing) : 1) *
      live.ctx.bucketSec;
    const pts = source.pts.map((a) => {
      const y = live.ctx.series.priceToCoordinate(a.p);
      const p =
        y === null ? null : live.ctx.series.coordinateToPrice(y + CLONE_OFFSET);
      return { t: a.t + dt, p: p ?? a.p };
    });
    const copy = { ...structuredClone(source), id: newDrawingId(), pts };
    L.drawings.push(copy);
    L.selectedId = copy.id;
    syncSelection(live);
    persist(live);
    L.update();
  }, [remember, syncSelection, persist]);

  const deleteSelected = useCallback(() => {
    const live = liveRef.current;
    if (live?.layer.selectedId) dropSelected(live);
  }, [dropSelected]);

  // selectTool 是工具栏的入口：画线被隐藏着时选了画线工具就自动显示，画完看不见太奇怪。
  const selectTool = useCallback(
    (next) => {
      if (next !== null && !Object.hasOwn(PLACE_POINTS, next)) return;
      if (next !== null && hiddenRef.current) setHiddenAllSync(false);
      toolRef.current = next;
      setTool(next);
      if (next !== null) liveRef.current?.onInteract?.();
    },
    [setHiddenAllSync],
  );

  // 图表库的点击可能晚于结束绘制；保留本次手势的归属，直到下次按下再重置。
  const shouldHandleChartClick = useCallback(() => {
    const live = liveRef.current;
    return (
      !live?.suppressClick &&
      toolRef.current === null &&
      !dragRef.current &&
      !touchRef.current
    );
  }, []);

  return {
    attach,
    shouldHandleChartClick,
    tool,
    setTool: selectTool,
    magnet,
    setMagnet,
    hiddenAll,
    setHiddenAll: setHiddenAllSync,
    selection,
    setStyle,
    cloneSelected,
    deleteSelected,
    editText,
    undo,
    canUndo: undoCount > 0,
    count,
    trash,
    textEdit,
    commitText,
    cancelText,
  };
}
