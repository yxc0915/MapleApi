/*
Copyright (C) 2023-2026 QuantumNous

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
import type { StatusBadgeProps } from '@/components/status-badge'

export const SENSITIVE_DETECTION_ALL_VALUE = 'all'

export const SENSITIVE_DETECTION_STATUS_FILTERS = [
  { value: SENSITIVE_DETECTION_ALL_VALUE, label: 'All Detection Statuses' },
  { value: 'allowed', label: 'Allowed' },
  { value: 'blocked', label: 'Blocked' },
  { value: 'bypassed', label: 'Bypassed' },
  { value: 'error_open', label: 'Failed open' },
] as const

export function getSensitiveDetectionStatusMeta(status?: string): {
  label: string
  variant: StatusBadgeProps['variant']
} {
  switch (status) {
    case 'allowed':
      return { label: 'Allowed', variant: 'success' }
    case 'blocked':
      return { label: 'Blocked', variant: 'danger' }
    case 'bypassed':
      return { label: 'Bypassed', variant: 'neutral' }
    case 'error_open':
      return { label: 'Failed open', variant: 'warning' }
    default:
      return { label: 'Historical unmarked', variant: 'neutral' }
  }
}
