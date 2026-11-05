@php
    use MarshmallowMia\EchoVrMatch\Services\Format;
    $st = $status ?? [];
    $db = $st['serverdb'] ?? '';
    $exits = $st['exits'] ?? [];
    $cell = 'padding:.3rem .5rem;white-space:nowrap';
@endphp
@if ($st)
    <x-filament::section :heading="trans('echovr-match::match.health.title')" collapsible collapsed>
        <dl style="display:grid;grid-template-columns:repeat(auto-fit,minmax(12rem,1fr));gap:.75rem 1.5rem;font-size:.875rem">
            <div>
                <dt style="font-size:.75rem;opacity:.6">{{ trans('echovr-match::match.health.serverdb') }}</dt>
                <dd>
                    @if ($db === 'connected')
                        <x-filament::badge color="success">{{ trans('echovr-match::match.health.connected') }}</x-filament::badge>
                    @elseif ($db === 'disconnected')
                        <x-filament::badge color="danger">{{ trans('echovr-match::match.health.disconnected') }}</x-filament::badge>
                    @else
                        —
                    @endif
                    @if (!empty($st['serverdb_since'])) <span style="opacity:.6">{{ Format::when($st['serverdb_since']) }}</span> @endif
                    @if (!empty($st['serverdb_reconnects'])) <div style="opacity:.75">{{ trans('echovr-match::match.health.reconnects', ['n' => $st['serverdb_reconnects']]) }}</div> @endif
                </dd>
            </div>
            <div>
                <dt style="font-size:.75rem;opacity:.6">{{ trans('echovr-match::match.health.server_id') }}</dt>
                <dd style="font-variant-numeric:tabular-nums">{{ $st['server_id'] ?? '—' }}</dd>
            </div>
            <div>
                <dt style="font-size:.75rem;opacity:.6">{{ trans('echovr-match::match.health.runtime') }}</dt>
                <dd>{{ $st['runtime_version'] ?? '—' }} · {{ $st['public_ip'] ?? '' }}:{{ $st['port'] ?? '' }}</dd>
            </div>
            <div>
                <dt style="font-size:.75rem;opacity:.6">{{ trans('echovr-match::match.health.telemetry') }}</dt>
                <dd>
                    @if (($st['telemetry'] ?? '') === 'on')
                        <x-filament::badge :color="!empty($st['telemetry_connected']) ? 'success' : 'warning'">
                            {{ trans(!empty($st['telemetry_connected']) ? 'echovr-match::match.health.connected' : 'echovr-match::match.health.disconnected') }}
                        </x-filament::badge>
                    @else
                        {{ trans('echovr-match::match.api.off') }}
                    @endif
                </dd>
            </div>
            <div>
                <dt style="font-size:.75rem;opacity:.6">{{ trans('echovr-match::match.health.counters') }}</dt>
                <dd>
                    {{ trans('echovr-match::match.health.sessions', ['n' => $st['sessions_total'] ?? 0]) }}
                    · {{ trans('echovr-match::match.health.restarts', ['n' => $st['restarts'] ?? 0]) }}
                    @if (!empty($st['crash_recoveries']))
                        · <span style="color:#ef4444">{{ trans('echovr-match::match.health.crashes', ['n' => $st['crash_recoveries']]) }}</span>
                    @endif
                </dd>
            </div>
        </dl>
        @if (!empty($st['last_error']))
            <p style="margin-top:.75rem;font-size:.875rem;color:#ef4444">{{ $st['last_error'] }}</p>
        @endif
        @if (count($exits) > 0)
            <div style="margin-top:1rem;overflow-x:auto">
                <table style="width:100%;border-collapse:collapse;font-size:.8rem;font-variant-numeric:tabular-nums">
                    <thead>
                        <tr style="border-bottom:1px solid rgb(127 127 127 / .25);text-align:left;opacity:.7">
                            <th style="{{ $cell }}">{{ trans('echovr-match::match.health.exit_at') }}</th>
                            <th style="{{ $cell }}">{{ trans('echovr-match::match.health.exit_code') }}</th>
                            <th style="{{ $cell }}">{{ trans('echovr-match::match.health.uptime') }}</th>
                            <th style="{{ $cell }}">{{ trans('echovr-match::match.health.why') }}</th>
                        </tr>
                    </thead>
                    <tbody>
                        @foreach ($exits as $e)
                            <tr style="border-bottom:1px solid rgb(127 127 127 / .12)" wire:key="exit-{{ $e['at'] }}">
                                <td style="{{ $cell }}" title="{{ $e['at'] }}">{{ Format::when($e['at']) }}</td>
                                <td style="{{ $cell }};color:{{ ($e['clean'] ?? false) ? 'inherit' : '#ef4444' }}">{{ $e['code'] }}</td>
                                <td style="{{ $cell }}">{{ Format::duration((int) ($e['uptime_s'] ?? 0)) }}</td>
                                <td style="{{ $cell }};white-space:normal">{{ ($e['clean'] ?? false) ? trans('echovr-match::match.health.clean') : ($e['reason'] ?? '') }}</td>
                            </tr>
                        @endforeach
                    </tbody>
                </table>
            </div>
        @endif
    </x-filament::section>
@endif
