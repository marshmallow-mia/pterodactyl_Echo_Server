@php
    use MarshmallowMia\EchoVrMatch\Services\Format;
    $players = $team['players'] ?? [];
    $combat = collect($players)->contains(fn ($p) => !empty($p['weapon']) || !empty($p['ordnance']) || !empty($p['tac_mod']));
    $cell = 'padding:.35rem .5rem;text-align:right;white-space:nowrap';
    $head = $cell . ';font-size:.75rem;font-weight:600;opacity:.7';
    $hasStats = collect($players)->contains(fn ($p) => !empty($p['stats']));
    $stats = !$hasStats ? [] : ($combat ? ['points', 'stuns', 'saves', 'blocks'] : ['points', 'goals', 'assists', 'saves', 'stuns', 'steals', 'blocks', 'passes']);
@endphp
<x-filament::section>
    <x-slot name="heading">
        <span style="color:{{ Format::teamColor($side) }}">{{ trans('echovr-match::match.' . $side) }}</span>
        <span style="margin-left:.5rem;font-variant-numeric:tabular-nums">{{ \MarshmallowMia\EchoVrMatch\Services\Format::points($team['points'] ?? 0, $percent ?? false) }}</span>
        @if (!empty($team['possession']))
            <x-filament::icon icon="tabler-disc" style="display:inline-block;width:1rem;height:1rem;vertical-align:middle;margin-left:.35rem" />
        @endif
    </x-slot>
    @if (count($players) === 0)
        <p style="font-size:.875rem;opacity:.6">—</p>
    @else
        <div style="overflow-x:auto">
            <table style="width:100%;border-collapse:collapse;font-size:.875rem;font-variant-numeric:tabular-nums">
                <thead>
                    <tr style="border-bottom:1px solid rgb(127 127 127 / .25)">
                        <th style="{{ $head }}">{{ trans('echovr-match::match.cols.number') }}</th>
                        <th style="{{ $head }};text-align:left">{{ trans('echovr-match::match.cols.player') }}</th>
                        <th style="{{ $head }}">{{ trans('echovr-match::match.cols.level') }}</th>
                        <th style="{{ $head }}">{{ trans('echovr-match::match.cols.ping') }}</th>
                        <th style="{{ $head }}">{{ trans('echovr-match::match.cols.loss') }}</th>
                        @foreach ($stats as $s)
                            <th style="{{ $head }}">{{ trans('echovr-match::match.cols.' . $s) }}</th>
                        @endforeach
                        @if ($combat)
                            <th style="{{ $head }};text-align:left">{{ trans('echovr-match::match.cols.weapon') }}</th>
                            <th style="{{ $head }};text-align:left">{{ trans('echovr-match::match.cols.ordnance') }}</th>
                            <th style="{{ $head }};text-align:left">{{ trans('echovr-match::match.cols.tac_mod') }}</th>
                        @endif
                    </tr>
                </thead>
                <tbody>
                    @foreach ($players as $p)
                        <tr style="border-bottom:1px solid rgb(127 127 127 / .12)" wire:key="{{ $side }}-{{ $p['user_id'] ?? $p['name'] }}">
                            <td style="{{ $cell }};opacity:.7">{{ $p['number'] ?? '' }}</td>
                            <td style="{{ $cell }};text-align:left;font-weight:500">
                                {{ $p['name'] }}
                                @if (!empty($p['possession'])) <x-filament::icon icon="tabler-disc" style="display:inline-block;width:.9rem;height:.9rem;vertical-align:middle" /> @endif
                                @if (!empty($p['stunned'])) <x-filament::icon icon="tabler-bolt" style="display:inline-block;width:.9rem;height:.9rem;vertical-align:middle" /> @endif
                            </td>
                            <td style="{{ $cell }}">{{ $p['level'] ?? '' }}</td>
                            <td style="{{ $cell }}">{{ $p['ping'] ?? '' }}</td>
                            <td style="{{ $cell }}">{{ isset($p['packet_loss']) ? number_format($p['packet_loss'] * 100, 1) . '%' : '' }}</td>
                            @foreach ($stats as $s)
                                <td style="{{ $cell }}">{{ $p['stats'][$s] ?? 0 }}</td>
                            @endforeach
                            @if ($combat)
                                <td style="{{ $cell }};text-align:left">{{ $p['weapon'] ?? '' }}</td>
                                <td style="{{ $cell }};text-align:left">{{ $p['ordnance'] ?? '' }}</td>
                                <td style="{{ $cell }};text-align:left">{{ $p['tac_mod'] ?? '' }}</td>
                            @endif
                        </tr>
                    @endforeach
                </tbody>
            </table>
        </div>
    @endif
</x-filament::section>
