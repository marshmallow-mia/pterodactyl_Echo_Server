@php
    use MarshmallowMia\EchoVrMatch\Services\Format;
    $m = $match['data'] ?? null;
    $active = $m && ($m['active'] ?? false);
    [$stateLabel, $stateColor] = Format::state($status['state'] ?? null);
    $cell = 'padding:.35rem .5rem;white-space:nowrap';
    $head = $cell . ';font-size:.75rem;font-weight:600;opacity:.7;text-align:left';
@endphp
<x-filament-panels::page>
    <div wire:poll.{{ $poll }} style="display:flex;flex-direction:column;gap:1.5rem">
        <x-filament::section>
            <div style="display:flex;flex-wrap:wrap;gap:1rem 2rem;align-items:center;justify-content:space-between">
                <div style="display:flex;flex-direction:column;gap:.35rem">
                    <div style="display:flex;gap:.5rem;flex-wrap:wrap">
                        @if ($active)
                            <x-filament::badge color="success" icon="tabler-broadcast">{{ trans('echovr-match::match.live') }}</x-filament::badge>
                        @else
                            <x-filament::badge :color="$stateColor">{{ $stateLabel }}</x-filament::badge>
                        @endif
                        <x-filament::badge :color="$apiOn ? 'gray' : 'warning'" icon="tabler-chart-bar">
                            {{ trans('echovr-match::match.api.label') }}: {{ trans($apiOn ? 'echovr-match::match.api.on' : 'echovr-match::match.api.off') }}
                        </x-filament::badge>
                        @if (!empty($m['private'])) <x-filament::badge color="gray">private</x-filament::badge> @endif
                        @if (!empty($m['tournament'])) <x-filament::badge color="gray">tournament</x-filament::badge> @endif
                    </div>
                    <div style="font-size:1.5rem;font-weight:700">{{ $m ? Format::title($m) : trans('echovr-match::match.no_match') }}</div>
                    @if ($m)
                        <div style="font-size:.8rem;opacity:.7">
                            {{ trans('echovr-match::match.session') }} {{ $m['session_id'] ?? '—' }}
                            · {{ trans('echovr-match::match.started') }} {{ Format::time($m['started_at'] ?? null) }}
                            @if (!$active && !empty($m['ended_at'])) · {{ trans('echovr-match::match.ended') }} {{ Format::when($m['ended_at']) }} @endif
                        </div>
                    @endif
                </div>
                @if ($m)
                    @include('echovr-match::partials.score', ['m' => $m, 'size' => '2.5rem'])
                @endif
            </div>
            @include('echovr-match::partials.notice')
        </x-filament::section>

        @if ($m && isset($m['blue'], $m['orange']))
            <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,28rem),1fr));gap:1.5rem">
                @include('echovr-match::partials.team', ['side' => 'blue', 'team' => $m['blue'], 'percent' => $m['percent'] ?? false])
                @include('echovr-match::partials.team', ['side' => 'orange', 'team' => $m['orange'], 'percent' => $m['percent'] ?? false])
            </div>
        @endif

        @if ($m && (!empty($m['last_goal']) || !empty($m['spectators']) || !empty($m['payload'])))
            <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,18rem),1fr));gap:1.5rem">
                @if (!empty($m['last_goal']))
                    @php $g = $m['last_goal']; @endphp
                    <x-filament::section :heading="trans('echovr-match::match.last_goal')">
                        <div style="font-weight:600;color:{{ Format::teamColor($g['team'] ?? null) }}">
                            {{ $g['scorer'] ?: trans('echovr-match::match.' . ($g['team'] ?? 'blue')) }} · {{ $g['points'] ?? 0 }}
                        </div>
                        <div style="font-size:.875rem;opacity:.8">
                            @if (!empty($g['type'])) {{ $g['type'] }} · @endif
                            {{ number_format($g['speed'] ?? 0, 1) }} m/s · {{ number_format($g['distance'] ?? 0, 1) }} m
                            @if (!empty($g['assist'])) · assist {{ $g['assist'] }} @endif
                        </div>
                    </x-filament::section>
                @endif
                @if (!empty($m['payload']))
                    @php $pl = $m['payload']; @endphp
                    <x-filament::section heading="Payload">
                        <div style="font-size:.875rem">
                            checkpoint {{ $pl['checkpoint'] ?? 0 }} · {{ number_format($pl['distance'] ?? 0, 1) }} m
                            · speed {{ number_format($pl['speed'] ?? 0, 2) }} · defenders {{ $pl['defenders'] ?? 0 }}
                        </div>
                    </x-filament::section>
                @endif
                @if (!empty($m['spectators']))
                    <x-filament::section :heading="trans('echovr-match::match.spectators')">
                        <div style="font-size:.875rem">{{ collect($m['spectators'])->pluck('name')->join(', ') }}</div>
                    </x-filament::section>
                @endif
            </div>
        @endif

        @if (!empty($m['events']))
            <x-filament::section :heading="trans('echovr-match::match.events')" collapsible>
                <ul style="display:flex;flex-direction:column;gap:.25rem;font-size:.875rem">
                    @foreach ($m['events'] as $e)
                        <li style="display:flex;gap:.75rem" wire:key="ev-{{ $loop->index }}-{{ $e['at'] }}">
                            <span style="opacity:.6;font-variant-numeric:tabular-nums;min-width:4.5rem">{{ Format::time($e['at'] ?? null) }}</span>
                            <span style="opacity:.6;font-variant-numeric:tabular-nums;min-width:5rem">{{ $e['clock'] ?? '' }}</span>
                            <span style="color:{{ Format::teamColor($e['team'] ?? null) }}">{{ $e['text'] }}</span>
                        </li>
                    @endforeach
                </ul>
            </x-filament::section>
        @endif

        <x-filament::section :heading="trans('echovr-match::match.history')" collapsible>
            @if (count($history) === 0)
                <p style="font-size:.875rem;opacity:.6">{{ trans('echovr-match::match.no_history') }}</p>
            @else
                <div style="overflow-x:auto">
                    <table style="width:100%;border-collapse:collapse;font-size:.875rem;font-variant-numeric:tabular-nums">
                        <thead>
                            <tr style="border-bottom:1px solid rgb(127 127 127 / .25)">
                                <th style="{{ $head }}">{{ trans('echovr-match::match.ended') }}</th>
                                <th style="{{ $head }}">{{ trans('echovr-match::match.mode') }}</th>
                                <th style="{{ $head }}">{{ trans('echovr-match::match.duration') }}</th>
                                <th style="{{ $head }}">{{ trans('echovr-match::match.score') }}</th>
                                <th style="{{ $head }}">{{ trans('echovr-match::match.winner') }}</th>
                                <th style="{{ $head }}">{{ trans('echovr-match::match.cols.player') }}</th>
                            </tr>
                        </thead>
                        <tbody>
                            @foreach ($history as $h)
                                <tr style="border-bottom:1px solid rgb(127 127 127 / .12)" wire:key="h-{{ $h['session_id'] }}">
                                    <td style="{{ $cell }}" title="{{ $h['ended_at'] ?? '' }}">{{ Format::when($h['ended_at'] ?? null) }}</td>
                                    <td style="{{ $cell }}">{{ Format::title($h) }}</td>
                                    <td style="{{ $cell }}">{{ Format::duration((int) ($h['duration_s'] ?? 0)) }}</td>
                                    <td style="{{ $cell }}">
                                        @if (($h['source'] ?? '') === 'session_api')
                                            <span style="color:{{ Format::BLUE }}">{{ Format::points($h['blue_points'] ?? 0, $h['percent'] ?? false) }}</span> :
                                            <span style="color:{{ Format::ORANGE }}">{{ Format::points($h['orange_points'] ?? 0, $h['percent'] ?? false) }}</span>
                                        @else
                                            —
                                        @endif
                                    </td>
                                    <td style="{{ $cell }};color:{{ Format::teamColor($h['winner'] ?? null) }}">
                                        {{ match ($h['winner'] ?? '') { 'blue' => trans('echovr-match::match.blue'), 'orange' => trans('echovr-match::match.orange'), 'tie' => trans('echovr-match::match.tie'), default => '—' } }}
                                    </td>
                                    <td style="{{ $cell }};white-space:normal">
                                        {{ collect($h['players'] ?? [])->where('team', '!=', 'spectator')->pluck('name')->join(', ') ?: '—' }}
                                    </td>
                                </tr>
                            @endforeach
                        </tbody>
                    </table>
                </div>
            @endif
        </x-filament::section>
    </div>
</x-filament-panels::page>
