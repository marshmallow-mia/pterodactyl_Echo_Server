@php
    use MarshmallowMia\EchoVrMatch\Services\Format;
    $cell = 'padding:.5rem .6rem;white-space:nowrap';
    $head = $cell . ';font-size:.75rem;font-weight:600;opacity:.7;text-align:left';
@endphp
<x-filament-panels::page>
    <div wire:poll.{{ $poll }}>
        <x-filament::section>
            @if (count($rows) === 0)
                <p style="font-size:.875rem;opacity:.6">{{ trans('echovr-match::match.no_servers') }}</p>
            @else
                <div style="overflow-x:auto">
                    <table style="width:100%;border-collapse:collapse;font-size:.875rem;font-variant-numeric:tabular-nums">
                        <thead>
                            <tr style="border-bottom:1px solid rgb(127 127 127 / .25)">
                                <th style="{{ $head }}">{{ trans('echovr-match::match.server') }}</th>
                                <th style="{{ $head }}">{{ trans('echovr-match::match.node') }}</th>
                                <th style="{{ $head }}">{{ trans('echovr-match::match.state') }}</th>
                                <th style="{{ $head }}">{{ trans('echovr-match::match.mode') }}</th>
                                <th style="{{ $head }}">{{ trans('echovr-match::match.score') }}</th>
                                <th style="{{ $head }}">{{ trans('echovr-match::match.clock') }}</th>
                                <th style="{{ $head }}">{{ trans('echovr-match::match.cols.player') }}</th>
                                <th style="{{ $head }}">{{ trans('echovr-match::match.api.label') }}</th>
                                <th style="{{ $head }}">{{ trans('echovr-match::match.health.serverdb') }}</th>
                                <th style="{{ $head }}"></th>
                            </tr>
                        </thead>
                        <tbody>
                            @foreach ($rows as $r)
                                @php
                                    $m = $r['match']['data'] ?? null;
                                    $active = $m && ($m['active'] ?? false);
                                    [$stateLabel, $stateColor] = Format::state($r['status']['state'] ?? null);
                                    $api = $r['status']['session_api'] ?? null;
                                @endphp
                                <tr style="border-bottom:1px solid rgb(127 127 127 / .12)" wire:key="srv-{{ $r['server']->uuid }}">
                                    <td style="{{ $cell }};font-weight:500">
                                        {{ $r['server']->name }}
                                        <div style="font-size:.75rem;opacity:.6">{{ $r['server']->allocation?->port }}</div>
                                    </td>
                                    <td style="{{ $cell }}">{{ $r['server']->node?->name }}</td>
                                    <td style="{{ $cell }}">
                                        @if ($r['match']['error'] === \MarshmallowMia\EchoVrMatch\Services\MatchReader::UNREACHABLE && !$r['status'])
                                            <x-filament::badge color="danger">Wings</x-filament::badge>
                                        @else
                                            <x-filament::badge :color="$stateColor">{{ $stateLabel }}</x-filament::badge>
                                        @endif
                                    </td>
                                    <td style="{{ $cell }}">
                                        @if ($active) {{ Format::title($m) }}
                                        @elseif ($m) <span style="opacity:.6">{{ Format::title($m) }} · {{ Format::when($m['ended_at'] ?? null) }}</span>
                                        @else — @endif
                                    </td>
                                    <td style="{{ $cell }}">
                                        @if ($m && isset($m['blue'], $m['orange']) && !Format::isLobby($m))
                                            <span style="font-weight:600;color:{{ Format::BLUE }}">{{ Format::points($m['blue']['points'] ?? 0, $m['percent'] ?? false) }}</span> :
                                            <span style="font-weight:600;color:{{ Format::ORANGE }}">{{ Format::points($m['orange']['points'] ?? 0, $m['percent'] ?? false) }}</span>
                                        @else — @endif
                                    </td>
                                    <td style="{{ $cell }}">
                                        @if ($active)
                                            @if (!empty($m['round']) && empty($m['clock_display'])) {{ trans('echovr-match::match.round_n', ['n' => $m['round']]) }} @endif
                                            {{ Format::clock($m) }} <span style="opacity:.6">{{ Format::phase($m) }}</span>
                                        @endif
                                    </td>
                                    <td style="{{ $cell }}">{{ $active && ($m['players'] ?? 0) > 0 ? $m['players'] : '' }}</td>
                                    <td style="{{ $cell }}">{{ $api ?? '—' }}</td>
                                    <td style="{{ $cell }}">
                                        @php $db = $r['status']['serverdb'] ?? ''; @endphp
                                        @if ($db === 'connected')
                                            <x-filament::badge color="success">{{ trans('echovr-match::match.health.connected') }}</x-filament::badge>
                                        @elseif ($db === 'disconnected')
                                            <x-filament::badge color="danger">{{ trans('echovr-match::match.health.disconnected') }}</x-filament::badge>
                                        @else — @endif
                                        @if (!empty($r['status']['crash_recoveries']))
                                            <div style="color:#ef4444;font-size:.75rem">{{ trans('echovr-match::match.health.crashes', ['n' => $r['status']['crash_recoveries']]) }}</div>
                                        @endif
                                    </td>
                                    <td style="{{ $cell }}">
                                        @if ($r['url'])
                                            <x-filament::link :href="$r['url']" icon="tabler-trophy">{{ trans('echovr-match::match.open') }}</x-filament::link>
                                        @endif
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
