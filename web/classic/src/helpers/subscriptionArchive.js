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

export function partitionSubscriptionArchive(subscriptions, now) {
  const currentSubscriptions = [];
  const archivedSubscriptions = [];
  let activeCount = 0;
  let inactiveCount = 0;
  let nextExpiry = 0;

  for (const summary of subscriptions || []) {
    const subscription = summary?.subscription;
    const endTime = Number(subscription?.end_time || 0);
    const status = subscription?.status;
    const isCurrent =
      (status === 'active' || status === 'inactive') &&
      Number.isFinite(endTime) &&
      endTime > now;
    if (!isCurrent) {
      archivedSubscriptions.push(summary);
      continue;
    }

    currentSubscriptions.push(summary);
    if (status === 'active') activeCount++;
    else inactiveCount++;
    if (!nextExpiry || endTime < nextExpiry) nextExpiry = endTime;
  }

  return {
    currentSubscriptions,
    archivedSubscriptions,
    activeCount,
    inactiveCount,
    nextExpiry,
  };
}
