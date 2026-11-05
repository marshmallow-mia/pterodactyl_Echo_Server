@php
    use MarshmallowMia\EchoVrMatch\Services\Format;
    $m = $match['data'] ?? null;
    $active = $m && ($m['active'] ?? false);
    [$stateLabel, $stateColor] = Format::state($status['state'] ?? null);
@endphp
<x-filament-widgets::widget>
    <x-filament::section>
        <div wire:poll.{{ $poll }} style="display:flex;flex-wrap:wrap;gap:1rem 1.5rem;align-items:center;justify-content:space-between">
            <div style="display:flex;flex-direction:column;gap:.35rem;min-width:12rem">
                <div style="display:flex;gap:.5rem;align-items:center;flex-wrap:wrap">
                    @if ($active)
                        <x-filament::badge color="success" icon="tabler-broadcast">{{ trans('echovr-match::match.live') }}</x-filament::badge>
                    @else
                        <x-filament::badge :color="$stateColor">{{ $stateLabel }}</x-filament::badge>
                    @endif
                    <x-filament::badge :color="$apiOn ? 'gray' : 'warning'" icon="tabler-chart-bar">
                        {{ trans('echovr-match::match.api.label') }}: {{ trans($apiOn ? 'echovr-match::match.api.on' : 'echovr-match::match.api.off') }}
                    </x-filament::badge>
                </div>
                <div style="font-size:1.125rem;font-weight:600">
                    @if ($m)
                        {{ Format::title($m) }}
                        @if (!$active)
                            <span style="font-size:.8rem;font-weight:400;opacity:.7">· {{ trans('echovr-match::match.last_match') }} {{ Format::when($m['ended_at'] ?? null) }}</span>
                        @endif
                    @else
                        {{ trans('echovr-match::match.no_match') }}
                    @endif
                </div>
            </div>

            @if ($m && !Format::isLobby($m))
                @include('echovr-match::partials.score', ['m' => $m, 'size' => '1.75rem'])
            @endif

            <div style="display:flex;gap:.75rem;align-items:center;flex-wrap:wrap">
                @if ($active && ($m['players'] ?? 0) > 0)
                    <span style="font-size:.875rem;opacity:.8">{{ trans('echovr-match::match.players', ['count' => $m['players'] ?? 0]) }}</span>
                @endif
                <x-filament::button tag="a" :href="$pageUrl" color="gray" size="sm" icon="tabler-trophy">
                    {{ trans('echovr-match::match.scoreboard') }}
                </x-filament::button>
                @if ($canToggle)
                    <x-filament::button size="sm" :color="$apiOn ? 'gray' : 'primary'" wire:click="toggleApi"
                        wire:confirm="{{ trans('echovr-match::match.api.confirm') }}">
                        {{ trans($apiOn ? 'echovr-match::match.api.turn_off' : 'echovr-match::match.api.turn_on') }}
                    </x-filament::button>
                @endif
            </div>
        </div>
        @include('echovr-match::partials.notice')
    </x-filament::section>
</x-filament-widgets::widget>
