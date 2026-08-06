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

import React, { useEffect, useState, useSyncExternalStore } from 'react';
import { Button, Popover, Spin, Tag, Typography } from '@douyinfe/semi-ui';
import { IconCopy } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, copy, showError, showSuccess } from '../../../helpers';

const { Text } = Typography;

// Module-level cache: the backend already persists lookups per IP, this only
// avoids re-fetching within the same page session.
const ipInfoCache = new Map();
const ipInfoRequests = new Map();
const ipInfoCacheListeners = new Set();
let ipInfoCacheVersion = 0;

const subscribeIpInfoCache = (listener) => {
  ipInfoCacheListeners.add(listener);
  return () => ipInfoCacheListeners.delete(listener);
};

const getIpInfoCacheVersion = () => ipInfoCacheVersion;

const fetchIpInfo = (ip, version) => {
  const pending = ipInfoRequests.get(ip);
  if (pending?.version === version) return pending.request;

  const request = API.get(`/api/ip_info?ip=${encodeURIComponent(ip)}`)
    .then((res) => {
      if (version !== ipInfoCacheVersion) return null;
      const { success, data } = res.data;
      if (!success || !data) return null;
      ipInfoCache.set(ip, data);
      return data;
    })
    .finally(() => {
      if (ipInfoRequests.get(ip)?.request === request) {
        ipInfoRequests.delete(ip);
      }
    });

  ipInfoRequests.set(ip, { version, request });
  return request;
};

// Clears cached and in-flight results, then notifies mounted popovers to refetch.
export const clearIpInfoCache = () => {
  ipInfoCache.clear();
  ipInfoRequests.clear();
  ipInfoCacheVersion += 1;
  ipInfoCacheListeners.forEach((listener) => listener());
};

function InfoRow({ label, value }) {
  return (
    <div className='flex justify-between gap-4 py-0.5'>
      <Text type='tertiary' size='small'>
        {label}
      </Text>
      <Text size='small' strong>
        {value || '-'}
      </Text>
    </div>
  );
}

function IpInfoContent({ ip }) {
  const { t } = useTranslation();
  const cacheVersion = useSyncExternalStore(
    subscribeIpInfoCache,
    getIpInfoCacheVersion,
    getIpInfoCacheVersion,
  );
  const [requestState, setRequestState] = useState(null);
  const cachedInfo = ipInfoCache.get(ip) || null;
  const currentState = cachedInfo
    ? { status: 'success', info: cachedInfo }
    : requestState?.ip === ip && requestState.version === cacheVersion
      ? requestState
      : { status: 'loading', info: null };

  useEffect(() => {
    if (ipInfoCache.has(ip)) return;

    let cancelled = false;
    (async () => {
      try {
        const data = await fetchIpInfo(ip, cacheVersion);
        if (cancelled || cacheVersion !== ipInfoCacheVersion) return;
        setRequestState({
          ip,
          version: cacheVersion,
          status: data ? 'success' : 'failed',
          info: data,
        });
      } catch (e) {
        if (!cancelled && cacheVersion === ipInfoCacheVersion) {
          setRequestState({
            ip,
            version: cacheVersion,
            status: 'failed',
            info: null,
          });
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [cacheVersion, ip]);

  const handleCopy = async () => {
    if (await copy(ip)) {
      showSuccess(t('已复制') + ': ' + ip);
    } else {
      showError(t('复制失败，请手动复制'));
    }
  };

  return (
    <div style={{ minWidth: 220, padding: 4 }}>
      <div className='flex items-center justify-between gap-2 pb-1'>
        <Text
          strong
          style={{ fontFamily: 'monospace', wordBreak: 'break-all' }}
        >
          {ip}
        </Text>
        <Button
          icon={<IconCopy />}
          size='small'
          theme='borderless'
          onClick={handleCopy}
          aria-label={t('复制')}
        />
      </div>
      {currentState.status === 'loading' ? (
        <div className='flex justify-center py-3'>
          <Spin size='small' />
        </div>
      ) : currentState.status === 'failed' ? (
        <Text type='tertiary' size='small'>
          {t('归属地查询失败')}
        </Text>
      ) : currentState.info ? (
        <>
          <InfoRow label={t('大洲')} value={currentState.info.continent} />
          <InfoRow label={t('国家')} value={currentState.info.country} />
          <InfoRow label={t('省份')} value={currentState.info.province} />
          <InfoRow label={t('城市')} value={currentState.info.city} />
          {currentState.info.district ? (
            <InfoRow label={t('区/县')} value={currentState.info.district} />
          ) : null}
          {currentState.info.latitude && currentState.info.longitude ? (
            <InfoRow
              label={t('经纬度')}
              value={`${currentState.info.latitude}, ${currentState.info.longitude}`}
            />
          ) : null}
          <InfoRow label={t('运营商')} value={currentState.info.isp} />
          {currentState.info.org ? (
            <InfoRow label={t('组织')} value={currentState.info.org} />
          ) : null}
          {currentState.info.asn ? (
            <InfoRow label={t('ASN')} value={currentState.info.asn} />
          ) : null}
          {currentState.info.postal ? (
            <InfoRow label={t('邮编')} value={currentState.info.postal} />
          ) : null}
        </>
      ) : null}
    </div>
  );
}

/**
 * Clickable IP tag: clicking opens a popover with the IP's location info
 * (continent / country / province / city / ISP, cached server-side) plus a
 * copy button. Location is only fetched when the popover opens.
 */
const IpTag = ({ ip, color = 'orange' }) => {
  const [visible, setVisible] = useState(false);

  if (!ip) return null;

  return (
    <Popover
      trigger='custom'
      visible={visible}
      onClickOutSide={() => setVisible(false)}
      content={visible ? <IpInfoContent ip={ip} /> : null}
      showArrow
    >
      <span>
        <Tag
          color={color}
          shape='circle'
          style={{ cursor: 'pointer' }}
          onClick={(event) => {
            event.stopPropagation();
            setVisible((v) => !v);
          }}
        >
          {ip}
        </Tag>
      </span>
    </Popover>
  );
};

export default IpTag;
