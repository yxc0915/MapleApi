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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  CircleSlash2,
  ShieldAlert,
  ShieldCheck,
  SlidersHorizontal,
} from 'lucide-react'
import { useEffect, useMemo, type ReactNode } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import {
  MultiSelect,
  type Option as MultiSelectOption,
} from '@/components/multi-select'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { formatTimestampToDate } from '@/lib/format'

import {
  getSensitiveDetectionChannels,
  getSensitiveDetectionStats,
  updateSensitiveDetectionChannels,
  updateSystemOption,
} from '../api'
import {
  SettingsForm,
  SettingsFormGrid,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import type {
  SensitiveDetectionCounter,
  SensitiveDetectionRecentLog,
  SensitiveDetectionStats,
  SensitiveDetectionStatus,
  UpdateOptionRequest,
} from '../types'

const detectionSchema = z.object({
  SensitiveDetectionModel: z.string().optional(),
  SensitiveDetectionBaseURL: z.string().optional(),
  SensitiveDetectionAPIKey: z.string().optional(),
  SensitiveDetectionPrompt: z.string().optional(),
  SensitiveDetectionGroups: z.array(z.string()),
  SensitiveDetectionChannelIDs: z.array(z.string()),
})

type DetectionFormValues = z.infer<typeof detectionSchema>

type SensitiveWordsSectionProps = {
  defaultValues: {
    SensitiveDetectionModel: string
    SensitiveDetectionBaseURL: string
    SensitiveDetectionAPIKey: string
    SensitiveDetectionPrompt: string
    SensitiveDetectionGroups: string
    GroupRatio: string
  }
}

const EMPTY_STATS: SensitiveDetectionStats = {
  normal_count: 0,
  illegal_count: 0,
  allowed_count: 0,
  bypassed_count: 0,
  error_open_count: 0,
  top_objects: [],
  channel_stats: [],
  group_stats: [],
  recent_blocked: [],
}

function parseStringArray(value: string | undefined): string[] {
  if (!value) return []
  try {
    const parsed = JSON.parse(value)
    if (!Array.isArray(parsed)) return []
    return parsed
      .map((item) => String(item).trim())
      .filter((item) => item.length > 0)
  } catch {
    return []
  }
}

function parseGroupNames(groupRatio: string, selectedGroups: string[]) {
  const names = new Set(selectedGroups)
  try {
    const parsed = JSON.parse(groupRatio || '{}')
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      Object.keys(parsed).forEach((name) => names.add(name))
    }
  } catch {
    // Keep selected groups visible even if ratio JSON is temporarily invalid.
  }
  if (names.size === 0) names.add('default')
  return [...names].sort((a, b) => a.localeCompare(b))
}

function normalizeList(values: string[]) {
  return [...new Set(values.map((value) => value.trim()).filter(Boolean))].sort(
    (a, b) => a.localeCompare(b)
  )
}

function serializeList(values: string[]) {
  return JSON.stringify(normalizeList(values))
}

function formatCount(value: number | undefined) {
  return new Intl.NumberFormat().format(value ?? 0)
}

function truncateMiddle(value: string | undefined, maxLength = 80) {
  if (!value) return '-'
  if (value.length <= maxLength) return value
  return `${value.slice(0, maxLength - 1)}…`
}

function detectionStatusMeta(status?: SensitiveDetectionStatus): {
  label: string
  variant: StatusVariant
} {
  switch (status) {
    case 'allowed':
      return { label: 'Passed', variant: 'success' }
    case 'blocked':
      return { label: 'Blocked', variant: 'danger' }
    case 'bypassed':
      return { label: 'Bypassed', variant: 'neutral' }
    case 'error_open':
      return { label: 'Failed open', variant: 'warning' }
    default:
      return { label: 'Unmarked', variant: 'neutral' }
  }
}

function DetectionStatusBadge({
  status,
}: {
  status?: SensitiveDetectionStatus
}) {
  const { t } = useTranslation()
  const meta = detectionStatusMeta(status)
  return (
    <StatusBadge
      label={t(meta.label)}
      variant={meta.variant}
      size='sm'
      copyable={false}
    />
  )
}

function StatCard({
  title,
  value,
  icon,
  description,
}: {
  title: string
  value: number
  icon: ReactNode
  description: string
}) {
  const { t } = useTranslation()
  return (
    <Card>
      <CardContent className='flex items-start justify-between gap-3 p-4'>
        <div className='min-w-0 space-y-1'>
          <p className='text-muted-foreground text-sm'>{t(title)}</p>
          <p className='text-2xl font-semibold tabular-nums'>
            {formatCount(value)}
          </p>
          <p className='text-muted-foreground text-xs'>{t(description)}</p>
        </div>
        <div className='bg-muted text-muted-foreground rounded-md p-2'>
          {icon}
        </div>
      </CardContent>
    </Card>
  )
}

function CounterTable({
  title,
  description,
  data,
  keyHeader,
  emptyText,
}: {
  title: string
  description: string
  data: SensitiveDetectionCounter[]
  keyHeader: string
  emptyText: string
}) {
  const { t } = useTranslation()
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t(title)}</CardTitle>
        <CardDescription>{t(description)}</CardDescription>
      </CardHeader>
      <CardContent>
        <StaticDataTable
          data={data}
          getRowKey={(row) => row.key}
          emptyContent={t(emptyText)}
          emptyClassName='text-muted-foreground h-20 text-sm'
          columns={[
            {
              id: 'key',
              header: t(keyHeader),
              className: 'min-w-32',
              cell: (row) => (
                <span className='block truncate' title={row.name || row.key}>
                  {truncateMiddle(row.name || row.key, 56)}
                </span>
              ),
            },
            {
              id: 'normal',
              header: t('Normal'),
              className: 'w-24 text-right',
              cellClassName: 'text-right tabular-nums',
              cell: (row) => formatCount(row.normal_count),
            },
            {
              id: 'illegal',
              header: t('Illegal'),
              className: 'w-24 text-right',
              cellClassName: 'text-right tabular-nums',
              cell: (row) => formatCount(row.illegal_count),
            },
          ]}
        />
      </CardContent>
    </Card>
  )
}

function RecentViolationsTable({
  rows,
}: {
  rows: SensitiveDetectionRecentLog[]
}) {
  const { t } = useTranslation()
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Recent violations')}</CardTitle>
        <CardDescription>
          {t('Latest requests blocked by violation detection.')}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <StaticDataTable
          data={rows}
          getRowKey={(row) => row.id}
          emptyContent={t('No violations recorded yet.')}
          emptyClassName='text-muted-foreground h-24 text-sm'
          columns={[
            {
              id: 'time',
              header: t('Time'),
              className: 'min-w-36',
              cell: (row) => formatTimestampToDate(row.created_at),
            },
            {
              id: 'status',
              header: t('Status'),
              className: 'w-28',
              cell: (row) => (
                <DetectionStatusBadge status={row.sensitive_detection_status} />
              ),
            },
            {
              id: 'target',
              header: t('Target'),
              className: 'min-w-44',
              cell: (row) => (
                <div className='min-w-0'>
                  <p className='truncate font-medium'>
                    {row.username || `#${row.id}`}
                  </p>
                  <p className='text-muted-foreground truncate text-xs'>
                    {row.token_name || row.group || '-'}
                  </p>
                </div>
              ),
            },
            {
              id: 'route',
              header: t('Route'),
              className: 'min-w-44',
              cell: (row) => (
                <div className='min-w-0'>
                  <p className='truncate'>{row.model_name || '-'}</p>
                  <p className='text-muted-foreground truncate text-xs'>
                    {row.channel_name || `#${row.channel || '-'}`}
                  </p>
                </div>
              ),
            },
            {
              id: 'trigger',
              header: t('Trigger'),
              className: 'w-32',
              cell: (row) => row.sensitive_detection_trigger || '-',
            },
            {
              id: 'reason',
              header: t('Reason'),
              className: 'min-w-56',
              cell: (row) => (
                <span
                  className='block truncate'
                  title={
                    row.sensitive_detection_reason ||
                    row.sensitive_detection_objects ||
                    ''
                  }
                >
                  {truncateMiddle(
                    row.sensitive_detection_reason ||
                      row.sensitive_detection_objects,
                    72
                  )}
                </span>
              ),
            },
          ]}
        />
      </CardContent>
    </Card>
  )
}

export function SensitiveWordsSection({
  defaultValues,
}: SensitiveWordsSectionProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const selectedGroups = useMemo(
    () => parseStringArray(defaultValues.SensitiveDetectionGroups),
    [defaultValues.SensitiveDetectionGroups]
  )

  const statsQuery = useQuery({
    queryKey: ['sensitive-detection-stats'],
    queryFn: () => getSensitiveDetectionStats(10),
  })
  const channelsQuery = useQuery({
    queryKey: ['sensitive-detection-channels'],
    queryFn: getSensitiveDetectionChannels,
  })

  const channels = channelsQuery.data?.data ?? []
  const selectedChannelIds = useMemo(
    () =>
      channels
        .filter((channel) => channel.enabled)
        .map((channel) => String(channel.id)),
    [channels]
  )

  const formDefaults = useMemo<DetectionFormValues>(
    () => ({
      SensitiveDetectionModel: defaultValues.SensitiveDetectionModel || '',
      SensitiveDetectionBaseURL: defaultValues.SensitiveDetectionBaseURL || '',
      SensitiveDetectionAPIKey: '',
      SensitiveDetectionPrompt: defaultValues.SensitiveDetectionPrompt || '',
      SensitiveDetectionGroups: selectedGroups,
      SensitiveDetectionChannelIDs: selectedChannelIds,
    }),
    [defaultValues, selectedGroups, selectedChannelIds]
  )

  const form = useForm<DetectionFormValues>({
    resolver: zodResolver(detectionSchema),
    defaultValues: formDefaults,
  })

  useEffect(() => {
    form.reset(formDefaults)
  }, [form, formDefaults])

  const groupOptions = useMemo<MultiSelectOption[]>(() => {
    return parseGroupNames(defaultValues.GroupRatio, selectedGroups).map(
      (group) => ({
        label: group,
        value: group,
      })
    )
  }, [defaultValues.GroupRatio, selectedGroups])

  const channelOptions = useMemo<MultiSelectOption[]>(() => {
    return channels.map((channel) => ({
      label: `#${channel.id} ${channel.name}`,
      value: String(channel.id),
    }))
  }, [channels])

  const saveMutation = useMutation({
    mutationFn: async (values: DetectionFormValues) => {
      const updates: UpdateOptionRequest[] = []
      const groupValue = serializeList(values.SensitiveDetectionGroups)
      const currentGroupValue = serializeList(selectedGroups)

      const queueUpdate = (
        key: keyof SensitiveWordsSectionProps['defaultValues'],
        value: string,
        current: string
      ) => {
        if (value !== current) {
          updates.push({ key, value })
        }
      }

      queueUpdate(
        'SensitiveDetectionModel',
        values.SensitiveDetectionModel || '',
        defaultValues.SensitiveDetectionModel || ''
      )
      queueUpdate(
        'SensitiveDetectionBaseURL',
        values.SensitiveDetectionBaseURL || '',
        defaultValues.SensitiveDetectionBaseURL || ''
      )
      queueUpdate(
        'SensitiveDetectionPrompt',
        values.SensitiveDetectionPrompt || '',
        defaultValues.SensitiveDetectionPrompt || ''
      )
      if (values.SensitiveDetectionAPIKey?.trim()) {
        updates.push({
          key: 'SensitiveDetectionAPIKey',
          value: values.SensitiveDetectionAPIKey.trim(),
        })
      }
      if (groupValue !== currentGroupValue) {
        updates.push({ key: 'SensitiveDetectionGroups', value: groupValue })
      }

      for (const update of updates) {
        const response = await updateSystemOption(update)
        if (!response.success) {
          throw new Error(response.message || t('Failed to update setting'))
        }
      }

      const nextChannelIds = normalizeList(values.SensitiveDetectionChannelIDs)
      if (serializeList(nextChannelIds) !== serializeList(selectedChannelIds)) {
        const response = await updateSensitiveDetectionChannels(
          nextChannelIds.map((id) => Number(id)).filter(Number.isFinite)
        )
        if (!response.success) {
          throw new Error(response.message || t('Failed to update channels'))
        }
      }
    },
    onSuccess: () => {
      toast.success(t('Violation detection settings saved'))
      queryClient.invalidateQueries({ queryKey: ['system-options'] })
      queryClient.invalidateQueries({
        queryKey: ['sensitive-detection-channels'],
      })
      queryClient.invalidateQueries({ queryKey: ['sensitive-detection-stats'] })
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to save violation detection'))
    },
  })

  const stats = statsQuery.data?.data ?? EMPTY_STATS

  const scrollToConfig = () => {
    document
      .getElementById('sensitive-detection-config')
      ?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  return (
    <SettingsSection title={t('Violation Detection')}>
      <div className='space-y-6'>
        <div className='flex justify-end'>
          <Button variant='outline' size='sm' onClick={scrollToConfig}>
            <SlidersHorizontal className='mr-2 h-4 w-4' />
            {t('Detection configuration')}
          </Button>
        </div>

        <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
          <StatCard
            title='Normal requests'
            value={stats.normal_count}
            description='Requests forwarded upstream'
            icon={<ShieldCheck className='h-5 w-5' />}
          />
          <StatCard
            title='Illegal requests'
            value={stats.illegal_count}
            description='Requests blocked before upstream'
            icon={<ShieldAlert className='h-5 w-5' />}
          />
          <StatCard
            title='Detection passed'
            value={stats.allowed_count}
            description='Checked by the detector and passed'
            icon={<CheckCircle2 className='h-5 w-5' />}
          />
          <StatCard
            title='Failed open'
            value={stats.error_open_count}
            description='Detector failures that were forwarded'
            icon={<AlertTriangle className='h-5 w-5' />}
          />
        </div>

        <RecentViolationsTable rows={stats.recent_blocked ?? []} />

        <div className='grid gap-4 xl:grid-cols-3'>
          <CounterTable
            title='Top flagged objects'
            description='Most frequent detector labels among blocked requests.'
            data={stats.top_objects ?? []}
            keyHeader='Object'
            emptyText='No flagged objects yet.'
          />
          <CounterTable
            title='Channel detection stats'
            description='Forwarded and blocked requests grouped by channel.'
            data={stats.channel_stats ?? []}
            keyHeader='Channel'
            emptyText='No channel statistics yet.'
          />
          <CounterTable
            title='Group detection stats'
            description='Forwarded and blocked requests grouped by user group.'
            data={stats.group_stats ?? []}
            keyHeader='Group'
            emptyText='No group statistics yet.'
          />
        </div>

        <Card id='sensitive-detection-config' className='scroll-mt-6'>
          <CardHeader>
            <CardTitle>{t('Detection configuration')}</CardTitle>
            <CardDescription>
              {t(
                'Configure the OpenAI-compatible detector and enabled scopes.'
              )}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Form {...form}>
              <SettingsForm
                onSubmit={form.handleSubmit((values) =>
                  saveMutation.mutate(values)
                )}
              >
                <SettingsPageFormActions
                  onSave={form.handleSubmit((values) =>
                    saveMutation.mutate(values)
                  )}
                  isSaving={saveMutation.isPending}
                  saveLabel='Save violation detection'
                />

                <SettingsFormGrid>
                  <FormField
                    control={form.control}
                    name='SensitiveDetectionChannelIDs'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Enabled channels')}</FormLabel>
                        <FormControl>
                          <MultiSelect
                            options={channelOptions}
                            selected={field.value}
                            onChange={field.onChange}
                            placeholder={t('Select channels...')}
                            emptyText={t('No channels found.')}
                            maxVisibleChips={6}
                            disabled={channelsQuery.isLoading}
                          />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Requests using any selected channel will be checked.'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='SensitiveDetectionGroups'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Enabled groups')}</FormLabel>
                        <FormControl>
                          <MultiSelect
                            options={groupOptions}
                            selected={field.value}
                            onChange={field.onChange}
                            placeholder={t('Select groups...')}
                            emptyText={t('No groups found.')}
                            maxVisibleChips={6}
                          />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Requests from any selected group will be checked.'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='SensitiveDetectionModel'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Detector model ID')}</FormLabel>
                        <FormControl>
                          <Input placeholder='gpt-4.1-mini' {...field} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='SensitiveDetectionBaseURL'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Detector base URL')}</FormLabel>
                        <FormControl>
                          <Input
                            placeholder='https://api.openai.com/v1'
                            {...field}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='SensitiveDetectionAPIKey'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Detector API key')}</FormLabel>
                        <FormControl>
                          <Input
                            type='password'
                            placeholder={t('Leave blank to keep current key')}
                            autoComplete='new-password'
                            {...field}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='SensitiveDetectionPrompt'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Violation detection prompt')}</FormLabel>
                        <FormControl>
                          <Textarea
                            rows={10}
                            placeholder={t(
                              'Return JSON only. Use status 200 for allowed requests.'
                            )}
                            {...field}
                          />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Only JSON content is accepted from the detector response.'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </SettingsFormGrid>
              </SettingsForm>
            </Form>
          </CardContent>
        </Card>

        {stats.bypassed_count > 0 && (
          <div className='text-muted-foreground flex items-center gap-2 text-sm'>
            <CircleSlash2 className='h-4 w-4' />
            <span>
              {t('{{count}} requests bypassed detection by scope or format.', {
                count: formatCount(stats.bypassed_count),
              })}
            </span>
          </div>
        )}

        {statsQuery.isFetching && (
          <div className='text-muted-foreground flex items-center gap-2 text-sm'>
            <Activity className='h-4 w-4 animate-pulse' />
            <span>{t('Refreshing detection data...')}</span>
          </div>
        )}
      </div>
    </SettingsSection>
  )
}
