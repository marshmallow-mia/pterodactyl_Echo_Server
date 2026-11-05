@php use MarshmallowMia\EchoVrMatch\Services\Format; @endphp
{{-- Big blue : orange score with clock and what the match is doing. --}}
@if (isset($m['blue'], $m['orange']))
    @php
        $r = $m['rounds'] ?? null;
        $roundWins = $r && (($m['percent'] ?? false) || ($r['total'] ?? 0) > 1) && (($r['blue'] ?? 0) + ($r['orange'] ?? 0)) > 0;
    @endphp
    <div style="display:flex;align-items:center;gap:.75rem;font-variant-numeric:tabular-nums">
        <span style="font-size:{{ $size ?? '2rem' }};font-weight:700;color:{{ Format::BLUE }}">{{ Format::points($m['blue']['points'] ?? 0, $m['percent'] ?? false) }}</span>
        <span style="font-size:1.25rem;opacity:.5">:</span>
        <span style="font-size:{{ $size ?? '2rem' }};font-weight:700;color:{{ Format::ORANGE }}">{{ Format::points($m['orange']['points'] ?? 0, $m['percent'] ?? false) }}</span>
        <span style="display:flex;flex-direction:column;line-height:1.2;margin-left:.5rem">
            <span style="font-size:1.125rem;font-weight:600">
                @if (!empty($m['round']) && empty($m['clock_display']))
                    {{ trans('echovr-match::match.round_n', ['n' => $m['round']]) }}
                @endif
                {{ Format::clock($m) }}
            </span>
            <span style="font-size:.75rem;opacity:.7">
                {{ Format::phase($m) }}
                @if (!empty($m['paused']))
                    · {{ trans('echovr-match::match.paused') }}
                    @if (!empty($m['paused_by']))
                        ({{ trans('echovr-match::match.' . $m['paused_by']) }})
                    @endif
                @endif
                @if (!empty($m['contested'])) · {{ trans('echovr-match::match.contested') }} @endif
                @if ($roundWins)
                    · {{ trans('echovr-match::match.round_wins') }}
                    <span style="color:{{ Format::BLUE }}">{{ $r['blue'] }}</span>–<span style="color:{{ Format::ORANGE }}">{{ $r['orange'] }}</span>
                    @if (($r['total'] ?? 0) > 1) / {{ $r['total'] }} @endif
                @endif
                @if (!empty($m['restart_request']))
                    · {{ trans('echovr-match::match.restart_request.' . $m['restart_request']) }}
                @endif
            </span>
        </span>
    </div>
@endif
