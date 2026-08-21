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

import React from 'react';
import { Progress, Divider, Empty, Tooltip } from '@douyinfe/semi-ui';
import {
  IllustrationConstruction,
  IllustrationConstructionDark,
} from '@douyinfe/semi-illustrations';
import {
  timestamp2string,
  timestamp2string1,
  isDataCrossYear,
  copy,
  showSuccess,
} from './utils';
import {
  STORAGE_KEYS,
  DEFAULT_TIME_INTERVALS,
  DEFAULTS,
  ILLUSTRATION_SIZE,
} from '../constants/dashboard.constants';

// ========== 时间相关工具函数 ==========
export const getDefaultTime = () => {
  return localStorage.getItem(STORAGE_KEYS.DATA_EXPORT_DEFAULT_TIME) || 'hour';
};

export const getTimeInterval = (timeType, isSeconds = false) => {
  const intervals =
    DEFAULT_TIME_INTERVALS[timeType] || DEFAULT_TIME_INTERVALS.hour;
  return isSeconds ? intervals.seconds : intervals.minutes;
};

export const getInitialTimestamp = () => {
  const defaultTime = getDefaultTime();
  const now = new Date().getTime() / 1000;

  switch (defaultTime) {
    case 'hour':
      return timestamp2string(now - 86400);
    case 'week':
      return timestamp2string(now - 86400 * 30);
    default:
      return timestamp2string(now - 86400 * 7);
  }
};

// ========== 数据处理工具函数 ==========
export const updateMapValue = (map, key, value) => {
  if (!map.has(key)) {
    map.set(key, 0);
  }
  map.set(key, map.get(key) + value);
};

export const initializeMaps = (key, ...maps) => {
  maps.forEach((map) => {
    if (!map.has(key)) {
      map.set(key, 0);
    }
  });
};

// ========== 图表相关工具函数 ==========
export const updateChartSpec = (
  setterFunc,
  newData,
  subtitle,
  newColors,
  dataId,
) => {
  setterFunc((prev) => ({
    ...prev,
    data: [{ id: dataId, values: newData }],
    title: {
      ...prev.title,
      subtext: subtitle,
    },
    color: {
      specified: newColors,
    },
  }));
};

export const getTrendSpec = (data, color) => ({
  type: 'line',
  data: [{ id: 'trend', values: data.map((val, idx) => ({ x: idx, y: val })) }],
  xField: 'x',
  yField: 'y',
  height: 40,
  width: 100,
  axes: [
    {
      orient: 'bottom',
      visible: false,
    },
    {
      orient: 'left',
      visible: false,
    },
  ],
  padding: 0,
  autoFit: false,
  legends: { visible: false },
  tooltip: { visible: false },
  crosshair: { visible: false },
  line: {
    style: {
      stroke: color,
      lineWidth: 2,
    },
  },
  point: {
    visible: false,
  },
  background: {
    fill: 'transparent',
  },
});

// ========== UI 工具函数 ==========
export const createSectionTitle = (Icon, text) => (
  <div className='flex items-center gap-2'>
    <Icon size={16} />
    {text}
  </div>
);

export const createFormField = (Component, props, FORM_FIELD_PROPS) => (
  <Component {...FORM_FIELD_PROPS} {...props} />
);

// ========== 操作处理函数 ==========
export const handleCopyUrl = async (url, t) => {
  if (await copy(url)) {
    showSuccess(t('复制成功'));
  }
};

export const handleSpeedTest = (apiUrl) => {
  const encodedUrl = encodeURIComponent(apiUrl);
  const speedTestUrl = `https://www.tcptest.cn/http/${encodedUrl}`;
  window.open(speedTestUrl, '_blank', 'noopener,noreferrer');
};

// ========== 状态映射函数 ==========
export const getUptimeStatusColor = (status, uptimeStatusMap) =>
  uptimeStatusMap[status]?.color || '#8b9aa7';

export const getUptimeStatusText = (status, uptimeStatusMap, t) =>
  uptimeStatusMap[status]?.text || t('未知');

// ========== 模型可用性相关函数 ==========
export const getModelAvailabilityStatusColor = (status, statusMap) =>
  statusMap[status]?.color || '#8b9aa7';

export const getModelAvailabilityStatusText = (status, statusMap, t) =>
  statusMap[status]?.text || t('未知');

// ========== 模型可用性列表渲染函数 ==========
export const renderModelAvailabilityList = (
  models,
  getStatusColor,
  getStatusText,
  t,
) => {
  if (!models || models.length === 0) {
    return (
      <div className='flex justify-center items-center py-4'>
        <Empty
          image={<IllustrationConstruction style={ILLUSTRATION_SIZE} />}
          darkModeImage={
            <IllustrationConstructionDark style={ILLUSTRATION_SIZE} />
          }
          title={t('暂无模型数据')}
        />
      </div>
    );
  }

  // 每小时一格的状态条：格子颜色按该小时成功率分档，与整体状态图例一致
  const renderHourlyBar = (model) => (
    <div className='flex flex-1 items-center gap-0.5 min-w-0'>
      {model.hourly.map((bucket, i) => {
        const success = bucket.success_count || 0;
        const failure = bucket.error_count || 0;
        const total = success + failure;
        let status = 'no_data';
        if (total > 0) {
          const rate = (success / total) * 100;
          status = rate >= 95 ? 'normal' : rate >= 80 ? 'warning' : 'error';
        }
        const startHour = String(
          new Date(bucket.timestamp * 1000).getHours(),
        ).padStart(2, '0');
        const endHour = String(
          new Date((bucket.timestamp + 3600) * 1000).getHours(),
        ).padStart(2, '0');
        const rangeLabel = `${startHour}:00 - ${endHour}:00`;
        const tip =
          total > 0
            ? `${rangeLabel} · ${t('成功')} ${success} / ${t('失败')} ${failure} · ${((success / total) * 100).toFixed(1)}%`
            : `${rangeLabel} · ${t('暂无调用记录')}`;
        return (
          <Tooltip key={bucket.timestamp || i} content={tip}>
            <div
              className='flex-1 h-3 rounded-sm min-w-0'
              style={{
                backgroundColor: getStatusColor(status),
                opacity: total > 0 ? 1 : 0.3,
              }}
            />
          </Tooltip>
        );
      })}
    </div>
  );

  const renderItem = (model, idx) => (
    <div key={idx} className='p-2 hover:bg-white rounded-lg transition-colors'>
      <div className='flex items-center justify-between mb-1'>
        <div className='flex items-center gap-2'>
          <div
            className='w-2 h-2 rounded-full flex-shrink-0'
            style={{ backgroundColor: getStatusColor(model.status) }}
          />
          <span className='text-sm font-medium text-gray-900'>
            {model.model_name}
          </span>
        </div>
        <span className='text-xs text-gray-500'>
          {model.total_count > 0 ? `${model.success_rate.toFixed(2)}%` : '-'}
        </span>
      </div>
      <div className='flex items-center gap-2'>
        <span className='text-xs text-gray-500'>
          {getStatusText(model.status)}
        </span>
        {model.total_count > 0 ? (
          Array.isArray(model.hourly) && model.hourly.length > 0 ? (
            renderHourlyBar(model)
          ) : (
            <div className='flex-1'>
              <Progress
                percent={model.success_rate}
                showInfo={false}
                aria-label={`${model.model_name} success rate`}
                stroke={getStatusColor(model.status)}
              />
            </div>
          )
        ) : (
          <div className='flex-1 text-xs text-gray-400'>
            {t('过去24小时无调用记录')}
          </div>
        )}
      </div>
      {model.total_count > 0 && (
        <div className='text-xs text-gray-400 mt-1 flex gap-3'>
          <span>{t('总计')}: {model.total_count}{t('次')}</span>
          <span>{t('成功')}: {model.success_count}{t('次')}</span>
          <span>{t('失败')}: {model.error_count}{t('次')}</span>
          {typeof model.reliability === 'number' && (
            <span>{t('可靠度')}: {model.reliability.toFixed(2)}%</span>
          )}
        </div>
      )}
    </div>
  );

  return (
    <div className='max-h-96 overflow-y-auto'>
      {models.map(renderItem)}
    </div>
  );
};
export const renderMonitorList = (
  monitors,
  getUptimeStatusColor,
  getUptimeStatusText,
  t,
) => {
  if (!monitors || monitors.length === 0) {
    return (
      <div className='flex justify-center items-center py-4'>
        <Empty
          image={<IllustrationConstruction style={ILLUSTRATION_SIZE} />}
          darkModeImage={
            <IllustrationConstructionDark style={ILLUSTRATION_SIZE} />
          }
          title={t('暂无监控数据')}
        />
      </div>
    );
  }

  const grouped = {};
  monitors.forEach((m) => {
    const g = m.group || '';
    if (!grouped[g]) grouped[g] = [];
    grouped[g].push(m);
  });

  const renderItem = (monitor, idx) => (
    <div key={idx} className='p-2 hover:bg-white rounded-lg transition-colors'>
      <div className='flex items-center justify-between mb-1'>
        <div className='flex items-center gap-2'>
          <div
            className='w-2 h-2 rounded-full flex-shrink-0'
            style={{ backgroundColor: getUptimeStatusColor(monitor.status) }}
          />
          <span className='text-sm font-medium text-gray-900'>
            {monitor.name}
          </span>
        </div>
        <span className='text-xs text-gray-500'>
          {((monitor.uptime || 0) * 100).toFixed(2)}%
        </span>
      </div>
      <div className='flex items-center gap-2'>
        <span className='text-xs text-gray-500'>
          {getUptimeStatusText(monitor.status)}
        </span>
        <div className='flex-1'>
          <Progress
            percent={(monitor.uptime || 0) * 100}
            showInfo={false}
            aria-label={`${monitor.name} uptime`}
            stroke={getUptimeStatusColor(monitor.status)}
          />
        </div>
      </div>
    </div>
  );

  return Object.entries(grouped).map(([gname, list]) => (
    <div key={gname || 'default'} className='mb-2'>
      {gname && (
        <>
          <div className='text-md font-semibold text-gray-500 px-2 py-1'>
            {gname}
          </div>
          <Divider />
        </>
      )}
      {list.map(renderItem)}
    </div>
  ));
};

// ========== 数据处理函数 ==========
export const processRawData = (
  data,
  dataExportDefaultTime,
  initializeMaps,
  updateMapValue,
) => {
  const result = {
    totalQuota: 0,
    totalTimes: 0,
    totalTokens: 0,
    uniqueModels: new Set(),
    timePoints: [],
    timeQuotaMap: new Map(),
    timeTokensMap: new Map(),
    timeCountMap: new Map(),
  };

  // 检查数据是否跨年
  const showYear = isDataCrossYear(data.map((item) => item.created_at));

  data.forEach((item) => {
    result.uniqueModels.add(item.model_name);
    result.totalTokens += item.token_used;
    result.totalQuota += item.quota;
    result.totalTimes += item.count;

    const timeKey = timestamp2string1(
      item.created_at,
      dataExportDefaultTime,
      showYear,
    );
    if (!result.timePoints.includes(timeKey)) {
      result.timePoints.push(timeKey);
    }

    initializeMaps(
      timeKey,
      result.timeQuotaMap,
      result.timeTokensMap,
      result.timeCountMap,
    );
    updateMapValue(result.timeQuotaMap, timeKey, item.quota);
    updateMapValue(result.timeTokensMap, timeKey, item.token_used);
    updateMapValue(result.timeCountMap, timeKey, item.count);
  });

  result.timePoints.sort();
  return result;
};

export const calculateTrendData = (
  timePoints,
  timeQuotaMap,
  timeTokensMap,
  timeCountMap,
  dataExportDefaultTime,
) => {
  const quotaTrend = timePoints.map((time) => timeQuotaMap.get(time) || 0);
  const tokensTrend = timePoints.map((time) => timeTokensMap.get(time) || 0);
  const countTrend = timePoints.map((time) => timeCountMap.get(time) || 0);

  const rpmTrend = [];
  const tpmTrend = [];

  if (timePoints.length >= 2) {
    const interval = getTimeInterval(dataExportDefaultTime);

    for (let i = 0; i < timePoints.length; i++) {
      rpmTrend.push(timeCountMap.get(timePoints[i]) / interval);
      tpmTrend.push(timeTokensMap.get(timePoints[i]) / interval);
    }
  }

  return {
    balance: [],
    usedQuota: [],
    requestCount: [],
    times: countTrend,
    consumeQuota: quotaTrend,
    tokens: tokensTrend,
    rpm: rpmTrend,
    tpm: tpmTrend,
  };
};

export const aggregateDataByTimeAndModel = (data, dataExportDefaultTime) => {
  const aggregatedData = new Map();

  // 检查数据是否跨年
  const showYear = isDataCrossYear(data.map((item) => item.created_at));

  data.forEach((item) => {
    const timeKey = timestamp2string1(
      item.created_at,
      dataExportDefaultTime,
      showYear,
    );
    const modelKey = item.model_name;
    const key = `${timeKey}-${modelKey}`;

    if (!aggregatedData.has(key)) {
      aggregatedData.set(key, {
        time: timeKey,
        model: modelKey,
        quota: 0,
        count: 0,
        token_used: 0,
      });
    }

    const existing = aggregatedData.get(key);
    existing.quota += item.quota;
    existing.count += item.count;
    existing.token_used += item.token_used || 0;
  });

  return aggregatedData;
};

export const generateChartTimePoints = (
  aggregatedData,
  data,
  dataExportDefaultTime,
) => {
  let chartTimePoints = Array.from(
    new Set([...aggregatedData.values()].map((d) => d.time)),
  );

  if (chartTimePoints.length < DEFAULTS.MAX_TREND_POINTS) {
    const lastTime = Math.max(...data.map((item) => item.created_at));
    const interval = getTimeInterval(dataExportDefaultTime, true);

    // 生成时间点数组，用于检查是否跨年
    const generatedTimestamps = Array.from(
      { length: DEFAULTS.MAX_TREND_POINTS },
      (_, i) => lastTime - (6 - i) * interval,
    );
    const showYear = isDataCrossYear(generatedTimestamps);

    chartTimePoints = generatedTimestamps.map((ts) =>
      timestamp2string1(ts, dataExportDefaultTime, showYear),
    );
  }

  return chartTimePoints;
};

// ========== 用户维度数据处理 ==========
export const processUserData = (data, dataExportDefaultTime, limit = 10) => {
  const userQuotaTotal = new Map();
  const userTokenTotal = new Map();
  data.forEach((item) => {
    const prevQuota = userQuotaTotal.get(item.username) || 0;
    userQuotaTotal.set(item.username, prevQuota + item.quota);
    const prevToken = userTokenTotal.get(item.username) || 0;
    userTokenTotal.set(item.username, prevToken + (item.token_used || 0));
  });

  const sorted = Array.from(userQuotaTotal.entries()).sort(
    (a, b) => b[1] - a[1],
  );
  const topUsers = sorted.slice(0, limit).map(([u]) => u);
  const topUserSet = new Set(topUsers);

  const rankingData = sorted.slice(0, limit).map(([username, quota]) => ({
    User: username,
    Quota: quota,
    Tokens: userTokenTotal.get(username) || 0,
  }));

  const showYear = isDataCrossYear(data.map((item) => item.created_at));

  const timeUserMap = new Map();
  const allTimePoints = new Set();

  data.forEach((item) => {
    const timeKey = timestamp2string1(
      item.created_at,
      dataExportDefaultTime,
      showYear,
    );
    allTimePoints.add(timeKey);
    const user = topUserSet.has(item.username) ? item.username : null;
    if (!user) return;
    const key = `${timeKey}-${user}`;
    const prev = timeUserMap.get(key) || { quota: 0 };
    timeUserMap.set(key, { quota: prev.quota + item.quota });
  });

  const sortedTimePoints = Array.from(allTimePoints).sort();
  const trendData = [];
  sortedTimePoints.forEach((time) => {
    topUsers.forEach((user) => {
      const key = `${time}-${user}`;
      const val = timeUserMap.get(key);
      trendData.push({
        Time: time,
        User: user,
        Quota: val?.quota || 0,
      });
    });
  });

  return { rankingData, trendData, topUsers };
};
