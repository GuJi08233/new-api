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

import React, { useState } from 'react';
import { Button, Popover, Spin, Tag, Typography } from '@douyinfe/semi-ui';
import { IconCopy } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, copy, showError, showSuccess } from '../../../helpers';

const { Text } = Typography;

// Module-level cache: the backend already persists lookups per IP, this only
// avoids re-fetching within the same page session.
const ipInfoCache = new Map();

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
  const [loading, setLoading] = useState(!ipInfoCache.has(ip));
  const [info, setInfo] = useState(ipInfoCache.get(ip) || null);
  const [failed, setFailed] = useState(false);

  React.useEffect(() => {
    if (ipInfoCache.has(ip)) return;
    let cancelled = false;
    (async () => {
      try {
        const res = await API.get(`/api/ip_info?ip=${encodeURIComponent(ip)}`);
        const { success, data } = res.data;
        if (cancelled) return;
        if (success && data) {
          ipInfoCache.set(ip, data);
          setInfo(data);
        } else {
          setFailed(true);
        }
      } catch (e) {
        if (!cancelled) setFailed(true);
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [ip]);

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
      {loading ? (
        <div className='flex justify-center py-3'>
          <Spin size='small' />
        </div>
      ) : failed ? (
        <Text type='tertiary' size='small'>
          {t('归属地查询失败')}
        </Text>
      ) : info ? (
        <>
          <InfoRow label={t('大洲')} value={info.continent} />
          <InfoRow label={t('国家')} value={info.country} />
          <InfoRow label={t('省份')} value={info.province} />
          <InfoRow label={t('城市')} value={info.city} />
          {info.district ? <InfoRow label={t('区/县')} value={info.district} /> : null}
          {info.latitude && info.longitude ? (
            <InfoRow label={t('经纬度')} value={`${info.latitude}, ${info.longitude}`} />
          ) : null}
          <InfoRow label={t('运营商')} value={info.isp} />
          {info.org ? <InfoRow label={t('组织')} value={info.org} /> : null}
          {info.asn ? <InfoRow label={t('ASN')} value={info.asn} /> : null}
          {info.postal ? <InfoRow label={t('邮编')} value={info.postal} /> : null}
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
