@php use MarshmallowMia\EchoVrMatch\Services\Format; @endphp
{{-- Big blue : orange score with clock and game status. --}}
@if (isset($m['blue'], $m['orange']))
    <div style="display:flex;align-items:center;gap:.75rem;font-variant-numeric:tabular-nums">
        <span style="font-size:{{ $size ?? '2rem' }};font-weight:700;color:{{ Format::BLUE }}">{{ Format::points($m['blue']['points'] ?? 0, $m['percent'] ?? false) }}</span>
        <span style="font-size:1.25rem;opacity:.5">:</span>
        <span style="font-size:{{ $size ?? '2rem' }};font-weight:700;color:{{ Format::ORANGE }}">{{ Format::points($m['orange']['points'] ?? 0, $m['percent'] ?? false) }}</span>
        <span style="display:flex;flex-direction:column;line-height:1.2;margin-left:.5rem">
            <span style="font-size:1.125rem;font-weight:600">{{ $m['clock_display'] ?? '' }}</span>
            <span style="font-size:.75rem;opacity:.7">
                {{ Format::status($m['status'] ?? null) }}
                @if (!empty($m['paused'])) · {{ trans('echovr-match::match.paused') }} @endif
                @if (!empty($m['contested'])) · {{ trans('echovr-match::match.contested') }} @endif
                @if (!empty($m['rounds']) && ($m['rounds']['total'] ?? 0) > 1)
                    · {{ trans('echovr-match::match.round') }} {{ $m['rounds']['blue'] }}–{{ $m['rounds']['orange'] }} / {{ $m['rounds']['total'] }}
                @endif
            </span>
        </span>
    </div>
@endif
