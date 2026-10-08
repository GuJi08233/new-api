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

import React, { useEffect, useRef, useState } from 'react';

// 用户打开高级档后才挂载并请求官方脚本；只发送公开交易对，不传账户、持仓或资金数据。
export default function TradingViewChart({ symbol, locale, theme, t }) {
  const hostRef = useRef(null);
  const [failed, setFailed] = useState(false);
  const language =
    {
      'zh-CN': 'zh_CN',
      'zh-TW': 'zh_TW',
      en: 'en',
      fr: 'fr',
      ru: 'ru',
      ja: 'ja',
      vi: 'vi_VN',
    }[locale] || 'en';

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    setFailed(false);
    const script = document.createElement('script');
    script.src =
      'https://s3.tradingview.com/external-embedding/embed-widget-advanced-chart.js';
    script.async = true;
    script.type = 'text/javascript';
    script.textContent = JSON.stringify({
      autosize: true,
      symbol,
      interval: 'D',
      timezone: 'Etc/UTC',
      theme: theme === 'dark' ? 'dark' : 'light',
      locale: language,
      style: '1',
      allow_symbol_change: true,
      hide_side_toolbar: true,
      hide_top_toolbar: false,
      hide_volume: false,
      save_image: true,
      calendar: false,
      details: false,
      withdateranges: false,
    });
    script.onerror = () => setFailed(true);
    const widget = document.createElement('div');
    widget.className = 'tradingview-widget-container__widget';
    host.append(widget, script);
    return () => {
      script.onerror = null;
      // 脚本还在下载时被移出文档也会照常执行，并去找自己的父节点；挪进一个脱离文档的节点，别让它报错。
      const orphan = document.createElement('div');
      orphan.append(...host.childNodes);
    };
  }, [symbol, language, theme]);

  return (
    <div className='trade-chart-advanced'>
      <div
        ref={hostRef}
        className='tradingview-widget-container trade-chart-advanced-widget'
      />
      <div className='tradingview-widget-copyright'>
        <a
          href={`https://www.tradingview.com/chart/?symbol=${encodeURIComponent(symbol)}`}
          target='_blank'
          rel='noopener noreferrer'
        >
          {symbol}
        </a>
        {' by TradingView'}
      </div>
      {failed && (
        <div className='trade-chart-advanced-error' role='status'>
          {t('高级图表暂时无法加载，请切换回基础图表。')}
        </div>
      )}
    </div>
  );
}
