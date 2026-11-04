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

import { useEffect, useState } from 'react';
import { partitionSubscriptionArchive } from '../../helpers/subscriptionArchive';

export function useSubscriptionArchive(subscriptions) {
  const [clockNow, setClockNow] = useState(() => Date.now() / 1000);
  const now = Math.max(clockNow, Date.now() / 1000);
  const partition = partitionSubscriptionArchive(subscriptions, now);
  const { nextExpiry } = partition;

  useEffect(() => {
    if (!nextExpiry) return;
    const delay = Math.min(
      Math.max(1, nextExpiry * 1000 - Date.now()),
      2147483647,
    );
    const timer = setTimeout(() => setClockNow(Date.now() / 1000), delay);
    return () => clearTimeout(timer);
  }, [nextExpiry, clockNow]);

  return { ...partition, now };
}
